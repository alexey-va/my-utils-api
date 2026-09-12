package agent

import "testing"

func TestValidateWorkoutEvidence(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		action    plannedAction
		userTexts []string
		wantErr   bool
	}{
		{
			name:      "decimal weight with date between weight and reps",
			action:    workoutEvidenceAction("22.5 10/10", "Жим 22,5 кг за 2026-09-12 — 10/10", ""),
			userTexts: []string{"Жим 22,5 кг за 2026-09-12 — 10/10"},
		},
		{
			name:      "plain decimal comma weight",
			action:    workoutEvidenceAction("22.5 10/10", "Плечи 22,5 10/10", ""),
			userTexts: []string{"Плечи 22,5 10/10"},
		},
		{
			name:      "literal weight mismatch",
			action:    workoutEvidenceAction("23 10/10", "Жим 22,5 кг — 10/10", ""),
			userTexts: []string{"Жим 22,5 кг — 10/10"},
			wantErr:   true,
		},
		{
			name:      "pounds convert to kilograms",
			action:    workoutEvidenceAction("35 10/12", "Трицепс 77 фунтов 10/12", ""),
			userTexts: []string{"Трицепс 77 фунтов 10/12"},
		},
		{
			name:      "pounds are not kilograms",
			action:    workoutEvidenceAction("77 10/12", "Трицепс 77 фунтов 10/12", ""),
			userTexts: []string{"Трицепс 77 фунтов 10/12"},
			wantErr:   true,
		},
		{
			name:      "corrected scalar weight comes from request quote",
			action:    workoutEvidenceAction("22 10/10", "10/10", "22 тогда"),
			userTexts: []string{"Сделал жим 20 кг 10/10", "22 тогда"},
		},
		{
			name:      "decimal comma correction comes from request quote",
			action:    workoutEvidenceAction("22.5 10/10", "10/10", "22,5 тогда"),
			userTexts: []string{"Сделал жим 20 кг 10/10", "22,5 тогда"},
		},
		{
			name:      "explicit varied weights remain one result",
			action:    workoutEvidenceAction("70/75 10/12", "Жим 70/75 10/12", ""),
			userTexts: []string{"Жим 70/75 10/12"},
		},
		{
			name:      "multiple exercises are rejected",
			action:    workoutEvidenceAction("70 10/10", "Жим 70 10/10 и тяга 80 8/8", ""),
			userTexts: []string{"Жим 70 10/10 и тяга 80 8/8"},
			wantErr:   true,
		},
		{
			name:      "copied notation for two exercises is rejected",
			action:    workoutEvidenceAction("70 10/10", "Жим 70 10/10 и тяга 70 10/10", ""),
			userTexts: []string{"Жим 70 10/10 и тяга 70 10/10"},
			wantErr:   true,
		},
		{
			name:      "quote must be present verbatim",
			action:    workoutEvidenceAction("70 10/10", "Жим 70 10/10", ""),
			userTexts: []string{"Сделал жим 70 10/10"},
			wantErr:   true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateWorkoutEvidence(test.action, test.userTexts)
			if (err != nil) != test.wantErr {
				t.Fatalf("validateWorkoutEvidence() error = %v, wantErr=%v", err, test.wantErr)
			}
		})
	}
}

func TestValidateWorkoutEvidenceIsPure(t *testing.T) {
	t.Parallel()
	action := workoutEvidenceAction("22 10/10", "10/10", "22 тогда")
	texts := []string{"Сделал жим 20 кг 10/10", "22 тогда"}
	first := validateWorkoutEvidence(action, texts)
	second := validateWorkoutEvidence(action, texts)
	if (first == nil) != (second == nil) || (first != nil && first.Error() != second.Error()) {
		t.Fatalf("repeated validation changed result: first=%v second=%v", first, second)
	}
}

func TestValidateWorkoutEvidenceIgnoresOtherTools(t *testing.T) {
	t.Parallel()
	if err := validateWorkoutEvidence(plannedAction{Tool: "get_days"}, nil); err != nil {
		t.Fatalf("non-workout action returned error: %v", err)
	}
}

func workoutEvidenceAction(notation, dataQuote, requestQuote string) plannedAction {
	return plannedAction{
		Tool:         "log_workout",
		Arguments:    map[string]any{"exercise_name": "Жим", "notation": notation},
		DataQuote:    dataQuote,
		RequestQuote: requestQuote,
	}
}

func TestCopyWeightOverrideUsesUserWeightWithoutRepeatingReps(t *testing.T) {
	for _, tc := range []struct {
		name, request, data string
		weight              float64
		allowed             bool
	}{
		{"weight only", "только бабочку на 65 кг", "", 65, true},
		{"decimal comma", "исправь на 62,5", "", 62.5, true},
		{"contextual confirmation", "Да", "бабочку на 65 кг", 65, true},
		{"fabricated weight", "только бабочку на 65 кг", "", 70, false},
		{"multiple exercises", "жим 65, тяга 80", "", 65, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			action := plannedAction{Tool: "copy_workout", Arguments: map[string]any{"weight_kg": tc.weight}, RequestQuote: tc.request, DataQuote: tc.data}
			err := validateWorkoutEvidence(action, []string{tc.request, "бабочку на 65 кг"})
			if (err == nil) != tc.allowed {
				t.Fatalf("err=%v, allowed=%v", err, tc.allowed)
			}
		})
	}
}
