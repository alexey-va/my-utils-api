package startup

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type Warmer interface {
	Name() string
	Warm(context.Context) error
}

type ServeFunc func(context.Context) error

func Run(ctx context.Context, warmers []Warmer, serve ServeFunc) error {
	started := time.Now()
	for _, warmer := range warmers {
		warmStarted := time.Now()
		if err := warmer.Warm(ctx); err != nil {
			return fmt.Errorf("startup warmup %s: %w", warmer.Name(), err)
		}
		slog.InfoContext(ctx, "startup warmup completed", "component", warmer.Name(), "elapsed_ms", time.Since(warmStarted).Milliseconds())
	}
	slog.InfoContext(ctx, "startup barrier completed", "elapsed_ms", time.Since(started).Milliseconds())
	return serve(ctx)
}
