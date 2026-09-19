package agent

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alexey-va/my-utils-api/internal/workout"
)

var (
	workoutEvidenceGroupPattern  = regexp.MustCompile(`(?:\d+\s*[*xх×]\s*)?\d+(?:\s*[/,]\s*\d+)+`)
	workoutEvidenceNumberPattern = regexp.MustCompile(`\d+(?:[.,]\d+)?`)
	workoutEvidenceDatePattern   = regexp.MustCompile(`(?:\d{4}[-/.]\d{1,2}[-/.]\d{1,2}|\d{1,2}[-/.]\d{1,2}[-/.]\d{4})`)
)

type workoutEvidenceGroup struct {
	raw           string
	start, end    int
	parsed        workout.ParsedNotation
	decimalWeight bool
	weightOnly    bool
}

type workoutEvidenceNumber struct {
	raw        string
	start, end int
}

type workoutEvidenceValue struct {
	reps       []int
	weights    []int
	weightKg   float64
	hasWeight  bool
	weightFrom string
}

func validateWorkoutEvidence(action plannedAction, userTexts []string) error {
	if NormalizeToolName(action.Tool) == "copy_workout" {
		return validateCopyWeightEvidence(action, userTexts)
	}
	if NormalizeToolName(action.Tool) != "log_workout" {
		return nil
	}
	quote := strings.TrimSpace(action.DataQuote)
	if quote == "" {
		return fmt.Errorf("log_workout: data_quote is required")
	}
	found := false
	for _, text := range userTexts {
		if strings.Contains(text, quote) {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("log_workout: data_quote must be an exact user-text substring")
	}
	rawNotation, ok := action.Arguments["notation"].(string)
	if !ok || strings.TrimSpace(rawNotation) == "" {
		return fmt.Errorf("log_workout: notation is required")
	}
	expected, err := workout.ParseNotation(rawNotation)
	if err != nil {
		return fmt.Errorf("log_workout: invalid notation: %w", err)
	}

	groups := parseWorkoutEvidenceGroups(quote)
	values := make([]workoutEvidenceValue, 0, len(groups))
	for index, group := range groups {
		if group.weightOnly || group.decimalWeight {
			continue
		}
		if value, ok := workoutEvidenceValueForGroup(quote, groups, index); ok {
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return fmt.Errorf("log_workout: data_quote has no parseable repetition result")
	}
	if len(values) > 1 {
		return fmt.Errorf("log_workout: data_quote must contain one workout result")
	}
	requestWeights := workoutEvidenceWeights(action.RequestQuote)
	if err := matchWorkoutEvidence(expected, values[0], requestWeights); err != nil {
		return fmt.Errorf("log_workout: %w", err)
	}
	return nil
}

// A copy gets its repetitions from storage. Only an explicit new weight needs
// literal evidence; it must not require the user to repeat stored repetitions.
func validateCopyWeightEvidence(action plannedAction, userTexts []string) error {
	weight, present := action.Arguments["weight_kg"]
	if !present {
		return nil
	}
	quote := strings.TrimSpace(action.DataQuote)
	if quote == "" {
		quote = strings.TrimSpace(action.RequestQuote)
	}
	found := false
	for _, text := range userTexts {
		found = found || (quote != "" && strings.Contains(text, quote))
	}
	values := workoutEvidenceWeights(quote)
	if !found || len(values) != 1 || values[0].weightKg != weight {
		return fmt.Errorf("copy_workout: weight_kg должен совпадать с единственным весом в дословном data_quote пользователя")
	}
	return nil
}

func parseWorkoutEvidenceGroups(text string) []workoutEvidenceGroup {
	matches := workoutEvidenceGroupPattern.FindAllStringIndex(text, -1)
	groups := make([]workoutEvidenceGroup, 0, len(matches))
	for _, match := range matches {
		raw := text[match[0]:match[1]]
		parsed, err := workout.ParseNotation("1 " + raw)
		if err != nil {
			continue
		}
		groups = append(groups, workoutEvidenceGroup{raw: raw, start: match[0], end: match[1], parsed: parsed})
	}
	for index := range groups {
		group := &groups[index]
		trimmed := strings.TrimSpace(group.raw)
		if strings.Contains(trimmed, "/") || strings.Count(trimmed, ",") != 1 || strings.ContainsAny(trimmed, "*xх×") || index+1 >= len(groups) {
			continue
		}
		group.decimalWeight = true
	}
	for index := 1; index < len(groups); index++ {
		previous := &groups[index-1]
		if previous.decimalWeight || !strings.Contains(previous.raw, "/") {
			continue
		}
		if evidenceWeightGap(text[previous.end:groups[index].start]) {
			previous.weightOnly = true
		}
	}
	return groups
}

func workoutEvidenceValueForGroup(text string, groups []workoutEvidenceGroup, index int) (workoutEvidenceValue, bool) {
	group := groups[index]
	value := workoutEvidenceValue{reps: append([]int(nil), group.parsed.Reps...)}
	if index > 0 && groups[index-1].weightOnly {
		weightGroup := groups[index-1]
		parsed, err := workout.ParseNotation(weightGroup.raw + " 1/1")
		combined, combinedErr := workout.ParseNotation(weightGroup.raw + " " + group.raw)
		if err == nil && combinedErr == nil && len(parsed.Weights) > 0 {
			value.reps = append([]int(nil), combined.Reps...)
			weights := append([]int(nil), parsed.Weights...)
			unit := evidenceUnitAt(text, weightGroup.end)
			if isPoundUnit(unit) {
				for index := range weights {
					weights[index] = int(math.Round(float64(weights[index]) * 0.45359237))
				}
			}
			value.weights = weights
			value.weightKg = float64(slices.Max(weights))
			value.hasWeight = true
			value.weightFrom = weightGroup.raw
			return value, true
		}
	}
	if weight, ok := nearestWorkoutEvidenceWeight(text, groups, index); ok {
		value.weightKg = weight.weightKg
		value.hasWeight = true
		value.weightFrom = weight.weightFrom
	}
	return value, true
}

func nearestWorkoutEvidenceWeight(text string, groups []workoutEvidenceGroup, groupIndex int) (workoutEvidenceValue, bool) {
	numbers := workoutEvidenceNumbers(text, groups)
	group := groups[groupIndex]
	var nearest workoutEvidenceValue
	distance := -1
	for _, number := range numbers {
		currentDistance := 0
		if number.end <= group.start {
			currentDistance = group.start - number.end
		} else if number.start >= group.end {
			currentDistance = number.start - group.end
		} else {
			continue
		}
		weight, err := workoutEvidenceWeight(number.raw, evidenceUnitAt(text, number.end))
		if err != nil {
			continue
		}
		if distance >= 0 && currentDistance == distance {
			return workoutEvidenceValue{}, false
		}
		if distance < 0 || currentDistance < distance {
			distance = currentDistance
			nearest = workoutEvidenceValue{weightKg: weight.weightKg, hasWeight: true, weightFrom: weight.weightFrom}
		}
	}
	return nearest, distance >= 0
}

func workoutEvidenceNumbers(text string, groups []workoutEvidenceGroup) []workoutEvidenceNumber {
	dates := workoutEvidenceDatePattern.FindAllStringIndex(text, -1)
	numbers := workoutEvidenceNumberPattern.FindAllStringIndex(text, -1)
	result := make([]workoutEvidenceNumber, 0, len(numbers))
	for _, match := range numbers {
		if evidenceSpanInAny(match, dates) || evidenceSpanInRegularGroup(match, groups) || evidenceSetDescriptorNumber(text, match) {
			continue
		}
		result = append(result, workoutEvidenceNumber{raw: text[match[0]:match[1]], start: match[0], end: match[1]})
	}
	return result
}

func evidenceSetDescriptorNumber(text string, span []int) bool {
	if evidenceUnitAt(text, span[1]) != "" {
		return false
	}
	before := strings.ToLower(strings.TrimSpace(text[:span[0]]))
	after := strings.ToLower(strings.TrimSpace(text[span[1]:]))
	for _, prefix := range []string{"подход", "сет", "повтор"} {
		if strings.HasPrefix(after, prefix) {
			return true
		}
	}
	if strings.HasSuffix(before, "по") {
		words := strings.Fields(strings.TrimSpace(strings.TrimSuffix(before, "по")))
		for index := len(words) - 1; index >= 0 && index >= len(words)-3; index-- {
			word := strings.Trim(words[index], " ,.:;!?()[]{}")
			for _, prefix := range []string{"подход", "сет", "повтор"} {
				if strings.HasPrefix(word, prefix) {
					return true
				}
			}
		}
	}
	return false
}

func evidenceSpanInAny(span []int, ranges [][]int) bool {
	for _, candidate := range ranges {
		if span[0] >= candidate[0] && span[1] <= candidate[1] {
			return true
		}
	}
	return false
}

func evidenceSpanInRegularGroup(span []int, groups []workoutEvidenceGroup) bool {
	for _, group := range groups {
		if !group.decimalWeight && span[0] >= group.start && span[1] <= group.end {
			return true
		}
	}
	return false
}

func workoutEvidenceWeights(text string) []workoutEvidenceValue {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	groups := parseWorkoutEvidenceGroups(text)
	if len(groups) == 1 && strings.Contains(groups[0].raw, ",") && !strings.Contains(groups[0].raw, "/") && !strings.ContainsAny(groups[0].raw, "*xх×") {
		weight, err := workoutEvidenceWeight(groups[0].raw, evidenceUnitAt(text, groups[0].end))
		if err == nil {
			return []workoutEvidenceValue{weight}
		}
	}
	if len(groups) == 0 {
		result := make([]workoutEvidenceValue, 0)
		for _, number := range workoutEvidenceNumbers(text, nil) {
			weight, err := workoutEvidenceWeight(number.raw, evidenceUnitAt(text, number.end))
			if err == nil {
				result = append(result, workoutEvidenceValue{weightKg: weight.weightKg, hasWeight: true, weightFrom: weight.weightFrom})
			}
		}
		return result
	}
	result := make([]workoutEvidenceValue, 0, len(groups))
	seen := map[string]bool{}
	for index, group := range groups {
		if group.weightOnly || group.decimalWeight {
			continue
		}
		weight, ok := nearestWorkoutEvidenceWeight(text, groups, index)
		if ok && !seen[weight.weightFrom] {
			seen[weight.weightFrom] = true
			result = append(result, weight)
		}
	}
	return result
}

func workoutEvidenceWeight(raw, unit string) (workoutEvidenceValue, error) {
	parsed, err := workout.ParseNotation(raw + " 1/1")
	if err != nil {
		return workoutEvidenceValue{}, err
	}
	weight := parsed.WeightKg
	if isPoundUnit(unit) {
		weight = math.Round(weight * 0.45359237)
	}
	return workoutEvidenceValue{weightKg: weight, hasWeight: true, weightFrom: raw}, nil
}

func matchWorkoutEvidence(expected workout.ParsedNotation, actual workoutEvidenceValue, requestWeights []workoutEvidenceValue) error {
	if !workoutEvidenceRepsMatch(expected, actual.reps) {
		return fmt.Errorf("repetition data does not match data_quote")
	}
	if len(expected.Weights) > 0 {
		if !actual.hasWeight || !slices.Equal(expected.Weights, actual.weights) {
			return fmt.Errorf("per-set weights do not match data_quote")
		}
		return nil
	}
	if len(requestWeights) > 1 {
		return fmt.Errorf("request_quote contains multiple possible weights")
	}
	if len(requestWeights) == 1 {
		if requestWeights[0].weightKg != expected.WeightKg {
			return fmt.Errorf("scalar weight does not match request_quote")
		}
		if len(actual.weights) > 0 {
			return fmt.Errorf("data_quote contains per-set weights")
		}
		return nil
	}
	if !actual.hasWeight || len(actual.weights) > 0 || actual.weightKg != expected.WeightKg {
		return fmt.Errorf("scalar weight does not match data_quote")
	}
	return nil
}

func workoutEvidenceRepsMatch(expected workout.ParsedNotation, actual []int) bool {
	if slices.Equal(expected.Reps, actual) {
		return true
	}
	if expected.SetCount != 3 || len(expected.Reps) != 4 || len(actual) != 2 {
		return false
	}
	return expected.Reps[0] == actual[0] &&
		expected.Reps[1] == actual[0] &&
		expected.Reps[2] == actual[0] &&
		expected.Reps[3] == actual[1]
}

func evidenceWeightGap(raw string) bool {
	trimmed := strings.ToLower(strings.TrimSpace(raw))
	if trimmed == "" {
		return true
	}
	for _, unit := range []string{"кг", "kg", "lb", "lbs", "фунт", "фунта", "фунтов"} {
		if trimmed == unit {
			return true
		}
	}
	return false
}

func evidenceUnitAt(text string, end int) string {
	rest := strings.TrimLeftFunc(text[end:], unicode.IsSpace)
	lower := strings.ToLower(rest)
	for _, unit := range []string{"фунтов", "фунта", "фунт", "lbs", "lb", "кг", "kg"} {
		if !strings.HasPrefix(lower, unit) {
			continue
		}
		if len(lower) == len(unit) {
			return unit
		}
		next, _ := utf8.DecodeRuneInString(lower[len(unit):])
		if !unicode.IsLetter(next) {
			return unit
		}
	}
	return ""
}

func isPoundUnit(unit string) bool {
	return unit == "lb" || unit == "lbs" || strings.HasPrefix(unit, "фунт")
}
