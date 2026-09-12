package agent

import (
	"encoding/json"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"testing"
)

func decisionResponse(mode, reply string, actions ...plannedAction) openrouter.Response {
	raw, _ := json.Marshal(turnDecision{Mode: mode, Reply: reply, Actions: actions})
	return openrouter.Response{Message: openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "decision", Type: "function", Function: openrouter.ToolFunction{Name: "resolve_turn", Arguments: string(raw)}}}}}
}
func TestDecisionValidationBeforeAnySideEffect(t *testing.T) {
	action := plannedAction{Tool: "log_workout", Arguments: map[string]any{"exercise_name": "Жим", "notation": "65 3*8/11"}, RequestQuote: "запиши"}
	for _, tc := range []struct {
		name, mode, text string
		readOnly         bool
		actions          []plannedAction
		allowed          bool
	}{
		{"literal request", "write", "запиши", false, []plannedAction{action}, true},
		{"read mode cannot mutate", "read", "запиши", false, []plannedAction{action}, false},
		{"read loop cannot escalate", "write", "запиши", true, []plannedAction{action}, false},
		{"fabricated authorization", "write", "покажи", false, []plannedAction{action}, false},
		{"duplicate plan", "write", "запиши", false, []plannedAction{action, action}, false},
		{"unknown tool", "write", "запиши", false, []plannedAction{{Tool: "drop_database", Arguments: map[string]any{}, RequestQuote: "запиши"}}, false},
		{"bad arguments", "write", "запиши", false, []plannedAction{{Tool: "log_workout", Arguments: map[string]any{"exercise_name": "Жим", "notation": "65 8/11", "unexpected": true}, RequestQuote: "запиши"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseDecision(decisionResponse(tc.mode, "", tc.actions...).Message, tc.text, ToolDefinitions(false), tc.readOnly)
			if (err == nil) != tc.allowed {
				t.Fatalf("err=%v allowed=%v", err, tc.allowed)
			}
		})
	}
}
func TestCorrectionCannotDeleteBeforeReplacement(t *testing.T) {
	actions := []plannedAction{
		{Tool: "delete_workout", Arguments: map[string]any{"exercise_name": "Жим", "performed_on": "2026-09-01"}, RequestQuote: "исправь"},
		{Tool: "log_workout", Arguments: map[string]any{"exercise_name": "Жим", "date": "2026-09-01", "notation": "65 8/11"}, RequestQuote: "исправь"},
	}
	if _, err := parseDecision(decisionResponse("write", "", actions...).Message, "исправь", ToolDefinitions(false), false); err == nil {
		t.Fatal("destructive replacement accepted")
	}
}
