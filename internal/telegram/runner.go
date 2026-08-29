package telegram

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type Bot interface {
	GetUpdates(context.Context, int64, int) ([]Update, error)
	DeleteWebhook(context.Context, bool) error
	AnswerCallback(context.Context, string) error
	SendHTMLMessage(context.Context, int64, string, string) (int, error)
}

type Dispatcher interface {
	Dispatch(context.Context, InboundMessage) error
}

type DispatchFunc func(context.Context, InboundMessage) error

func (f DispatchFunc) Dispatch(ctx context.Context, message InboundMessage) error {
	return f(ctx, message)
}

type InboundMessage struct {
	ChatID int64
	UserID int64
	Text   string
	Voice  *Voice
}

type Runner struct {
	bot        Bot
	dispatcher Dispatcher
	polling    bool
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	queues     map[int64]chan InboundMessage
	wait       sync.WaitGroup
	started    bool
}

func NewRunner(bot Bot, dispatcher Dispatcher, polling bool) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{bot: bot, dispatcher: dispatcher, polling: polling, ctx: ctx, cancel: cancel, queues: make(map[int64]chan InboundMessage)}
}

func (r *Runner) Name() string { return "telegram-long-polling" }

func (r *Runner) Warm(ctx context.Context) error {
	if !r.polling {
		return nil
	}
	if err := r.bot.DeleteWebhook(ctx, true); err != nil {
		slog.WarnContext(ctx, "Telegram deleteWebhook failed; long polling will still start", "error", err)
	}
	r.mu.Lock()
	if !r.started {
		r.started = true
		r.wait.Add(1)
		go r.pollLoop()
	}
	r.mu.Unlock()
	return nil
}

func (r *Runner) Close() {
	r.cancel()
	r.wait.Wait()
}

func (r *Runner) pollLoop() {
	defer r.wait.Done()
	var offset int64
	for {
		if r.ctx.Err() != nil {
			return
		}
		updates, err := r.bot.GetUpdates(r.ctx, offset, 30)
		if err != nil {
			if errors.Is(err, context.Canceled) || r.ctx.Err() != nil {
				return
			}
			slog.WarnContext(r.ctx, "Telegram polling failed", "error", err)
			timer := time.NewTimer(time.Second)
			select {
			case <-r.ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			continue
		}
		for _, update := range updates {
			if update.UpdateID >= offset {
				offset = update.UpdateID + 1
			}
			r.routeUpdate(update)
		}
	}
}

func (r *Runner) routeUpdate(update Update) {
	if callback := update.CallbackQuery; callback != nil {
		if callback.From == nil || callback.Message == nil || strings.TrimSpace(callback.Data) == "" {
			return
		}
		_ = r.bot.AnswerCallback(r.ctx, callback.ID)
		r.enqueue(InboundMessage{ChatID: callback.Message.Chat.ID, UserID: callback.From.ID, Text: strings.TrimSpace(callback.Data)})
		return
	}
	message := update.Message
	if message == nil {
		message = update.EditedMessage
	}
	if message == nil || message.From == nil {
		return
	}
	text := strings.TrimSpace(message.Text)
	if text != "" {
		r.enqueue(InboundMessage{ChatID: message.Chat.ID, UserID: message.From.ID, Text: text})
		return
	}
	if message.Voice != nil && strings.TrimSpace(message.Voice.FileID) != "" {
		voice := *message.Voice
		r.enqueue(InboundMessage{ChatID: message.Chat.ID, UserID: message.From.ID, Voice: &voice})
		return
	}
	_, _ = r.bot.SendHTMLMessage(r.ctx, message.Chat.ID, "❌ Я понимаю только текстовые и голосовые сообщения.", "")
}

func (r *Runner) enqueue(message InboundMessage) {
	r.mu.Lock()
	queue := r.queues[message.ChatID]
	if queue == nil {
		queue = make(chan InboundMessage, 100)
		r.queues[message.ChatID] = queue
		r.wait.Add(1)
		go r.consume(message.ChatID, queue)
	}
	r.mu.Unlock()
	select {
	case queue <- message:
	case <-r.ctx.Done():
	}
}

func (r *Runner) consume(chatID int64, queue <-chan InboundMessage) {
	defer r.wait.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case message := <-queue:
			if err := r.dispatcher.Dispatch(r.ctx, message); err != nil {
				slog.ErrorContext(r.ctx, "Telegram dispatch failed", "chatId", chatID, "error", err)
				_, _ = r.bot.SendHTMLMessage(r.ctx, chatID, "❌ Не удалось обработать запрос. Попробуй ещё раз.", "")
			}
		}
	}
}
