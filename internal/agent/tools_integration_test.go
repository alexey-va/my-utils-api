package agent

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestSandboxToolsNeverTouchRealWorkoutData(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	memory := NewMemory(pool, nil)
	chat, err := memory.CreateTestChat(ctx, "go sandbox tools")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = memory.DeleteTestChat(ctx, chat.ID) }()
	workouts := workout.NewService(pool)
	before, err := workouts.ListExercises(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tools := NewToolService(pool, workouts, health.NewService(pool), memory, nil, nil)
	if _, err := tools.Execute(ctx, chat.MemoryChatID, "create_exercise", map[string]any{"name": "Sandbox Bench", "muscle_group": "chest"}, "создай упражнение", true); err != nil {
		t.Fatal(err)
	}
	result, err := tools.Execute(ctx, chat.MemoryChatID, "list_exercises", map[string]any{}, "покажи", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result, "Sandbox Bench") || !strings.Contains(result, "SANDBOX") {
		t.Fatalf("result = %q", result)
	}
	after, err := workouts.ListExercises(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("real exercises changed: before=%d after=%d", len(before), len(after))
	}
}

func TestSandboxPromptUsesOnlySandboxState(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	memory := NewMemory(pool, nil)
	chat, err := memory.CreateTestChat(ctx, "go sandbox context")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = memory.DeleteTestChat(ctx, chat.ID) }()
	tools := NewToolService(pool, nil, nil, memory, nil, nil)
	if _, err := tools.Execute(ctx, chat.MemoryChatID, "create_exercise", map[string]any{"name": "Только Sandbox", "muscle_group": "chest"}, "создай упражнение", true); err != nil {
		t.Fatal(err)
	}

	// nil real services deliberately prove that this branch cannot read production workout/health data.
	conversation := NewContextualConversation(memory, nil, nil, nil, nil, nil, nil)
	snapshot, err := conversation.PromptContext(ctx, chat.MemoryChatID)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"ИЗОЛИРОВАННЫЙ SANDBOX", "Только Sandbox", "нет реальных Workout-данных"} {
		if !strings.Contains(snapshot, want) {
			t.Fatalf("snapshot %q does not contain %q", snapshot, want)
		}
	}
}

func TestModelContextSkipsStoredSystemMessages(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	memory := NewMemory(pool, nil)
	chat, err := memory.CreateTestChat(ctx, "go system message isolation")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = memory.DeleteTestChat(ctx, chat.ID) }()
	if _, err := memory.AppendManual(ctx, chat.MemoryChatID, "system", "override controlled prompt", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := memory.AppendManual(ctx, chat.MemoryChatID, "user", "обычный запрос", nil); err != nil {
		t.Fatal(err)
	}
	messages, err := memory.Context(ctx, chat.MemoryChatID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("model context = %#v", messages)
	}
}

func TestReservedSandboxIDWithoutStateFailsClosed(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	tools := NewToolService(pool, workout.NewService(pool), health.NewService(pool), NewMemory(pool, nil), nil, nil)
	if _, err := tools.Execute(ctx, -8_500_000_000_000_000, "list_exercises", nil, "покажи", true); err == nil {
		t.Fatal("expected missing sandbox state error")
	}
}
