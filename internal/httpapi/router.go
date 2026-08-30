package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"regexp"
	"strings"
	"time"

	"github.com/alexey-va/my-utils-api/internal/agent"
	"github.com/alexey-va/my-utils-api/internal/auth"
	"github.com/alexey-va/my-utils-api/internal/health"
	"github.com/alexey-va/my-utils-api/internal/observability"
	"github.com/alexey-va/my-utils-api/internal/settings"
	"github.com/alexey-va/my-utils-api/internal/telegram"
	"github.com/alexey-va/my-utils-api/internal/wireguard"
	"github.com/alexey-va/my-utils-api/internal/workout"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type AuthService interface {
	Login(context.Context, string, string) (auth.LoginResponse, error)
	Register(context.Context, auth.RegisterRequest) (auth.LoginResponse, error)
	Refresh(context.Context, string) (auth.LoginResponse, error)
	Profile(context.Context, string) (auth.UserDTO, error)
	UpdateCredentials(context.Context, string, auth.UpdateCredentialsRequest) (auth.LoginResponse, error)
	Logout(context.Context, string, string) error
	Authenticate(context.Context, string) (auth.Principal, error)
}

type SettingsService interface {
	List() []settings.View
	Update(context.Context, string, json.RawMessage, string) (settings.View, error)
	String(string) string
}

type WorkoutService interface {
	ListExercises(context.Context) ([]workout.Exercise, error)
	CreateExercise(context.Context, workout.CreateExerciseRequest) (workout.Exercise, error)
	UpdateExercise(context.Context, string, workout.CreateExerciseRequest) (workout.Exercise, error)
	DeleteExercise(context.Context, string) error
	Grid(context.Context) (workout.Grid, error)
	Progress(context.Context, string) (workout.Progress, error)
	UpsertEntry(context.Context, workout.EntryRequest) error
	MoveEntry(context.Context, workout.MoveRequest) error
	DeleteEntry(context.Context, string, string) error
}

type HealthService interface {
	UpsertSteps(context.Context, health.ParsedSteps) (int, error)
	StepsHistory(context.Context, int, time.Time) (health.StepsHistory, error)
	UpsertWeight(context.Context, float64, string) (health.WeightResult, error)
	UpsertWeights(context.Context, []health.WeightDay) ([]health.WeightResult, error)
	WeightHistory(context.Context, int, time.Time) (health.WeightHistory, error)
}

type WireGuardService interface {
	ListRelays(context.Context) ([]wireguard.Relay, error)
	CreateRelay(context.Context, wireguard.CreateRelayRequest) (wireguard.CreatedRelay, error)
	RotateToken(context.Context, string) (wireguard.AgentTokenResponse, error)
	DeleteRelay(context.Context, string) error
	UpdateExitPreference(context.Context, string, wireguard.UpdateExitPreferenceRequest) (wireguard.Relay, error)
	Snapshot(context.Context, string, string) (wireguard.Snapshot, error)
	ListPeers(context.Context, string, string) ([]wireguard.Peer, error)
	CreatePeer(context.Context, string, wireguard.CreatePeerRequest) (wireguard.PeerCredentials, error)
	Credentials(context.Context, string, string) (wireguard.PeerCredentials, error)
	UpdatePeer(context.Context, string, string, wireguard.UpdatePeerRequest) (wireguard.Peer, error)
	ReorderPeers(context.Context, string, wireguard.UpdatePeerOrderRequest) error
	DeletePeer(context.Context, string, string) error
	Desired(context.Context, string) (wireguard.DesiredState, error)
	Heartbeat(context.Context, string, wireguard.Heartbeat) error
	Metrics(context.Context, string, string, string) (wireguard.Metrics, error)
	AgentTokenMatches(context.Context, string, string) bool
}

type AgentMemoryService interface {
	ListChats(context.Context) ([]agent.ChatSummary, error)
	Detail(context.Context, int64) (agent.ChatDetail, error)
	Messages(context.Context, int64, *int64, int) (agent.MessagePage, error)
	AppendManual(context.Context, int64, string, string, []string) (agent.Message, error)
	CreateFact(context.Context, int64, string, *float64) (agent.Fact, error)
	UpdateFact(context.Context, string, string, *float64) (agent.Fact, error)
	DeleteFact(context.Context, string) error
	DeleteSummary(context.Context, string) error
	ExcludeMessage(context.Context, int64, bool) (agent.Message, error)
	DeleteMessage(context.Context, int64) error
	ClearDialog(context.Context, int64) error
	Turn(context.Context, int64, string, []string, bool) (agent.TurnResult, error)
	Compact(context.Context, int64, int) (agent.CompactResult, error)
	CreateTestChat(context.Context, string) (agent.TestChat, error)
	ListTestChats(context.Context) ([]agent.TestChat, error)
	TestChat(context.Context, string) (agent.TestChat, error)
	RenameTestChat(context.Context, string, string) (agent.TestChat, error)
	DeleteTestChat(context.Context, string) error
	ClearTestChat(context.Context, string) error
}

type TelegramFileService interface {
	Deliver(context.Context, string, string, string, string, []byte) (telegram.FileUploadResponse, error)
}

type Dependencies struct {
	Auth          AuthService
	Settings      SettingsService
	Workout       WorkoutService
	Health        HealthService
	WireGuard     WireGuardService
	AgentMemory   AgentMemoryService
	TelegramFiles TelegramFileService
	Metrics       *observability.Metrics
	CORS          []string
	RefreshCookie RefreshCookieConfig
}

type RefreshCookieConfig struct {
	Name   string
	TTL    time.Duration
	Secure bool
}

type principalKey struct{}

func NewRouter(dependencies Dependencies) http.Handler {
	api := &API{auth: dependencies.Auth, settings: dependencies.Settings, workout: dependencies.Workout, health: dependencies.Health, wireGuard: dependencies.WireGuard, agentMemory: dependencies.AgentMemory, telegramFiles: dependencies.TelegramFiles, refreshCookie: dependencies.RefreshCookie}
	router := chi.NewRouter()
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	if dependencies.Metrics != nil {
		router.Use(dependencies.Metrics.HTTPMiddleware)
	}
	router.Use(corsMiddleware(dependencies.CORS))
	router.Use(api.optionalAuthentication)

	router.Get("/api/health", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
	})
	router.Get("/actuator/health", func(response http.ResponseWriter, _ *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{"status": "UP"})
	})
	if dependencies.Metrics != nil {
		router.Handle("/actuator/prometheus", dependencies.Metrics.Handler())
	}

	router.Post("/api/auth/login", api.login)
	router.Post("/api/auth/register", api.register)
	router.Post("/api/auth/refresh", api.refresh)
	router.Post("/api/client-events", api.clientEvents)
	api.registerWorkoutRoutes(router)
	api.registerHealthRoutes(router)
	api.registerWireGuardAgentRoutes(router)
	if api.telegramFiles != nil {
		router.Post("/api/telegram/files", api.uploadTelegramFile)
	}
	router.Group(func(protected chi.Router) {
		protected.Use(api.requireAuthenticated)
		protected.Post("/api/auth/logout", api.logout)
		protected.Get("/api/auth/me", api.me)
		protected.Post("/api/auth/credentials", api.updateCredentials)
	})

	router.Group(func(admin chi.Router) {
		admin.Use(api.requireAdmin)
		admin.Get("/api/admin/settings", api.listSettings)
		admin.Get("/api/admin/settings/{key:.+}", api.getSetting)
		admin.Put("/api/admin/settings/{key:.+}", api.updateSetting)
		api.registerWireGuardAdminRoutes(admin)
		api.registerAgentAdminRoutes(admin)
	})
	router.NotFound(api.notFound)
	router.MethodNotAllowed(api.methodNotAllowed)

	return router
}

type API struct {
	auth          AuthService
	settings      SettingsService
	workout       WorkoutService
	health        HealthService
	wireGuard     WireGuardService
	agentMemory   AgentMemoryService
	telegramFiles TelegramFileService
	refreshCookie RefreshCookieConfig
}

func (a *API) optionalAuthentication(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		header := request.Header.Get("Authorization")
		if strings.HasPrefix(header, "Bearer ") && a.auth != nil {
			token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
			if principal, err := a.auth.Authenticate(request.Context(), token); err == nil {
				request = request.WithContext(context.WithValue(request.Context(), principalKey{}, principal))
			}
		}
		next.ServeHTTP(response, request)
	})
}

func (a *API) requireAuthenticated(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if _, ok := principalFrom(request.Context()); !ok {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (a *API) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		principal, ok := principalFrom(request.Context())
		if !ok {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		if principal.User.Role != "ADMIN" || principal.User.MustChangePassword {
			response.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (a *API) notFound(response http.ResponseWriter, request *http.Request) {
	if _, ok := principalFrom(request.Context()); !ok {
		writeError(response, http.StatusUnauthorized, "Unauthorized")
		return
	}
	writeError(response, http.StatusNotFound, "Not found")
}

func (a *API) methodNotAllowed(response http.ResponseWriter, request *http.Request) {
	if _, ok := principalFrom(request.Context()); !ok {
		writeError(response, http.StatusUnauthorized, "Unauthorized")
		return
	}
	writeError(response, http.StatusMethodNotAllowed, "Method not allowed")
}

func principalFrom(ctx context.Context) (auth.Principal, bool) {
	principal, ok := ctx.Value(principalKey{}).(auth.Principal)
	return principal, ok
}

func (a *API) login(response http.ResponseWriter, request *http.Request) {
	var body struct {
		Login    string `json:"login"`
		Password string `json:"password"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	if strings.TrimSpace(body.Login) == "" || strings.TrimSpace(body.Password) == "" {
		writeError(response, http.StatusBadRequest, "Validation failed")
		return
	}
	result, err := a.auth.Login(request.Context(), body.Login, body.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(response, http.StatusUnauthorized, "Invalid credentials")
			return
		}
		writeError(response, http.StatusInternalServerError, "Internal server error")
		return
	}
	a.setRefreshCookie(response, result.RefreshToken)
	writeJSON(response, http.StatusOK, result)
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func (a *API) register(response http.ResponseWriter, request *http.Request) {
	var body auth.RegisterRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if !validUsername(body.Username) || !validEmail(body.Email) || len(body.Password) < 8 || len(body.Password) > 128 {
		writeError(response, http.StatusBadRequest, "Validation failed")
		return
	}
	result, err := a.auth.Register(request.Context(), body)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			writeError(response, http.StatusConflict, "Username is already taken")
		case errors.Is(err, auth.ErrEmailTaken):
			writeError(response, http.StatusConflict, "Email is already registered")
		default:
			writeError(response, http.StatusInternalServerError, "Internal server error")
		}
		return
	}
	a.setRefreshCookie(response, result.RefreshToken)
	writeJSON(response, http.StatusOK, result)
}

func (a *API) refresh(response http.ResponseWriter, request *http.Request) {
	refreshToken := a.readRefreshCookie(request)
	if refreshToken == "" {
		a.clearRefreshCookie(response)
		writeError(response, http.StatusUnauthorized, "Session expired")
		return
	}
	result, err := a.auth.Refresh(request.Context(), refreshToken)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			a.clearRefreshCookie(response)
			writeError(response, http.StatusUnauthorized, "Session expired")
			return
		}
		writeError(response, http.StatusInternalServerError, "Internal server error")
		return
	}
	a.setRefreshCookie(response, result.RefreshToken)
	writeJSON(response, http.StatusOK, result)
}

func (a *API) logout(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFrom(request.Context())
	refreshToken := a.readRefreshCookie(request)
	a.clearRefreshCookie(response)
	if err := a.auth.Logout(request.Context(), principal.SessionID, refreshToken); err != nil {
		writeError(response, http.StatusInternalServerError, "Internal server error")
		return
	}
	writeJSON(response, http.StatusOK, nil)
}

func (a *API) me(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFrom(request.Context())
	profile, err := a.auth.Profile(request.Context(), principal.User.ID)
	if err != nil {
		writeError(response, http.StatusUnauthorized, "Account not found")
		return
	}
	writeJSON(response, http.StatusOK, profile)
}

func (a *API) updateCredentials(response http.ResponseWriter, request *http.Request) {
	principal, _ := principalFrom(request.Context())
	var body auth.UpdateCredentialsRequest
	if !decodeJSON(response, request, &body) {
		return
	}
	if strings.TrimSpace(body.CurrentPassword) == "" || (body.Username != nil && !validUsername(*body.Username)) || (body.Email != nil && !validEmail(*body.Email)) || (body.NewPassword != nil && (len(*body.NewPassword) < 8 || len(*body.NewPassword) > 128)) {
		writeError(response, http.StatusBadRequest, "Validation failed")
		return
	}
	result, err := a.auth.UpdateCredentials(request.Context(), principal.User.ID, body)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrUsernameTaken):
			writeError(response, http.StatusConflict, "Username is already taken")
		case errors.Is(err, auth.ErrEmailTaken):
			writeError(response, http.StatusConflict, "Email is already registered")
		case errors.Is(err, auth.ErrNoCredentialChange):
			writeError(response, http.StatusBadRequest, "No credential changes supplied")
		case strings.Contains(err.Error(), "current password"):
			writeError(response, http.StatusUnauthorized, "Current password is incorrect")
		default:
			writeError(response, http.StatusInternalServerError, "Internal server error")
		}
		return
	}
	a.setRefreshCookie(response, result.RefreshToken)
	writeJSON(response, http.StatusOK, result)
}

func (a *API) readRefreshCookie(request *http.Request) string {
	if a.refreshCookie.Name == "" {
		return ""
	}
	cookie, err := request.Cookie(a.refreshCookie.Name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(cookie.Value)
}

func (a *API) setRefreshCookie(response http.ResponseWriter, token string) {
	if a.refreshCookie.Name == "" || token == "" || a.refreshCookie.TTL <= 0 {
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name: a.refreshCookie.Name, Value: token, Path: "/api/auth",
		MaxAge: int(a.refreshCookie.TTL / time.Second), HttpOnly: true, Secure: a.refreshCookie.Secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (a *API) clearRefreshCookie(response http.ResponseWriter) {
	if a.refreshCookie.Name == "" {
		return
	}
	http.SetCookie(response, &http.Cookie{
		Name: a.refreshCookie.Name, Path: "/api/auth", MaxAge: -1, Expires: time.Unix(1, 0),
		HttpOnly: true, Secure: a.refreshCookie.Secure, SameSite: http.SameSiteLaxMode,
	})
}

func (a *API) listSettings(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, a.settings.List())
}

func (a *API) getSetting(response http.ResponseWriter, request *http.Request) {
	key := chi.URLParam(request, "key")
	for _, view := range a.settings.List() {
		if view.Key == key {
			writeJSON(response, http.StatusOK, view)
			return
		}
	}
	writeError(response, http.StatusNotFound, "Unknown property: "+key)
}

func (a *API) updateSetting(response http.ResponseWriter, request *http.Request) {
	var body struct {
		Value json.RawMessage `json:"value"`
	}
	if !decodeJSON(response, request, &body) {
		return
	}
	if len(body.Value) == 0 {
		writeError(response, http.StatusBadRequest, "value is required")
		return
	}
	view, err := a.settings.Update(request.Context(), chi.URLParam(request, "key"), body.Value, "")
	if err != nil {
		if errors.Is(err, settings.ErrUnknownSetting) {
			writeError(response, http.StatusNotFound, "Unknown property: "+chi.URLParam(request, "key"))
		} else {
			writeError(response, http.StatusBadRequest, err.Error())
		}
		return
	}
	writeJSON(response, http.StatusOK, view)
}

func decodeJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	decoder := json.NewDecoder(io.LimitReader(request.Body, 2<<20))
	if err := decoder.Decode(target); err != nil {
		writeError(response, http.StatusBadRequest, "Malformed JSON")
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(response, http.StatusBadRequest, "Malformed JSON")
		return false
	}
	return true
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	if value != nil {
		_ = json.NewEncoder(response).Encode(value)
	}
}

func writeError(response http.ResponseWriter, status int, message string) {
	writeJSON(response, status, map[string]string{"message": message})
}

func validUsername(value string) bool {
	trimmed := strings.TrimSpace(value)
	return len(trimmed) >= 3 && len(trimmed) <= 32 && usernamePattern.MatchString(trimmed)
}

func validEmail(value string) bool {
	trimmed := strings.TrimSpace(value)
	address, err := mail.ParseAddress(trimmed)
	return err == nil && address.Address == trimmed && strings.Contains(trimmed, "@")
}

func corsMiddleware(origins []string) func(http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		allowed[origin] = struct{}{}
	}
	clientEventAllowed := make(map[string]struct{}, len(allowed)+2)
	for origin := range allowed {
		clientEventAllowed[origin] = struct{}{}
	}
	clientEventAllowed["https://route.alexeyav.ru"] = struct{}{}
	clientEventAllowed["https://utils.alexeyav.ru"] = struct{}{}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			if !strings.HasPrefix(request.URL.Path, "/api/") {
				next.ServeHTTP(response, request)
				return
			}
			origin := strings.TrimSpace(request.Header.Get("Origin"))
			if origin == "" {
				next.ServeHTTP(response, request)
				return
			}

			isClientEvents := request.URL.Path == "/api/client-events"
			policyOrigins := allowed
			allowedMethods := "GET, POST, PUT, PATCH, DELETE, OPTIONS"
			if isClientEvents {
				policyOrigins = clientEventAllowed
				allowedMethods = "POST, OPTIONS"
			}
			if _, ok := policyOrigins[origin]; !ok {
				response.WriteHeader(http.StatusForbidden)
				return
			}

			preflight := request.Method == http.MethodOptions && request.Header.Get("Access-Control-Request-Method") != ""
			if preflight && !corsMethodAllowed(request.Header.Get("Access-Control-Request-Method"), isClientEvents) {
				response.WriteHeader(http.StatusForbidden)
				return
			}
			requestedHeaders := strings.TrimSpace(request.Header.Get("Access-Control-Request-Headers"))
			if preflight && isClientEvents && !onlyContentTypeHeader(requestedHeaders) {
				response.WriteHeader(http.StatusForbidden)
				return
			}

			response.Header().Set("Access-Control-Allow-Origin", origin)
			response.Header().Add("Vary", "Origin")
			response.Header().Set("Access-Control-Allow-Methods", allowedMethods)
			if isClientEvents {
				response.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			} else {
				response.Header().Set("Access-Control-Allow-Credentials", "true")
				if requestedHeaders != "" {
					response.Header().Set("Access-Control-Allow-Headers", requestedHeaders)
				}
			}
			if preflight {
				response.Header().Add("Vary", "Access-Control-Request-Method")
				response.Header().Add("Vary", "Access-Control-Request-Headers")
				response.Header().Set("Access-Control-Max-Age", "3600")
				response.WriteHeader(http.StatusOK)
				return
			}
			next.ServeHTTP(response, request)
		})
	}
}

func corsMethodAllowed(method string, clientEvents bool) bool {
	method = strings.ToUpper(strings.TrimSpace(method))
	if clientEvents {
		return method == http.MethodPost
	}
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func onlyContentTypeHeader(raw string) bool {
	if strings.TrimSpace(raw) == "" {
		return true
	}
	for _, header := range strings.Split(raw, ",") {
		if !strings.EqualFold(strings.TrimSpace(header), "Content-Type") {
			return false
		}
	}
	return true
}
