package business

import (
	"context"
	"reflect"
	"testing"
)

func rentalInvoice(id, period, number string, amount int64) Invoice {
	i := fixtureInvoice(id, period, true)
	i.Number = number
	i.AmountMinor = amount
	i.Items = []LineItem{{Description: "Аренда", Quantity: "1", Unit: "мес.", PriceMinor: amount, AmountMinor: amount}}
	return i
}
func rentalState() state {
	s := emptyState()
	s.Seller = fixtureSeller()
	s.Counterparties = []Counterparty{fixtureParty()}
	s.Invoices = []Invoice{rentalInvoice("september", "2026-09", "1", 10000), rentalInvoice("october", "2026-10", "2", 10000)}
	return s
}
func rentalPayment(id, date string, amount int64, description string) Payment {
	return Payment{ID: id, Date: date, AmountMinor: amount, Currency: "RUB", CounterpartyID: "payer-one", PayerINN: "7701234567", Description: description, Source: "tochka"}
}
func cumulativeSource(s *state, start, end string) {
	i := rentalInvoice("cumulative", "2026-09", "1", 40000)
	i.IsCurrent = false
	i.IssueDate = "2026-09-16"
	i.PeriodStart = start
	i.PeriodEnd = end
	s.Invoices = append(s.Invoices, i)
}
func paymentViewByID(t *testing.T, views []PaymentView, id string) PaymentView {
	t.Helper()
	for _, v := range views {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("payment %s missing", id)
	return PaymentView{}
}

func TestBusinessPartialPaymentsCloseOneInvoice(t *testing.T) {
	s := rentalState()
	s.Invoices = s.Invoices[:1]
	s.Invoices[0] = rentalInvoice("september", "2026-09", "1", 17000)
	a := rentalPayment("first", "2026-09-20", 10000, "по сч 1 от01.09.2026")
	b := rentalPayment("second", "2026-09-21", 7000, "по сч №1 от 01.09.2026")
	s.Payments = []Payment{a, b}
	automaticAllocations(&s, []Payment{b, a})
	v := workspaceView(s, "all", "")
	if len(s.Allocations) != 2 || v.Summary.PaidMinor != 17000 || v.Summary.OutstandingMinor != 0 || v.Summary.AdvanceMinor != 0 || v.Summary.UnallocatedMinor != 0 {
		t.Fatalf("partial settlement = %#v", v.Summary)
	}
}

func TestBusinessPaymentSplitsCurrentInvoicesAndKeepsAdvance(t *testing.T) {
	s := rentalState()
	p := rentalPayment("payment", "2026-09-20", 25000, "по счету №1 от 01.09.2026")
	s.Payments = []Payment{p}
	automaticAllocations(&s, []Payment{p})
	addCoverage(&s, "2026-09-01", "2026-10-31")
	v := workspaceView(s, "all", "")
	pv := paymentViewByID(t, v.Payments, p.ID)
	if len(s.Allocations) != 2 || v.Summary.PaidMinor != 20000 || v.Summary.OutstandingMinor != 0 || pv.UnallocatedMinor != 5000 || pv.AdvanceMinor != 5000 || v.Counterparties[0].AdvanceMinor != 5000 || !v.Summary.ReconciliationComplete {
		t.Fatalf("split/advance = %#v %#v", v.Summary, pv)
	}
	if !reflect.DeepEqual(v.AccountingPeriods, []string{"2026-09", "2026-10"}) {
		t.Fatalf("accounting periods = %#v", v.AccountingPeriods)
	}
}

func TestBusinessCumulativeSourceUsesExactDateAndDeduplicatesVersions(t *testing.T) {
	s := rentalState()
	cumulativeSource(&s, "2026-09-01", "2026-12-31")
	duplicate := s.Invoices[2]
	duplicate.ID = "second-version"
	duplicate.Revision = 2
	s.Invoices = append(s.Invoices, duplicate)
	p := rentalPayment("payment", "2026-09-20", 40000, "по сч 1 от16.09.2026")
	s.Payments = []Payment{p}
	automaticAllocations(&s, []Payment{p})
	v := workspaceView(s, "all", "")
	if len(s.Allocations) != 2 || v.Summary.InvoiceCount != 2 || v.Summary.InvoicedMinor != 20000 || v.Summary.PaidMinor != 20000 || v.Summary.AdvanceMinor != 20000 || v.Payments[0].Classification != "rental" {
		t.Fatalf("cumulative settlement = %#v %#v", v.Summary, s.Allocations)
	}
	// The current invoice with the same number has a different issue date.
	monthly := rentalPayment("monthly", "2026-09-20", 10000, "по сч 1 от01.09.2026")
	if matchPayment(s, monthly).scope.end != "2026-09-30" {
		t.Fatal("number reuse overrode the exact issue date")
	}
}

func TestBusinessCumulativeMappingStopsAtMissingPrefixOrGap(t *testing.T) {
	for _, test := range []struct {
		name, start string
		gap         bool
		wantPaid    int64
	}{{"unknown old prefix", "2026-08-01", false, 0}, {"missing middle month", "2026-09-01", true, 10000}} {
		t.Run(test.name, func(t *testing.T) {
			s := rentalState()
			if test.gap {
				s.Invoices[1] = rentalInvoice("november", "2026-11", "3", 10000)
			}
			cumulativeSource(&s, test.start, "2026-12-31")
			p := rentalPayment("payment", "2026-09-20", 40000, "по сч №1 от16.09.2026")
			s.Payments = []Payment{p}
			automaticAllocations(&s, []Payment{p})
			v := workspaceView(s, "all", "")
			if v.Summary.PaidMinor != test.wantPaid || v.Summary.AdvanceMinor != 0 {
				t.Fatalf("missing-period guard = %#v", v.Summary)
			}
			for _, a := range s.Allocations {
				if a.InvoiceID == "november" {
					t.Fatal("allocation jumped over a missing month")
				}
			}
		})
	}
}

func TestBusinessConflictingExactScopesRemainUnclassified(t *testing.T) {
	s := rentalState()
	cumulativeSource(&s, "2026-09-01", "2026-12-31")
	s.Invoices[2].IssueDate = "2026-09-01"
	p := rentalPayment("payment", "2026-09-20", 40000, "по сч 1 от01.09.2026")
	s.Payments = []Payment{p}
	automaticAllocations(&s, []Payment{p})
	v := workspaceView(s, "all", "")
	if len(s.Allocations) != 0 || v.Payments[0].Classification != "unclassified" || v.Summary.AdvanceMinor != 0 {
		t.Fatal("conflicting source ranges were guessed")
	}
}

func TestBusinessHistoricalReferenceAndOutsideDateAreDistinct(t *testing.T) {
	s := rentalState()
	old := rentalInvoice("old", "2026-08", "7", 10000)
	old.IsCurrent = false
	old.IssueDate = "2026-09-01"
	s.Invoices = append(s.Invoices, old)
	variant := old
	variant.ID = "older-cumulative"
	variant.PeriodStart = "2026-02-01"
	s.Invoices = append(s.Invoices, variant)
	historical := rentalPayment("historical", "2026-09-20", 10000, "по сч №7 от01.09.2026")
	outside := rentalPayment("outside", "2026-08-20", 5000, "Перечисление по договору")
	prepayment := rentalPayment("prepayment", "2026-08-25", 10000, "по сч №1 от01.09.2026")
	s.Payments = []Payment{historical, outside, prepayment}
	automaticAllocations(&s, s.Payments)
	v := workspaceView(s, "all", "")
	h := paymentViewByID(t, v.Payments, historical.ID)
	o := paymentViewByID(t, v.Payments, outside.ID)
	p := paymentViewByID(t, v.Payments, prepayment.ID)
	if h.Classification != "historical" || h.UnallocatedMinor != 0 || h.AdvanceMinor != 0 || o.Classification != "outside_period" || o.UnallocatedMinor != 0 || o.AdvanceMinor != 0 || p.Classification != "rental" || p.AllocatedMinor != 10000 {
		t.Fatalf("cash classes = %#v", v.Payments)
	}
	if v.Summary.ReceivedMinor != 0 || v.Summary.HistoricalReceivedMinor != 10000 || v.Summary.PaidMinor != 10000 || v.Summary.UnallocatedMinor != 0 {
		t.Fatalf("cash and allocated totals = %#v", v.Summary)
	}
	august := workspaceView(s, "2026-08", "")
	if august.Summary.ReceivedMinor != 10000 || august.Summary.HistoricalReceivedMinor != 5000 {
		t.Fatalf("outside-month cash = %#v", august.Summary)
	}
}

func TestBusinessOwnTransfersDoNotEnterPublicAccounting(t *testing.T) {
	s := rentalState()
	s.Invoices = s.Invoices[:1]
	own := rentalPayment("own", "2026-09-20", 5000, "по сч №1 от01.09.2026")
	own.PayerINN = s.Seller.INN
	external := rentalPayment("external", "2026-09-20", 5000, "по сч №1 от01.09.2026")
	s.Payments = []Payment{own, external}
	automaticAllocations(&s, s.Payments)
	// Even a legacy own-transfer allocation must not inflate public paid totals.
	s.Allocations = append(s.Allocations, Allocation{ID: "legacy-own", InvoiceID: "september", PaymentID: own.ID, AmountMinor: 5000})
	v := workspaceView(s, "all", "")
	if len(s.Payments) != 2 || len(v.Payments) != 1 || v.Summary.ReceivedMinor != 5000 || v.Summary.PaidMinor != 5000 || v.Summary.OutstandingMinor != 5000 || v.Summary.AdvanceMinor != 0 {
		t.Fatalf("own transfer leaked = %#v", v.Summary)
	}
}

func TestBusinessAdvanceDoesNotNetAcrossContracts(t *testing.T) {
	s := rentalState()
	s.Invoices = s.Invoices[:1]
	second := s.Invoices[0]
	second.ID = "other-contract"
	second.TemplateID = "other-rent"
	second.Number = "9"
	s.Invoices = append(s.Invoices, second)
	p := rentalPayment("payment", "2026-09-20", 20000, "по сч №1 от01.09.2026")
	s.Payments = []Payment{p}
	automaticAllocations(&s, []Payment{p})
	v := workspaceView(s, "all", "")
	if len(s.Allocations) != 1 || s.Allocations[0].InvoiceID != "september" || v.Summary.OutstandingMinor != 10000 || v.Summary.AdvanceMinor != 10000 || v.Counterparties[0].OutstandingMinor != 10000 || v.Counterparties[0].AdvanceMinor != 10000 {
		t.Fatalf("contracts were netted = %#v", v.Summary)
	}
}

func TestBusinessAdvanceReservationCountsReleasedNeedOnlyOnce(t *testing.T) {
	s := rentalState()
	cumulativeSource(&s, "2026-09-01", "2026-12-31")
	small := rentalPayment("small", "2026-09-18", 10000, "по сч №1 от16.09.2026")
	large := rentalPayment("large", "2026-09-19", 30000, "по сч №1 от16.09.2026")
	s.Payments = []Payment{small, large}
	automaticAllocations(&s, s.Payments)
	before := paymentViewByID(t, paymentViews(s), large.ID)
	if before.AdvanceMinor != 20000 {
		t.Fatalf("initial advance = %d", before.AdvanceMinor)
	}
	for i, a := range s.Allocations {
		if a.PaymentID == small.ID {
			s.Allocations = append(s.Allocations[:i], s.Allocations[i+1:]...)
			break
		}
	}
	views := paymentViews(s)
	sv := paymentViewByID(t, views, small.ID)
	lv := paymentViewByID(t, views, large.ID)
	if sv.UnallocatedMinor != 10000 || sv.AdvanceMinor != 0 || lv.AdvanceMinor != before.AdvanceMinor {
		t.Fatalf("released need counted twice: small %#v large %#v", sv, lv)
	}
	// Existing rows are not new credits, so a repeated sync must not repair it.
	automaticAllocations(&s, []Payment{})
	if workspaceView(s, "all", "").Summary.PaidMinor != 10000 {
		t.Fatal("reversal was silently reapplied")
	}
}

func TestBusinessAdvanceReservationIgnoresMonthFilter(t *testing.T) {
	s := rentalState()
	p := rentalPayment("payment", "2026-09-20", 12000, "по сч №1 от01.09.2026")
	s.Payments = []Payment{p}
	s.Allocations = []Allocation{{ID: "september-only", InvoiceID: "september", PaymentID: p.ID, AmountMinor: 10000}}
	september := workspaceView(s, "2026-09", "")
	if september.Summary.OutstandingMinor != 0 || september.Summary.UnallocatedMinor != 2000 || september.Summary.AdvanceMinor != 0 || september.Counterparties[0].AdvanceMinor != 0 {
		t.Fatal("unpaid October was presented as September advance")
	}
	s.Allocations = append(s.Allocations, Allocation{ID: "october-paid", InvoiceID: "october", PaymentID: p.ID, AmountMinor: 2000})
	// A separate recognized surplus stays global even when another month is selected.
	s.Invoices[1].AmountMinor = 2000
	extra := rentalPayment("advance", "2026-10-20", 5000, "по сч №1 от01.09.2026")
	s.Payments = append(s.Payments, extra)
	for _, period := range []string{"all", "2026-09", "2026-10"} {
		v := workspaceView(s, period, "")
		if v.Summary.AdvanceMinor != 5000 || v.Counterparties[0].AdvanceMinor != 5000 {
			t.Fatalf("advance changed with period %s: %#v", period, v.Summary)
		}
	}
}

func TestBusinessManualAllocationRejectsOwnAndNonCurrentCash(t *testing.T) {
	service, owner := testService(t, nil)
	ctx := context.Background()
	request := importFixture()
	old := rentalInvoice("past-bill", "2026-08", "7", 10000)
	old.IsCurrent = false
	old.IssueDate = "2026-09-01"
	request.Invoices = append(request.Invoices, old)
	own := rentalPayment("own", "2026-09-20", 10000, "Перевод")
	own.PayerINN = request.Seller.INN
	past := rentalPayment("past", "2026-09-20", 10000, "по сч №7 от01.09.2026")
	outside := rentalPayment("outside", "2026-08-20", 10000, "Перевод")
	request.Payments = []Payment{own, past, outside}
	if _, err := service.Import(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	for _, payment := range request.Payments {
		_, err := service.Allocate(ctx, owner, AllocateRequest{InvoiceID: "current", PaymentID: payment.ID, AmountMinor: 1})
		requireStatus(t, err, 409)
	}
}

func TestBusinessExplicitLegacyHistoryOverridesInvoiceEvidence(t *testing.T) {
	s := rentalState()
	cumulativeSource(&s, "2026-09-01", "2026-12-31")
	legacy := rentalPayment("legacy", "2026-09-20", 40000, "по сч №1 от16.09.2026")
	fresh := rentalPayment("new", "2026-09-21", 5000, "по сч №1 от16.09.2026")
	s.Payments = []Payment{legacy, fresh}
	s.HistoricalPaymentIDs = []string{legacy.ID}
	automaticAllocations(&s, s.Payments)
	v := workspaceView(s, "all", "")
	old := paymentViewByID(t, v.Payments, legacy.ID)
	if len(s.Allocations) != 1 || s.Allocations[0].PaymentID != fresh.ID || old.Classification != "historical" || old.UnallocatedMinor != 0 || old.AdvanceMinor != 0 || v.Summary.ReceivedMinor != 5000 || v.Summary.HistoricalReceivedMinor != 40000 || v.Summary.PaidMinor != 5000 || v.Summary.AdvanceMinor != 0 || v.Counterparties[0].LastPaymentAt != fresh.Date {
		t.Fatalf("owner legacy boundary ignored: %#v %#v", v.Summary, v.Payments)
	}
	s.Payments = s.Payments[:1]
	s.Allocations = nil
	historyOnly := workspaceView(s, "all", "")
	if historyOnly.Counterparties[0].LastPaymentAt != "" || historyOnly.Counterparties[0].ReceivedMinor != 0 {
		t.Fatal("legacy history was presented as a current payer receipt")
	}
}

func TestBusinessExplicitLegacyHistorySurvivesSyncAndReversal(t *testing.T) {
	fixture := &mcpFixture{t: t}
	client := testBankClient(t, fixture.roundTrip)
	ctx := context.Background()
	credits, err := client.Statements(ctx, mcpTestToken, []BankAccount{{ID: "40817810802000000008/044525104"}}, "2026-09-01", "2026-09-30")
	if err != nil || len(credits) != 1 {
		t.Fatalf("synthetic credit = %#v %v", credits, err)
	}
	credits[0].CounterpartyID = "payer-one"
	service, owner := testService(t, client)
	if _, err := service.Connect(ctx, owner, mcpTestToken); err != nil {
		t.Fatal(err)
	}
	request := importFixture()
	request.Payments = credits
	request.HistoricalPaymentIDs = []string{credits[0].ID}
	if _, err := service.Import(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, err := service.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
			t.Fatal(err)
		}
	}
	v, err := service.Workspace(ctx, owner, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Payments) != 1 || v.Payments[0].Classification != "historical" || v.Summary.PaidMinor != 0 || v.Summary.ReceivedMinor != 0 || v.Summary.HistoricalReceivedMinor != 10000 || v.Counterparties[0].LastPaymentAt != "" {
		t.Fatalf("sync reopened explicit legacy credit: %#v %#v", v.Summary, v.Payments)
	}
	if _, err := service.Import(ctx, owner, ImportRequest{HistoricalPaymentIDs: request.HistoricalPaymentIDs}); err != nil {
		t.Fatal(err)
	}
	newCredit := rentalPayment("future-credit", "2026-09-22", 5000, "по сч №17 от01.09.2026")
	if _, err := service.Import(ctx, owner, ImportRequest{Payments: []Payment{newCredit}}); err != nil {
		t.Fatal(err)
	}
	v, err = service.Workspace(ctx, owner, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	current := paymentViewByID(t, v.Payments, newCredit.ID)
	if current.Classification != "rental" || len(current.Allocations) != 1 || current.AllocatedMinor != 5000 {
		t.Fatal("a new transaction inherited an unrelated legacy override")
	}
	if err := service.ReverseAllocation(ctx, owner, current.Allocations[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
		t.Fatal(err)
	}
	v, err = service.Workspace(ctx, owner, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Summary.PaidMinor != 0 || paymentViewByID(t, v.Payments, newCredit.ID).AllocatedMinor != 0 || paymentViewByID(t, v.Payments, credits[0].ID).Classification != "historical" || v.Bank.CoverageStart != "2026-09-01" || v.Bank.CoverageEnd != "2026-09-30" {
		t.Fatal("sync restored a reversed allocation or lost legacy/coverage state")
	}
	raw, err := service.load(ctx, owner)
	if err != nil || len(raw.Payments) != 2 || !reflect.DeepEqual(raw.HistoricalPaymentIDs, request.HistoricalPaymentIDs) {
		t.Fatalf("raw dedup/history boundary lost: %v", err)
	}
}

func TestBusinessLegacyOverridesRejectUnknownAndAllocatedPayments(t *testing.T) {
	service, owner := testService(t, nil)
	ctx := context.Background()
	if _, err := service.Import(ctx, owner, importFixture()); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Allocate(ctx, owner, AllocateRequest{InvoiceID: "current", PaymentID: "payment-1", AmountMinor: 1000}); err != nil {
		t.Fatal(err)
	}
	_, err := service.Import(ctx, owner, ImportRequest{HistoricalPaymentIDs: []string{"payment-1"}})
	requireStatus(t, err, 409)
	_, err = service.Import(ctx, owner, ImportRequest{HistoricalPaymentIDs: []string{"missing-payment"}})
	requireStatus(t, err, 400)
	other, otherOwner := testService(t, nil)
	_, err = other.Import(ctx, otherOwner, ImportRequest{HistoricalPaymentIDs: []string{"payment-2"}})
	requireStatus(t, err, 400)
	if _, err := service.Import(ctx, owner, ImportRequest{HistoricalPaymentIDs: []string{"payment-2", "payment-2"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Import(ctx, owner, ImportRequest{}); err != nil {
		t.Fatal(err)
	}
	raw, err := service.load(ctx, owner)
	if err != nil || !reflect.DeepEqual(raw.HistoricalPaymentIDs, []string{"payment-2"}) || len(raw.Allocations) != 1 {
		t.Fatal("failed/empty import changed the sticky set or existing allocation")
	}
}
