package agent

import (
	"context"
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRelativeWorkoutClarificationPersistsWholePlan(t *testing.T) {
	if os.Getenv("TEST_POSTGRES_URL") == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("TEST_POSTGRES_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	memory := NewMemory(pool, nil)
	chat, err := memory.CreateTestChat(ctx, "relative regression")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = memory.DeleteTestChat(ctx, chat.ID) }()
	tools := NewToolService(pool, nil, nil, memory, nil, nil)
	for _, row := range []struct{ name, notation string }{{"Тестовый жим", "42 8/8"}, {"Тестовая тяга", "32 8/8"}} {
		if _, err := tools.Execute(ctx, chat.MemoryChatID, "create_exercise", map[string]any{"name": row.name}, "fixture", true); err != nil {
			t.Fatal(err)
		}
		if _, err := tools.Execute(ctx, chat.MemoryChatID, "log_workout", map[string]any{"exercise_name": row.name, "notation": row.notation, "date": "2026-09-01"}, "fixture", true); err != nil {
			t.Fatal(err)
		}
	}
	first := "Тестовый жим на 3 кг больше прошлого 10/10 за 2026-09-02"
	second := "Обычный жим. И тестовая тяга как в прошлый 10/12 за ту же дату. Запомни: жим без уточнений обычный."
	llm := &fakeCompleter{responses: []openrouter.Response{
		decisionResponse("clarify", "Обычный жим или другой?"),
		decisionResponse("write", "",
			plannedAction{Tool: "copy_workout", Arguments: map[string]any{"exercise_name": "Тестовый жим", "date": "2026-09-02", "weight_delta_kg": float64(3), "repetitions": "3*10/10"}, RequestQuote: "Обычный жим", DataQuote: first},
			plannedAction{Tool: "copy_workout", Arguments: map[string]any{"exercise_name": "Тестовая тяга", "date": "2026-09-02", "repetitions": "3*10/12"}, RequestQuote: "тестовая тяга как в прошлый 10/12", DataQuote: "тестовая тяга как в прошлый 10/12"},
			plannedAction{Tool: "remember_fact", Arguments: map[string]any{"content": "Жим без уточнений означает обычный жим."}, RequestQuote: "Запомни: жим без уточнений обычный."}),
	}}
	turner := testTurner(llm, NewContextualConversation(memory, nil, nil, nil, nil, nil, nil), tools)
	if _, err := turner.Turn(ctx, chat.MemoryChatID, first, nil, true); err != nil {
		t.Fatal(err)
	}
	result, err := turner.Turn(ctx, chat.MemoryChatID, second, nil, true)
	if err != nil || strings.Contains(result.Reply, "Не удалось") {
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
	count := 0
	for _, row := range state.Workouts {
		if row.PerformedOn != "2026-09-02" {
			continue
		}
		count++
		weight, reps := float64(45), []int{10, 10, 10, 10}
		if row.ExerciseName == "Тестовая тяга" {
			weight, reps = 32, []int{10, 10, 10, 12}
		}
		if row.WeightKg != weight || !slices.Equal(row.Reps, reps) || row.SetCount != 3 {
			t.Fatalf("bad workout: %+v", row)
		}
	}
	if count != 2 || len(state.Facts) != 1 {
		t.Fatalf("state=%s", raw)
	}
	history, err := memory.Context(ctx, chat.MemoryChatID, 10)
	if err != nil {
		t.Fatal(err)
	}
	var calls, receipts int
	for _, msg := range history {
		if msg.Role == "assistant" {
			calls += len(msg.ToolCalls)
		}
		if msg.Role == "tool" {
			receipts++
			if !strings.Contains(contentString(msg.Content), `"ok":true`) {
				t.Fatalf("receipt=%+v", msg)
			}
		}
	}
	if calls != 3 || receipts != 3 {
		t.Fatalf("calls=%d receipts=%d", calls, receipts)
	}
}
