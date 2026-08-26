package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAppCatalogContainsEveryKotlinRuntimeProperty(t *testing.T) {
	t.Parallel()

	catalog := AppCatalog(nil)
	if len(catalog.Definitions()) != 16 {
		t.Fatalf("definition count = %d, want 16", len(catalog.Definitions()))
	}
	wantDefaults := map[string]any{
		"temporal.evening-reminder.enabled":       false,
		"temporal.evening-reminder.hour":          float64(20),
		"temporal.evening-reminder.minute":        float64(0),
		"temporal.zone-id":                        "Europe/Moscow",
		"openrouter.model":                        "@preset/deepseek",
		"openrouter.max-tool-iterations":          float64(30),
		"agent.memory.recent-messages":            float64(10),
		"agent.context.recent-entries":            float64(30),
		"agent.context.calendar-days":             float64(14),
		"agent.context.progress-sessions":         float64(4),
		"agent.memory.compact-threshold-messages": float64(40),
		"agent.memory.compact-model":              "",
		"openrouter.retry.max-attempts":           float64(3),
		"openrouter.retry.initial-delay-ms":       float64(1000),
		"telegram.conversation-ttl-hours":         float64(48),
	}
	for _, definition := range catalog.Definitions() {
		if want, ok := wantDefaults[definition.Key]; ok {
			var got any
			if err := json.Unmarshal(definition.Default, &got); err != nil {
				t.Fatalf("decode default %s: %v", definition.Key, err)
			}
			if got != want {
				t.Errorf("default %s = %#v, want %#v", definition.Key, got, want)
			}
			delete(wantDefaults, definition.Key)
		}
	}
	if len(wantDefaults) != 0 {
		t.Fatalf("missing property defaults: %#v", wantDefaults)
	}
}

func TestEffectiveAgentSystemPromptAppendsMandatoryRulesToStaleSetting(t *testing.T) {
	t.Parallel()
	effective := EffectiveAgentSystemPrompt("старый пользовательский prompt")
	for _, want := range []string{
		"старый пользовательский prompt",
		agentRequiredRulesMarker,
		"фунты × 0.45359237",
		"Никогда не копируй числа первого упражнения",
		"продолжай исходную явно запрошенную операцию",
	} {
		if !strings.Contains(effective, want) {
			t.Fatalf("effective prompt does not contain %q", want)
		}
	}
	if repeated := EffectiveAgentSystemPrompt(effective); repeated != effective {
		t.Fatal("mandatory rules must be appended exactly once")
	}
}

func TestAgentSystemPromptDefaultContainsMandatoryRules(t *testing.T) {
	t.Parallel()
	for _, definition := range AppCatalog(nil).Definitions() {
		if definition.Key != AgentSystemPrompt {
			continue
		}
		var prompt string
		if err := json.Unmarshal(definition.Default, &prompt); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(prompt, agentRequiredRulesMarker) {
			t.Fatalf("default prompt = %q", prompt)
		}
		return
	}
	t.Fatal("agent system prompt definition not found")
}
