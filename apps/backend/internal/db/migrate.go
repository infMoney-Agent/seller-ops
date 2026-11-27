package db

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"
	"seller-ops/backend/internal/model"
)

const (
	MigrateModeDryRun = "dry-run"
	MigrateModeApply  = "apply"
)

// MigrateOptions 控制受控迁移行为。
type MigrateOptions struct {
	AppEnv string
	Mode   string
}

// TableCheck 记录单张表与关键索引的校验结果。
type TableCheck struct {
	Table          string
	Exists         bool
	MissingIndexes []string
}

// MigrateReport 汇总迁移与校验结果。
type MigrateReport struct {
	AppEnv  string
	Mode    string
	Applied bool
	Tables  []TableCheck
}

type requiredIndexes struct {
	model   any
	table   string
	indexes []string
}

func expectedSchema() []requiredIndexes {
	return []requiredIndexes{
		{&model.Workspace{}, "workspaces", []string{"uk_workspace_code"}},
		{&model.WorkspaceMember{}, "workspace_members", []string{"uk_ws_user", "idx_ws_member_ws", "idx_ws_member_user"}},
		{&model.ImportTask{}, "import_tasks", []string{"uk_import_task_no", "idx_import_task_ws_status"}},
		{&model.ImportRecord{}, "import_records", []string{"uk_task_row", "idx_import_rec_ws_task"}},
		{&model.ExportTask{}, "export_tasks", []string{"uk_export_task_no", "idx_export_task_ws"}},
		{&model.ScreeningItem{}, "screening_items", []string{"uk_ws_asin", "idx_screen_ws_parent", "idx_screen_ws_status"}},
		{&model.ScreeningItemMonthlyMetric{}, "screening_item_monthly_metrics", []string{"uk_ws_asin_month", "idx_metric_ws_month", "idx_metric_item"}},
		{&model.ScreeningReviewLog{}, "screening_review_logs", []string{"idx_review_log_ws_item"}},
		{&model.ProfitCalculation{}, "profit_calculations", []string{"idx_profit_ws_item"}},
		{&model.ScreeningConfig{}, "screening_configs", []string{"uk_cfg_ws_kind_ver", "idx_cfg_ws_kind_active"}},
		{&model.ScreeningJob{}, "screening_jobs", []string{"idx_sjob_ws_kind_state"}},
	}
}

// Migrate 执行受控表结构迁移与索引校验（全程使用 GORM Migrator，零手写 SQL）。
func Migrate(ctx context.Context, gdb *gorm.DB, opts MigrateOptions) (*MigrateReport, error) {
	mode := strings.ToLower(strings.TrimSpace(opts.Mode))
	if mode == "" {
		if opts.AppEnv == EnvProd {
			mode = MigrateModeDryRun
		} else {
			mode = MigrateModeApply
		}
	}
	if mode != MigrateModeDryRun && mode != MigrateModeApply {
		return nil, fmt.Errorf("unsupported MIGRATE_MODE %q: must be dry-run or apply", mode)
	}

	report := &MigrateReport{
		AppEnv: opts.AppEnv,
		Mode:   mode,
	}

	if mode == MigrateModeApply {
		if err := gdb.WithContext(ctx).AutoMigrate(model.AllModels()...); err != nil {
			return nil, fmt.Errorf("gorm automigrate: %w", err)
		}
		report.Applied = true
	}

	checks, err := VerifySchema(ctx, gdb)
	report.Tables = checks
	if err != nil {
		return report, err
	}
	return report, nil
}

// VerifySchema 校验全部 9 张表和核心唯一索引/联合索引是否存在。
func VerifySchema(ctx context.Context, gdb *gorm.DB) ([]TableCheck, error) {
	migrator := gdb.WithContext(ctx).Migrator()
	specs := expectedSchema()
	checks := make([]TableCheck, 0, len(specs))
	var missing []string

	for _, spec := range specs {
		check := TableCheck{
			Table:  spec.table,
			Exists: migrator.HasTable(spec.model),
		}
		if !check.Exists {
			missing = append(missing, fmt.Sprintf("table %s", spec.table))
		} else {
			for _, idx := range spec.indexes {
				if !migrator.HasIndex(spec.model, idx) {
					check.MissingIndexes = append(check.MissingIndexes, idx)
					missing = append(missing, fmt.Sprintf("index %s.%s", spec.table, idx))
				}
			}
		}
		checks = append(checks, check)
	}

	if len(missing) > 0 {
		return checks, fmt.Errorf("schema verification incomplete: missing %s", strings.Join(missing, ", "))
	}
	return checks, nil
}
