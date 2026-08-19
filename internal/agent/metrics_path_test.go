package agent

import (
	"context"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
)

type recordingMetrics struct {
	requests []string
	turns    []string
	llm      []string
	tools    []string
}

func (m *recordingMetrics) RecordAgentRequest(path, outcome string) {
	m.requests = append(m.requests, path+":"+outcome)
}
func (m *recordingMetrics) RecordAgentTurn(path, outcome string, _ time.Duration) {
	m.turns = append(m.turns, path+":"+outcome)
}
func (m *recordingMetrics) RecordLLMStep(path string, _ time.Duration) {
	m.llm = append(m.llm, path)
}
func (m *recordingMetrics) RecordTool(path, tool, status string, _ time.Duration) {
	m.tools = append(m.tools, path+":"+tool+":"+status)
}

func TestTemporalMetricsPathFlowsThroughTurnAndTools(t *testing.T) {
	metrics := &recordingMetrics{}
	llm := &fakeCompleter{responses: []openrouter.Response{{Message: openrouter.Message{Role: "assistant", Content: "готово"}}}}
	turner := NewTurner(
		TurnerConfig{
			Model: func() string { return "p/m" }, MaxToolIterations: func() int { return 2 },
			RecentMessages: func() int { return 10 }, SystemPrompt: func() string { return "system" },
		},
		llm, &fakeConversation{}, &fakeTools{},
	)
	turner.SetMetrics(metrics)
	ctx := WithMetricsPath(context.Background(), "temporal")
	if _, err := turner.Turn(ctx, 42, "покажи план", nil, false); err != nil {
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

	delivery := &recordingDelivery{}
	tools := NewToolService(nil, nil, nil, nil, nil, delivery)
	tools.SetMetrics(metrics)
	if _, err := tools.Execute(ctx, 42, "send_rich_message", map[string]any{"text": "ok"}, "отправь", false); err != nil {
		t.Fatal(err)
	}
	if len(metrics.tools) != 1 || metrics.tools[0] != "temporal:send_rich_message:success" {
		t.Fatalf("tool metrics = %#v", metrics.tools)
	}

	failingTools := NewToolService(nil, nil, nil, nil, nil, nil)
	failingTools.SetMetrics(metrics)
	if _, err := failingTools.Execute(ctx, 42, "send_rich_message", map[string]any{"text": "ok"}, "отправь", false); err == nil {
		t.Fatal("expected unavailable Telegram error")
	}
	if len(metrics.tools) != 2 || metrics.tools[1] != "temporal:send_rich_message:error" {
		t.Fatalf("failed tool metrics = %#v", metrics.tools)
	}
}

func TestSandboxMetricsPathCannotBeOverridden(t *testing.T) {
	if got := metricsPath(WithMetricsPath(context.Background(), "temporal"), true); got != "sandbox" {
		t.Fatalf("sandbox metrics path = %q", got)
	}
}
