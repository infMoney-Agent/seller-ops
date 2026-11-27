package screening

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"os"
	"seller-ops/backend/internal/db"
	"seller-ops/backend/internal/storage/gormrepo"
	"strings"
)

// Runtime shares the business host's listener and cancellation context.
type Runtime struct{ service *service }

func useGORMStorage() bool {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("SCREENING_STORAGE_BACKEND")))
	if backend == "mysql" || backend == "gorm" {
		return true
	}
	if backend == "postgres" || backend == "pgx" {
		return false
	}
	// 当未配置 PostgreSQL SCREENING_DATABASE_URL 且存在 MySQL/GORM 环境配置时，自动启用 GORM MySQL。
	if strings.TrimSpace(os.Getenv("SCREENING_DATABASE_URL")) == "" &&
		(strings.TrimSpace(os.Getenv("MYSQL_DSN")) != "" || strings.TrimSpace(os.Getenv("ENV_FILE")) != "") {
		return true
	}
	return false
}

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

func NewRuntime(ctx context.Context) (*Runtime, error) {
	if useGORMStorage() {
		return NewGORMRuntime(ctx)
	}
	cfg, err := loadConfig()
	if err != nil {
		return nil, err
	}
	if len(cfg.Secret) < 32 {
		return nil, fmt.Errorf("SCREENING_INTERNAL_SECRET must contain at least 32 bytes")
	}
	poolConfig, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	// Interactive catalog queries spend more time compiling complex expressions
	// than executing them once JIT cost thresholds are crossed. Apply this to
	// every screening connection, including replacements after reconnects.
	poolConfig.ConnConfig.RuntimeParams["jit"] = "off"
	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
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
	return &Runtime{&service{db: pool, cfg: cfg}}, nil
}
func (r *Runtime) Handler() http.Handler { return r.service.handler() }
func (r *Runtime) Worker(ctx context.Context) error {
	if r.service.gormStore != nil {
		return r.service.gormWorker(ctx)
	}
	return r.service.worker(ctx)
}
func (r *Runtime) Health(ctx context.Context) error { return r.service.ping(ctx) }
func (r *Runtime) Close() error {
	if r.service.gormStore != nil {
		return db.CloseMySQL(r.service.gormStore.DB())
	}
	if r.service.db != nil {
		r.service.db.Close()
	}
	return nil
}
func MigrateDatabase(ctx context.Context) error {
	if useGORMStorage() {
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
