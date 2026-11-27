package screening

import (
	"context"
	"encoding/json"
	"math/big"
	"net/url"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
	"sort"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func gormProfitItem(d gormrepo.ScreeningItemWithDetails) Object {
	out := gormItemToObject(d)
	var source Object
	if json.Unmarshal(d.Item.ProfitSourceData, &source) == nil {
		for _, key := range []string{"scenario", "scenario_stale", "imported_fees", "profit_source"} {
			out[key] = source[key]
		}
	}
	if calc := d.ActiveProfitCalc; calc != nil && len(calc.SourceData) > 0 {
		scenario := gormProfitToObject(*calc)
		out["scenario"] = scenario
		out["scenario_stale"] = false
		out["profit_output"] = scenario["output"]
	}
	out["family_rank"] = d.Item.SourceFamilyRank
	key := "parent:" + d.Item.ParentASIN
	if d.Item.ParentASIN == "" {
		key = "item:" + str(out["id"])
	}
	out["family_key"] = key
	if specs, ok := out["specs"].(map[string]any); ok {
		delete(specs, "legacy_site_snapshot")
	}
	return out
}

func (s *service) gormProfitProductsUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	ws, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	grouped := params.Get("group_by_parent") == "true"
	if v := params.Get("group_by_parent"); v != "" && v != "true" && v != "false" {
		return nil, invalid("父体分组参数无效")
	}
	completion := params.Get("completion")
	if completion != "" && completion != "complete" && completion != "incomplete" {
		return nil, invalid("不支持的填写状态")
	}
	f := buildGORMScreeningFilter(params)
	f.ParentOnly = false
	f.ManualStatus = "approved"
	f.PageSize = 200
	f.Page = 1
	f.SortBy = "id"
	f.SortDesc = false
	focus, selected := params.Get("product_id"), params.Get("selected_ids")
	selectedSet := map[uint64]bool{}
	if selected != "" {
		parts := strings.Split(selected, ",")
		if len(parts) > 100 {
			return nil, invalid("最多选择100个商品")
		}
		for _, part := range parts {
			id, e := parseUintID(part)
			if e != nil {
				return nil, e
			}
			selectedSet[id] = true
			f.ItemIDs = append(f.ItemIDs, id)
		}
	}
	if focus != "" {
		id, e := parseUintID(focus)
		if e != nil {
			return nil, e
		}
		f.ItemIDs = []uint64{id}
	}
	if focus != "" || selected != "" {
		f.ManualStatus = ""
	}
	if family := params.Get("family_product_id"); family != "" {
		id, e := parseUintID(family)
		if e != nil {
			return nil, e
		}
		anchor, e := s.gormStore.Screening().GetByID(ctx, ws, id)
		if e != nil {
			return nil, e
		}
		if anchor.ParentASIN == "" {
			f.ItemIDs = []uint64{id}
		} else {
			f.ParentASIN = anchor.ParentASIN
		}
	}
	field := params.Get("sort")
	direction := "ASC"
	if field == "" {
		field = "updated_at"
		direction = "DESC"
	}
	if strings.HasPrefix(field, "-") {
		field = strings.TrimPrefix(field, "-")
		direction = "DESC"
	}
	allowed := map[string]bool{"updated_at": true, "asin": true, "price_usd": true, "monthly_sales": true, "fba_fee_usd": true, "storage_usd": true, "freight_cny": true, "freight_rate_cny_per_kg": true, "purchase_cny": true, "commission_rate": true, "profit_cny": true, "margin": true, "volumetric_weight_kg": true, "billable_weight_kg": true, "commission_usd": true}
	if !allowed[field] {
		return nil, invalid("不支持的利润排序字段")
	}

	page, size := queryPage(params)
	gdb := s.gormStore.DB().WithContext(ctx)
	filterScopes := []func(*gorm.DB) *gorm.DB{
		gormrepo.WithWorkspace(ws),
		func(db *gorm.DB) *gorm.DB {
			if len(f.ItemIDs) == 0 {
				return db
			}
			vals := make([]any, len(f.ItemIDs))
			for i, id := range f.ItemIDs {
				vals[i] = id
			}
			return db.Where(clause.IN{Column: clause.Column{Name: "id"}, Values: vals})
		},
		gormrepo.WithKeyword(f.Keyword),
		gormrepo.WithBrandCategory(f.BrandCategory),
		gormrepo.WithMachineResult(f.MachineResult),
		gormrepo.WithManualStatus(f.ManualStatus),
		gormrepo.WithProfitCalcStatus(f.ProfitCalcStatus),
		gormrepo.WithPriceRange(f.MinPrice, f.MaxPrice),
		gormrepo.WithSalesRange(f.MinSales, f.MaxSales),
		gormrepo.WithParentFold(false, f.ParentASIN),
		gormrepo.WithMonthlyMetricFilter(ws, f.StatMonth, f.MinMonthlySales, f.MaxMonthlySales, f.MinMonthlyPrice, f.MaxMonthlyPrice),
	}

	// 快速路径：未按填写完成度过滤且排序字段为 updated_at / asin 时，仅拉轻量字段完成分组、排序与分页，再仅为当前页（≤50条）加载大 JSON 与利润计算。
	if completion == "" && (field == "updated_at" || field == "asin") {
		var light []model.ScreeningItem
		if err := gdb.Model(&model.ScreeningItem{}).
			Select("id", "asin", "parent_asin", "source_family_rank", "updated_at").
			Scopes(filterScopes...).
			Find(&light).Error; err != nil {
			return nil, err
		}
		if selected != "" {
			kept := light[:0]
			for _, it := range light {
				if selectedSet[it.ID] {
					kept = append(kept, it)
				}
			}
			light = kept
		}
		productTotal := len(light)

		type lightEntry struct {
			item        model.ScreeningItem
			familyCount int
		}
		entries := make([]lightEntry, 0, len(light))
		if grouped {
			repByKey := make(map[string]int, len(light))
			for _, it := range light {
				key := "parent:" + it.ParentASIN
				if it.ParentASIN == "" {
					key = "item:" + strconv.FormatUint(it.ID, 10)
				}
				if idx, ok := repByKey[key]; ok {
					entries[idx].familyCount++
					if it.SourceFamilyRank < entries[idx].item.SourceFamilyRank {
						entries[idx].item = it
					}
				} else {
					repByKey[key] = len(entries)
					entries = append(entries, lightEntry{item: it, familyCount: 1})
				}
			}
		} else {
			for _, it := range light {
				entries = append(entries, lightEntry{item: it, familyCount: 1})
			}
		}

		sort.SliceStable(entries, func(i, j int) bool {
			a, b := entries[i].item, entries[j].item
			var cmp int
			if field == "asin" {
				cmp = strings.Compare(a.ASIN, b.ASIN)
			} else {
				if a.UpdatedAt.Before(b.UpdatedAt) {
					cmp = -1
				} else if a.UpdatedAt.After(b.UpdatedAt) {
					cmp = 1
				}
			}
			if cmp == 0 {
				return strconv.FormatUint(a.ID, 10) < strconv.FormatUint(b.ID, 10)
			}
			if direction == "DESC" {
				return cmp > 0
			}
			return cmp < 0
		})

		total := len(entries)
		start := (page - 1) * size
		if start > total {
			start = total
		}
		end := start + size
		if end > total {
			end = total
		}
		pageEntries := entries[start:end]

		pageIDs := make([]uint64, len(pageEntries))
		idVals := make([]any, len(pageEntries))
		for i, e := range pageEntries {
			pageIDs[i] = e.item.ID
			idVals[i] = e.item.ID
		}
		fullByID := make(map[uint64]model.ScreeningItem, len(pageIDs))
		if len(pageIDs) > 0 {
			var fullItems []model.ScreeningItem
			if err := gdb.Scopes(gormrepo.WithWorkspace(ws)).
				Where(clause.IN{Column: clause.Column{Name: "id"}, Values: idVals}).
				Find(&fullItems).Error; err != nil {
				return nil, err
			}
			for _, it := range fullItems {
				fullByID[it.ID] = it
			}
		}
		metricsByItem, err := s.gormStore.MonthlyMetrics().ListByItemIDs(ctx, ws, pageIDs)
		if err != nil {
			return nil, err
		}
		profitsByItem, err := s.gormStore.ProfitCalcs().GetActiveByItemIDs(ctx, ws, pageIDs)
		if err != nil {
			return nil, err
		}

		pageItems := make([]Object, 0, len(pageEntries))
		for _, e := range pageEntries {
			full, ok := fullByID[e.item.ID]
			if !ok {
				continue
			}
			var activeCalc *model.ProfitCalculation
			if c, ok := profitsByItem[full.ID]; ok {
				cp := c
				activeCalc = &cp
			}
			obj := gormProfitItem(gormrepo.ScreeningItemWithDetails{
				Item:             full,
				MonthlyMetrics:   metricsByItem[full.ID],
				ActiveProfitCalc: activeCalc,
			})
			obj["family_count"] = e.familyCount
			delete(obj, "family_key")
			delete(obj, "family_rank")
			pageItems = append(pageItems, obj)
		}
		populateProfitRows(pageItems)
		return Object{
			"items":               pageItems,
			"page":                page,
			"page_size":           size,
			"total":               total,
			"product_total":       productTotal,
			"referral_categories": referralOptions(),
		}, nil
	}

	// 计算排序或按填写状态过滤路径：一次性批量加载，避免多轮分页循环
	var allItems []model.ScreeningItem
	if err := gdb.Model(&model.ScreeningItem{}).
		Scopes(filterScopes...).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}).
		Find(&allItems).Error; err != nil {
		return nil, err
	}
	allIDs := make([]uint64, 0, len(allItems))
	for _, it := range allItems {
		if selected != "" && !selectedSet[it.ID] {
			continue
		}
		allIDs = append(allIDs, it.ID)
	}
	metricsByItem, err := s.gormStore.MonthlyMetrics().ListByItemIDs(ctx, ws, allIDs)
	if err != nil {
		return nil, err
	}
	profitsByItem, err := s.gormStore.ProfitCalcs().GetActiveByItemIDs(ctx, ws, allIDs)
	if err != nil {
		return nil, err
	}
	items := make([]Object, 0, len(allIDs))
	for _, it := range allItems {
		if selected != "" && !selectedSet[it.ID] {
			continue
		}
		var activeCalc *model.ProfitCalculation
		if c, ok := profitsByItem[it.ID]; ok {
			cp := c
			activeCalc = &cp
		}
		items = append(items, gormProfitItem(gormrepo.ScreeningItemWithDetails{
			Item:             it,
			MonthlyMetrics:   metricsByItem[it.ID],
			ActiveProfitCalc: activeCalc,
		}))
	}
	populateProfitRows(items)
	if completion != "" {
		filtered := []Object{}
		for _, it := range items {
			output, _ := it["preview_output"].(map[string]any)
			if str(output["state"]) == completion {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	productTotal := len(items)
	if grouped {
		representatives := map[string]Object{}
		counts := map[string]int{}
		for _, it := range items {
			key := str(it["family_key"])
			counts[key]++
			old := representatives[key]
			if old == nil || integer(it["family_rank"]) < integer(old["family_rank"]) {
				representatives[key] = it
			}
		}
		items = []Object{}
		for key, it := range representatives {
			it["family_count"] = counts[key]
			items = append(items, it)
		}
	} else {
		for _, it := range items {
			it["family_count"] = 1
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		left, right := items[i], items[j]
		cmp := 0
		if field == "updated_at" || field == "asin" {
			cmp = strings.Compare(str(left[field]), str(right[field]))
		} else {
			a, b := gormProfitSortValue(left, field), gormProfitSortValue(right, field)
			if a == nil || b == nil {
				if a == nil && b != nil {
					return false
				}
				if a != nil && b == nil {
					return true
				}
			} else {
				cmp = a.Cmp(b)
			}
		}
		if cmp == 0 {
			return str(left["id"]) < str(right["id"])
		}
		if direction == "DESC" {
			return cmp > 0
		}
		return cmp < 0
	})
	total := len(items)
	start := (page - 1) * size
	if start > total {
		start = total
	}
	end := start + size
	if end > total {
		end = total
	}
	for _, it := range items[start:end] {
		delete(it, "family_key")
		delete(it, "family_rank")
	}
	return Object{"items": items[start:end], "page": page, "page_size": size, "total": total, "product_total": productTotal, "referral_categories": referralOptions()}, nil
}

func gormProfitSortValue(it Object, field string) *big.Rat {
	if field == "price_usd" {
		in, _ := it["draft_inputs"].(map[string]any)
		v, ok := new(big.Rat).SetString(str(in["sale_price_usd"]))
		if ok {
			return v
		}
		return nil
	}
	if field == "monthly_sales" {
		v, ok := new(big.Rat).SetString(str(it["display_child_sales"]))
		if ok {
			return v
		}
		return nil
	}
	return profitRowSortValue(it, field)
}

// Keep numeric identifiers in scenario responses even for imported snapshots.
func gormScenarioID(id uint64) string { return strconv.FormatUint(id, 10) }
