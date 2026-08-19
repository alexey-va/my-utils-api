package settings

import (
	"encoding/json"
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
