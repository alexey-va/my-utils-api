package workout

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"os"
	"reflect"
	"testing"
)

func TestApplyCopyOptionsRelativeWeightAndRepetitions(t *testing.T) {
	weightDelta := 4.0
	request, err := ApplyCopyOptions(EntryRequest{
		ExerciseID: "press", WeightKg: 68, SetCount: 2, RepsPerSet: 10, MaxReps: 10,
		SetReps: []int{10, 10},
	}, &CopyOptions{WeightDeltaKg: &weightDelta, Repetitions: "3*10/12"})
	if err != nil {
		t.Fatal(err)
	}
	if request.WeightKg != 72 || request.SetCount != 3 || request.RepsPerSet != 10 || request.MaxReps != 12 || !reflect.DeepEqual(request.SetReps, []int{10, 10, 10, 12}) {
		t.Fatalf("request=%+v", request)
	}
}

func TestParseCopyRepetitionsPreservesExplicitPairAndClassicNotation(t *testing.T) {
	pair, err := ParseCopyRepetitions("10/10")
	if err != nil || pair.SetCount != 2 || !reflect.DeepEqual(pair.Reps, []int{10, 10}) {
		t.Fatalf("pair=%+v err=%v", pair, err)
	}
	classic, err := ParseCopyRepetitions("3*10/12")
	if err != nil || classic.SetCount != 3 || !reflect.DeepEqual(classic.Reps, []int{10, 10, 10, 12}) {
		t.Fatalf("classic=%+v err=%v", classic, err)
	}
	if _, err := ParseCopyRepetitions("50 10/10"); err == nil {
		t.Fatal("repetition-only notation must reject an extra weight")
	}
}

func TestApplyCopyOptionsRejectsAmbiguousPerSetWeightChanges(t *testing.T) {
	weightDelta := 4.0
	for _, options := range []*CopyOptions{
		{WeightDeltaKg: &weightDelta},
		{Repetitions: "3*10/12"},
	} {
		_, err := ApplyCopyOptions(EntryRequest{WeightKg: 68, SetCount: 2, RepsPerSet: 10, MaxReps: 10, SetReps: []int{10, 10}, SetWeights: []int{65, 68}}, options)
		if err == nil {
			t.Fatalf("options=%+v unexpectedly accepted", options)
		}
	}
}

func TestApplyCopyOptionsScalarOverrideCanReplacePerSetWeightsAndRepetitions(t *testing.T) {
	weight := 72.0
	request, err := ApplyCopyOptions(EntryRequest{
		WeightKg: 68, SetCount: 2, RepsPerSet: 10, MaxReps: 10,
		SetReps: []int{10, 10}, SetWeights: []int{65, 68},
	}, &CopyOptions{WeightKg: &weight, Repetitions: "3*10/12"})
	if err != nil {
		t.Fatal(err)
	}
	if request.WeightKg != 72 || len(request.SetWeights) != 0 || request.SetCount != 3 || !reflect.DeepEqual(request.SetReps, []int{10, 10, 10, 12}) {
		t.Fatalf("request=%+v", request)
	}
}

func TestCopyPreviousEntryPreservesExactSetsAndIgnoresFuture(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_POSTGRES_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := NewService(pool)
	exercise, err := s.CreateExercise(ctx, CreateExerciseRequest{Name: "Copy regression fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM workout_entries WHERE exercise_id=$1::uuid", exercise.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM exercises WHERE id=$1::uuid", exercise.ID)
	}()
	for _, entry := range []EntryRequest{
		{ExerciseID: exercise.ID, PerformedOn: "2026-07-01", WeightKg: 55, SetReps: []int{8, 10}, SetWeights: []int{50, 55}},
		{ExerciseID: exercise.ID, PerformedOn: "2026-07-20", WeightKg: 99, SetReps: []int{8, 8}},
	} {
		if err := s.UpsertEntry(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}
	result, date, err := s.CopyPreviousEntry(ctx, exercise.ID, "2026-07-10")
	if err != nil || date != "2026-07-01" || result.WeightKg != 55 || len(result.SetWeights) != 2 || result.SetWeights[0] != 50 {
		t.Fatalf("result=%+v date=%s err=%v", result, date, err)
	}
	// Read authoritative storage through the existing service boundary.
	entries, err := s.entries(ctx, exercise.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 || entries[1].SetWeights != "50,55" || entries[1].SetReps != "8,10" {
		t.Fatalf("entries=%+v", entries)
	}
	if _, _, err := s.CopyPreviousEntry(ctx, exercise.ID, "2026-06-01"); err == nil {
		t.Fatal("missing source must not create a guessed row")
	}
}

func TestCopyPreviousEntryTypedSourceAndWeightOverride(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_POSTGRES_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	s := NewService(pool)
	exercise, err := s.CreateExercise(ctx, CreateExerciseRequest{Name: "Typed copy regression fixture"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM workout_entries WHERE exercise_id=$1::uuid", exercise.ID)
		_, _ = pool.Exec(ctx, "DELETE FROM exercises WHERE id=$1::uuid", exercise.ID)
	}()
	for _, entry := range []EntryRequest{
		{ExerciseID: exercise.ID, PerformedOn: "2026-07-01", WeightKg: 55, SetReps: []int{8, 10}, SetWeights: []int{50, 55}},
		{ExerciseID: exercise.ID, PerformedOn: "2026-07-20", WeightKg: 99, SetReps: []int{9, 9}},
		{ExerciseID: exercise.ID, PerformedOn: "2026-08-01", WeightKg: 70, SetReps: []int{6, 8}, SetWeights: []int{65, 70}},
	} {
		if err := s.UpsertEntry(ctx, entry); err != nil {
			t.Fatal(err)
		}
	}

	// Default selection ignores the future row and chooses July 1.
	request, sourceDate, err := s.CopyPreviousEntry(ctx, exercise.ID, "2026-07-10")
	if err != nil || sourceDate != "2026-07-01" || request.WeightKg != 55 || len(request.SetWeights) != 2 {
		t.Fatalf("default copy request=%+v sourceDate=%s err=%v", request, sourceDate, err)
	}

	// A scalar override retains exact reps and removes per-set weights.
	request, sourceDate, err = s.CopyPreviousEntry(ctx, exercise.ID, "2026-07-11", &CopyOptions{WeightKg: copyFloat(60)})
	if err != nil || sourceDate != "2026-07-10" || request.WeightKg != 60 || len(request.SetWeights) != 0 || len(request.SetReps) != 2 || request.SetReps[0] != 8 || request.SetReps[1] != 10 {
		t.Fatalf("override request=%+v sourceDate=%s err=%v", request, sourceDate, err)
	}

	// The target can be read before being upserted for a same-target correction.
	request, sourceDate, err = s.CopyPreviousEntry(ctx, exercise.ID, "2026-07-11", &CopyOptions{SourceDate: "2026-07-11", WeightKg: copyFloat(61)})
	if err != nil || sourceDate != "2026-07-11" || request.WeightKg != 61 || len(request.SetWeights) != 0 || len(request.SetReps) != 2 || request.SetReps[0] != 8 || request.SetReps[1] != 10 {
		t.Fatalf("same-target correction request=%+v sourceDate=%s err=%v", request, sourceDate, err)
	}

	// An explicitly selected source may be later than the target and keeps its varied weights.
	request, sourceDate, err = s.CopyPreviousEntry(ctx, exercise.ID, "2026-07-12", &CopyOptions{SourceDate: "2026-08-01"})
	if err != nil || sourceDate != "2026-08-01" || request.WeightKg != 70 || len(request.SetWeights) != 2 || request.SetWeights[0] != 65 || request.SetWeights[1] != 70 || len(request.SetReps) != 2 || request.SetReps[0] != 6 || request.SetReps[1] != 8 {
		t.Fatalf("explicit source request=%+v sourceDate=%s err=%v", request, sourceDate, err)
	}

	if _, _, err := s.CopyPreviousEntry(ctx, exercise.ID, "2026-07-13", &CopyOptions{SourceDate: "2026-01-01"}); err == nil {
		t.Fatal("missing explicit source must not create a guessed row")
	}
	invalid := math.NaN()
	if _, _, err := s.CopyPreviousEntry(ctx, exercise.ID, "2026-07-13", &CopyOptions{WeightKg: &invalid}); err == nil {
		t.Fatal("non-finite weight override must be rejected")
	}
}

func copyFloat(value float64) *float64 { return &value }
