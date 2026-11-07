package agent

import "testing"

func TestSandboxExerciseUsesCanonicalMuscleGroupNormalization(t *testing.T) {
	t.Parallel()
	tools := &ToolService{}
	state := &sandboxState{}
	result, err := tools.runSandboxTool(state, "create_exercise", map[string]any{
		"name":         "Задняя дельта",
		"muscle_group": " SHOULDERS ",
	})
	if err != nil {
		t.Fatal(err)
	}
	if result != "Добавил упражнение «Задняя дельта» (плечи)." || len(state.Exercises) != 1 || state.Exercises[0].MuscleGroup != "shoulders" {
		t.Fatalf("result=%q state=%#v", result, state)
	}
}
