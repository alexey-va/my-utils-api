package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadUsesContractCompatibleDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := Load(mapLookup(nil))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTP.Address != ":8080" {
		t.Errorf("HTTP address = %q, want :8080", cfg.HTTP.Address)
	}
	if cfg.Postgres.Host != "localhost" || cfg.Postgres.Port != 5432 || cfg.Postgres.Database != "myutils" {
		t.Errorf("Postgres defaults = %#v", cfg.Postgres)
	}
	if cfg.Redis.Host != "localhost" || cfg.Redis.Port != 6379 {
		t.Errorf("Redis defaults = %#v", cfg.Redis)
	}
	if cfg.JWT.Expiration != 24*time.Hour {
		t.Errorf("JWT expiration = %s, want 24h", cfg.JWT.Expiration)
	}
	if cfg.Temporal.TaskQueue != "myutils-go-v1" {
		t.Errorf("Temporal task queue = %q, want myutils-go-v1", cfg.Temporal.TaskQueue)
	}
	if cfg.Session.RedisKeyPrefix != "myutils:session:" || cfg.Session.UserSessionsKeyPrefix != "myutils:user-sessions:" || cfg.Session.RefreshKeyPrefix != "myutils:refresh:" || cfg.Session.UserRefreshKeyPrefix != "myutils:user-refresh-sessions:" {
		t.Errorf("session prefixes = %#v", cfg.Session)
	}
	if cfg.Session.RefreshTTL != 30*24*time.Hour || cfg.Session.RefreshCookieName != "myutils_refresh_session" {
		t.Errorf("refresh session defaults = %#v", cfg.Session)
	}
	if cfg.Session.RefreshCookieSecure {
		t.Error("refresh cookie should stay non-Secure in local HTTP development")
	}
}

func TestLoadReadsExistingEnvironmentNames(t *testing.T) {
	t.Parallel()

	cfg, err := Load(mapLookup(map[string]string{
		"SERVER_PORT":                     "18080",
		"POSTGRES_HOST":                   "postgres.internal",
		"POSTGRES_PORT":                   "15432",
		"POSTGRES_DB":                     "utilities",
		"POSTGRES_USER":                   "service",
		"POSTGRES_PASSWORD":               "database-secret",
		"REDIS_HOST":                      "redis.internal",
		"REDIS_PORT":                      "16379",
		"MYUTILS_JWT_SECRET":              strings.Repeat("j", 48),
		"MYUTILS_JWT_EXPIRATION_HOURS":    "12",
		"MYUTILS_REFRESH_SESSION_DAYS":    "45",
		"MYUTILS_REFRESH_COOKIE_NAME":     "custom_refresh",
		"MYUTILS_REFRESH_COOKIE_SECURE":   "true",
		"MYUTILS_TEMPORAL_ENABLED":        "true",
		"TEMPORAL_TARGET":                 "temporal:7233",
		"TELEGRAM_ALLOWED_USER_IDS":       " 42, 1001,broken ",
		"MYUTILS_CORS_ALLOWED_ORIGINS":    "https://utils.example.test,http://localhost:5173",
		"MYUTILS_BOOTSTRAP_ADMIN_ENABLED": "false",
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.HTTP.Address != ":18080" {
		t.Errorf("HTTP address = %q", cfg.HTTP.Address)
	}
	if cfg.Postgres.Host != "postgres.internal" || cfg.Postgres.Port != 15432 || cfg.Postgres.Database != "utilities" || cfg.Postgres.User != "service" || cfg.Postgres.Password != "database-secret" {
		t.Errorf("Postgres config = %#v", cfg.Postgres)
	}
	if cfg.Redis.Host != "redis.internal" || cfg.Redis.Port != 16379 {
		t.Errorf("Redis config = %#v", cfg.Redis)
	}
	if cfg.JWT.Expiration != 12*time.Hour {
		t.Errorf("JWT expiration = %s", cfg.JWT.Expiration)
	}
	if cfg.Session.RefreshTTL != 45*24*time.Hour || cfg.Session.RefreshCookieName != "custom_refresh" || !cfg.Session.RefreshCookieSecure {
		t.Errorf("refresh session config = %#v", cfg.Session)
	}
	if !cfg.Temporal.Enabled || cfg.Temporal.Target != "temporal:7233" {
		t.Errorf("Temporal config = %#v", cfg.Temporal)
	}
	if len(cfg.Telegram.AllowedUserIDs) != 2 || cfg.Telegram.AllowedUserIDs[0] != 42 || cfg.Telegram.AllowedUserIDs[1] != 1001 {
		t.Errorf("allowed user IDs = %#v", cfg.Telegram.AllowedUserIDs)
	}
	if len(cfg.CORS.AllowedOrigins) != 2 || cfg.CORS.AllowedOrigins[0] != "https://utils.example.test" {
		t.Errorf("CORS origins = %#v", cfg.CORS.AllowedOrigins)
	}
	if cfg.Auth.BootstrapAdmin.Enabled {
		t.Error("bootstrap admin should be disabled")
	}
}

func TestProductionRejectsDefaultJWTSecret(t *testing.T) {
	t.Parallel()

	_, err := Load(mapLookup(map[string]string{"MYUTILS_ENV": "production"}))
	if err == nil || !strings.Contains(err.Error(), "MYUTILS_JWT_SECRET") {
		t.Fatalf("Load() error = %v, want MYUTILS_JWT_SECRET validation", err)
	}
}

func TestProductionDefaultsRefreshCookieToSecure(t *testing.T) {
	t.Parallel()

	cfg, err := Load(mapLookup(map[string]string{
		"MYUTILS_ENV":        "production",
		"MYUTILS_JWT_SECRET": strings.Repeat("p", 32),
	}))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !cfg.Session.RefreshCookieSecure {
		t.Error("production refresh cookie must default to Secure")
	}
}

func TestVPNTelegramRequiresDedicatedCompleteConfiguration(t *testing.T) {
	t.Parallel()
	base := map[string]string{
		"MYUTILS_VPN_TELEGRAM_ENABLED": "true",
		"VPN_TELEGRAM_BOT_TOKEN":       "vpn-token",
		"VPN_TELEGRAM_ADMIN_USER_IDS":  "42,1001",
		"VPN_TELEGRAM_RELAY_ID":        "00000000-0000-0000-0000-000000000001",
	}
	cfg, err := Load(mapLookup(base))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.VPNTelegram.Enabled || !cfg.VPNTelegram.PollingEnabled || len(cfg.VPNTelegram.AdminUserIDs) != 2 || cfg.VPNTelegram.RelayID == "" {
		t.Fatalf("VPN Telegram config = %#v", cfg.VPNTelegram)
	}
	base["MYUTILS_TELEGRAM_ENABLED"] = "true"
	base["TELEGRAM_BOT_TOKEN"] = "vpn-token"
	if _, err := Load(mapLookup(base)); err == nil || !strings.Contains(err.Error(), "must be different") {
		t.Fatalf("shared Telegram token error = %v", err)
	}
}

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
