package agent

import "testing"

func TestMutationPolicyFailsClosed(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, message string
		allowed       bool
	}{
		{"log_workout", "покажи тренировку 70 3*10/12", false},
		{"log_workout", "запиши жим 70 3*10/12", true},
		{"log_workout", "Сделал бабочку на заднюю дельту — 58кг 8/10", true},
		{"create_exercise", "Запиши бабочку на заднюю дельту 58 кг 8/10", true},
		{"create_exercise", "Исправь", true},
		{"delete_workout", "Исправь", true},
		{"log_workout", "Исправь", true},
		{"log_body_weight", "сколько я весил?", false},
		{"log_body_weight", "сегодня вес 82.5 кг", true},
		{"remember_fact", "кажется, у меня болит плечо?", false},
		{"remember_fact", "запомни: болит плечо", true},
		{"schedule_notification", "какие напоминания есть?", false},
		{"schedule_notification", "напомни завтра потренироваться", true},
		{"get_days", "что было вчера?", true},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name+"/"+test.message, func(t *testing.T) {
			if got := MutationAllowed(test.name, test.message); got != test.allowed {
				t.Fatalf("MutationAllowed() = %v, want %v", got, test.allowed)
			}
		})
	}
}

func TestGroundWorkoutNotationUsesLiteralUserInput(t *testing.T) {
	t.Parallel()
	args := map[string]any{"exercise_name": "Жим", "notation": "100 10/10"}
	GroundToolArguments("log_workout", args, "запиши жим 70 3*10/12")
	if args["notation"] != "70 3*10/12" {
		t.Fatalf("notation = %#v", args["notation"])
	}
}

func TestGroundWorkoutNotationRemovesKilogramUnit(t *testing.T) {
	t.Parallel()
	args := map[string]any{"exercise_name": "Бабочка на заднюю дельту", "notation": "100 3*10/12"}
	GroundToolArguments("log_workout", args, "Сделал бабочку на заднюю дельту — 58кг 8/10")
	if args["notation"] != "58 8/10" {
		t.Fatalf("notation = %#v", args["notation"])
	}
}
