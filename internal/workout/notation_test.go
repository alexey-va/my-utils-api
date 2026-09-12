package workout

import (
	"reflect"
	"testing"
)

func TestParseNotation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		raw     string
		weight  float64
		weights []int
		reps    []int
	}{
		{"70 3*10/12", 70, nil, []int{10, 10, 10, 12}},
		{"70 10/12", 70, nil, []int{10, 10, 10, 12}},
		{"70 10/10", 70, nil, []int{10, 10}},
		{"22.5 10/10", 22.5, nil, []int{10, 10}},
		{"22,5 10/10", 22.5, nil, []int{10, 10}},
		{"70 7/7/7", 70, nil, []int{7, 7, 7}},
		{"70/75/80 10/10/10", 80, []int{70, 75, 80}, []int{10, 10, 10}},
		{"70/75 10/12", 75, []int{70, 75}, []int{10, 12}},
	}
	for _, test := range tests {
		test := test
		t.Run(test.raw, func(t *testing.T) {
			parsed, err := ParseNotation(test.raw)
			if err != nil {
				t.Fatal(err)
			}
			if parsed.WeightKg != test.weight || !reflect.DeepEqual(parsed.Weights, test.weights) || !reflect.DeepEqual(parsed.Reps, test.reps) {
				t.Fatalf("parsed = %#v", parsed)
			}
		})
	}
}

func TestParseNotationRejectsMismatchedWeights(t *testing.T) {
	t.Parallel()
	if _, err := ParseNotation("70/75 10/10/10"); err == nil {
		t.Fatal("expected error")
	}
}

func TestParseNotationRejectsUnboundedValues(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{
		"70 1000000000*10/10",
		"1000000000 1/1",
		"70 1001/1",
	} {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, err := ParseNotation(raw); err == nil {
				t.Fatal("expected bounded notation error")
			}
		})
	}
}
