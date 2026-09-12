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

func TestAutoCompactorCountsOnlyRawUserTurns(t *testing.T) {
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
	chatID := int64(7_707_707_711)
	clearCompactionChat(t, ctx, pool, chatID)
	defer clearCompactionChat(t, ctx, pool, chatID)
	memory := NewMemory(pool, nil)
	for index := 0; index < 10; index++ {
		if _, err := memory.AppendManual(ctx, chatID, "user", "initial", nil); err != nil {
			t.Fatal(err)
		}
	}
	llm := &fakeCompleter{responses: []openrouter.Response{{Message: openrouter.Message{Content: "summary one"}}, {Message: openrouter.Message{Content: "summary two"}}}}
	compactor := NewCompactor(pool, llm, func() string { return "p/m" })
	first, err := compactor.Compact(ctx, chatID, 5)
	if err != nil || !first.Compacted || first.MessageCount != 5 {
		t.Fatalf("first = %#v err=%v", first, err)
	}
	for index := 0; index < 5; index++ {
		if _, err := memory.AppendManual(ctx, chatID, "user", "new", nil); err != nil {
			t.Fatal(err)
		}
	}
	result, err := compactor.CompactAuto(ctx, chatID, 2, 6)
	if err != nil || !result.Compacted || result.MessageCount != 8 {
		t.Fatalf("raw-turn threshold result=%#v err=%v", result, err)
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

func TestSelectCompactMessagesKeepsUserTurnsWhole(t *testing.T) {
	call := ToolCall{ID: "call-1", Type: "function", Function: ToolFunction{Name: "log_workout", Arguments: `{}`}}
	toolID := "call-1"
	turns := splitCompactTurns([]compactMessage{
		{id: 1, role: "user", parsed: ChatMessage{Role: "user"}},
		{id: 2, role: "assistant", parsed: ChatMessage{Role: "assistant", ToolCalls: []ToolCall{call}}},
		{id: 3, role: "tool", parsed: ChatMessage{Role: "tool", ToolCallID: &toolID}},
		{id: 4, role: "assistant", parsed: ChatMessage{Role: "assistant"}},
		{id: 5, role: "user", parsed: ChatMessage{Role: "user"}},
		{id: 6, role: "assistant", parsed: ChatMessage{Role: "assistant"}},
		{id: 7, role: "user", parsed: ChatMessage{Role: "user"}},
		{id: 8, role: "assistant", parsed: ChatMessage{Role: "assistant", ToolCalls: []ToolCall{{ID: "ongoing"}}}},
	})
	if len(turns) != 3 || !turns[0].complete || !turns[1].complete || turns[2].complete {
		t.Fatalf("turns = %#v", turns)
	}
	selected := selectCompactMessages(turns, 2)
	if len(selected) != 6 {
		t.Fatalf("selected %d messages, want first two complete turns", len(selected))
	}
	for index, message := range selected {
		if message.id != int64(index+1) {
			t.Fatalf("selected[%d].id = %d", index, message.id)
		}
	}
	if selected[len(selected)-1].id >= 7 {
		t.Fatal("latest user turn was selected for compaction")
	}

	turns[1].complete = false
	if selected = selectCompactMessages(turns, 2); len(selected) != 4 {
		t.Fatalf("incomplete second turn selected %d messages, want only first turn", len(selected))
	}
}

func TestSelectCompactMessagesSkipsLegacyMixedTurns(t *testing.T) {
	toolID := "legacy-call"
	turns := splitCompactTurns([]compactMessage{
		{id: 668, role: "user", compacted: true, parsed: ChatMessage{Role: "user"}},
		{id: 669, role: "tool", parsed: ChatMessage{Role: "tool", ToolCallID: &toolID}},
		{id: 670, role: "user", parsed: ChatMessage{Role: "user"}},
		{id: 671, role: "assistant", parsed: ChatMessage{Role: "assistant"}},
		{id: 672, role: "user", parsed: ChatMessage{Role: "user"}},
		{id: 673, role: "assistant", parsed: ChatMessage{Role: "assistant"}},
	})
	if len(turns) != 3 || turns[0].complete || turns[0].fullyRaw {
		t.Fatalf("legacy turn = %#v", turns[0])
	}
	if got := compactRawTurnCount(turns); got != 2 {
		t.Fatalf("raw turn count = %d, want two new raw turns", got)
	}
	selected := selectCompactMessages(turns, 1)
	if len(selected) != 2 || selected[0].id != 670 || selected[1].id != 671 {
		t.Fatalf("selected after legacy turn = %#v", selected)
	}
}
