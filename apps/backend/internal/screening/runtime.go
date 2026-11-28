package screening

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"seller-ops/backend/internal/db"
	"seller-ops/backend/internal/storage/gormrepo"
)

// Runtime shares the business host's listener and cancellation context.
type Runtime struct{ service *service }

// NewGORMRuntime 使用 GORM MySQL 仓储层初始化初筛运行时（遵循零手写 SQL 规范，启动时仅校验表结构不隐式 AutoMigrate）。
func NewGORMRuntime(ctx context.Context) (*Runtime, error) {
	secret := os.Getenv("SCREENING_INTERNAL_SECRET")
	if len(secret) < 32 {
		return nil, fmt.Errorf("SCREENING_INTERNAL_SECRET must contain at least 32 bytes")
	}
	storageDir := os.Getenv("SCREENING_STORAGE_DIR")
	if storageDir == "" {
		storageDir = "/screening-data"
	}
	dbCfg, err := db.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("load mysql config: %w", err)
	}
	gdb, err := db.OpenMySQL(ctx, dbCfg)
	if err != nil {
		return nil, fmt.Errorf("screening mysql unavailable: %w", err)
	}
	if _, err := db.VerifySchema(ctx, gdb); err != nil {
		_ = db.CloseMySQL(gdb)
		return nil, fmt.Errorf("screening mysql schema is not current; run cmd/migrate before startup: %w", err)
	}
	return &Runtime{
		service: &service{
			gormStore: gormrepo.NewStore(gdb),
			cfg:       Config{Secret: secret, Storage: storageDir},
		},
	}, nil
}

// NewRuntime 统一使用 GORM MySQL 初始化初筛业务运行时，不再依赖 PostgreSQL。
func NewRuntime(ctx context.Context) (*Runtime, error) {
	return NewGORMRuntime(ctx)
}

func (r *Runtime) Handler() http.Handler            { return r.service.handler() }
func (r *Runtime) Worker(ctx context.Context) error { return r.service.gormWorker(ctx) }
func (r *Runtime) Health(ctx context.Context) error { return r.service.ping(ctx) }
func (r *Runtime) Close() error {
	if r.service.gormStore != nil {
		return db.CloseMySQL(r.service.gormStore.DB())
	}
	return nil
}

// MigrateDatabase 统一使用 MySQL GORM 受控迁移，不再依赖 PostgreSQL。
func MigrateDatabase(ctx context.Context) error {
	dbCfg, err := db.LoadConfig()
	if err != nil {
		return err
	}
	gdb, err := db.OpenMySQL(ctx, dbCfg)
	if err != nil {
		return err
	}
	defer db.CloseMySQL(gdb)
	_, err = db.Migrate(ctx, gdb, db.MigrateOptions{
		AppEnv: dbCfg.AppEnv,
		Mode:   os.Getenv("MIGRATE_MODE"),
	})
	return err
}
