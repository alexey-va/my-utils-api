package agent

import (
	"context"
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
