package agent

import (
	"context"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
)

type fakeCompleter struct {
	requests  []openrouter.Request
	responses []openrouter.Response
}

func (f *fakeCompleter) Complete(_ context.Context, request openrouter.Request) (openrouter.Response, error) {
	f.requests = append(f.requests, request)
	response := f.responses[0]
	f.responses = f.responses[1:]
	return response, nil
}

type fakeConversation struct{ messages []openrouter.Message }

func (f *fakeConversation) Append(_ context.Context, _ int64, message openrouter.Message) (Message, error) {
	f.messages = append(f.messages, message)
	return Message{ID: int64(len(f.messages)), ChatID: 1, Role: message.Role}, nil
}
func (f *fakeConversation) Context(_ context.Context, _ int64, _ int) ([]openrouter.Message, error) {
	return append([]openrouter.Message(nil), f.messages...), nil
}
func (f *fakeConversation) PromptContext(context.Context, int64) (string, error) { return "", nil }

type fakeTools struct{ calls []string }

func (f *fakeTools) Execute(_ context.Context, _ int64, name string, _ map[string]any, _ string, sandbox bool) (string, error) {
	f.calls = append(f.calls, name)
	if !sandbox {
		return "", errTest("expected sandbox")
	}
	return "sandbox result", nil
}

type errTest string

func (e errTest) Error() string { return string(e) }

type fakeTurnStatus struct{ events []string }

func (f *fakeTurnStatus) Thinking(context.Context, int64, int) {
	f.events = append(f.events, "thinking")
}
func (f *fakeTurnStatus) ToolsStarted(context.Context, int64, []string) {
	f.events = append(f.events, "tools")
}
func (f *fakeTurnStatus) ToolRunning(context.Context, int64, string) {
	f.events = append(f.events, "tool")
}
func (f *fakeTurnStatus) ComposingReply(context.Context, int64) {
	f.events = append(f.events, "composing")
}
func (f *fakeTurnStatus) Complete(context.Context, int64) { f.events = append(f.events, "complete") }

func TestTurnerStoresExactToolSequenceAndReturnsFinalReply(t *testing.T) {
	t.Parallel()
	content := "готово"
	llm := &fakeCompleter{responses: []openrouter.Response{
		{Message: openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "call-1", Type: "function", Function: openrouter.ToolFunction{Name: "list_exercises", Arguments: `{}`}}}}},
		{Message: openrouter.Message{Role: "assistant", Content: content}},
	}}
	conversation := &fakeConversation{}
	tools := &fakeTools{}
	turner := NewTurner(TurnerConfig{Model: func() string { return "provider/model" }, MaxToolIterations: func() int { return 4 }, RecentMessages: func() int { return 10 }, SystemPrompt: func() string { return "system" }}, llm, conversation, tools)

	status := &fakeTurnStatus{}
	result, err := turner.Turn(WithTurnStatus(context.Background(), status), 1, "покажи упражнения", nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply != content {
		t.Fatalf("reply = %q", result.Reply)
	}
	wantRoles := []string{"user", "assistant", "tool", "assistant"}
	if len(conversation.messages) != len(wantRoles) {
		t.Fatalf("messages = %#v", conversation.messages)
	}
	for index, role := range wantRoles {
		if conversation.messages[index].Role != role {
			t.Fatalf("message %d role = %q", index, conversation.messages[index].Role)
		}
	}
	if conversation.messages[1].ToolCalls[0].ID != "call-1" || conversation.messages[2].ToolCallID != "call-1" {
		t.Fatalf("tool-call linkage lost: %#v", conversation.messages)
	}
	if len(tools.calls) != 1 || tools.calls[0] != "list_exercises" {
		t.Fatalf("tool calls = %#v", tools.calls)
	}
	if len(llm.requests) != 2 || len(llm.requests[0].Tools) == 0 {
		t.Fatalf("llm requests = %#v", llm.requests)
	}
	wantStatus := []string{"thinking", "tool", "composing", "complete"}
	if len(status.events) != len(wantStatus) {
		t.Fatalf("status events = %#v", status.events)
	}
	for index, event := range wantStatus {
		if status.events[index] != event {
			t.Fatalf("status events = %#v", status.events)
		}
	}
}

func TestTurnerRejectsUnapprovedMutationBeforeExecutor(t *testing.T) {
	t.Parallel()
	llm := &fakeCompleter{responses: []openrouter.Response{
		{Message: openrouter.Message{Role: "assistant", ToolCalls: []openrouter.ToolCall{{ID: "call-1", Type: "function", Function: openrouter.ToolFunction{Name: "log_workout", Arguments: `{"exercise_name":"Жим","notation":"70 3*10/12"}`}}}}},
		{Message: openrouter.Message{Role: "assistant", Content: "ничего не менял"}},
	}}
	conversation := &fakeConversation{}
	tools := &fakeTools{}
	turner := NewTurner(TurnerConfig{Model: func() string { return "p/m" }, MaxToolIterations: func() int { return 2 }, RecentMessages: func() int { return 10 }, SystemPrompt: func() string { return "system" }}, llm, conversation, tools)
	if _, err := turner.Turn(context.Background(), 1, "покажи тренировку", nil, true); err != nil {
		t.Fatal(err)
	}
	if len(tools.calls) != 0 {
		t.Fatalf("mutating executor was called: %#v", tools.calls)
	}
	if got, _ := conversation.messages[2].Content.(string); got == "" {
		t.Fatalf("denial feedback missing: %#v", conversation.messages[2])
	}
}

func TestUserImagesBecomeMultimodalContent(t *testing.T) {
	t.Parallel()
	llm := &fakeCompleter{responses: []openrouter.Response{{Message: openrouter.Message{Role: "assistant", Content: "вижу"}}}}
	conversation := &fakeConversation{}
	turner := NewTurner(TurnerConfig{Model: func() string { return "p/m" }, MaxToolIterations: func() int { return 1 }, RecentMessages: func() int { return 10 }, SystemPrompt: func() string { return "system" }}, llm, conversation, &fakeTools{})
	if _, err := turner.Turn(context.Background(), 1, "фото", []string{"data:image/png;base64,AA=="}, true); err != nil {
		t.Fatal(err)
	}
	parts, ok := conversation.messages[0].Content.([]openrouter.ContentPart)
	if !ok || len(parts) != 2 || parts[1].ImageURL == nil {
		t.Fatalf("content = %#v", conversation.messages[0].Content)
	}
}

func TestTurnerRetriesNonRussianReplyWithoutTools(t *testing.T) {
	t.Parallel()
	llm := &fakeCompleter{responses: []openrouter.Response{
		{Message: openrouter.Message{Role: "assistant", Content: "作为一个人工智能语言模型"}},
		{Message: openrouter.Message{Role: "assistant", Content: "Готово на русском."}},
	}}
	conversation := &fakeConversation{}
	turner := NewTurner(TurnerConfig{Model: func() string { return "p/m" }, MaxToolIterations: func() int { return 1 }, RecentMessages: func() int { return 10 }, SystemPrompt: func() string { return "system" }}, llm, conversation, &fakeTools{})
	result, err := turner.Turn(context.Background(), 1, "подведи итог", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if result.Reply != "Готово на русском." || len(llm.requests) != 2 {
		t.Fatalf("result=%#v requests=%d", result, len(llm.requests))
	}
	if len(llm.requests[1].Tools) != 0 {
		t.Fatal("language-only retry must not call tools")
	}
}
