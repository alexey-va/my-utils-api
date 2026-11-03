package business

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type testTransport func(*http.Request) (*http.Response, error)

func (f testTransport) RoundTrip(request *http.Request) (*http.Response, error) { return f(request) }
func bankResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

const accountsJSON = `{"Data":{"Account":[{"accountId":"40817810802000000008/044525104","customerCode":"300000092","status":"Enabled","currency":"RUB","accountType":"Business"}]},"Meta":{"totalPages":1}}`
const secondAccount = "40817810802000000009/044525104"

func testBankClient(t *testing.T, transport testTransport) *BankClient {
	t.Helper()
	client, err := NewBankClient(&http.Client{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	client.pollInterval = 0
	return client
}
func TestBusinessBankStatementsUseVerifiedSchemaAndExactMoney(t *testing.T) {
	calls := 0
	client := testBankClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		if r.URL.Scheme != "https" || r.URL.Host != "enter.tochka.com" || r.Header.Get("Authorization") != "Bearer synthetic-token" || strings.Contains(r.URL.String(), "synthetic-token") {
			t.Fatal("bank destination/auth boundary changed")
		}
		switch r.URL.Path {
		case "/uapi/open-banking/v1.0/statements":
			if r.Method != http.MethodPost {
				t.Fatal(r.Method)
			}
			var body struct {
				Data struct {
					Statement struct{ AccountID, StartDateTime, EndDateTime string }
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body.Data.Statement.AccountID != "40817810802000000008/044525104" || body.Data.Statement.StartDateTime != "2026-09-01" || body.Data.Statement.EndDateTime != "2026-09-30" {
				t.Fatalf("init body = %#v", body)
			}
			return bankResponse(200, `{"Data":{"Statement":{"accountId":"40817810802000000008/044525104","statementId":"statement-1","status":"Created"}}}`), nil
		case "/uapi/open-banking/v1.0/accounts/40817810802000000008/044525104/statements/statement-1":
			return bankResponse(200, `{"Data":{"Statement":[{"accountId":"40817810802000000008/044525104","statementId":"statement-1","status":"Ready","startDateTime":"2026-09-01","endDateTime":"2026-09-30","Transaction":[{"transactionId":"tx1","creditDebitIndicator":"Credit","status":"Booked","documentProcessDate":"2026-09-02","description":"Аренда","Amount":{"amount":100.01,"currency":"RUB"},"DebtorParty":{"inn":"7701234567","name":"Тестовый плательщик"}},{"transactionId":"tx2","creditDebitIndicator":"Debit","Amount":{"amount":999,"currency":"RUB"}}]}]},"Meta":{"totalPages":1}}`), nil
		default:
			t.Fatalf("unexpected bank path %s", r.URL.Path)
			return nil, errors.New("unexpected path")
		}
	})
	payments, err := client.Statements(context.Background(), "synthetic-token", []BankAccount{{ID: "40817810802000000008/044525104"}}, "2026-09-01", "2026-09-30")
	if err != nil || len(payments) != 1 || payments[0].AmountMinor != 10001 || payments[0].PayerINN != "7701234567" || calls != 2 {
		t.Fatalf("statement result = %#v, %v; calls %d", payments, err, calls)
	}
	for _, raw := range []string{`0.001`, `-1`, `0`, `1e100`, `"not money"`} {
		if _, err := bankAmount(json.RawMessage(raw)); err == nil {
			t.Fatalf("accepted bank amount %s", raw)
		}
	}
}
func TestBusinessBankHTTPFailureIsRedactedAndRedirectIsNotFollowed(t *testing.T) {
	calls := 0
	client := testBankClient(t, func(r *http.Request) (*http.Response, error) {
		calls++
		response := bankResponse(302, `private token synthetic-token`)
		response.Header.Set("Location", "https://untrusted.example/private")
		return response, nil
	})
	_, err := client.Accounts(context.Background(), "synthetic-token")
	if err == nil || calls != 1 || strings.Contains(err.Error(), "synthetic-token") {
		t.Fatalf("redirect/error = %v, calls %d", err, calls)
	}
	client = testBankClient(t, func(*http.Request) (*http.Response, error) {
		return bankResponse(403, `{"message":"private upstream details synthetic-token"}`), nil
	})
	_, err = client.Accounts(context.Background(), "synthetic-token")
	if err == nil || strings.Contains(err.Error(), "private") || strings.Contains(err.Error(), "synthetic-token") {
		t.Fatalf("bank error leaked: %v", err)
	}
}

func TestBusinessBankRefusesUnprovenPagination(t *testing.T) {
	for _, pages := range []string{"0", "2"} {
		t.Run(pages, func(t *testing.T) {
			client := testBankClient(t, func(*http.Request) (*http.Response, error) {
				return bankResponse(200, strings.Replace(accountsJSON, `"totalPages":1`, `"totalPages":`+pages, 1)), nil
			})
			if _, err := client.Accounts(context.Background(), "synthetic-token"); err == nil {
				t.Fatal("partial or absent pagination metadata was accepted as all accounts")
			}
		})
	}
}
func TestBusinessBankInvoicePayloadUsesExactServerAmounts(t *testing.T) {
	posts := 0
	client := testBankClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodGet {
			return bankResponse(200, accountsJSON), nil
		}
		posts++
		if r.URL.Path != "/uapi/invoice/v1.0/bills" {
			t.Fatal(r.URL.Path)
		}
		var body struct {
			Data struct {
				AccountID string `json:"accountId"`
				Buyer     struct {
					Type string `json:"type"`
				} `json:"SecondSide"`
				Content struct {
					Invoice struct {
						Positions []bankPosition `json:"Positions"`
						Amount    json.Number    `json:"totalAmount"`
					} `json:"Invoice"`
				} `json:"Content"`
			} `json:"Data"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Data.Buyer.Type != "company" || body.Data.Content.Invoice.Amount.String() != "100.00" || body.Data.Content.Invoice.Positions[0].Unit != "усл.ед." || body.Data.Content.Invoice.Positions[0].VAT != "without_nds" {
			t.Fatalf("bank invoice = %#v", body)
		}
		return bankResponse(200, `{"Data":{"documentId":"created-document"}}`), nil
	})
	id, err := client.CreateInvoice(context.Background(), "synthetic-token", fixtureSeller(), fixtureParty(), fixtureInvoice("invoice", "2026-09", true))
	if err != nil || id != "created-document" || posts != 1 {
		t.Fatalf("create = %s %v, posts %d", id, err, posts)
	}
}
