package gormrepo

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
)

// ScreeningFilter 聚合初筛列表的全部筛选、父体折叠、按月过滤、排序与分页参数。
type ScreeningFilter struct {
	ItemIDs          []uint64
	Keyword          string
	BrandCategory    string
	MachineResult    string
	ManualStatus     string
	ProfitCalcStatus string
	MinPrice         *float64
	MaxPrice         *float64
	MinSales         *int
	MaxSales         *int
	ParentOnly       bool
	ParentASIN       string
	StatMonth        string // 指定月份过滤（YYYY-MM）
	MinMonthlySales  *int
	MaxMonthlySales  *int
	MinMonthlyPrice  *float64
	MaxMonthlyPrice  *float64
	RecentMonths     int // 每个商品返回的近 N 个月明细数量（0 表示返回全部）
	SortBy           string
	SortDesc         bool
	Page             int
	PageSize         int
}

// ScreeningItemWithDetails 聚合商品主表、月度售价销量明细与最新有效利润测算。
type ScreeningItemWithDetails struct {
	Item             model.ScreeningItem
	MonthlyMetrics   []model.ScreeningItemMonthlyMetric
	ActiveProfitCalc *model.ProfitCalculation
}

// ScreeningListResult 返回分页初筛商品及其聚合关联数据。
type ScreeningListResult struct {
	Items    []ScreeningItemWithDetails
	Total    int64
	Page     int
	PageSize int
}

// BatchManualReviewRequest 描述单条或批量人工复核请求。
type BatchManualReviewRequest struct {
	ItemIDs    []uint64
	NextStatus string // pending / approved / rejected
	Note       string
	OperatorID string
	BatchID    string
	ActionType string // single / batch
	ReviewedAt time.Time
}

// ScreeningRepo 定义初筛商品、复核流水与父子体汇总的 GORM 仓储接口。
type ScreeningRepo interface {
	BatchUpsertItems(ctx context.Context, workspaceID uint64, items []model.ScreeningItem) error
	GetByID(ctx context.Context, workspaceID uint64, id uint64) (*model.ScreeningItem, error)
	GetByASIN(ctx context.Context, workspaceID uint64, asin string) (*model.ScreeningItem, error)
	ListByASINs(ctx context.Context, workspaceID uint64, asins []string) (map[string]model.ScreeningItem, error)
	ListWithDetails(ctx context.Context, workspaceID uint64, filter ScreeningFilter) (*ScreeningListResult, error)
	BatchManualReview(ctx context.Context, workspaceID uint64, req BatchManualReviewRequest) (int, error)
	ListReviewLogs(ctx context.Context, workspaceID uint64, itemID uint64, page, pageSize int) ([]model.ScreeningReviewLog, int64, error)
	RecalculateParentChildCounts(ctx context.Context, workspaceID uint64) error
}

type gormScreeningRepo struct {
	store *Store
	db    *gorm.DB
}

func (r *gormScreeningRepo) BatchUpsertItems(ctx context.Context, workspaceID uint64, items []model.ScreeningItem) error {
	if len(items) == 0 {
		return nil
	}
	for i := range items {
		items[i].WorkspaceID = workspaceID
		items[i].DeletedAt = gorm.DeletedAt{}
		if items[i].Currency == "" {
			items[i].Currency = "USD"
		}
		if items[i].MachineResult == "" {
			items[i].MachineResult = "pending"
		}
		if items[i].ManualStatus == "" {
			items[i].ManualStatus = "pending"
		}
		if items[i].ProfitCalcStatus == "" {
			items[i].ProfitCalcStatus = "pending"
		}
	}
	// 增量写入严禁覆盖已有的人工复核结论与利润测算结果，且冲突时恢复 deleted_at=NULL。
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "workspace_id"}, {Name: "asin"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_data", "parent_asin", "is_parent", "child_count",
			"title_zh", "title_en", "brand", "brand_category",
			"category_path", "category_zh", "image_url", "listing_period",
			"latest_price", "currency", "latest_sales", "child_sales",
			"rating", "review_count",
			"item_dimensions", "package_dimensions", "net_weight_grams", "gross_weight_grams",
			"machine_result", "machine_rule_version", "machine_reasons",
			"last_import_task_id", "deleted_at", "updated_at",
		}),
	}).CreateInBatches(items, DefaultBatchSize).Error
}

func (r *gormScreeningRepo) GetByID(ctx context.Context, workspaceID uint64, id uint64) (*model.ScreeningItem, error) {
	var item model.ScreeningItem
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *gormScreeningRepo) GetByASIN(ctx context.Context, workspaceID uint64, asin string) (*model.ScreeningItem, error) {
	var item model.ScreeningItem
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "asin"}, Value: asin}).
		First(&item).Error
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *gormScreeningRepo) ListByASINs(ctx context.Context, workspaceID uint64, asins []string) (map[string]model.ScreeningItem, error) {
	result := make(map[string]model.ScreeningItem, len(asins))
	if len(asins) == 0 {
		return result, nil
	}
	vals := make([]any, len(asins))
	for i, a := range asins {
		vals[i] = a
	}
	var rows []model.ScreeningItem
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.IN{Column: clause.Column{Name: "asin"}, Values: vals}).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.ASIN] = row
	}
	return result, nil
}

// ListWithDetails 分页查询初筛商品，并一次性批量拉取月度指标与有效利润测算，杜绝 N+1 查询。
func (r *gormScreeningRepo) ListWithDetails(ctx context.Context, workspaceID uint64, filter ScreeningFilter) (*ScreeningListResult, error) {
	page, pageSize := normalizePagination(filter.Page, filter.PageSize)

	base := r.db.WithContext(ctx).
		Model(&model.ScreeningItem{}).
		Scopes(
			WithWorkspace(workspaceID),
			WithKeyword(filter.Keyword),
			WithBrandCategory(filter.BrandCategory),
			WithMachineResult(filter.MachineResult),
			WithManualStatus(filter.ManualStatus),
			WithProfitCalcStatus(filter.ProfitCalcStatus),
			WithPriceRange(filter.MinPrice, filter.MaxPrice),
			WithSalesRange(filter.MinSales, filter.MaxSales),
			WithMonthlyMetricFilter(workspaceID, filter.StatMonth, filter.MinMonthlySales, filter.MaxMonthlySales, filter.MinMonthlyPrice, filter.MaxMonthlyPrice),
			WithParentFold(filter.ParentOnly, filter.ParentASIN),
		)

	if len(filter.ItemIDs) > 0 {
		base = base.Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uint64SliceToAny(filter.ItemIDs)})
	}

	var total int64
	if err := base.Count(&total).Error; err != nil {
		return nil, err
	}

	var items []model.ScreeningItem
	if err := base.
		Scopes(WithSort(filter.SortBy, filter.SortDesc), WithPagination(page, pageSize)).
		Find(&items).Error; err != nil {
		return nil, err
	}

	itemIDs := make([]uint64, len(items))
	for i, item := range items {
		itemIDs[i] = item.ID
	}

	metricsByItem, err := r.store.MonthlyMetrics().ListByItemIDs(ctx, workspaceID, itemIDs)
	if err != nil {
		return nil, err
	}
	profitsByItem, err := r.store.ProfitCalcs().GetActiveByItemIDs(ctx, workspaceID, itemIDs)
	if err != nil {
		return nil, err
	}

	details := make([]ScreeningItemWithDetails, len(items))
	for i, item := range items {
		metrics := metricsByItem[item.ID]
		if filter.RecentMonths > 0 && len(metrics) > filter.RecentMonths {
			metrics = metrics[:filter.RecentMonths]
		}
		var activeCalc *model.ProfitCalculation
		if calc, ok := profitsByItem[item.ID]; ok {
			c := calc
			activeCalc = &c
		}
		details[i] = ScreeningItemWithDetails{
			Item:             item,
			MonthlyMetrics:   metrics,
			ActiveProfitCalc: activeCalc,
		}
	}

	return &ScreeningListResult{
		Items:    details,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}

// BatchManualReview 在单一事务中更新人工复核结论并写入审计流水。
func (r *gormScreeningRepo) BatchManualReview(ctx context.Context, workspaceID uint64, req BatchManualReviewRequest) (int, error) {
	if len(req.ItemIDs) == 0 {
		return 0, nil
	}
	if req.NextStatus != "pending" && req.NextStatus != "approved" && req.NextStatus != "rejected" && req.NextStatus != "unsure" {
		return 0, fmt.Errorf("invalid manual status %q", req.NextStatus)
	}
	actionType := req.ActionType
	if actionType == "" {
		if len(req.ItemIDs) == 1 {
			actionType = "single"
		} else {
			actionType = "batch"
		}
	}
	reviewedAt := req.ReviewedAt
	if reviewedAt.IsZero() {
		reviewedAt = time.Now()
	}

	updatedCount := 0
	err := r.store.Transaction(ctx, func(txStore *Store) error {
		tx := txStore.db
		var targets []model.ScreeningItem
		if err := tx.Scopes(WithWorkspace(workspaceID)).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uint64SliceToAny(req.ItemIDs)}).
			Find(&targets).Error; err != nil {
			return err
		}
		if len(targets) == 0 {
			return nil
		}

		targetIDs := make([]uint64, len(targets))
		logs := make([]model.ScreeningReviewLog, len(targets))
		for i, item := range targets {
			targetIDs[i] = item.ID
			logs[i] = model.ScreeningReviewLog{
				WorkspaceID: workspaceID,
				ItemID:      item.ID,
				ASIN:        item.ASIN,
				BatchID:     req.BatchID,
				ActionType:  actionType,
				PrevStatus:  item.ManualStatus,
				NextStatus:  req.NextStatus,
				Note:        req.Note,
				OperatorID:  req.OperatorID,
			}
		}

		if err := tx.Model(&model.ScreeningItem{}).
			Scopes(WithWorkspace(workspaceID)).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: uint64SliceToAny(targetIDs)}).
			Updates(map[string]any{
				"manual_status": req.NextStatus,
				"manual_note":   req.Note,
				"reviewed_by":   req.OperatorID,
				"reviewed_at":   &reviewedAt,
			}).Error; err != nil {
			return err
		}

		if err := tx.CreateInBatches(logs, DefaultBatchSize).Error; err != nil {
			return err
		}
		updatedCount = len(targets)
		return nil
	})
	return updatedCount, err
}

func (r *gormScreeningRepo) ListReviewLogs(ctx context.Context, workspaceID uint64, itemID uint64, page, pageSize int) ([]model.ScreeningReviewLog, int64, error) {
	var total int64
	base := r.db.WithContext(ctx).
		Model(&model.ScreeningReviewLog{}).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "item_id"}, Value: itemID})
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []model.ScreeningReviewLog
	err := base.
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}, Desc: true}}}).
		Scopes(WithPagination(page, pageSize)).
		Find(&rows).Error
	return rows, total, err
}

// RecalculateParentChildCounts 统计并回填工作空间内所有父体的 ChildCount。
func (r *gormScreeningRepo) RecalculateParentChildCounts(ctx context.Context, workspaceID uint64) error {
	var items []model.ScreeningItem
	if err := r.db.WithContext(ctx).
		Select("id", "asin", "parent_asin", "is_parent", "child_count").
		Scopes(WithWorkspace(workspaceID)).
		Find(&items).Error; err != nil {
		return err
	}

	childCounts := map[string]int{}
	for _, item := range items {
		if item.ParentASIN != "" && item.ParentASIN != item.ASIN {
			childCounts[item.ParentASIN]++
		}
	}

	return r.store.Transaction(ctx, func(txStore *Store) error {
		tx := txStore.db
		for _, item := range items {
			count := childCounts[item.ASIN]
			isParent := item.IsParent || count > 0 || item.ParentASIN == "" || item.ParentASIN == item.ASIN
			if item.ChildCount != count || item.IsParent != isParent {
				if err := tx.Model(&model.ScreeningItem{}).
					Scopes(WithWorkspace(workspaceID)).
					Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: item.ID}).
					Updates(map[string]any{
						"child_count": count,
						"is_parent":   isParent,
					}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}
