package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	store := NewSessionStore(client, "myutils:session:", "myutils:user-sessions:", "myutils:refresh:", "myutils:user-refresh-sessions:")
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
	store := NewSessionStore(client, "myutils:session:", "myutils:user-sessions:", "myutils:refresh:", "myutils:user-refresh-sessions:")

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

func TestRefreshSessionUsesOpaqueRedisKeyAndSlidesUntilRevoked(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewSessionStore(client, "myutils:session:", "myutils:user-sessions:", "myutils:refresh:", "myutils:user-refresh-sessions:")
	ctx := context.Background()
	userID := "bbf2ae8e-3097-481e-a48c-5e8af6f7856d"
	const ttl = 30 * 24 * time.Hour

	rawToken, err := store.CreateRefresh(ctx, userID, ttl)
	if err != nil {
		t.Fatalf("CreateRefresh() error = %v", err)
	}
	if rawToken == "" {
		t.Fatal("CreateRefresh() returned an empty token")
	}
	digest := sha256.Sum256([]byte(rawToken))
	refreshKey := "myutils:refresh:" + hex.EncodeToString(digest[:])
	if server.Exists("myutils:refresh:"+rawToken) || !server.Exists(refreshKey) {
		t.Fatalf("refresh token storage keys = %#v", server.Keys())
	}

	server.FastForward(29 * 24 * time.Hour)
	gotUserID, ok, err := store.ResolveRefresh(ctx, rawToken, ttl)
	if err != nil || !ok || gotUserID != userID {
		t.Fatalf("ResolveRefresh() = %q, %v, %v", gotUserID, ok, err)
	}
	if remaining := server.TTL(refreshKey); remaining < 29*24*time.Hour {
		t.Fatalf("sliding refresh TTL = %s, want close to %s", remaining, ttl)
	}

	if err := store.RevokeRefresh(ctx, rawToken); err != nil {
		t.Fatalf("RevokeRefresh() error = %v", err)
	}
	if _, ok, err := store.ResolveRefresh(ctx, rawToken, ttl); err != nil || ok {
		t.Fatalf("ResolveRefresh(revoked) ok = %v, error = %v", ok, err)
	}
}

func TestRevokeUserSessionsAlsoRevokesRefreshSessions(t *testing.T) {
	t.Parallel()

	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	t.Cleanup(func() { _ = client.Close() })
	store := NewSessionStore(client, "myutils:session:", "myutils:user-sessions:", "myutils:refresh:", "myutils:user-refresh-sessions:")
	ctx := context.Background()
	userID := "bbf2ae8e-3097-481e-a48c-5e8af6f7856d"

	rawToken, err := store.CreateRefresh(ctx, userID, 30*24*time.Hour)
	if err != nil {
		t.Fatalf("CreateRefresh() error = %v", err)
	}
	if err := store.RevokeUserSessions(ctx, userID); err != nil {
		t.Fatalf("RevokeUserSessions() error = %v", err)
	}
	if _, ok, err := store.ResolveRefresh(ctx, rawToken, 30*24*time.Hour); err != nil || ok {
		t.Fatalf("ResolveRefresh(revoked user) ok = %v, error = %v", ok, err)
	}
	if server.Exists("myutils:user-refresh-sessions:" + userID) {
		t.Fatal("user refresh-session index still exists after revocation")
	}
}
