package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/auth"
	"github.com/alexey-va/my-utils-api/internal/settings"
	"github.com/alexey-va/my-utils-api/internal/wireguard"
)

func TestHealthContract(t *testing.T) {
	t.Parallel()

	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}})
	for _, test := range []struct {
		path string
		want string
	}{
		{path: "/api/health", want: `{"status":"ok"}`},
		{path: "/actuator/health", want: `{"status":"UP"}`},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || strings.TrimSpace(response.Body.String()) != test.want {
			t.Errorf("GET %s = %d %q", test.path, response.Code, response.Body.String())
		}
	}
}

func TestAdminSettingsSecurityMatrix(t *testing.T) {
	t.Parallel()

	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}})
	for _, test := range []struct {
		name  string
		token string
		want  int
	}{
		{name: "anonymous", want: http.StatusUnauthorized},
		{name: "user", token: "user", want: http.StatusForbidden},
		{name: "bootstrap admin", token: "bootstrap-admin", want: http.StatusForbidden},
		{name: "ready admin", token: "ready-admin", want: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestLoginAndRefreshKeepRefreshCredentialInHTTPOnlyCookie(t *testing.T) {
	t.Parallel()

	service := &cookieAuth{}
	router := NewRouter(Dependencies{
		Auth:     service,
		Settings: fakeSettings{},
		RefreshCookie: RefreshCookieConfig{
			Name: "myutils_refresh_session", TTL: 30 * 24 * time.Hour, Secure: true,
		},
	})
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"login":"admin","password":"password"}`))
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse := httptest.NewRecorder()

	router.ServeHTTP(loginResponse, loginRequest)

	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body=%s", loginResponse.Code, loginResponse.Body.String())
	}
	loginCookies := loginResponse.Result().Cookies()
	if len(loginCookies) != 1 {
		t.Fatalf("login cookies = %#v", loginCookies)
	}
	cookie := loginCookies[0]
	if cookie.Name != "myutils_refresh_session" || cookie.Value != "refresh-one" || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Path != "/api/auth" || cookie.MaxAge != 30*24*60*60 {
		t.Fatalf("login refresh cookie = %#v", cookie)
	}
	if strings.Contains(loginResponse.Body.String(), "refresh-one") {
		t.Fatalf("refresh credential leaked into JSON: %s", loginResponse.Body.String())
	}

	refreshRequest := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	refreshRequest.AddCookie(cookie)
	refreshResponse := httptest.NewRecorder()
	router.ServeHTTP(refreshResponse, refreshRequest)

	if refreshResponse.Code != http.StatusOK || service.refreshIn != "refresh-one" {
		t.Fatalf("refresh = status %d, input %q, body=%s", refreshResponse.Code, service.refreshIn, refreshResponse.Body.String())
	}
	if got := refreshResponse.Result().Cookies(); len(got) != 1 || got[0].Value != "refresh-one" || got[0].MaxAge != 30*24*60*60 {
		t.Fatalf("renewed refresh cookie = %#v", got)
	}
}

func TestRefreshRejectsMissingOrRevokedCookieAndClearsIt(t *testing.T) {
	t.Parallel()

	service := &cookieAuth{refreshErr: auth.ErrInvalidCredentials}
	router := NewRouter(Dependencies{
		Auth:          service,
		Settings:      fakeSettings{},
		RefreshCookie: RefreshCookieConfig{Name: "myutils_refresh_session", TTL: 30 * 24 * time.Hour, Secure: true},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: "myutils_refresh_session", Value: "revoked"})
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Name != "myutils_refresh_session" || cookies[0].MaxAge >= 0 {
		t.Fatalf("cleared refresh cookie = %#v", cookies)
	}
}

func TestRefreshKeepsCookieOnTransientBackendFailure(t *testing.T) {
	t.Parallel()

	service := &cookieAuth{refreshErr: errors.New("redis unavailable")}
	router := NewRouter(Dependencies{
		Auth:          service,
		Settings:      fakeSettings{},
		RefreshCookie: RefreshCookieConfig{Name: "myutils_refresh_session", TTL: 30 * 24 * time.Hour, Secure: true},
	})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", nil)
	request.AddCookie(&http.Cookie{Name: "myutils_refresh_session", Value: "still-valid"})
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if cookies := response.Result().Cookies(); len(cookies) != 0 {
		t.Fatalf("transient failure must not clear refresh cookie: %#v", cookies)
	}
}

type listRelaysService struct {
	WireGuardService
	relays []wireguard.Relay
}

func (service listRelaysService) ListRelays(context.Context) ([]wireguard.Relay, error) {
	return service.relays, nil
}

type wireGuardSnapshotService struct {
	WireGuardService
	snapshot wireguard.Snapshot
	rangeIn  string
}

type wireGuardExitPreferenceService struct {
	WireGuardService
	relayID    string
	preference string
}

type wireGuardCategoryService struct {
	WireGuardService
	relayID string
	name    string
}

func (service *wireGuardExitPreferenceService) UpdateExitPreference(_ context.Context, relayID string, body wireguard.UpdateExitPreferenceRequest) (wireguard.Relay, error) {
	service.relayID = relayID
	service.preference = body.Preference
	return wireguard.Relay{ID: relayID, ExitPreference: body.Preference}, nil
}

func (service *wireGuardSnapshotService) Snapshot(_ context.Context, _ string, rangeName string) (wireguard.Snapshot, error) {
	service.rangeIn = rangeName
	return service.snapshot, nil
}

func (service *wireGuardCategoryService) CreatePeerCategory(_ context.Context, relayID string, body wireguard.CreatePeerCategoryRequest) (wireguard.PeerCategory, error) {
	service.relayID = relayID
	service.name = body.Name
	return wireguard.PeerCategory{ID: "category-1", Name: body.Name, SortOrder: 2}, nil
}

func TestWireGuardRelayCollectionRouteIsNotShadowedByNestedRoutes(t *testing.T) {
	t.Parallel()

	router := NewRouter(Dependencies{
		Auth:      fakeAuth{},
		Settings:  fakeSettings{},
		WireGuard: listRelaysService{relays: []wireguard.Relay{{ID: "relay-1"}}},
	})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/wireguard/relays", nil)
	request.Header.Set("Authorization", "Bearer ready-admin")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	var relays []wireguard.Relay
	if err := json.Unmarshal(response.Body.Bytes(), &relays); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(relays) != 1 || relays[0].ID != "relay-1" {
		t.Fatalf("relays = %#v", relays)
	}
}

func TestWireGuardSnapshotRouteReturnsOneDashboardPayload(t *testing.T) {
	t.Parallel()

	service := &wireGuardSnapshotService{snapshot: wireguard.Snapshot{
		Relay: wireguard.Relay{ID: "relay-1"},
		Peers: []wireguard.Peer{{ID: "peer-1"}},
		PeerMetrics: map[string]wireguard.Metrics{
			"peer-1": {PeerID: "peer-1", Range: "WEEK"},
		},
	}}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, WireGuard: service})
	request := httptest.NewRequest(http.MethodGet, "/api/admin/wireguard/relays/"+testRelayID+"/snapshot?range=WEEK", nil)
	request.Header.Set("Authorization", "Bearer ready-admin")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if service.rangeIn != "WEEK" {
		t.Fatalf("range = %q, want WEEK", service.rangeIn)
	}
	var snapshot wireguard.Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if snapshot.Relay.ID != "relay-1" || len(snapshot.Peers) != 1 || snapshot.PeerMetrics["peer-1"].Range != "WEEK" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
}

func TestWireGuardExitPreferenceRouteUpdatesRelay(t *testing.T) {
	t.Parallel()

	service := &wireGuardExitPreferenceService{}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, WireGuard: service})
	request := httptest.NewRequest(http.MethodPut, "/api/admin/wireguard/relays/"+testRelayID+"/exit-preference", strings.NewReader(`{"preference":"SECONDARY"}`))
	request.Header.Set("Authorization", "Bearer ready-admin")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusOK, response.Body.String())
	}
	if service.relayID != testRelayID || service.preference != "SECONDARY" {
		t.Fatalf("update = relay %q preference %q", service.relayID, service.preference)
	}
	var relay wireguard.Relay
	if err := json.Unmarshal(response.Body.Bytes(), &relay); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if relay.ID != testRelayID || relay.ExitPreference != "SECONDARY" {
		t.Fatalf("relay = %#v", relay)
	}
}

func TestWireGuardPeerCategoryRouteCreatesPersistentRecord(t *testing.T) {
	t.Parallel()

	service := &wireGuardCategoryService{}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, WireGuard: service})
	request := httptest.NewRequest(http.MethodPost, "/api/admin/wireguard/relays/"+testRelayID+"/categories", strings.NewReader(`{"name":"Личные"}`))
	request.Header.Set("Authorization", "Bearer ready-admin")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d, body=%s", response.Code, http.StatusCreated, response.Body.String())
	}
	if service.relayID != testRelayID || service.name != "Личные" {
		t.Fatalf("create category = relay %q name %q", service.relayID, service.name)
	}
	var category wireguard.PeerCategory
	if err := json.Unmarshal(response.Body.Bytes(), &category); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if category.ID != "category-1" || category.Name != "Личные" || category.SortOrder != 2 {
		t.Fatalf("category = %#v", category)
	}
}

func TestSettingsUpdateKeepsValueAsJSONNotBase64(t *testing.T) {
	t.Parallel()

	store := fakeSettings{view: settings.View{
		Key: "temporal.evening-reminder.hour", Type: settings.TypeInt,
		Value: json.RawMessage("23"), DefaultValue: json.RawMessage("20"), Editor: "DEFAULT",
	}}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: store})
	request := httptest.NewRequest(http.MethodPut, "/api/admin/settings/temporal.evening-reminder.hour", strings.NewReader(`{"value":23}`))
	request.Header.Set("Authorization", "Bearer ready-admin")
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "MjM=") || !strings.Contains(response.Body.String(), `"value":23`) {
		t.Fatalf("JSON value was encoded incorrectly: %s", response.Body.String())
	}
}

func TestUnknownAndWrongMethodRoutesKeepDefaultDeny(t *testing.T) {
	t.Parallel()
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}})
	for _, test := range []struct {
		name, method, path, token string
		want                      int
	}{
		{name: "anonymous unknown", method: http.MethodGet, path: "/api/unknown", want: http.StatusUnauthorized},
		{name: "authenticated unknown", method: http.MethodGet, path: "/api/unknown", token: "user", want: http.StatusNotFound},
		{name: "anonymous wrong method", method: http.MethodGet, path: "/api/auth/login", want: http.StatusUnauthorized},
		{name: "authenticated wrong method", method: http.MethodGet, path: "/api/auth/login", token: "user", want: http.StatusMethodNotAllowed},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestJSONHandlersRejectTrailingDocuments(t *testing.T) {
	t.Parallel()
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}})
	request := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(`{"login":"a","password":"b"} {}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
	}
	if strings.Count(strings.TrimSpace(response.Body.String()), "\n") != 0 || !strings.Contains(response.Body.String(), `"Malformed JSON"`) {
		t.Fatalf("handler wrote more than one error document: %q", response.Body.String())
	}
}

func TestCORSKeepsSeparateAPIAndClientEventPolicies(t *testing.T) {
	t.Parallel()
	router := NewRouter(Dependencies{
		Auth: fakeAuth{}, Settings: fakeSettings{}, CORS: []string{"https://app.example.test"},
	})

	tests := []struct {
		name, method, path, origin, requestMethod, requestHeaders string
		wantStatus, wantCredentials                               int
		wantOrigin                                                string
	}{
		{
			name: "production route planner telemetry", method: http.MethodPost, path: "/api/client-events",
			origin: "https://route.alexeyav.ru", wantStatus: http.StatusNoContent, wantOrigin: "https://route.alexeyav.ru",
		},
		{
			name: "credentialed API preflight with custom header", method: http.MethodOptions, path: "/api/auth/me",
			origin: "https://app.example.test", requestMethod: http.MethodGet, requestHeaders: "Authorization, X-Custom",
			wantStatus: http.StatusOK, wantOrigin: "https://app.example.test", wantCredentials: 1,
		},
		{
			name: "client events reject credential header", method: http.MethodOptions, path: "/api/client-events",
			origin: "https://utils.alexeyav.ru", requestMethod: http.MethodPost, requestHeaders: "Authorization",
			wantStatus: http.StatusForbidden,
		},
		{
			name: "unknown origin rejected", method: http.MethodOptions, path: "/api/auth/me",
			origin: "https://attacker.example", requestMethod: http.MethodGet, wantStatus: http.StatusForbidden,
		},
		{
			name: "actuator is outside API CORS", method: http.MethodGet, path: "/actuator/health",
			origin: "https://app.example.test", wantStatus: http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Origin", test.origin)
			if test.requestMethod != "" {
				request.Header.Set("Access-Control-Request-Method", test.requestMethod)
			}
			if test.requestHeaders != "" {
				request.Header.Set("Access-Control-Request-Headers", test.requestHeaders)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d, body=%s", response.Code, test.wantStatus, response.Body.String())
			}
			if got := response.Header().Get("Access-Control-Allow-Origin"); got != test.wantOrigin {
				t.Fatalf("allow origin = %q, want %q", got, test.wantOrigin)
			}
			wantCredentials := ""
			if test.wantCredentials == 1 {
				wantCredentials = "true"
			}
			if got := response.Header().Get("Access-Control-Allow-Credentials"); got != wantCredentials {
				t.Fatalf("allow credentials = %q, want %q", got, wantCredentials)
			}
		})
	}
}

type fakeAuth struct{}

func (fakeAuth) Login(context.Context, string, string) (auth.LoginResponse, error) {
	return auth.LoginResponse{}, errors.New("unused")
}
func (fakeAuth) Register(context.Context, auth.RegisterRequest) (auth.LoginResponse, error) {
	return auth.LoginResponse{}, errors.New("unused")
}
func (fakeAuth) Profile(context.Context, string) (auth.UserDTO, error) {
	return auth.UserDTO{}, errors.New("unused")
}
func (fakeAuth) UpdateCredentials(context.Context, string, auth.UpdateCredentialsRequest) (auth.LoginResponse, error) {
	return auth.LoginResponse{}, errors.New("unused")
}
func (fakeAuth) Refresh(context.Context, string) (auth.LoginResponse, error) {
	return auth.LoginResponse{}, errors.New("unused")
}
func (fakeAuth) Logout(context.Context, string, string) error { return nil }
func (fakeAuth) Authenticate(_ context.Context, token string) (auth.Principal, error) {
	switch token {
	case "user":
		return auth.Principal{User: auth.UserDTO{ID: "user", Role: "USER"}}, nil
	case "bootstrap-admin":
		return auth.Principal{User: auth.UserDTO{ID: "bootstrap", Role: "ADMIN", MustChangePassword: true}}, nil
	case "ready-admin":
		return auth.Principal{User: auth.UserDTO{ID: "admin", Role: "ADMIN"}}, nil
	default:
		return auth.Principal{}, auth.ErrInvalidCredentials
	}
}

type cookieAuth struct {
	fakeAuth
	refreshIn  string
	refreshErr error
}

func (service *cookieAuth) Login(context.Context, string, string) (auth.LoginResponse, error) {
	return auth.LoginResponse{Token: "access-one", RefreshToken: "refresh-one", User: auth.UserDTO{ID: "admin", Username: "admin", Role: "ADMIN"}}, nil
}

func (service *cookieAuth) Refresh(_ context.Context, refreshToken string) (auth.LoginResponse, error) {
	service.refreshIn = refreshToken
	if service.refreshErr != nil {
		return auth.LoginResponse{}, service.refreshErr
	}
	return auth.LoginResponse{Token: "access-two", RefreshToken: refreshToken, User: auth.UserDTO{ID: "admin", Username: "admin", Role: "ADMIN"}}, nil
}

type fakeSettings struct {
	view settings.View
}

func (s fakeSettings) List() []settings.View {
	if s.view.Key == "" {
		return []settings.View{}
	}
	return []settings.View{s.view}
}
func (s fakeSettings) Update(_ context.Context, _ string, _ json.RawMessage, _ string) (settings.View, error) {
	return s.view, nil
}
func (s fakeSettings) String(string) string { return "Europe/Moscow" }
