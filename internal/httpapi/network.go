package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	maxNetworkRequestBytes  = 2 << 20
	maxNetworkResponseBytes = 4 << 20
	networkProxyTimeout     = 40 * time.Second
)

var errNetworkPayloadTooLarge = errors.New("network gateway payload exceeds the configured limit")

// NetworkProxyConfig describes the fixed, private RCNet gateway endpoint.
// BaseURL and Token are deployment values, never request-controlled values.
type NetworkProxyConfig struct {
	BaseURL string
	Token   string
	Client  *http.Client
	Timeout time.Duration
}

// NetworkActor identifies the authenticated My Utils user for audit events.
// It is supplied by the admin HTTP middleware and never read from request
// headers.
type NetworkActor struct {
	ID   string
	Name string
}

// NetworkProxy forwards the allowlisted RCNet API surface. It builds every
// upstream request from a pinned URL and never copies browser cookies.
type NetworkProxy struct {
	baseURL *url.URL
	token   string
	client  *http.Client
	timeout time.Duration
}

func NewNetworkProxy(config NetworkProxyConfig) (*NetworkProxy, error) {
	rawURL := strings.TrimSpace(config.BaseURL)
	if rawURL == "" {
		return nil, nil
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Opaque != "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("RCNET_URL must be an absolute HTTP(S) URL without credentials, query or fragment")
	}
	if !strings.EqualFold(parsed.Scheme, "http") && !strings.EqualFold(parsed.Scheme, "https") {
		return nil, errors.New("RCNET_URL must use http or https")
	}
	token := strings.TrimSpace(config.Token)
	if token == "" {
		return nil, errors.New("RCNET_TOKEN or RCNET_TOKEN_FILE is required when RCNET_URL is configured")
	}
	client := config.Client
	if client == nil {
		client = &http.Client{}
	} else {
		clientCopy := *client
		client = &clientCopy
	}
	// A redirect would escape the pinned destination and could send a private
	// service token to an untrusted host. Surface it as an upstream error.
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = networkProxyTimeout
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/")
	parsed.RawPath = ""
	return &NetworkProxy{baseURL: parsed, token: token, client: client, timeout: timeout}, nil
}

func (a *API) registerNetworkAdminRoutes(router chi.Router) {
	router.Get("/api/admin/network/health", a.networkAdminHealth)
	router.Route("/api/admin/network/v1", func(routes chi.Router) {
		routes.Get("/nodes", a.networkForward("/v1/nodes", true))
		routes.Get("/actions", a.networkForward("/v1/actions", true))
		routes.Get("/activity", a.networkForward("/v1/activity", true))
		routes.Get("/doctor", a.networkForward("/v1/doctor", true))
		routes.Get("/jobs", a.networkForward("/v1/jobs", true))
		routes.Post("/jobs", a.networkForward("/v1/jobs", true))
		routes.Get("/jobs/{jobID}", a.networkForwardID("/v1/jobs/", "jobID", "", true))
		routes.Post("/jobs/{jobID}/cancel", a.networkForwardID("/v1/jobs/", "jobID", "/cancel", true))
		routes.Get("/credentials", a.networkForward("/v1/credentials", true))
		routes.Post("/credentials", a.networkForward("/v1/credentials", true))
		routes.Delete("/credentials/{credentialID}", a.networkForwardID("/v1/credentials/", "credentialID", "", true))
		routes.Post("/enrollments", a.networkForward("/v1/enrollments", true))
		routes.Post("/nodes/{nodeID}/disable", a.networkForwardID("/v1/nodes/", "nodeID", "/disable", true))
	})
}

// registerNetworkAuditRoute deliberately uses the /api/network prefix from
// the RCNet contract, while being mounted in the My Utils admin group. The
// other /api/network/v1 routes remain the unauthenticated agent surface.
func (a *API) registerNetworkAuditRoute(router chi.Router) {
	router.Get("/api/network/v1/audit", a.networkForward("/v1/audit", true))
}

func (a *API) registerNetworkPublicRoutes(router chi.Router) {
	router.Get("/api/network/v1/nodes", a.networkForward("/v1/nodes", false))
	router.Get("/api/network/v1/actions", a.networkForward("/v1/actions", false))
	router.Get("/api/network/v1/jobs", a.networkForward("/v1/jobs", false))
	router.Post("/api/network/v1/jobs", a.networkForward("/v1/jobs", false))
	router.Get("/api/network/v1/jobs/{jobID}", a.networkForwardID("/v1/jobs/", "jobID", "", false))
	router.Post("/api/network/v1/jobs/{jobID}/cancel", a.networkForwardID("/v1/jobs/", "jobID", "/cancel", false))
	router.Post("/api/network/v1/enroll", a.networkForward("/v1/enroll", false))
	router.Post("/api/network/v1/agent/poll", a.networkForward("/v1/agent/poll", false))
	router.Post("/api/network/v1/agent/jobs/{jobID}/start", a.networkForwardID("/v1/agent/jobs/", "jobID", "/start", false))
	router.Post("/api/network/v1/agent/jobs/{jobID}/result", a.networkForwardID("/v1/agent/jobs/", "jobID", "/result", false))
}

func (a *API) networkForward(upstreamPath string, admin bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		a.forwardNetwork(response, request, upstreamPath, admin)
	}
}

func (a *API) networkForwardID(prefix, parameter, suffix string, admin bool) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		id := strings.TrimSpace(chi.URLParam(request, parameter))
		if id == "" || id == "." || id == ".." || strings.ContainsAny(id, "/\\?#") {
			writeNetworkError(response, http.StatusBadRequest, "Invalid network identifier")
			return
		}
		a.forwardNetwork(response, request, prefix+url.PathEscape(id)+suffix, admin)
	}
}

func (a *API) networkAdminHealth(response http.ResponseWriter, request *http.Request) {
	a.forwardNetwork(response, request, "/healthz", true)
}

func (a *API) forwardNetwork(response http.ResponseWriter, request *http.Request, upstreamPath string, admin bool) {
	if a.network == nil {
		writeNetworkError(response, http.StatusServiceUnavailable, "Network controller is not configured; set RCNET_URL")
		return
	}
	actor := NetworkActor{}
	if admin {
		if principal, ok := principalFrom(request.Context()); ok {
			actor = NetworkActor{ID: principal.User.ID, Name: principal.User.Username}
		}
	}
	if err := a.network.forward(response, request, upstreamPath, admin, actor); err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, errNetworkPayloadTooLarge) {
			status = http.StatusRequestEntityTooLarge
		} else {
			var gatewayErr networkGatewayError
			if errors.As(err, &gatewayErr) && gatewayErr.status >= http.StatusBadRequest && gatewayErr.status <= 599 {
				status = gatewayErr.status
			}
		}
		writeNetworkError(response, status, err.Error())
	}
}

type networkGatewayError struct {
	status int
}

func (e networkGatewayError) Error() string {
	return "network gateway returned an error"
}

func (p *NetworkProxy) forward(response http.ResponseWriter, request *http.Request, upstreamPath string, admin bool, actor NetworkActor) error {
	var body []byte
	var err error
	if request.Body != nil {
		body, err = io.ReadAll(io.LimitReader(request.Body, maxNetworkRequestBytes+1))
		if err != nil {
			return errors.New("failed to read network gateway request")
		}
	}
	if len(body) > maxNetworkRequestBytes {
		return errNetworkPayloadTooLarge
	}

	target := *p.baseURL
	target.Path = strings.TrimRight(p.baseURL.Path, "/") + "/" + strings.TrimLeft(upstreamPath, "/")
	target.RawPath = ""
	target.RawQuery = request.URL.RawQuery
	ctx, cancel := context.WithTimeout(request.Context(), p.timeout)
	defer cancel()
	upstreamRequest, err := http.NewRequestWithContext(ctx, request.Method, target.String(), bytes.NewReader(body))
	if err != nil {
		return errors.New("failed to create network gateway request")
	}
	if contentType := request.Header.Get("Content-Type"); contentType != "" {
		upstreamRequest.Header.Set("Content-Type", contentType)
	}
	if accept := request.Header.Get("Accept"); accept != "" {
		upstreamRequest.Header.Set("Accept", accept)
	}
	if admin {
		upstreamRequest.Header.Set("Authorization", bearerValue(p.token))
		// These headers are generated from the already authenticated My Utils
		// principal. Caller supplied values are never copied to the upstream.
		upstreamRequest.Header.Set("X-RCNet-Actor-ID", actor.ID)
		upstreamRequest.Header.Set("X-RCNet-Actor-Name", actor.Name)
		upstreamRequest.Header.Set("X-RCNet-Source", "web")
	} else {
		if bearer := suppliedBearer(request.Header.Get("Authorization")); bearer != "" {
			upstreamRequest.Header.Set("Authorization", bearer)
		}
		// Agent clients may identify themselves as CLI or MCP. Other caller
		// supplied values, including web, are untrusted and become api.
		upstreamRequest.Header.Set("X-RCNet-Source", networkSource(request.Header.Get("X-RCNet-Source")))
	}

	upstreamResponse, err := p.client.Do(upstreamRequest)
	if err != nil {
		return errors.New("network gateway is unavailable")
	}
	defer upstreamResponse.Body.Close()
	upstreamBody, err := io.ReadAll(io.LimitReader(upstreamResponse.Body, maxNetworkResponseBytes+1))
	if err != nil {
		return errors.New("failed to read network gateway response")
	}
	if len(upstreamBody) > maxNetworkResponseBytes {
		return errors.New("network gateway response exceeds the configured limit")
	}
	if upstreamResponse.StatusCode < http.StatusOK || upstreamResponse.StatusCode >= http.StatusMultipleChoices {
		return networkGatewayError{status: upstreamResponse.StatusCode}
	}
	if contentType := upstreamResponse.Header.Get("Content-Type"); contentType != "" {
		response.Header().Set("Content-Type", contentType)
	}
	if cacheControl := upstreamResponse.Header.Get("Cache-Control"); cacheControl != "" {
		response.Header().Set("Cache-Control", cacheControl)
	}
	response.WriteHeader(upstreamResponse.StatusCode)
	_, _ = response.Write(upstreamBody)
	return nil
}

func suppliedBearer(value string) string {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return "Bearer " + parts[1]
}

func networkSource(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "cli", "mcp":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "api"
	}
}

func bearerValue(token string) string {
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bearer " + token
}

func writeNetworkError(response http.ResponseWriter, status int, message string) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(map[string]string{"error": message})
}
