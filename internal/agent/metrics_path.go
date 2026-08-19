package agent

import (
	"context"
	"strings"
)

type metricsPathContextKey struct{}

// WithMetricsPath tags a turn and every tool it invokes with its execution path.
func WithMetricsPath(ctx context.Context, path string) context.Context {
	path = strings.TrimSpace(path)
	if path == "" {
		return ctx
	}
	return context.WithValue(ctx, metricsPathContextKey{}, path)
}

func metricsPath(ctx context.Context, sandbox bool) string {
	if sandbox {
		return "sandbox"
	}
	if path, ok := ctx.Value(metricsPathContextKey{}).(string); ok && strings.TrimSpace(path) != "" {
		return path
	}
	return "direct"
}
