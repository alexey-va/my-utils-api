package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/alexey-va/my-utils-api/internal/agent"
	"github.com/redis/go-redis/v9"
)

const statusTTL = 2 * time.Hour

type StatusBot interface {
	SendHTMLMessage(context.Context, int64, string, string) (int, error)
	EditHTMLMessage(context.Context, int64, int, string) error
	DeleteMessage(context.Context, int64, int) error
	SendTyping(context.Context, int64) error
}

type StatusMessenger struct {
	bot   StatusBot
	redis redis.UniversalClient
}

func NewStatusMessenger(bot StatusBot, client redis.UniversalClient) *StatusMessenger {
	return &StatusMessenger{bot: bot, redis: client}
}

func (s *StatusMessenger) Begin(ctx context.Context, chatID int64) {
	s.reset(ctx, chatID)
	s.update(ctx, chatID, "Думаю…")
}

func (s *StatusMessenger) Thinking(ctx context.Context, chatID int64, step int) {
	text := "Думаю…"
	if step > 1 {
		text = fmt.Sprintf("Думаю (шаг %d)…", step)
	}
	s.update(ctx, chatID, text)
}

func (s *StatusMessenger) ToolsStarted(ctx context.Context, chatID int64, names []string) {
	s.update(ctx, chatID, agent.ToolsStatusLabel(names))
}

func (s *StatusMessenger) ToolRunning(ctx context.Context, chatID int64, name string) {
	s.update(ctx, chatID, agent.ToolStatusLabel(name))
}

func (s *StatusMessenger) ComposingReply(ctx context.Context, chatID int64) {
	s.update(ctx, chatID, "Формирую ответ…")
}

func (s *StatusMessenger) Complete(_ context.Context, chatID int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if messageID, ok := s.load(ctx, chatID); ok {
		if err := s.bot.DeleteMessage(ctx, chatID, messageID); err != nil {
			slog.WarnContext(ctx, "delete Telegram agent status failed", "chatId", chatID, "error", err)
		}
	}
	s.clear(ctx, chatID)
}

func (s *StatusMessenger) reset(ctx context.Context, chatID int64) {
	if messageID, ok := s.load(ctx, chatID); ok {
		if err := s.bot.DeleteMessage(ctx, chatID, messageID); err != nil {
			slog.WarnContext(ctx, "delete stale Telegram agent status failed", "chatId", chatID, "error", err)
		}
	}
	s.clear(ctx, chatID)
}

func (s *StatusMessenger) update(ctx context.Context, chatID int64, text string) {
	text = "⏳ " + text
	if messageID, ok := s.load(ctx, chatID); ok {
		if err := s.bot.EditHTMLMessage(ctx, chatID, messageID, text); err == nil {
			s.typing(ctx, chatID)
			return
		}
		// The stored Telegram message may have been deleted manually. Fall back to
		// a fresh message instead of losing all progress feedback.
		s.clear(ctx, chatID)
	}
	messageID, err := s.bot.SendHTMLMessage(ctx, chatID, text, "")
	if err != nil {
		slog.WarnContext(ctx, "send Telegram agent status failed", "chatId", chatID, "error", err)
		return
	}
	if err := s.redis.Set(ctx, s.key(chatID), strconv.Itoa(messageID), statusTTL).Err(); err != nil {
		slog.WarnContext(ctx, "store Telegram agent status failed", "chatId", chatID, "error", err)
	}
	s.typing(ctx, chatID)
}

func (s *StatusMessenger) typing(ctx context.Context, chatID int64) {
	if err := s.bot.SendTyping(ctx, chatID); err != nil {
		slog.DebugContext(ctx, "send Telegram typing action failed", "chatId", chatID, "error", err)
	}
}

func (s *StatusMessenger) load(ctx context.Context, chatID int64) (int, bool) {
	raw, err := s.redis.Get(ctx, s.key(chatID)).Result()
	if err != nil {
		if err != redis.Nil {
			slog.WarnContext(ctx, "load Telegram agent status failed", "chatId", chatID, "error", err)
		}
		return 0, false
	}
	messageID, err := strconv.Atoi(raw)
	return messageID, err == nil && messageID > 0
}

func (s *StatusMessenger) clear(ctx context.Context, chatID int64) {
	if err := s.redis.Del(ctx, s.key(chatID)).Err(); err != nil {
		slog.WarnContext(ctx, "clear Telegram agent status failed", "chatId", chatID, "error", err)
	}
}

func (*StatusMessenger) key(chatID int64) string {
	return fmt.Sprintf("agent:status:%d", chatID)
}
