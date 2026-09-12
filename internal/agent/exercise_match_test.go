package agent

import "testing"

func TestExerciseNameMatchDoesNotCollapseLongerRequestedName(t *testing.T) {
	candidates := []ExerciseCandidate{{ID: "ordinary", Name: "Бабочка"}}
	if _, err := ResolveExercise("", "Бабочка на заднюю дельту", candidates); err == nil {
		t.Fatal("a longer, more specific name must not match a shorter existing exercise")
	}
}
