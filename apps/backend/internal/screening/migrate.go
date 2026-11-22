package screening

import (
	"context"
	"embed"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"strings"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Transactional migrations are serialized across deploy processes. Existing
// Alembic revisions are adopted without replaying their data transformations.
func migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, e := pool.Acquire(ctx)
	if e != nil {
		return e
	}
	defer conn.Release()
	if _, e = conn.Exec(ctx, "SELECT pg_advisory_lock(741938201)"); e != nil {
		return e
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock(741938201)")
	_, e = conn.Exec(ctx, "CREATE TABLE IF NOT EXISTS screening_schema_versions(version integer PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())")
	if e != nil {
		return e
	}
	var current int
	if e = conn.QueryRow(ctx, "SELECT COALESCE(max(version),0) FROM screening_schema_versions").Scan(&current); e != nil {
		return e
	}
	if current == 0 {
		var exists bool
		if e = conn.QueryRow(ctx, "SELECT to_regclass('alembic_version') IS NOT NULL").Scan(&exists); e != nil {
			return e
		}
		if exists {
			var revision string
			if e = conn.QueryRow(ctx, "SELECT version_num FROM alembic_version").Scan(&revision); e != nil {
				return e
			}
			if _, e = fmt.Sscanf(revision, "screening_%04d", &current); e != nil || current < 1 || current > 4 {
				return fmt.Errorf("unsupported screening migration revision %q", revision)
			}
			if _, e = conn.Exec(ctx, "INSERT INTO screening_schema_versions(version) VALUES($1)", current); e != nil {
				return e
			}
		}
	}
	files := []string{"001_initial.sql", "002_groups.sql", "003_monthly.sql", "004_compact.sql", "005_go_jobs.sql", "006_self_parent.sql", "007_import_columns.sql", "008_child_sales.sql", "009_import_removal.sql", "010_sales_display_index.sql"}
	if current > len(files) {
		return fmt.Errorf("database schema is newer than this application")
	}
	for i, name := range files {
		version := i + 1
		if version <= current {
			continue
		}
		data, e := migrationFiles.ReadFile("migrations/" + name)
		if e != nil {
			return e
		}
		tx, e := conn.Begin(ctx)
		if e != nil {
			return e
		}
		// Each asset contains plain DDL/DML, without procedural bodies.
		for _, statement := range strings.Split(string(data), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, e = tx.Exec(ctx, statement); e != nil {
				break
			}
		}
		if e == nil && version == 7 {
			e = backfillImportedColumns(ctx, tx)
		}
		if e == nil {
			_, e = tx.Exec(ctx, "INSERT INTO screening_schema_versions(version) VALUES($1)", version)
		}
		if e != nil {
			tx.Rollback(ctx)
			return fmt.Errorf("migration %s: %w", name, e)
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	return nil
}
