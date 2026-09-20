package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
)

type fakeCompleter struct {
	requests  []openrouter.Request
	responses []openrouter.Response
}

func (f *fakeCompleter) Complete(_ context.Context, request openrouter.Request) (openrouter.Response, error) {
	f.requests = append(f.requests, request)
	if len(f.responses) == 0 {
		return openrouter.Response{}, errTest("unexpected extra completion")
	}
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

type fakeTools struct {
	calls []string
	args  []map[string]any
}

func (f *fakeTools) Execute(_ context.Context, _ int64, name string, args map[string]any, _ string, sandbox bool) (string, error) {
	f.calls = append(f.calls, name)
	copyArgs := make(map[string]any, len(args))
	for key, value := range args {
		copyArgs[key] = value
	}
	f.args = append(f.args, copyArgs)
	if !sandbox {
		return "", errTest("expected sandbox")
	}
	return "Записано: проверенный результат сервиса.", nil
}

type recordedPlanTools struct {
	planCalls    int
	executeCalls int
}

func (f *recordedPlanTools) Execute(context.Context, int64, string, map[string]any, string, bool) (string, error) {
	f.executeCalls++
	return "", errTest("unexpected individual execution")
}

func (f *recordedPlanTools) ExecutePlanRecorded(_ context.Context, _ int64, calls []RecordedToolCall, _ bool) ([]ToolAttempt, error) {
	f.planCalls++
	attempts := make([]ToolAttempt, len(calls))
	for index := range calls {
		result := "Проверено в общей транзакции."
		attempts[index] = ToolAttempt{Result: result, Message: Message{ID: int64(index + 100), Role: "tool"}}
	}
	return attempts, nil
}

type errTest string

func (e errTest) Error() string { return string(e) }

type failingCompleter struct{ err error }

func (f failingCompleter) Complete(context.Context, openrouter.Request) (openrouter.Response, error) {
	return openrouter.Response{}, f.err
}

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

func testTurner(llm Completer, c Conversation, tools ToolExecutor) *AgentTurner {
	return NewTurner(TurnerConfig{Model: func() string { return "p/m" }, MaxToolIterations: func() int { return 4 }, RecentMessages: func() int { return 2 }, SystemPrompt: func() string { return "system" }}, llm, c, tools)
}
func TestTurnerStoresExactToolSequenceAndReturnsFinalReply(t *testing.T) {
	llm := &fakeCompleter{responses: []openrouter.Response{
		decisionResponse("read", "", plannedAction{Tool: "list_exercises", Arguments: map[string]any{}}), decisionResponse("read", "Список проверен."),
	}}
	c := &fakeConversation{}
	tools := &fakeTools{}
	result, err := testTurner(llm, c, tools).Turn(context.Background(), 1, "покажи упражнения", nil, true)
	if err != nil || result.Reply != "Список проверен." {
		t.Fatalf("result=%+v err=%v", result, err)
	}
	for i, role := range []string{"user", "assistant", "tool", "assistant"} {
		if c.messages[i].Role != role {
			t.Fatalf("messages=%+v", c.messages)
		}
	}
	if c.messages[1].ToolCalls[0].ID != c.messages[2].ToolCallID {
		t.Fatal("tool linkage lost")
	}
	if len(llm.requests) != 2 || llm.requests[0].ToolChoice == nil {
		t.Fatal("structured protocol not requested")
	}
}

func TestTurnerUsesRecordedPlanWithoutAppendingDuplicateReceipts(t *testing.T) {
	llm := &fakeCompleter{responses: []openrouter.Response{
		decisionResponse("read", "", plannedAction{Tool: "list_exercises", Arguments: map[string]any{}}),
		decisionResponse("read", "Список проверен."),
	}}
	tools := &recordedPlanTools{}
	c := &fakeConversation{}
	result, err := testTurner(llm, c, tools).Turn(context.Background(), 1, "покажи упражнения", nil, true)
	if err != nil || result.Reply != "Список проверен." || tools.planCalls != 1 || tools.executeCalls != 0 {
		t.Fatalf("result=%+v tools=%+v err=%v", result, tools, err)
	}
	if len(c.messages) != 3 || len(result.Messages) != 4 || result.Messages[2].Role != "tool" {
		t.Fatalf("conversation=%+v result=%+v", c.messages, result.Messages)
	}
}
func TestTurnerWritesCorrectJSONDespiteInterveningWords(t *testing.T) {
	text := "Бабочка 46кг сегодня 12/15"
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("write", "", plannedAction{Tool: "log_workout", RequestQuote: text, DataQuote: text, Arguments: map[string]any{"exercise_name": "Бабочка", "date": "2026-09-11", "notation": "46 3*12/15"}})}}
	c := &fakeConversation{}
	tools := &fakeTools{}
	result, err := testTurner(llm, c, tools).Turn(context.Background(), 1, text, nil, true)
	if err != nil || len(tools.calls) != 1 || tools.args[0]["notation"] != "46 3*12/15" {
		t.Fatalf("calls=%v err=%v", tools.args, err)
	}
	if result.Reply != "Записано: проверенный результат сервиса." || len(llm.requests) != 1 {
		t.Fatalf("receipt fabricated or extra completion: %+v", result)
	}
	var receipt map[string]any
	if err := json.Unmarshal([]byte(contentString(c.messages[2].Content)), &receipt); err != nil || receipt["ok"] != true {
		t.Fatalf("receipt=%v err=%v", receipt, err)
	}
}

func TestTurnerPromptDefaultsEqualPairToThreeWorkingSetsAndMax(t *testing.T) {
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("clarify", "Какое упражнение?")}}
	_, err := testTurner(llm, &fakeConversation{}, &fakeTools{}).Turn(context.Background(), 1, "86 10/10", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(llm.requests) != 1 {
		t.Fatalf("requests = %d, want 1", len(llm.requests))
	}
	system, _ := llm.requests[0].Messages[0].Content.(string)
	if !strings.Contains(system, "A/A по умолчанию означает 3*A/A") {
		t.Fatalf("system prompt does not define equal-pair shorthand: %q", system)
	}
	if strings.Contains(system, "одинаковые A/A — два явно перечисленных сета") {
		t.Fatalf("system prompt still contains the conflicting equal-pair rule: %q", system)
	}
}

func TestTurnerExecutesDefaultEqualPairExpansion(t *testing.T) {
	text := "Пулл даун 86 10/10"
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("write", "", plannedAction{
		Tool: "log_workout", RequestQuote: text, DataQuote: text,
		Arguments: map[string]any{"exercise_name": "Пулл даун", "notation": "86 3*10/10"},
	})}}
	tools := &fakeTools{}
	result, err := testTurner(llm, &fakeConversation{}, tools).Turn(context.Background(), 1, text, nil, true)
	if err != nil || len(tools.calls) != 1 || tools.args[0]["notation"] != "86 3*10/10" {
		t.Fatalf("result=%+v calls=%v err=%v", result, tools.args, err)
	}
}

func TestTurnerExecutesExplicitTwoSetOverride(t *testing.T) {
	text := "Пулл даун 86, ровно 2 подхода по 10, без max: 10/10"
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("write", "", plannedAction{
		Tool: "log_workout", RequestQuote: text, DataQuote: text,
		Arguments: map[string]any{"exercise_name": "Пулл даун", "notation": "86 10/10"},
	})}}
	tools := &fakeTools{}
	result, err := testTurner(llm, &fakeConversation{}, tools).Turn(context.Background(), 1, text, nil, true)
	if err != nil || len(tools.calls) != 1 || tools.args[0]["notation"] != "86 10/10" {
		t.Fatalf("result=%+v calls=%v err=%v", result, tools.args, err)
	}
}

func TestTurnerAllowsContinuationWithoutMagicVerb(t *testing.T) {
	for _, text := range []string{"Да", "Бабочка на грудь", "22 тогда"} {
		t.Run(text, func(t *testing.T) {
			a := plannedAction{Tool: "log_workout", RequestQuote: text, DataQuote: "Плечи 22.5 10/10", Arguments: map[string]any{"exercise_name": "Плечи", "notation": "22.5 10/10"}}
			if text == "22 тогда" {
				a.Arguments["notation"] = "22 10/10"
			}
			llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("write", "", a)}}
			c := &fakeConversation{messages: []openrouter.Message{{Role: "user", Content: a.DataQuote}, {Role: "assistant", Content: "Не удалось записать; уточни вес."}}}
			tools := &fakeTools{}
			_, err := testTurner(llm, c, tools).Turn(context.Background(), 1, text, nil, true)
			if err != nil || len(tools.calls) != 1 {
				t.Fatalf("calls=%v err=%v", tools.calls, err)
			}
		})
	}
}
func TestReadOnlyDecisionCannotEscalateToWrite(t *testing.T) {
	bad := decisionResponse("write", "", plannedAction{Tool: "delete_workout", Arguments: map[string]any{"exercise_name": "Жим"}, RequestQuote: "покажи"})
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("read", "", plannedAction{Tool: "get_days", Arguments: map[string]any{}}), bad, bad}}
	tools := &fakeTools{}
	_, err := testTurner(llm, &fakeConversation{}, tools).Turn(context.Background(), 1, "покажи", nil, true)
	if err != nil || len(tools.calls) != 1 || tools.calls[0] != "get_days" {
		t.Fatalf("calls=%v err=%v", tools.calls, err)
	}
}

func TestTurnerLimitsActionsAcrossSteps(t *testing.T) {
	reads := make([]plannedAction, maxTurnActions)
	for index := range reads {
		reads[index] = plannedAction{Tool: "get_days", Arguments: map[string]any{"days": fmt.Sprintf("day-%d", index)}}
	}
	llm := &fakeCompleter{responses: []openrouter.Response{
		decisionResponse("write", "", reads...),
		decisionResponse("write", "", plannedAction{Tool: "create_exercise", RequestQuote: "запиши", Arguments: map[string]any{"name": "Новое"}}),
	}}
	tools := &fakeTools{}
	result, err := testTurner(llm, &fakeConversation{}, tools).Turn(context.Background(), 1, "запиши", nil, true)
	if err != nil || !strings.Contains(result.Reply, "не более 20 действий") || len(tools.calls) != maxTurnActions || len(llm.requests) != 2 {
		t.Fatalf("result=%+v calls=%d requests=%d err=%v", result, len(tools.calls), len(llm.requests), err)
	}
}

type selectiveFailureTools struct{ calls []string }

func (f *selectiveFailureTools) Execute(_ context.Context, _ int64, name string, args map[string]any, _ string, _ bool) (string, error) {
	f.calls = append(f.calls, name)
	if args["exercise_name"] == "Плечи" {
		return "", errTest("база временно недоступна")
	}
	return "Записано: Тяга — 55 кг.", nil
}
func TestPartialFailureReturnsEachActualReceiptWithoutReplay(t *testing.T) {
	text := "Плечи 22.5 10/10 тяга 55 8/12"
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("write", "",
		plannedAction{Tool: "log_workout", RequestQuote: "Плечи 22.5 10/10", DataQuote: "Плечи 22.5 10/10", Arguments: map[string]any{"exercise_name": "Плечи", "notation": "22.5 10/10"}},
		plannedAction{Tool: "log_workout", RequestQuote: "тяга 55 8/12", DataQuote: "тяга 55 8/12", Arguments: map[string]any{"exercise_name": "Тяга", "notation": "55 3*8/12"}},
	)}}
	c := &fakeConversation{}
	tools := &selectiveFailureTools{}
	result, err := testTurner(llm, c, tools).Turn(context.Background(), 1, text, nil, true)
	if err != nil || len(tools.calls) != 2 || !strings.Contains(result.Reply, "база временно недоступна") || !strings.Contains(result.Reply, "Записано: Тяга") {
		t.Fatalf("result=%+v calls=%v err=%v", result, tools.calls, err)
	}
	if len(llm.requests) != 1 || c.messages[len(c.messages)-1].Role != "assistant" {
		t.Fatal("unexpected replay or missing final receipt")
	}
}
func TestToolFailureNeverReturnsDone(t *testing.T) {
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("write", "", plannedAction{Tool: "send_progress_chart", RequestQuote: "график", Arguments: map[string]any{"exercise_name": "Плечи"}})}}
	result, err := testTurner(llm, &fakeConversation{}, &selectiveFailureTools{}).Turn(context.Background(), 1, "график", nil, true)
	if err != nil || strings.Contains(result.Reply, "Готово") || !strings.Contains(result.Reply, "Не удалось") {
		t.Fatalf("reply=%s err=%v", result.Reply, err)
	}
}
func TestMalformedPlanNeverExecutesPartialActions(t *testing.T) {
	bad := decisionResponse("write", "", plannedAction{Tool: "create_exercise", RequestQuote: "запиши", Arguments: map[string]any{"name": "Новое"}}, plannedAction{Tool: "log_workout", RequestQuote: "запиши", Arguments: map[string]any{"exercise_name": "Новое", "notation": 42}})
	llm := &fakeCompleter{responses: []openrouter.Response{bad, bad}}
	tools := &fakeTools{}
	_, err := testTurner(llm, &fakeConversation{}, tools).Turn(context.Background(), 1, "запиши", nil, true)
	if err != nil || len(tools.calls) != 0 {
		t.Fatalf("executed malformed plan: %v err=%v", tools.calls, err)
	}
}

func TestProviderPaymentErrorIsNotReportedAsParsingFailure(t *testing.T) {
	result, err := testTurner(failingCompleter{err: &openrouter.APIError{StatusCode: 402, Body: "insufficient credits"}}, &fakeConversation{}, &fakeTools{}).Turn(context.Background(), 1, "запиши ноги", nil, true)
	if err != nil || !strings.Contains(result.Reply, "баланса") || strings.Contains(result.Reply, "разобрать") {
		t.Fatalf("reply=%q err=%v", result.Reply, err)
	}
}
func TestInvalidRussianReplyIsRepairedWithoutActions(t *testing.T) {
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("read", "output truncated..."), decisionResponse("read", "Данные проверены.")}}
	result, err := testTurner(llm, &fakeConversation{}, &fakeTools{}).Turn(context.Background(), 1, "повтори", nil, true)
	if err != nil || result.Reply != "Данные проверены." {
		t.Fatalf("result=%+v err=%v", result, err)
	}
}
func TestUserImagesBecomeMultimodalContent(t *testing.T) {
	llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("clarify", "Что записать с фото?")}}
	c := &fakeConversation{}
	_, err := testTurner(llm, c, &fakeTools{}).Turn(context.Background(), 1, "фото", []string{"data:image/png;base64,AA=="}, true)
	parts, ok := c.messages[0].Content.([]openrouter.ContentPart)
	if err != nil || !ok || len(parts) != 2 || parts[1].ImageURL == nil {
		t.Fatalf("parts=%v err=%v", parts, err)
	}
}

func TestTurnerAcceptsResolvedWeightAbsentFromUserText(t *testing.T) {
	for _, text := range []string{"Жим на 3 кг больше прошлого 10/10", "Как раньше, но на три килограмма больше, десять на десять"} {
		t.Run(text, func(t *testing.T) {
			llm := &fakeCompleter{responses: []openrouter.Response{decisionResponse("write", "", plannedAction{
				Tool: "log_workout", RequestQuote: text, Arguments: map[string]any{"exercise_name": "Жим", "notation": "45 3*10/10"},
			})}}
			conversation := &fakeConversation{messages: []openrouter.Message{{Role: "assistant", Content: "Последний подтверждённый вес жима: 42 кг."}}}
			tools := &fakeTools{}
			result, err := testTurner(llm, conversation, tools).Turn(context.Background(), 1, text, nil, true)
			if err != nil || len(tools.calls) != 1 || tools.args[0]["notation"] != "45 3*10/10" || len(llm.requests) != 1 {
				t.Fatalf("result=%+v calls=%v err=%v", result, tools.calls, err)
			}
		})
	}
}
