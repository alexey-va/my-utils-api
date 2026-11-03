package business

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestBusinessBankStartsAllReportsThenWaitsPastInitialProcessing(t *testing.T) {
	const first = "40817810802000000008/044525104"
	initialized, reads := 0, 0
	client := testBankClient(t, func(r *http.Request) (*http.Response, error) {
		if r.Method == http.MethodPost {
			var body struct {
				Data struct {
					Statement struct {
						ID string `json:"accountId"`
					} `json:"Statement"`
				} `json:"Data"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			initialized++
			return bankResponse(200, `{"Data":{"Statement":{"accountId":"`+body.Data.Statement.ID+`","statementId":"statement","status":"Created"}}}`), nil
		}
		if initialized != 2 {
			t.Fatal("polling started before both reports were initialized")
		}
		account := secondAccount
		status := "Ready"
		if strings.Contains(r.URL.Path, first) {
			account = first
			reads++
			if reads <= 19 {
				status = "Created"
			}
		}
		return bankResponse(200, `{"Data":{"Statement":[{"accountId":"`+account+`","statementId":"statement","status":"`+status+`","startDateTime":"2026-09-01","endDateTime":"2026-09-30","Transaction":[]}]},"Meta":{"totalPages":1}}`), nil
	})
	client.pollInterval = time.Nanosecond
	payments, err := client.Statements(context.Background(), "synthetic-token", []BankAccount{{ID: first}, {ID: secondAccount}}, "2026-09-01", "2026-09-30")
	if err != nil || len(payments) != 0 || initialized != 2 || reads != 20 {
		t.Fatalf("delayed reports = %#v %v; initialized %d reads %d", payments, err, initialized, reads)
	}
}

func TestBusinessCanceledReportPollingKeepsPreviousCoverage(t *testing.T) {
	var cancelRead context.CancelFunc
	client := testBankClient(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/accounts") {
			return bankResponse(200, accountsJSON), nil
		}
		if r.Method == http.MethodPost {
			return bankResponse(200, `{"Data":{"Statement":{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Created"}}}`), nil
		}
		cancelRead()
		return bankResponse(200, `{"Data":{"Statement":[{"accountId":"40817810802000000008/044525104","statementId":"statement","status":"Created","startDateTime":"2026-09-01","endDateTime":"2026-09-30","Transaction":[]}]},"Meta":{"totalPages":1}}`), nil
	})
	client.pollInterval = time.Hour // Cancellation, not a short retry timer, ends the wait.
	service, owner := testService(t, client)
	ctx := context.Background()
	if _, err := service.Connect(ctx, owner, "synthetic-token"); err != nil {
		t.Fatal(err)
	}
	request := importFixture()
	request.Payments = nil
	request.Bank = &Bank{CoverageStart: "2026-08-01", CoverageEnd: "2026-08-31", AccountCount: 1}
	if _, err := service.Import(ctx, owner, request); err != nil {
		t.Fatal(err)
	}
	readCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cancelRead = cancel
	_, err := service.Sync(readCtx, owner, "2026-09-01", "2026-09-30")
	requireStatus(t, err, 502)
	view, err := service.Workspace(ctx, owner, "all", "")
	if err != nil {
		t.Fatal(err)
	}
	if view.Bank.CoverageStart != "2026-08-01" || view.Bank.CoverageEnd != "2026-08-31" || view.Bank.LastSyncedAt != "" || len(view.Payments) != 0 || view.Summary.ReconciliationComplete {
		t.Fatalf("cancelled report changed coverage/history: %#v %#v", view.Bank, view.Summary)
	}
}
