package agent

import (
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
)

func TestTimestampContentMakesRelativeDatesAbsolute(t *testing.T) {
	t.Parallel()
	zone, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, 8, 3, 19, 44, 40, 0, time.UTC)
	got := timestampContent("user", "Вчера присед 60 кг", created, zone)
	want := "[Отправлено 03.08.2026 22:44 Europe/Moscow] Вчера присед 60 кг"
	if got != want {
		t.Fatalf("content = %q, want %q", got, want)
	}
	if got := timestampContent("tool", "ok", created, zone); got != "ok" {
		t.Fatalf("tool content changed: %q", got)
	}
}

func TestDropIncompleteToolTurns(t *testing.T) {
	t.Parallel()
	call := openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "a1"}}}
	tool := openrouter.Message{Role: "tool", ToolCallID: "a1"}
	user := openrouter.Message{Role: "user", Content: "next"}
	if got := dropIncompleteToolTurns([]openrouter.Message{call, user}); len(got) != 1 || got[0].Role != "user" {
		t.Fatalf("incomplete turn = %#v", got)
	}
	if got := dropIncompleteToolTurns([]openrouter.Message{tool, user}); len(got) != 1 || got[0].Role != "user" {
		t.Fatalf("orphan tool = %#v", got)
	}
	if got := dropIncompleteToolTurns([]openrouter.Message{call, tool}); len(got) != 2 {
		t.Fatalf("complete turn = %#v", got)
	}
	unexpectedTool := openrouter.Message{Role: "tool", ToolCallID: "other"}
	if got := dropIncompleteToolTurns([]openrouter.Message{call, tool, unexpectedTool, user}); len(got) != 1 || got[0].Role != "user" {
		t.Fatalf("turn with unexpected tool result = %#v", got)
	}
	if got := dropIncompleteToolTurns([]openrouter.Message{call, tool, tool, user}); len(got) != 1 || got[0].Role != "user" {
		t.Fatalf("turn with duplicate tool result = %#v", got)
	}
}
