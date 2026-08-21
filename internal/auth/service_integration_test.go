package auth_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/auth"
	"github.com/alexey-va/my-utils-api/internal/store"
	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

func TestServiceLoginRegistrationAndSessionCompatibility(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)
	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	sessions := auth.NewSessionStore(redisClient, "myutils:session:", "myutils:user-sessions:", "myutils:refresh:", "myutils:user-refresh-sessions:")
	tokens, err := auth.NewJWTService(strings.Repeat("k", 32), 24*time.Hour, time.Now)
	if err != nil {
		t.Fatalf("NewJWTService() error = %v", err)
	}
	service := auth.NewService(store.NewUserStore(pool), tokens, sessions, 30*24*time.Hour)

	login, err := service.Login(ctx, "DEV@example.com", "password")
	if err != nil {
		t.Fatalf("Login(seed) error = %v", err)
	}
	if login.User.Username != "dev" || login.User.Role != "USER" || login.Token == "" || login.RefreshToken == "" {
		t.Fatalf("Login(seed) = %#v", login)
	}
	claims, err := tokens.Parse(login.Token)
	if err != nil {
		t.Fatalf("Parse(login token) error = %v", err)
	}
	if ok, err := sessions.BelongsToUser(ctx, claims.ID, login.User.ID); err != nil || !ok {
		t.Fatalf("stored session belongs = %v, %v", ok, err)
	}
	refreshed, err := service.Refresh(ctx, login.RefreshToken)
	if err != nil {
		t.Fatalf("Refresh() error = %v", err)
	}
	if refreshed.Token == "" || refreshed.Token == login.Token || refreshed.RefreshToken != login.RefreshToken || refreshed.User != login.User {
		t.Fatalf("Refresh() = %#v", refreshed)
	}

	username := fmt.Sprintf("go-integration-%d", time.Now().UnixNano())
	registered, err := service.Register(ctx, auth.RegisterRequest{
		Username: username,
		Email:    username + "@example.com",
		Password: "password-123",
	})
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if registered.User.Username != username || registered.User.Role != "USER" {
		t.Fatalf("Register() = %#v", registered)
	}
	if _, err := service.Register(ctx, auth.RegisterRequest{Username: strings.ToUpper(username), Email: "other@example.com", Password: "password-123"}); !errors.Is(err, auth.ErrUsernameTaken) {
		t.Fatalf("duplicate username error = %v", err)
	}
	if _, err := service.Login(ctx, username, "wrong"); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("bad password error = %v", err)
	}
}
