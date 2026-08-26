package agent

import (
	"testing"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
)

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

func TestGroundWorkoutNotationKeepsMultipleExercisesIndependent(t *testing.T) {
	t.Parallel()
	message := "Запиши приводящую ног сегодня 90кг 12/15 и трицепс 77 фунтов 10/12"
	first := map[string]any{"exercise_name": "Приводящие ног", "notation": "90 3*12/15"}
	second := map[string]any{"exercise_name": "Трицепс", "notation": "35 3*10/12"}
	grounder := newToolArgumentGrounder(message)

	if err := grounder.Ground("log_workout", first); err != nil {
		t.Fatal(err)
	}
	if err := grounder.Ground("log_workout", second); err != nil {
		t.Fatal(err)
	}

	if first["notation"] != "90 12/15" {
		t.Fatalf("first notation = %#v", first["notation"])
	}
	if second["notation"] != "35 10/12" {
		t.Fatalf("second notation = %#v", second["notation"])
	}
}

func TestGroundWorkoutNotationConvertsPoundsWithoutAsking(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		message, want string
	}{
		{"Трицепс 77 фунтов 10/12", "35 10/12"},
		{"Трицепс 71 lb 12/15", "32 12/15"},
		{"Трицепс 71 lbs 12/15", "32 12/15"},
		{"Трицепс 72 фунта 12/15", "33 12/15"},
	} {
		test := test
		t.Run(test.message, func(t *testing.T) {
			t.Parallel()
			args := map[string]any{"exercise_name": "Трицепс", "notation": "999 3*10/12"}
			GroundToolArguments("log_workout", args, test.message)
			if args["notation"] != test.want {
				t.Fatalf("notation = %#v, want %q", args["notation"], test.want)
			}
		})
	}
}

func TestGroundWorkoutNotationFailsClosedOnExtraToolCall(t *testing.T) {
	t.Parallel()
	grounder := newToolArgumentGrounder("Трицепс 77 фунтов 10/12")
	if err := grounder.Ground("log_workout", map[string]any{"notation": "35 3*10/12"}); err != nil {
		t.Fatal(err)
	}
	if err := grounder.Ground("log_workout", map[string]any{"notation": "35 3*10/12"}); err == nil {
		t.Fatal("expected extra workout call to fail closed")
	}
}

func TestMutationFollowUpRequiresBoundedPendingWrite(t *testing.T) {
	t.Parallel()
	pendingDelete := []openrouter.Message{
		{Role: "user", Content: "Удали запись бабочки за пятницу"},
		{Role: "assistant", Content: "Уточни: обычную бабочку или заднюю дельту?"},
		{Role: "user", Content: "Обычную"},
	}
	if !MutationAllowedWithContext("delete_workout", "Обычную", pendingDelete) {
		t.Fatal("bounded clarification reply must inherit the explicit delete intent")
	}
	if MutationAllowedWithContext("delete_workout", "Нет", pendingDelete) {
		t.Fatal("negative reply must not authorize deletion")
	}
	if MutationAllowedWithContext("delete_workout", "Да", []openrouter.Message{{Role: "user", Content: "Да"}}) {
		t.Fatal("unrelated affirmative reply must not authorize deletion")
	}
	readOnly := []openrouter.Message{
		{Role: "user", Content: "Что было в пятницу?"},
		{Role: "assistant", Content: "Уточни: обычная бабочка или задняя дельта?"},
		{Role: "user", Content: "Обычная"},
	}
	if MutationAllowedWithContext("delete_workout", "Обычная", readOnly) {
		t.Fatal("clarification after a read-only request must not authorize deletion")
	}
}
