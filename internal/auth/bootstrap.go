package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alexey-va/my-utils-api/internal/store"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

type BootstrapConfig struct {
	Enabled  bool
	Username string
	Password string
	Email    string
}

type AdminBootstrap struct {
	users  UserRepository
	config BootstrapConfig
}

func NewAdminBootstrap(users UserRepository, config BootstrapConfig) *AdminBootstrap {
	return &AdminBootstrap{users: users, config: config}
}

func (b *AdminBootstrap) Name() string { return "admin-bootstrap" }

func (b *AdminBootstrap) Warm(ctx context.Context) error {
	if !b.config.Enabled {
		return nil
	}
	exists, err := b.users.RoleExists(ctx, "ADMIN")
	if err != nil {
		return fmt.Errorf("check bootstrap admin: %w", err)
	}
	if exists {
		return nil
	}
	username := strings.TrimSpace(b.config.Username)
	email := strings.ToLower(strings.TrimSpace(b.config.Email))
	if username == "" || b.config.Password == "" {
		return errors.New("bootstrap admin username and password must not be blank")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(b.config.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash bootstrap admin password: %w", err)
	}
	user, err := b.users.FindByLogin(ctx, username)
	if errors.Is(err, pgx.ErrNoRows) {
		user, err = b.users.FindByLogin(ctx, email)
	}
	if errors.Is(err, pgx.ErrNoRows) {
		_, err = b.users.Create(ctx, store.User{
			Username: username, Email: email, PasswordHash: string(hash), Role: "ADMIN", MustChangePassword: true,
		})
	} else if err == nil {
		user.Username = username
		user.Email = email
		user.PasswordHash = string(hash)
		user.Role = "ADMIN"
		user.MustChangePassword = true
		_, err = b.users.Save(ctx, user)
	}
	if err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	return nil
}
