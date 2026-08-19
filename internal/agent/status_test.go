package agent

import "testing"

func TestToolsStatusLabelNormalizesAliasesAndCountsDistinctActions(t *testing.T) {
	t.Parallel()
	if got := ToolsStatusLabel([]string{"logWorkout", "log_workout"}); got != "Записываю в дневник…" {
		t.Fatalf("same action label = %q", got)
	}
	if got := ToolsStatusLabel([]string{"logWorkout", "getDays", "log_workout"}); got != "Выполняю 2 действия…" {
		t.Fatalf("multi action label = %q", got)
	}
	if got := ToolStatusLabel("send_progress_chart"); got != "Строю график прогресса…" {
		t.Fatalf("chart label = %q", got)
	}
}
