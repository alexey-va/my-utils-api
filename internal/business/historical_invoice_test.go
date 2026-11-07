package business

import "testing"

func TestHistoricalInvoiceLinksUseReferenceAndAggregateAmount(t *testing.T) {
	ledger := state{
		Counterparties: []Counterparty{{ID: "payer", INN: "1234567890"}},
		Invoices: []Invoice{
			{ID: "monthly", Number: "2", IssueDate: "2026-09-01", CounterpartyID: "payer", AmountMinor: 57028000},
			{ID: "cumulative", Number: "2", IssueDate: "2026-09-01", CounterpartyID: "payer", AmountMinor: 399196000},
			{ID: "autumn", Number: "3", IssueDate: "2026-09-16", CounterpartyID: "payer", AmountMinor: 228112000},
		},
	}
	credit := func(description string, amount int64) PaymentView {
		return PaymentView{Payment: Payment{PayerINN: "1234567890", CounterpartyID: "payer", Description: description, AmountMinor: amount}, Classification: "historical"}
	}
	views := []PaymentView{
		credit("Оплата по сч №2 от 01.09.2026г.", 57028000),
		credit("Частичная оплата по сч 3 от 16.09.2026г", 191000000),
		credit("Частичная оплата по сч 3 от 16.09.2026г", 37112000),
		credit("Оплата по договору аренды", 57028000),
	}
	linkHistoricalInvoices(ledger, views)
	for i, want := range []string{"monthly", "autumn", "autumn", ""} {
		if views[i].HistoricalInvoiceID != want || views[i].AllocatedMinor != 0 || views[i].UnallocatedMinor != 0 {
			t.Fatalf("credit %d: %+v, wanted archive link %q without allocation", i, views[i], want)
		}
	}
	duplicate := ledger.Invoices[0]
	duplicate.ID = "same-reference-and-total"
	duplicate.IsCurrent = true
	ledger.Invoices = append(ledger.Invoices, duplicate)
	views[0].HistoricalInvoiceID = ""
	linkHistoricalInvoices(ledger, views)
	if views[0].HistoricalInvoiceID != "" {
		t.Fatal("conflicting current invoice should block archival association")
	}
}
