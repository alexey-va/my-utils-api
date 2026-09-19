package workout

import (
	"reflect"
	"testing"
)

func TestNormalizeLegacyTrainerNotation(t *testing.T) {
	t.Parallel()

	normalized, err := NormalizeSets(3, 10, 12, nil, nil)
	if err != nil {
		t.Fatalf("NormalizeSets() error = %v", err)
	}
	if want := []int{10, 10, 10, 12}; !reflect.DeepEqual(normalized.Reps, want) {
		t.Fatalf("reps = %#v, want %#v", normalized.Reps, want)
	}
	if normalized.SetCount != 3 || normalized.RepsPerSet != 10 || normalized.MaxReps != 12 || normalized.RepsStorage != "10,10,10,12" {
		t.Fatalf("normalized = %#v", normalized)
	}
	if got := Display(70, normalized.Reps, nil); got != "70  3×10  (12)" {
		t.Errorf("Display() = %q", got)
	}
}

func TestNormalizeExplicitSetsAndPerSetWeights(t *testing.T) {
	t.Parallel()

	normalized, err := NormalizeSets(1, 1, 1, []int{8, 10, 12}, []int{70, 75, 80})
	if err != nil {
		t.Fatalf("NormalizeSets() error = %v", err)
	}
	if normalized.SetCount != 3 || normalized.RepsPerSet != 8 || normalized.MaxReps != 12 || normalized.WeightsStorage != "70,75,80" {
		t.Fatalf("normalized = %#v", normalized)
	}
	if got := Display(70, normalized.Reps, normalized.Weights); got != "70/75/80  8/10/12" {
		t.Errorf("Display() = %q", got)
	}
}

func TestDisplayShowsEqualFourthSetAsMax(t *testing.T) {
	if got := Display(86, []int{10, 10, 10, 10}, nil, 3); got != "86  3×10  (10)" {
		t.Fatalf("Display() = %q, want %q", got, "86  3×10  (10)")
	}
}

func TestDisplayKeepsExplicitFourEqualSets(t *testing.T) {
	if got := Display(86, []int{10, 10, 10, 10}, nil, 4); got != "86  10/10/10/10" {
		t.Fatalf("Display() = %q, want explicit four-set list", got)
	}
}

func TestNormalizeClassicNotationPreservesWorkingSetCount(t *testing.T) {
	normalized, err := NormalizeSets(3, 10, 10, []int{10, 10, 10, 10}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if normalized.SetCount != 3 {
		t.Fatalf("SetCount = %d, want 3", normalized.SetCount)
	}
}

func TestNormalizePerSetWeightsRemainExplicitSets(t *testing.T) {
	normalized, err := NormalizeSets(3, 10, 10, []int{10, 10, 10, 10}, []int{80, 82, 84, 86})
	if err != nil {
		t.Fatal(err)
	}
	if normalized.SetCount != 4 {
		t.Fatalf("SetCount = %d, want 4 explicit weighted sets", normalized.SetCount)
	}
}
