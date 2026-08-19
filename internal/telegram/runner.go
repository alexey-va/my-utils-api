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
	Dispatch(context.Context, int64, int64, string) error
}

type DispatchFunc func(context.Context, int64, int64, string) error

func (f DispatchFunc) Dispatch(ctx context.Context, chatID, userID int64, text string) error {
	return f(ctx, chatID, userID, text)
}

type inbound struct {
	userID int64
	text   string
}

type Runner struct {
	bot        Bot
	dispatcher Dispatcher
	polling    bool
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	queues     map[int64]chan inbound
	wait       sync.WaitGroup
	started    bool
}

func NewRunner(bot Bot, dispatcher Dispatcher, polling bool) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{bot: bot, dispatcher: dispatcher, polling: polling, ctx: ctx, cancel: cancel, queues: make(map[int64]chan inbound)}
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
		r.enqueue(callback.Message.Chat.ID, callback.From.ID, strings.TrimSpace(callback.Data))
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
	if text == "" {
		_, _ = r.bot.SendHTMLMessage(r.ctx, message.Chat.ID, "❌ Я понимаю только текстовые сообщения.", "")
		return
	}
	r.enqueue(message.Chat.ID, message.From.ID, text)
}

func (r *Runner) enqueue(chatID, userID int64, text string) {
	r.mu.Lock()
	queue := r.queues[chatID]
	if queue == nil {
		queue = make(chan inbound, 100)
		r.queues[chatID] = queue
		r.wait.Add(1)
		go r.consume(chatID, queue)
	}
	r.mu.Unlock()
	select {
	case queue <- inbound{userID: userID, text: text}:
	case <-r.ctx.Done():
	}
}

func (r *Runner) consume(chatID int64, queue <-chan inbound) {
	defer r.wait.Done()
	for {
		select {
		case <-r.ctx.Done():
			return
		case message := <-queue:
			if err := r.dispatcher.Dispatch(r.ctx, chatID, message.userID, message.text); err != nil {
				slog.ErrorContext(r.ctx, "Telegram dispatch failed", "chatId", chatID, "error", err)
				_, _ = r.bot.SendHTMLMessage(r.ctx, chatID, "❌ Не удалось обработать запрос. Попробуй ещё раз.", "")
			}
		}
	}
}
