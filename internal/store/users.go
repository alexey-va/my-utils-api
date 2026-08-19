package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type User struct {
	ID                 string
	Email              string
	Username           string
	PasswordHash       string
	Role               string
	MustChangePassword bool
	CreatedAt          time.Time
}

type UserStore struct {
	pool *pgxpool.Pool
}

func NewUserStore(pool *pgxpool.Pool) *UserStore { return &UserStore{pool: pool} }

func (s *UserStore) FindByLogin(ctx context.Context, login string) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT id::text, email, username, password_hash, role, must_change_password, created_at
		FROM users
		WHERE lower(username) = lower($1) OR lower(email) = lower($1)
		LIMIT 1
	`, login))
}

func (s *UserStore) FindByID(ctx context.Context, id string) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		SELECT id::text, email, username, password_hash, role, must_change_password, created_at
		FROM users WHERE id = $1::uuid
	`, id))
}

func (s *UserStore) UsernameExists(ctx context.Context, username, excludingID string) (bool, error) {
	return s.exists(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(username) = lower($1) AND ($2 = '' OR id <> $2::uuid))`, username, excludingID)
}

func (s *UserStore) EmailExists(ctx context.Context, email, excludingID string) (bool, error) {
	return s.exists(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(email) = lower($1) AND ($2 = '' OR id <> $2::uuid))`, email, excludingID)
}

func (s *UserStore) exists(ctx context.Context, query, value, excludingID string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, query, value, excludingID).Scan(&exists); err != nil {
		return false, err
	}
	return exists, nil
}

func (s *UserStore) RoleExists(ctx context.Context, role string) (bool, error) {
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE role = $1)`, role).Scan(&exists); err != nil {
		return false, fmt.Errorf("check user role: %w", err)
	}
	return exists, nil
}

func (s *UserStore) Create(ctx context.Context, user User) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		INSERT INTO users (email, username, password_hash, role, must_change_password)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text, email, username, password_hash, role, must_change_password, created_at
	`, user.Email, user.Username, user.PasswordHash, user.Role, user.MustChangePassword))
}

func (s *UserStore) Save(ctx context.Context, user User) (User, error) {
	return scanUser(s.pool.QueryRow(ctx, `
		UPDATE users
		SET email = $2, username = $3, password_hash = $4, role = $5, must_change_password = $6
		WHERE id = $1::uuid
		RETURNING id::text, email, username, password_hash, role, must_change_password, created_at
	`, user.ID, user.Email, user.Username, user.PasswordHash, user.Role, user.MustChangePassword))
}

type rowScanner interface {
	Scan(...any) error
}

func scanUser(row rowScanner) (User, error) {
	var user User
	err := row.Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Role, &user.MustChangePassword, &user.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return User{}, pgx.ErrNoRows
		}
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	return user, nil
}
