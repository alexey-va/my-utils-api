package httpapi

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"

	"github.com/alexey-va/my-utils-api/internal/clientevents"
)

func (a *API) clientEvents(response http.ResponseWriter, request *http.Request) {
	body, _ := io.ReadAll(io.LimitReader(request.Body, 64*1024+1))
	ipAddress := clientIPAddress(request)
	clientevents.LogBatch(request.Context(), slog.Default(), body, clientevents.RequestContext{
		Origin: pointerHeader(request, "Origin"), IPAddress: pointerString(ipAddress), UserAgent: pointerHeader(request, "User-Agent"),
		AcceptLanguage: pointerHeader(request, "Accept-Language"), SecCHUA: pointerHeader(request, "Sec-CH-UA"),
		SecCHUAPlatform: pointerHeader(request, "Sec-CH-UA-Platform"), SecCHUAMobile: pointerHeader(request, "Sec-CH-UA-Mobile"),
	})
	response.WriteHeader(http.StatusNoContent)
}

func clientIPAddress(request *http.Request) string {
	if forwarded := strings.TrimSpace(request.Header.Get("X-Real-IP")); forwarded != "" {
		return forwarded
	}
	remote := strings.TrimSpace(request.RemoteAddr)
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return strings.Trim(remote, "[]")
}

func pointerHeader(request *http.Request, name string) *string {
	return pointerString(request.Header.Get(name))
}
func pointerString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return &value
}
