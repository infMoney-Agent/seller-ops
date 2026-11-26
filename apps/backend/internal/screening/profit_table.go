package screening

import (
	"context"
	"fmt"
	"math/big"
	"net/url"
	"sort"
	"strings"
)

// Parent folding only changes browsing. Each child retains its own cost snapshot.
func (s *service) profitProductsUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	values := cloneQuery(params)
	sortBy := values.Get("sort")
	grouped := values.Get("group_by_parent") == "true"
	if v := values.Get("group_by_parent"); v != "" && v != "true" && v != "false" {
		return nil, invalid("父体分组参数无效")
	}
	family := values.Get("family_product_id")
	values.Del("group_by_parent")
	values.Del("family_product_id")
	completion := values.Get("completion")
	values.Del("completion")
	if completion != "" && completion != "complete" && completion != "incomplete" {
		return nil, invalid("不支持的填写状态")
	}
	focus := values.Get("product_id")
	selected := values.Get("selected_ids")
	values.Del("sort")
	values.Del("product_id")
	values.Del("selected_ids")
	values.Del("show_merged")
	values.Del("review_status")
	if err := validateFilters(values); err != nil {
		return nil, err
	}
	if focus == "" && selected == "" {
		values.Set("review_status", "kept")
	}
	q := newProductQuery(a, values)
	if focus != "" {
		if !uuidPattern.MatchString(focus) {
			return nil, invalid("商品ID无效")
		}
		q.args = append(q.args, focus)
		q.where += fmt.Sprintf(" AND p.id=$%d::uuid", len(q.args))
	}
	if selected != "" {
		ids := strings.Split(selected, ",")
		if len(ids) > 100 {
			return nil, invalid("最多选择100个商品")
		}
		for _, id := range ids {
			if !uuidPattern.MatchString(id) {
				return nil, invalid("商品ID无效")
			}
		}
		q.args = append(q.args, ids)
		q.where += fmt.Sprintf(" AND p.id=ANY($%d::uuid[])", len(q.args))
	}
	if family != "" {
		if !uuidPattern.MatchString(family) {
			return nil, invalid("父体商品ID无效")
		}
		q.args = append(q.args, family)
		q.where += fmt.Sprintf(" AND EXISTS(SELECT 1 FROM ps_products anchor WHERE anchor.tenant_id=p.tenant_id AND anchor.id=$%d::uuid AND anchor.archived_at IS NULL AND anchor.marketplace=p.marketplace AND (anchor.id=p.id OR (NULLIF(anchor.parent_asin,'') IS NOT NULL AND anchor.parent_asin=p.parent_asin)))", len(q.args))
	}
	scenarioJoin := ` LEFT JOIN LATERAL(SELECT s.id,s.name,s.inputs,s.output,s.formula_version,s.product_data_revision,s.monthly_id,s.monthly_revision,s.template_id,s.created_by,s.created_at,s.revision,s.record_kind FROM ps_profit_scenarios s WHERE s.tenant_id=p.tenant_id AND s.product_id=p.id AND s.record_kind='calculated' ORDER BY s.created_at DESC,s.id DESC LIMIT 1)q ON true `
	feeJoin := ` LEFT JOIN LATERAL(SELECT s.imported_fba_fee_usd,s.imported_gross_margin_rate,s.source_row_id FROM ps_profit_scenarios s WHERE s.tenant_id=p.tenant_id AND s.product_id=p.id AND s.record_kind='imported' AND (s.source_row_id=p.current_row_id OR s.monthly_id=m.id) ORDER BY (s.source_row_id=p.current_row_id) DESC,s.created_at DESC,s.id DESC LIMIT 1)fees ON true `
	joins := q.sets + scenarioJoin + feeJoin
	countJoins := q.sets
	if strings.Contains(q.where, "q.") {
		countJoins += scenarioJoin
	}
	var total int64
	if completion == "" && !grouped {
		if err := s.db.QueryRow(ctx, "SELECT count(*) FROM ps_products p "+countJoins+" WHERE "+q.where, q.args...).Scan(&total); err != nil {
			return nil, err
		}
	}

	direction := "ASC"
	if sortBy == "" {
		sortBy = "updated_at"
		direction = "DESC"
	}
	if strings.HasPrefix(sortBy, "-") {
		sortBy = strings.TrimPrefix(sortBy, "-")
		direction = "DESC"
	}
	fields := map[string]string{"updated_at": "p.updated_at", "asin": "p.asin", "price_usd": "coalesce((q.inputs->>'sale_price_usd')::numeric,m.price_usd)", "monthly_sales": displayChildSalesSQL, "fba_fee_usd": "coalesce((q.inputs->>'fba_fee_usd')::numeric,fees.imported_fba_fee_usd)", "storage_usd": "(q.inputs->>'storage_usd')::numeric", "freight_cny": "(q.inputs->>'freight_cny')::numeric", "freight_rate_cny_per_kg": "(q.inputs->>'freight_rate_cny_per_kg')::numeric", "purchase_cny": "(q.inputs->>'purchase_cny')::numeric", "commission_rate": "(q.inputs->>'commission_rate')::numeric", "profit_cny": "CASE WHEN q.output->>'state'='complete' THEN coalesce((q.output->>'profit_cny')::numeric,(q.output->>'profit_usd')::numeric*(q.inputs->>'fx_cny_per_usd')::numeric) END", "margin": "(q.output->>'margin')::numeric", "volumetric_weight_kg": "(q.output->>'volumetric_weight_kg')::numeric", "billable_weight_kg": "(q.output->>'billable_weight_kg')::numeric", "commission_usd": "(q.output->>'commission_usd')::numeric"}
	column := fields[sortBy]
	if column == "" {
		return nil, invalid("不支持的利润排序字段")
	}
	pageNo, size := queryPage(params)
	computedSort := sortBy != "updated_at" && sortBy != "asin" && sortBy != "price_usd" && sortBy != "monthly_sales"
	tail := fmt.Sprintf(" ORDER BY %s %s NULLS LAST,p.id", column, direction)
	familyCounts := map[string]int{}
	var productTotal int64
	if computedSort || completion != "" || grouped {
		// Formula inputs determine the full ordering; fetch heavy details for one page.
		lightProjection := "jsonb_build_object('id',p.id,'marketplace',p.marketplace,'category_path',p.category_path,'category',p.category,'main_category',p.main_category,'package_length_cm',p.package_length_cm::text,'package_width_cm',p.package_width_cm::text,'package_height_cm',p.package_height_cm::text,'package_weight_g',p.package_weight_g::text,'price_usd',m.price_usd::text,'scenario',jsonb_build_object('inputs',q.inputs),'imported_fees',jsonb_build_object('fba_fee_usd',fees.imported_fba_fee_usd::text))||" + profitSourceProjection
		if grouped {
			lightProjection += "||jsonb_build_object('family_key'," + parentFamilySQL + ",'family_rank',row_number() OVER(PARTITION BY " + parentFamilySQL + " ORDER BY " + familyRankSQL + "))"
		}
		candidates, err := s.query(ctx, "SELECT "+lightProjection+" FROM ps_products p "+joins+" WHERE "+q.where, q.args...)
		if err != nil {
			return nil, err
		}
		populateProfitRows(candidates)
		if completion != "" {
			filtered := candidates[:0]
			for _, item := range candidates {
				output, _ := item["preview_output"].(Object)
				if str(output["state"]) == completion {
					filtered = append(filtered, item)
				}
			}
			candidates = filtered
			total = int64(len(candidates))
		}
		if grouped {
			productTotal = int64(len(candidates))
			representatives := map[string]Object{}
			counts := map[string]int{}
			for _, item := range candidates {
				key := str(item["family_key"])
				counts[key]++
				old := representatives[key]
				if old == nil || integer(item["family_rank"]) < integer(old["family_rank"]) {
					representatives[key] = item
				}
			}
			candidates = make([]Object, 0, len(representatives))
			for key, item := range representatives {
				candidates = append(candidates, item)
				familyCounts[str(item["id"])] = counts[key]
			}
			total = int64(len(candidates))
		}
		ids := make([]string, 0, len(candidates))
		if computedSort {
			ids = profitPageIDs(candidates, sortBy, direction, pageNo, size)
		} else {
			for _, item := range candidates {
				ids = append(ids, str(item["id"]))
			}
		}
		if len(ids) == 0 {
			return Object{"items": []Object{}, "total": total, "product_total": productTotal, "page": pageNo, "page_size": size, "referral_categories": referralOptions()}, nil
		}
		q.args = append(q.args, ids)
		position := len(q.args)
		q.where += fmt.Sprintf(" AND p.id=ANY($%d::uuid[])", position)
		if computedSort {
			tail = fmt.Sprintf(" ORDER BY array_position($%d::uuid[],p.id)", position)
		} else {
			q.args = append(q.args, size, (pageNo-1)*size)
			tail += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(q.args)-1, len(q.args))
		}
	} else {
		q.args = append(q.args, size, (pageNo-1)*size)
		tail += fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(q.args)-1, len(q.args))
	}
	// Keep imported raw library snapshots in detail/history endpoints, not each table row.
	catalogProjection := strings.Replace(productProjection, "to_jsonb(p)", "(to_jsonb(p)-'specs')||jsonb_build_object('specs',p.specs-'legacy_site_snapshot')", 1)
	projection := catalogProjection + "||" + profitSourceProjection + `||jsonb_build_object('scenario',CASE WHEN q.id IS NULL THEN NULL ELSE to_jsonb(q) END,'imported_fees',jsonb_build_object('fba_fee_usd',fees.imported_fba_fee_usd::text,'gross_margin_rate',fees.imported_gross_margin_rate::text),'scenario_stale',q.id IS NOT NULL AND (q.product_data_revision<>p.data_revision OR q.monthly_id IS DISTINCT FROM m.id OR q.monthly_revision IS DISTINCT FROM m.revision))`
	items, err := s.query(ctx, "SELECT "+projection+" FROM ps_products p "+joins+" WHERE "+q.where+tail, q.args...)
	if err != nil {
		return nil, err
	}
	populateProfitRows(items)
	if grouped {
		for _, item := range items {
			item["family_count"] = familyCounts[str(item["id"])]
		}
	} else {
		productTotal = total
	}
	return Object{"items": items, "total": total, "product_total": productTotal, "page": pageNo, "page_size": size, "referral_categories": referralOptions()}, nil
}

func populateProfitRows(items []Object) {
	for _, item := range items {
		in := profitRowInputs(item)
		out, err := calculateProductProfit(in, item)
		if err != nil {
			out = Object{"state": "incomplete", "missing": []string{}, "formula_version": formulaVersion}
			item["calculation_error"] = err.Error()
		}
		item["draft_inputs"] = in
		item["preview_output"] = out
		delete(item, "machine_evidence")
		delete(item, "machine_reasons")
	}
}
func profitPageIDs(items []Object, field, direction string, pageNo, size int) []string {
	type ranked struct {
		id    string
		value *big.Rat
	}
	rows := make([]ranked, 0, len(items))
	for _, item := range items {
		rows = append(rows, ranked{str(item["id"]), profitRowSortValue(item, field)})
	}
	sort.Slice(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]
		if a.value == nil || b.value == nil {
			if a.value == nil && b.value == nil {
				return a.id < b.id
			}
			return b.value == nil
		}
		cmp := a.value.Cmp(b.value)
		if cmp == 0 {
			return a.id < b.id
		}
		if direction == "DESC" {
			return cmp > 0
		}
		return cmp < 0
	})
	start := (pageNo - 1) * size
	if start >= len(rows) {
		return []string{}
	}
	end := start + size
	if end > len(rows) {
		end = len(rows)
	}
	ids := make([]string, 0, end-start)
	for _, row := range rows[start:end] {
		ids = append(ids, row.id)
	}
	return ids
}

func profitRowInputs(item Object) Object {
	in := Object{"name": "基准", "fee_source": "", "note": "", "supplier_url": "", "monthly_units": nil, "freight_rate_cny_per_kg": nil, "volumetric_divisor_cm3_per_kg": nil, "commission_mode": nil, "commission_category": nil, "storage_mode": nil, "storage_days": nil, "storage_month": nil}
	for _, key := range profitFields {
		in[key] = nil
	}
	scenario, _ := item["scenario"].(map[string]any)
	saved, _ := scenario["inputs"].(map[string]any)
	for key := range in {
		if value, present := saved[key]; present {
			in[key] = value
		}
	}
	if str(in["sale_price_usd"]) == "" {
		in["sale_price_usd"] = item["price_usd"]
	}
	fees, _ := item["imported_fees"].(map[string]any)
	if str(in["fba_fee_usd"]) == "" {
		in["fba_fee_usd"] = fees["fba_fee_usd"]
	}
	if str(in["supplier_url"]) == "" && validateProfitExtras(Object{"supplier_url": in["note"]}) == nil {
		in["supplier_url"] = str(in["note"])
	}
	for _, key := range append(append([]string{}, profitFields...), freightFields...) {
		if text, ok := in[key].(string); ok && strings.Contains(text, ".") {
			in[key] = strings.TrimRight(strings.TrimRight(text, "0"), ".")
		}
	}
	return in
}
func profitRowSortValue(item Object, key string) *big.Rat {
	input, _ := item["draft_inputs"].(map[string]any)
	output, _ := item["preview_output"].(map[string]any)
	var value any
	switch key {
	case "profit_cny", "margin":
		if output["state"] != "complete" {
			return nil
		}
		value = output[key]
	case "volumetric_weight_kg", "billable_weight_kg", "commission_usd":
		value = output[key]
	default:
		value = input[key]
	}
	n, ok := new(big.Rat).SetString(str(value))
	if !ok {
		return nil
	}
	return n
}
