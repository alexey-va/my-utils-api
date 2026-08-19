package workout

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type NormalizedSets struct {
	RepsStorage    string
	WeightsStorage string
	SetCount       int
	RepsPerSet     int
	MaxReps        int
	Reps           []int
	Weights        []int
}

func NormalizeSets(setCount, repsPerSet, maxReps int, setReps, setWeights []int) (NormalizedSets, error) {
	if len(setReps) > 0 {
		for _, reps := range setReps {
			if reps < 1 {
				return NormalizedSets{}, errors.New("each set must contain at least one repetition")
			}
		}
		weights := setWeights
		if len(weights) > 0 && len(weights) != len(setReps) {
			return NormalizedSets{}, errors.New("setWeights must match setReps length")
		}
		return NormalizedSets{
			RepsStorage: joinInts(setReps), WeightsStorage: joinInts(weights),
			SetCount: len(setReps), RepsPerSet: slices.Min(setReps), MaxReps: slices.Max(setReps),
			Reps: append([]int(nil), setReps...), Weights: append([]int(nil), weights...),
		}, nil
	}
	if setCount < 1 || repsPerSet < 1 || maxReps < 1 {
		return NormalizedSets{}, errors.New("setCount, repsPerSet and maxReps must be at least 1")
	}
	reps := make([]int, setCount)
	for index := range reps {
		reps[index] = repsPerSet
	}
	storage := ""
	if maxReps != repsPerSet {
		reps = append(reps, maxReps)
		storage = joinInts(reps)
	}
	return NormalizedSets{
		RepsStorage: storage, SetCount: setCount, RepsPerSet: repsPerSet, MaxReps: maxReps, Reps: reps,
	}, nil
}

func ParseStorage(raw string) []int {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]int, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil {
			return nil
		}
		result = append(result, value)
	}
	return result
}

func EffectiveReps(setCount, repsPerSet, maxReps int, storage string) []int {
	if parsed := ParseStorage(storage); len(parsed) > 0 {
		return parsed
	}
	result := make([]int, setCount)
	for index := range result {
		result[index] = repsPerSet
	}
	if maxReps != repsPerSet {
		result = append(result, maxReps)
	}
	return result
}

func Display(weight float64, reps, weights []int) string {
	if len(weights) > 0 && len(weights) == len(reps) {
		return joinSlash(weights) + "  " + joinSlash(reps)
	}
	formattedWeight := strconv.FormatFloat(weight, 'f', -1, 64)
	if len(reps) == 0 {
		return formattedWeight
	}
	if len(reps) >= 3 {
		working := reps[:len(reps)-1]
		maximum := reps[len(reps)-1]
		uniform := true
		for _, value := range working[1:] {
			if value != working[0] {
				uniform = false
				break
			}
		}
		if uniform && maximum != working[0] {
			return fmt.Sprintf("%s  %d×%d  (%d)", formattedWeight, len(working), working[0], maximum)
		}
	}
	return formattedWeight + "  " + joinSlash(reps)
}

func Volume(weight float64, reps, weights []int) float64 {
	var result float64
	if len(weights) > 0 && len(weights) == len(reps) {
		for index, repetitions := range reps {
			result += float64(weights[index] * repetitions)
		}
		return result
	}
	for _, repetitions := range reps {
		result += weight * float64(repetitions)
	}
	return result
}

func joinInts(values []int) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, len(values))
	for index, value := range values {
		parts[index] = strconv.Itoa(value)
	}
	return strings.Join(parts, ",")
}

func joinSlash(values []int) string {
	return strings.ReplaceAll(joinInts(values), ",", "/")
}
