package agent

import "testing"

func TestCreateExerciseMuscleGroupUsesCanonicalValues(t *testing.T) {
	t.Parallel()
	for _, tool := range ToolDefinitions(false) {
		if tool.Function.Name != "create_exercise" {
			continue
		}
		properties := tool.Function.Parameters["properties"].(map[string]any)
		group := properties["muscle_group"].(map[string]any)
		values, ok := group["enum"].([]string)
		if !ok {
			t.Fatalf("muscle_group enum = %#v", group["enum"])
		}
		want := []string{"arms", "back", "chest", "core", "legs", "other", "shoulders"}
		if len(values) != len(want) {
			t.Fatalf("muscle_group enum = %#v", values)
		}
		for index := range want {
			if values[index] != want[index] {
				t.Fatalf("muscle_group enum = %#v", values)
			}
		}
		return
	}
	t.Fatal("create_exercise tool not found")
}
