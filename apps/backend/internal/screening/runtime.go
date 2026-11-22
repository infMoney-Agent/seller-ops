package screening

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

// Runtime shares the business host's listener and cancellation context.
type Runtime struct{ service *service }

func NewRuntime(ctx context.Context) (*Runtime, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if len(cfg.Secret) < 32 {
		return nil, fmt.Errorf("SCREENING_INTERNAL_SECRET must contain at least 32 bytes")
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if err = pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("screening database unavailable: %w", err)
	}
	var version int
	if err = pool.QueryRow(ctx, "SELECT COALESCE(max(version),0) FROM screening_schema_versions").Scan(&version); err != nil || version != 10 {
		pool.Close()
		return nil, fmt.Errorf("screening schema is not current; run seller-backend migrate before startup")
	}
	return &Runtime{&service{pool, cfg}}, nil
}
func (r *Runtime) Handler() http.Handler            { return r.service.handler() }
func (r *Runtime) Worker(ctx context.Context) error { return r.service.worker(ctx) }
func (r *Runtime) Health(ctx context.Context) error { return r.service.db.Ping(ctx) }
func (r *Runtime) Close() error                     { r.service.db.Close(); return nil }
func MigrateDatabase(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	return migrate(ctx, pool)
}
