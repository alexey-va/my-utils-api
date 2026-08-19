package agent

import "testing"

func TestNormalizeReplyForTelegram(t *testing.T) {
	t.Parallel()
	got := NormalizeReply("### План\n**Воскресенье, 02.08** — отдых. Потом `70 кг 3×10`.")
	want := "<b>План</b>\n<b>Воскресенье, 02.08</b> — отдых. Потом <code>70 кг 3×10</code>."
	if got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
	if got := NormalizeReply("<b>Сегодня</b> — отдых."); got != "<b>Сегодня</b> — отдых." {
		t.Fatalf("existing HTML changed: %q", got)
	}
}

func TestRussianReplyGuard(t *testing.T) {
	t.Parallel()
	if !LooksInvalidForRussianUser("作为一个人工智能语言模型，我还没学习") {
		t.Fatal("Chinese refusal should be rejected")
	}
	if LooksInvalidForRussianUser("Сегодня тренировка: жим и присед.") {
		t.Fatal("Russian reply should be accepted")
	}
}
