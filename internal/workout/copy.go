package workout

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"
	"unicode"
)

// CopyOptions selects an optional source row and/or changes its scalar weight
// and repetitions.
type CopyOptions struct {
	SourceDate    string   `json:"source_date,omitempty"`
	WeightKg      *float64 `json:"weight_kg,omitempty"`
	WeightDeltaKg *float64 `json:"weight_delta_kg,omitempty"`
	Repetitions   string   `json:"repetitions,omitempty"`
}

// ApplyCopyOptions applies the user-requested changes to a copied entry. It
// does not persist the result; callers use the same transformation for the
// real journal and the sandbox fixture.
func ApplyCopyOptions(source EntryRequest, options *CopyOptions) (EntryRequest, error) {
	result := source
	result.SetReps = append([]int(nil), source.SetReps...)
	result.SetWeights = append([]int(nil), source.SetWeights...)
	if options == nil {
		return result, nil
	}
	if options.WeightKg != nil && options.WeightDeltaKg != nil {
		return EntryRequest{}, badRequest("weight_kg и weight_delta_kg нельзя указывать одновременно")
	}
	if options.WeightDeltaKg != nil {
		if len(source.SetWeights) > 0 {
			return EntryRequest{}, errors.New("нельзя применить weight_delta_kg к записи с разными весами по подходам")
		}
		if err := validateCopyDelta(*options.WeightDeltaKg); err != nil {
			return EntryRequest{}, err
		}
		result.WeightKg += *options.WeightDeltaKg
		if err := validateCopyWeight(result.WeightKg); err != nil {
			return EntryRequest{}, err
		}
	}
	if options.WeightKg != nil {
		if err := validateCopyWeight(*options.WeightKg); err != nil {
			return EntryRequest{}, err
		}
		result.WeightKg = *options.WeightKg
		// A scalar override intentionally replaces any per-set source weights.
		result.SetWeights = nil
	}
	if strings.TrimSpace(options.Repetitions) != "" {
		if len(source.SetWeights) > 0 && options.WeightKg == nil {
			return EntryRequest{}, errors.New("нельзя заменить подходы у записи с разными весами по подходам")
		}
		parsed, err := ParseCopyRepetitions(options.Repetitions)
		if err != nil {
			return EntryRequest{}, err
		}
		result.SetReps = append([]int(nil), parsed.Reps...)
		result.SetCount = parsed.SetCount
		result.RepsPerSet = parsed.RepsPerSet
		result.MaxReps = parsed.MaxReps
	}
	return result, nil
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

	request := EntryRequest{ExerciseID: exerciseID, PerformedOn: date, WeightKg: source.Weight,
		SetCount: source.SetCount, RepsPerSet: source.RepsPerSet, MaxReps: source.MaxReps,
		SetReps: source.reps(), SetWeights: ParseStorage(source.SetWeights)}
	request, err = ApplyCopyOptions(request, option)
	if err != nil {
		return EntryRequest{}, "", err
	}
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

func validateCopyDelta(delta float64) error {
	if math.IsNaN(delta) || math.IsInf(delta, 0) || math.Abs(delta) > maxNotationWeight {
		return badRequest("weight_delta_kg должен быть конечным числом не больше 10000 кг по модулю")
	}
	return nil
}

// ParseCopyRepetitions parses repetition-only notation using the diary's
// existing notation semantics. A weight is deliberately not accepted here.
func ParseCopyRepetitions(raw string) (ParsedNotation, error) {
	notation := strings.TrimSpace(raw)
	if notation == "" || !strings.ContainsAny(notation, "/,") {
		return ParsedNotation{}, badRequest("repetitions должны быть в формате 10/10, 3*10/12 или 8/8/8")
	}
	if strings.IndexFunc(notation, unicode.IsSpace) >= 0 {
		return ParsedNotation{}, badRequest("repetitions не должны содержать пробелы или вес")
	}
	parsed, err := ParseNotation("1 " + notation)
	if err != nil {
		return ParsedNotation{}, badRequest("некорректные repetitions: " + err.Error())
	}
	return parsed, nil
}
