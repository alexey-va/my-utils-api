package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const defaultJWTSecret = "change-me-in-production-use-long-random-string-at-least-32-chars"

type LookupEnv func(string) (string, bool)

type Config struct {
	Environment string
	HTTP        HTTP
	Postgres    Postgres
	Redis       Redis
	JWT         JWT
	Session     Session
	Auth        Auth
	CORS        CORS
	Telegram    Telegram
	OpenRouter  OpenRouter
	Temporal    Temporal
	WireGuard   WireGuard
}

type HTTP struct {
	Address string
}

type Postgres struct {
	Host     string
	Port     int
	Database string
	User     string
	Password string
}

func (p Postgres) URL() string {
	u := &url.URL{
		Scheme: "postgres",
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
		Path:   p.Database,
	}
	u.User = url.UserPassword(p.User, p.Password)
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

type Redis struct {
	Host string
	Port int
}

func (r Redis) Address() string {
	return net.JoinHostPort(r.Host, strconv.Itoa(r.Port))
}

type JWT struct {
	Secret     string
	Expiration time.Duration
}

type Session struct {
	RedisKeyPrefix        string
	UserSessionsKeyPrefix string
	RefreshKeyPrefix      string
	UserRefreshKeyPrefix  string
	RefreshTTL            time.Duration
	RefreshCookieName     string
	RefreshCookieSecure   bool
}

type Auth struct {
	BootstrapAdmin BootstrapAdmin
}

type BootstrapAdmin struct {
	Enabled  bool
	Username string
	Password string
	Email    string
}

type CORS struct {
	AllowedOrigins []string
}

type Telegram struct {
	Enabled         bool
	PollingEnabled  bool
	BotToken        string
	FileUploadToken string
	AllowedUserIDs  []int64
}

type OpenRouter struct {
	APIKey      string
	BaseURL     string
	HTTPReferer string
	AppTitle    string
	Proxy       HTTPProxy
}

type HTTPProxy struct {
	Enabled bool
	Host    string
	Port    int
}

type Temporal struct {
	Enabled   bool
	Target    string
	Namespace string
	TaskQueue string
}

type WireGuard struct {
	CredentialsEncryptionKey string
}

func Load(lookup LookupEnv) (Config, error) {
	if lookup == nil {
		return Config{}, errors.New("environment lookup is nil")
	}

	get := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			return value
		}
		return fallback
	}
	getInt := func(key string, fallback int) (int, error) {
		raw := strings.TrimSpace(get(key, strconv.Itoa(fallback)))
		value, err := strconv.Atoi(raw)
		if err != nil || value <= 0 || value > 65535 {
			return 0, fmt.Errorf("%s must be an integer between 1 and 65535", key)
		}
		return value, nil
	}
	getBool := func(key string, fallback bool) (bool, error) {
		raw := strings.TrimSpace(get(key, strconv.FormatBool(fallback)))
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return false, fmt.Errorf("%s must be true or false", key)
		}
		return value, nil
	}
	environment := strings.ToLower(strings.TrimSpace(get("MYUTILS_ENV", "development")))

	serverPort, err := getInt("SERVER_PORT", 8080)
	if err != nil {
		return Config{}, err
	}
	postgresPort, err := getInt("POSTGRES_PORT", 5432)
	if err != nil {
		return Config{}, err
	}
	redisPort, err := getInt("REDIS_PORT", 6379)
	if err != nil {
		return Config{}, err
	}
	proxyPort, err := getInt("OPENROUTER_PROXY_PORT", 8888)
	if err != nil {
		return Config{}, err
	}
	expirationHours, err := strconv.ParseInt(strings.TrimSpace(get("MYUTILS_JWT_EXPIRATION_HOURS", "24")), 10, 64)
	if err != nil || expirationHours <= 0 {
		return Config{}, errors.New("MYUTILS_JWT_EXPIRATION_HOURS must be a positive integer")
	}
	refreshDays, err := strconv.ParseInt(strings.TrimSpace(get("MYUTILS_REFRESH_SESSION_DAYS", "30")), 10, 64)
	if err != nil || refreshDays <= 0 {
		return Config{}, errors.New("MYUTILS_REFRESH_SESSION_DAYS must be a positive integer")
	}
	refreshCookieSecure, err := getBool("MYUTILS_REFRESH_COOKIE_SECURE", environment == "production")
	if err != nil {
		return Config{}, err
	}
	temporalEnabled, err := getBool("MYUTILS_TEMPORAL_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	telegramEnabled, err := getBool("MYUTILS_TELEGRAM_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	pollingEnabled, err := getBool("MYUTILS_TELEGRAM_POLLING_ENABLED", true)
	if err != nil {
		return Config{}, err
	}
	proxyEnabled, err := getBool("OPENROUTER_PROXY_ENABLED", false)
	if err != nil {
		return Config{}, err
	}
	bootstrapEnabled, err := getBool("MYUTILS_BOOTSTRAP_ADMIN_ENABLED", true)
	if err != nil {
		return Config{}, err
	}

	cfg := Config{
		Environment: environment,
		HTTP:        HTTP{Address: ":" + strconv.Itoa(serverPort)},
		Postgres: Postgres{
			Host:     strings.TrimSpace(get("POSTGRES_HOST", "localhost")),
			Port:     postgresPort,
			Database: strings.TrimSpace(get("POSTGRES_DB", "myutils")),
			User:     strings.TrimSpace(get("POSTGRES_USER", "myutils")),
			Password: get("POSTGRES_PASSWORD", "myutils"),
		},
		Redis: Redis{
			Host: strings.TrimSpace(get("REDIS_HOST", "localhost")),
			Port: redisPort,
		},
		JWT: JWT{
			Secret:     get("MYUTILS_JWT_SECRET", defaultJWTSecret),
			Expiration: time.Duration(expirationHours) * time.Hour,
		},
		Session: Session{
			RedisKeyPrefix:        get("MYUTILS_SESSION_REDIS_KEY_PREFIX", "myutils:session:"),
			UserSessionsKeyPrefix: get("MYUTILS_SESSION_USER_SESSIONS_KEY_PREFIX", "myutils:user-sessions:"),
			RefreshKeyPrefix:      get("MYUTILS_REFRESH_REDIS_KEY_PREFIX", "myutils:refresh:"),
			UserRefreshKeyPrefix:  get("MYUTILS_USER_REFRESH_REDIS_KEY_PREFIX", "myutils:user-refresh-sessions:"),
			RefreshTTL:            time.Duration(refreshDays) * 24 * time.Hour,
			RefreshCookieName:     strings.TrimSpace(get("MYUTILS_REFRESH_COOKIE_NAME", "myutils_refresh_session")),
			RefreshCookieSecure:   refreshCookieSecure,
		},
		Auth: Auth{BootstrapAdmin: BootstrapAdmin{
			Enabled:  bootstrapEnabled,
			Username: strings.TrimSpace(get("MYUTILS_BOOTSTRAP_ADMIN_USERNAME", "freedeeml")),
			Password: get("MYUTILS_BOOTSTRAP_ADMIN_PASSWORD", "admin"),
			Email:    strings.ToLower(strings.TrimSpace(get("MYUTILS_BOOTSTRAP_ADMIN_EMAIL", "freedeeml@local.invalid"))),
		}},
		CORS: CORS{AllowedOrigins: splitNonEmpty(get("MYUTILS_CORS_ALLOWED_ORIGINS", "http://localhost:5173,http://localhost:4173,http://127.0.0.1:5173"))},
		Telegram: Telegram{
			Enabled:         telegramEnabled,
			PollingEnabled:  pollingEnabled,
			BotToken:        get("TELEGRAM_BOT_TOKEN", ""),
			FileUploadToken: get("TELEGRAM_FILE_UPLOAD_TOKEN", ""),
			AllowedUserIDs:  parseInt64List(get("TELEGRAM_ALLOWED_USER_IDS", "")),
		},
		OpenRouter: OpenRouter{
			APIKey:      get("OPENROUTER_API_KEY", ""),
			BaseURL:     strings.TrimRight(get("OPENROUTER_BASE_URL", "https://openrouter.ai/api/v1"), "/"),
			HTTPReferer: get("OPENROUTER_HTTP_REFERER", "https://github.com/alexey-va/my-utils"),
			AppTitle:    get("OPENROUTER_APP_TITLE", "my-utils-workout-bot"),
			Proxy: HTTPProxy{
				Enabled: proxyEnabled,
				Host:    strings.TrimSpace(get("OPENROUTER_PROXY_HOST", "")),
				Port:    proxyPort,
			},
		},
		Temporal: Temporal{
			Enabled:   temporalEnabled,
			Target:    strings.TrimSpace(get("TEMPORAL_TARGET", "127.0.0.1:7233")),
			Namespace: strings.TrimSpace(get("TEMPORAL_NAMESPACE", "default")),
			TaskQueue: "myutils-go-v1",
		},
		WireGuard: WireGuard{CredentialsEncryptionKey: get("WIREGUARD_CREDENTIALS_ENCRYPTION_KEY", "")},
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) validate() error {
	if c.Postgres.Host == "" || c.Postgres.Database == "" || c.Postgres.User == "" {
		return errors.New("PostgreSQL host, database and user must not be blank")
	}
	if c.Redis.Host == "" {
		return errors.New("Redis host must not be blank")
	}
	if c.Session.RefreshCookieName == "" {
		return errors.New("MYUTILS_REFRESH_COOKIE_NAME must not be blank")
	}
	if len(c.JWT.Secret) < 32 {
		return errors.New("MYUTILS_JWT_SECRET must contain at least 32 bytes")
	}
	if c.Environment == "production" && c.JWT.Secret == defaultJWTSecret {
		return errors.New("MYUTILS_JWT_SECRET must be explicitly configured in production")
	}
	if c.Telegram.Enabled && strings.TrimSpace(c.Telegram.BotToken) == "" {
		return errors.New("TELEGRAM_BOT_TOKEN is required when Telegram is enabled")
	}
	if c.OpenRouter.Proxy.Enabled && c.OpenRouter.Proxy.Host == "" {
		return errors.New("OPENROUTER_PROXY_HOST is required when the proxy is enabled")
	}
	return nil
}

func splitNonEmpty(raw string) []string {
	parts := strings.Split(raw, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func parseInt64List(raw string) []int64 {
	parts := splitNonEmpty(raw)
	result := make([]int64, 0, len(parts))
	for _, part := range parts {
		value, err := strconv.ParseInt(part, 10, 64)
		if err == nil {
			result = append(result, value)
		}
	}
	return result
}
