package auth

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestSessionStorePreservesRedisKeyContractAndRevokesAllUserSessions(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewSessionStore(client, "myutils:session:", "myutils:user-sessions:")
	ctx := context.Background()
	userID := "bbf2ae8e-3097-481e-a48c-5e8af6f7856d"

	if err := store.Store(ctx, "session-one", userID, 12*time.Hour); err != nil {
		t.Fatalf("Store(one) error = %v", err)
	}
	if err := store.Store(ctx, "session-two", userID, 12*time.Hour); err != nil {
		t.Fatalf("Store(two) error = %v", err)
	}
	if got, err := client.Get(ctx, "myutils:session:session-one").Result(); err != nil || got != userID {
		t.Fatalf("stored JVM-compatible value = %q, %v", got, err)
	}
	if ok, err := store.BelongsToUser(ctx, "session-one", userID); err != nil || !ok {
		t.Fatalf("BelongsToUser() = %v, %v", ok, err)
	}
	if ok, err := store.BelongsToUser(ctx, "session-one", "another-user"); err != nil || ok {
		t.Fatalf("BelongsToUser(other) = %v, %v", ok, err)
	}

	if err := store.RevokeUserSessions(ctx, userID); err != nil {
		t.Fatalf("RevokeUserSessions() error = %v", err)
	}
	for _, key := range []string{"myutils:session:session-one", "myutils:session:session-two", "myutils:user-sessions:" + userID} {
		if server.Exists(key) {
			t.Errorf("key %q still exists after revocation", key)
		}
	}
}

func TestSessionWarmupProbesCreateReadDeleteWithoutLeavingKey(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewSessionStore(client, "myutils:session:", "myutils:user-sessions:")

	if store.Name() != "redis-session" {
		t.Errorf("Name() = %q", store.Name())
	}
	if err := store.Warm(context.Background()); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}
	if got := server.Keys(); len(got) != 0 {
		t.Fatalf("warmup left Redis keys: %#v", got)
	}
}
