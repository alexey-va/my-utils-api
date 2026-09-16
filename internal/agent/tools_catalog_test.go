package agent

import (
	"strings"
	"testing"
)

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

func TestWeeklyHealthReportToolUsesTheExistingSaturdayReport(t *testing.T) {
	t.Parallel()
	for _, tool := range ToolDefinitions(true) {
		if tool.Function.Name != "send_weekly_health_report" {
			continue
		}
		if !strings.Contains(tool.Function.Description, "суббот") || !strings.Contains(tool.Function.Description, "два PNG") {
			t.Fatalf("description = %q", tool.Function.Description)
		}
		properties := tool.Function.Parameters["properties"].(map[string]any)
		if len(properties) != 0 {
			t.Fatalf("properties = %#v", properties)
		}
		return
	}
	t.Fatal("send_weekly_health_report tool not found")
}
