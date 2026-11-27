package gormrepo

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
)

// ProfitCalcRepo 定义利润测算明细与主表状态联动的 GORM 仓储接口。
type ProfitCalcRepo interface {
	SaveActiveCalculation(ctx context.Context, workspaceID uint64, calc *model.ProfitCalculation) error
	GetActiveByItemIDs(ctx context.Context, workspaceID uint64, itemIDs []uint64) (map[uint64]model.ProfitCalculation, error)
	ListByItemID(ctx context.Context, workspaceID uint64, itemID uint64) ([]model.ProfitCalculation, error)
}

type gormProfitCalcRepo struct {
	store *Store
	db    *gorm.DB
}

// SaveActiveCalculation 在单一事务中停用旧测算版本、写入新有效测算记录，并同步更新 screening_items 利润快照。
func (r *gormProfitCalcRepo) SaveActiveCalculation(ctx context.Context, workspaceID uint64, calc *model.ProfitCalculation) error {
	return r.store.Transaction(ctx, func(txStore *Store) error {
		tx := txStore.db

		var locked model.ScreeningItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Scopes(WithWorkspace(workspaceID)).Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: calc.ItemID}).First(&locked).Error; err != nil {
			return err
		}

		var latest model.ProfitCalculation
		nextVersion := 1
		err := tx.Model(&model.ProfitCalculation{}).
			Scopes(WithWorkspace(workspaceID)).
			Where(clause.Eq{Column: clause.Column{Name: "item_id"}, Value: calc.ItemID}).
			Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{
				{Column: clause.Column{Name: "version"}, Desc: true},
			}}).
			First(&latest).Error
		if err == nil {
			nextVersion = latest.Version + 1
		} else if err != gorm.ErrRecordNotFound {
			return err
		}

		if err := tx.Model(&model.ProfitCalculation{}).
			Scopes(WithWorkspace(workspaceID)).
			Where(clause.Eq{Column: clause.Column{Name: "item_id"}, Value: calc.ItemID}).
			Where(clause.Eq{Column: clause.Column{Name: "is_active"}, Value: true}).
			Updates(map[string]any{"is_active": false}).Error; err != nil {
			return err
		}

		calc.WorkspaceID = workspaceID
		if calc.Version <= 0 {
			calc.Version = nextVersion
		}
		calc.IsActive = true
		if calc.Currency == "" {
			calc.Currency = "USD"
		}
		if err := tx.Create(calc).Error; err != nil {
			return err
		}

		calcID := calc.ID
		margin := calc.ProfitMargin
		profit := calc.EstimatedProfit
		return tx.Model(&model.ScreeningItem{}).
			Scopes(WithWorkspace(workspaceID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: calc.ItemID}).
			Updates(map[string]any{
				"profit_calc_status": "calculated",
				"latest_calc_id":     &calcID,
				"profit_margin":      &margin,
				"estimated_profit":   &profit,
			}).Error
	})
}

// GetActiveByItemIDs 批量拉取当前页商品的有效利润测算记录，避免 N+1 查询。
func (r *gormProfitCalcRepo) GetActiveByItemIDs(ctx context.Context, workspaceID uint64, itemIDs []uint64) (map[uint64]model.ProfitCalculation, error) {
	result := make(map[uint64]model.ProfitCalculation, len(itemIDs))
	if len(itemIDs) == 0 {
		return result, nil
	}
	var rows []model.ProfitCalculation
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.IN{Column: clause.Column{Name: "item_id"}, Values: uint64SliceToAny(itemIDs)}).
		Where(clause.Eq{Column: clause.Column{Name: "is_active"}, Value: true}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "version"}, Desc: true},
			{Column: clause.Column{Name: "id"}, Desc: true},
		}}).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if _, exists := result[row.ItemID]; !exists {
			result[row.ItemID] = row
		}
	}
	return result, nil
}

func (r *gormProfitCalcRepo) ListByItemID(ctx context.Context, workspaceID uint64, itemID uint64) ([]model.ProfitCalculation, error) {
	var rows []model.ProfitCalculation
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "item_id"}, Value: itemID}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "version"}, Desc: true},
			{Column: clause.Column{Name: "id"}, Desc: true},
		}}).
		Find(&rows).Error
	return rows, err
}
