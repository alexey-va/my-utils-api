package telegram

import (
	"context"
	"testing"
	"time"
)

type fakeBot struct {
	callbackIDs []string
	messages    []string
}

func (f *fakeBot) GetUpdates(context.Context, int64, int) ([]Update, error) { return nil, nil }
func (f *fakeBot) DeleteWebhook(context.Context, bool) error                { return nil }
func (f *fakeBot) AnswerCallback(_ context.Context, id string) error {
	f.callbackIDs = append(f.callbackIDs, id)
	return nil
}
func (f *fakeBot) SendHTMLMessage(_ context.Context, _ int64, text, _ string) (int, error) {
	f.messages = append(f.messages, text)
	return 1, nil
}

type dispatched struct {
	input InboundMessage
}
type fakeDispatcher struct{ values chan dispatched }

func (f *fakeDispatcher) Dispatch(_ context.Context, input InboundMessage) error {
	f.values <- dispatched{input: input}
	return nil
}

func TestRunnerRoutesTextAndCallbackThroughSerialQueue(t *testing.T) {
	t.Parallel()
	bot := &fakeBot{}
	dispatcher := &fakeDispatcher{values: make(chan dispatched, 2)}
	runner := NewRunner(bot, dispatcher, false)
	defer runner.Close()
	runner.routeUpdate(Update{Message: &Message{From: &User{ID: 7}, Chat: Chat{ID: 42}, Text: " hello "}})
	runner.routeUpdate(Update{CallbackQuery: &CallbackQuery{ID: "cb-1", From: &User{ID: 7}, Message: &Message{Chat: Chat{ID: 42}}, Data: "next"}})
	for _, want := range []string{"hello", "next"} {
		select {
		case got := <-dispatcher.values:
			if got.input.ChatID != 42 || got.input.UserID != 7 || got.input.Text != want || got.input.Voice != nil {
				t.Fatalf("dispatch = %#v", got)
			}
		case <-time.After(time.Second):
			t.Fatal("dispatch timeout")
		}
	}
	if len(bot.callbackIDs) != 1 || bot.callbackIDs[0] != "cb-1" {
		t.Fatalf("callbacks = %#v", bot.callbackIDs)
	}
}

func TestRunnerRoutesVoiceThroughSerialQueue(t *testing.T) {
	t.Parallel()
	bot := &fakeBot{}
	dispatcher := &fakeDispatcher{values: make(chan dispatched, 1)}
	runner := NewRunner(bot, dispatcher, false)
	defer runner.Close()
	runner.routeUpdate(Update{Message: &Message{
		From: &User{ID: 7}, Chat: Chat{ID: 42},
		Voice: &Voice{FileID: "voice-1", FileSize: 123, Duration: 8, MimeType: "audio/ogg"},
	}})
	select {
	case got := <-dispatcher.values:
		if got.input.ChatID != 42 || got.input.UserID != 7 || got.input.Text != "" || got.input.Voice == nil {
			t.Fatalf("dispatch = %#v", got)
		}
		if got.input.Voice.FileID != "voice-1" || got.input.Voice.MimeType != "audio/ogg" {
			t.Fatalf("voice = %#v", got.input.Voice)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatch timeout")
	}
	if len(bot.messages) != 0 {
		t.Fatalf("voice was rejected: %#v", bot.messages)
	}
}

func TestRunnerRejectsNonTextMessage(t *testing.T) {
	t.Parallel()
	bot := &fakeBot{}
	runner := NewRunner(bot, &fakeDispatcher{values: make(chan dispatched, 1)}, false)
	defer runner.Close()
	runner.routeUpdate(Update{Message: &Message{From: &User{ID: 7}, Chat: Chat{ID: 42}}})
	if len(bot.messages) != 1 {
		t.Fatalf("messages = %#v", bot.messages)
	}
}
