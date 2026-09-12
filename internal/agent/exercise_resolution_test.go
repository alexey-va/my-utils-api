package agent

import (
	"errors"
	"strings"
	"testing"
)

func TestResolveExerciseExactNameWinsOverFuzzyCandidate(t *testing.T) {
	candidates := []ExerciseCandidate{
		{ID: "ordinary-id", Name: "Бабочка"},
		{ID: "rear-id", Name: "Бабочка на заднюю дельту"},
	}
	got, err := ResolveExercise("", "  БАБОЧКА  ", candidates)
	if err != nil || got.ID != "ordinary-id" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
	got, err = ResolveExercise("", "бабочка на заднюю", candidates)
	if err != nil || got.ID != "rear-id" {
		t.Fatalf("fuzzy got=%#v err=%v", got, err)
	}
}

func TestResolveExerciseUnknownIDNeverFallsThroughToName(t *testing.T) {
	candidates := []ExerciseCandidate{{ID: "ordinary-id", Name: "Бабочка"}}
	_, err := ResolveExercise("00000000-0000-0000-0000-000000000001", "Бабочка", candidates)
	var resolutionErr *ExerciseResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.Kind != ExerciseResolutionUnknownID {
		t.Fatalf("err=%v", err)
	}
	if strings.Contains(err.Error(), "успешно") {
		t.Fatalf("unknown ID was treated as a name match: %v", err)
	}
}

func TestResolveExerciseCanonicalIDWinsOverConflictingName(t *testing.T) {
	candidates := []ExerciseCandidate{
		{ID: "ordinary-id", Name: "Бабочка"},
		{ID: "rear-id", Name: "Бабочка на заднюю дельту"},
	}
	got, err := ResolveExercise("rear-id", "Бабочка", candidates)
	if err != nil || got.ID != "rear-id" {
		t.Fatalf("got=%#v err=%v", got, err)
	}
}

func TestResolveExerciseAmbiguityListsCanonicalIDsAndNames(t *testing.T) {
	candidates := []ExerciseCandidate{
		{ID: "upper-id", Name: "Тяга верхнего блока"},
		{ID: "lower-id", Name: "Тяга нижнего блока"},
	}
	_, err := ResolveExercise("", "тяга", candidates)
	var resolutionErr *ExerciseResolutionError
	if !errors.As(err, &resolutionErr) || resolutionErr.Kind != ExerciseResolutionAmbiguous {
		t.Fatalf("err=%v", err)
	}
	message := err.Error()
	for _, value := range []string{"upper-id", "Тяга верхнего блока", "lower-id", "Тяга нижнего блока"} {
		if !strings.Contains(message, value) {
			t.Fatalf("ambiguity error %q omits %q", message, value)
		}
	}
}

func TestResolveExerciseRejectsEmptyOrInvalidCandidates(t *testing.T) {
	if _, err := ResolveExercise("", "Бабочка", []ExerciseCandidate{{ID: "", Name: "Бабочка"}}); err == nil {
		t.Fatal("candidate without canonical ID resolved")
	}
	if _, err := ResolveExercise("missing", "Бабочка", []ExerciseCandidate{{ID: "ordinary-id", Name: "Бабочка"}}); err == nil {
		t.Fatal("unknown canonical ID was accepted")
	}
}
