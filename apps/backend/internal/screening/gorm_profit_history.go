package screening

import (
	"bytes"
	"context"
	"encoding/json"
	"net/url"
	"sort"
	"time"
)

func (s *service) gormListProductProfitScenariosUsecase(ctx context.Context, a identity, id string, params url.Values) (any, error) {
	ws, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	item, err := s.gormStore.Screening().GetByID(ctx, ws, itemID)
	if err != nil {
		return nil, err
	}
	rows, err := s.gormStore.ProfitCalcs().ListByItemID(ctx, ws, itemID)
	if err != nil {
		return nil, err
	}
	var source Object
	decoder := json.NewDecoder(bytes.NewReader(item.ProfitSourceData))
	decoder.UseNumber()
	archived := decoder.Decode(&source) == nil
	items := []Object{}
	if raw, ok := source["source_profit_history"].([]any); ok {
		for _, v := range raw {
			row := v.(map[string]any)
			row["product_id"] = id
			for _, key := range []string{"imported_fba_fee_usd", "imported_gross_margin_rate"} {
				if row[key] != nil {
					row[key] = str(row[key])
				}
			}
			items = append(items, row)
		}
	}
	for _, c := range rows {
		if !archived || len(c.SourceData) > 0 {
			items = append(items, gormProfitToObject(c))
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, _ := time.Parse(time.RFC3339Nano, str(items[i]["created_at"]))
		right, _ := time.Parse(time.RFC3339Nano, str(items[j]["created_at"]))
		if left.Equal(right) {
			return str(items[i]["id"]) > str(items[j]["id"])
		}
		return left.After(right)
	})
	p, n := queryPage(params)
	total := len(items)
	start := (p - 1) * n
	if start > total {
		start = total
	}
	end := start + n
	if end > total {
		end = total
	}
	return Object{"items": items[start:end], "total": total, "page": p, "page_size": n}, nil
}
