package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/workout"
)

func TestFormatFreshSnapshotIncludesWorkoutAndBodyWeight(t *testing.T) {
	t.Parallel()
	snapshot := FormatFreshSnapshot(
		time.Date(2026, 8, 20, 12, 0, 0, 0, time.FixedZone("Europe/Moscow", 3*60*60)),
		workout.Grid{Dates: []string{"2026-08-20"}, Rows: []workout.GridRow{{ExerciseID: "bench", ExerciseName: "Жим", Cells: map[string]workout.Cell{"2026-08-20": {Display: "70  3×10  (12)"}}}}},
		[]workout.Exercise{{ID: "bench", Name: "Жим", MuscleGroup: "chest"}},
		health.WeightHistory{Days: []health.WeightDay{{Date: "2026-08-20", WeightKg: 82.5}}},
		snapshotLimits{calendarDays: 14, recentEntries: 30, progressSessions: 4},
	)
	for _, want := range []string{"Актуальный снимок дневника", "20.08", "Жим", "82.5 кг", "грудь", "Эта неделя", "Последние 30"} {
		if !strings.Contains(snapshot, want) {
			t.Fatalf("snapshot %q does not contain %q", snapshot, want)
		}
	}
}

func TestToolServiceTodayUsesRuntimeZone(t *testing.T) {
	t.Parallel()
	service := &ToolService{now: func() time.Time {
		return time.Date(2026, 8, 20, 23, 30, 0, 0, time.UTC)
	}}
	service.SetZoneID(func() string { return "Asia/Tokyo" })
	if got := service.today(); got != "2026-08-21" {
		t.Fatalf("today = %s", got)
	}
}
