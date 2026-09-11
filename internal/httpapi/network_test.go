package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/auth"
)

type networkRequestCapture struct {
	sync.Mutex
	method string
	path   string
	query  string
	auth   string
	cookie string
	body   []byte
	calls  int
}

type networkAuthProbe struct {
	fakeAuth
	calls int
}

func (p *networkAuthProbe) Authenticate(ctx context.Context, token string) (auth.Principal, error) {
	p.calls++
	return p.fakeAuth.Authenticate(ctx, token)
}

func (c *networkRequestCapture) handler(response http.ResponseWriter, request *http.Request) {
	body := make([]byte, request.ContentLength)
	if request.ContentLength > 0 {
		_, _ = request.Body.Read(body)
	}
	c.Lock()
	c.method = request.Method
	c.path = request.URL.Path
	c.query = request.URL.RawQuery
	c.auth = request.Header.Get("Authorization")
	c.cookie = request.Header.Get("Cookie")
	c.body = body
	c.calls++
	c.Unlock()
	response.WriteHeader(http.StatusNoContent)
}

func TestNetworkProxyAllowlistedRoutes(t *testing.T) {
	t.Parallel()

	capture := &networkRequestCapture{}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	defer server.Close()
	proxy, err := NewNetworkProxy(NetworkProxyConfig{BaseURL: server.URL, Token: "private-token"})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, Network: proxy})

	tests := []struct {
		name     string
		method   string
		path     string
		upstream string
		admin    bool
	}{
		{"admin health", http.MethodGet, "/api/admin/network/health", "/healthz", true},
		{"admin nodes", http.MethodGet, "/api/admin/network/v1/nodes", "/v1/nodes", true},
		{"admin actions", http.MethodGet, "/api/admin/network/v1/actions", "/v1/actions", true},
		{"admin jobs list", http.MethodGet, "/api/admin/network/v1/jobs", "/v1/jobs", true},
		{"admin jobs create", http.MethodPost, "/api/admin/network/v1/jobs", "/v1/jobs", true},
		{"admin job", http.MethodGet, "/api/admin/network/v1/jobs/job-1", "/v1/jobs/job-1", true},
		{"admin cancel", http.MethodPost, "/api/admin/network/v1/jobs/job-1/cancel", "/v1/jobs/job-1/cancel", true},
		{"admin credentials", http.MethodGet, "/api/admin/network/v1/credentials", "/v1/credentials", true},
		{"admin credential create", http.MethodPost, "/api/admin/network/v1/credentials", "/v1/credentials", true},
		{"admin credential delete", http.MethodDelete, "/api/admin/network/v1/credentials/cred-1", "/v1/credentials/cred-1", true},
		{"admin enrollment", http.MethodPost, "/api/admin/network/v1/enrollments", "/v1/enrollments", true},
		{"admin disable", http.MethodPost, "/api/admin/network/v1/nodes/node-1/disable", "/v1/nodes/node-1/disable", true},
		{"public nodes", http.MethodGet, "/api/network/v1/nodes", "/v1/nodes", false},
		{"public actions", http.MethodGet, "/api/network/v1/actions", "/v1/actions", false},
		{"public jobs list", http.MethodGet, "/api/network/v1/jobs", "/v1/jobs", false},
		{"public jobs create", http.MethodPost, "/api/network/v1/jobs", "/v1/jobs", false},
		{"public job", http.MethodGet, "/api/network/v1/jobs/job-1", "/v1/jobs/job-1", false},
		{"public cancel", http.MethodPost, "/api/network/v1/jobs/job-1/cancel", "/v1/jobs/job-1/cancel", false},
		{"public enrollment", http.MethodPost, "/api/network/v1/enroll", "/v1/enroll", false},
		{"agent poll", http.MethodPost, "/api/network/v1/agent/poll", "/v1/agent/poll", false},
		{"agent start", http.MethodPost, "/api/network/v1/agent/jobs/job-1/start", "/v1/agent/jobs/job-1/start", false},
		{"agent result", http.MethodPost, "/api/network/v1/agent/jobs/job-1/result", "/v1/agent/jobs/job-1/result", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(`{"name":"test"}`))
			if test.admin {
				request.Header.Set("Authorization", "Bearer ready-admin")
			} else {
				request.Header.Set("Authorization", "Bearer scoped-token")
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			capture.Lock()
			gotPath := capture.path
			capture.Unlock()
			if gotPath != test.upstream {
				t.Fatalf("upstream path = %q, want %q", gotPath, test.upstream)
			}
		})
	}
}

func TestNetworkProxyAuthHeaderIsolation(t *testing.T) {
	t.Parallel()

	capture := &networkRequestCapture{}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	defer server.Close()
	proxy, err := NewNetworkProxy(NetworkProxyConfig{BaseURL: server.URL, Token: "private-token"})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, Network: proxy})

	tests := []struct {
		name       string
		path       string
		authorize  string
		wantBearer string
	}{
		{"admin replaces browser token", "/api/admin/network/v1/nodes", "Bearer ready-admin", "Bearer private-token"},
		{"public preserves scoped token", "/api/network/v1/jobs", "Bearer scoped-token", "Bearer scoped-token"},
		{"public drops non-bearer", "/api/network/v1/jobs", "Basic browser-secret", ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			request.Header.Set("Authorization", test.authorize)
			request.Header.Set("Cookie", "myutils_refresh=browser-secret")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusNoContent {
				t.Fatalf("status = %d, body=%s", response.Code, response.Body.String())
			}
			capture.Lock()
			gotAuth, gotCookie := capture.auth, capture.cookie
			capture.Unlock()
			if gotAuth != test.wantBearer || gotCookie != "" {
				t.Fatalf("upstream headers auth=%q cookie=%q, want auth=%q cookie empty", gotAuth, gotCookie, test.wantBearer)
			}
		})
	}
}

func TestNetworkPublicRoutesDoNotAuthenticateAsMyUtilsJWT(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	proxy, err := NewNetworkProxy(NetworkProxyConfig{BaseURL: server.URL, Token: "private-token"})
	if err != nil {
		t.Fatal(err)
	}
	probe := &networkAuthProbe{}
	router := NewRouter(Dependencies{Auth: probe, Settings: fakeSettings{}, Network: proxy})
	request := httptest.NewRequest(http.MethodGet, "/api/network/v1/nodes", nil)
	request.Header.Set("Authorization", "Bearer ready-admin")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || probe.calls != 0 {
		t.Fatalf("public route status=%d JWT authenticate calls=%d", response.Code, probe.calls)
	}
}

func TestNetworkProxyPublicSurfaceCannotReachAdminRoutes(t *testing.T) {
	t.Parallel()

	capture := &networkRequestCapture{}
	server := httptest.NewServer(http.HandlerFunc(capture.handler))
	defer server.Close()
	proxy, err := NewNetworkProxy(NetworkProxyConfig{BaseURL: server.URL, Token: "private-token"})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, Network: proxy})
	for _, path := range []string{
		"/api/network/v1/credentials",
		"/api/network/v1/credentials/cred-1",
		"/api/network/v1/enrollments",
		"/api/network/v1/nodes/node-1/disable",
	} {
		request := httptest.NewRequest(http.MethodPost, path, nil)
		request.Header.Set("Authorization", "Bearer ready-admin")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("POST %s status = %d, want %d", path, response.Code, http.StatusUnauthorized)
		}
	}
	capture.Lock()
	defer capture.Unlock()
	if capture.calls != 0 {
		t.Fatalf("blocked public admin paths reached upstream %d times", capture.calls)
	}
}

func TestNetworkProxyRequestAndResponseLimits(t *testing.T) {
	t.Parallel()

	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		calls++
		if request.Method == http.MethodPost {
			response.WriteHeader(http.StatusNoContent)
			return
		}
		response.WriteHeader(http.StatusOK)
		_, _ = response.Write(bytes.Repeat([]byte("x"), maxNetworkResponseBytes+1))
	}))
	defer server.Close()
	proxy, err := NewNetworkProxy(NetworkProxyConfig{BaseURL: server.URL, Token: "private-token"})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, Network: proxy})

	tooLarge := httptest.NewRequest(http.MethodPost, "/api/network/v1/jobs", bytes.NewReader(bytes.Repeat([]byte("x"), maxNetworkRequestBytes+1)))
	tooLargeResponse := httptest.NewRecorder()
	router.ServeHTTP(tooLargeResponse, tooLarge)
	if tooLargeResponse.Code != http.StatusRequestEntityTooLarge || calls != 0 {
		t.Fatalf("oversized request status=%d upstream calls=%d", tooLargeResponse.Code, calls)
	}

	tooLargeResponse = httptest.NewRecorder()
	router.ServeHTTP(tooLargeResponse, httptest.NewRequest(http.MethodGet, "/api/network/v1/nodes", nil))
	if tooLargeResponse.Code != http.StatusBadGateway || strings.Contains(tooLargeResponse.Body.String(), "xxx") {
		t.Fatalf("oversized response status=%d body=%s", tooLargeResponse.Code, tooLargeResponse.Body.String())
	}
}

func TestNetworkProxyUpstreamErrorsDoNotLeakBody(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusBadGateway)
		_, _ = response.Write([]byte(`{"error":"private upstream detail"}`))
	}))
	defer server.Close()
	proxy, err := NewNetworkProxy(NetworkProxyConfig{BaseURL: server.URL, Token: "private-token"})
	if err != nil {
		t.Fatal(err)
	}
	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}, Network: proxy})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/network/v1/nodes", nil))
	if response.Code != http.StatusBadGateway || strings.Contains(response.Body.String(), "private upstream detail") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body["error"] == "" {
		t.Fatalf("error body = %s", response.Body.String())
	}
}

func TestNetworkProxyAbsentAndURLValidation(t *testing.T) {
	t.Parallel()

	proxy, err := NewNetworkProxy(NetworkProxyConfig{})
	if err != nil || proxy != nil {
		t.Fatalf("empty config = proxy %v, err %v", proxy, err)
	}
	for _, rawURL := range []string{
		"not-a-url",
		"ftp://gateway.example",
		"https://user:password@gateway.example",
		"https://gateway.example?target=other",
	} {
		if _, err := NewNetworkProxy(NetworkProxyConfig{BaseURL: rawURL, Token: "token"}); err == nil {
			t.Fatalf("NewNetworkProxy(%q) accepted invalid URL", rawURL)
		}
	}
	if _, err := NewNetworkProxy(NetworkProxyConfig{BaseURL: "https://gateway.example"}); err == nil {
		t.Fatal("missing token was accepted")
	}

	router := NewRouter(Dependencies{Auth: fakeAuth{}, Settings: fakeSettings{}})
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/network/v1/nodes", nil))
	if response.Code != http.StatusServiceUnavailable || !strings.Contains(response.Body.String(), "RCNET_URL") {
		t.Fatalf("absent proxy status=%d body=%s", response.Code, response.Body.String())
	}
}
