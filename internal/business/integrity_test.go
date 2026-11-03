package business

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestBusinessLegacyOwnAllocationsDoNotConsumeBusinessInvoiceCapacity(t *testing.T) {
	for _, mode := range []string{"manual", "automatic"} {
		t.Run(mode, func(t *testing.T) {
			var bank *BankClient
			if mode == "automatic" {
				bank = testBankClient(t, func(r *http.Request) (*http.Response, error) {
					if strings.HasSuffix(r.URL.Path, "/accounts") {
						return bankResponse(200, accountsJSON), nil
					}
					if r.Method == http.MethodPost {
						return bankResponse(200, `{"Data":{"Statement":{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Created"}}}`), nil
					}
					return bankResponse(200, `{"Data":{"Statement":[{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Ready","startDateTime":"2026-09-01","endDateTime":"2026-09-30","Transaction":[{"transactionId":"new-external-credit","creditDebitIndicator":"Credit","status":"Booked","documentProcessDate":"2026-09-21","description":"по сч №17 от01.09.2026","Amount":{"amount":50,"currency":"RUB"},"DebtorParty":{"inn":"7701234567","name":"Тестовый плательщик"}}]}]},"Meta":{"totalPages":1}}`), nil
				})
			}
			service, owner := testService(t, bank)
			ctx := context.Background()
			if bank != nil {
				if _, err := service.Connect(ctx, owner, "synthetic-token"); err != nil {
					t.Fatal(err)
				}
			}
			request := importFixture()
			own := rentalPayment("own-transfer", "2026-09-20", 5000, "")
			own.PayerINN = request.Seller.INN
			external := rentalPayment("old-external", "2026-09-20", 5000, "")
			request.Payments = []Payment{own, external}
			if _, err := service.Import(ctx, owner, request); err != nil {
				t.Fatal(err)
			}
			// This was a valid snapshot before own transfers were excluded from rent.
			if err := service.mutate(ctx, owner, func(current *state, _ pgx.Tx) error {
				current.Allocations = []Allocation{{ID: "legacy-own", InvoiceID: "current", PaymentID: own.ID, AmountMinor: 5000}, {ID: "legacy-external", InvoiceID: "current", PaymentID: external.ID, AmountMinor: 5000}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			before, err := service.Workspace(ctx, owner, "all", "")
			if err != nil || before.Summary.PaidMinor != 5000 || before.Summary.OutstandingMinor != 5000 {
				t.Fatalf("legacy business view = %#v %v", before.Summary, err)
			}
			if mode == "manual" {
				fresh := rentalPayment("new-external", "2026-09-21", 5000, "по сч №17 от01.09.2026")
				fresh.Source = "manual"
				if _, err := service.Import(ctx, owner, ImportRequest{Payments: []Payment{fresh}}); err != nil {
					t.Fatal(err)
				}
				if _, err := service.Allocate(ctx, owner, AllocateRequest{InvoiceID: "current", PaymentID: fresh.ID, AmountMinor: 5000}); err != nil {
					t.Fatalf("visible remaining capacity could not be allocated: %v", err)
				}
			} else {
				if _, err := service.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
					t.Fatalf("new bank credit rolled back against an ignored own allocation: %v", err)
				}
			}
			after, err := service.Workspace(ctx, owner, "all", "")
			if err != nil || after.Summary.PaidMinor != 10000 || after.Summary.OutstandingMinor != 0 || after.Summary.ReceivedMinor != 10000 || len(after.Payments) != 2 {
				t.Fatalf("new business settlement = %#v %v", after.Summary, err)
			}
			raw, err := service.load(ctx, owner)
			if err != nil || len(raw.Payments) != 3 || len(raw.Allocations) != 3 {
				t.Fatal("legacy audit/payment records were deleted")
			}
			kept := false
			for _, a := range raw.Allocations {
				if a.ID == "legacy-own" && a.PaymentID == own.ID && a.AmountMinor == 5000 {
					kept = true
				}
			}
			if !kept {
				t.Fatal("own allocation audit was changed")
			}
			err = service.mutate(ctx, owner, func(current *state, _ pgx.Tx) error {
				current.Allocations = append(current.Allocations, Allocation{ID: "own-over-cap", InvoiceID: "current", PaymentID: own.ID, AmountMinor: 1})
				return nil
			})
			requireStatus(t, err, 409)
		})
	}
}

func TestBusinessUnsafePreperiodReferenceUsesOutsideDateFallback(t *testing.T) {
	for _, kind := range []string{"unknown old prefix", "ambiguous exact scopes", "missing source month"} {
		for _, early := range []bool{true, false} {
			t.Run(kind+map[bool]string{true: " early", false: " current"}[early], func(t *testing.T) {
				s := rentalState()
				reference := "по сч №1 от01.08.2026"
				date := "2026-09-20"
				earlier := "2026-08-20"
				switch kind {
				case "unknown old prefix":
					cumulativeSource(&s, "2026-08-01", "2026-12-31")
					s.Invoices[2].IssueDate = "2026-08-01"
				case "ambiguous exact scopes":
					cumulativeSource(&s, "2026-09-01", "2026-12-31")
					s.Invoices[2].IssueDate = "2026-09-01"
					reference = "по сч №1 от01.09.2026"
				case "missing source month":
					s.Invoices[0] = rentalInvoice("august", "2026-08", "8", 10000)
					cumulativeSource(&s, "2026-09-01", "2026-12-31")
					s.Invoices[2].IssueDate = "2026-08-01"
					earlier = "2026-07-20"
					date = "2026-08-20"
				}
				if early {
					date = earlier
				}
				p := rentalPayment("credit", date, 40000, reference)
				s.Payments = []Payment{p}
				automaticAllocations(&s, []Payment{p})
				v := workspaceView(s, "all", "")
				want := "unclassified"
				if early {
					want = "outside_period"
				}
				if len(s.Allocations) != 0 || v.Payments[0].Classification != want || v.Payments[0].AdvanceMinor != 0 {
					t.Fatalf("unsafe source mapping = %#v", v.Payments[0])
				}
				if early && (v.Payments[0].UnallocatedMinor != 0 || v.Counterparties[0].LastPaymentAt != "") {
					t.Fatal("unsafe early cash entered current availability/last payment")
				}
			})
		}
	}
	s := rentalState()
	p := rentalPayment("proved-prepayment", "2026-08-20", 5000, "по сч №1 от01.09.2026")
	s.Payments = []Payment{p}
	automaticAllocations(&s, []Payment{p})
	if matchPayment(s, p).classification != "rental" || len(s.Allocations) != 1 || s.Allocations[0].InvoiceID != "september" {
		t.Fatal("unique current-invoice evidence lost to the date fallback")
	}
}
