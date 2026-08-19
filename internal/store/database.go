package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	configuration, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse PostgreSQL configuration: %w", err)
	}
	configuration.MaxConns = 10
	configuration.MinConns = 2
	configuration.MaxConnIdleTime = 5 * time.Minute
	configuration.MaxConnLifetime = 9 * time.Minute
	configuration.HealthCheckPeriod = time.Minute
	configuration.ConnConfig.ConnectTimeout = 30 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		return nil, fmt.Errorf("create PostgreSQL pool: %w", err)
	}
	return pool, nil
}
