package agent

import (
	"context"
	"os"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestCompactorMaintainsOneRollingSummary(t *testing.T) {
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
	chatID := int64(7_707_707_707)
	clearCompactionChat(t, ctx, pool, chatID)
	defer func() {
		clearCompactionChat(t, ctx, pool, chatID)
	}()
	memory := NewMemory(pool, nil)
	for _, text := range []string{"one", "two", "three", "four"} {
		if _, err := memory.AppendManual(ctx, chatID, "user", text, nil); err != nil {
			t.Fatal(err)
		}
	}
	llm := &fakeCompleter{responses: []openrouter.Response{{Message: openrouter.Message{Content: "summary one"}}, {Message: openrouter.Message{Content: "summary two"}}}}
	compactor := NewCompactor(pool, llm, func() string { return "p/m" })
	first, err := compactor.Compact(ctx, chatID, 2)
	if err != nil || !first.Compacted || first.MessageCount != 2 {
		t.Fatalf("first = %#v err=%v", first, err)
	}
	for _, text := range []string{"five", "six"} {
		_, _ = memory.AppendManual(ctx, chatID, "user", text, nil)
	}
	second, err := compactor.Compact(ctx, chatID, 2)
	if err != nil || !second.Compacted || second.MessageCount != 2 {
		t.Fatalf("second = %#v err=%v", second, err)
	}
	var summaries, compacted int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM agent_context_summaries WHERE chat_id=$1),(SELECT count(*) FROM agent_conversation_messages WHERE chat_id=$1 AND is_compacted)`, chatID).Scan(&summaries, &compacted); err != nil {
		t.Fatal(err)
	}
	if summaries != 1 || compacted != 4 {
		t.Fatalf("summaries=%d compacted=%d", summaries, compacted)
	}
}

func TestAutoCompactorRespectsThresholdAndKeepsTail(t *testing.T) {
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
	chatID := int64(7_707_707_708)
	clearCompactionChat(t, ctx, pool, chatID)
	defer func() {
		clearCompactionChat(t, ctx, pool, chatID)
	}()
	memory := NewMemory(pool, nil)
	for index := 0; index < 50; index++ {
		if _, err := memory.AppendManual(ctx, chatID, "user", "message", nil); err != nil {
			t.Fatal(err)
		}
	}
	llm := &fakeCompleter{responses: []openrouter.Response{{Message: openrouter.Message{Content: "summary"}}}}
	compactor := NewCompactor(pool, llm, func() string { return "p/m" })
	if result, err := compactor.CompactAuto(ctx, chatID, 10, 40); err != nil || result.Compacted {
		t.Fatalf("below threshold result=%#v err=%v", result, err)
	}
	if _, err := memory.AppendManual(ctx, chatID, "user", "threshold crossed", nil); err != nil {
		t.Fatal(err)
	}
	result, err := compactor.CompactAuto(ctx, chatID, 10, 40)
	if err != nil || !result.Compacted || result.MessageCount != 41 {
		t.Fatalf("result=%#v err=%v", result, err)
	}
}

func clearCompactionChat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, chatID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DELETE FROM agent_conversation_messages WHERE chat_id=$1`, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM agent_context_summaries WHERE chat_id=$1`, chatID); err != nil {
		t.Fatal(err)
	}
}

func TestRewindSplitToolTurn(t *testing.T) {
	t.Parallel()
	messages := []compactMessage{{role: "user"}, {role: "assistant"}, {role: "tool"}, {role: "tool"}, {role: "user"}}
	if got := rewindSplitToolTurn(messages, 2); got != 1 {
		t.Fatalf("boundary 2 rewound to %d", got)
	}
	if got := rewindSplitToolTurn(messages, 3); got != 1 {
		t.Fatalf("boundary 3 rewound to %d", got)
	}
	if got := rewindSplitToolTurn(messages, 4); got != 4 {
		t.Fatalf("boundary 4 rewound to %d", got)
	}
}
