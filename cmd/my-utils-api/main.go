package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alexey-va/my-utils-api/internal/agent"
	"github.com/alexey-va/my-utils-api/internal/auth"
	"github.com/alexey-va/my-utils-api/internal/config"
	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/httpapi"
	"github.com/alexey-va/my-utils-api/internal/migrate"
	"github.com/alexey-va/my-utils-api/internal/observability"
	"github.com/alexey-va/my-utils-api/internal/openrouter"
	"github.com/alexey-va/my-utils-api/internal/report"
	"github.com/alexey-va/my-utils-api/internal/settings"
	"github.com/alexey-va/my-utils-api/internal/startup"
	"github.com/alexey-va/my-utils-api/internal/store"
	"github.com/alexey-va/my-utils-api/internal/telegram"
	workflowtemporal "github.com/alexey-va/my-utils-api/internal/temporal"
	"github.com/alexey-va/my-utils-api/internal/wireguard"
	"github.com/alexey-va/my-utils-api/internal/workout"
	migrations "github.com/alexey-va/my-utils-api/src/main/resources/db/migration"
	"github.com/redis/go-redis/v9"
)

var gitCommit = "unknown"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 4 * time.Second}
		if err := healthcheck(context.Background(), "http://127.0.0.1:8080/api/health", client); err != nil {
			os.Exit(1)
		}
		return
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if err := run(ctx); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func healthcheck(ctx context.Context, url string, client *http.Client) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4<<10))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned %d", response.StatusCode)
	}
	return nil
}

func configuredOutboundProxy(proxy config.HTTPProxy) (*url.URL, string) {
	if !proxy.Enabled {
		return nil, ""
	}
	host := strings.Trim(strings.TrimSpace(proxy.Host), "[]")
	value := &url.URL{Scheme: "http", Host: net.JoinHostPort(host, strconv.Itoa(proxy.Port))}
	return value, value.String()
}

func run(ctx context.Context) error {
	slog.InfoContext(ctx, "starting my-utils-api", "gitCommit", gitCommit)
	cfg, err := config.Load(os.LookupEnv)
	if err != nil {
		return err
	}
	pool, err := store.Open(ctx, cfg.Postgres.URL())
	if err != nil {
		return err
	}
	defer pool.Close()
	migrationsRunner, err := migrate.NewRunner(pool, migrations.FS)
	if err != nil {
		return err
	}
	redisClient := redis.NewClient(&redis.Options{
		Addr:        cfg.Redis.Address(),
		DialTimeout: 10 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
	})
	defer func() { _ = redisClient.Close() }()
	tokens, err := auth.NewJWTService(cfg.JWT.Secret, cfg.JWT.Expiration, time.Now)
	if err != nil {
		return err
	}
	sessions := auth.NewSessionStore(
		redisClient,
		cfg.Session.RedisKeyPrefix,
		cfg.Session.UserSessionsKeyPrefix,
		cfg.Session.RefreshKeyPrefix,
		cfg.Session.UserRefreshKeyPrefix,
	)
	users := store.NewUserStore(pool)
	authService := auth.NewService(users, tokens, sessions, cfg.Session.RefreshTTL)
	adminBootstrap := auth.NewAdminBootstrap(users, auth.BootstrapConfig{
		Enabled: cfg.Auth.BootstrapAdmin.Enabled, Username: cfg.Auth.BootstrapAdmin.Username,
		Password: cfg.Auth.BootstrapAdmin.Password, Email: cfg.Auth.BootstrapAdmin.Email,
	})
	var temporalService *workflowtemporal.Service
	runtimeSettings := settings.NewService(settings.AppCatalog(func(applyContext context.Context) error {
		if temporalService == nil {
			return nil
		}
		return temporalService.RefreshEveningReminders(applyContext)
	}), store.NewSettingsStore(pool))
	workoutService := workout.NewService(pool)
	healthService := health.NewService(pool)
	wireGuardCipher, err := wireguard.NewCredentialsCipher(cfg.WireGuard.CredentialsEncryptionKey)
	if err != nil {
		return err
	}
	wireGuardService := wireguard.NewService(pool, wireGuardCipher)
	agentMemory := agent.NewMemory(pool, nil)
	agentMemory.SetZoneID(func() string { return runtimeSettings.String(settings.TemporalZoneID) })
	metrics := observability.NewMetrics()
	metrics.RegisterWireGuard(wireGuardService)
	reportRenderer := report.NewRenderer()
	telegramProxy, outboundProxyURL := configuredOutboundProxy(cfg.OpenRouter.Proxy)
	var telegramClient *telegram.Client
	var agentStatus *telegram.StatusMessenger
	if cfg.Telegram.Enabled {
		// Keep the JVM contract: Telegram and OpenRouter share the configured
		// outbound HTTP proxy. Production cannot reach Telegram directly.
		telegramClient = telegram.NewClient(cfg.Telegram.BotToken, "https://api.telegram.org", telegramProxy)
		agentStatus = telegram.NewStatusMessenger(telegramClient, redisClient)
	}
	telegramFiles := telegram.NewFileDelivery(
		telegram.FileUploadToken(cfg.Telegram.FileUploadToken, cfg.Telegram.BotToken),
		cfg.Telegram.AllowedUserIDs,
		telegramClient,
	)
	allowedUsers := make(map[int64]bool, len(cfg.Telegram.AllowedUserIDs))
	for _, userID := range cfg.Telegram.AllowedUserIDs {
		allowedUsers[userID] = true
	}
	temporalActivities := &workflowtemporal.Activities{
		Workout: workoutService, Health: healthService, Messenger: telegramClient,
		Renderer: reportRenderer, Status: agentStatus, Metrics: metrics, Allowed: allowedUsers,
	}
	if cfg.Temporal.Enabled {
		if telegramClient == nil {
			return errors.New("MYUTILS_TEMPORAL_ENABLED requires MYUTILS_TELEGRAM_ENABLED")
		}
		temporalService = workflowtemporal.NewService(workflowtemporal.ServiceConfig{
			Target: cfg.Temporal.Target, Namespace: cfg.Temporal.Namespace, TaskQueue: cfg.Temporal.TaskQueue,
			AllowedChatIDs: cfg.Telegram.AllowedUserIDs,
			ZoneID:         func() string { return runtimeSettings.String(settings.TemporalZoneID) },
			ReminderOn:     func() bool { return runtimeSettings.Bool(settings.TemporalReminderEnabled) },
			ReminderHour:   func() int { return runtimeSettings.Int(settings.TemporalReminderHour) },
			ReminderMinute: func() int { return runtimeSettings.Int(settings.TemporalReminderMinute) },
		}, temporalActivities)
		defer temporalService.Close()
	}
	var turner *agent.AgentTurner
	var autoCompactor *agent.AutoCompactor
	if cfg.OpenRouter.APIKey != "" {
		openRouterClient, clientErr := openrouter.New(openrouter.Config{
			APIKey: cfg.OpenRouter.APIKey, BaseURL: cfg.OpenRouter.BaseURL,
			HTTPReferer: cfg.OpenRouter.HTTPReferer, AppTitle: cfg.OpenRouter.AppTitle,
			Timeout:         3 * time.Minute,
			MaxAttemptsFunc: func() int { return runtimeSettings.Int(settings.OpenRouterRetryAttempts) },
			InitialDelayFunc: func() time.Duration {
				return time.Duration(runtimeSettings.Int(settings.OpenRouterRetryDelayMS)) * time.Millisecond
			},
			ProxyURL: outboundProxyURL,
		})
		if clientErr != nil {
			return clientErr
		}
		contextualConversation := agent.NewContextualConversation(
			agentMemory,
			workoutService,
			healthService,
			func() int { return runtimeSettings.Int(settings.AgentCalendarDays) },
			func() int { return runtimeSettings.Int(settings.AgentRecentEntries) },
			func() int { return runtimeSettings.Int(settings.AgentProgressSessions) },
			func() string { return runtimeSettings.String(settings.TemporalZoneID) },
		)
		var delivery agent.ToolDelivery
		if telegramClient != nil {
			delivery = report.NewDelivery(workoutService, reportRenderer, telegramClient)
		}
		toolService := agent.NewToolService(pool, workoutService, healthService, agentMemory, temporalService, delivery)
		toolService.SetZoneID(func() string { return runtimeSettings.String(settings.TemporalZoneID) })
		turner = agent.NewTurner(agent.TurnerConfig{
			Model:             func() string { return runtimeSettings.String(settings.OpenRouterModel) },
			MaxToolIterations: func() int { return runtimeSettings.Int(settings.OpenRouterMaxTools) },
			RecentMessages:    func() int { return runtimeSettings.Int(settings.AgentRecentMessages) },
			SystemPrompt:      func() string { return runtimeSettings.String(settings.AgentSystemPrompt) },
			TemporalEnabled:   cfg.Temporal.Enabled,
		}, openRouterClient, contextualConversation, toolService)
		turner.SetMetrics(metrics)
		toolService.SetMetrics(metrics)
		agentMemory.SetTurner(turner)
		compactor := agent.NewCompactor(pool, openRouterClient, func() string {
			model := runtimeSettings.String(settings.AgentCompactModel)
			if model == "" {
				model = runtimeSettings.String(settings.OpenRouterModel)
			}
			return model
		})
		autoCompactor = agent.NewAutoCompactor(
			compactor,
			func() int { return runtimeSettings.Int(settings.AgentRecentMessages) },
			func() int { return runtimeSettings.Int(settings.AgentCompactThreshold) },
		)
		agentMemory.SetCompactor(compactor)
		agentMemory.SetAutoCompactor(autoCompactor)
	}
	temporalActivities.Agent = turner
	var telegramRunner *telegram.Runner
	if cfg.Telegram.Enabled {
		if turner == nil {
			return errors.New("OPENROUTER_API_KEY is required when Telegram is enabled")
		}
		var dispatcher telegram.Dispatcher
		if temporalService != nil {
			dispatcher = telegram.DispatchFunc(func(dispatchContext context.Context, chatID, userID int64, text string) error {
				if len(allowedUsers) > 0 && !allowedUsers[userID] {
					metrics.RecordAgentRequest("none", "rejected")
					_, dispatchErr := telegramClient.SendHTMLMessage(dispatchContext, chatID, "У вас нет доступа к этому боту.", "")
					return dispatchErr
				}
				if text != "/start" && agentStatus != nil {
					agentStatus.Begin(dispatchContext, chatID)
				}
				_, dispatchErr := temporalService.StartAgentTurn(dispatchContext, workflowtemporal.AgentTurnInput{
					ChatID: chatID, UserID: userID, Text: text, DeliverToTelegram: true,
				})
				if dispatchErr != nil && agentStatus != nil {
					agentStatus.Complete(dispatchContext, chatID)
				}
				return dispatchErr
			})
		} else {
			dispatcher = telegram.DispatchFunc(func(dispatchContext context.Context, chatID, userID int64, text string) error {
				if len(allowedUsers) > 0 && !allowedUsers[userID] {
					metrics.RecordAgentRequest("none", "rejected")
					_, dispatchErr := telegramClient.SendHTMLMessage(dispatchContext, chatID, "У вас нет доступа к этому боту.", "")
					return dispatchErr
				}
				if text == "/start" {
					metrics.RecordAgentRequest("direct", "received")
					metrics.RecordAgentTurn("direct", "start_command", 0)
					_, dispatchErr := telegramClient.SendHTMLMessage(dispatchContext, chatID, "Тренер по дневнику. Напиши «что на сегодня» — скажу, что уже было, и предложу план. Или сразу запиши подход: «жим 70 3*10/12».", "")
					return dispatchErr
				}
				if agentStatus != nil {
					agentStatus.Begin(dispatchContext, chatID)
					dispatchContext = agent.WithTurnStatus(dispatchContext, agentStatus)
				}
				result, dispatchErr := turner.Turn(dispatchContext, chatID, text, nil, false)
				if dispatchErr != nil {
					return dispatchErr
				}
				_, dispatchErr = telegramClient.SendHTMLMessage(dispatchContext, chatID, agent.NormalizeReply(result.Reply), "")
				return dispatchErr
			})
		}
		telegramRunner = telegram.NewRunner(telegramClient, dispatcher, cfg.Telegram.PollingEnabled)
		defer telegramRunner.Close()
	}
	router := httpapi.NewRouter(httpapi.Dependencies{
		Auth: authService, Settings: runtimeSettings, Workout: workoutService, Health: healthService,
		WireGuard: wireGuardService, AgentMemory: agentMemory, TelegramFiles: telegramFiles,
		Metrics: metrics, CORS: cfg.CORS.AllowedOrigins,
		RefreshCookie: httpapi.RefreshCookieConfig{
			Name: cfg.Session.RefreshCookieName, TTL: cfg.Session.RefreshTTL, Secure: cfg.Session.RefreshCookieSecure,
		},
	})
	server := &http.Server{
		Addr: cfg.HTTP.Address, Handler: router,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute,
	}

	warmers := []startup.Warmer{migrationsRunner, adminBootstrap, runtimeSettings, tokens, sessions}
	if temporalService != nil {
		warmers = append(warmers, temporalService)
	}
	if telegramRunner != nil {
		warmers = append(warmers, telegramRunner)
	}
	return startup.Run(ctx, warmers, func(servingContext context.Context) error {
		refreshContext, cancelRefresh := context.WithCancel(servingContext)
		defer cancelRefresh()
		go runtimeSettings.RunRefresh(refreshContext, time.Minute)
		if autoCompactor != nil {
			go autoCompactor.Run(servingContext)
		}
		shutdownComplete := make(chan struct{})
		go func() {
			defer close(shutdownComplete)
			<-servingContext.Done()
			shutdownContext, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdownContext)
		}()
		slog.InfoContext(servingContext, "HTTP server listening", "address", cfg.HTTP.Address)
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			<-shutdownComplete
			return nil
		}
		return err
	})
}
