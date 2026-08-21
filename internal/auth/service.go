package auth

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/store"
	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrEmailTaken         = errors.New("email is already registered")
	ErrAccountNotFound    = errors.New("account not found")
	ErrNoCredentialChange = errors.New("no credential changes supplied")
)

type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UpdateCredentialsRequest struct {
	CurrentPassword string  `json:"currentPassword"`
	Username        *string `json:"username"`
	Email           *string `json:"email"`
	NewPassword     *string `json:"newPassword"`
}

type UserDTO struct {
	ID                 string `json:"id"`
	Username           string `json:"username"`
	Email              string `json:"email"`
	Role               string `json:"role"`
	MustChangePassword bool   `json:"mustChangePassword"`
}

type LoginResponse struct {
	Token        string  `json:"token"`
	User         UserDTO `json:"user"`
	RefreshToken string  `json:"-"`
}

type Principal struct {
	User      UserDTO
	SessionID string
}

type UserRepository interface {
	FindByLogin(context.Context, string) (store.User, error)
	FindByID(context.Context, string) (store.User, error)
	UsernameExists(context.Context, string, string) (bool, error)
	EmailExists(context.Context, string, string) (bool, error)
	RoleExists(context.Context, string) (bool, error)
	Create(context.Context, store.User) (store.User, error)
	Save(context.Context, store.User) (store.User, error)
}

type Service struct {
	users      UserRepository
	tokens     *JWTService
	sessions   *SessionStore
	refreshTTL time.Duration
}

func NewService(users UserRepository, tokens *JWTService, sessions *SessionStore, refreshTTL time.Duration) *Service {
	return &Service{users: users, tokens: tokens, sessions: sessions, refreshTTL: refreshTTL}
}

func (s *Service) Login(ctx context.Context, login, password string) (LoginResponse, error) {
	user, err := s.users.FindByLogin(ctx, strings.TrimSpace(login))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return LoginResponse{}, ErrInvalidCredentials
		}
		return LoginResponse{}, err
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return LoginResponse{}, ErrInvalidCredentials
	}
	return s.issueSession(ctx, user)
}

func (s *Service) Register(ctx context.Context, request RegisterRequest) (LoginResponse, error) {
	username := strings.ToLower(strings.TrimSpace(request.Username))
	email := strings.ToLower(strings.TrimSpace(request.Email))
	if exists, err := s.users.UsernameExists(ctx, username, ""); err != nil {
		return LoginResponse{}, err
	} else if exists {
		return LoginResponse{}, ErrUsernameTaken
	}
	if exists, err := s.users.EmailExists(ctx, email, ""); err != nil {
		return LoginResponse{}, err
	} else if exists {
		return LoginResponse{}, ErrEmailTaken
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(request.Password), bcrypt.DefaultCost)
	if err != nil {
		return LoginResponse{}, fmt.Errorf("hash password: %w", err)
	}
	user, err := s.users.Create(ctx, store.User{
		Username: username, Email: email, PasswordHash: string(hash), Role: "USER",
	})
	if err != nil {
		return LoginResponse{}, err
	}
	return s.issueSession(ctx, user)
}

func (s *Service) Profile(ctx context.Context, userID string) (UserDTO, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return UserDTO{}, ErrAccountNotFound
		}
		return UserDTO{}, err
	}
	return userDTO(user), nil
}

func (s *Service) UpdateCredentials(ctx context.Context, userID string, request UpdateCredentialsRequest) (LoginResponse, error) {
	user, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return LoginResponse{}, ErrAccountNotFound
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(request.CurrentPassword)) != nil {
		return LoginResponse{}, errors.New("current password is incorrect")
	}
	username := normalizedOptional(request.Username)
	email := normalizedOptional(request.Email)
	password := nonBlankOptional(request.NewPassword)
	if username == nil && email == nil && password == nil {
		return LoginResponse{}, ErrNoCredentialChange
	}
	if username != nil {
		if exists, checkErr := s.users.UsernameExists(ctx, *username, user.ID); checkErr != nil {
			return LoginResponse{}, checkErr
		} else if exists {
			return LoginResponse{}, ErrUsernameTaken
		}
		user.Username = *username
	}
	if email != nil {
		if exists, checkErr := s.users.EmailExists(ctx, *email, user.ID); checkErr != nil {
			return LoginResponse{}, checkErr
		} else if exists {
			return LoginResponse{}, ErrEmailTaken
		}
		user.Email = *email
	}
	if password != nil {
		hash, hashErr := bcrypt.GenerateFromPassword([]byte(*password), bcrypt.DefaultCost)
		if hashErr != nil {
			return LoginResponse{}, fmt.Errorf("hash password: %w", hashErr)
		}
		user.PasswordHash = string(hash)
		user.MustChangePassword = false
	}
	saved, err := s.users.Save(ctx, user)
	if err != nil {
		return LoginResponse{}, err
	}
	if err := s.sessions.RevokeUserSessions(ctx, saved.ID); err != nil {
		return LoginResponse{}, err
	}
	return s.issueSession(ctx, saved)
}

func (s *Service) Refresh(ctx context.Context, refreshToken string) (LoginResponse, error) {
	userID, ok, err := s.sessions.ResolveRefresh(ctx, refreshToken, s.refreshTTL)
	if err != nil {
		return LoginResponse{}, err
	}
	if !ok {
		return LoginResponse{}, ErrInvalidCredentials
	}
	user, err := s.users.FindByID(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return LoginResponse{}, ErrInvalidCredentials
	}
	if err != nil {
		return LoginResponse{}, fmt.Errorf("load refresh user: %w", err)
	}
	return s.issueAccess(ctx, user, refreshToken)
}

func (s *Service) Logout(ctx context.Context, sessionID, refreshToken string) error {
	if err := s.sessions.Revoke(ctx, sessionID); err != nil {
		return err
	}
	return s.sessions.RevokeRefresh(ctx, refreshToken)
}

func (s *Service) Authenticate(ctx context.Context, rawToken string) (Principal, error) {
	claims, err := s.tokens.Parse(rawToken)
	if err != nil || claims.Subject == "" || claims.ID == "" {
		return Principal{}, ErrInvalidCredentials
	}
	ok, err := s.sessions.BelongsToUser(ctx, claims.ID, claims.Subject)
	if err != nil || !ok {
		return Principal{}, ErrInvalidCredentials
	}
	user, err := s.users.FindByID(ctx, claims.Subject)
	if err != nil {
		return Principal{}, ErrInvalidCredentials
	}
	return Principal{User: userDTO(user), SessionID: claims.ID}, nil
}

func (s *Service) issueSession(ctx context.Context, user store.User) (LoginResponse, error) {
	refreshToken, err := s.sessions.CreateRefresh(ctx, user.ID, s.refreshTTL)
	if err != nil {
		return LoginResponse{}, err
	}
	result, err := s.issueAccess(ctx, user, refreshToken)
	if err != nil {
		_ = s.sessions.RevokeRefresh(context.WithoutCancel(ctx), refreshToken)
		return LoginResponse{}, err
	}
	return result, nil
}

func (s *Service) issueAccess(ctx context.Context, user store.User, refreshToken string) (LoginResponse, error) {
	issued, err := s.tokens.Issue(UserIdentity{ID: user.ID, Username: user.Username, Role: user.Role})
	if err != nil {
		return LoginResponse{}, err
	}
	if err := s.sessions.Store(ctx, issued.SessionID, user.ID, s.tokens.expiration); err != nil {
		return LoginResponse{}, err
	}
	return LoginResponse{Token: issued.Token, User: userDTO(user), RefreshToken: refreshToken}, nil
}

func userDTO(user store.User) UserDTO {
	return UserDTO{ID: user.ID, Username: user.Username, Email: user.Email, Role: user.Role, MustChangePassword: user.MustChangePassword}
}

func normalizedOptional(value *string) *string {
	if value == nil {
		return nil
	}
	normalized := strings.ToLower(strings.TrimSpace(*value))
	if normalized == "" {
		return nil
	}
	return &normalized
}

func nonBlankOptional(value *string) *string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil
	}
	return value
}
