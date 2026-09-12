package workout

import (
	"context"
	"fmt"
	"math"
	"time"
)

// CopyOptions selects an optional source row and/or replaces its scalar
// weight. Reps always come from the selected source row.
type CopyOptions struct {
	SourceDate string   `json:"source_date,omitempty"`
	WeightKg   *float64 `json:"weight_kg,omitempty"`
}

// CopyPreviousEntry resolves "same as last time" against the journal, never
// against a model-generated weight. Future and target-day rows are excluded
// unless an explicit source date is supplied.
func (s *Service) CopyPreviousEntry(ctx context.Context, exerciseID, date string, options ...*CopyOptions) (EntryRequest, string, error) {
	targetDate, err := parseCopyDate(date, "performedOn")
	if err != nil {
		return EntryRequest{}, "", err
	}
	option, err := oneCopyOptions(options)
	if err != nil {
		return EntryRequest{}, "", err
	}
	var sourceDate time.Time
	if option != nil && option.SourceDate != "" {
		sourceDate, err = parseCopyDate(option.SourceDate, "sourceDate")
		if err != nil {
			return EntryRequest{}, "", err
		}
	}
	if option != nil && option.WeightKg != nil {
		if err := validateCopyWeight(*option.WeightKg); err != nil {
			return EntryRequest{}, "", err
		}
	}

	entries, err := s.entries(ctx, exerciseID, false)
	if err != nil {
		return EntryRequest{}, "", err
	}
	var source entry
	found := false
	for _, entry := range entries {
		if option != nil && option.SourceDate != "" {
			if entry.Date != sourceDate.Format(time.DateOnly) {
				continue
			}
		} else if entry.Date >= targetDate.Format(time.DateOnly) {
			continue
		}
		source = entry
		found = true
		break
	}
	if !found {
		if option != nil && option.SourceDate != "" {
			return EntryRequest{}, "", notFound(fmt.Sprintf("Нет записи на %s; нужны вес и подходы.", option.SourceDate))
		}
		return EntryRequest{}, "", notFound(fmt.Sprintf("Нет предыдущей записи до %s; нужны вес и подходы.", date))
	}

	weight := source.Weight
	setWeights := ParseStorage(source.SetWeights)
	if option != nil && option.WeightKg != nil {
		weight = *option.WeightKg
		// An override supplies one scalar weight for every retained repetition.
		setWeights = nil
	}
	request := EntryRequest{ExerciseID: exerciseID, PerformedOn: date, WeightKg: weight,
		SetCount: source.SetCount, RepsPerSet: source.RepsPerSet, MaxReps: source.MaxReps,
		SetReps: source.reps(), SetWeights: setWeights}
	return request, source.Date, s.UpsertEntry(ctx, request)
}

func oneCopyOptions(options []*CopyOptions) (*CopyOptions, error) {
	if len(options) > 1 {
		return nil, badRequest("only one copy options value is allowed")
	}
	if len(options) == 0 {
		return nil, nil
	}
	return options[0], nil
}

func parseCopyDate(raw, field string) (time.Time, error) {
	parsed, err := time.Parse(time.DateOnly, raw)
	if err != nil {
		return time.Time{}, badRequest(fmt.Sprintf("%s must be YYYY-MM-DD", field))
	}
	return parsed, nil
}

func validateCopyWeight(weight float64) error {
	if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < minNotationWeight || weight > maxNotationWeight {
		return badRequest("weightKg must be finite and between 0.25 and 10000")
	}
	return nil
}
