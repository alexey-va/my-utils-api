package agent

import (
	"context"
	"log/slog"
	"time"
)

type AutoCompactor struct {
	compactor  *Compactor
	keepRecent func() int
	threshold  func() int
	requests   chan int64
}

func NewAutoCompactor(compactor *Compactor, keepRecent, threshold func() int) *AutoCompactor {
	return &AutoCompactor{
		compactor: compactor, keepRecent: keepRecent, threshold: threshold,
		requests: make(chan int64, 128),
	}
}

func (a *AutoCompactor) Trigger(chatID int64) {
	select {
	case a.requests <- chatID:
	default:
		slog.Warn("agent compaction queue is full", "chatId", chatID)
	}
}

func (a *AutoCompactor) Run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case chatID := <-a.requests:
			compactContext, cancel := context.WithTimeout(ctx, 4*time.Minute)
			result, err := a.compactor.CompactAuto(compactContext, chatID, a.keepRecent(), a.threshold())
			cancel()
			if err != nil {
				slog.WarnContext(ctx, "automatic agent compaction failed", "chatId", chatID, "error", err)
			} else if result.Compacted {
				slog.InfoContext(ctx, "agent dialog compacted", "chatId", chatID, "messages", result.MessageCount)
			}
		}
	}
}
