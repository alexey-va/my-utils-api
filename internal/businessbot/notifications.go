package businessbot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/business"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

const (
	notificationCheckInterval = 5 * time.Minute
	bankSyncInterval          = 30 * time.Minute
	notificationCheckTimeout  = 4 * time.Minute

	syncAttemptField      = "sync-attempt"
	paymentsBaselineField = "payments-baseline"
	weeklyDateField       = "weekly-date"
	weeklyBatchField      = "weekly-batch"
	weeklyIndexField      = "weekly-index"
	paymentFieldPrefix    = "payment:"

	// Keep each report page within the Telegram sender's message limit.
	maxReportPageBytes = 3800
)

var notificationZone = time.FixedZone("MSK", 3*60*60)

var releaseNotificationLease = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)

// Notifications sends updates for one business workspace to an authorized chat.
// The caller controls opt-in and supplies a sender for plain text.
type Notifications struct {
	workspace func(context.Context, string, string, string) (business.Workspace, error)
	syncBank  func(context.Context, string) (business.Bank, error)
	redis     redis.UniversalClient
	owner     string
	key       string
	send      func(context.Context, string) error
	ready     func(context.Context) bool
}

func NewNotifications(service *business.Service, client redis.UniversalClient, owner string, send func(context.Context, string) error, ready func(context.Context) bool) *Notifications {
	return &Notifications{
		workspace: service.Workspace, syncBank: service.SyncRecent, redis: client,
		owner: owner, key: "myutils:business-notifications:" + owner, send: send, ready: ready,
	}
}

func (n *Notifications) Run(ctx context.Context) {
	ticker := time.NewTicker(notificationCheckInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		if err := n.check(ctx, time.Now()); err != nil && ctx.Err() == nil {
			slog.WarnContext(ctx, "business notifications check failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *Notifications) check(ctx context.Context, now time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, notificationCheckTimeout)
	defer cancel()
	if n.ready == nil || !n.ready(ctx) {
		return nil
	}

	leaseKey, leaseID := n.key+":lease", uuid.NewString()
	acquired, err := n.redis.SetNX(ctx, leaseKey, leaseID, notificationCheckTimeout+30*time.Second).Result()
	if err != nil || !acquired {
		return err
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = releaseNotificationLease.Run(releaseCtx, n.redis, []string{leaseKey}, leaseID).Err()
	}()

	checkpoint, err := n.redis.HGetAll(ctx, n.key).Result()
	if err != nil {
		return fmt.Errorf("read business notification checkpoint: %w", err)
	}
	now = now.In(notificationZone)
	if checkpoint[weeklyDateField] == "" {
		// Start with the first scheduled report on or after the day of activation.
		checkpoint[weeklyDateField] = weeklyReportTime(now.AddDate(0, 0, -1)).Format(time.DateOnly)
		if err := n.redis.HSet(ctx, n.key, weeklyDateField, checkpoint[weeklyDateField]).Err(); err != nil {
			return fmt.Errorf("initialize weekly report checkpoint: %w", err)
		}
	}
	workspace, err := n.workspace(ctx, n.owner, "all", "")
	if err != nil {
		return fmt.Errorf("read workspace for business notifications: %w", err)
	}

	var syncErr error
	lastAttempt, _ := time.Parse(time.RFC3339, checkpoint[syncAttemptField])
	lastSync, _ := time.Parse(time.RFC3339, workspace.Bank.LastSyncedAt)
	syncFailed := lastAttempt.After(lastSync)
	if workspace.Bank.Connected && !now.Before(lastAttempt.Add(bankSyncInterval)) && !now.Before(lastSync.Add(bankSyncInterval)) {
		if err := n.redis.HSet(ctx, n.key, syncAttemptField, now.UTC().Format(time.RFC3339)).Err(); err != nil {
			return fmt.Errorf("checkpoint business bank sync attempt: %w", err)
		}
		_, syncErr = n.syncBank(ctx, n.owner)
		syncFailed = syncErr != nil
		updated, err := n.workspace(ctx, n.owner, "all", "")
		if err != nil {
			return errors.Join(syncErr, fmt.Errorf("read workspace after bank refresh: %w", err))
		}
		workspace = updated
	}
	return errors.Join(
		syncErr,
		n.sendNewPayments(ctx, checkpoint, workspace, now, !syncFailed),
		n.sendWeeklyReport(ctx, checkpoint, workspace, now, syncFailed),
	)
}

func (n *Notifications) sendNewPayments(ctx context.Context, checkpoint map[string]string, workspace business.Workspace, now time.Time, syncOK bool) error {
	baseline := checkpoint[paymentsBaselineField]
	if baseline == "" {
		if !syncOK || !workspace.Bank.Connected || workspace.Bank.LastSyncedAt == "" || workspace.Bank.Error != "" {
			return nil
		}
		// Store only IDs and the activation day. A later archive import can add
		// bank rows with old dates, which are outside the alert window.
		fields := map[string]any{paymentsBaselineField: now.Format(time.DateOnly)}
		for _, payment := range workspace.Payments {
			if payment.Source == "tochka" {
				fields[paymentFieldPrefix+payment.ID] = "1"
			}
		}
		return n.redis.HSet(ctx, n.key, fields).Err()
	}

	parties := notificationPayers(workspace)
	// Workspace payments are newest first. Deliver an accumulated batch from
	// oldest to newest so the newest credit ends up last in the chat.
	for i := len(workspace.Payments) - 1; i >= 0; i-- {
		payment := workspace.Payments[i]
		field := paymentFieldPrefix + payment.ID
		if payment.Source != "tochka" || payment.Date < baseline || checkpoint[field] != "" {
			continue
		}
		if payment.Classification == "historical" || payment.Classification == "outside_period" ||
			payment.PayerINN != "" && payment.PayerINN == workspace.Seller.INN {
			continue
		}
		payer := parties[payment.CounterpartyID]
		if payer == "" {
			payer = notificationLabel(payment.CounterpartyName)
		}
		if err := n.deliver(ctx, paymentNotice(payment, payer), field, "1"); err != nil {
			return fmt.Errorf("deliver new business payment: %w", err)
		}
	}
	return nil
}

func paymentNotice(payment business.PaymentView, payer string) string {
	if payer == "" {
		payer = "плательщик не указан"
	}
	text := fmt.Sprintf("Новое поступление в Точке\n%s · %s · %s", payment.Date, notificationAmount(payment.AmountMinor), payer)
	if payment.AllocatedMinor > 0 {
		text += "\nСопоставлено со счетами " + notificationAmount(payment.AllocatedMinor) + "."
	}
	if payment.AdvanceMinor > 0 {
		text += "\nАванс " + notificationAmount(payment.AdvanceMinor) + "."
	}
	if payment.AllocatedMinor+payment.AdvanceMinor < payment.AmountMinor {
		text += "\nПроверьте назначение или распределение платежа."
	}
	return text
}

func weeklyReportTime(now time.Time) time.Time {
	monday := now.Day() - (int(now.Weekday())+6)%7
	scheduled := time.Date(now.Year(), now.Month(), monday, 10, 0, 0, 0, notificationZone)
	if now.Before(scheduled) {
		scheduled = scheduled.AddDate(0, 0, -7)
	}
	return scheduled
}

type weeklyBatch struct {
	Date  string
	Pages []string
}

func (n *Notifications) sendWeeklyReport(ctx context.Context, checkpoint map[string]string, workspace business.Workspace, now time.Time, syncFailed bool) error {
	scheduled := weeklyReportTime(now).Format(time.DateOnly)
	var batch weeklyBatch
	index := 0
	if pending := checkpoint[weeklyBatchField]; pending != "" {
		var err error
		index, err = strconv.Atoi(checkpoint[weeklyIndexField])
		if err != nil || json.Unmarshal([]byte(pending), &batch) != nil || index < 0 || index >= len(batch.Pages) {
			return errors.New("invalid pending business weekly report checkpoint")
		}
	} else {
		if scheduled <= checkpoint[weeklyDateField] {
			return nil
		}
		batch = weeklyBatch{Date: scheduled, Pages: weeklyReportPages(workspace, now, syncFailed)}
		if len(batch.Pages) > 1 {
			// Retain a snapshot only while a multi-message report is in progress,
			// so an intervening payment cannot shift rows between retried pages.
			pending, err := json.Marshal(batch)
			if err != nil {
				return err
			}
			if err := n.redis.HSet(ctx, n.key, weeklyBatchField, pending, weeklyIndexField, index).Err(); err != nil {
				return err
			}
		}
	}
	for ; index < len(batch.Pages); index++ {
		fields := []any{weeklyIndexField, index + 1}
		if index == len(batch.Pages)-1 {
			fields = []any{weeklyDateField, batch.Date, weeklyBatchField, "", weeklyIndexField, ""}
		}
		if err := n.deliver(ctx, batch.Pages[index], fields...); err != nil {
			return fmt.Errorf("deliver business weekly report: %w", err)
		}
	}
	return nil
}

func (n *Notifications) deliver(ctx context.Context, message string, fields ...any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := n.send(ctx, message); err != nil {
		return err
	}
	// Checkpoint after the transport acknowledges receipt. A lost response or
	// crash before this write can still cause a duplicate on the next check.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return n.redis.HSet(saveCtx, n.key, fields...).Err()
}

func weeklyReportPages(workspace business.Workspace, now time.Time, syncFailed bool) []string {
	title := "Сверка счетов на " + now.Format("02.01.2006")
	lines := []string{}
	parties := notificationPayers(workspace)
	for _, invoice := range workspace.Invoices {
		if !invoice.IsCurrent || invoice.OutstandingMinor <= 0 {
			continue
		}
		payer := parties[invoice.CounterpartyID]
		if payer == "" {
			payer = "плательщик не указан"
		}
		state := "платёж не сопоставлен"
		switch {
		case invoice.Provider == "tochka" && (invoice.ProviderState == "pending" || invoice.ProviderState == "unknown" || invoice.ProviderState == "rejected"):
			state = "выставление требует проверки"
		case invoice.Status == "partial":
			state = "частичная оплата"
		case invoice.Status == "unreviewed":
			state = "период выписки покрыт не полностью"
		}
		lines = append(lines, fmt.Sprintf("• %s · %s · №%s · %s — %s",
			invoice.Period, payer, notificationLabel(invoice.Number), notificationAmount(invoice.OutstandingMinor), state))
	}
	sort.Strings(lines)
	first := title + "\n" + notificationCoverage(workspace.Bank, now, syncFailed)
	if len(lines) == 0 {
		return []string{first + "\n\nПо данным сверки открытых счетов нет."}
	}
	first += "\n\nОстатки по сопоставленным платежам"
	if workspace.Summary.UnallocatedMinor > workspace.Summary.AdvanceMinor {
		first += "\nЕсть платежи, требующие сверки."
	}
	pages := []string{first}
	for _, line := range lines {
		last := len(pages) - 1
		page := pages[last] + "\n" + line
		if len(page) > maxReportPageBytes {
			pages = append(pages, title+" · продолжение\n"+line)
			continue
		}
		pages[last] = page
	}
	return pages
}

func notificationCoverage(bank business.Bank, now time.Time, syncFailed bool) string {
	lines := []string{}
	switch {
	case bank.CoverageStart != "" && bank.CoverageEnd != "":
		lines = append(lines, fmt.Sprintf("Выписка Точки · %s — %s.", bank.CoverageStart, bank.CoverageEnd))
	default:
		lines = append(lines, "Подтверждённого периода выписки Точки пока нет.")
	}
	if lastSync, err := time.Parse(time.RFC3339, bank.LastSyncedAt); err == nil {
		lines = append(lines, "Последнее обновление "+lastSync.In(notificationZone).Format("02.01.2006 15:04")+" МСК.")
	}
	switch {
	case !bank.Connected:
		lines = append(lines, "Банк отключён.")
	case bank.Error != "":
		lines = append(lines, notificationLabel(bank.Error))
	case syncFailed:
		lines = append(lines, "Обновление выписки завершилось ошибкой.")
	case bank.CoverageEnd != "" && bank.CoverageEnd < now.Format(time.DateOnly):
		lines = append(lines, "Данные за дни после окончания выписки ещё не получены.")
	}
	return strings.Join(lines, "\n")
}

func notificationPayers(workspace business.Workspace) map[string]string {
	parties := make(map[string]string, len(workspace.Counterparties))
	for _, party := range workspace.Counterparties {
		parties[party.ID] = notificationLabel(party.Name)
	}
	return parties
}

func notificationLabel(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if trimmed := limitText(text, 150); len(trimmed) < len(text) {
		return trimmed + "…"
	}
	return text
}

func notificationAmount(minor int64) string {
	return fmt.Sprintf("%d,%02d ₽", minor/100, minor%100)
}
