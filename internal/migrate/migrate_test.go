package migrate

import (
	"testing"

	migrations "github.com/alexey-va/my-utils-api/src/main/resources/db/migration"
)

func TestDiscoverOrdersFlywayVersionsNumerically(t *testing.T) {
	t.Parallel()

	discovered, err := Discover(migrations.FS)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(discovered) != 30 {
		t.Fatalf("migration count = %d, want 30", len(discovered))
	}
	for index, migration := range discovered {
		want := index + 1
		if migration.Version != want {
			t.Fatalf("migration[%d].Version = %d, want %d", index, migration.Version, want)
		}
	}
	if discovered[9].Script != "V10__workout_set_reps.sql" || discovered[9].Description != "workout set reps" {
		t.Fatalf("V10 metadata = %#v", discovered[9])
	}
}

func TestChecksumMatchesFlywayLineNormalization(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"first\nsecond\n", "first\r\nsecond\r\n", "first\nsecond"} {
		if got := Checksum([]byte(source)); got != 493157375 {
			t.Errorf("Checksum(%q) = %d, want JVM CRC32 493157375", source, got)
		}
	}
}
