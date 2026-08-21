package agent

import "testing"

func TestExerciseNameMatchDoesNotCollapseLongerRequestedName(t *testing.T) {
	t.Parallel()
	if exerciseNameMatches("Бабочка", "Бабочка на заднюю дельту") {
		t.Fatal("a longer, more specific requested name must not match a shorter existing exercise")
	}
	if !exerciseNameMatches("Бабочка на заднюю дельту", "бабочка на заднюю") {
		t.Fatal("an unambiguous shorter user phrase should still match a canonical exercise name")
	}
}
