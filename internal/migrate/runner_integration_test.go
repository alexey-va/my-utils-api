package migrate

import (
	"context"
	"os"
	"testing"

	migrations "github.com/alexey-va/my-utils-api/src/main/resources/db/migration"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunnerAppliesFlywaySchemaAndIsIdempotent(t *testing.T) {
	databaseURL := os.Getenv("TEST_POSTGRES_URL")
	if databaseURL == "" {
		t.Skip("TEST_POSTGRES_URL is not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("pgxpool.New() error = %v", err)
	}
	t.Cleanup(pool.Close)

	runner, err := NewRunner(pool, migrations.FS)
	if err != nil {
		t.Fatalf("NewRunner() error = %v", err)
	}
	if err := runner.Warm(ctx); err != nil {
		t.Fatalf("first Warm() error = %v", err)
	}
	if err := runner.Warm(ctx); err != nil {
		t.Fatalf("second Warm() error = %v", err)
	}

	var successful int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM flyway_schema_history WHERE success`).Scan(&successful); err != nil {
		t.Fatalf("count schema history: %v", err)
	}
	if successful != 26 {
		t.Errorf("successful migration rows = %d, want 26", successful)
	}
	for _, table := range []string{"users", "workout_entries", "app_settings", "agent_test_sandbox_states", "wireguard_peer_metric_samples"} {
		var exists bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.' || $1) IS NOT NULL`, table).Scan(&exists); err != nil || !exists {
			t.Errorf("table %s exists = %v, error = %v", table, exists, err)
		}
	}
	var routeColumn bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'wireguard_relays'
			  AND column_name = 'route_quality_updated_at'
		)`).Scan(&routeColumn); err != nil || !routeColumn {
		t.Errorf("V26 column exists = %v, error = %v", routeColumn, err)
	}
}
