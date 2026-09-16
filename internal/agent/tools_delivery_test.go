package agent

import (
	"context"
	"strings"
	"testing"
)

type recordingDelivery struct {
	text    string
	buttons string
}

func (d *recordingDelivery) SendRichMessage(_ context.Context, _ int64, text, buttons string) error {
	d.text = text
	d.buttons = buttons
	return nil
}

func (*recordingDelivery) SendProgressChart(context.Context, int64, string, int) error {
	return nil
}

func (*recordingDelivery) SendOneRM(context.Context, int64, string, float64, int, float64) error {
	return nil
}

type recordingScheduler struct{ weeklyReportChatID int64 }

func (*recordingScheduler) SendNow(context.Context, int64, string) (string, error) { return "", nil }
func (*recordingScheduler) Schedule(context.Context, int64, string, string) (string, error) {
	return "", nil
}
func (*recordingScheduler) Cancel(context.Context, string) (bool, error) { return false, nil }
func (s *recordingScheduler) SendWeeklyReportNow(_ context.Context, chatID int64) (string, error) {
	s.weeklyReportChatID = chatID
	return "Субботние графики шагов и веса отправляются сейчас (workflow go-v1-weekly-health-report-now-test).", nil
}

func TestSendRichMessageNormalizesTelegramMarkup(t *testing.T) {
	t.Parallel()
	delivery := &recordingDelivery{}
	tools := NewToolService(nil, nil, nil, nil, nil, delivery)

	result, err := tools.Execute(context.Background(), 42, "send_rich_message", map[string]any{
		"text":    "### План\n**Сегодня** — `3×10`.",
		"buttons": "Готово:done",
	}, "покажи сообщение", false)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Сообщение отправлено." {
		t.Fatalf("result = %q", result)
	}
	if want := "<b>План</b>\n<b>Сегодня</b> — <code>3×10</code>."; delivery.text != want {
		t.Fatalf("text = %q, want %q", delivery.text, want)
	}
	if delivery.buttons != "Готово:done" {
		t.Fatalf("buttons = %q", delivery.buttons)
	}
}

func TestSendWeeklyHealthReportUsesRuntimeDateAndSaturdayLookback(t *testing.T) {
	t.Parallel()
	scheduler := &recordingScheduler{}
	tools := NewToolService(nil, nil, nil, nil, scheduler, nil)

	result, err := tools.Execute(context.Background(), 42, "send_weekly_health_report", map[string]any{}, "пришли графики", false)
	if err != nil {
		t.Fatal(err)
	}
	if result != "Субботние графики шагов и веса отправляются сейчас (workflow go-v1-weekly-health-report-now-test)." {
		t.Fatalf("result = %q", result)
	}
	if scheduler.weeklyReportChatID != 42 {
		t.Fatalf("chat ID = %d", scheduler.weeklyReportChatID)
	}
}

func TestSandboxWeeklyHealthReportNeverSendsToTelegram(t *testing.T) {
	t.Parallel()
	tools := &ToolService{}
	result, err := tools.runSandboxTool(&sandboxState{}, "send_weekly_health_report", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "SANDBOX") || !strings.Contains(result, "ничего не отправлено") {
		t.Fatalf("result = %q", result)
	}
}
