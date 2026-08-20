package observability

import (
	"context"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/wireguard"
)

type fakeWireGuardRelaySource struct {
	relays []wireguard.Relay
	err    error
}

func (source fakeWireGuardRelaySource) ListRelays(context.Context) ([]wireguard.Relay, error) {
	return source.relays, source.err
}

func TestWireGuardCollectorExportsPersistedRelayHealth(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)
	primaryLatency := 0.033
	secondaryLatency := 0.048
	directRTT := 18.5
	externalRTT := 33.2
	active := "primary"
	metrics := NewMetrics()
	metrics.RegisterWireGuard(fakeWireGuardRelaySource{relays: []wireguard.Relay{{
		ID: "relay-1", Name: "utils", Status: "READY", LastSeenAt: &now,
		RoutingHealthy: boolPointer(true),
		ExitHealth: &wireguard.ExitHealth{
			ActiveExit: &active,
			Exits: map[string]wireguard.ExitProbeHealth{
				"primary":   {Healthy: true, LatencyMs: floatPointer(primaryLatency * 1000)},
				"secondary": {Healthy: true, LatencyMs: floatPointer(secondaryLatency * 1000)},
			},
		},
		RouteQuality: &wireguard.RouteQuality{
			Direct: wireguard.RouteProbe{PacketLossPercent: 0, AverageRTTMs: &directRTT},
			Veesp:  wireguard.RouteProbe{PacketLossPercent: 2.5, AverageRTTMs: &externalRTT},
		},
	}}})

	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/actuator/prometheus", nil))
	body, _ := io.ReadAll(response.Body)
	text := string(body)
	for _, want := range []string{
		`myutils_wireguard_collection_success 1`,
		`myutils_wireguard_relay_ready{relay="utils",relay_id="relay-1"} 1`,
		`myutils_wireguard_routing_healthy{relay="utils",relay_id="relay-1"} 1`,
		`myutils_wireguard_agent_last_seen_timestamp_seconds{relay="utils",relay_id="relay-1"} 1.7873064e+09`,
		`myutils_wireguard_exit_healthy{exit="primary",relay="utils",relay_id="relay-1"} 1`,
		`myutils_wireguard_exit_selected{exit="secondary",relay="utils",relay_id="relay-1"} 0`,
		`myutils_wireguard_exit_latency_seconds{exit="primary",relay="utils",relay_id="relay-1"} 0.033`,
		`myutils_wireguard_route_packet_loss_percent{path="external",relay="utils",relay_id="relay-1"} 2.5`,
		`myutils_wireguard_route_rtt_seconds{path="internal",relay="utils",relay_id="relay-1"} 0.0185`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("WireGuard metrics missing %q:\n%s", want, text)
		}
	}
}

func TestWireGuardCollectorReportsSourceFailureWithoutPanicking(t *testing.T) {
	t.Parallel()
	metrics := NewMetrics()
	metrics.RegisterWireGuard(fakeWireGuardRelaySource{err: errors.New("database unavailable")})
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/actuator/prometheus", nil))
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), "myutils_wireguard_collection_success 0") {
		t.Fatalf("collection failure gauge missing:\n%s", body)
	}
}

func TestWireGuardCollectorKeepsMissingHeartbeatAlertable(t *testing.T) {
	t.Parallel()
	metrics := NewMetrics()
	metrics.RegisterWireGuard(fakeWireGuardRelaySource{relays: []wireguard.Relay{{
		ID: "relay-1", Name: "utils", Status: "OFFLINE",
	}}})
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest("GET", "/actuator/prometheus", nil))
	body, _ := io.ReadAll(response.Body)
	if !strings.Contains(string(body), `myutils_wireguard_agent_last_seen_timestamp_seconds{relay="utils",relay_id="relay-1"} 0`) {
		t.Fatalf("missing heartbeat must remain a zero-valued alertable series:\n%s", body)
	}
}

func boolPointer(value bool) *bool        { return &value }
func floatPointer(value float64) *float64 { return &value }
