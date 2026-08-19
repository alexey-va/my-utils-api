package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type SessionStore struct {
	redis                 redis.UniversalClient
	redisKeyPrefix        string
	userSessionsKeyPrefix string
}

func NewSessionStore(client redis.UniversalClient, redisKeyPrefix, userSessionsKeyPrefix string) *SessionStore {
	return &SessionStore{
		redis:                 client,
		redisKeyPrefix:        redisKeyPrefix,
		userSessionsKeyPrefix: userSessionsKeyPrefix,
	}
}

func (s *SessionStore) Name() string { return "redis-session" }

func (s *SessionStore) Warm(ctx context.Context) error {
	if err := s.redis.Ping(ctx).Err(); err != nil {
		return fmt.Errorf("ping Redis: %w", err)
	}
	id, err := randomUUID()
	if err != nil {
		return fmt.Errorf("create warmup key: %w", err)
	}
	key := s.redisKeyPrefix + "startup-warmup-" + id
	const value = "startup-warmup"
	if err := s.redis.Set(ctx, key, value, 30*time.Second).Err(); err != nil {
		return fmt.Errorf("write warmup session: %w", err)
	}
	defer func() { _ = s.redis.Del(context.WithoutCancel(ctx), key).Err() }()
	stored, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("read warmup session: %w", err)
	}
	if stored != value {
		return fmt.Errorf("read warmup session: got %q", stored)
	}
	if err := s.redis.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("delete warmup session: %w", err)
	}
	return nil
}

func (s *SessionStore) Store(ctx context.Context, sessionID, userID string, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("session TTL must be positive")
	}
	_, err := s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, s.sessionKey(sessionID), userID, ttl)
		pipe.SAdd(ctx, s.userSessionsKey(userID), sessionID)
		pipe.Expire(ctx, s.userSessionsKey(userID), ttl)
		return nil
	})
	if err != nil {
		return fmt.Errorf("store session: %w", err)
	}
	return nil
}

func (s *SessionStore) BelongsToUser(ctx context.Context, sessionID, userID string) (bool, error) {
	stored, err := s.redis.Get(ctx, s.sessionKey(sessionID)).Result()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read session: %w", err)
	}
	return stored == userID, nil
}

func (s *SessionStore) Revoke(ctx context.Context, sessionID string) error {
	if err := s.redis.Del(ctx, s.sessionKey(sessionID)).Err(); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}
	return nil
}

func (s *SessionStore) RevokeUserSessions(ctx context.Context, userID string) error {
	userKey := s.userSessionsKey(userID)
	sessionIDs, err := s.redis.SMembers(ctx, userKey).Result()
	if err != nil {
		return fmt.Errorf("list user sessions: %w", err)
	}
	keys := make([]string, 0, len(sessionIDs)+1)
	for _, sessionID := range sessionIDs {
		keys = append(keys, s.sessionKey(sessionID))
	}
	keys = append(keys, userKey)
	if err := s.redis.Del(ctx, keys...).Err(); err != nil {
		return fmt.Errorf("revoke user sessions: %w", err)
	}
	return nil
}

func (s *SessionStore) sessionKey(sessionID string) string {
	return s.redisKeyPrefix + sessionID
}

func (s *SessionStore) userSessionsKey(userID string) string {
	return s.userSessionsKeyPrefix + userID
}
