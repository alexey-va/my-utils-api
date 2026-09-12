package agent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestContextUsesRecentUserTurnsWithCompleteToolGroups(t *testing.T) {
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
	chatID := int64(7_707_707_709)
	clearMemoryHistoryChat(t, ctx, pool, chatID)
	defer clearMemoryHistoryChat(t, ctx, pool, chatID)
	memory := NewMemory(pool, nil)
	appendMessage := func(message openrouter.Message) int64 {
		stored, err := memory.AppendOpenRouter(ctx, chatID, message)
		if err != nil {
			t.Fatal(err)
		}
		return stored.ID
	}
	appendMessage(openrouter.Message{Role: "user", Content: "old turn"})
	secondUserID := appendMessage(openrouter.Message{Role: "user", Content: "second turn"})
	secondAssistantID := appendMessage(openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "call-2", Type: "function", Function: openrouter.ToolFunction{Name: "get_progress", Arguments: `{}`}}}})
	secondToolID := appendMessage(openrouter.Message{Role: "tool", ToolCallID: "call-2", Name: "get_progress", Content: "tool result 2"})
	appendMessage(openrouter.Message{Role: "assistant", Content: "second reply"})
	appendMessage(openrouter.Message{Role: "user", Content: "latest turn"})
	appendMessage(openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "call-3", Type: "function", Function: openrouter.ToolFunction{Name: "get_progress", Arguments: `{}`}}}})
	appendMessage(openrouter.Message{Role: "tool", ToolCallID: "call-3", Name: "get_progress", Content: "tool result 3"})
	appendMessage(openrouter.Message{Role: "assistant", Content: "latest reply"})
	if _, err := pool.Exec(ctx, `UPDATE agent_conversation_messages SET is_compacted=true WHERE id=ANY($1::bigint[])`, []int64{secondUserID, secondAssistantID, secondToolID}); err != nil {
		t.Fatal(err)
	}

	got, err := memory.Context(ctx, chatID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 8 {
		t.Fatalf("context has %d messages, want the two complete user turns", len(got))
	}
	if got[0].Role != "user" || !strings.Contains(contentString(got[0].Content), "second turn") {
		t.Fatalf("context starts with %#v", got[0])
	}
	if len(got[1].ToolCalls) != 1 || got[1].ToolCalls[0].ID != "call-2" || got[2].ToolCallID != "call-2" {
		t.Fatalf("second tool group was not preserved: %#v", got[:4])
	}
	if !strings.Contains(contentString(got[7].Content), "latest reply") {
		t.Fatalf("context tail = %#v", got[7])
	}
	for _, message := range got {
		if strings.Contains(contentString(message.Content), "old turn") {
			t.Fatal("older user turn leaked into context")
		}
	}
	all, err := memory.Context(ctx, chatID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 9 {
		t.Fatalf("context with a larger limit has %d messages, want all 3 user turns", len(all))
	}
}

func TestConversationHistoryReturnsBoundedStructuredArchive(t *testing.T) {
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
	chatID := int64(7_707_707_710)
	otherChatID := chatID + 1
	clearMemoryHistoryChat(t, ctx, pool, chatID)
	clearMemoryHistoryChat(t, ctx, pool, otherChatID)
	defer clearMemoryHistoryChat(t, ctx, pool, chatID)
	defer clearMemoryHistoryChat(t, ctx, pool, otherChatID)
	memory := NewMemory(pool, nil)
	appendMessage := func(id int64, message openrouter.Message) Message {
		stored, err := memory.AppendOpenRouter(ctx, id, message)
		if err != nil {
			t.Fatal(err)
		}
		return stored
	}
	user := appendMessage(chatID, openrouter.Message{Role: "user", Content: "archive user"})
	call := appendMessage(chatID, openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "archive-call", Type: "function", Function: openrouter.ToolFunction{Name: "get_progress", Arguments: `{"exercise":"Присед"}`}}}})
	tool := appendMessage(chatID, openrouter.Message{Role: "tool", ToolCallID: "archive-call", Name: "get_progress", Content: "archive result"})
	appendMessage(chatID, openrouter.Message{Role: "assistant", Content: "archive reply"})
	hidden := appendMessage(chatID, openrouter.Message{Role: "user", Content: "hidden"})
	appendMessage(otherChatID, openrouter.Message{Role: "user", Content: "other chat"})
	if _, err := memory.ExcludeMessage(ctx, hidden.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_conversation_messages SET is_compacted=true WHERE id=$1`, call.ID); err != nil {
		t.Fatal(err)
	}

	archive, err := memory.ConversationHistory(ctx, chatID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var page MessagePage
	if err := json.Unmarshal([]byte(archive), &page); err != nil {
		t.Fatalf("archive JSON = %q: %v", archive, err)
	}
	if len(page.Messages) != 4 {
		t.Fatalf("archive has %d messages, want 4 visible rows", len(page.Messages))
	}
	byID := make(map[int64]Message, len(page.Messages))
	for _, message := range page.Messages {
		byID[message.ID] = message
		if message.ChatID != chatID || message.CreatedAt.IsZero() || message.RawJSON == "" {
			t.Fatalf("archive message lost exact row metadata: %#v", message)
		}
	}
	if _, ok := byID[hidden.ID]; ok {
		t.Fatal("manually excluded message returned in conversation history")
	}
	if got, ok := byID[call.ID]; !ok || !got.Compacted || len(got.ToolCalls) != 1 || got.ToolCalls[0].ID != "archive-call" {
		t.Fatalf("compacted assistant tool call = %#v", got)
	}
	if got, ok := byID[tool.ID]; !ok || got.ToolCallID == nil || *got.ToolCallID != "archive-call" {
		t.Fatalf("tool result = %#v", got)
	}
	if got, ok := byID[user.ID]; !ok || got.Role != "user" {
		t.Fatalf("user archive row = %#v", got)
	}

	var newestID int64
	for index := 0; index < 101; index++ {
		newestID = appendMessage(chatID, openrouter.Message{Role: "user", Content: "bounded"}).ID
	}
	archive, err = memory.ConversationHistory(ctx, chatID, 0, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(archive), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 100 || page.NextBeforeID == nil || page.Messages[0].ID != newestID {
		t.Fatalf("bounded archive = count %d next=%v first=%d newest=%d", len(page.Messages), page.NextBeforeID, page.Messages[0].ID, newestID)
	}
}

func clearMemoryHistoryChat(t *testing.T, ctx context.Context, pool *pgxpool.Pool, chatID int64) {
	t.Helper()
	if _, err := pool.Exec(ctx, `DELETE FROM agent_conversation_messages WHERE chat_id=$1`, chatID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM agent_context_summaries WHERE chat_id=$1`, chatID); err != nil {
		t.Fatal(err)
	}
}
