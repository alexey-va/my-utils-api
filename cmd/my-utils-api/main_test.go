package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexey-va/my-utils-api/internal/config"
)

func TestHealthcheckRequiresSuccessfulAPIResponse(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()
	if err := healthcheck(context.Background(), server.URL, server.Client()); err != nil {
		t.Fatal(err)
	}
	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) })
	if err := healthcheck(context.Background(), server.URL, server.Client()); err == nil {
		t.Fatal("expected unhealthy status error")
	}
}

func TestSharedOutboundProxyMatchesJVMTelegramContract(t *testing.T) {
	t.Parallel()
	proxy, raw := configuredOutboundProxy(config.HTTPProxy{Enabled: true, Host: "2001:db8::1", Port: 8888})
	if proxy == nil || proxy.String() != "http://[2001:db8::1]:8888" {
		t.Fatalf("Telegram proxy = %v", proxy)
	}
	if raw != proxy.String() {
		t.Fatalf("OpenRouter proxy = %q, Telegram proxy = %q", raw, proxy.String())
	}

	proxy, raw = configuredOutboundProxy(config.HTTPProxy{})
	if proxy != nil || raw != "" {
		t.Fatalf("disabled proxy = %v / %q", proxy, raw)
	}
}
