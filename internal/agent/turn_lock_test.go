package agent

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/openrouter"
)

type turnLockEvent struct {
	chatID  int64
	role    string
	content string
}

type turnLockConversation struct {
	mu     sync.Mutex
	nextID int64
	events []turnLockEvent
}

func (c *turnLockConversation) Append(_ context.Context, chatID int64, message openrouter.Message) (Message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextID++
	c.events = append(c.events, turnLockEvent{chatID: chatID, role: message.Role, content: contentString(message.Content)})
	return Message{ID: c.nextID, ChatID: chatID, Role: message.Role}, nil
}

func (*turnLockConversation) Context(context.Context, int64, int) ([]openrouter.Message, error) {
	return nil, nil
}

func (*turnLockConversation) PromptContext(context.Context, int64) (string, error) {
	return "", nil
}

func (c *turnLockConversation) snapshot() []turnLockEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]turnLockEvent(nil), c.events...)
}

type turnLockCompleter struct {
	mu            sync.Mutex
	calls         int
	firstStarted  chan struct{}
	secondStarted chan struct{}
	releaseFirst  chan struct{}
}

func (c *turnLockCompleter) Complete(ctx context.Context, _ openrouter.Request) (openrouter.Response, error) {
	c.mu.Lock()
	c.calls++
	call := c.calls
	c.mu.Unlock()
	switch call {
	case 1:
		close(c.firstStarted)
		select {
		case <-c.releaseFirst:
		case <-ctx.Done():
			return openrouter.Response{}, ctx.Err()
		}
	case 2:
		if c.secondStarted != nil {
			close(c.secondStarted)
		}
	}
	return decisionResponse("read", "reply-"+string(rune('0'+call))), nil
}

type turnLockOutcome struct {
	result TurnResult
	err    error
}

func awaitTurnLockOutcome(t *testing.T, done <-chan turnLockOutcome) turnLockOutcome {
	t.Helper()
	select {
	case outcome := <-done:
		return outcome
	case <-time.After(2 * time.Second):
		t.Fatal("turn did not finish")
		return turnLockOutcome{}
	}
}

func TestTurnLockSerializesSameChatThroughFinalAssistant(t *testing.T) {
	completer := &turnLockCompleter{firstStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	var release sync.Once
	releaseFirst := func() { release.Do(func() { close(completer.releaseFirst) }) }
	defer releaseFirst()
	conversation := &turnLockConversation{}
	turner := testTurner(completer, conversation, &fakeTools{})
	firstDone := make(chan turnLockOutcome, 1)
	go func() {
		result, err := turner.Turn(context.Background(), 77, "first", nil, true)
		firstDone <- turnLockOutcome{result: result, err: err}
	}()
	select {
	case <-completer.firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first turn did not reach the gated model call")
	}

	secondStarted := make(chan struct{})
	secondDone := make(chan turnLockOutcome, 1)
	go func() {
		close(secondStarted)
		result, err := turner.Turn(context.Background(), 77, "second", nil, true)
		secondDone <- turnLockOutcome{result: result, err: err}
	}()
	select {
	case <-secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("second turn did not start")
	}
	select {
	case <-secondDone:
		t.Fatal("same-chat turn completed while the first turn was still gated")
	default:
	}
	releaseFirst()
	first := awaitTurnLockOutcome(t, firstDone)
	second := awaitTurnLockOutcome(t, secondDone)
	if first.err != nil || second.err != nil || first.result.Reply != "reply-1" || second.result.Reply != "reply-2" {
		t.Fatalf("first=%+v second=%+v", first, second)
	}

	events := conversation.snapshot()
	firstAssistant, secondUser := -1, -1
	for index, event := range events {
		if event.chatID != 77 {
			continue
		}
		if event.role == "assistant" && event.content == "reply-1" {
			firstAssistant = index
		}
		if event.role == "user" && event.content == "second" {
			secondUser = index
		}
	}
	if firstAssistant < 0 || secondUser < 0 || secondUser <= firstAssistant {
		t.Fatalf("same-chat event order = %+v", events)
	}
}

func TestTurnLockAllowsDifferentChatsToProgressIndependently(t *testing.T) {
	completer := &turnLockCompleter{firstStarted: make(chan struct{}), secondStarted: make(chan struct{}), releaseFirst: make(chan struct{})}
	var release sync.Once
	releaseFirst := func() { release.Do(func() { close(completer.releaseFirst) }) }
	defer releaseFirst()
	conversation := &turnLockConversation{}
	turner := testTurner(completer, conversation, &fakeTools{})
	firstDone := make(chan turnLockOutcome, 1)
	go func() {
		result, err := turner.Turn(context.Background(), 101, "first", nil, true)
		firstDone <- turnLockOutcome{result: result, err: err}
	}()
	select {
	case <-completer.firstStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("first turn did not reach the gated model call")
	}
	secondDone := make(chan turnLockOutcome, 1)
	go func() {
		result, err := turner.Turn(context.Background(), 202, "second", nil, true)
		secondDone <- turnLockOutcome{result: result, err: err}
	}()
	select {
	case <-completer.secondStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("different-chat turn did not reach the model while first was gated")
	}
	second := awaitTurnLockOutcome(t, secondDone)
	if second.err != nil || second.result.Reply != "reply-2" {
		t.Fatalf("second=%+v", second)
	}
	releaseFirst()
	first := awaitTurnLockOutcome(t, firstDone)
	if first.err != nil || first.result.Reply != "reply-1" {
		t.Fatalf("first=%+v", first)
	}
}
