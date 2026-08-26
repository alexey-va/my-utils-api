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

func TestExactExerciseNameWinsOverLongerFuzzyCandidate(t *testing.T) {
	t.Parallel()
	matches := bestExerciseMatchIndexes(
		[]string{"Бабочка", "Бабочка на заднюю дельту"},
		"Бабочка",
	)
	if len(matches) != 1 || matches[0] != 0 {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestMultipleFuzzyExerciseNamesRemainAmbiguous(t *testing.T) {
	t.Parallel()
	matches := bestExerciseMatchIndexes(
		[]string{"Тяга верхнего блока", "Тяга нижнего блока"},
		"Тяга",
	)
	if len(matches) != 2 {
		t.Fatalf("matches = %#v", matches)
	}
}
