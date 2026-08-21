package auth

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/store"
	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"
)

func TestRefreshDistinguishesMissingUserFromRepositoryFailure(t *testing.T) {
	t.Parallel()

	redisServer := miniredis.RunT(t)
	redisClient := redis.NewClient(&redis.Options{Addr: redisServer.Addr()})
	t.Cleanup(func() { _ = redisClient.Close() })
	sessions := NewSessionStore(redisClient, "session:", "user-session:", "refresh:", "user-refresh:")
	tokens, err := NewJWTService(strings.Repeat("k", 32), time.Hour, time.Now)
	if err != nil {
		t.Fatalf("NewJWTService() error = %v", err)
	}

	for _, test := range []struct {
		name string
		err  error
		want error
	}{
		{name: "deleted user", err: pgx.ErrNoRows, want: ErrInvalidCredentials},
		{name: "database unavailable", err: errRepositoryUnavailable, want: errRepositoryUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			refreshToken, createErr := sessions.CreateRefresh(context.Background(), "user-1", 30*24*time.Hour)
			if createErr != nil {
				t.Fatalf("CreateRefresh() error = %v", createErr)
			}
			service := NewService(errorUserRepository{findByIDErr: test.err}, tokens, sessions, 30*24*time.Hour)

			_, refreshErr := service.Refresh(context.Background(), refreshToken)
			if !errors.Is(refreshErr, test.want) {
				t.Fatalf("Refresh() error = %v, want %v", refreshErr, test.want)
			}
		})
	}
}

var errRepositoryUnavailable = errors.New("repository unavailable")

type errorUserRepository struct {
	findByIDErr error
}

func (errorUserRepository) FindByLogin(context.Context, string) (store.User, error) {
	return store.User{}, errors.New("unused")
}

func (repository errorUserRepository) FindByID(context.Context, string) (store.User, error) {
	return store.User{}, repository.findByIDErr
}

func (errorUserRepository) UsernameExists(context.Context, string, string) (bool, error) {
	return false, errors.New("unused")
}

func (errorUserRepository) EmailExists(context.Context, string, string) (bool, error) {
	return false, errors.New("unused")
}

func (errorUserRepository) RoleExists(context.Context, string) (bool, error) {
	return false, errors.New("unused")
}

func (errorUserRepository) Create(context.Context, store.User) (store.User, error) {
	return store.User{}, errors.New("unused")
}

func (errorUserRepository) Save(context.Context, store.User) (store.User, error) {
	return store.User{}, errors.New("unused")
}
