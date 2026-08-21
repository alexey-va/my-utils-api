package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type SessionStore struct {
	redis                 redis.UniversalClient
	redisKeyPrefix        string
	userSessionsKeyPrefix string
	refreshKeyPrefix      string
	userRefreshKeyPrefix  string
}

func NewSessionStore(client redis.UniversalClient, redisKeyPrefix, userSessionsKeyPrefix, refreshKeyPrefix, userRefreshKeyPrefix string) *SessionStore {
	return &SessionStore{
		redis:                 client,
		redisKeyPrefix:        redisKeyPrefix,
		userSessionsKeyPrefix: userSessionsKeyPrefix,
		refreshKeyPrefix:      refreshKeyPrefix,
		userRefreshKeyPrefix:  userRefreshKeyPrefix,
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

func (s *SessionStore) CreateRefresh(ctx context.Context, userID string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		return "", errors.New("refresh session TTL must be positive")
	}
	rawToken, err := randomRefreshToken()
	if err != nil {
		return "", fmt.Errorf("create refresh token: %w", err)
	}
	digest := refreshDigest(rawToken)
	_, err = s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Set(ctx, s.refreshKey(digest), userID, ttl)
		pipe.SAdd(ctx, s.userRefreshKey(userID), digest)
		pipe.Expire(ctx, s.userRefreshKey(userID), ttl)
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("store refresh session: %w", err)
	}
	return rawToken, nil
}

func (s *SessionStore) ResolveRefresh(ctx context.Context, rawToken string, ttl time.Duration) (string, bool, error) {
	if rawToken == "" || ttl <= 0 {
		return "", false, nil
	}
	digest := refreshDigest(rawToken)
	userID, err := s.redis.Get(ctx, s.refreshKey(digest)).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("read refresh session: %w", err)
	}
	_, err = s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Expire(ctx, s.refreshKey(digest), ttl)
		pipe.Expire(ctx, s.userRefreshKey(userID), ttl)
		return nil
	})
	if err != nil {
		return "", false, fmt.Errorf("extend refresh session: %w", err)
	}
	return userID, true, nil
}

func (s *SessionStore) RevokeRefresh(ctx context.Context, rawToken string) error {
	if rawToken == "" {
		return nil
	}
	digest := refreshDigest(rawToken)
	userID, err := s.redis.Get(ctx, s.refreshKey(digest)).Result()
	if errors.Is(err, redis.Nil) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read refresh session for revocation: %w", err)
	}
	_, err = s.redis.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Del(ctx, s.refreshKey(digest))
		pipe.SRem(ctx, s.userRefreshKey(userID), digest)
		return nil
	})
	if err != nil {
		return fmt.Errorf("revoke refresh session: %w", err)
	}
	return nil
}

func (s *SessionStore) RevokeUserSessions(ctx context.Context, userID string) error {
	userKey := s.userSessionsKey(userID)
	sessionIDs, err := s.redis.SMembers(ctx, userKey).Result()
	if err != nil {
		return fmt.Errorf("list user sessions: %w", err)
	}
	refreshUserKey := s.userRefreshKey(userID)
	refreshDigests, err := s.redis.SMembers(ctx, refreshUserKey).Result()
	if err != nil {
		return fmt.Errorf("list user refresh sessions: %w", err)
	}
	keys := make([]string, 0, len(sessionIDs)+len(refreshDigests)+2)
	for _, sessionID := range sessionIDs {
		keys = append(keys, s.sessionKey(sessionID))
	}
	for _, digest := range refreshDigests {
		keys = append(keys, s.refreshKey(digest))
	}
	keys = append(keys, userKey, refreshUserKey)
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

func (s *SessionStore) refreshKey(digest string) string {
	return s.refreshKeyPrefix + digest
}

func (s *SessionStore) userRefreshKey(userID string) string {
	return s.userRefreshKeyPrefix + userID
}

func randomRefreshToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func refreshDigest(rawToken string) string {
	digest := sha256.Sum256([]byte(rawToken))
	return hex.EncodeToString(digest[:])
}
