package business

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testService(t *testing.T, bank *BankClient) (*Service, string) {
	t.Helper()
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	owner := uuid.NewString()
	if _, err := pool.Exec(ctx, `INSERT INTO users(id,email,password_hash,username,role) VALUES($1::uuid,$2,'unused',$3,'ADMIN')`, owner, owner+"@business-test.invalid", "business-"+owner); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), `DELETE FROM users WHERE id=$1::uuid`, owner); err != nil {
			t.Error(err)
		}
	})
	service, err := NewService(pool, strings.Repeat("synthetic-secret-", 3), bank)
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC) }
	return service, owner
}
func importFixture() ImportRequest {
	current := fixtureInvoice("current", "2026-09", true)
	current.Documents = []Document{{ID: "original-file", Kind: "invoice"}}
	old := fixtureInvoice("archived", "2026-09", false)
	old.Documents = []Document{{ID: "same-file-other-id", Kind: "invoice"}}
	data := base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\nsynthetic original document\n%%EOF"))
	return ImportRequest{Seller: fixtureSeller(), Counterparties: []Counterparty{fixtureParty()}, Invoices: []Invoice{current, old}, Payments: []Payment{{ID: "payment-1", Date: "2026-10-02", AmountMinor: 6000, Currency: "RUB", CounterpartyID: "payer-one", PayerINN: "7701234567"}, {ID: "payment-2", Date: "2026-10-03", AmountMinor: 5000, Currency: "RUB", CounterpartyID: "payer-one", PayerINN: "7701234567"}}, Documents: []ImportDocument{{ID: "original-file", Filename: "исходный счёт.pdf", MIMEType: "application/pdf", DataBase64: data}, {ID: "same-file-other-id", Filename: "копия.pdf", MIMEType: "application/pdf", DataBase64: data}}}
}
func requireStatus(t *testing.T, err error, status int) {
	t.Helper()
	var domain *workout.Error
	if !errors.As(err, &domain) || domain.Status != status {
		t.Fatalf("error = %v, want status %d", err, status)
	}
}
func TestBusinessImportIsolationIdempotencyAndAllocationTransactions(t *testing.T) {
	s, owner := testService(t, nil)
	ctx := context.Background()
	request := importFixture()
	result, err := s.Import(ctx, owner, request)
	if err != nil {
		t.Fatal(err)
	}
	if result.InvoiceCount != 2 || result.DocumentCount != 1 || result.PaymentCount != 2 {
		t.Fatalf("import = %#v", result)
	}
	allocation, err := s.Allocate(ctx, owner, AllocateRequest{InvoiceID: "current", PaymentID: "payment-1", AmountMinor: 6000})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(ctx, owner, importFixture()); err != nil {
		t.Fatalf("idempotent re-import: %v", err)
	}
	v, err := s.Workspace(ctx, owner, "2026-09", "")
	if err != nil {
		t.Fatal(err)
	}
	if v.Summary.InvoicedMinor != 10000 || v.Summary.PaidMinor != 6000 || v.Summary.ReceivedMinor != 0 || v.Summary.InvoiceCount != 1 || v.Summary.PartialInvoiceCount != 1 {
		t.Fatalf("summary = %#v", v.Summary)
	}
	_, err = s.Allocate(ctx, owner, AllocateRequest{InvoiceID: "current", PaymentID: "payment-2", AmountMinor: 4001})
	requireStatus(t, err, 409)
	_, err = s.Allocate(ctx, owner, AllocateRequest{InvoiceID: "current", PaymentID: "payment-1", AmountMinor: 1})
	requireStatus(t, err, 409)
	_, err = s.Allocate(ctx, owner, AllocateRequest{InvoiceID: "archived", PaymentID: "payment-2", AmountMinor: 1})
	requireStatus(t, err, 409)
	second, err := s.Allocate(ctx, owner, AllocateRequest{InvoiceID: "current", PaymentID: "payment-2", AmountMinor: 4000})
	if err != nil {
		t.Fatal(err)
	}
	v, _ = s.Workspace(ctx, owner, "all", "")
	if v.Summary.PaidMinor != 10000 || v.Summary.UnallocatedMinor != 1000 {
		t.Fatalf("multiple allocations = %#v", v.Summary)
	}
	if err := s.ReverseAllocation(ctx, owner, allocation.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.ReverseAllocation(ctx, owner, second.ID); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Workspace(ctx, owner, "all", "")
	if v.Summary.PaidMinor != 0 || v.Summary.OutstandingMinor != 10000 || v.Invoices[0].Status != "unreviewed" {
		t.Fatalf("reversal = %#v", v.Summary)
	}
	other, otherOwner := testService(t, nil)
	otherView, err := other.Workspace(ctx, otherOwner, "all", "")
	if err != nil || len(otherView.Invoices) != 0 {
		t.Fatalf("other workspace = %#v, %v", otherView, err)
	}
	_, err = other.File(ctx, otherOwner, "original-file")
	requireStatus(t, err, 404)
	_, err = other.Allocate(ctx, otherOwner, AllocateRequest{InvoiceID: "current", PaymentID: "payment-1", AmountMinor: 1})
	requireStatus(t, err, 404)
	file, err := s.File(ctx, owner, "original-file")
	if err != nil || file.Size == 0 || file.SHA256 == "" {
		t.Fatalf("private file = %#v, %v", file.Document, err)
	}
	bad := importFixture()
	bad.Invoices[0].ID = "rollback-invoice"
	bad.Invoices[0].Documents[0].ID = "missing-file"
	bad.Documents = []ImportDocument{{ID: "rollback-file", Filename: "new.pdf", MIMEType: "application/pdf", DataBase64: base64.StdEncoding.EncodeToString([]byte("%PDF-1.4\ndifferent\n%%EOF"))}}
	_, err = s.Import(ctx, owner, bad)
	requireStatus(t, err, 400)
	_, err = s.File(ctx, owner, "rollback-file")
	requireStatus(t, err, 404)
	duplicate := importFixture()
	duplicate.Invoices = []Invoice{fixtureInvoice("second-current", "2026-09", true)}
	duplicate.Documents = nil
	_, err = s.Import(ctx, owner, duplicate)
	requireStatus(t, err, 409)
}
func TestBusinessLocalCreationIsAtomicAndAvailableForPerson(t *testing.T) {
	s, owner := testService(t, nil)
	ctx := context.Background()
	request := importFixture()
	request.Invoices = nil
	request.Payments = nil
	request.Documents = nil
	request.Counterparties[0].EntityType = "person"
	request.Counterparties[0].INN = ""
	if _, err := s.Import(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	create := CreateInvoiceRequest{CounterpartyID: "payer-one", TemplateID: "rent", Period: "2026-11", IssueDate: "2026-11-03", Items: fixtureItems()}
	var success, duplicates atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.CreateInvoice(ctx, owner, create)
			if err == nil {
				success.Add(1)
			} else {
				var domain *workout.Error
				if errors.As(err, &domain) && domain.Status == 409 {
					duplicates.Add(1)
				} else {
					t.Errorf("create: %v", err)
				}
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 || duplicates.Load() != 1 {
		t.Fatalf("concurrent creates = success %d duplicate %d", success.Load(), duplicates.Load())
	}
	v, err := s.Workspace(ctx, owner, "all", "")
	if err != nil || v.Summary.InvoiceCount != 1 || v.Invoices[0].Number != "1" || len(v.Invoices[0].Documents) != 1 || v.Bank.Connected {
		t.Fatalf("local result = %#v, %v", v, err)
	}
	create.Period = "2026-12"
	create.Provider = "tochka"
	_, err = s.CreateInvoice(ctx, owner, create)
	requireStatus(t, err, 400)
}
func TestBusinessBankWriteUncertaintyBlocksRetryBut403AllowsLocal(t *testing.T) {
	for _, mode := range []string{"unknown", "rejected"} {
		t.Run(mode, func(t *testing.T) {
			var writes atomic.Int32
			bank := testBankClient(t, func(r *http.Request) (*http.Response, error) {
				if r.Method == http.MethodGet {
					return bankResponse(200, accountsJSON), nil
				}
				writes.Add(1)
				if mode == "rejected" {
					return bankResponse(403, `{"message":"synthetic private upstream body"}`), nil
				}
				return nil, errors.New("synthetic response lost")
			})
			s, owner := testService(t, bank)
			ctx := context.Background()
			request := importFixture()
			request.Invoices = nil
			request.Payments = nil
			request.Documents = nil
			if _, err := s.Import(ctx, owner, request); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Connect(ctx, owner, "synthetic-token"); err != nil {
				t.Fatal(err)
			}
			var data, nonce, snapshot []byte
			if err := s.pool.QueryRow(ctx, `SELECT bank_token_ciphertext,bank_token_nonce,snapshot FROM business_workspaces WHERE owner_id=$1::uuid`, owner).Scan(&data, &nonce, &snapshot); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), "synthetic-token") || strings.Contains(string(snapshot), "synthetic-token") {
				t.Fatal("token leaked at rest")
			}
			create := CreateInvoiceRequest{CounterpartyID: "payer-one", TemplateID: "rent", Period: "2026-11", IssueDate: "2026-11-03", Items: fixtureItems(), Provider: "tochka"}
			created, err := s.CreateInvoice(ctx, owner, create)
			if mode == "unknown" {
				if err != nil || created.ProviderState != "unknown" {
					t.Fatalf("uncertain create = %#v %v", created, err)
				}
				_, err = s.CreateInvoice(ctx, owner, create)
				requireStatus(t, err, 409)
				create.Provider = "local"
				_, err = s.CreateInvoice(ctx, owner, create)
				requireStatus(t, err, 409)
			} else {
				requireStatus(t, err, 400)
				create.Provider = "local"
				local, err := s.CreateInvoice(ctx, owner, create)
				if err != nil || !local.IsCurrent || local.Provider != "local" {
					t.Fatalf("local after 403 = %#v %v", local, err)
				}
			}
			if writes.Load() != 1 {
				t.Fatalf("bank POST retried %d times", writes.Load())
			}
			if err := s.Disconnect(ctx, owner); err != nil {
				t.Fatal(err)
			}
			view, _ := s.Workspace(ctx, owner, "all", "")
			if view.Bank.Connected || len(view.Invoices) == 0 {
				t.Fatal("disconnect erased history or retained connection")
			}
			if _, err := s.loadToken(ctx, owner); err == nil {
				t.Fatal("disconnect retained token")
			}
		})
	}
}
func TestBusinessBankSyncDeduplicatesAndDoesNotUndoReversal(t *testing.T) {
	bank := testBankClient(t, func(r *http.Request) (*http.Response, error) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/accounts"):
			return bankResponse(200, accountsJSON), nil
		case r.Method == http.MethodPost:
			return bankResponse(200, `{"Data":{"Statement":{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Created"}}}`), nil
		default:
			return bankResponse(200, `{"Data":{"Statement":[{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Ready","startDateTime":"2026-09-01","endDateTime":"2026-09-30","Transaction":[{"transactionId":"credit","creditDebitIndicator":"Credit","status":"Booked","documentProcessDate":"2026-09-02","description":"Оплата по счёту № 17 от 01.09.2026","Amount":{"amount":100,"currency":"RUB"},"DebtorParty":{"inn":"7701234567","name":"Тестовый плательщик"}}]}]},"Meta":{"totalPages":1}}`), nil
		}
	})
	s, owner := testService(t, bank)
	ctx := context.Background()
	request := importFixture()
	request.Payments = nil
	if _, err := s.Import(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connect(ctx, owner, "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Workspace(ctx, owner, "all", "")
	if len(v.Payments) != 1 || len(v.Payments[0].Allocations) != 1 || !v.Summary.ReconciliationComplete {
		t.Fatalf("first sync = %#v", v.Summary)
	}
	if err := s.ReverseAllocation(ctx, owner, v.Payments[0].Allocations[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Workspace(ctx, owner, "all", "")
	if len(v.Payments) != 1 || v.Summary.PaidMinor != 0 || v.Summary.UnallocatedMinor != 10000 {
		t.Fatalf("repeat sync undid reversal = %#v", v.Summary)
	}
	encoded, _ := json.Marshal(v)
	if strings.Contains(string(encoded), "synthetic-token") {
		t.Fatal("token leaked to client")
	}
}

func TestBusinessPartialBankFailureDoesNotClaimCoverage(t *testing.T) {
	bank := testBankClient(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/accounts") {
			return bankResponse(200, `{"Data":{"Account":[{"accountId":"40817810802000000008/044525104","customerCode":"300000092","status":"Enabled","currency":"RUB","accountType":"Business"},{"accountId":"40817810802000000009/044525104","customerCode":"300000092","status":"Enabled","currency":"RUB","accountType":"Business"}]},"Meta":{"totalPages":1}}`), nil
		}
		if r.Method == http.MethodPost {
			var body struct {
				Data struct {
					Statement struct {
						AccountID string `json:"accountId"`
					} `json:"Statement"`
				} `json:"Data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			return bankResponse(200, `{"Data":{"Statement":{"accountId":"`+body.Data.Statement.AccountID+`","statementId":"statement","status":"Created"}}}`), nil
		}
		if strings.Contains(r.URL.Path, secondAccount) {
			return bankResponse(500, `private bank response must not leak`), nil
		}
		return bankResponse(200, `{"Data":{"Statement":[{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Ready","startDateTime":"2026-09-01","endDateTime":"2026-09-30","Transaction":[{"transactionId":"partial-credit","creditDebitIndicator":"Credit","status":"Booked","documentProcessDate":"2026-09-02","Amount":{"amount":100,"currency":"RUB"},"DebtorParty":{"inn":"7701234567"}}]}]},"Meta":{"totalPages":1}}`), nil
	})
	s, owner := testService(t, bank)
	ctx := context.Background()
	request := importFixture()
	request.Payments = nil
	request.Bank = &Bank{CoverageStart: "2026-08-01", CoverageEnd: "2026-08-31", AccountCount: 2}
	request.Invoices[1].PeriodEnd = "2026-12-31"
	if _, err := s.Connect(ctx, owner, "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Import(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	_, err := s.Sync(ctx, owner, "2026-09-01", "2026-09-30")
	requireStatus(t, err, 502)
	v, err := s.Workspace(ctx, owner, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Payments) != 0 || v.Bank.CoverageStart != "2026-08-01" || v.Bank.CoverageEnd != "2026-08-31" || v.Bank.LastSyncedAt != "" || v.Bank.Error == "" || v.Summary.ReconciliationComplete {
		t.Fatalf("partial bank read changed ledger = %#v %#v", v.Summary, v.Bank)
	}
	for _, invoice := range v.Invoices {
		if invoice.DueDate != "" {
			t.Fatal("an absent source deadline was invented")
		}
		if invoice.ID == "archived" && invoice.PeriodEnd != "2026-12-31" {
			t.Fatal("cumulative historical period was lost")
		}
	}
}

func TestBusinessBankReconnectInvalidatesCoverageButPreservesHistory(t *testing.T) {
	for _, replacement := range []string{"additional account", "different account same count"} {
		t.Run(replacement, func(t *testing.T) {
			const firstAccount = "40817810802000000008/044525104"
			accountIDs := []string{firstAccount}
			bank := testBankClient(t, func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/accounts") {
					if r.Header.Get("Authorization") == "Bearer rejected-token" {
						return bankResponse(403, `{"message":"private bank rejection"}`), nil
					}
					var response struct {
						Data struct {
							Accounts []BankAccount `json:"Account"`
						} `json:"Data"`
						Meta bankMeta `json:"Meta"`
					}
					for _, id := range accountIDs {
						response.Data.Accounts = append(response.Data.Accounts, BankAccount{ID: id, CustomerCode: "300000092", Status: "Enabled", Currency: "RUB", Type: "Business"})
					}
					response.Meta.TotalPages = 1
					data, err := json.Marshal(response)
					if err != nil {
						t.Fatal(err)
					}
					return bankResponse(200, string(data)), nil
				}
				if r.Method == http.MethodPost {
					var body struct {
						Data struct {
							Statement struct {
								AccountID string `json:"accountId"`
							} `json:"Statement"`
						} `json:"Data"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Fatal(err)
					}
					return bankResponse(200, `{"Data":{"Statement":{"accountId":"`+body.Data.Statement.AccountID+`","statementId":"statement","status":"Created"}}}`), nil
				}
				id := secondAccount
				if strings.Contains(r.URL.Path, firstAccount) {
					id = firstAccount
				}
				statement := bankStatement{AccountID: id, ID: "statement", Status: "Ready", From: "2026-09-01", To: "2026-09-30", Transactions: []bankTransaction{}}
				if id == firstAccount {
					credit := bankTransaction{ID: "credit", Direction: "Credit", Status: "Booked", Date: "2026-09-02", Description: "Оплата по счёту № 17 от 01.09.2026"}
					credit.Amount.Amount = json.RawMessage(`100`)
					credit.Amount.Currency = "RUB"
					credit.Debtor.INN = "7701234567"
					credit.Debtor.Name = "Тестовый плательщик"
					statement.Transactions = append(statement.Transactions, credit)
				}
				var response struct {
					Data struct {
						Statements []bankStatement `json:"Statement"`
					} `json:"Data"`
					Meta bankMeta `json:"Meta"`
				}
				response.Data.Statements = []bankStatement{statement}
				response.Meta.TotalPages = 1
				data, err := json.Marshal(response)
				if err != nil {
					t.Fatal(err)
				}
				return bankResponse(200, string(data)), nil
			})
			s, owner := testService(t, bank)
			ctx := context.Background()
			request := importFixture()
			request.Payments = nil
			if _, err := s.Import(ctx, owner, request); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Connect(ctx, owner, "initial-token"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
				t.Fatal(err)
			}
			before, err := s.Workspace(ctx, owner, "all", "")
			if err != nil {
				t.Fatal(err)
			}
			if !before.Summary.ReconciliationComplete || len(before.Payments) != 1 || len(before.Payments[0].Allocations) != 1 || before.Bank.LastSyncedAt == "" {
				t.Fatalf("initial scope = %#v %#v", before.Summary, before.Bank)
			}
			if _, err := s.Connect(ctx, owner, "rejected-token"); err == nil {
				t.Fatal("failed connection unexpectedly succeeded")
			}
			unchanged, err := s.Workspace(ctx, owner, "all", "")
			if err != nil {
				t.Fatal(err)
			}
			if unchanged.Bank != before.Bank || unchanged.Summary != before.Summary {
				t.Fatal("failed connection invalidated the established scope")
			}
			if token, err := s.loadToken(ctx, owner); err != nil || token != "initial-token" {
				t.Fatalf("failed connection changed credential: %v", err)
			}
			accountIDs = []string{firstAccount, secondAccount}
			if replacement == "different account same count" {
				accountIDs = []string{secondAccount}
			}
			connected, err := s.Connect(ctx, owner, "replacement-token")
			if err != nil {
				t.Fatal(err)
			}
			if connected.CoverageStart != "" || connected.CoverageEnd != "" || connected.LastSyncedAt != "" || connected.AccountCount != len(accountIDs) {
				t.Fatalf("new connection retained old proof: %#v", connected)
			}
			after, err := s.Workspace(ctx, owner, "all", "")
			if err != nil {
				t.Fatal(err)
			}
			if after.Summary.ReconciliationComplete || after.Summary.PaidMinor != before.Summary.PaidMinor || len(after.Payments) != 1 || len(after.Payments[0].Allocations) != 1 {
				t.Fatalf("replacement erased history or retained proof = %#v", after.Summary)
			}
			if _, err := s.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
				t.Fatal(err)
			}
			refreshed, err := s.Workspace(ctx, owner, "all", "")
			if err != nil {
				t.Fatal(err)
			}
			if !refreshed.Summary.ReconciliationComplete || len(refreshed.Payments) != 1 || len(refreshed.Payments[0].Allocations) != 1 || refreshed.Bank.AccountCount != len(accountIDs) {
				t.Fatalf("new full scope was not established = %#v %#v", refreshed.Summary, refreshed.Bank)
			}
		})
	}
}

func TestBusinessExistingBankCreditResolvesPayerWithoutReplayingAllocation(t *testing.T) {
	for _, enrichVia := range []string{"sync", "import"} {
		t.Run(enrichVia, func(t *testing.T) {
			bank := testBankClient(t, func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/accounts") {
					return bankResponse(200, accountsJSON), nil
				}
				if r.Method == http.MethodPost {
					return bankResponse(200, `{"Data":{"Statement":{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Created"}}}`), nil
				}
				return bankResponse(200, `{"Data":{"Statement":[{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Ready","startDateTime":"2026-09-01","endDateTime":"2026-09-30","Transaction":[{"transactionId":"credit","creditDebitIndicator":"Credit","status":"Booked","documentProcessDate":"2026-09-02","description":"Оплата по счёту № 17 от 01.09.2026","Amount":{"amount":100,"currency":"RUB"},"DebtorParty":{"inn":"7701234567","name":"Тестовый плательщик"}}]}]},"Meta":{"totalPages":1}}`), nil
			})
			s, owner := testService(t, bank)
			ctx := context.Background()
			request := importFixture()
			request.Payments = nil
			request.Counterparties[0].INN = "7700000000"
			if _, err := s.Import(ctx, owner, request); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Connect(ctx, owner, "synthetic-token"); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
				t.Fatal(err)
			}
			unknown, err := s.Workspace(ctx, owner, "all", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(unknown.Payments) != 1 || unknown.Payments[0].CounterpartyID != "" {
				t.Fatal("unknown payer was guessed")
			}
			original := unknown.Payments[0].Payment
			allocation, err := s.Allocate(ctx, owner, AllocateRequest{InvoiceID: "current", PaymentID: original.ID, AmountMinor: 10000})
			if err != nil {
				t.Fatal(err)
			}
			if err := s.ReverseAllocation(ctx, owner, allocation.ID); err != nil {
				t.Fatal(err)
			}
			party := fixtureParty()
			party.ID = "newly-known"
			matchingInvoice := fixtureInvoice("known-party-invoice", "2026-09", true)
			matchingInvoice.CounterpartyID = party.ID
			if _, err := s.Import(ctx, owner, ImportRequest{Counterparties: []Counterparty{party}, Invoices: []Invoice{matchingInvoice}}); err != nil {
				t.Fatal(err)
			}
			if enrichVia == "import" {
				enriched := original
				enriched.CounterpartyID = party.ID
				if _, err := s.Import(ctx, owner, ImportRequest{Payments: []Payment{enriched}}); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := s.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
				t.Fatalf("existing credit failed after payer discovery: %v", err)
			}
			resolved, err := s.Workspace(ctx, owner, "all", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(resolved.Payments) != 1 || resolved.Payments[0].CounterpartyID != party.ID || len(resolved.Payments[0].Allocations) != 0 || resolved.Summary.PaidMinor != 0 {
				t.Fatalf("credit enrichment duplicated or reallocated payment: %#v", resolved.Payments)
			}
			// Both an old archive without the later association and an incoming
			// derived alternative must preserve the established non-empty link.
			alternative := original
			alternative.CounterpartyID = "payer-one"
			for _, incoming := range []Payment{original, alternative} {
				if _, err := s.Import(ctx, owner, ImportRequest{Payments: []Payment{incoming}}); err != nil {
					t.Fatal(err)
				}
			}
			after, err := s.Workspace(ctx, owner, "all", "")
			if err != nil {
				t.Fatal(err)
			}
			if len(after.Payments) != 1 || after.Payments[0].CounterpartyID != party.ID || len(after.Payments[0].Allocations) != 0 {
				t.Fatal("idempotent import reassigned or reallocated a stored credit")
			}
		})
	}
}

func TestBusinessSyncCoverageDescribesOnlyLatestFullRetrieval(t *testing.T) {
	from, to := "", ""
	bank := testBankClient(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/accounts") {
			return bankResponse(200, accountsJSON), nil
		}
		if r.Method == http.MethodPost {
			var body struct {
				Data struct {
					Statement struct {
						From string `json:"startDateTime"`
						To   string `json:"endDateTime"`
					} `json:"Statement"`
				} `json:"Data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			from, to = body.Data.Statement.From, body.Data.Statement.To
			return bankResponse(200, `{"Data":{"Statement":{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Created"}}}`), nil
		}
		statement := bankStatement{AccountID: "40817810802000000008/044525104", ID: "statement", Status: "Ready", From: from, To: to, Transactions: []bankTransaction{}}
		if from == "2026-09-01" {
			credit := bankTransaction{ID: "september-credit", Direction: "Credit", Status: "Booked", Date: "2026-09-02", Description: "Оплата по счёту № 17 от 01.09.2026"}
			credit.Amount.Amount = json.RawMessage(`100`)
			credit.Amount.Currency = "RUB"
			credit.Debtor.INN = "7701234567"
			credit.Debtor.Name = "Тестовый плательщик"
			statement.Transactions = append(statement.Transactions, credit)
		}
		var response struct {
			Data struct {
				Statements []bankStatement `json:"Statement"`
			} `json:"Data"`
			Meta bankMeta `json:"Meta"`
		}
		response.Data.Statements = []bankStatement{statement}
		response.Meta.TotalPages = 1
		data, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		return bankResponse(200, string(data)), nil
	})
	s, owner := testService(t, bank)
	ctx := context.Background()
	request := importFixture()
	request.Payments = nil
	template := request.Counterparties[0].Templates[0]
	template.ID = "second-rent"
	request.Counterparties[0].Templates = append(request.Counterparties[0].Templates, template)
	unpaid := fixtureInvoice("unpaid-september", "2026-09", true)
	unpaid.TemplateID = template.ID
	unpaid.Number = "18"
	request.Invoices = append(request.Invoices, unpaid)
	if _, err := s.Import(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Connect(ctx, owner, "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Sync(ctx, owner, "2026-09-01", "2026-09-30"); err != nil {
		t.Fatal(err)
	}
	before, err := s.Workspace(ctx, owner, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if !before.Summary.ReconciliationComplete || before.Bank.CoverageStart != "2026-09-01" || before.Bank.CoverageEnd != "2026-09-30" {
		t.Fatalf("first retrieval = %#v %#v", before.Summary, before.Bank)
	}
	if _, err := s.Sync(ctx, owner, "2026-10-01", "2026-10-31"); err != nil {
		t.Fatal(err)
	}
	after, err := s.Workspace(ctx, owner, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if after.Summary.ReconciliationComplete || after.Bank.CoverageStart != "2026-10-01" || after.Bank.CoverageEnd != "2026-10-31" || len(after.Payments) != 1 || len(after.Payments[0].Allocations) != 1 || after.Summary.PaidMinor != 10000 {
		t.Fatalf("latest retrieval retained old proof or lost history: %#v %#v", after.Summary, after.Bank)
	}
	for _, invoice := range after.Invoices {
		if invoice.ID == unpaid.ID && invoice.Status != "unreviewed" {
			t.Fatalf("old uncovered invoice status = %s", invoice.Status)
		}
	}
}
