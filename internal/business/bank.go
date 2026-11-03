package business

import (
	"context"
	"crypto/subtle"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func (s *Service) tokenTx(ctx context.Context, tx pgx.Tx, owner string) (string, error) {
	var ciphertext, nonce []byte
	if err := tx.QueryRow(ctx, `SELECT bank_token_ciphertext,bank_token_nonce FROM business_workspaces WHERE owner_id=$1::uuid`, owner).Scan(&ciphertext, &nonce); err != nil {
		return "", err
	}
	if len(ciphertext) == 0 {
		return "", unavailable("Банк не подключён")
	}
	return s.cipher.decrypt(owner, ciphertext, nonce)
}
func (s *Service) loadToken(ctx context.Context, owner string) (string, error) {
	var ciphertext, nonce []byte
	err := s.pool.QueryRow(ctx, `SELECT bank_token_ciphertext,bank_token_nonce FROM business_workspaces WHERE owner_id=$1::uuid`, owner).Scan(&ciphertext, &nonce)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && len(ciphertext) == 0 {
		return "", unavailable("Банк не подключён")
	}
	if err != nil {
		return "", err
	}
	return s.cipher.decrypt(owner, ciphertext, nonce)
}
func (s *Service) Connect(ctx context.Context, owner, token string) (Bank, error) {
	if s.bank == nil {
		return Bank{}, unavailable("Банк не настроен")
	}
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 16384 || strings.ContainsAny(token, "\r\n\x00") {
		return Bank{}, invalid("Укажите корректный токен банка")
	}
	accounts, err := s.bank.Accounts(ctx, token)
	if err != nil {
		return Bank{}, publicBankError(err)
	}
	ciphertext, nonce, err := s.cipher.encrypt(owner, token)
	if err != nil {
		return Bank{}, err
	}
	var result Bank
	err = s.mutate(ctx, owner, func(current *state, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE business_workspaces SET bank_token_ciphertext=$2,bank_token_nonce=$3 WHERE owner_id=$1::uuid`, owner, ciphertext, nonce); err != nil {
			return err
		}
		current.Bank.Connected, current.Bank.AccountCount, current.Bank.Error = true, len(accounts), ""
		// A new credential can expose different accounts even at the same count.
		// Old credits remain valid history, but cannot prove this scope's coverage.
		current.Coverage = []Coverage{}
		current.Bank.CoverageStart, current.Bank.CoverageEnd, current.Bank.LastSyncedAt = "", "", ""
		result = current.Bank
		return nil
	})
	return result, err
}
func (s *Service) Disconnect(ctx context.Context, owner string) error {
	return s.mutate(ctx, owner, func(current *state, tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE business_workspaces SET bank_token_ciphertext=NULL,bank_token_nonce=NULL WHERE owner_id=$1::uuid`, owner); err != nil {
			return err
		}
		current.Bank.Connected, current.Bank.Error = false, ""
		return nil
	})
}
func (s *Service) Sync(ctx context.Context, owner, from, to string) (Bank, error) {
	if s.bank == nil {
		return Bank{}, unavailable("Банк не настроен")
	}
	if !validDate(from) || !validDate(to) || from > to {
		return Bank{}, invalid("Неверный период выписки")
	}
	start, _ := time.Parse(time.DateOnly, from)
	end, _ := time.Parse(time.DateOnly, to)
	if end.Sub(start) >= 366*24*time.Hour || to > s.now().In(time.FixedZone("MSK", 3*3600)).Format(time.DateOnly) {
		return Bank{}, invalid("Выберите период до 366 дней без будущих дат")
	}
	token, err := s.loadToken(ctx, owner)
	if err != nil {
		return Bank{}, err
	}
	readCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	accounts, err := s.bank.Accounts(readCtx, token)
	var payments []Payment
	if err == nil {
		payments, err = s.bank.Statements(readCtx, token, accounts, from, to)
	}
	if err != nil {
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer finishCancel()
		_ = s.mutate(finishCtx, owner, func(current *state, _ pgx.Tx) error {
			current.Bank.Error = "Не удалось получить полную выписку. Покрытие не обновлено."
			return nil
		})
		return Bank{}, publicBankError(err)
	}
	var result Bank
	err = s.mutate(ctx, owner, func(current *state, tx pgx.Tx) error {
		currentToken, err := s.tokenTx(ctx, tx, owner)
		if err != nil {
			return err
		}
		if subtle.ConstantTimeCompare([]byte(currentToken), []byte(token)) != 1 {
			return conflict("Подключение банка изменилось; запросите выписку заново")
		}
		newPayments := []Payment{}
		for _, payment := range payments {
			payment.CounterpartyID = uniquePayerID(*current, payment.PayerINN)
			if exists, err := mergeExistingPayment(current, payment); err != nil {
				return err
			} else if exists {
				continue
			}
			current.Payments = append(current.Payments, payment)
			newPayments = append(newPayments, payment)
		}
		automaticAllocations(current, newPayments)
		// Only this full retrieval proves the current credential's account scope.
		current.Coverage = []Coverage{}
		addCoverage(current, from, to)
		current.Bank.LastSyncedAt, current.Bank.AccountCount, current.Bank.Error = s.now().UTC().Format(time.RFC3339), len(accounts), ""
		result = current.Bank
		return nil
	})
	return result, err
}

func uniquePayerID(current state, inn string) string {
	if inn == "" {
		return ""
	}
	matched := ""
	for _, party := range current.Counterparties {
		if party.INN == inn {
			if matched != "" {
				return ""
			}
			matched = party.ID
		}
	}
	return matched
}

func mergeExistingPayment(current *state, incoming Payment) (bool, error) {
	if incoming.CounterpartyID != "" {
		if _, exists := findParty(*current, incoming.CounterpartyID); !exists {
			return false, invalid("Платёж ссылается на неизвестного плательщика")
		}
	}
	for index, existing := range current.Payments {
		if existing.ID != incoming.ID {
			continue
		}
		providerExisting, providerIncoming := existing, incoming
		providerExisting.CounterpartyID, providerIncoming.CounterpartyID = "", ""
		if !reflect.DeepEqual(providerExisting, providerIncoming) {
			return true, conflict("Сохранённый платёж содержит другие банковские данные; нужна ручная проверка")
		}
		if existing.CounterpartyID == "" {
			matched := uniquePayerID(*current, incoming.PayerINN)
			if incoming.CounterpartyID != "" && incoming.CounterpartyID != matched {
				return true, conflict("Связь платежа с плательщиком требует ручной проверки")
			}
			existing.CounterpartyID = matched
			current.Payments[index] = existing
		}
		// An existing association is retained; enrichment never makes this an
		// unseen credit or replays automatic allocations after a reversal.
		return true, nil
	}
	return false, nil
}

func addCoverage(current *state, from, to string) {
	spans := append(current.Coverage, Coverage{From: from, To: to})
	sort.Slice(spans, func(i, j int) bool { return spans[i].From < spans[j].From })
	merged := []Coverage{}
	for _, span := range spans {
		if len(merged) == 0 {
			merged = append(merged, span)
			continue
		}
		last := &merged[len(merged)-1]
		end, _ := time.Parse(time.DateOnly, last.To)
		if span.From <= end.AddDate(0, 0, 1).Format(time.DateOnly) {
			if span.To > last.To {
				last.To = span.To
			}
		} else {
			merged = append(merged, span)
		}
	}
	current.Coverage = merged
	// Metadata describes the latest contiguous span; gaps never become coverage.
	latest := merged[len(merged)-1]
	current.Bank.CoverageStart, current.Bank.CoverageEnd = latest.From, latest.To
}

// This is a document-reference grammar, not payer/purpose keyword inference.
var invoiceNumberReference = regexp.MustCompile(`(?i)(?:^|[^\pL\pN])сч(?:[её]т(?:у|а|ом)?)?\.?\s*(?:№|N(?:o)?\.?)?\s*([0-9][0-9A-Za-zА-Яа-я./_-]*)`)
var datedInvoiceReference = regexp.MustCompile(`(?i)(?:^|[^\pL\pN])сч(?:[её]т(?:у|а|ом)?)?\.?\s*(?:№|N(?:o)?\.?)?\s*([0-9][0-9A-Za-zА-Яа-я./_-]*)\s*от\s*([0-9]{1,2}\.[0-9]{1,2}\.[0-9]{4}|[0-9]{4}-[0-9]{2}-[0-9]{2})`)

type receiptScope struct{ partyID, templateID, start, end string }
type paymentMatch struct {
	classification string
	scope          receiptScope
	targets        []Invoice
	canAdvance     bool
}

func ownPayment(s state, p Payment) bool {
	return s.Seller.INN != "" && p.PayerINN != "" && p.PayerINN == s.Seller.INN
}
func accountingPeriods(s state) []string {
	set := map[string]bool{}
	for _, i := range s.Invoices {
		if i.IsCurrent {
			set[i.Period] = true
		}
	}
	result := []string{}
	for p := range set {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}
func nextMonth(period string) string {
	date, _ := time.Parse("2006-01", period)
	return date.AddDate(0, 1, 0).Format("2006-01")
}

func matchPayment(s state, p Payment) paymentMatch {
	for _, id := range s.HistoricalPaymentIDs {
		if id == p.ID {
			return paymentMatch{classification: "historical"}
		}
	}
	unknown := paymentMatch{classification: "unclassified"}
	periods := accountingPeriods(s)
	if len(periods) > 0 && p.Date < periods[0]+"-01" {
		unknown.classification = "outside_period"
	}
	refs := datedInvoiceReference.FindAllStringSubmatch(p.Description, -1)
	if len(refs) != 1 || len(invoiceNumberReference.FindAllStringSubmatch(p.Description, -1)) != 1 || p.PayerINN == "" {
		return unknown
	}
	issued, err := time.Parse("2.1.2006", refs[0][2])
	if err != nil {
		issued, err = time.Parse(time.DateOnly, refs[0][2])
	}
	if err != nil {
		return unknown
	}
	scopes := map[receiptScope]bool{}
	for _, invoice := range s.Invoices {
		party, ok := findParty(s, invoice.CounterpartyID)
		if ok && party.INN == p.PayerINN && invoice.Number == refs[0][1] && invoice.IssueDate == issued.Format(time.DateOnly) {
			scopes[receiptScope{invoice.CounterpartyID, invoice.TemplateID, invoice.PeriodStart, invoice.PeriodEnd}] = true
		}
	}
	if len(scopes) == 0 {
		return unknown
	}
	if len(periods) == 0 {
		return unknown
	}
	allPast := true
	for scope := range scopes {
		if scope.end >= periods[0]+"-01" {
			allPast = false
		}
	}
	if allPast {
		return paymentMatch{classification: "historical"}
	}
	if len(scopes) != 1 {
		return unknown
	}
	var scope receiptScope
	for candidate := range scopes {
		scope = candidate
	}
	if p.CounterpartyID != "" && p.CounterpartyID != scope.partyID {
		return unknown
	}
	if !validDate(scope.start) || !validDate(scope.end) {
		return unknown
	}
	start, _, err := periodDates(scope.start[:7])
	if err != nil || scope.start != start {
		return unknown
	}
	_, end, err := periodDates(scope.end[:7])
	if err != nil || scope.end != end {
		return unknown
	}
	current := []Invoice{}
	for _, invoice := range s.Invoices {
		if !invoice.IsCurrent || invoice.CounterpartyID != scope.partyID || invoice.TemplateID != scope.templateID {
			continue
		}
		start, end, err := periodDates(invoice.Period)
		if err != nil || invoice.PeriodStart != start || invoice.PeriodEnd != end {
			return unknown
		}
		current = append(current, invoice)
	}
	sort.Slice(current, func(i, j int) bool { return current[i].Period < current[j].Period })
	if len(current) == 0 || scope.start[:7] < current[0].Period {
		return unknown
	}
	matched := paymentMatch{classification: "rental", scope: scope, targets: []Invoice{}, canAdvance: true}
	expected := scope.start[:7]
	cumulative := scope.start[:7] != scope.end[:7]
	for _, invoice := range current {
		if invoice.Period < scope.start[:7] {
			continue
		}
		if cumulative && invoice.Period > scope.end[:7] {
			break
		}
		if invoice.Period != expected {
			matched.canAdvance = false
			break
		}
		matched.targets = append(matched.targets, invoice)
		expected = nextMonth(expected)
	}
	if expected <= current[len(current)-1].Period && (!cumulative || expected <= scope.end[:7]) {
		matched.canAdvance = false
	}
	if len(matched.targets) == 0 && !matched.canAdvance {
		return unknown
	}
	return matched
}

func automaticAllocations(current *state, newPayments []Payment) {
	// A repeated sync must not undo a user's explicit allocation reversal.
	sort.SliceStable(newPayments, func(i, j int) bool {
		if newPayments[i].Date == newPayments[j].Date {
			return newPayments[i].ID < newPayments[j].ID
		}
		return newPayments[i].Date < newPayments[j].Date
	})
	for _, payment := range newPayments {
		if payment.Source != "tochka" || ownPayment(*current, payment) {
			continue
		}
		already := false
		for _, a := range current.Allocations {
			if a.PaymentID == payment.ID {
				already = true
			}
		}
		if already {
			continue
		}
		matched := matchPayment(*current, payment)
		if matched.classification != "rental" {
			continue
		}
		remaining := payment.AmountMinor
		for _, invoice := range matched.targets {
			amount := min(remaining, viewInvoice(*current, invoice).OutstandingMinor)
			if amount <= 0 {
				continue
			}
			current.Allocations = append(current.Allocations, Allocation{ID: uuid.NewString(), InvoiceID: invoice.ID, PaymentID: payment.ID, AmountMinor: amount, Automatic: true})
			remaining -= amount
		}
	}
}

func publicBankError(err error) error {
	var safe *workout.Error
	if errors.As(err, &safe) {
		return err
	}
	return bankFailure()
}
