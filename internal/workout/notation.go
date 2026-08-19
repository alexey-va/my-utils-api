package workout

import (
	"fmt"
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

func ParseNotation(raw string) (ParsedNotation, error) {
	notation := strings.Join(strings.Fields(strings.TrimSpace(raw)), " ")
	if notation == "" {
		return ParsedNotation{}, fmt.Errorf("Пустая notation")
	}
	if match := classicNotation.FindStringSubmatch(notation); match != nil {
		weight, _ := strconv.ParseFloat(strings.ReplaceAll(match[1], ",", "."), 64)
		working, _ := strconv.Atoi(match[2])
		reps, _ := strconv.Atoi(match[3])
		maximum, _ := strconv.Atoi(match[4])
		if working < 1 || reps < 1 || maximum < 1 || weight <= 0 {
			return ParsedNotation{}, fmt.Errorf("Все значения notation должны быть положительными")
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
	left, err := parseNotationNumbers(parts[0], true)
	if err != nil {
		return ParsedNotation{}, err
	}
	right, err := parseNotationNumbers(parts[1], false)
	if err != nil {
		return ParsedNotation{}, err
	}
	if len(right) == 2 && right[0] != right[1] {
		right = []int{right[0], right[0], right[0], right[1]}
	}
	if len(left) == 1 {
		return notationResult(float64(left[0]), nil, right), nil
	}
	if len(left) != len(right) {
		return ParsedNotation{}, fmt.Errorf("Число весов (%d) должно совпадать с числом подходов (%d)", len(left), len(right))
	}
	return notationResult(float64(slices.Max(left)), left, right), nil
}

func parseNotationNumbers(raw string, allowDot bool) ([]int, error) {
	separator := regexp.MustCompile(`[/,]`)
	if allowDot {
		separator = regexp.MustCompile(`[/]`)
	}
	parts := separator.Split(raw, -1)
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || value < 1 {
			return nil, fmt.Errorf("Некорректное число: %q", part)
		}
		result = append(result, value)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("Список чисел пуст")
	}
	return result, nil
}

func notationResult(weight float64, weights, reps []int) ParsedNotation {
	return ParsedNotation{
		WeightKg: weight, Weights: append([]int(nil), weights...), Reps: append([]int(nil), reps...),
		SetCount: len(reps), RepsPerSet: slices.Min(reps), MaxReps: slices.Max(reps),
	}
}
