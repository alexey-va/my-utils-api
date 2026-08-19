package temporal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/agent"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
)

type failingAgent struct{}

func (failingAgent) Turn(context.Context, int64, string, []string, bool) (agent.TurnResult, error) {
	return agent.TurnResult{}, errors.New("provider unavailable")
}

type recordingMessenger struct{ messages []string }

func (m *recordingMessenger) SendHTMLMessage(_ context.Context, _ int64, text, _ string) (int, error) {
	m.messages = append(m.messages, text)
	return len(m.messages), nil
}
func (*recordingMessenger) SendPhoto(context.Context, int64, []byte, string) error { return nil }

type recordingActivityMetrics struct {
	requests []string
	turns    []string
	llm      []string
}

func (m *recordingActivityMetrics) RecordAgentRequest(path, outcome string) {
	m.requests = append(m.requests, path+":"+outcome)
}
func (m *recordingActivityMetrics) RecordAgentTurn(path, outcome string, _ time.Duration) {
	m.turns = append(m.turns, path+":"+outcome)
}
func (m *recordingActivityMetrics) RecordLLMStep(path string, _ time.Duration) {
	m.llm = append(m.llm, path)
}
func (*recordingActivityMetrics) RecordTool(string, string, string, time.Duration) {}

type activityCompleter struct{}

func (activityCompleter) Complete(context.Context, openrouter.Request) (openrouter.Response, error) {
	return openrouter.Response{Message: openrouter.Message{Role: "assistant", Content: "готово"}}, nil
}

type activityConversation struct{ messages []openrouter.Message }

func (c *activityConversation) Append(_ context.Context, chatID int64, message openrouter.Message) (agent.Message, error) {
	c.messages = append(c.messages, message)
	return agent.Message{ID: int64(len(c.messages)), ChatID: chatID, Role: message.Role}, nil
}
func (c *activityConversation) Context(context.Context, int64, int) ([]openrouter.Message, error) {
	return append([]openrouter.Message(nil), c.messages...), nil
}
func (*activityConversation) PromptContext(context.Context, int64) (string, error) { return "", nil }

type activityStatus struct{}

func (activityStatus) Thinking(context.Context, int64, int)          {}
func (activityStatus) ToolsStarted(context.Context, int64, []string) {}
func (activityStatus) ToolRunning(context.Context, int64, string)    {}
func (activityStatus) ComposingReply(context.Context, int64)         {}
func (activityStatus) Complete(context.Context, int64)               {}

func TestAgentActivityReportsTerminalFailureToTelegram(t *testing.T) {
	t.Parallel()
	messenger := &recordingMessenger{}
	activities := &Activities{Agent: failingAgent{}, Messenger: messenger}
	err := activities.RunAgentTurn(context.Background(), AgentTurnInput{ChatID: 42, UserID: 42, Text: "test", DeliverToTelegram: true})
	if err == nil {
		t.Fatal("turn failure must remain visible to Temporal")
	}
	if len(messenger.messages) != 1 || !strings.Contains(messenger.messages[0], "Не удалось обработать") {
		t.Fatalf("terminal messages = %#v", messenger.messages)
	}
}

func TestAgentActivityRecordsTemporalStartCommand(t *testing.T) {
	t.Parallel()
	messenger := &recordingMessenger{}
	metrics := &recordingActivityMetrics{}
	activities := &Activities{Messenger: messenger, Metrics: metrics}
	if err := activities.RunAgentTurn(context.Background(), AgentTurnInput{ChatID: 42, UserID: 42, Text: "/start", DeliverToTelegram: true}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.requests) != 1 || metrics.requests[0] != "temporal:received" {
		t.Fatalf("request metrics = %#v", metrics.requests)
	}
	if len(metrics.turns) != 1 || metrics.turns[0] != "temporal:start_command" {
		t.Fatalf("turn metrics = %#v", metrics.turns)
	}
	if len(messenger.messages) != 1 {
		t.Fatalf("messages = %#v", messenger.messages)
	}
}

func TestAgentActivityPreservesTemporalMetricsWhenStatusIsAttached(t *testing.T) {
	t.Parallel()
	metrics := &recordingActivityMetrics{}
	turner := agent.NewTurner(agent.TurnerConfig{
		Model: func() string { return "provider/model" }, MaxToolIterations: func() int { return 2 },
		RecentMessages: func() int { return 10 }, SystemPrompt: func() string { return "system" },
	}, activityCompleter{}, &activityConversation{}, nil)
	turner.SetMetrics(metrics)
	activities := &Activities{Agent: turner, Messenger: &recordingMessenger{}, Status: activityStatus{}}
	if err := activities.RunAgentTurn(context.Background(), AgentTurnInput{ChatID: 42, UserID: 42, Text: "покажи план", DeliverToTelegram: true}); err != nil {
		t.Fatal(err)
	}
	if len(metrics.requests) != 1 || metrics.requests[0] != "temporal:received" {
		t.Fatalf("request metrics = %#v", metrics.requests)
	}
	if len(metrics.turns) != 1 || metrics.turns[0] != "temporal:reply" {
		t.Fatalf("turn metrics = %#v", metrics.turns)
	}
	if len(metrics.llm) != 1 || metrics.llm[0] != "temporal" {
		t.Fatalf("LLM metrics = %#v", metrics.llm)
	}
}
