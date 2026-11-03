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

// Only this explicit invoice-reference grammar can auto-allocate a bank credit.
// Equal amounts, payer names and monthly coincidence never establish a match.
var explicitInvoiceReference = regexp.MustCompile(`(?i)сч[её]т(?:у|а)?\s*(?:№|N(?:o)?\.?)\s*([0-9A-Za-zА-Яа-я./-]+)`)
var explicitYear = regexp.MustCompile(`\b20[0-9]{2}\b`)

func automaticAllocations(current *state, newPayments []Payment) {
	// A repeated sync must not undo a user's explicit allocation reversal.
	sort.SliceStable(newPayments, func(i, j int) bool {
		if newPayments[i].Date == newPayments[j].Date {
			return newPayments[i].ID < newPayments[j].ID
		}
		return newPayments[i].Date < newPayments[j].Date
	})
	for _, payment := range newPayments {
		if payment.Source != "tochka" || payment.PayerINN == "" {
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
		references := explicitInvoiceReference.FindAllStringSubmatch(payment.Description, -1)
		if len(references) != 1 {
			continue
		}
		years := explicitYear.FindAllString(payment.Description, -1)
		yearSet := map[string]bool{}
		for _, year := range years {
			yearSet[year] = true
		}
		if len(yearSet) != 1 {
			continue
		}
		var matched *Invoice
		for i := range current.Invoices {
			invoice := &current.Invoices[i]
			party, ok := findParty(*current, invoice.CounterpartyID)
			if !ok || !invoice.IsCurrent || party.INN != payment.PayerINN || invoice.Number != references[0][1] || !yearSet[invoice.IssueDate[:4]] {
				continue
			}
			if matched != nil {
				matched = nil
				break
			}
			matched = invoice
		}
		if matched == nil {
			continue
		}
		ambiguous := false
		for _, historical := range current.Invoices {
			party, ok := findParty(*current, historical.CounterpartyID)
			if !ok || historical.IsCurrent || party.INN != payment.PayerINN || historical.Number != matched.Number || historical.IssueDate[:4] != matched.IssueDate[:4] {
				continue
			}
			if historical.TemplateID != matched.TemplateID || historical.PeriodStart != matched.PeriodStart || historical.PeriodEnd != matched.PeriodEnd {
				ambiguous = true
				break
			}
		}
		if ambiguous {
			continue
		}
		view := viewInvoice(*current, *matched)
		if payment.AmountMinor > view.OutstandingMinor {
			continue
		}
		current.Allocations = append(current.Allocations, Allocation{ID: uuid.NewString(), InvoiceID: matched.ID, PaymentID: payment.ID, AmountMinor: payment.AmountMinor, Automatic: true})
	}
}

func publicBankError(err error) error {
	var safe *workout.Error
	if errors.As(err, &safe) {
		return err
	}
	return bankFailure()
}
