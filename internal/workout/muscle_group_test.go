package workout

import "testing"

func TestNormalizeMuscleGroupPreservesCanonicalValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"arms", "back", "chest", "core", "legs", "other", "shoulders"} {
		value := value
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			if got := NormalizeMuscleGroup(value); got != value {
				t.Fatalf("NormalizeMuscleGroup(%q) = %q", value, got)
			}
		})
	}
	if got := NormalizeMuscleGroup("  SHOULDERS "); got != "shoulders" {
		t.Fatalf("NormalizeMuscleGroup mixed case = %q", got)
	}
	if got := NormalizeMuscleGroup("плечи"); got != "other" {
		t.Fatalf("NormalizeMuscleGroup unknown = %q", got)
	}
}
