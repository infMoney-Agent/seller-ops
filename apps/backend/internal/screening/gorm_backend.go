package screening

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
)

// ping 检查当前活跃存储后端的连通性。
func (s *service) ping(ctx context.Context) error {
	if s.gormStore != nil {
		sqlDB, err := s.gormStore.DB().DB()
		if err != nil {
			return err
		}
		return sqlDB.PingContext(ctx)
	}
	return s.db.Ping(ctx)
}

func (s *service) validResourceID(id string) error {
	if s.gormStore != nil {
		n, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
		if err != nil || n == 0 {
			return invalid("资源ID无效")
		}
		return nil
	}
	return validImportResourceID(id)
}

func parseUintID(id string) (uint64, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(id), 10, 64)
	if err != nil || n == 0 {
		return 0, invalid("资源ID无效")
	}
	return n, nil
}

// resolveWorkspaceID 将鉴权上下文中的 Tenant ID 映射为 MySQL workspaces.id。
func (s *service) resolveWorkspaceID(ctx context.Context, a identity) (uint64, error) {
	for _, code := range []string{
		fmt.Sprintf("ws-tenant-%d", a.Tenant),
		fmt.Sprintf("tenant-%d", a.Tenant),
	} {
		if ws, err := s.gormStore.Workspaces().GetByCode(ctx, code); err == nil {
			return ws.ID, nil
		}
	}
	// Authentication tenant IDs must resolve through an explicit migration code;
	// a coincidentally equal MySQL primary key belongs to a different namespace.

	return 0, notFound("工作空间不存在")
}

func mapLegacyToGORMManual(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "kept", "approved":
		return "approved"
	case "excluded", "rejected":
		return "rejected"
	case "unsure":
		return "unsure"
	case "pending":
		return "pending"
	default:
		return ""
	}
}

func mapGORMToLegacyManual(status string) string {
	switch status {
	case "approved":
		return "kept"
	case "rejected":
		return "excluded"
	case "unsure":
		return "unsure"
	default:
		return "pending"
	}
}

func mapLegacyToGORMMachine(decision string) string {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "passed":
		return "passed"
	case "excluded", "rejected":
		return "rejected"
	case "insufficient":
		return "insufficient"
	case "pending", "unscreened":
		return "pending"
	default:
		return ""
	}
}

func mapGORMToLegacyMachine(result string) string {
	switch result {
	case "passed":
		return "passed"
	case "rejected":
		return "excluded"
	case "insufficient":
		return "insufficient"
	default:
		return "pending"
	}
}

func gormItemToObject(d gormrepo.ScreeningItemWithDetails) Object {
	it := d.Item
	var reasons any = []any{}
	if len(it.MachineReasons) > 0 {
		var parsed any
		if json.Unmarshal(it.MachineReasons, &parsed) == nil {
			reasons = parsed
		}
	}
	metrics := make([]Object, 0, len(d.MonthlyMetrics))
	for _, m := range d.MonthlyMetrics {
		metrics = append(metrics, gormMonthlyToObject(m))
	}

	var activeProfit any
	var profitOutput any
	if d.ActiveProfitCalc != nil {
		activeProfit = gormProfitToObject(*d.ActiveProfitCalc)
		profitOutput = Object{
			"state":          "complete",
			"profit_usd":     fmt.Sprintf("%.2f", d.ActiveProfitCalc.EstimatedProfit),
			"margin":         fmt.Sprintf("%.4f", d.ActiveProfitCalc.ProfitMargin),
			"commission_usd": fmt.Sprintf("%.2f", d.ActiveProfitCalc.ReferralFee),
			"fba_fee_usd":    fmt.Sprintf("%.2f", d.ActiveProfitCalc.FulfillmentFee),
			"ref_stat_month": d.ActiveProfitCalc.RefStatMonth,
			"selling_price":  fmt.Sprintf("%.2f", d.ActiveProfitCalc.SellingPrice),
			"purchase_cost":  fmt.Sprintf("%.2f", d.ActiveProfitCalc.PurchaseCost),
			"first_leg_cost": fmt.Sprintf("%.2f", d.ActiveProfitCalc.FirstLegFreight),
			"ads_other_cost": fmt.Sprintf("%.2f", d.ActiveProfitCalc.AdsAndOtherCost),
		}
	}

	var latestStatMonth any
	if len(d.MonthlyMetrics) > 0 {
		latestStatMonth = d.MonthlyMetrics[0].StatMonth
	}

	var reviewedAt any
	if it.ReviewedAt != nil {
		reviewedAt = it.ReviewedAt.UTC().Format(time.RFC3339Nano)
	}

	familyCount := 1
	if it.IsParent && it.ChildCount > 0 {
		familyCount = it.ChildCount + 1
	}

	out := Object{
		"id":                     strconv.FormatUint(it.ID, 10),
		"asin":                   it.ASIN,
		"parent_asin":            nullableString(it.ParentASIN),
		"is_parent":              it.IsParent,
		"child_count":            it.ChildCount,
		"family_count":           familyCount,
		"marketplace":            "US",
		"title":                  it.TitleEn,
		"title_zh":               it.TitleZh,
		"brand":                  it.Brand,
		"brand_category":         it.BrandCategory,
		"category":               it.CategoryZh,
		"category_path":          it.CategoryPath,
		"image_url":              it.ImageURL,
		"listing_date":           it.ListingPeriod,
		"price_usd":              fmt.Sprintf("%.2f", it.LatestPrice),
		"latest_price":           it.LatestPrice,
		"currency":               it.Currency,
		"monthly_sales":          it.LatestSales,
		"reported_monthly_sales": it.LatestSales,
		"display_child_sales":    it.ChildSales,
		"rating":                 fmt.Sprintf("%.2f", it.Rating),
		"review_count":           it.ReviewCount,
		"product_dimensions":     it.ItemDimensions,
		"package_dimensions":     it.PackageDimensions,
		"weight_g":               it.NetWeightGrams,
		"package_weight_g":       it.GrossWeightGrams,
		"decision":               mapGORMToLegacyMachine(it.MachineResult),
		"machine_result":         it.MachineResult,
		"reasons":                reasons,
		"review_status":          mapGORMToLegacyManual(it.ManualStatus),
		"manual_status":          it.ManualStatus,
		"review_note":            it.ManualNote,
		"reviewed_by":            it.ReviewedBy,
		"reviewed_at":            reviewedAt,
		"profit_calc_status":     it.ProfitCalcStatus,
		"latest_calc_id":         it.LatestCalcID,
		"profit_margin":          it.ProfitMargin,
		"estimated_profit":       it.EstimatedProfit,
		"profit_output":          profitOutput,
		"active_profit_calc":     activeProfit,
		"stat_month":             latestStatMonth,
		"monthly_metrics":        metrics,
		"data_revision":          1,
		"review_revision":        1,
		"created_at":             it.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":             it.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	var source Object
	if len(it.SourceData) > 0 && json.Unmarshal(it.SourceData, &source) == nil {
		mutable := map[string]bool{"id": true, "created_at": true, "updated_at": true, "review_status": true, "manual_status": true, "review_note": true, "reviewed_by": true, "reviewed_at": true, "decision": true, "machine_result": true, "reasons": true, "parent_asin": true, "is_parent": true, "child_count": true, "family_count": true, "active_profit_calc": true, "profit_output": true, "profit_calc_status": true, "latest_calc_id": true, "profit_margin": true, "estimated_profit": true, "data_revision": true, "review_revision": true}
		for key, value := range source {
			if key != "source_monthly_history" && key != "source_family_rank" && !mutable[key] {
				out[key] = value
			}
		}
	}
	var profitSource Object
	if json.Unmarshal(it.ProfitSourceData, &profitSource) == nil {
		out["profit_source"] = profitSource["profit_source"]
	}

	if out["price_usd"] == nil {
		if specs, ok := source["specs"].(map[string]any); ok {
			if legacy, ok := specs["legacy_site_snapshot"].(map[string]any); ok {
				if original, ok := legacy["p"].(map[string]any); ok {
					if price := parseFloatAny(original["price"]); price > 0 {
						out["price_usd"] = fmt.Sprintf("%.2f", price)
						out["price_source"] = "legacy_import_snapshot"
					}
				}
			}
		}
	}
	return out
}

func gormMonthlyToObject(m model.ScreeningItemMonthlyMetric) Object {
	return Object{
		"id":                  strconv.FormatUint(m.ID, 10),
		"product_id":          strconv.FormatUint(m.ItemID, 10),
		"asin":                m.ASIN,
		"parent_asin":         nullableString(m.ParentASIN),
		"stat_month":          m.StatMonth,
		"price_usd":           fmt.Sprintf("%.2f", m.SellingPrice),
		"selling_price":       m.SellingPrice,
		"currency":            m.Currency,
		"monthly_sales":       m.MonthlySales,
		"display_child_sales": m.ChildSales,
		"bsr_rank":            m.BSRRank,
		"source_task_id":      m.SourceTaskID,
		"created_at":          m.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":          m.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func gormProfitToObject(c model.ProfitCalculation) Object {
	out := Object{
		"id":                strconv.FormatUint(c.ID, 10),
		"product_id":        strconv.FormatUint(c.ItemID, 10),
		"asin":              c.ASIN,
		"version":           c.Version,
		"is_active":         c.IsActive,
		"ref_stat_month":    c.RefStatMonth,
		"selling_price":     c.SellingPrice,
		"purchase_cost":     c.PurchaseCost,
		"first_leg_freight": c.FirstLegFreight,
		"referral_fee":      c.ReferralFee,
		"fulfillment_fee":   c.FulfillmentFee,
		"ads_and_other":     c.AdsAndOtherCost,
		"profit_margin":     c.ProfitMargin,
		"estimated_profit":  c.EstimatedProfit,
		"currency":          c.Currency,
		"calculated_by":     c.CalculatedBy,
		"stale":             !c.IsActive,
		"output": Object{
			"state":      "complete",
			"profit_usd": fmt.Sprintf("%.2f", c.EstimatedProfit),
			"margin":     fmt.Sprintf("%.4f", c.ProfitMargin),
		},
		"created_at": c.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at": c.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
	var source Object
	if json.Unmarshal(c.SourceData, &source) == nil {
		for key, value := range source {
			out[key] = value
		}
	}
	out["id"] = strconv.FormatUint(c.ID, 10)
	out["product_id"] = strconv.FormatUint(c.ItemID, 10)
	out["created_at"] = c.CreatedAt.UTC().Format(time.RFC3339Nano)
	out["revision"] = c.Version
	return out
}

func buildGORMScreeningFilter(params url.Values) gormrepo.ScreeningFilter {
	page, pageSize := queryPage(params)
	f := gormrepo.ScreeningFilter{
		Keyword:          params.Get("q"),
		BrandCategory:    params.Get("brand_category"),
		MachineResult:    mapLegacyToGORMMachine(params.Get("machine")),
		ManualStatus:     mapLegacyToGORMManual(params.Get("review_status")),
		ProfitCalcStatus: params.Get("profit_calc_status"),
		ParentOnly:       params.Get("show_merged") != "true",
		ParentASIN:       params.Get("parent_asin"),
		StatMonth:        normalizeQueryMonth(params.Get("stat_month")),
		RecentMonths:     12,
		Page:             page,
		PageSize:         pageSize,
	}
	if v := params.Get("min_price"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			f.MinPrice = &n
		}
	}
	if v := params.Get("max_price"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			f.MaxPrice = &n
		}
	}
	if v := params.Get("child_sales_min"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			if f.StatMonth != "" {
				f.MinMonthlySales = &n
			} else {
				f.MinSales = &n
			}
		}
	}
	if v := params.Get("min_sales"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.MinSales = &n
		}
	}
	if v := params.Get("max_sales"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			f.MaxSales = &n
		}
	}
	sortParam := params.Get("sort")
	if sortParam == "" {
		f.SortBy = "updated_at"
		f.SortDesc = true
	} else {
		if strings.HasPrefix(sortParam, "-") {
			f.SortDesc = true
			sortParam = strings.TrimPrefix(sortParam, "-")
		}
		sortMap := map[string]string{
			"price_usd":     "latest_price",
			"latest_price":  "latest_price",
			"monthly_sales": "latest_sales",
			"latest_sales":  "latest_sales",
			"rating":        "rating",
			"review_count":  "review_count",
			"updated_at":    "updated_at",
			"created_at":    "created_at",
			"asin":          "asin",
			"profit_usd":    "estimated_profit",
			"margin":        "profit_margin",
		}
		if mapped, ok := sortMap[sortParam]; ok {
			f.SortBy = mapped
		} else {
			f.SortBy = "updated_at"
		}
	}
	return f
}

func normalizeQueryMonth(raw string) string {
	s := strings.TrimSpace(raw)
	if len(s) >= 7 {
		return s[:7]
	}
	return s
}

func itemFamilyKey(id uint64, parentASIN string) string {
	if p := strings.TrimSpace(parentASIN); p != "" {
		return "parent:" + p
	}
	return "product:" + strconv.FormatUint(id, 10)
}

func (s *service) gormProductsUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	filter := buildGORMScreeningFilter(params)

	// 展开全部变体或按指定父体查询时，直接走仓储分页
	if !filter.ParentOnly || filter.ParentASIN != "" {
		filter.ParentOnly = false
		res, err := s.gormStore.Screening().ListWithDetails(ctx, wsID, filter)
		if err != nil {
			return nil, err
		}
		items := make([]Object, 0, len(res.Items))
		for _, d := range res.Items {
			items = append(items, gormItemToObject(d))
		}
		if err := s.gormAttachFamilies(ctx, wsID, items); err != nil {
			return nil, err
		}
		return Object{
			"items":     items,
			"page":      res.Page,
			"page_size": res.PageSize,
			"total":     res.Total,
		}, nil
	}

	// 父体折叠模式：按 (parent_asin || id) 聚合所有父子体家族（与 GetSummary 口径 100% 一致）
	gdb := s.gormStore.DB().WithContext(ctx)
	var light []model.ScreeningItem
	if err := gdb.Model(&model.ScreeningItem{}).
		Select("id", "asin", "parent_asin", "latest_price", "latest_sales", "child_sales", "rating", "review_count", "profit_margin", "estimated_profit", "created_at", "updated_at").
		Scopes(
			gormrepo.WithWorkspace(wsID),
			gormrepo.WithKeyword(filter.Keyword),
			gormrepo.WithBrandCategory(filter.BrandCategory),
			gormrepo.WithMachineResult(filter.MachineResult),
			gormrepo.WithManualStatus(filter.ManualStatus),
			gormrepo.WithProfitCalcStatus(filter.ProfitCalcStatus),
			gormrepo.WithPriceRange(filter.MinPrice, filter.MaxPrice),
			gormrepo.WithSalesRange(filter.MinSales, filter.MaxSales),
			gormrepo.WithMonthlyMetricFilter(wsID, filter.StatMonth, filter.MinMonthlySales, filter.MaxMonthlySales, filter.MinMonthlyPrice, filter.MaxMonthlyPrice),
		).
		Find(&light).Error; err != nil {
		return nil, err
	}

	type familyGroup struct {
		rep       model.ScreeningItem
		memberIDs []string
	}
	groupsMap := make(map[string]*familyGroup, len(light))
	for _, it := range light {
		fk := itemFamilyKey(it.ID, it.ParentASIN)
		g := groupsMap[fk]
		idStr := strconv.FormatUint(it.ID, 10)
		if g == nil {
			groupsMap[fk] = &familyGroup{rep: it, memberIDs: []string{idStr}}
			continue
		}
		g.memberIDs = append(g.memberIDs, idStr)
		if isBetterFamilyRep(it, g.rep) {
			g.rep = it
		}
	}

	groups := make([]*familyGroup, 0, len(groupsMap))
	for _, g := range groupsMap {
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool {
		return compareScreeningRep(groups[i].rep, groups[j].rep, filter.SortBy, filter.SortDesc)
	})

	total := int64(len(groups))
	p, n := queryPage(params)
	start := (p - 1) * n
	if start > len(groups) {
		start = len(groups)
	}
	end := start + n
	if end > len(groups) {
		end = len(groups)
	}
	pageGroups := groups[start:end]

	pageIDs := make([]uint64, len(pageGroups))
	idVals := make([]any, len(pageGroups))
	for i, g := range pageGroups {
		pageIDs[i] = g.rep.ID
		idVals[i] = g.rep.ID
	}

	fullByID := make(map[uint64]model.ScreeningItem, len(pageIDs))
	if len(pageIDs) > 0 {
		var fullItems []model.ScreeningItem
		if err := gdb.Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: idVals}).
			Find(&fullItems).Error; err != nil {
			return nil, err
		}
		for _, it := range fullItems {
			fullByID[it.ID] = it
		}
	}

	metricsByItem, err := s.gormStore.MonthlyMetrics().ListByItemIDs(ctx, wsID, pageIDs)
	if err != nil {
		return nil, err
	}
	profitsByItem, err := s.gormStore.ProfitCalcs().GetActiveByItemIDs(ctx, wsID, pageIDs)
	if err != nil {
		return nil, err
	}

	items := make([]Object, 0, len(pageGroups))
	for _, g := range pageGroups {
		full, ok := fullByID[g.rep.ID]
		if !ok {
			continue
		}
		metrics := metricsByItem[full.ID]
		if filter.RecentMonths > 0 && len(metrics) > filter.RecentMonths {
			metrics = metrics[:filter.RecentMonths]
		}
		var activeCalc *model.ProfitCalculation
		if c, ok := profitsByItem[full.ID]; ok {
			cp := c
			activeCalc = &cp
		}
		obj := gormItemToObject(gormrepo.ScreeningItemWithDetails{
			Item:             full,
			MonthlyMetrics:   metrics,
			ActiveProfitCalc: activeCalc,
		})
		obj["family_count"] = len(g.memberIDs)
		obj["family_member_ids"] = g.memberIDs
		items = append(items, obj)
	}
	if err := s.gormAttachFamilies(ctx, wsID, items); err != nil {
		return nil, err
	}

	return Object{
		"items":     items,
		"page":      p,
		"page_size": n,
		"total":     total,
	}, nil
}

func isBetterFamilyRep(cand, cur model.ScreeningItem) bool {
	if cand.LatestSales != cur.LatestSales {
		return cand.LatestSales > cur.LatestSales
	}
	if !cand.UpdatedAt.Equal(cur.UpdatedAt) {
		return cand.UpdatedAt.After(cur.UpdatedAt)
	}
	return cand.ID < cur.ID
}

func compareScreeningRep(a, b model.ScreeningItem, sortBy string, desc bool) bool {
	var less bool
	switch sortBy {
	case "latest_price":
		if a.LatestPrice != b.LatestPrice {
			less = a.LatestPrice < b.LatestPrice
		} else {
			return a.ID < b.ID
		}
	case "latest_sales":
		if a.LatestSales != b.LatestSales {
			less = a.LatestSales < b.LatestSales
		} else {
			return a.ID < b.ID
		}
	case "rating":
		if a.Rating != b.Rating {
			less = a.Rating < b.Rating
		} else {
			return a.ID < b.ID
		}
	case "asin":
		if a.ASIN != b.ASIN {
			less = a.ASIN < b.ASIN
		} else {
			return a.ID < b.ID
		}
	case "estimated_profit":
		av, bv := derefFloat(a.EstimatedProfit), derefFloat(b.EstimatedProfit)
		if av != bv {
			less = av < bv
		} else {
			return a.ID < b.ID
		}
	case "profit_margin":
		av, bv := derefFloat(a.ProfitMargin), derefFloat(b.ProfitMargin)
		if av != bv {
			less = av < bv
		} else {
			return a.ID < b.ID
		}
	default:
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			less = a.UpdatedAt.Before(b.UpdatedAt)
		} else {
			return a.ID < b.ID
		}
	}
	if desc {
		return !less
	}
	return less
}

func derefFloat(p *float64) float64 {
	if p == nil {
		return -1e18
	}
	return *p
}

func (s *service) gormSummaryUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	gdb := s.gormStore.DB().WithContext(ctx)
	f := buildGORMScreeningFilter(params)
	baseScopes := []func(*gorm.DB) *gorm.DB{
		gormrepo.WithWorkspace(wsID),
		gormrepo.WithKeyword(f.Keyword),
		gormrepo.WithBrandCategory(f.BrandCategory),
		gormrepo.WithMachineResult(f.MachineResult),
		gormrepo.WithPriceRange(f.MinPrice, f.MaxPrice),
		gormrepo.WithSalesRange(f.MinSales, f.MaxSales),
		gormrepo.WithMonthlyMetricFilter(wsID, f.StatMonth, f.MinMonthlySales, f.MaxMonthlySales, f.MinMonthlyPrice, f.MaxMonthlyPrice),
	}

	var familyRows []model.ScreeningItem
	if err := gdb.Model(&model.ScreeningItem{}).Scopes(baseScopes...).Select("id", "parent_asin", "manual_status").Find(&familyRows).Error; err != nil {
		return nil, err
	}
	families, pendingFamilies := make(map[string]struct{}, len(familyRows)), make(map[string]struct{}, len(familyRows)/4)
	var pending, kept, excluded int64
	for _, it := range familyRows {
		key := itemFamilyKey(it.ID, it.ParentASIN)
		families[key] = struct{}{}
		switch it.ManualStatus {
		case "pending":
			pending++
			pendingFamilies[key] = struct{}{}
		case "approved":
			kept++
		case "rejected":
			excluded++
		}
	}

	var importTasks []model.ImportTask
	var importRecords int64
	if err := gdb.Model(&model.ImportTask{}).Scopes(gormrepo.WithWorkspace(wsID)).Select("total_rows").Find(&importTasks).Error; err == nil {
		for _, t := range importTasks {
			importRecords += int64(t.TotalRows)
		}
	}
	if importRecords == 0 {
		if err := gdb.Model(&model.ImportRecord{}).Scopes(gormrepo.WithWorkspace(wsID)).Count(&importRecords).Error; err != nil {
			return nil, err
		}
	}

	return Object{
		"products":          int64(len(familyRows)),
		"displayed":         int64(len(families)),
		"pending_displayed": int64(len(pendingFamilies)),
		"pending":           pending,
		"kept":              kept,
		"excluded":          excluded,
		"source_rows":       importRecords,
	}, nil
}

func (s *service) gormGetProductUsecase(ctx context.Context, a identity, id string) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	item, err := s.gormStore.Screening().GetByID(ctx, wsID, itemID)
	if err != nil {
		return nil, err
	}
	metricsByItem, err := s.gormStore.MonthlyMetrics().ListByItemIDs(ctx, wsID, []uint64{item.ID})
	if err != nil {
		return nil, err
	}
	profitsByItem, err := s.gormStore.ProfitCalcs().GetActiveByItemIDs(ctx, wsID, []uint64{item.ID})
	if err != nil {
		return nil, err
	}
	var activeCalc *model.ProfitCalculation
	if c, ok := profitsByItem[item.ID]; ok {
		cp := c
		activeCalc = &cp
	}
	return gormItemToObject(gormrepo.ScreeningItemWithDetails{
		Item:             *item,
		MonthlyMetrics:   metricsByItem[item.ID],
		ActiveProfitCalc: activeCalc,
	}), nil
}

func (s *service) gormGetProductFamilyUsecase(ctx context.Context, a identity, id string, params url.Values) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	anchor, err := s.gormStore.Screening().GetByID(ctx, wsID, itemID)
	if err != nil {
		return nil, err
	}
	parentASIN := anchor.ParentASIN
	if parentASIN == "" {
		return Object{"parent_asin": nil, "items": []Object{}, "total": 0, "page": 1, "page_size": 100, "parent_monthly": []Object{}, "known_child_sales_total": nil, "unknown_child_count": 0, "aggregate_label": "仅合计同来源、同周期且明确为子体的销量"}, nil
	}
	filter := buildGORMScreeningFilter(params)
	filter.ParentOnly = false
	filter.ParentASIN = parentASIN
	res, err := s.gormStore.Screening().ListWithDetails(ctx, wsID, filter)
	if err != nil {
		return nil, err
	}
	items := make([]Object, 0, len(res.Items))
	knownTotal := 0
	unknownCount := 0
	for _, d := range res.Items {
		items = append(items, gormItemToObject(d))
		if d.Item.ChildSales != nil {
			knownTotal += *d.Item.ChildSales
		} else {
			unknownCount++
		}
	}
	return Object{
		"parent_asin":             nullableString(parentASIN),
		"items":                   items,
		"total":                   res.Total,
		"page":                    res.Page,
		"page_size":               res.PageSize,
		"parent_monthly":          []Object{},
		"known_child_sales_total": knownTotal,
		"unknown_child_count":     unknownCount,
		"aggregate_label":         "仅合计同来源、同周期且明确为子体的销量",
	}, nil
}

func (s *service) gormGetProductMonthlyUsecase(ctx context.Context, a identity, id string, params url.Values) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	item, err := s.gormStore.Screening().GetByID(ctx, wsID, itemID)
	if err != nil {
		return nil, err
	}
	var source Object
	decoder := json.NewDecoder(bytes.NewReader(item.SourceData))
	decoder.UseNumber()
	if decoder.Decode(&source) == nil {
		if raw, ok := source["source_monthly_history"].([]any); ok {
			p, n := queryPage(params)
			selected := []Object{}
			cutoff := ""
			if params.Get("recent_months") == "12" && len(raw) > 0 {
				first := raw[0].(map[string]any)
				if t, e := time.Parse("2006-01-02", str(first["stat_month"])); e == nil {
					cutoff = t.AddDate(0, -11, 0).Format("2006-01-02")
				}
			}
			for _, v := range raw {
				row := v.(map[string]any)
				month := str(row["stat_month"])
				if cutoff != "" && month < cutoff {
					continue
				}
				if m := params.Get("stat_month"); m != "" && !strings.HasPrefix(month, normalizeQueryMonth(m)) {
					continue
				}
				row = gormArchivedMonthlyToObject(row, id)
				selected = append(selected, row)
			}
			total := len(selected)
			start := (p - 1) * n
			if start > total {
				start = total
			}
			end := start + n
			if end > total {
				end = total
			}
			return Object{"items": selected[start:end], "total": total, "page": p, "page_size": n}, nil
		}
	}

	grouped, err := s.gormStore.MonthlyMetrics().ListByItemIDs(ctx, wsID, []uint64{itemID})
	if err != nil {
		return nil, err
	}
	rows := grouped[itemID]
	if params.Get("recent_months") == "12" && len(rows) > 12 {
		rows = rows[:12]
	}
	p, n := queryPage(params)
	total := len(rows)
	start := (p - 1) * n
	if start > total {
		start = total
	}
	end := start + n
	if end > total {
		end = total
	}
	items := make([]Object, 0, end-start)
	for _, m := range rows[start:end] {
		items = append(items, gormMonthlyToObject(m))
	}
	return Object{"items": items, "total": total, "page": p, "page_size": n}, nil
}

func (s *service) gormListProductHistoryUsecase(ctx context.Context, a identity, id string, params url.Values) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	p, n := queryPage(params)
	logs, total, err := s.gormStore.Screening().ListReviewLogs(ctx, wsID, itemID, p, n)
	if err != nil {
		return nil, err
	}
	items := make([]Object, 0, len(logs))
	for _, l := range logs {
		items = append(items, Object{
			"id":          strconv.FormatUint(l.ID, 10),
			"product_id":  strconv.FormatUint(l.ItemID, 10),
			"asin":        l.ASIN,
			"batch_id":    l.BatchID,
			"action_type": l.ActionType,
			"prev_status": mapGORMToLegacyManual(l.PrevStatus),
			"next_status": mapGORMToLegacyManual(l.NextStatus),
			"note":        l.Note,
			"operator_id": l.OperatorID,
			"created_at":  l.CreatedAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return Object{"items": items, "total": total, "page": p, "page_size": n}, nil
}

func (s *service) gormListMonthsUsecase(ctx context.Context, a identity) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	months, err := s.gormStore.MonthlyMetrics().ListDistinctMonths(ctx, wsID)
	if err != nil {
		return nil, err
	}
	gdb := s.gormStore.DB().WithContext(ctx)
	items := make([]Object, 0, len(months))
	for _, month := range months {
		var count int64
		if err := gdb.Model(&model.ScreeningItemMonthlyMetric{}).
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "stat_month"}, Value: month}).
			Count(&count).Error; err != nil {
			return nil, err
		}
		items = append(items, Object{
			"stat_month":  month,
			"period_type": "calendar_month",
			"source_type": "mysql_gorm",
			"products":    count,
		})
	}
	return Object{"items": items}, nil
}

func (s *service) gormReviewProduct(ctx context.Context, a identity, id string, cmd ReviewProductCommand) (Object, error) {
	if err := cmd.validate(); err != nil {
		return nil, err
	}
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	note := strings.TrimSpace(cmd.Note)
	if note == "" {
		note = strings.TrimSpace(cmd.Reason)
	}
	updated, err := s.gormStore.Screening().BatchManualReview(ctx, wsID, gormrepo.BatchManualReviewRequest{
		ItemIDs:    []uint64{itemID},
		NextStatus: mapLegacyToGORMManual(string(cmd.Status)),
		Note:       note,
		OperatorID: fmt.Sprintf("user-%d", a.User),
		ActionType: "single",
		ReviewedAt: time.Now(),
	})
	if err != nil {
		return nil, err
	}
	if updated == 0 {
		return nil, notFound("资源不存在")
	}
	val, err := s.gormGetProductUsecase(ctx, a, id)
	if err != nil {
		return nil, err
	}
	return val.(Object), nil
}

func (s *service) gormUpdateProductCategory(ctx context.Context, a identity, id string, cmd UpdateCategoryCommand) (Object, error) {
	if err := cmd.validate(); err != nil {
		return nil, err
	}
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	res := s.gormStore.DB().WithContext(ctx).
		Model(&model.ScreeningItem{}).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: itemID}).
		Updates(map[string]any{"category_zh": cmd.Category})
	if res.Error != nil {
		return nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, notFound("资源不存在")
	}
	val, err := s.gormGetProductUsecase(ctx, a, id)
	if err != nil {
		return nil, err
	}
	return val.(Object), nil
}

func (s *service) gormSaveProfit(ctx context.Context, a identity, id string, cmd SaveProfitScenarioCommand) (*SavedProfitScenario, error) {
	if !cmd.HasInputs {
		return nil, invalid("测算输入无效")
	}
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	item, err := s.gormStore.Screening().GetByID(ctx, wsID, itemID)
	if err != nil {
		return nil, err
	}

	if cmd.ExpectedDataRevision != nil && *cmd.ExpectedDataRevision != 1 {
		return nil, conflict("商品数据已变化")
	}
	if err := s.gormValidateMonthlyReference(ctx, wsID, *item, cmd.Monthly); err != nil {
		return nil, err
	}

	in := cmd.Inputs
	itemObj := gormItemToObject(gormrepo.ScreeningItemWithDetails{Item: *item})
	out, calcErr := calculateProductProfitStructured(in, itemObj)
	if calcErr != nil {
		return nil, invalid(calcErr.Error())
	}

	fx := parseFloatAny(in["fx_cny_per_usd"])
	if fx <= 0 {
		fx = 7.2
	}
	sellingPrice := parseFloatAny(in["sale_price_usd"])
	if sellingPrice <= 0 {
		sellingPrice = item.LatestPrice
	}
	purchaseCost := parseFloatAny(in["purchase_cny"]) / fx
	freightCNY := parseFloatAny(in["freight_cny"])
	if freightCNY <= 0 && out.FreightCNY != nil {
		freightCNY = parseFloatAny(*out.FreightCNY)
	}
	freightCost := freightCNY / fx
	var referralFee float64
	if out.CommissionUSD != nil {
		referralFee = parseFloatAny(*out.CommissionUSD)
	}
	fulfillmentFee := parseFloatAny(in["fba_fee_usd"])
	var storageUSD float64
	if out.StorageUSD != nil {
		storageUSD = parseFloatAny(*out.StorageUSD)
	} else {
		storageUSD = parseFloatAny(in["storage_usd"])
	}
	adsAndOther := sellingPrice*parseFloatAny(in["ad_rate"]) + storageUSD + parseFloatAny(in["other_usd"])
	estProfit := parseFloatAny(out.ProfitUSD)
	margin := parseFloatAny(out.Margin)

	calc := &model.ProfitCalculation{
		WorkspaceID:     wsID,
		ItemID:          item.ID,
		ASIN:            item.ASIN,
		RefStatMonth:    normalizeQueryMonth(str(in["storage_month"])),
		SellingPrice:    sellingPrice,
		PurchaseCost:    purchaseCost,
		FirstLegFreight: freightCost,
		ReferralFee:     referralFee,
		FulfillmentFee:  fulfillmentFee,
		AdsAndOtherCost: adsAndOther,
		ProfitMargin:    margin,
		EstimatedProfit: estProfit,
		Currency:        item.Currency,
		CalculatedBy:    fmt.Sprintf("user-%d", a.User),
	}
	calc.SourceData = encoded(Object{"name": in["name"], "inputs": in, "output": out, "formula_version": formulaVersion, "record_kind": "calculated", "stale": false, "product_data_revision": 1, "monthly_id": itemObj["monthly_id"], "monthly_revision": itemObj["monthly_revision"]})

	if err := s.gormStore.ProfitCalcs().SaveActiveCalculation(ctx, wsID, calc); err != nil {
		return nil, err
	}
	return &SavedProfitScenario{
		ID:     strconv.FormatUint(calc.ID, 10),
		Name:   optionalImportString(in["name"]),
		Inputs: in,
		Output: out,
		Stale:  false,
	}, nil
}

func (s *service) gormListImports(ctx context.Context, a identity, p, n int) (*ImportList, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	p, n = normalizeImportPage(p, n)
	tasks, total, err := s.gormStore.Imports().ListTasks(ctx, wsID, p, n)
	if err != nil {
		return nil, err
	}
	items := make([]ImportListRecord, 0, len(tasks))
	for _, t := range tasks {
		items = append(items, gormImportTaskToListRecord(t))
	}
	return &ImportList{Items: items, Page: p, PageSize: n, Total: total}, nil
}

func (s *service) gormGetImport(ctx context.Context, a identity, id string) (Object, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	taskID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	t, err := s.gormStore.Imports().GetTaskByID(ctx, wsID, taskID)
	if err != nil {
		return nil, err
	}
	return gormImportTaskToObject(*t), nil
}

func (s *service) gormImportRows(ctx context.Context, a identity, id string, p, n int) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	taskID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	if _, err := s.gormStore.Imports().GetTaskByID(ctx, wsID, taskID); err != nil {
		return nil, err
	}
	p, n = normalizeImportPage(p, n)
	rows, total, err := s.gormStore.Imports().ListRecords(ctx, wsID, taskID, p, n)
	if err != nil {
		return nil, err
	}
	items := make([]Object, 0, len(rows))
	for _, r := range rows {
		rawObj := Object{}
		_ = json.Unmarshal(r.RawPayload, &rawObj)
		state := "committed"
		if r.Status == "failed" {
			state = "invalid"
		}
		items = append(items, Object{
			"id":            strconv.FormatUint(r.ID, 10),
			"import_id":     strconv.FormatUint(r.TaskID, 10),
			"member_path":   "",
			"sheet_name":    "",
			"row_no":        r.RowNo,
			"asin":          r.ASIN,
			"state":         state,
			"raw_data":      rawObj,
			"error_message": r.ErrorMessage,
		})
	}
	return Object{"items": items, "total": total, "page": p, "page_size": n}, nil
}

func gormImportTaskToListRecord(t model.ImportTask) ImportListRecord {
	state := t.Status
	switch t.Status {
	case "pending":
		state = "uploaded"
	case "running":
		state = "importing"
	}
	committedAt := ""
	if t.FinishedAt != nil {
		committedAt = t.FinishedAt.UTC().Format(time.RFC3339Nano)
	}
	return ImportListRecord{
		ID:          strconv.FormatUint(t.ID, 10),
		FileName:    t.FileName,
		State:       state,
		Marketplace: "US",
		SourceType:  t.SourceType,
		PeriodType:  "calendar_month",
		Counts: Object{
			"total":     t.TotalRows,
			"committed": t.SuccessRows,
			"invalid":   t.FailedRows,
			"skipped":   t.SkippedRows,
			"task_no":   t.TaskNo,
		},
		Mapping:      Object{},
		LegacyFields: Object{"task_no": t.TaskNo, "operator_id": t.OperatorID},
		CreatedAt:    t.CreatedAt.UTC().Format(time.RFC3339Nano),
		CommittedAt:  committedAt,
	}
}

func gormImportTaskToObject(t model.ImportTask) Object {
	rec := gormImportTaskToListRecord(t)
	obj := rec.legacy()
	obj["created_at"] = rec.CreatedAt
	obj["committed_at"] = rec.CommittedAt
	return obj
}

func parseFloatAny(v any) float64 {
	if v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		f, _ := strconv.ParseFloat(strings.TrimSpace(fmt.Sprint(v)), 64)
		return f
	}
}

func (s *service) gormListConfigsUsecase(ctx context.Context, a identity, kind string) (any, error) {
	if _, err := s.resolveWorkspaceID(ctx, a); err != nil {
		return nil, err
	}
	if kind == string(ConfigScreening) {
		cfg := decodeObject(defaultPolicyData)
		now := time.Now().UTC().Format(time.RFC3339Nano)
		return Object{
			"items": []Object{{
				"id":          "default-screening-policy",
				"kind":        kind,
				"version":     1,
				"name":        "通用初筛规则",
				"config":      cfg,
				"params":      cfg,
				"config_hash": shaDigest(defaultPolicyData),
				"active":      true,
				"created_by":  a.User,
				"created_at":  now,
			}},
		}, nil
	}
	return Object{"items": []Object{}}, nil
}

func (s *service) gormTrademarkOptionsUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	f := buildGORMScreeningFilter(params)
	var items []model.ScreeningItem
	if err := s.gormStore.DB().WithContext(ctx).
		Select("brand_category").
		Scopes(
			gormrepo.WithWorkspace(wsID),
			gormrepo.WithKeyword(f.Keyword),
			gormrepo.WithMachineResult(f.MachineResult),
			gormrepo.WithManualStatus(f.ManualStatus),
			gormrepo.WithMonthlyMetricFilter(wsID, f.StatMonth, f.MinMonthlySales, f.MaxMonthlySales, f.MinMonthlyPrice, f.MaxMonthlyPrice),
		).
		Find(&items).Error; err != nil {
		return nil, err
	}
	counts := map[string]int64{}
	for _, it := range items {
		counts[it.BrandCategory]++
	}
	rows := make([]Object, 0, len(counts))
	for label, cnt := range counts {
		rows = append(rows, Object{"label": label, "count": cnt})
	}
	return Object{"items": trademarkFacets(rows)}, nil
}

func splitCategoryPath(raw string) []string {
	replacer := strings.NewReplacer(">", ":", "›", ":")
	raw = replacer.Replace(raw)
	segments := strings.Split(raw, ":")
	out := make([]string, 0, len(segments))
	for _, seg := range segments {
		if s := strings.TrimSpace(seg); s != "" {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{"未分类"}
	}
	return out
}

func (s *service) gormListReviewBatchesUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	if _, err := s.resolveWorkspaceID(ctx, a); err != nil {
		return nil, err
	}
	p, n := queryPage(params)
	return Object{"items": []Object{}, "total": int64(0), "page": p, "page_size": n}, nil
}

func (s *service) gormListParentMonthlyUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	p, n := queryPage(params)
	q := s.gormStore.DB().WithContext(ctx).
		Model(&model.ScreeningItemMonthlyMetric{}).
		Scopes(gormrepo.WithWorkspace(wsID))
	if parentASIN := strings.TrimSpace(params.Get("parent_asin")); parentASIN != "" {
		q = q.Where(clause.Eq{Column: clause.Column{Name: "parent_asin"}, Value: parentASIN})
	}
	if statMonth := normalizeQueryMonth(params.Get("stat_month")); statMonth != "" {
		q = q.Where(clause.Eq{Column: clause.Column{Name: "stat_month"}, Value: statMonth})
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []model.ScreeningItemMonthlyMetric
	if err := q.
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{
			{Column: clause.Column{Name: "stat_month"}, Desc: true},
			{Column: clause.Column{Name: "id"}, Desc: true},
		}}).
		Scopes(gormrepo.WithPagination(p, n)).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]Object, 0, len(rows))
	for _, m := range rows {
		items = append(items, gormMonthlyToObject(m))
	}
	return Object{"items": items, "total": total, "page": p, "page_size": n}, nil
}

func (s *service) gormListProductObservationsUsecase(ctx context.Context, a identity, id string, params url.Values) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	item, err := s.gormStore.Screening().GetByID(ctx, wsID, itemID)
	if err != nil {
		return nil, err
	}
	p, n := queryPage(params)
	q := s.gormStore.DB().WithContext(ctx).
		Model(&model.ImportRecord{}).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "asin"}, Value: item.ASIN})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []model.ImportRecord
	if err := q.
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}, Desc: true}}}).
		Scopes(gormrepo.WithPagination(p, n)).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]Object, 0, len(rows))
	for _, r := range rows {
		rawObj := Object{}
		_ = json.Unmarshal(r.RawPayload, &rawObj)
		items = append(items, Object{
			"id":        strconv.FormatUint(r.ID, 10),
			"import_id": strconv.FormatUint(r.TaskID, 10),
			"row_no":    r.RowNo,
			"asin":      r.ASIN,
			"state":     "committed",
			"raw_data":  rawObj,
		})
	}
	return Object{"items": items, "total": total, "page": p, "page_size": n}, nil
}

func (s *service) gormListProductReferencesUsecase(ctx context.Context, a identity, id string) (any, error) {
	if _, err := s.resolveWorkspaceID(ctx, a); err != nil {
		return nil, err
	}
	return Object{"items": []Object{}}, nil
}

// 保持 sort 包引用稳定（用于后续扩展）
var _ = sort.Strings

// Archived PostgreSQL decimals are JSON numbers. The monthly RPC contract uses
// decimal strings; leave integer counts, nulls, and existing string values intact.
func gormArchivedMonthlyToObject(row Object, productID string) Object {
	out := Object{}
	for key, value := range row {
		out[key] = value
	}
	for _, key := range []string{"price_usd", "monthly_revenue", "rating"} {
		if v := out[key]; v != nil {
			out[key] = str(v)
		}
	}
	out["product_id"] = productID
	return out
}
