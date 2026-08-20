package migrate

import (
	"context"
	"io/fs"
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
	if successful != 30 {
		t.Errorf("successful migration rows = %d, want 30", successful)
	}
	for _, table := range []string{"users", "workout_entries", "app_settings", "agent_test_sandbox_states", "wireguard_peer_metric_samples", "wireguard_exit_health_samples"} {
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
	var persistedRateColumns int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'wireguard_peers'
		  AND column_name IN (
		    'current_download_bytes_per_second', 'current_upload_bytes_per_second',
		    'raw_ru_download_bytes', 'raw_ru_upload_bytes',
		    'raw_non_ru_download_bytes', 'raw_non_ru_upload_bytes'
		  )
	`).Scan(&persistedRateColumns); err != nil || persistedRateColumns != 6 {
		t.Errorf("V27 persisted rate and route counter columns = %d, error = %v", persistedRateColumns, err)
	}
	var runtimeHealthColumns int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'wireguard_relays'
		  AND column_name IN ('routing_healthy', 'routing_checked_at', 'exit_health')
	`).Scan(&runtimeHealthColumns); err != nil || runtimeHealthColumns != 3 {
		t.Errorf("V28 runtime health columns = %d, error = %v", runtimeHealthColumns, err)
	}
	var exitPreferenceColumn bool
	if err := pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = 'wireguard_relays'
			  AND column_name = 'exit_preference'
		)`).Scan(&exitPreferenceColumn); err != nil || !exitPreferenceColumn {
		t.Errorf("V30 exit preference column exists = %v, error = %v", exitPreferenceColumn, err)
	}
}

func TestV27ConvertsHistoricalRouteCountersToDeltas(t *testing.T) {
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

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin() error = %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(ctx, `CREATE TEMP TABLE wireguard_peers (id UUID PRIMARY KEY)`); err != nil {
		t.Fatalf("create peers fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE wireguard_peer_metric_samples (
			id UUID PRIMARY KEY,
			peer_id UUID NOT NULL,
			recorded_at TIMESTAMPTZ NOT NULL,
			ru_download_bytes BIGINT NOT NULL,
			ru_upload_bytes BIGINT NOT NULL,
			non_ru_download_bytes BIGINT NOT NULL,
			non_ru_upload_bytes BIGINT NOT NULL
		)
	`); err != nil {
		t.Fatalf("create samples fixture: %v", err)
	}
	const peerID = "00000000-0000-0000-0000-000000000001"
	if _, err := tx.Exec(ctx, `INSERT INTO wireguard_peers(id) VALUES($1::uuid)`, peerID); err != nil {
		t.Fatalf("insert peer fixture: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO wireguard_peer_metric_samples(id,peer_id,recorded_at,ru_download_bytes,ru_upload_bytes,non_ru_download_bytes,non_ru_upload_bytes) VALUES
		('00000000-0000-0000-0000-000000000011',$1::uuid,'2026-08-20T12:00:00Z',80,30,120,70),
		('00000000-0000-0000-0000-000000000012',$1::uuid,'2026-08-20T12:00:10Z',100,50,160,100),
		('00000000-0000-0000-0000-000000000013',$1::uuid,'2026-08-20T12:00:20Z',5,2,15,8)
	`, peerID); err != nil {
		t.Fatalf("insert samples fixture: %v", err)
	}

	source, err := fs.ReadFile(migrations.FS, "V27__wireguard_persisted_peer_rates.sql")
	if err != nil {
		t.Fatalf("read V27: %v", err)
	}
	if _, err := tx.Exec(ctx, string(source)); err != nil {
		t.Fatalf("apply V27: %v", err)
	}

	var rawRUDownload, rawRUUpload, rawNonRUDownload, rawNonRUUpload int64
	if err := tx.QueryRow(ctx, `SELECT raw_ru_download_bytes,raw_ru_upload_bytes,raw_non_ru_download_bytes,raw_non_ru_upload_bytes FROM wireguard_peers WHERE id=$1::uuid`, peerID).Scan(&rawRUDownload, &rawRUUpload, &rawNonRUDownload, &rawNonRUUpload); err != nil {
		t.Fatalf("read persisted raw counters: %v", err)
	}
	if rawRUDownload != 5 || rawRUUpload != 2 || rawNonRUDownload != 15 || rawNonRUUpload != 8 {
		t.Fatalf("persisted raw counters = %d/%d/%d/%d", rawRUDownload, rawRUUpload, rawNonRUDownload, rawNonRUUpload)
	}

	rows, err := tx.Query(ctx, `SELECT ru_download_bytes,ru_upload_bytes,non_ru_download_bytes,non_ru_upload_bytes FROM wireguard_peer_metric_samples ORDER BY recorded_at`)
	if err != nil {
		t.Fatalf("read repaired samples: %v", err)
	}
	defer rows.Close()
	want := [][4]int64{{0, 0, 0, 0}, {20, 20, 40, 30}, {5, 2, 15, 8}}
	index := 0
	for rows.Next() {
		var got [4]int64
		if err := rows.Scan(&got[0], &got[1], &got[2], &got[3]); err != nil {
			t.Fatalf("scan repaired sample: %v", err)
		}
		if got != want[index] {
			t.Errorf("repaired sample[%d] = %v, want %v", index, got, want[index])
		}
		index++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate repaired samples: %v", err)
	}
	if index != len(want) {
		t.Fatalf("repaired sample count = %d, want %d", index, len(want))
	}
}
