package telegram

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type fakeStatusBot struct {
	sent, edited []string
	deleted      []int
	typing       int
}

func (f *fakeStatusBot) SendHTMLMessage(_ context.Context, chatID int64, text, _ string) (int, error) {
	f.sent = append(f.sent, text)
	return len(f.sent) + 100, nil
}
func (f *fakeStatusBot) EditHTMLMessage(_ context.Context, _ int64, _ int, text string) error {
	f.edited = append(f.edited, text)
	return nil
}
func (f *fakeStatusBot) DeleteMessage(_ context.Context, _ int64, messageID int) error {
	f.deleted = append(f.deleted, messageID)
	return nil
}
func (f *fakeStatusBot) SendTyping(context.Context, int64) error { f.typing++; return nil }

func TestStatusMessengerKeepsOneEditableMessageAndCleansItUp(t *testing.T) {
	t.Parallel()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	bot := &fakeStatusBot{}
	status := NewStatusMessenger(bot, client)
	ctx := context.Background()

	status.Begin(ctx, 42)
	status.ToolsStarted(ctx, 42, []string{"logWorkout", "log_workout"})
	status.ComposingReply(ctx, 42)
	if len(bot.sent) != 1 || len(bot.edited) != 2 || bot.edited[0] != "⏳ Записываю в дневник…" {
		t.Fatalf("sent=%#v edited=%#v", bot.sent, bot.edited)
	}
	if ttl := server.TTL("agent:status:42"); ttl <= 0 {
		t.Fatalf("status TTL = %s", ttl)
	}
	status.Complete(ctx, 42)
	if len(bot.deleted) != 1 || server.Exists("agent:status:42") {
		t.Fatalf("deleted=%#v redisExists=%v", bot.deleted, server.Exists("agent:status:42"))
	}
}
