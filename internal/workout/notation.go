package workout

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type ParsedNotation struct {
	WeightKg   float64
	Weights    []int
	Reps       []int
	SetCount   int
	RepsPerSet int
	MaxReps    int
}

var classicNotation = regexp.MustCompile(`^(\d+(?:[.,]\d+)?)\s+(\d+)\s*[*xх×]\s*(\d+)/(\d+)$`)

const (
	maxNotationSets   = 100
	maxNotationReps   = 1000
	maxNotationWeight = 10000
	minNotationWeight = 0.25
)

func ParseNotation(raw string) (ParsedNotation, error) {
	notation := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if notation == "" {
		return ParsedNotation{}, fmt.Errorf("Пустая notation")
	}
	if match := classicNotation.FindStringSubmatch(notation); match != nil {
		weight, err := parseNotationWeight(match[1])
		if err != nil {
			return ParsedNotation{}, err
		}
		working, err := parseNotationInt(match[2], maxNotationSets)
		if err != nil {
			return ParsedNotation{}, err
		}
		reps, err := parseNotationInt(match[3], maxNotationReps)
		if err != nil {
			return ParsedNotation{}, err
		}
		maximum, err := parseNotationInt(match[4], maxNotationReps)
		if err != nil {
			return ParsedNotation{}, err
		}
		sets := append(make([]int, working), maximum)
		for index := 0; index < working; index++ {
			sets[index] = reps
		}
		return notationResult(weight, nil, sets), nil
	}
	parts := strings.Fields(notation)
	if len(parts) != 2 {
		return ParsedNotation{}, fmt.Errorf("Не понял notation %q", raw)
	}
	var (
		weight  float64
		weights []int
		err     error
	)
	if strings.Contains(parts[0], "/") {
		weights, err = parseNotationNumbers(parts[0], true)
		if err != nil {
			return ParsedNotation{}, err
		}
		weight = float64(slices.Max(weights))
	} else {
		weight, err = parseNotationWeight(parts[0])
		if err != nil {
			return ParsedNotation{}, err
		}
	}
	right, err := parseNotationNumbers(parts[1], false)
	if err != nil {
		return ParsedNotation{}, err
	}
	if len(weights) == 0 && len(right) == 2 && right[0] != right[1] {
		right = []int{right[0], right[0], right[0], right[1]}
	}
	if len(weights) == 0 {
		return notationResult(weight, nil, right), nil
	}
	if len(weights) != len(right) {
		return ParsedNotation{}, fmt.Errorf("Число весов (%d) должно совпадать с числом подходов (%d)", len(weights), len(right))
	}
	return notationResult(weight, weights, right), nil
}

func parseNotationNumbers(raw string, allowDot bool) ([]int, error) {
	separator := regexp.MustCompile(`[/,]`)
	if allowDot {
		separator = regexp.MustCompile(`[/]`)
	}
	separatorCount := strings.Count(raw, "/")
	if !allowDot {
		separatorCount += strings.Count(raw, ",")
	}
	if separatorCount+1 > maxNotationSets {
		return nil, fmt.Errorf("Слишком много значений notation")
	}
	parts := separator.Split(raw, -1)
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		maximum := maxNotationReps
		if allowDot {
			maximum = maxNotationWeight
		}
		value, err := parseNotationInt(part, maximum)
		if err != nil {
			return nil, fmt.Errorf("Некорректное число: %q", part)
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Список чисел пуст")
	}
	return result, nil
}

func parseNotationInt(raw string, maximum int) (int, error) {
	value, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || value < 1 || value > maximum {
		return 0, fmt.Errorf("Некорректное число: %q", raw)
	}
	return value, nil
}

func parseNotationWeight(raw string) (float64, error) {
	value, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(raw), ",", "."), 64)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < minNotationWeight || value > maxNotationWeight {
		return 0, fmt.Errorf("Некорректный вес: %q", raw)
	}
	return value, nil
}

func notationResult(weight float64, weights, reps []int) ParsedNotation {
	return ParsedNotation{
		WeightKg: weight, Weights: append([]int(nil), weights...), Reps: append([]int(nil), reps...),
		SetCount: len(reps), RepsPerSet: slices.Min(reps), MaxReps: slices.Max(reps),
	}
}
