package wireguard

import (
	"testing"
	"time"
)

func TestValidateExitHealthAcceptsHealthyIndependentExits(t *testing.T) {
	now := time.Date(2026, time.August, 20, 14, 0, 0, 0, time.UTC)
	health := healthyExitHealth(now)
	if err := validateExitHealth(&health, now); err != nil {
		t.Fatalf("validateExitHealth() error = %v", err)
	}
}

func TestValidateExitHealthAcceptsHealthyExitWithoutOptionalLatency(t *testing.T) {
	now := time.Date(2026, time.August, 20, 14, 0, 0, 0, time.UTC)
	health := healthyExitHealth(now)
	primary := health.Exits["primary"]
	primary.LatencyMs = nil
	health.Exits["primary"] = primary
	if err := validateExitHealth(&health, now); err != nil {
		t.Fatalf("validateExitHealth() without optional latency error = %v", err)
	}
}

func TestValidateExitHealthRejectsContradictoryOverallStatus(t *testing.T) {
	now := time.Date(2026, time.August, 20, 14, 0, 0, 0, time.UTC)
	health := healthyExitHealth(now)
	primary := health.Exits["primary"]
	primary.Healthy = false
	reason := "egress_probe_failed"
	primary.Reason = &reason
	primary.ObservedEgressIP = nil
	health.Exits["primary"] = primary
	if err := validateExitHealth(&health, now); err == nil {
		t.Fatal("validateExitHealth() accepted HEALTHY with an unhealthy active exit")
	}
}

func TestRelayStatusRequiresFreshDataPlaneHealth(t *testing.T) {
	now := time.Date(2026, time.August, 20, 14, 0, 0, 0, time.UTC)
	revision := int64(3)
	lastSeen := now.Add(-10 * time.Second)
	relay := Relay{ServerPublicKey: stringPointer("server"), AppliedRevision: &revision, DesiredRevision: revision, LastSeenAt: &lastSeen}
	if got := relayStatus(relay, now); got != "DEGRADED" {
		t.Fatalf("relayStatus() without data-plane health = %q, want DEGRADED", got)
	}
	health := healthyExitHealth(now.Add(-5 * time.Second))
	relay.ExitHealth = &health
	routingHealthy := true
	routingCheckedAt := now.Add(-5 * time.Second)
	relay.RoutingHealthy = &routingHealthy
	relay.RoutingCheckedAt = &routingCheckedAt
	if got := relayStatus(relay, now); got != "READY" {
		t.Fatalf("relayStatus() with fresh healthy exits = %q, want READY", got)
	}
	routingHealthy = false
	if got := relayStatus(relay, now); got != "DEGRADED" {
		t.Fatalf("relayStatus() with broken policy routing = %q, want DEGRADED", got)
	}
	routingHealthy = true
	routingCheckedAt = now.Add(-time.Minute)
	if got := relayStatus(relay, now); got != "DEGRADED" {
		t.Fatalf("relayStatus() with stale policy routing health = %q, want DEGRADED", got)
	}
	routingCheckedAt = now
	health.CheckedAt = now.Add(-time.Minute)
	if got := relayStatus(relay, now); got != "DEGRADED" {
		t.Fatalf("relayStatus() with stale data-plane health = %q, want DEGRADED", got)
	}
	health.CheckedAt = now
	health.OverallStatus = "DOWN"
	if got := relayStatus(relay, now); got != "DOWN" {
		t.Fatalf("relayStatus() with both exits down = %q, want DOWN", got)
	}
}

func healthyExitHealth(checkedAt time.Time) ExitHealth {
	primaryObserved := "91.197.0.191"
	secondaryObserved := "153.76.223.117"
	activeExit := "primary"
	activeInterface := "awg-exit"
	return ExitHealth{
		SchemaVersion:   1,
		CheckedAt:       checkedAt,
		OverallStatus:   "HEALTHY",
		ActiveExit:      &activeExit,
		ActiveInterface: &activeInterface,
		Counters: map[string]ExitHealthCounter{
			"primary": {Successes: 4}, "secondary": {Successes: 4},
		},
		Exits: map[string]ExitProbeHealth{
			"primary": {
				ID: "primary", Interface: "awg-exit", Healthy: true,
				ExpectedEgressIP: "91.197.0.191", ObservedEgressIP: &primaryObserved,
				HandshakeAtEpoch: checkedAt.Unix(), HandshakeAgeSeconds: int64Pointer(0), LatencyMs: floatPointer(25),
			},
			"secondary": {
				ID: "secondary", Interface: "awg-exit-b", Healthy: true,
				ExpectedEgressIP: "153.76.223.117", ObservedEgressIP: &secondaryObserved,
				HandshakeAtEpoch: checkedAt.Unix(), HandshakeAgeSeconds: int64Pointer(0), LatencyMs: floatPointer(35),
			},
		},
	}
}

func stringPointer(value string) *string  { return &value }
func int64Pointer(value int64) *int64     { return &value }
func floatPointer(value float64) *float64 { return &value }
