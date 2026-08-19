package health

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type StepDay struct {
	Date  string `json:"date"`
	Steps int    `json:"steps"`
}

type ParsedSteps struct {
	Source string    `json:"source"`
	Days   []StepDay `json:"days"`
}

func (p ParsedSteps) TodaySteps() *int {
	if len(p.Days) == 0 {
		return nil
	}
	value := p.Days[len(p.Days)-1].Steps
	return &value
}

func ParseSteps(body json.RawMessage, today time.Time) (*ParsedSteps, error) {
	if len(bytes.TrimSpace(body)) == 0 || bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
		return nil, nil
	}
	if !json.Valid(body) {
		return nil, errors.New("invalid JSON")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		// Valid non-object JSON is merely an unknown payload, as it was with the
		// previous JsonNode parser. Only malformed JSON is a bad request.
		return nil, nil
	}
	multiline := ""
	if raw, ok := object[""]; ok {
		_ = json.Unmarshal(raw, &multiline)
	}
	if multiline == "" {
		for _, raw := range object {
			var candidate string
			if json.Unmarshal(raw, &candidate) == nil && strings.Contains(candidate, "\n") {
				multiline = candidate
				break
			}
		}
	}
	if multiline != "" {
		var counts []int
		for _, line := range strings.Split(strings.ReplaceAll(multiline, "\r\n", "\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			value, err := strconv.Atoi(line)
			if err != nil {
				return nil, errors.New("invalid step count")
			}
			counts = append(counts, value)
		}
		if len(counts) == 0 {
			return nil, nil
		}
		days := make([]StepDay, len(counts))
		for index, steps := range counts {
			date := today.AddDate(0, 0, -(len(counts) - 1 - index))
			days[index] = StepDay{Date: date.Format(time.DateOnly), Steps: steps}
		}
		return &ParsedSteps{Source: "apple-shortcut-multiline", Days: days}, nil
	}
	var steps int
	if raw, ok := object["steps"]; !ok || json.Unmarshal(raw, &steps) != nil {
		return nil, nil
	}
	date := today.Format(time.DateOnly)
	if raw, ok := object["date"]; ok {
		var candidate string
		if json.Unmarshal(raw, &candidate) == nil && candidate != "" {
			parsed, err := time.Parse(time.DateOnly, candidate)
			if err != nil {
				return nil, err
			}
			date = parsed.Format(time.DateOnly)
		}
	}
	return &ParsedSteps{Source: "structured", Days: []StepDay{{Date: date, Steps: steps}}}, nil
}

type WeightDay struct {
	Date     string  `json:"date"`
	WeightKg float64 `json:"weightKg"`
}

type ParsedWeight struct {
	ReceivedDays int
	Days         []WeightDay
}

var decimalPattern = regexp.MustCompile(`\d+(?:[.,]\d+)?`)

func ParseWeightImport(body json.RawMessage, today time.Time) (*ParsedWeight, error) {
	var object map[string]json.RawMessage
	if json.Unmarshal(body, &object) != nil {
		return nil, nil
	}
	var multiline string
	if raw, ok := object[""]; !ok || json.Unmarshal(raw, &multiline) != nil {
		return nil, nil
	}
	normalized := strings.TrimRight(strings.ReplaceAll(multiline, "\r\n", "\n"), "\r\n")
	if strings.TrimSpace(normalized) == "" {
		return nil, nil
	}
	lines := strings.Split(normalized, "\n")
	result := &ParsedWeight{ReceivedDays: len(lines)}
	for index, line := range lines {
		match := decimalPattern.FindString(strings.TrimSpace(line))
		if match == "" {
			continue
		}
		value, err := strconv.ParseFloat(strings.ReplaceAll(match, ",", "."), 64)
		if err != nil {
			return nil, err
		}
		if value <= 0 {
			continue
		}
		date := today.AddDate(0, 0, -(len(lines) - 1 - index))
		result.Days = append(result.Days, WeightDay{Date: date.Format(time.DateOnly), WeightKg: value})
	}
	return result, nil
}
