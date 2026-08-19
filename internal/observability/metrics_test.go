package observability

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMetricsExposeGoProcessAndHTTPSeries(t *testing.T) {
	t.Parallel()
	metrics := NewMetrics()
	handler := metrics.HTTPMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/health", nil))
	response := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/actuator/prometheus", nil))
	body, _ := io.ReadAll(response.Body)
	for _, name := range []string{"go_memstats_heap_alloc_bytes", "process_resident_memory_bytes", "http_server_requests_seconds_count"} {
		if !strings.Contains(string(body), name) {
			t.Fatalf("metrics missing %s", name)
		}
	}
	for _, labelSet := range []string{
		`agent_requests_total{application="my-utils-api",outcome="received",path="temporal"} 0`,
		`agent_requests_total{application="my-utils-api",outcome="rejected",path="none"} 0`,
		`agent_turns_total{application="my-utils-api",outcome="start_command",path="temporal"} 0`,
		`agent_llm_steps_total{application="my-utils-api",path="temporal"} 0`,
	} {
		if !strings.Contains(string(body), labelSet) {
			t.Fatalf("metrics missing eager series %s", labelSet)
		}
	}
}

func TestMaximumMetricsNeverDecrease(t *testing.T) {
	t.Parallel()
	metrics := NewMetrics()
	metrics.RecordAgentTurn("direct", "reply", 5*time.Second)
	metrics.RecordAgentTurn("direct", "reply", time.Second)
	metrics.RecordLLMStep("direct", 4*time.Second)
	metrics.RecordLLMStep("direct", 2*time.Second)
	metrics.RecordTool("direct", "list_exercises", "ok", 3*time.Second)
	metrics.RecordTool("direct", "list_exercises", "ok", time.Second)

	families, err := metrics.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]float64{
		"agent_turn_duration_seconds_max":     5,
		"agent_llm_step_duration_seconds_max": 4,
		"agent_tool_duration_seconds_max":     3,
	}
	for _, family := range families {
		value, ok := want[family.GetName()]
		if !ok || len(family.Metric) == 0 {
			continue
		}
		if got := family.Metric[0].GetGauge().GetValue(); got != value {
			t.Fatalf("%s = %v, want %v", family.GetName(), got, value)
		}
		delete(want, family.GetName())
	}
	if len(want) != 0 {
		t.Fatalf("metrics not found: %v", want)
	}
}
