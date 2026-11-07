package business

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"
	"time"
)

func reconciliationFixture() state {
	s := rentalState()
	s.Invoices = []Invoice{rentalInvoice("september", "2026-09", "1", 100000), rentalInvoice("october", "2026-10", "2", 200000)}
	s.Invoices[0].Documents = []Document{{ID: "invoice-file", Kind: "invoice"}, {ID: "act-file", Kind: "act"}}
	old := s.Invoices[0]
	old.ID, old.IsCurrent, old.AmountMinor = "archived-overlap", false, 900000
	s.Invoices = append(s.Invoices, old)
	s.Payments = []Payment{rentalPayment("first", "2026-09-10", 50000, "по сч №1 от 01.09.2026"), rentalPayment("second", "2026-10-20", 150000, "по сч №1 от 01.09.2026")}
	s.Allocations = []Allocation{{ID: "a", InvoiceID: "september", PaymentID: "first", AmountMinor: 50000}, {ID: "b", InvoiceID: "september", PaymentID: "second", AmountMinor: 50000}, {ID: "c", InvoiceID: "october", PaymentID: "second", AmountMinor: 100000}}
	addCoverage(&s, "2026-09-01", "2026-10-31")
	return s
}

func TestBusinessReconciliationUsesPostedDatesAndOneCharge(t *testing.T) {
	s := reconciliationFixture()
	for _, test := range []struct {
		from, to                        string
		opening, debit, credit, closing int64
		rows                            int
	}{
		{"2026-09-01", "2026-10-31", 0, 300000, 200000, 100000, 4},
		{"2026-10-01", "2026-10-31", 50000, 200000, 150000, 100000, 2},
		{"2026-09-01", "2026-09-30", 0, 100000, 50000, 50000, 2},
		{"2026-09-10", "2026-09-10", 100000, 0, 50000, 50000, 1},
	} {
		r, err := buildReconciliation(s, "payer-one", test.from, test.to, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if r.opening != test.opening || r.debit != test.debit || r.credit != test.credit || r.closing != test.closing || len(r.rows) != test.rows {
			t.Fatalf("%s..%s = %#v", test.from, test.to, r)
		}
		if len(r.notes) != 0 {
			t.Fatalf("covered, allocated fixture unexpectedly needs review: %v", r.notes)
		}
		for i := 1; i < len(r.rows); i++ {
			if r.rows[i-1].date > r.rows[i].date {
				t.Fatal("operations are not chronological")
			}
		}
	}
}

func TestBusinessReconciliationSeparatesHistoricalUnknownAndAdvance(t *testing.T) {
	s := reconciliationFixture()
	legacy := rentalPayment("legacy", "2026-10-03", 900000, "по сч №1 от 01.09.2026")
	unknown := rentalPayment("unknown", "2026-10-04", 10000, "без ссылки на счёт")
	advance := rentalPayment("advance", "2026-10-05", 120000, "по сч №2 от 01.10.2026")
	own := rentalPayment("own", "2026-10-06", 1000000, "по сч №1 от 01.09.2026")
	own.PayerINN = s.Seller.INN
	s.Payments = append(s.Payments, legacy, unknown, advance, own)
	s.HistoricalPaymentIDs = []string{legacy.ID}
	s.Coverage = []Coverage{{From: "2026-09-01", To: "2026-09-30"}, {From: "2026-10-15", To: "2026-10-31"}}
	r, err := buildReconciliation(s, "payer-one", "2026-09-01", "2026-10-31", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if r.debit != 300000 || r.credit != 200000 || r.closing != 100000 || len(r.excluded) != 4 || len(r.notes) != 2 {
		t.Fatalf("excluded receipts changed settlement or lost warnings: %#v", r)
	}
	var excludedTotal int64
	for _, entry := range r.excluded {
		excludedTotal += entry.amount
	}
	if excludedTotal != legacy.AmountMinor+unknown.AmountMinor+advance.AmountMinor {
		t.Fatal("excluded parts must exactly partition receipts; own transfers must be absent")
	}
	other := fixtureParty()
	other.ID, other.INN, other.Name = "other", "7701234500", "Другой контрагент"
	s.Counterparties = append(s.Counterparties, other)
	separate, err := buildReconciliation(s, "other", "2026-09-01", "2026-10-31", time.Now())
	if err != nil || separate.debit != 0 || separate.credit != 0 || len(separate.rows) != 0 || len(separate.excluded) != 0 {
		t.Fatal("another counterparty leaked into the statement", err)
	}
}

func TestBusinessReconciliationNegativeOpeningAndValidation(t *testing.T) {
	s := reconciliationFixture()
	s.Invoices = s.Invoices[1:2]
	s.Payments = []Payment{rentalPayment("early", "2026-09-25", 10001, "по сч №2 от 01.10.2026")}
	s.Allocations = []Allocation{{ID: "early-allocation", PaymentID: "early", InvoiceID: "october", AmountMinor: 10001}}
	r, err := buildReconciliation(s, "payer-one", "2026-10-01", "2026-10-31", time.Now())
	if err != nil || r.opening != -10001 || r.closing != 189999 || reconciliationMoney(r.opening) != "-100,01" {
		t.Fatalf("prepayment should remain an exact credit opening: %#v %v", r, err)
	}
	for _, input := range [][3]string{{"", "2026-10-01", "2026-10-31"}, {"payer-one", "2026-02-30", "2026-10-31"}, {"payer-one", "2026-10-31", "2026-10-01"}} {
		_, err := buildReconciliation(s, input[0], input[1], input[2], time.Now())
		requireStatus(t, err, 400)
	}
	_, err = buildReconciliation(s, "missing", "2026-10-01", "2026-10-31", time.Now())
	requireStatus(t, err, 404)
}

func TestBusinessReconciliationCounterpartyAllocationsAndPriorUnresolvedCash(t *testing.T) {
	s := reconciliationFixture()
	other := fixtureParty()
	other.ID, other.INN = "other", "7701234500"
	s.Counterparties = append(s.Counterparties, other)
	invoice := rentalInvoice("other-invoice", "2026-10", "3", 10000)
	invoice.CounterpartyID = other.ID
	s.Invoices = append(s.Invoices, invoice)
	s.Payments = append(s.Payments, rentalPayment("shared", "2026-10-21", 10000, "без ссылки на счёт"), rentalPayment("prior", "2026-09-20", 1000, "без ссылки на счёт"))
	s.Allocations = append(s.Allocations, Allocation{ID: "shared-a", InvoiceID: "october", PaymentID: "shared", AmountMinor: 5000}, Allocation{ID: "shared-b", InvoiceID: "other-invoice", PaymentID: "shared", AmountMinor: 5000})
	r, err := buildReconciliation(s, "payer-one", "2026-10-01", "2026-10-31", time.Now())
	if err != nil || r.opening != 50000 || r.debit != 200000 || r.credit != 155000 || r.closing != 95000 || len(r.excluded) != 2 || len(r.notes) != 1 {
		t.Fatalf("wrong counterparty / prior receipt scope: %#v, %v", r, err)
	}
	if !strings.HasPrefix(r.excluded[0].description, "До начала периода.") || r.excluded[0].amount != 1000 || r.excluded[1].amount != 5000 {
		t.Fatalf("lost an unallocated prior receipt or attributed another party's allocation: %v", r.excluded)
	}
}

func TestBusinessReconciliationPDF(t *testing.T) {
	s := reconciliationFixture()
	r, err := buildReconciliation(s, "payer-one", "2026-09-01", "2026-10-31", time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	for _, variant := range []string{"sample", "multipage"} {
		if variant == "multipage" {
			s.Invoices, s.Payments, s.Allocations = nil, nil, nil
			for month := 0; month < 14; month++ {
				date := time.Date(2025, 9, 1, 0, 0, 0, 0, time.UTC).AddDate(0, month, 0)
				id, amount := date.Format("2006-01"), int64(100000+month*10000)
				invoice := rentalInvoice(id, id, id, amount)
				s.Invoices = append(s.Invoices, invoice)
				s.Payments = append(s.Payments, rentalPayment(id, date.AddDate(0, 0, 9).Format(time.DateOnly), amount/2, "по сч №"+id+" от "+date.Format("02.01.2006")))
				s.Allocations = append(s.Allocations, Allocation{ID: id, InvoiceID: id, PaymentID: id, AmountMinor: amount / 2})
			}
			s.Payments = append(s.Payments, rentalPayment("unknown", "2026-10-31", 10000, "без ссылки на счёт"))
			r, err = buildReconciliation(s, "payer-one", "2025-09-01", "2026-10-31", r.generatedAt)
			if err != nil {
				t.Fatal(err)
			}
		}
		data, err := renderReconciliation(r)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range []string{"%PDF-", "/ToUnicode", "/FontFile2", "/MediaBox [0 0 595.28 841.89]"} {
			if !bytes.Contains(data, []byte(marker)) {
				t.Fatalf("PDF missing %q", marker)
			}
		}
		if dir := os.Getenv("BUSINESS_RECONCILIATION_PDF_DIR"); dir != "" {
			if err := os.WriteFile(dir+"/reconciliation-"+variant+".pdf", data, 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestBusinessReconciliationPDFOwnerIsolation(t *testing.T) {
	service, owner := testService(t, nil)
	ctx := context.Background()
	if _, err := service.Import(ctx, owner, importFixture()); err != nil {
		t.Fatal(err)
	}
	file, err := service.ReconciliationPDF(ctx, owner, "payer-one", "2026-09-01", "2026-10-31")
	if err != nil || file.MIMEType != "application/pdf" || len(file.Data) == 0 || file.Size != len(file.Data) || !strings.HasSuffix(file.Filename, ".pdf") {
		t.Fatalf("statement file = %#v, %v", file.Document, err)
	}
	other, otherOwner := testService(t, nil)
	_, err = other.ReconciliationPDF(ctx, otherOwner, "payer-one", "2026-09-01", "2026-10-31")
	requireStatus(t, err, 404)
}
