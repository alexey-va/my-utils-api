package observability

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const application = "my-utils-api"

type Metrics struct {
	maxMu             sync.Mutex
	maxValues         map[string]float64
	registry          *prometheus.Registry
	httpRequests      *prometheus.HistogramVec
	agentRequests     *prometheus.CounterVec
	agentTurns        *prometheus.CounterVec
	agentTurnDuration *prometheus.SummaryVec
	agentTurnMax      *prometheus.GaugeVec
	agentLLMSteps     *prometheus.CounterVec
	agentLLMDuration  *prometheus.SummaryVec
	agentLLMMax       *prometheus.GaugeVec
	agentToolCalls    *prometheus.CounterVec
	agentToolDuration *prometheus.SummaryVec
	agentToolMax      *prometheus.GaugeVec
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		maxValues:         map[string]float64{},
		registry:          prometheus.NewRegistry(),
		httpRequests:      prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "http_server_requests_seconds", Help: "HTTP server request duration.", ConstLabels: prometheus.Labels{"application": application}, Buckets: prometheus.DefBuckets}, []string{"method", "uri", "status"}),
		agentRequests:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "agent_requests_total", Help: "Inbound agent requests.", ConstLabels: prometheus.Labels{"application": application}}, []string{"path", "outcome"}),
		agentTurns:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "agent_turns_total", Help: "Completed agent turns.", ConstLabels: prometheus.Labels{"application": application}}, []string{"path", "outcome"}),
		agentTurnDuration: prometheus.NewSummaryVec(prometheus.SummaryOpts{Name: "agent_turn_duration_seconds", Help: "Agent turn duration.", ConstLabels: prometheus.Labels{"application": application}}, []string{"path"}),
		agentTurnMax:      prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "agent_turn_duration_seconds_max", Help: "Largest observed agent turn duration.", ConstLabels: prometheus.Labels{"application": application}}, []string{"path"}),
		agentLLMSteps:     prometheus.NewCounterVec(prometheus.CounterOpts{Name: "agent_llm_steps_total", Help: "Agent LLM calls.", ConstLabels: prometheus.Labels{"application": application}}, []string{"path"}),
		agentLLMDuration:  prometheus.NewSummaryVec(prometheus.SummaryOpts{Name: "agent_llm_step_duration_seconds", Help: "LLM step duration.", ConstLabels: prometheus.Labels{"application": application}}, []string{"path"}),
		agentLLMMax:       prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "agent_llm_step_duration_seconds_max", Help: "Largest observed LLM step duration.", ConstLabels: prometheus.Labels{"application": application}}, []string{"path"}),
		agentToolCalls:    prometheus.NewCounterVec(prometheus.CounterOpts{Name: "agent_tool_calls_total", Help: "Agent tool calls.", ConstLabels: prometheus.Labels{"application": application}}, []string{"tool", "status", "path"}),
		agentToolDuration: prometheus.NewSummaryVec(prometheus.SummaryOpts{Name: "agent_tool_duration_seconds", Help: "Agent tool duration.", ConstLabels: prometheus.Labels{"application": application}}, []string{"tool", "path"}),
		agentToolMax:      prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "agent_tool_duration_seconds_max", Help: "Largest observed tool duration.", ConstLabels: prometheus.Labels{"application": application}}, []string{"tool", "path"}),
	}
	metrics.registry.MustRegister(
		collectors.NewGoCollector(), collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		metrics.httpRequests, metrics.agentRequests, metrics.agentTurns, metrics.agentTurnDuration, metrics.agentTurnMax,
		metrics.agentLLMSteps, metrics.agentLLMDuration, metrics.agentLLMMax,
		metrics.agentToolCalls, metrics.agentToolDuration, metrics.agentToolMax,
	)
	// Keep dashboard series present even before the first Telegram request.
	for _, path := range []string{"direct", "temporal", "none"} {
		metrics.agentRequests.WithLabelValues(path, "received").Add(0)
		metrics.agentRequests.WithLabelValues(path, "rejected").Add(0)
	}
	for _, path := range []string{"direct", "temporal"} {
		for _, outcome := range []string{"reply", "tool_limit", "start_command", "prelude_reply", "rejected"} {
			metrics.agentTurns.WithLabelValues(path, outcome).Add(0)
		}
		metrics.agentLLMSteps.WithLabelValues(path).Add(0)
	}
	return metrics
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) RegisterWireGuard(source WireGuardRelaySource) {
	m.registry.MustRegister(newWireGuardCollector(source))
}

func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		start := time.Now()
		writer := &statusWriter{ResponseWriter: response, status: http.StatusOK}
		next.ServeHTTP(writer, request)
		uri := request.URL.Path
		if route := chi.RouteContext(request.Context()).RoutePattern(); route != "" {
			uri = route
		}
		m.httpRequests.WithLabelValues(request.Method, boundedURI(uri), strconv.Itoa(writer.status)).Observe(time.Since(start).Seconds())
	})
}

func (m *Metrics) RecordAgentRequest(path, outcome string) {
	m.agentRequests.WithLabelValues(path, outcome).Inc()
}
func (m *Metrics) RecordAgentTurn(path, outcome string, elapsed time.Duration) {
	seconds := elapsed.Seconds()
	m.agentTurns.WithLabelValues(path, outcome).Inc()
	m.agentTurnDuration.WithLabelValues(path).Observe(seconds)
	m.setMax("turn\x00"+path, m.agentTurnMax.WithLabelValues(path), seconds)
}
func (m *Metrics) RecordLLMStep(path string, elapsed time.Duration) {
	seconds := elapsed.Seconds()
	m.agentLLMSteps.WithLabelValues(path).Inc()
	m.agentLLMDuration.WithLabelValues(path).Observe(seconds)
	m.setMax("llm\x00"+path, m.agentLLMMax.WithLabelValues(path), seconds)
}
func (m *Metrics) RecordTool(path, tool, status string, elapsed time.Duration) {
	seconds := elapsed.Seconds()
	m.agentToolCalls.WithLabelValues(tool, status, path).Inc()
	m.agentToolDuration.WithLabelValues(tool, path).Observe(seconds)
	m.setMax("tool\x00"+tool+"\x00"+path, m.agentToolMax.WithLabelValues(tool, path), seconds)
}

func (m *Metrics) setMax(key string, gauge prometheus.Gauge, value float64) {
	m.maxMu.Lock()
	defer m.maxMu.Unlock()
	if current, ok := m.maxValues[key]; ok && value <= current {
		return
	}
	m.maxValues[key] = value
	gauge.Set(value)
}

type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func boundedURI(uri string) string {
	if len(uri) > 160 || strings.TrimSpace(uri) == "" {
		return "UNKNOWN"
	}
	return uri
}
