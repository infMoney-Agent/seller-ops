package screening

import (
	"context"
	"encoding/json"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
	"strconv"
)

func (s *service) gormValidateMonthlyReference(ctx context.Context, ws uint64, item model.ScreeningItem, ref MonthlyReferenceSpec) error {
	if ref.MonthlyID == "" {
		if ref.ExpectedMonthlyRevision != nil {
			return invalid("月度ID和版本必须同时提供")
		}
		return nil
	}
	if ref.ExpectedMonthlyRevision == nil {
		return conflict("月度数据已变化")
	}
	var source Object
	if json.Unmarshal(item.SourceData, &source) == nil {
		if rows, ok := source["source_monthly_history"].([]any); ok {
			for _, v := range rows {
				row := v.(map[string]any)
				if str(row["id"]) == ref.MonthlyID {
					if integer(row["revision"]) != *ref.ExpectedMonthlyRevision {
						return conflict("月度数据已变化")
					}
					return nil
				}
			}
		}
	}
	grouped, err := s.gormStore.MonthlyMetrics().ListByItemIDs(ctx, ws, []uint64{item.ID})
	if err != nil {
		return err
	}
	for _, m := range grouped[item.ID] {
		if strconv.FormatUint(m.ID, 10) == ref.MonthlyID {
			if *ref.ExpectedMonthlyRevision != 1 {
				return conflict("月度数据已变化")
			}
			return nil
		}
	}
	return notFound("月度记录不存在")
}

func (s *service) gormCalculateProfit(ctx context.Context, a identity, cmd CalculateProfitCommand) (*ProfitCalculationResult, error) {
	ws, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	id, err := parseUintID(cmd.ProductID)
	if err != nil {
		return nil, err
	}
	item, err := s.gormStore.Screening().GetByID(ctx, ws, id)
	if err != nil {
		return nil, err
	}
	if cmd.ExpectedDataRevision == nil || *cmd.ExpectedDataRevision != 1 {
		return nil, conflict("商品数据已变化")
	}
	if err = s.gormValidateMonthlyReference(ctx, ws, *item, cmd.Monthly); err != nil {
		return nil, err
	}
	if !cmd.HasScenarioList || len(cmd.Scenarios) == 0 || len(cmd.Scenarios) > 20 {
		if !cmd.HasScenarioList || !cmd.InvalidScenario {
			return nil, invalid("每次试算1到20个方案")
		}
	}
	if cmd.InvalidScenario {
		return nil, invalid("方案无效")
	}
	p := gormItemToObject(gormrepo.ScreeningItemWithDetails{Item: *item})
	items := []CalculatedScenarioItem{}
	for _, in := range cmd.Scenarios {
		out, e := calculateProductProfitStructured(in, p)
		if e != nil {
			return nil, invalid(e.Error())
		}
		items = append(items, CalculatedScenarioItem{Name: optionalImportString(in["name"]), Inputs: in, Output: out})
	}
	return &ProfitCalculationResult{Items: items, FormulaVersion: formulaVersion}, nil
}
