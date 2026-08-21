package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
)

type Completer interface {
	Complete(context.Context, openrouter.Request) (openrouter.Response, error)
}

type Conversation interface {
	Append(context.Context, int64, openrouter.Message) (Message, error)
	Context(context.Context, int64, int) ([]openrouter.Message, error)
	PromptContext(context.Context, int64) (string, error)
}

type ToolExecutor interface {
	Execute(context.Context, int64, string, map[string]any, string, bool) (string, error)
}

type MetricsRecorder interface {
	RecordAgentRequest(string, string)
	RecordAgentTurn(string, string, time.Duration)
	RecordLLMStep(string, time.Duration)
	RecordTool(string, string, string, time.Duration)
}

type TurnerConfig struct {
	Model             func() string
	MaxToolIterations func() int
	RecentMessages    func() int
	SystemPrompt      func() string
	TemporalEnabled   bool
}

type AgentTurner struct {
	config       TurnerConfig
	client       Completer
	conversation Conversation
	tools        ToolExecutor
	metrics      MetricsRecorder
}

func NewTurner(config TurnerConfig, client Completer, conversation Conversation, tools ToolExecutor) *AgentTurner {
	return &AgentTurner{config: config, client: client, conversation: conversation, tools: tools}
}

func (t *AgentTurner) SetMetrics(metrics MetricsRecorder) { t.metrics = metrics }

func (t *AgentTurner) Turn(ctx context.Context, chatID int64, content string, images []string, sandbox bool) (TurnResult, error) {
	started := time.Now()
	path, outcome := metricsPath(ctx, sandbox), "error"
	status := statusFromContext(ctx)
	if sandbox {
		status = nil
	}
	if status != nil {
		defer status.Complete(ctx, chatID)
	}
	if t.metrics != nil {
		t.metrics.RecordAgentRequest(path, "received")
		defer func() { t.metrics.RecordAgentTurn(path, outcome, time.Since(started)) }()
	}
	content = strings.TrimSpace(content)
	if content == "" && len(images) == 0 {
		return TurnResult{}, badRequest("Нужен текст или хотя бы одно изображение.")
	}
	userMessage := openrouter.Message{Role: "user", Content: content}
	if len(images) > 0 {
		parts := []openrouter.ContentPart{}
		if content != "" {
			parts = append(parts, openrouter.ContentPart{Type: "text", Text: content})
		}
		for _, image := range normalizeImages(images) {
			parts = append(parts, openrouter.ContentPart{Type: "image_url", ImageURL: &openrouter.ImageURL{URL: image}})
		}
		userMessage.Content = parts
	}
	first, err := t.conversation.Append(ctx, chatID, userMessage)
	if err != nil {
		return TurnResult{}, err
	}
	appended := []Message{first}

	maxIterations := t.config.MaxToolIterations()
	if maxIterations < 1 {
		maxIterations = 1
	}
	for iteration := 0; iteration < maxIterations; iteration++ {
		if status != nil {
			if iteration == 0 {
				status.Thinking(ctx, chatID, iteration+1)
			} else {
				status.ComposingReply(ctx, chatID)
			}
		}
		memory, err := t.conversation.Context(ctx, chatID, t.config.RecentMessages())
		if err != nil {
			return TurnResult{}, err
		}
		promptContext, err := t.conversation.PromptContext(ctx, chatID)
		if err != nil {
			return TurnResult{}, err
		}
		system := strings.TrimSpace(t.config.SystemPrompt())
		if promptContext != "" {
			system += "\n\n" + promptContext
		}
		messages := make([]openrouter.Message, 0, len(memory)+1)
		messages = append(messages, openrouter.Message{Role: "system", Content: system})
		messages = append(messages, memory...)
		llmStarted := time.Now()
		response, err := t.client.Complete(ctx, openrouter.Request{Model: t.config.Model(), Messages: messages, Tools: ToolDefinitions(t.config.TemporalEnabled)})
		if t.metrics != nil {
			t.metrics.RecordLLMStep(path, time.Since(llmStarted))
		}
		if err != nil {
			return TurnResult{}, fmt.Errorf("OpenRouter completion: %w", err)
		}
		assistant := response.Message
		assistant.Role = "assistant"
		storedAssistant, err := t.conversation.Append(ctx, chatID, assistant)
		if err != nil {
			return TurnResult{}, err
		}
		appended = append(appended, storedAssistant)
		if len(assistant.ToolCalls) == 0 {
			reply := strings.TrimSpace(contentString(assistant.Content))
			if reply == "" {
				return TurnResult{}, errors.New("OpenRouter returned an empty assistant reply")
			}
			if LooksInvalidForRussianUser(reply) {
				retryMessages := append([]openrouter.Message(nil), messages...)
				retryMessages = append(retryMessages, openrouter.Message{Role: "user", Content: "Ответь только на русском. Кратко подведи итог записей для пользователя."})
				retryStarted := time.Now()
				retry, retryErr := t.client.Complete(ctx, openrouter.Request{Model: t.config.Model(), Messages: retryMessages})
				if t.metrics != nil {
					t.metrics.RecordLLMStep(path, time.Since(retryStarted))
				}
				if retryErr != nil {
					return TurnResult{}, fmt.Errorf("OpenRouter Russian reply retry: %w", retryErr)
				}
				retryReply := strings.TrimSpace(contentString(retry.Message.Content))
				if retryReply != "" && !LooksInvalidForRussianUser(retryReply) {
					retry.Message.Role = "assistant"
					storedRetry, appendErr := t.conversation.Append(ctx, chatID, retry.Message)
					if appendErr != nil {
						return TurnResult{}, appendErr
					}
					appended = append(appended, storedRetry)
					reply = retryReply
				}
			}
			outcome = "reply"
			return TurnResult{Reply: reply, Messages: appended}, nil
		}
		if status != nil && len(assistant.ToolCalls) > 1 {
			names := make([]string, len(assistant.ToolCalls))
			for index, call := range assistant.ToolCalls {
				names[index] = call.Function.Name
			}
			status.ToolsStarted(ctx, chatID, names)
		}
		allImmediate := true
		for _, call := range assistant.ToolCalls {
			if status != nil {
				status.ToolRunning(ctx, chatID, call.Function.Name)
			}
			result := ""
			var args map[string]any
			if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
				result = `{"ok":false,"error":"Неверный arguments JSON","hint":"Исправь JSON и вызови инструмент снова."}`
			} else if !MutationAllowed(call.Function.Name, content) {
				result = `{"ok":false,"error":"Текущее сообщение — только чтение: нет явной команды или данных для изменения.","hint":"Ответь без изменения данных."}`
			} else {
				GroundToolArguments(call.Function.Name, args, content)
				result, err = t.tools.Execute(ctx, chatID, call.Function.Name, args, content, sandbox)
				if err != nil {
					result = fmt.Sprintf(`{"ok":false,"error":%q}`, err.Error())
				}
			}
			toolMessage := openrouter.Message{Role: "tool", Content: result, ToolCallID: call.ID, Name: call.Function.Name}
			storedTool, appendErr := t.conversation.Append(ctx, chatID, toolMessage)
			if appendErr != nil {
				return TurnResult{}, appendErr
			}
			appended = append(appended, storedTool)
			allImmediate = allImmediate && isImmediateReturn(call.Function.Name)
		}
		if allImmediate {
			outcome = "immediate_tool"
			return TurnResult{Reply: "Готово.", Messages: appended}, nil
		}
	}
	if status != nil {
		status.ComposingReply(ctx, chatID)
	}
	outcome = "tool_limit"
	return TurnResult{Reply: "Слишком много шагов с инструментами, попробуй короче.", Messages: appended}, nil
}

func contentString(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case nil:
		return ""
	default:
		raw, _ := json.Marshal(value)
		return string(raw)
	}
}
