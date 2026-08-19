package health

import (
	"encoding/json"
	"testing"
	"time"
)

func TestParseStepsUsesLastNonBlankLineAsToday(t *testing.T) {
	t.Parallel()

	today := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	parsed, err := ParseSteps(json.RawMessage(`{"":"1000\n\n2000\n3000"}`), today)
	if err != nil {
		t.Fatalf("ParseSteps() error = %v", err)
	}
	if parsed == nil || len(parsed.Days) != 3 {
		t.Fatalf("parsed = %#v", parsed)
	}
	if parsed.Days[0].Date != "2026-08-18" || parsed.Days[2].Date != "2026-08-20" || parsed.Days[2].Steps != 3000 {
		t.Fatalf("days = %#v", parsed.Days)
	}
}

func TestParseStepsDistinguishesMalformedFromUnknownJSON(t *testing.T) {
	t.Parallel()

	today := time.Date(2026, 7, 14, 0, 0, 0, 0, time.UTC)
	if _, err := ParseSteps(json.RawMessage(`{"steps":`), today); err == nil {
		t.Fatal("malformed JSON must fail")
	}
	parsed, err := ParseSteps(json.RawMessage(`[1,2,3]`), today)
	if err != nil || parsed != nil {
		t.Fatalf("valid unknown JSON = %#v, %v", parsed, err)
	}
}

func TestParseWeightKeepsBlankAndZeroLinesAsMissingCalendarDays(t *testing.T) {
	t.Parallel()

	today := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	parsed, err := ParseWeightImport(json.RawMessage(`{"":"80.1\n\n0\n79,8"}`), today)
	if err != nil {
		t.Fatalf("ParseWeightImport() error = %v", err)
	}
	if parsed == nil || parsed.ReceivedDays != 4 || len(parsed.Days) != 2 {
		t.Fatalf("parsed = %#v", parsed)
	}
	if parsed.Days[0].Date != "2026-08-17" || parsed.Days[1].Date != "2026-08-20" || parsed.Days[1].WeightKg != 79.8 {
		t.Fatalf("days = %#v", parsed.Days)
	}
}
