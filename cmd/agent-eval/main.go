// agent-eval runs opt-in model checks against disposable local sandbox chats.
// It has no Telegram adapter and never imports production conversation fixtures.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/alexey-va/my-utils-api/internal/agent"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/alexey-va/my-utils-api/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	"time"
)

type scenario struct {
	Name    string          `json:"name"`
	Fixture []fixtureAction `json:"fixture"`
	Turns   []string        `json:"turns"`
}
type fixtureAction struct {
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}
type output struct {
	Name   string             `json:"name"`
	ChatID int64              `json:"chat_id"`
	Turns  []agent.TurnResult `json:"turns"`
	States []json.RawMessage  `json:"states"`
	Error  string             `json:"error,omitempty"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) != 2 {
		return fmt.Errorf("usage: agent-eval synthetic-scenarios.json; set AGENT_EVAL_DATABASE_URL, AGENT_EVAL_MODEL, AGENT_EVAL_BASE_URL and AGENT_EVAL_API_KEY")
	}
	var scenarios []scenario
	raw, err := os.ReadFile(os.Args[1])
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, &scenarios); err != nil {
		return err
	}
	ctx := context.Background()
	databaseURL := os.Getenv("AGENT_EVAL_DATABASE_URL")
	parsedURL, err := url.Parse(databaseURL)
	if err != nil || databaseURL == "" {
		return fmt.Errorf("AGENT_EVAL_DATABASE_URL must explicitly select a disposable local database")
	}
	switch parsedURL.Hostname() {
	case "localhost", "127.0.0.1", "::1", "host.docker.internal":
	default:
		return fmt.Errorf("agent-eval only accepts a local database")
	}
	if os.Getenv("AGENT_EVAL_MODEL") == "" || os.Getenv("AGENT_EVAL_API_KEY") == "" {
		return fmt.Errorf("AGENT_EVAL_MODEL and AGENT_EVAL_API_KEY are required")
	}
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	// Explicit opt-in only; ordinary go test never calls a real model.
	client, err := openrouter.New(openrouter.Config{APIKey: os.Getenv("AGENT_EVAL_API_KEY"), BaseURL: os.Getenv("AGENT_EVAL_BASE_URL"), MaxAttempts: 1, Timeout: 3 * time.Minute})
	if err != nil {
		return err
	}
	memory := agent.NewMemory(pool, nil)
	tools := agent.NewToolService(pool, nil, nil, memory, nil, nil)
	var prompt string
	for _, def := range settings.AppCatalog(nil).Definitions() {
		if def.Key == settings.AgentSystemPrompt {
			if err := json.Unmarshal(def.Default, &prompt); err != nil {
				return err
			}
		}
	}
	turner := agent.NewTurner(agent.TurnerConfig{Model: func() string { return os.Getenv("AGENT_EVAL_MODEL") }, MaxToolIterations: func() int { return 6 }, RecentMessages: func() int { return 10 }, SystemPrompt: func() string { return prompt }}, client, agent.NewContextualConversation(memory, nil, nil, nil, nil, nil, nil), tools)
	failed := 0
	for _, s := range scenarios {
		chat, err := memory.CreateTestChat(ctx, "synthetic eval: "+s.Name)
		if err != nil {
			return err
		}
		// Also clean up on an early error; the final delete below is idempotent.
		defer func() { _ = memory.DeleteTestChat(context.Background(), chat.ID) }()
		report := output{Name: s.Name, ChatID: chat.MemoryChatID}
		for _, f := range s.Fixture {
			if _, err := tools.Execute(ctx, chat.MemoryChatID, f.Tool, f.Arguments, "synthetic fixture", true); err != nil {
				report.Error = err.Error()
				break
			}
		}
		for _, text := range s.Turns {
			if report.Error != "" {
				break
			}
			turn, err := turner.Turn(ctx, chat.MemoryChatID, text, nil, true)
			if err != nil {
				report.Error = err.Error()
				break
			}
			report.Turns = append(report.Turns, turn)
			var state string
			if err := pool.QueryRow(ctx, "SELECT state_json FROM agent_test_sandbox_states WHERE memory_chat_id=$1", chat.MemoryChatID).Scan(&state); err != nil {
				return err
			}
			report.States = append(report.States, json.RawMessage(state))
		}
		if err := json.NewEncoder(os.Stdout).Encode(report); err != nil {
			return err
		}
		if report.Error != "" {
			failed++
		}
		if err := memory.DeleteTestChat(ctx, chat.ID); err != nil {
			return err
		}
	}
	if failed > 0 {
		return fmt.Errorf("%d synthetic scenarios failed; inspect JSONL output", failed)
	}
	return nil
}
