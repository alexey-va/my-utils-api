package agent

import (
	"strings"
	"testing"
)

func TestSandboxWorkoutReceiptsShowTheSavedValues(t *testing.T) {
	t.Parallel()
	tools := &ToolService{}
	state := sandboxState{Exercises: []sandboxExercise{{ID: "press", Name: "Жим"}}}
	const date = "2026-11-07"
	for _, check := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"log_workout", map[string]any{"exercise_name": "Жим", "notation": "62.5 3*10/10", "date": date}, "Записал за 7 ноября 2026 года: Жим — 62,5 кг, 4×10."},
		{"log_workout", map[string]any{"exercise_name": "Жим", "notation": "62.5 10/10", "date": date}, "Записал за 7 ноября 2026 года: Жим — 62,5 кг, 2×10."},
		{"log_workout", map[string]any{"exercise_name": "Жим", "notation": "60/65 10/8", "date": date}, "Записал за 7 ноября 2026 года: Жим — 60 кг × 10, 65 кг × 8."},
		{"log_body_weight", map[string]any{"weight_kg": 81.46, "date": date}, "Записал вес за 7 ноября 2026 года: 81,5 кг."},
		{"log_body_weight", map[string]any{"weight_kg": 82.04, "date": date}, "Обновил вес за 7 ноября 2026 года: 82 кг."},
	} {
		got, err := tools.runSandboxTool(&state, check.tool, check.args)
		if err != nil || got != check.want {
			t.Fatalf("%s %v: reply = %q, want %q, err = %v", check.tool, check.args, got, check.want, err)
		}
	}
}

func TestExerciseClarificationShowsNamesInsteadOfIDs(t *testing.T) {
	t.Parallel()
	_, err := ResolveExercise("", "Бабочка", []ExerciseCandidate{
		{ID: "secret-chest-id", Name: "Бабочка на грудь", MuscleGroup: "chest"},
		{ID: "secret-shoulder-id", Name: "Обратная бабочка", MuscleGroup: "shoulders"},
	})
	if err == nil {
		t.Fatal("expected ambiguous exercise")
	}
	got := visibleMutationError(plannedAction{}, err)
	if strings.Contains(got, "secret-") || !strings.Contains(got, "Бабочка на грудь") || !strings.Contains(got, "Обратная бабочка") {
		t.Fatalf("clarification = %q", got)
	}
}

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

func TestNormalizeReplyRemovesEncodedLeadingWhitespace(t *testing.T) {
	t.Parallel()
	want := "<b>Вес записан:</b> 81,5 кг за субботу, 29.08.\nИ еще поправь"
	for _, entity := range []string{"&#x20;", "&#32;", "&nbsp;"} {
		raw := "**Вес записан:** 81,5 кг за субботу, 29.08.\n" + entity + "И еще поправь"
		if got := NormalizeReply(raw); got != want {
			t.Fatalf("entity %q: reply = %q, want %q", entity, got, want)
		}
	}
}

func TestNormalizeReplyRemovesLeakedHistoryTimestamp(t *testing.T) {
	t.Parallel()
	raw := "[Отправлено 29.08.2026 19:20 Europe/Moscow] <b>Плечи</b> — <code>22 кг 10/10</code>."
	want := "<b>Плечи</b> — <code>22 кг 10/10</code>."
	if got := NormalizeReply(raw); got != want {
		t.Fatalf("reply = %q, want %q", got, want)
	}
	if got := NormalizeReply("[Отправoutput truncated..."); got != safeReplyFallback {
		t.Fatalf("corrupted reply = %q", got)
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
	for _, reply := range []string{
		"[Отправoutput truncated...",
		"output truncated...",
	} {
		if !LooksInvalidForRussianUser(reply) {
			t.Fatalf("internal truncation leak should be rejected: %q", reply)
		}
	}
}
