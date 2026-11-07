package businessbot

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/business"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func newTestNotifications(t *testing.T, workspace *business.Workspace) *Notifications {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return &Notifications{
		redis: client, owner: "test-owner", key: "business-notifications:test-owner",
		ready: func(context.Context) bool { return true },
		workspace: func(_ context.Context, owner, period, payer string) (business.Workspace, error) {
			if owner != "test-owner" || period != "all" || payer != "" {
				t.Fatalf("unexpected workspace query: %q %q %q", owner, period, payer)
			}
			return *workspace, nil
		},
	}
}

func TestNotificationsSkipUnavailableOwner(t *testing.T) {
	n := &Notifications{ready: func(context.Context) bool { return false }}
	if err := n.check(context.Background(), time.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestNotificationsBaselineFiltersAndRetriesNewPayments(t *testing.T) {
	ctx := context.Background()
	start := time.Date(2026, 11, 4, 12, 0, 0, 0, notificationZone)
	today := start.Format(time.DateOnly)
	payment := func(id, name, classification, date string) business.PaymentView {
		return business.PaymentView{
			Payment:        business.Payment{ID: id, CounterpartyName: name, Date: date, AmountMinor: 124001, Source: "tochka"},
			Classification: classification,
		}
	}
	workspace := business.Workspace{
		Bank:     business.Bank{Connected: true},
		Seller:   business.Seller{INN: "seller"},
		Payments: []business.PaymentView{payment("existing", "Старый", "rental", today)},
		Counterparties: []business.CounterpartyView{{
			Counterparty: business.Counterparty{ID: "payer", Name: "ООО Север"},
		}},
	}
	n := newTestNotifications(t, &workspace)
	syncCalls := 0
	now := start
	n.syncBank = func(_ context.Context, owner string) (business.Bank, error) {
		if owner != n.owner {
			t.Fatalf("sync for unexpected owner: %q", owner)
		}
		syncCalls++
		if syncCalls == 2 {
			rental := payment("rental", "Банковское название", "rental", today)
			rental.CounterpartyID, rental.AllocatedMinor = "payer", 124001
			own := payment("own", "Свой перевод", "unclassified", today)
			own.PayerINN = workspace.Seller.INN
			manual := payment("manual", "Ручной платёж", "unclassified", today)
			manual.Source = "manual"
			workspace.Payments = append([]business.PaymentView{
				payment("unknown", "Новый отправитель", "unclassified", today),
				payment("history", "Архив", "historical", today),
				payment("outside", "Вне периода", "outside_period", today),
				own, manual, rental,
			}, workspace.Payments...)
			workspace.Payments = append(workspace.Payments, payment("backfill", "Старая выписка", "unclassified", "2026-10-30"))
		}
		workspace.Bank.LastSyncedAt = now.UTC().Format(time.RFC3339)
		return workspace.Bank, nil
	}
	var sent []string
	sendError := errors.New("temporary delivery failure")
	n.send = func(_ context.Context, message string) error {
		if strings.Contains(message, "Новый отправитель") && len(sent) == 1 {
			return sendError
		}
		sent = append(sent, message)
		return nil
	}
	if err := n.check(ctx, now); err != nil || syncCalls != 1 || len(sent) != 0 {
		t.Fatalf("baseline check = %v, syncs %d, sends %q", err, syncCalls, sent)
	}
	now = start.Add(5 * time.Minute)
	if err := n.check(ctx, now); err != nil || syncCalls != 1 || len(sent) != 0 {
		t.Fatalf("rate-limited check = %v, syncs %d, sends %q", err, syncCalls, sent)
	}
	now = start.Add(bankSyncInterval)
	if err := n.check(ctx, now); !errors.Is(err, sendError) || syncCalls != 2 || len(sent) != 1 {
		t.Fatalf("arrival check = %v, syncs %d, sends %q", err, syncCalls, sent)
	}
	if !strings.Contains(sent[0], "ООО Север") || !strings.Contains(sent[0], "1240,01 ₽") || !strings.Contains(sent[0], "Сопоставлено со счетами") {
		t.Fatalf("known payment lacks reconciliation details: %q", sent[0])
	}

	// A new notifier instance retains the baseline, rate limit and sent IDs.
	restarted := *n
	restarted.send = func(_ context.Context, message string) error {
		sent = append(sent, message)
		return nil
	}
	now = now.Add(5 * time.Minute)
	if err := restarted.check(ctx, now); err != nil || syncCalls != 2 || len(sent) != 2 {
		t.Fatalf("restarted check = %v, syncs %d, sends %q", err, syncCalls, sent)
	}
	if !strings.Contains(sent[1], "Новый отправитель") || !strings.Contains(sent[1], "Проверьте назначение") {
		t.Fatalf("unknown payment lacks review details: %q", sent[1])
	}
	now = now.Add(5 * time.Minute)
	if err := restarted.check(ctx, now); err != nil || len(sent) != 2 {
		t.Fatalf("repeat check = %v, sends %q", err, sent)
	}
}

func TestNotificationsWeeklyScheduleAndPartialDelivery(t *testing.T) {
	ctx := context.Background()
	monday := time.Date(2026, 11, 9, 10, 0, 0, 0, notificationZone)
	workspace := business.Workspace{
		Bank:    business.Bank{CoverageStart: "2026-09-01", CoverageEnd: "2026-10-31", LastSyncedAt: "2026-10-31T17:42:00Z"},
		Summary: business.Summary{UnallocatedMinor: 150},
		Counterparties: []business.CounterpartyView{{
			Counterparty: business.Counterparty{ID: "payer", Name: "ООО Север"},
		}},
	}
	for index := range 45 {
		invoice := business.InvoiceView{
			Invoice: business.Invoice{
				ID: fmt.Sprint(index), Number: fmt.Sprint(index), CounterpartyID: "payer",
				Period: "2026-10", IsCurrent: true,
			},
			OutstandingMinor: 12300,
			Status:           "unmatched",
		}
		switch index {
		case 0:
			invoice.Status = "partial"
		case 1:
			invoice.Period, invoice.Status = "2026-11", "unreviewed"
		case 2:
			invoice.Provider, invoice.ProviderState = "tochka", "unknown"
		case 3:
			invoice.Number, invoice.IsCurrent = "archived", false
		case 4:
			invoice.Number, invoice.OutstandingMinor = "paid", 0
		}
		workspace.Invoices = append(workspace.Invoices, invoice)
	}
	n := newTestNotifications(t, &workspace)
	n.syncBank = func(context.Context, string) (business.Bank, error) {
		t.Fatal("disconnected bank must not be polled")
		return business.Bank{}, nil
	}
	var sent []string
	sendError := errors.New("temporary report failure")
	n.send = func(_ context.Context, message string) error {
		if len(sent) == 1 {
			return sendError
		}
		sent = append(sent, message)
		return nil
	}

	if err := n.check(ctx, monday.Add(-time.Minute)); err != nil || len(sent) != 0 {
		t.Fatalf("early report check = %v, sends %q", err, sent)
	}
	wantPages := weeklyReportPages(workspace, monday, false)
	if len(wantPages) < 2 {
		t.Fatalf("long weekly report has %d pages", len(wantPages))
	}
	if err := n.check(ctx, monday.UTC()); !errors.Is(err, sendError) || len(sent) != 1 {
		t.Fatalf("Monday 10 MSK check = %v, sends %d", err, len(sent))
	}

	// The retry uses the same report snapshot even though the ledger changed.
	workspace.Invoices = nil
	restarted := *n
	restarted.send = func(_ context.Context, message string) error {
		sent = append(sent, message)
		return nil
	}
	if err := restarted.check(ctx, monday.Add(5*time.Minute)); err != nil || !slices.Equal(sent, wantPages) {
		t.Fatalf("report retry = %v, sends %d, want %d original pages", err, len(sent), len(wantPages))
	}
	report := strings.Join(sent, "\n")
	for _, want := range []string{
		"2026-09-01 — 2026-10-31", "31.10.2026 20:42 МСК", "Банк отключён",
		"ООО Север", "123,00 ₽", "платёж не сопоставлен", "частичная оплата",
		"2026-11", "период выписки покрыт не полностью", "выставление требует проверки", "Есть платежи, требующие сверки",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report missing %q", want)
		}
	}
	for _, excluded := range []string{"archived", "paid"} {
		if strings.Contains(report, excluded) {
			t.Errorf("report includes excluded invoice %q", excluded)
		}
	}
	for _, page := range sent {
		if len(page) > maxReportPageBytes {
			t.Errorf("page has %d bytes", len(page))
		}
	}

	if err := restarted.check(ctx, monday.AddDate(0, 0, 1)); err != nil || len(sent) != len(wantPages) {
		t.Fatalf("same-week check = %v, sends %d", err, len(sent))
	}
	if err := restarted.check(ctx, monday.AddDate(0, 0, 7)); err != nil || len(sent) != len(wantPages)+1 {
		t.Fatalf("following-week check = %v, sends %d", err, len(sent))
	}
	if !strings.Contains(sent[len(sent)-1], "открытых счетов нет") {
		t.Fatalf("following week used stale invoice data: %q", sent[len(sent)-1])
	}
}
