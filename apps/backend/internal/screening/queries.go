package screening

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

const referenceSQL = "EXISTS(SELECT 1 FROM ps_products ref WHERE ref.tenant_id=p.tenant_id AND ref.archived_at IS NULL AND ref.id<>p.id AND ref.machine_direction=p.machine_direction AND ref.review_status<>'pending')"

// Display-only fallback: retain source values and machine screening semantics.
// Use the observation's variant count, never today's product parameters.
const displayChildSalesSQL = `CASE WHEN m.sales_scope='child' AND m.monthly_sales IS NOT NULL AND m.sales_quantity_kind IS DISTINCT FROM 'single_variant_estimate' THEN m.monthly_sales ELSE
(SELECT COALESCE((src.normalized_data->>'source_monthly_sales')::bigint,(src.normalized_data->>'parent_monthly_sales')::bigint)
 FROM ps_import_rows src
 WHERE src.tenant_id=m.tenant_id AND src.id=m.source_row_id
 AND src.normalized_data->>'variant_count'='1'
 AND COALESCE(src.normalized_data->>'listing_role','child')<>'parent'
 AND NULLIF(m.parent_asin_snapshot,'') IS NOT NULL
 AND NOT EXISTS (
 SELECT 1 FROM ps_products sibling
 JOIN ps_product_monthly sm ON sm.tenant_id=sibling.tenant_id AND sm.product_id=sibling.id
 JOIN ps_import_rows sr ON sr.tenant_id=sm.tenant_id AND sr.id=sm.source_row_id
 WHERE sibling.tenant_id=p.tenant_id AND sibling.marketplace=p.marketplace AND sibling.archived_at IS NULL
 AND sm.parent_asin_snapshot=m.parent_asin_snapshot
 AND sm.period_type=m.period_type AND sm.period_start=m.period_start AND sm.period_end=m.period_end
 AND (sibling.id<>p.id OR (sr.normalized_data->>'variant_count')::numeric>1)
 )) END`

const productProjection = "to_jsonb(p)||jsonb_build_object('display_child_sales'," + displayChildSalesSQL + ",'monthly_id',m.id,'monthly_revision',m.revision,'stat_month',m.stat_month,'period_type',m.period_type,'period_start',m.period_start,'period_end',m.period_end,'source_type',m.source_type,'sales_scope',m.sales_scope,'sales_quantity_kind',m.sales_quantity_kind,'child_sales_raw',m.child_sales_raw,'price_usd',m.price_usd::text,'reported_monthly_sales',m.monthly_sales,'monthly_sales',CASE WHEN m.sales_scope='child' THEN m.monthly_sales END,'monthly_revenue',CASE WHEN m.sales_scope='child' THEN m.monthly_revenue::text END,'review_count',m.review_count,'rating',m.rating::text,'bsr_rank',m.bsr_rank,'decision',p.machine_decision,'recommendation',p.machine_recommendation,'direction',p.machine_direction,'group_key',p.machine_group_key,'reasons',p.machine_reasons,'profit_output',q.output,'profit_data_revision',q.product_data_revision,'profit_monthly_revision',q.monthly_revision,'has_reference'," + referenceSQL + ")"
const parentFamilySQL = "COALESCE('parent:'||p.marketplace||':'||NULLIF(p.parent_asin,''),'product:'||p.id::text)"
const familyRankSQL = "m.stat_month DESC NULLS LAST,m.period_end DESC NULLS LAST,m.period_type,m.source_type," + displayChildSalesSQL + " DESC NULLS LAST,p.updated_at DESC,p.id"
const profitJoin = " LEFT JOIN LATERAL(SELECT output,product_data_revision,monthly_revision FROM ps_profit_scenarios q WHERE q.tenant_id=p.tenant_id AND q.product_id=p.id AND q.record_kind='calculated' AND q.monthly_id IS NOT DISTINCT FROM m.id ORDER BY created_at DESC,id DESC LIMIT 1) q ON true "

type productQuery struct {
	joins, sets, where string
	args               []any
}

func newProductQuery(a identity, values url.Values) productQuery {
	q := productQuery{where: "p.tenant_id=$1 AND p.archived_at IS NULL", args: []any{a.Tenant}}
	add := func(v any) string { q.args = append(q.args, v); return fmt.Sprintf("$%d", len(q.args)) }
	monthly := "m0.tenant_id=p.tenant_id AND m0.product_id=p.id"
	hasPeriod := false
	for _, key := range []string{"stat_month", "period_type", "source_type"} {
		if v := values.Get(key); v != "" {
			hasPeriod = true
			monthly += " AND m0." + key + "=" + add(v)
		}
	}
	if hasPeriod {
		q.where += " AND EXISTS(SELECT 1 FROM ps_product_monthly m0 WHERE " + monthly + ")"
		source := " FROM ps_product_monthly m0 JOIN ps_import_rows r0 ON r0.tenant_id=m0.tenant_id AND r0.id=m0.source_row_id JOIN ps_imports i0 ON i0.tenant_id=r0.tenant_id AND i0.id=r0.import_id WHERE "
		ordering := "m0.stat_month DESC,m0.period_end DESC,m0.observed_on DESC,i0.created_at DESC,r0.member_path DESC,r0.sheet_name DESC,r0.row_no DESC,r0.id DESC"
		q.joins = " LEFT JOIN LATERAL(SELECT m0.*" + source + monthly + " ORDER BY " + ordering + " LIMIT 1) m ON true " + profitJoin
		independent := strings.ReplaceAll(monthly, "m0.tenant_id=p.tenant_id AND m0.product_id=p.id", "m0.tenant_id=$1")
		q.sets = " LEFT JOIN (SELECT DISTINCT ON(m0.product_id) m0.*" + source + independent + " ORDER BY m0.product_id," + ordering + ") m ON m.tenant_id=p.tenant_id AND m.product_id=p.id "
	} else {
		q.sets = " LEFT JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id "
		q.joins = q.sets + profitJoin
	}
	for _, pair := range [][2]string{{"review_status", "p.review_status"}, {"machine", "p.machine_decision"}, {"recommendation", "p.machine_recommendation"}, {"category", "p.category"}, {"seller_region", "p.seller_region"}, {"direction", "p.machine_direction"}} {
		if v := values.Get(pair[0]); v != "" {
			if pair[0] == "machine" && (v == "pending" || v == "unscreened") {
				q.where += " AND p.machine_decision IS NULL"
			} else {
				q.where += " AND " + pair[1] + "=" + add(v)
			}
		}
	}
	if v := values.Get("trademark"); v == "pending" {
		q.where += " AND (" + trademarkClassSQL + "='' OR strpos(" + trademarkClassSQL + ", '待核实')>0)"
	} else if v != "" {
		q.where += " AND strpos(" + trademarkClassSQL + "," + add("第"+v+"类") + ")>0"
	}
	if v := values.Get("q"); v != "" {
		bind := add("%" + v + "%")
		q.where += " AND (p.asin ILIKE " + bind + " OR p.title ILIKE " + bind + ")"
	}
	if v := values.Get("import_id"); v != "" {
		q.where += " AND EXISTS(SELECT 1 FROM ps_import_rows ir WHERE ir.tenant_id=p.tenant_id AND ir.product_id=p.id AND ir.import_id=" + add(v) + "::uuid AND EXISTS(SELECT 1 FROM ps_imports ii WHERE ii.tenant_id=ir.tenant_id AND ii.id=ir.import_id AND NOT(ii.counts ? 'data_deleted_at')))"
	}
	if v := values.Get("has_reference"); v == "true" {
		q.where += " AND " + referenceSQL
	} else if v == "false" {
		q.where += " AND NOT " + referenceSQL
	}
	if v := values.Get("child_sales_min"); v != "" {
		q.where += " AND (" + displayChildSalesSQL + ")>=" + add(v) + "::bigint"
	}
	for _, pair := range [][2]string{{"profit_min", ">="}, {"profit_max", "<="}} {
		if v := values.Get(pair[0]); v != "" {
			q.where += " AND (q.output->>'profit_usd')::numeric " + pair[1] + add(v) + "::numeric"
		}
	}
	return q
}
func (s *service) products(r *http.Request) (any, error) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	groupKey := ""
	if len(parts) >= 3 && parts[len(parts)-3] == "groups" {
		groupKey = parts[len(parts)-2]
	}
	return s.productsUsecase(r.Context(), actor(r), r.URL.Query(), groupKey)
}
func (s *service) productsUsecase(ctx context.Context, a identity, params url.Values, groupKey string) (any, error) {
	if e := validateFilters(params); e != nil {
		return nil, e
	}
	q := newProductQuery(a, params)
	expand := groupKey != ""
	if expand {
		q.args = append(q.args, groupKey)
		q.where += fmt.Sprintf(" AND p.machine_group_key=$%d", len(q.args))
	}
	collapse := !expand && params.Get("show_merged") != "true"
	group := parentFamilySQL
	counter := "count(*)"
	if collapse {
		counter = "count(DISTINCT " + group + ")"
	}
	p, n := queryPage(params)
	var total int64
	if e := s.db.QueryRow(ctx, "SELECT "+counter+" FROM ps_products p "+filterJoins(q, false)+" WHERE "+q.where, q.args...).Scan(&total); e != nil {
		return nil, e
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
	column := map[string]string{"price_usd": "m.price_usd", "monthly_sales": displayChildSalesSQL, "rating": "m.rating", "updated_at": "p.updated_at", "asin": "p.asin", "profit_usd": "(q.output->>'profit_usd')::numeric", "margin": "(q.output->>'margin')::numeric"}[field]
	if column == "" {
		return nil, fail(422, "不支持的排序字段")
	}
	candidateJoins := filterJoins(q, true)
	if field == "profit_usd" || field == "margin" {
		candidateJoins = q.sets + profitJoin
	}
	sql := "WITH candidates AS (SELECT " + column + " AS sort_value,p.id,row_number() OVER(PARTITION BY " + group + " ORDER BY " + familyRankSQL + ") AS group_rank FROM ps_products p " + candidateJoins + " WHERE " + q.where + "), selected AS (SELECT id,sort_value FROM candidates"
	if collapse {
		sql += " WHERE group_rank=1"
	}
	q.args = append(q.args, n, (p-1)*n)
	sql += fmt.Sprintf(" ORDER BY sort_value %s NULLS LAST,id LIMIT $%d OFFSET $%d) SELECT ", direction, len(q.args)-1, len(q.args)) + productProjection + "||jsonb_build_object('family_count',CASE WHEN NULLIF(p.parent_asin,'') IS NULL THEN 1 ELSE (SELECT count(*) FROM ps_products sibling WHERE sibling.tenant_id=p.tenant_id AND sibling.archived_at IS NULL AND sibling.marketplace=p.marketplace AND sibling.parent_asin=p.parent_asin) END,'family_member_ids',CASE WHEN NULLIF(p.parent_asin,'') IS NULL THEN ARRAY[p.id::text] ELSE (SELECT COALESCE(array_agg(sibling.id::text ORDER BY sibling.id),ARRAY[]::text[]) FROM ps_products sibling WHERE sibling.tenant_id=p.tenant_id AND sibling.archived_at IS NULL AND sibling.marketplace=p.marketplace AND sibling.parent_asin=p.parent_asin) END,'family_sales_pending',EXISTS(SELECT 1 FROM ps_products sibling LEFT JOIN ps_product_monthly sm ON sm.tenant_id=sibling.tenant_id AND sm.id=sibling.current_monthly_id WHERE sibling.tenant_id=p.tenant_id AND sibling.marketplace=p.marketplace AND sibling.parent_asin=p.parent_asin AND (sm.sales_scope IS DISTINCT FROM 'child' OR sm.monthly_sales IS NULL)))" + " FROM selected JOIN ps_products p ON p.tenant_id=$1 AND p.id=selected.id " + q.joins + " ORDER BY selected.sort_value " + direction + " NULLS LAST,p.id"
	items, e := s.query(ctx, sql, q.args...)
	// List clients use the public aliases. Keep full machine evidence on detail responses.
	for _, item := range items {
		for _, field := range importedColumns {
			if field.Table == "product" && !containsColumn([]string{"listing_date", "product_length_cm", "product_width_cm", "product_height_cm", "product_dimensions", "product_dimensions_original", "package_dimensions", "package_dimensions_original"}, field.Key) {
				delete(item, field.Key)
			}
		}
		for _, key := range []string{"machine_evidence", "machine_direction", "machine_group_key", "machine_decision", "machine_recommendation", "machine_reasons"} {
			delete(item, key)
		}
	}
	return Object{"items": items, "page": p, "page_size": n, "total": total}, e
}
func (s *service) summary(r *http.Request) (any, error) {
	return s.summaryUsecase(r.Context(), actor(r), r.URL.Query())
}
func (s *service) summaryUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	if e := validateFilters(params); e != nil {
		return nil, e
	}
	q := newProductQuery(a, params)
	return s.one(ctx, "SELECT jsonb_build_object('products',count(*),'displayed',count(DISTINCT COALESCE('parent:'||p.marketplace||':'||NULLIF(p.parent_asin,''),'product:'||p.id::text)),'pending_displayed',count(DISTINCT "+parentFamilySQL+") FILTER(WHERE p.review_status='pending'),'pending',count(*) FILTER(WHERE p.review_status='pending'),'kept',count(*) FILTER(WHERE p.review_status='kept'),'excluded',count(*) FILTER(WHERE p.review_status='excluded'),'source_rows',COALESCE(sum((SELECT count(*) FROM ps_import_rows ir JOIN ps_imports ii ON ii.tenant_id=ir.tenant_id AND ii.id=ir.import_id WHERE ir.tenant_id=p.tenant_id AND ir.product_id=p.id AND NOT(ii.counts ? 'data_deleted_at'))),0)) FROM ps_products p "+filterJoins(q, false)+" WHERE "+q.where, q.args...)
}
func (s *service) monthlyList(r *http.Request, id string) (any, error) {
	return s.monthlyListUsecase(r.Context(), actor(r), r.URL.Query(), id)
}
func (s *service) monthlyListUsecase(ctx context.Context, a identity, params url.Values, id string) (any, error) {
	window := params.Get("recent_months")
	if window != "" && window != "12" {
		return nil, fail(422, "recent_months 只支持 12")
	}
	extra := ""
	if window == "12" {
		extra = " AND stat_month >= (SELECT max(stat_month) - interval '11 months' FROM ps_product_monthly WHERE tenant_id=$1 AND product_id=$2::uuid)"
	}

	p, n := queryPage(params)
	var count int
	if e := s.db.QueryRow(ctx, "SELECT count(*) FROM ps_product_monthly WHERE tenant_id=$1 AND product_id=$2::uuid"+extra, a.Tenant, id).Scan(&count); e != nil {
		return nil, e
	}
	items, e := s.query(ctx, "SELECT to_jsonb(m)||jsonb_build_object('display_child_sales',"+displayChildSalesSQL+",'file_name',i.file_name,'member_path',r.member_path,'sheet_name',r.sheet_name,'row_no',r.row_no,'price_usd',m.price_usd::text) FROM ps_product_monthly m JOIN ps_products p ON p.tenant_id=m.tenant_id AND p.id=m.product_id JOIN ps_import_rows r ON r.tenant_id=m.tenant_id AND r.id=m.source_row_id JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE m.tenant_id=$1 AND m.product_id=$2::uuid"+extra+" ORDER BY m.stat_month DESC,m.period_end DESC,m.observed_on DESC,m.id LIMIT $3 OFFSET $4", a.Tenant, id, n, (p-1)*n)
	return Object{"items": items, "total": count, "page": p, "page_size": n}, e
}
func (s *service) profitScenarios(r *http.Request, id string) (any, error) {
	return s.profitScenariosUsecase(r.Context(), actor(r), r.URL.Query(), id)
}
func (s *service) profitScenariosUsecase(ctx context.Context, a identity, params url.Values, id string) (any, error) {
	p, n := queryPage(params)
	var count int
	if e := s.db.QueryRow(ctx, "SELECT count(*) FROM ps_profit_scenarios WHERE tenant_id=$1 AND product_id=$2::uuid", a.Tenant, id).Scan(&count); e != nil {
		return nil, e
	}
	items, e := s.query(ctx, "SELECT to_jsonb(q)||jsonb_build_object('stale',q.record_kind='calculated' AND (q.product_data_revision<>p.data_revision OR q.monthly_revision IS DISTINCT FROM m.revision OR EXISTS(SELECT 1 FROM ps_configs newer JOIN ps_configs old ON old.tenant_id=newer.tenant_id AND old.id=q.template_id WHERE newer.tenant_id=q.tenant_id AND newer.kind='profit_template' AND newer.name=old.name AND newer.version>old.version))) FROM ps_profit_scenarios q JOIN ps_products p ON p.tenant_id=q.tenant_id AND p.id=q.product_id LEFT JOIN ps_product_monthly m ON m.tenant_id=q.tenant_id AND m.id=q.monthly_id WHERE q.tenant_id=$1 AND q.product_id=$2::uuid ORDER BY q.created_at DESC,q.id LIMIT $3 OFFSET $4", a.Tenant, id, n, (p-1)*n)
	return Object{"items": items, "total": count, "page": p, "page_size": n}, e
}
func (s *service) family(r *http.Request, id string) (any, error) {
	return s.familyUsecase(r.Context(), actor(r), r.URL.Query(), id)
}
func (s *service) familyUsecase(ctx context.Context, a identity, params url.Values, id string) (any, error) {
	p, e := s.product(ctx, a, id)
	if e != nil {
		return nil, e
	}
	parent := str(p["parent_asin"])
	out := Object{"parent_asin": nullableString(parent), "items": []Object{}, "total": 0, "page": 1, "page_size": 100, "parent_monthly": []Object{}, "known_child_sales_total": nil, "unknown_child_count": 0, "aggregate_label": "仅合计同来源、同周期且明确为子体的销量"}
	if parent == "" {
		return out, nil
	}
	q := newProductQuery(a, params)
	q.args = append(q.args, parent)
	q.where += fmt.Sprintf(" AND p.parent_asin=$%d", len(q.args))
	q.args = append(q.args, p["marketplace"])
	q.where += fmt.Sprintf(" AND p.marketplace=$%d", len(q.args))
	pageIndex, pageSize := queryPage(params)
	var total int64
	if e = s.db.QueryRow(ctx, "SELECT count(*) FROM ps_products p "+filterJoins(q, false)+" WHERE "+q.where, q.args...).Scan(&total); e != nil {
		return nil, e
	}
	q.args = append(q.args, pageSize, (pageIndex-1)*pageSize)
	items, e := s.query(ctx, "SELECT "+productProjection+" FROM ps_products p "+q.joins+" WHERE "+q.where+fmt.Sprintf(" ORDER BY "+familyRankSQL+" LIMIT $%d OFFSET $%d", len(q.args)-1, len(q.args)), q.args...)
	if e != nil {
		return nil, e
	}
	parents, e := s.query(ctx, "SELECT to_jsonb(m) FROM ps_parent_monthly m WHERE tenant_id=$1 AND parent_asin=$2 ORDER BY stat_month DESC,id LIMIT 100", a.Tenant, parent)
	if e != nil {
		return nil, e
	}
	out["items"] = items
	out["total"] = total
	out["page"] = pageIndex
	out["page_size"] = pageSize
	out["parent_monthly"] = parents
	anchor, e := s.productView(ctx, a, id)
	if e != nil {
		return nil, e
	}
	if str(anchor["monthly_id"]) != "" {
		totals, e := s.one(ctx, "SELECT jsonb_build_object('known_child_sales_total',sum(m.monthly_sales) FILTER(WHERE m.sales_scope='child'),'unknown_child_count',count(*) FILTER(WHERE m.sales_scope<>'child')) FROM ps_products p JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.product_id=p.id AND m.parent_asin_snapshot=p.parent_asin WHERE p.tenant_id=$1 AND p.parent_asin=$2 AND m.source_type=$3 AND m.period_type=$4 AND m.period_start=$5::date AND m.period_end=$6::date", a.Tenant, parent, anchor["source_type"], anchor["period_type"], anchor["period_start"], anchor["period_end"])
		if e != nil {
			return nil, e
		}
		for k, v := range totals {
			out[k] = v
		}
	}
	return out, nil
}

func filterJoins(q productQuery, monthly bool) string {
	if strings.Contains(q.where, "q.output") {
		return q.sets + profitJoin
	}
	if monthly || strings.Contains(q.where, "m.sales_scope") {
		return q.sets
	}
	return ""
}

func (s *service) observationSource(ctx context.Context, a identity, productID, rowID string) (Object, error) {
	return s.one(ctx, "SELECT jsonb_build_object('id',r.id,'import_id',i.id,'file_name',i.file_name,'member_path',r.member_path,'sheet_name',r.sheet_name,'row_no',r.row_no,'raw_data',r.raw_data) FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id JOIN ps_products p ON p.tenant_id=r.tenant_id AND p.id=r.product_id WHERE r.tenant_id=$1 AND r.product_id=$2::uuid AND r.id=$3::uuid AND p.archived_at IS NULL AND NOT(i.counts ? 'data_deleted_at')", a.Tenant, productID, rowID)
}
