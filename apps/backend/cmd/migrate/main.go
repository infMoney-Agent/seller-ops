package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"seller-ops/backend/internal/db"
)

func main() {
	appEnv := flag.String("env", os.Getenv("APP_ENV"), "target environment: test | dev | prod")
	envFile := flag.String("env-file", os.Getenv("ENV_FILE"), "path to connection.env (defaults to 43 server path if present)")
	mode := flag.String("mode", os.Getenv("MIGRATE_MODE"), "migration mode: dry-run | apply (prod defaults to dry-run)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cfg, err := db.LoadConfigWithOptions(*appEnv, *envFile)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	openCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	gdb, err := db.OpenMySQL(openCtx, cfg)
	if err != nil {
		log.Fatalf("connect mysql (%s/%s): %v", cfg.AppEnv, cfg.MySQLDatabase, err)
	}
	defer db.CloseMySQL(gdb)

	report, err := db.Migrate(ctx, gdb, db.MigrateOptions{
		AppEnv: cfg.AppEnv,
		Mode:   *mode,
	})
	if report != nil {
		fmt.Printf("env=%s db=%s mode=%s applied=%v\n", report.AppEnv, cfg.MySQLDatabase, report.Mode, report.Applied)
		for _, t := range report.Tables {
			fmt.Printf("  table=%-32s exists=%-5v missing_indexes=%v\n", t.Table, t.Exists, t.MissingIndexes)
		}
	}
	if err != nil {
		log.Fatalf("migrate failed: %v", err)
	}
	fmt.Println("schema migration and index verification succeeded")
}
