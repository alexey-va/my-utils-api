package agent

import (
	"context"
	"encoding/json"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func TestDecisionSandboxCopiesDatabaseHistoryAndArchivesFailures(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_POSTGRES_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	m := NewMemory(pool, nil)
	chat, err := m.CreateTestChat(ctx, "decision regression")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.DeleteTestChat(ctx, chat.ID) }()
	tools := NewToolService(pool, nil, nil, m, nil, nil)
	setup := []struct {
		name string
		args map[string]any
	}{
		{"create_exercise", map[string]any{"name": "Тестовый жим", "muscle_group": "chest"}},
		{"log_workout", map[string]any{"exercise_name": "Тестовый жим", "notation": "52.5 8/8", "date": "2026-08-01"}},
		{"log_workout", map[string]any{"exercise_name": "Тестовый жим", "notation": "90 8/8", "date": "2026-08-20"}},
	}
	for _, a := range setup {
		if _, err := tools.Execute(ctx, chat.MemoryChatID, a.name, a.args, "synthetic fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("write", "", plannedAction{Tool: "copy_workout", RequestQuote: "как в прошлый раз", Arguments: map[string]any{"exercise_name": "Тестовый жим", "date": "2026-08-10"}})}}
	result, err := testTurner(llm, NewContextualConversation(m, nil, nil, nil, nil, nil, nil), tools).Turn(ctx, chat.MemoryChatID, "Тестовый жим 10 августа как в прошлый раз", nil, true)
	if err != nil || !strings.Contains(result.Reply, "52.5") || strings.Contains(result.Reply, "90 ") {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	var raw string
	if err := pool.QueryRow(ctx, "SELECT state_json FROM agent_test_sandbox_states WHERE memory_chat_id=$1", chat.MemoryChatID).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var state sandboxState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range state.Workouts {
		if w.PerformedOn == "2026-08-10" {
			found = true
			if w.WeightKg != 52.5 || len(w.Reps) != 2 {
				t.Fatalf("copied=%+v", w)
			}
		}
	}
	if !found {
		t.Fatal("copy not persisted")
	}
	// Historical failed tool calls survive compaction and are actually retrievable
	// through the sandbox router, with no real services configured.
	_, err = m.Append(ctx, chat.MemoryChatID, openrouter.Message{Role: "tool", ToolCallID: "failed-1", Name: "log_workout", Content: `{"ok":false,"error":"synthetic database failure"}`})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE agent_conversation_messages SET is_compacted=true WHERE chat_id=$1", chat.MemoryChatID); err != nil {
		t.Fatal(err)
	}
	archive, err := tools.Execute(ctx, chat.MemoryChatID, "get_conversation_history", map[string]any{}, "покажи json", true)
	if err != nil || !strings.Contains(archive, "synthetic database failure") || !strings.Contains(archive, "copy_workout") {
		t.Fatalf("archive=%s err=%v", archive, err)
	}
}
