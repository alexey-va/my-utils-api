package agent

import (
	"reflect"
	"testing"
	"time"
)

func TestSandboxCorrectionCopiesSetsByCanonicalIdentity(t *testing.T) {
	state := sandboxState{
		Exercises: []sandboxExercise{{ID: "chest", Name: "Бабочка"}, {ID: "shoulders", Name: "Бабочка на плечи"}},
		Workouts: []sandboxWorkout{
			{ExerciseID: "chest", ExerciseName: "Бабочка", PerformedOn: "2026-09-01", WeightKg: 55, Reps: []int{10, 9}, Weights: []int{50, 55}},
			{ExerciseID: "shoulders", ExerciseName: "Бабочка на плечи", PerformedOn: "2026-09-01", WeightKg: 25, Reps: []int{12, 11}},
		},
	}
	tools := &ToolService{now: time.Now}
	args := map[string]any{"exercise_name": "Бабочка", "exercise_id": "chest", "date": "2026-09-01", "source_date": "2026-09-01", "weight_kg": 62.5}
	if _, err := tools.runSandboxTool(&state, "copy_workout", args); err != nil {
		t.Fatal(err)
	}
	for _, row := range state.Workouts {
		if row.ExerciseID == "chest" && (row.WeightKg != 62.5 || !reflect.DeepEqual(row.Reps, []int{10, 9}) || len(row.Weights) != 0) {
			t.Fatalf("correction corrupted original sets: %+v", row)
		}
		if row.ExerciseID == "shoulders" && (row.WeightKg != 25 || !reflect.DeepEqual(row.Reps, []int{12, 11})) {
			t.Fatalf("another exercise was changed: %+v", row)
		}
	}
	args["exercise_id"] = "unknown"
	if _, err := tools.runSandboxTool(&state, "copy_workout", args); err == nil {
		t.Fatal("unknown ID silently fell back to a real name")
	}
}

func TestSandboxExplicitCopySourceSurvivesMovingToEarlierDate(t *testing.T) {
	state := sandboxState{Exercises: []sandboxExercise{{ID: "press", Name: "Жим"}}, Workouts: []sandboxWorkout{{ExerciseID: "press", ExerciseName: "Жим", PerformedOn: "2026-09-10", WeightKg: 60, Reps: []int{10, 10}}}}
	tools := &ToolService{now: time.Now}
	args := map[string]any{"exercise_name": "Жим", "exercise_id": "press", "date": "2026-09-09", "source_date": "2026-09-10"}
	if _, err := tools.runSandboxTool(&state, "copy_workout", args); err != nil {
		t.Fatal(err)
	}
	if len(state.Workouts) != 2 || state.Workouts[1].PerformedOn != "2026-09-09" || state.Workouts[1].WeightKg != 60 {
		t.Fatalf("copy=%+v", state.Workouts)
	}
	deleteArgs := map[string]any{"exercise_name": "Жим", "exercise_id": "press", "performed_on": "2026-09-10"}
	if _, err := tools.runSandboxTool(&state, "delete_workout", deleteArgs); err != nil {
		t.Fatal(err)
	}
	if len(state.Workouts) != 1 || state.Workouts[0].PerformedOn != "2026-09-09" {
		t.Fatalf("move=%+v", state.Workouts)
	}
}
