package business

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
)

func fixtureParty() Counterparty {
	return Counterparty{ID: "payer-one", Name: "ООО Тестовый арендатор", EntityType: "company", INN: "7701234567", Address: "Москва, Тестовая улица, 1", Contacts: []Contact{}, Templates: []InvoiceTemplate{{ID: "rent", Label: "Аренда", Basis: "Договор аренды № 7", Items: fixtureItems()}}}
}
func fixtureSeller() Seller {
	return Seller{Name: "ИП Тестовый Поставщик", INN: "770123456789", Address: "Москва, Примерная улица, 2", BankName: "ООО Банк Точка", BankBIK: "044525104", BankAccount: "40817810802000000008", BankCorrespondentAccount: "30101810745374525104"}
}
func fixtureItems() []LineItem {
	return []LineItem{{Description: "Арендная плата за нежилое помещение", Quantity: "1", Unit: "мес.", PriceMinor: 10000}}
}
func fixtureInvoice(id, period string, current bool) Invoice {
	i := Invoice{ID: id, CounterpartyID: "payer-one", TemplateID: "rent", Number: "17", IssueDate: period + "-01", Period: period, Items: fixtureItems(), IsCurrent: current, Provider: "local", ProviderState: "imported", Source: "synthetic", Basis: "Договор аренды № 7"}
	if err := normalizeInvoice(&i, time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)); err != nil {
		panic(err)
	}
	return i
}
func TestBusinessExactMoneyRoundingAndLimits(t *testing.T) {
	for _, test := range []struct {
		quantity    string
		price, want int64
	}{{"1.005", 100, 101}, {"0.125", 100, 13}, {"3.333333", 30, 100}, {"2.5", 101, 253}, {"0001.50", 100, 150}} {
		items, total, err := lineAmounts([]LineItem{{Description: "Услуга", Quantity: test.quantity, PriceMinor: test.price, AmountMinor: 999999}})
		if err != nil || total != test.want || items[0].AmountMinor != test.want {
			t.Fatalf("%s × %d = %d, %v", test.quantity, test.price, total, err)
		}
	}
	for _, quantity := range []string{"0", "-1", "1e3", "1/2", "1.1234567", "999999999999"} {
		if _, _, err := lineAmounts([]LineItem{{Description: "Услуга", Quantity: quantity, PriceMinor: maxMinor}}); err == nil {
			t.Fatalf("accepted invalid/overflowing quantity %q", quantity)
		}
	}
}
func TestBusinessArchiveAndPaymentDateHaveSeparateTotals(t *testing.T) {
	s := emptyState()
	s.Seller = fixtureSeller()
	s.Counterparties = []Counterparty{fixtureParty()}
	s.Invoices = []Invoice{fixtureInvoice("current", "2026-09", true), fixtureInvoice("old", "2026-09", false)}
	s.Invoices[1].AmountMinor = 50000
	s.Payments = []Payment{{ID: "payment", Date: "2026-10-02", AmountMinor: 10000, Currency: "RUB", CounterpartyID: "payer-one"}}
	s.Allocations = []Allocation{{ID: "allocation", InvoiceID: "current", PaymentID: "payment", AmountMinor: 10000}}
	v := workspaceView(s, "2026-09", "")
	if v.Summary.InvoicedMinor != 10000 || v.Summary.PaidMinor != 10000 || v.Summary.ReceivedMinor != 0 || v.Summary.InvoiceCount != 1 || v.Summary.OutstandingMinor != 0 || v.Summary.ReconciliationComplete {
		t.Fatalf("September summary = %#v", v.Summary)
	}
	if len(v.Invoices) != 2 || v.Invoices[1].Status != "archived" {
		t.Fatalf("history = %#v", v.Invoices)
	}
	if len(v.Payments) != 1 || v.Payments[0].Date != "2026-10-02" {
		t.Fatal("cross-month payment disappeared from allocation controls")
	}
	all := workspaceView(s, "all", "")
	if all.Summary.ReceivedMinor != 10000 || all.Payments[0].UnallocatedMinor != 0 {
		t.Fatalf("all = %#v", all.Summary)
	}
}
func TestBusinessCoverageDoesNotInventMatchesOrFillGaps(t *testing.T) {
	s := emptyState()
	invoice := fixtureInvoice("invoice", "2026-09", true)
	s.Invoices = []Invoice{invoice}
	s.Counterparties = []Counterparty{fixtureParty()}
	s.Payments = []Payment{{ID: "same-total", Date: "2026-09-02", AmountMinor: invoice.AmountMinor, Currency: "RUB"}}
	if v := viewInvoice(s, invoice); v.Status != "unreviewed" || v.PaidMinor != 0 {
		t.Fatalf("uncovered invoice = %#v", v)
	}
	addCoverage(&s, "2026-09-01", "2026-09-05")
	addCoverage(&s, "2026-09-20", "2026-09-30")
	if invoiceCovered(s, invoice) {
		t.Fatal("a gap was treated as coverage")
	}
	addCoverage(&s, "2026-09-06", "2026-09-19")
	if v := viewInvoice(s, invoice); v.Status != "unmatched" || v.PaidMinor != 0 {
		t.Fatalf("covered invoice = %#v", v)
	}
	if workspaceView(s, "all", "").Summary.ReconciliationComplete {
		t.Fatal("an unallocated equal payment made reconciliation complete")
	}
}
func TestBusinessConservativeAutomaticAllocation(t *testing.T) {
	for _, test := range []struct {
		name, description, inn string
		amount                 int64
		want                   bool
	}{
		{"explicit", "Оплата по счёту № 17 от 01.09.2026", "7701234567", 10000, true},
		{"no reference", "Аренда за сентябрь 2026", "7701234567", 10000, false},
		{"third party", "Оплата по счёту № 17 от 01.09.2026", "7711111111", 10000, false},
		{"no year", "Оплата по счёту № 17", "7701234567", 10000, false},
		{"overpayment", "Оплата по счёту № 17 от 01.09.2026", "7701234567", 10001, false},
		{"two references", "Счёт № 17 и счёт № 18 от 01.09.2026", "7701234567", 10000, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := emptyState()
			s.Invoices = []Invoice{fixtureInvoice("invoice", "2026-09", true)}
			s.Counterparties = []Counterparty{fixtureParty()}
			p := Payment{ID: "bank-payment", PayerINN: test.inn, AmountMinor: test.amount, Description: test.description, Source: "tochka"}
			s.Payments = []Payment{p}
			automaticAllocations(&s, []Payment{p})
			if (len(s.Allocations) == 1) != test.want {
				t.Fatalf("allocations = %#v", s.Allocations)
			}
		})
	}
}
func TestBusinessTokenEncryptionIsOwnerBound(t *testing.T) {
	cipher, err := newTokenCipher(strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	token := "private-synthetic-token"
	data, nonce, err := cipher.encrypt("owner-one", token)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte(token)) {
		t.Fatal("plaintext token persisted")
	}
	if restored, err := cipher.decrypt("owner-one", data, nonce); err != nil || restored != token {
		t.Fatalf("restore = %q, %v", restored, err)
	}
	if _, err := cipher.decrypt("owner-two", data, nonce); err == nil {
		t.Fatal("another owner decrypted token")
	}
	other, _ := newTokenCipher(strings.Repeat("x", 32))
	if _, err := other.decrypt("owner-one", data, nonce); err == nil {
		t.Fatal("another application secret decrypted token")
	}
	encoded, _ := json.Marshal(workspaceView(emptyState(), "all", ""))
	if bytes.Contains(encoded, []byte("token")) || bytes.Contains(encoded, []byte("ciphertext")) {
		t.Fatal("credential fields leaked into workspace")
	}
}

func TestBusinessCumulativeHistoryPreventsReferenceGuessing(t *testing.T) {
	s := emptyState()
	current := fixtureInvoice("monthly", "2026-09", true)
	archived := fixtureInvoice("cumulative", "2026-09", false)
	archived.PeriodEnd = "2026-12-31"
	archived.AmountMinor = 40000
	s.Invoices = []Invoice{current, archived}
	s.Counterparties = []Counterparty{fixtureParty()}
	payment := Payment{ID: "remainder", Date: "2026-10-02", PayerINN: "7701234567", AmountMinor: 5000, Description: "Оплата по счёту № 17 от 01.09.2026", Source: "tochka"}
	s.Payments = []Payment{payment}
	automaticAllocations(&s, []Payment{payment})
	if len(s.Allocations) != 0 {
		t.Fatal("a cumulative bill remainder was allocated to a monthly invoice")
	}
}
func TestBusinessSplitOverpaymentUsesPostingOrder(t *testing.T) {
	s := emptyState()
	invoice := fixtureInvoice("invoice", "2026-09", true)
	invoice.AmountMinor = 17887800
	s.Invoices = []Invoice{invoice}
	s.Counterparties = []Counterparty{fixtureParty()}
	later := Payment{ID: "later", Date: "2026-10-28", PayerINN: "7701234567", AmountMinor: 9887800, Description: "Оплата по счёту № 17 от 01.09.2026", Source: "tochka"}
	earlier := later
	earlier.ID = "earlier"
	earlier.Date = "2026-10-23"
	earlier.AmountMinor = 10000000
	s.Payments = []Payment{later, earlier}
	automaticAllocations(&s, []Payment{later, earlier})
	if len(s.Allocations) != 1 || s.Allocations[0].PaymentID != "earlier" || s.Allocations[0].AmountMinor != 10000000 {
		t.Fatalf("split overpayment = %#v", s.Allocations)
	}
}
func TestBusinessPDFEmbedsCyrillicFont(t *testing.T) {
	font, err := sfnt.Parse(goregular.TTF)
	if err != nil {
		t.Fatal(err)
	}
	glyph, err := font.GlyphIndex(nil, 'С')
	if err != nil || glyph == 0 {
		t.Fatalf("Cyrillic glyph = %d, %v", glyph, err)
	}
	data, err := renderInvoice(fixtureSeller(), fixtureParty(), fixtureInvoice("invoice", "2026-11", true))
	if err != nil {
		t.Fatal(err)
	}
	for _, marker := range []string{"%PDF-", "/ToUnicode", "/FontFile2", "/MediaBox [0 0 595.28 841.89]"} {
		if !bytes.Contains(data, []byte(marker)) {
			t.Fatalf("PDF missing %q", marker)
		}
	}
	if path := os.Getenv("BUSINESS_PDF_SAMPLE"); path != "" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
