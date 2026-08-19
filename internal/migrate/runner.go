package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const advisoryLockID int64 = 0x4d595554494c53 // "MYUTILS"

type Runner struct {
	pool       *pgxpool.Pool
	migrations []Migration
}

func NewRunner(pool *pgxpool.Pool, source fs.FS) (*Runner, error) {
	if pool == nil {
		return nil, fmt.Errorf("migration pool is nil")
	}
	migrations, err := Discover(source)
	if err != nil {
		return nil, err
	}
	if len(migrations) == 0 {
		return nil, fmt.Errorf("no SQL migrations embedded")
	}
	return &Runner{pool: pool, migrations: migrations}, nil
}

func (r *Runner) Name() string { return "database-migrations" }

func (r *Runner) Warm(ctx context.Context) error {
	connection, err := r.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire PostgreSQL connection: %w", err)
	}
	defer connection.Release()
	if err := connection.Ping(ctx); err != nil {
		return fmt.Errorf("ping PostgreSQL: %w", err)
	}
	if _, err := connection.Exec(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockID); err != nil {
		return fmt.Errorf("lock migrations: %w", err)
	}
	defer func() {
		_, _ = connection.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, advisoryLockID)
	}()

	if err := ensureHistoryTable(ctx, connection); err != nil {
		return err
	}
	applied, err := loadApplied(ctx, connection)
	if err != nil {
		return err
	}
	for _, migration := range r.migrations {
		if existing, ok := applied[strconv.Itoa(migration.Version)]; ok {
			if !existing.Success {
				return fmt.Errorf("migration %s previously failed", migration.Script)
			}
			if existing.Checksum == nil || *existing.Checksum != migration.Checksum {
				return fmt.Errorf("migration %s checksum mismatch: database=%v embedded=%d", migration.Script, existing.Checksum, migration.Checksum)
			}
			continue
		}
		if err := apply(ctx, connection, migration); err != nil {
			return err
		}
	}
	return nil
}

func ensureHistoryTable(ctx context.Context, connection *pgxpool.Conn) error {
	_, err := connection.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS flyway_schema_history (
			installed_rank INTEGER NOT NULL PRIMARY KEY,
			version VARCHAR(50),
			description VARCHAR(200) NOT NULL,
			type VARCHAR(20) NOT NULL,
			script VARCHAR(1000) NOT NULL,
			checksum INTEGER,
			installed_by VARCHAR(100) NOT NULL,
			installed_on TIMESTAMP NOT NULL DEFAULT now(),
			execution_time INTEGER NOT NULL,
			success BOOLEAN NOT NULL
		);
		CREATE INDEX IF NOT EXISTS flyway_schema_history_s_idx ON flyway_schema_history (success);
	`, pgx.QueryExecModeSimpleProtocol)
	if err != nil {
		return fmt.Errorf("ensure Flyway schema history: %w", err)
	}
	return nil
}

type appliedMigration struct {
	Checksum *int32
	Success  bool
}

func loadApplied(ctx context.Context, connection *pgxpool.Conn) (map[string]appliedMigration, error) {
	rows, err := connection.Query(ctx, `SELECT version, checksum, success FROM flyway_schema_history WHERE version IS NOT NULL`)
	if err != nil {
		return nil, fmt.Errorf("read Flyway schema history: %w", err)
	}
	defer rows.Close()
	result := make(map[string]appliedMigration)
	for rows.Next() {
		var version string
		var checksum *int32
		var success bool
		if err := rows.Scan(&version, &checksum, &success); err != nil {
			return nil, fmt.Errorf("scan Flyway schema history: %w", err)
		}
		result[version] = appliedMigration{Checksum: checksum, Success: success}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate Flyway schema history: %w", err)
	}
	return result, nil
}

func apply(ctx context.Context, connection *pgxpool.Conn, migration Migration) error {
	transaction, err := connection.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", migration.Script, err)
	}
	defer func() { _ = transaction.Rollback(context.WithoutCancel(ctx)) }()
	started := time.Now()
	if _, err := transaction.Exec(ctx, string(migration.SQL), pgx.QueryExecModeSimpleProtocol); err != nil {
		return fmt.Errorf("apply migration %s: %w", migration.Script, err)
	}
	var rank int
	if err := transaction.QueryRow(ctx, `SELECT COALESCE(MAX(installed_rank), 0) + 1 FROM flyway_schema_history`).Scan(&rank); err != nil {
		return fmt.Errorf("allocate rank for migration %s: %w", migration.Script, err)
	}
	executionMillis := int(time.Since(started).Milliseconds())
	if _, err := transaction.Exec(ctx, `
		INSERT INTO flyway_schema_history (
			installed_rank, version, description, type, script, checksum,
			installed_by, execution_time, success
		) VALUES ($1, $2, $3, 'SQL', $4, $5, current_user, $6, true)
	`, rank, strconv.Itoa(migration.Version), migration.Description, migration.Script, migration.Checksum, executionMillis); err != nil {
		return fmt.Errorf("record migration %s: %w", migration.Script, err)
	}
	if err := transaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", migration.Script, err)
	}
	return nil
}
