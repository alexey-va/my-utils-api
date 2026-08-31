package wireguard

import (
	"encoding/base64"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/alexey-va/my-utils-api/internal/workout"
)

func TestRequiredTextCountsUserVisibleCharacters(t *testing.T) {
	t.Parallel()

	validCyrillic := strings.Repeat("я", 80)
	if got, err := requiredText("  "+validCyrillic+"  ", "Name", 80); err != nil || got != validCyrillic {
		t.Fatalf("requiredText(valid Cyrillic) = %q, %v", got, err)
	}

	for name, value := range map[string]string{
		"empty":        " \t ",
		"newline":      "valid\nsecond line",
		"too many":     strings.Repeat("я", 81),
		"ascii excess": strings.Repeat("a", 81),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			assertBadRequest(t, requiredTextError(value, 80))
		})
	}
}

func TestMetricRangeContracts(t *testing.T) {
	t.Parallel()
	to := time.Date(2026, time.August, 31, 12, 34, 56, 0, time.UTC)
	tests := []struct {
		input      string
		wantName   string
		wantWindow time.Duration
		wantBucket time.Duration
	}{
		{"", "HOUR", time.Hour, time.Minute},
		{"HOUR", "HOUR", time.Hour, time.Minute},
		{"DAY", "DAY", 24 * time.Hour, 15 * time.Minute},
		{"WEEK", "WEEK", 7 * 24 * time.Hour, time.Hour},
		{"MONTH", "MONTH", 30 * 24 * time.Hour, 6 * time.Hour},
	}
	for _, test := range tests {
		t.Run(test.wantName+"_from_"+test.input, func(t *testing.T) {
			t.Parallel()
			name, from, gotTo, bucket, err := metricRange(test.input, to)
			if err != nil || name != test.wantName || !gotTo.Equal(to) || to.Sub(from) != test.wantWindow || bucket != test.wantBucket {
				t.Fatalf("metricRange(%q) = %q, %v..%v, %v, %v", test.input, name, from, gotTo, bucket, err)
			}
		})
	}
	_, _, _, _, err := metricRange("YEAR", to)
	assertBadRequest(t, err)
}

func TestWireGuardInputValidators(t *testing.T) {
	t.Parallel()

	for _, endpoint := range []string{"vpn.example.test:51820", "203.0.113.5:1", "[2001:db8::1]:65535"} {
		if got, err := validateEndpoint(endpoint); err != nil || got != endpoint {
			t.Errorf("validateEndpoint(%q) = %q, %v", endpoint, got, err)
		}
	}
	for _, endpoint := range []string{"vpn.example.test", "vpn example.test:51820", "vpn.example.test:0", "vpn.example.test:65536"} {
		_, err := validateEndpoint(endpoint)
		assertBadRequest(t, err)
	}

	if got, err := validateIPv4(" 203.0.113.5 ", "Target"); err != nil || got != "203.0.113.5" {
		t.Fatalf("validateIPv4 = %q, %v", got, err)
	}
	for _, address := range []string{"2001:db8::1", "999.0.0.1", "host.example"} {
		_, err := validateIPv4(address, "Target")
		assertBadRequest(t, err)
	}

	validKey := base64.StdEncoding.EncodeToString(make([]byte, 32))
	if got, err := validatePublicKey(validKey); err != nil || got != validKey {
		t.Fatalf("validatePublicKey(valid) = %q, %v", got, err)
	}
	for _, key := range []string{"not-base64", base64.StdEncoding.EncodeToString(make([]byte, 31))} {
		_, err := validatePublicKey(key)
		assertBadRequest(t, err)
	}
}

func TestValidateProbeRejectsNonFiniteAndOutOfRangeMeasurements(t *testing.T) {
	t.Parallel()
	validRTT := 12.5
	if err := validateProbe(RouteProbe{Target: "203.0.113.5", PacketLossPercent: 0, AverageRTTMs: &validRTT}); err != nil {
		t.Fatalf("valid probe rejected: %v", err)
	}
	if err := validateProbe(RouteProbe{Target: "203.0.113.5", PacketLossPercent: 100}); err != nil {
		t.Fatalf("boundary probe rejected: %v", err)
	}

	invalidLosses := []float64{-0.1, 100.1, math.NaN(), math.Inf(1)}
	for _, loss := range invalidLosses {
		assertBadRequest(t, validateProbe(RouteProbe{Target: "203.0.113.5", PacketLossPercent: loss}))
	}
	invalidRTTs := []float64{-0.1, 60000.1, math.NaN(), math.Inf(-1)}
	for _, rtt := range invalidRTTs {
		value := rtt
		assertBadRequest(t, validateProbe(RouteProbe{Target: "203.0.113.5", AverageRTTMs: &value}))
	}
	assertBadRequest(t, validateProbe(RouteProbe{Target: "not-an-ip"}))
}

func requiredTextError(value string, max int) error {
	_, err := requiredText(value, "Name", max)
	return err
}

func assertBadRequest(t *testing.T, err error) {
	t.Helper()
	value, ok := err.(*workout.Error)
	if !ok || value.Status != 400 {
		t.Fatalf("error = %#v, want HTTP 400 workout error", err)
	}
}
