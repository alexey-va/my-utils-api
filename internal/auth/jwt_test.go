package auth

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestJWTIssueAndParsePreservesJavaClaimContract(t *testing.T) {
	t.Parallel()

	clock := func() time.Time { return time.Unix(1_800_000_000, 0).UTC() }
	service, err := NewJWTService(strings.Repeat("s", 48), 12*time.Hour, clock)
	if err != nil {
		t.Fatalf("NewJWTService() error = %v", err)
	}

	issued, err := service.Issue(UserIdentity{
		ID:       "bbf2ae8e-3097-481e-a48c-5e8af6f7856d",
		Username: "alexey",
		Role:     "ADMIN",
	})
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	claims, err := service.Parse(issued.Token)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if claims.Subject != "bbf2ae8e-3097-481e-a48c-5e8af6f7856d" {
		t.Errorf("sub = %q", claims.Subject)
	}
	if claims.ID == "" || claims.ID != issued.SessionID {
		t.Errorf("jti = %q, issued session = %q", claims.ID, issued.SessionID)
	}
	if claims.Username != "alexey" || claims.Role != "ADMIN" {
		t.Errorf("custom claims = %#v", claims)
	}
	if !claims.IssuedAt.Time.Equal(clock()) {
		t.Errorf("iat = %s, want %s", claims.IssuedAt.Time, clock())
	}
	if !claims.ExpiresAt.Time.Equal(clock().Add(12 * time.Hour)) {
		t.Errorf("exp = %s", claims.ExpiresAt.Time)
	}
	if service.Algorithm() != "HS384" {
		t.Errorf("algorithm = %q, want HS384 for a 48-byte JJWT key", service.Algorithm())
	}
}

func TestJWTWarmupSignsAndVerifiesBeforeStartupContinues(t *testing.T) {
	t.Parallel()

	service, err := NewJWTService(strings.Repeat("w", 64), time.Hour, time.Now)
	if err != nil {
		t.Fatalf("NewJWTService() error = %v", err)
	}
	if service.Name() != "jwt" {
		t.Errorf("Name() = %q", service.Name())
	}
	if err := service.Warm(context.Background()); err != nil {
		t.Fatalf("Warm() error = %v", err)
	}
	if service.Algorithm() != "HS512" {
		t.Errorf("algorithm = %q, want HS512", service.Algorithm())
	}
}

func TestJWTRejectsShortHMACSecret(t *testing.T) {
	t.Parallel()

	_, err := NewJWTService("too-short", time.Hour, time.Now)
	if err == nil || !strings.Contains(err.Error(), "32") {
		t.Fatalf("NewJWTService() error = %v, want minimum key length validation", err)
	}
}
