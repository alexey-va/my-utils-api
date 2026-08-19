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
	if cfg.Session.RedisKeyPrefix != "myutils:session:" || cfg.Session.UserSessionsKeyPrefix != "myutils:user-sessions:" {
		t.Errorf("session prefixes = %#v", cfg.Session)
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

func mapLookup(values map[string]string) LookupEnv {
	return func(key string) (string, bool) {
		value, ok := values[key]
		return value, ok
	}
}
