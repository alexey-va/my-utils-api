package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDropIncompleteToolTurnsRejectsEmptyAndDuplicateCallIDs(t *testing.T) {
	t.Parallel()
	user := openrouter.Message{Role: "user", Content: "next"}
	cases := []struct {
		name      string
		assistant openrouter.Message
		tools     []openrouter.Message
	}{
		{
			name:      "empty call ID",
			assistant: openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: ""}}},
			tools:     []openrouter.Message{{Role: "tool", ToolCallID: ""}},
		},
		{
			name:      "duplicate call IDs",
			assistant: openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "same"}, {ID: "same"}}},
			tools:     []openrouter.Message{{Role: "tool", ToolCallID: "same"}},
		},
		{
			name:      "whitespace call ID",
			assistant: openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "  "}}},
			tools:     []openrouter.Message{{Role: "tool", ToolCallID: "  "}},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			messages := append([]openrouter.Message{tc.assistant}, tc.tools...)
			messages = append(messages, user)
			got := dropIncompleteToolTurns(messages)
			if len(got) != 1 || got[0].Role != "user" {
				t.Fatalf("malformed tool turn survived: %#v", got)
			}
		})
	}
}

func TestConversationHistoryRespectsByteBudgetAndCursor(t *testing.T) {
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
	chatID := int64(7_707_707_713)
	clearArchiveBoundsChat(t, ctx, pool, chatID)
	defer clearArchiveBoundsChat(t, ctx, pool, chatID)
	memory := NewMemory(pool, nil)
	const payloadSize = 40 * 1024
	allIDs := make([]int64, 0, 8)
	for index := 0; index < 8; index++ {
		message, err := memory.AppendOpenRouter(ctx, chatID, openrouter.Message{
			Role:    "user",
			Content: fmt.Sprintf("row-%d-%s", index, strings.Repeat("x", payloadSize)),
		})
		if err != nil {
			t.Fatal(err)
		}
		allIDs = append(allIDs, message.ID)
	}

	seen := make(map[int64]bool, len(allIDs))
	var beforeID int64
	for pageNumber := 0; pageNumber < len(allIDs); pageNumber++ {
		archive, err := memory.ConversationHistory(ctx, chatID, beforeID, 100)
		if err != nil {
			t.Fatalf("page %d: %v", pageNumber, err)
		}
		if len(archive) > conversationHistoryByteBudget {
			t.Fatalf("page %d is %d bytes, budget is %d", pageNumber, len(archive), conversationHistoryByteBudget)
		}
		var page MessagePage
		if err := json.Unmarshal([]byte(archive), &page); err != nil {
			t.Fatalf("page %d archive JSON: %v", pageNumber, err)
		}
		if len(page.Messages) == 0 {
			t.Fatalf("page %d is empty before all rows were recovered", pageNumber)
		}
		for index, message := range page.Messages {
			if seen[message.ID] {
				t.Fatalf("message %d repeated on page %d", message.ID, pageNumber)
			}
			seen[message.ID] = true
			if index > 0 && message.ID >= page.Messages[index-1].ID {
				t.Fatalf("page %d is not newest-first: %#v", pageNumber, page.Messages)
			}
			if message.Content == nil || !strings.Contains(*message.Content, "row-") || len(*message.Content) < payloadSize {
				t.Fatalf("message %d was truncated: content length=%d", message.ID, len(valueOrEmpty(message.Content)))
			}
		}
		if page.NextBeforeID == nil {
			break
		}
		next := *page.NextBeforeID
		if next != page.Messages[len(page.Messages)-1].ID {
			t.Fatalf("page %d cursor=%d, last message=%d", pageNumber, next, page.Messages[len(page.Messages)-1].ID)
		}
		if beforeID != 0 && next >= beforeID {
			t.Fatalf("page %d cursor moved forward: previous=%d next=%d", pageNumber, beforeID, next)
		}
		beforeID = next
	}
	if len(seen) != len(allIDs) {
		t.Fatalf("recovered %d of %d rows; seen=%v", len(seen), len(allIDs), seen)
	}
}

func TestConversationHistoryRejectsSingleOversizedRow(t *testing.T) {
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
	chatID := int64(7_707_707_714)
	clearArchiveBoundsChat(t, ctx, pool, chatID)
	defer clearArchiveBoundsChat(t, ctx, pool, chatID)
	memory := NewMemory(pool, nil)
	message, err := memory.AppendOpenRouter(ctx, chatID, openrouter.Message{Role: "user", Content: strings.Repeat("z", conversationHistoryByteBudget)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := memory.ConversationHistory(ctx, chatID, 0, 100); err == nil {
		t.Fatal("oversized archive row was accepted")
	} else if !strings.Contains(err.Error(), strconv.FormatInt(message.ID, 10)) || !strings.Contains(err.Error(), "before_id") {
		t.Fatalf("oversized row error=%v", err)
	}
}

func TestConversationHistoryDefersOversizedMiddleRowError(t *testing.T) {
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
	chatID := int64(7_707_707_715)
	clearArchiveBoundsChat(t, ctx, pool, chatID)
	defer clearArchiveBoundsChat(t, ctx, pool, chatID)
	memory := NewMemory(pool, nil)
	if _, err := memory.AppendOpenRouter(ctx, chatID, openrouter.Message{Role: "user", Content: "old"}); err != nil {
		t.Fatal(err)
	}
	oversized, err := memory.AppendOpenRouter(ctx, chatID, openrouter.Message{Role: "user", Content: strings.Repeat("m", conversationHistoryByteBudget)})
	if err != nil {
		t.Fatal(err)
	}
	newerIDs := make([]int64, 0, 2)
	for _, content := range []string{"newer one", "newer two"} {
		message, err := memory.AppendOpenRouter(ctx, chatID, openrouter.Message{Role: "user", Content: content})
		if err != nil {
			t.Fatal(err)
		}
		newerIDs = append(newerIDs, message.ID)
	}

	archive, err := memory.ConversationHistory(ctx, chatID, 0, 100)
	if err != nil {
		t.Fatalf("newest page failed before oversized row: %v", err)
	}
	var page MessagePage
	if err := json.Unmarshal([]byte(archive), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != len(newerIDs) || page.NextBeforeID == nil {
		t.Fatalf("newest page=%#v, want newer rows and a cursor", page)
	}
	for index, id := range newerIDs {
		if page.Messages[len(newerIDs)-1-index].ID != id {
			t.Fatalf("newest page IDs=%v, want %v", messageIDs(page.Messages), newerIDs)
		}
	}
	if *page.NextBeforeID != newerIDs[0] {
		t.Fatalf("cursor=%d, want oldest newer row %d", *page.NextBeforeID, newerIDs[0])
	}
	if _, err := memory.ConversationHistory(ctx, chatID, *page.NextBeforeID, 100); err == nil || !strings.Contains(err.Error(), strconv.FormatInt(oversized.ID, 10)) {
		t.Fatalf("oversized middle row error=%v, want row ID %d", err, oversized.ID)
	}
}

func messageIDs(messages []Message) []int64 {
	ids := make([]int64, len(messages))
	for index, message := range messages {
		ids[index] = message.ID
	}
	return ids
}

func clearArchiveBoundsChat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, chatID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DELETE FROM agent_conversation_messages WHERE chat_id=$1`, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM agent_context_summaries WHERE chat_id=$1`, chatID); err != nil {
		t.Fatal(err)
	}
}
