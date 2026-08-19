package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type UserIdentity struct {
	ID       string
	Username string
	Role     string
}

type IssuedToken struct {
	Token     string
	SessionID string
	UserID    string
}

type Claims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	jwt.RegisteredClaims
}

type JWTService struct {
	key        []byte
	expiration time.Duration
	method     jwt.SigningMethod
	clock      func() time.Time
}

func NewJWTService(secret string, expiration time.Duration, clock func() time.Time) (*JWTService, error) {
	key := []byte(secret)
	if len(key) < 32 {
		return nil, fmt.Errorf("JWT HMAC secret must contain at least 32 bytes, got %d", len(key))
	}
	if expiration <= 0 {
		return nil, errors.New("JWT expiration must be positive")
	}
	if clock == nil {
		return nil, errors.New("JWT clock must not be nil")
	}

	var method jwt.SigningMethod
	switch {
	case len(key) >= 64:
		method = jwt.SigningMethodHS512
	case len(key) >= 48:
		method = jwt.SigningMethodHS384
	default:
		method = jwt.SigningMethodHS256
	}
	return &JWTService{key: key, expiration: expiration, method: method, clock: clock}, nil
}

func (s *JWTService) Name() string { return "jwt" }

func (s *JWTService) Algorithm() string { return s.method.Alg() }

func (s *JWTService) Warm(context.Context) error {
	warmup := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "startup-warmup",
			ExpiresAt: jwt.NewNumericDate(time.Unix(0, 0).UTC()),
		},
	}
	token, err := jwt.NewWithClaims(s.method, warmup).SignedString(s.key)
	if err != nil {
		return fmt.Errorf("sign warmup JWT: %w", err)
	}

	claims := &Claims{}
	parsed, err := jwt.NewParser(
		jwt.WithValidMethods([]string{s.method.Alg()}),
		jwt.WithoutClaimsValidation(),
	).ParseWithClaims(token, claims, func(*jwt.Token) (any, error) { return s.key, nil })
	if err != nil {
		return fmt.Errorf("verify warmup JWT: %w", err)
	}
	if !parsed.Valid || claims.Subject != "startup-warmup" {
		return errors.New("warmup JWT signature or subject is invalid")
	}
	return nil
}

func (s *JWTService) Issue(user UserIdentity) (IssuedToken, error) {
	sessionID, err := randomUUID()
	if err != nil {
		return IssuedToken{}, fmt.Errorf("create JWT session ID: %w", err)
	}
	now := s.clock().UTC().Truncate(time.Second)
	claims := Claims{
		Username: user.Username,
		Role:     user.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        sessionID,
			Subject:   user.ID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(s.expiration)),
		},
	}
	signed, err := jwt.NewWithClaims(s.method, claims).SignedString(s.key)
	if err != nil {
		return IssuedToken{}, fmt.Errorf("sign JWT: %w", err)
	}
	return IssuedToken{Token: signed, SessionID: sessionID, UserID: user.ID}, nil
}

func (s *JWTService) Parse(raw string) (*Claims, error) {
	claims := &Claims{}
	parsed, err := jwt.NewParser(
		jwt.WithValidMethods([]string{s.method.Alg()}),
		jwt.WithTimeFunc(s.clock),
	).ParseWithClaims(raw, claims, func(*jwt.Token) (any, error) { return s.key, nil })
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("JWT is invalid")
	}
	return claims, nil
}

func randomUUID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	var encoded [36]byte
	hex.Encode(encoded[0:8], value[0:4])
	encoded[8] = '-'
	hex.Encode(encoded[9:13], value[4:6])
	encoded[13] = '-'
	hex.Encode(encoded[14:18], value[6:8])
	encoded[18] = '-'
	hex.Encode(encoded[19:23], value[8:10])
	encoded[23] = '-'
	hex.Encode(encoded[24:36], value[10:16])
	return string(encoded[:]), nil
}
