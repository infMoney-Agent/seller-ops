package gormrepo

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
)

// MonthlyMetricRepo 定义商品多月份售价与销量明细表的 GORM 仓储接口。
type MonthlyMetricRepo interface {
	BatchUpsert(ctx context.Context, workspaceID uint64, metrics []model.ScreeningItemMonthlyMetric) error
	ListByItemIDs(ctx context.Context, workspaceID uint64, itemIDs []uint64) (map[uint64][]model.ScreeningItemMonthlyMetric, error)
	ListDistinctMonths(ctx context.Context, workspaceID uint64) ([]string, error)
	SyncLatestToItems(ctx context.Context, workspaceID uint64, itemIDs []uint64) error
}

type gormMonthlyMetricRepo struct {
	db *gorm.DB
}

func (r *gormMonthlyMetricRepo) BatchUpsert(ctx context.Context, workspaceID uint64, metrics []model.ScreeningItemMonthlyMetric) error {
	if len(metrics) == 0 {
		return nil
	}
	for i := range metrics {
		metrics[i].WorkspaceID = workspaceID
		if metrics[i].Currency == "" {
			metrics[i].Currency = "USD"
		}
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "workspace_id"},
			{Name: "asin"},
			{Name: "stat_month"},
		},
		DoUpdates: clause.AssignmentColumns([]string{
			"item_id", "parent_asin", "selling_price", "currency",
			"monthly_sales", "child_sales", "bsr_rank", "source_task_id", "updated_at",
		}),
	}).CreateInBatches(metrics, DefaultBatchSize).Error
}

// ListByItemIDs 批量拉取指定商品集合的月度明细并按 item_id 分组，避免 N+1 查询。
func (r *gormMonthlyMetricRepo) ListByItemIDs(ctx context.Context, workspaceID uint64, itemIDs []uint64) (map[uint64][]model.ScreeningItemMonthlyMetric, error) {
	result := make(map[uint64][]model.ScreeningItemMonthlyMetric, len(itemIDs))
	if len(itemIDs) == 0 {
		return result, nil
	}
	var rows []model.ScreeningItemMonthlyMetric
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.IN{Column: clause.Column{Name: "item_id"}, Values: uint64SliceToAny(itemIDs)}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "stat_month"}, Desc: true},
			{Column: clause.Column{Name: "id"}, Desc: true},
		}}).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ItemID] = append(result[row.ItemID], row)
	}
	return result, nil
}

func (r *gormMonthlyMetricRepo) ListDistinctMonths(ctx context.Context, workspaceID uint64) ([]string, error) {
	var months []string
	err := r.db.WithContext(ctx).
		Model(&model.ScreeningItemMonthlyMetric{}).
		Distinct("stat_month").
		Scopes(WithWorkspace(workspaceID)).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "stat_month"}, Desc: true},
		}}).
		Pluck("stat_month", &months).Error
	return months, err
}

// SyncLatestToItems 将每个商品最新月份（MAX(stat_month)）的售价与销量回填至 screening_items 主表。
func (r *gormMonthlyMetricRepo) SyncLatestToItems(ctx context.Context, workspaceID uint64, itemIDs []uint64) error {
	grouped, err := r.ListByItemIDs(ctx, workspaceID, itemIDs)
	if err != nil {
		return err
	}
	for itemID, list := range grouped {
		if len(list) == 0 {
			continue
		}
		latest := list[0] // 已按 stat_month DESC 排序
		updates := map[string]any{
			"latest_price": latest.SellingPrice,
			"latest_sales": latest.MonthlySales,
			"currency":     latest.Currency,
		}
		if latest.ChildSales != nil {
			updates["child_sales"] = latest.ChildSales
		}
		if err := r.db.WithContext(ctx).
			Model(&model.ScreeningItem{}).
			Scopes(WithWorkspace(workspaceID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: itemID}).
			Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

func uint64SliceToAny(ids []uint64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}
