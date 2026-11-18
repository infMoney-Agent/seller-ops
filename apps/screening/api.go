package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func page(r *http.Request) (int, int) {
	p, _ := strconv.Atoi(r.URL.Query().Get("page"))
	n, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	if p < 1 {
		p = 1
	}
	if n < 1 {
		n = 50
	}
	if n > 100 {
		n = 100
	}
	return p, n
}
func (s *service) list(r *http.Request, table, where string, args ...any) (any, error) {
	p, n := page(r)
	ctx := r.Context()
	var total int64
	if e := s.db.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE "+where, args...).Scan(&total); e != nil {
		return nil, e
	}
	order := "created_at DESC,id DESC"
	if table == "ps_import_rows" {
		order = "row_no,id"
	}
	args = append(args, n, (p-1)*n)
	items, e := s.query(ctx, fmt.Sprintf("SELECT to_jsonb(t) FROM %s t WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d", table, where, order, len(args)-1, len(args)), args...)
	return Object{"items": items, "total": total, "page": p, "page_size": n}, e
}
func (s *service) route(w http.ResponseWriter, r *http.Request) (any, error) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/screening/"), "/")
	parts := strings.Split(path, "/")
	a := actor(r)
	ctx := r.Context()
	if r.Method == "GET" {
		switch path {
		case "products":
			return s.products(r)
		case "summary":
			return s.one(ctx, "SELECT jsonb_build_object('products',count(*),'unique_products',count(*),'displayed',count(*),'pending',count(*) FILTER(WHERE review_status='pending'),'kept',count(*) FILTER(WHERE review_status='kept'),'excluded',count(*) FILTER(WHERE review_status='excluded'),'unsure',count(*) FILTER(WHERE review_status='unsure')) FROM ps_products WHERE tenant_id=$1", a.Tenant)
		case "imports":
			return s.list(r, "ps_imports", "tenant_id=$1", a.Tenant)
		case "review-batches":
			return s.list(r, "ps_jobs", "tenant_id=$1 AND kind='review'", a.Tenant)
		case "policies", "profit/templates":
			kind := "screening"
			if path == "profit/templates" {
				kind = "profit_template"
			}
			items, e := s.query(ctx, "SELECT to_jsonb(c)||jsonb_build_object('params',config) FROM ps_configs c WHERE tenant_id=$1 AND kind=$2 ORDER BY version DESC", a.Tenant, kind)
			return Object{"items": items}, e
		case "parents/monthly":
			return s.list(r, "ps_parent_monthly", "tenant_id=$1", a.Tenant)
		case "months":
			items, e := s.query(ctx, "SELECT jsonb_build_object('stat_month',stat_month,'period_type',period_type,'source_type',source_type,'products',count(DISTINCT product_id)) FROM ps_product_monthly WHERE tenant_id=$1 GROUP BY stat_month,period_type,source_type ORDER BY stat_month DESC", a.Tenant)
			return Object{"items": items}, e
		}
		if len(parts) >= 2 {
			id := parts[1]
			switch parts[0] {
			case "products":
				if len(parts) == 2 {
					items, e := s.productView(ctx, a, id)
					return items, e
				}
				switch parts[2] {
				case "monthly":
					return s.list(r, "ps_product_monthly", "tenant_id=$1 AND product_id=$2::uuid", a.Tenant, id)
				case "observations":
					return s.list(r, "ps_import_rows", "tenant_id=$1 AND product_id=$2::uuid", a.Tenant, id)
				case "history":
					return s.list(r, "ps_review_events", "tenant_id=$1 AND product_id=$2::uuid", a.Tenant, id)
				case "references":
					items, e := s.query(ctx, "SELECT to_jsonb(p) FROM ps_products p WHERE tenant_id=$1 AND id<>$2::uuid AND review_status<>'pending' AND machine_direction IS NOT DISTINCT FROM (SELECT machine_direction FROM ps_products WHERE tenant_id=$1 AND id=$2::uuid) ORDER BY reviewed_at DESC,id LIMIT 100", a.Tenant, id)
					return Object{"items": items}, e
				case "family":
					return s.family(r, id)
				case "profit-scenarios":
					return s.list(r, "ps_profit_scenarios", "tenant_id=$1 AND product_id=$2::uuid", a.Tenant, id)
				}
			case "imports":
				if len(parts) == 2 {
					return s.one(ctx, "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id)
				}
				if parts[2] == "preview" {
					return s.list(r, "ps_import_rows", "tenant_id=$1 AND import_id=$2::uuid", a.Tenant, id)
				}
			case "jobs", "screen-runs", "review-batches":
				return s.one(ctx, "SELECT to_jsonb(j) FROM ps_jobs j WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id)
			}
		}
	}
	if r.Method == "POST" || r.Method == "PATCH" || r.Method == "PUT" {
		body, e := readBody(w, r)
		if e != nil {
			return nil, e
		}
		if path == "profit/calculate" {
			return s.calculateRequest(ctx, a, body)
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 200 {
			return nil, fail(422, "修改请求需要有效的Idempotency-Key")
		}
		a.RequestKey = key
		a.RequestHash = shaDigest(append([]byte(r.Method+" "+r.URL.RequestURI()+"\n"), encoded(body)...))
		if path == "screen-runs" {
			return s.enqueueScreen(ctx, a, body, "")
		}
		if path == "policies" || path == "profit/templates" {
			return s.createConfig(ctx, a, path, body)
		}
		if len(parts) == 3 && parts[0] == "products" {
			switch parts[2] {
			case "review", "category", "parameters":
				return s.mutateProduct(ctx, a, parts[1], parts[2], body)
			case "profit-scenarios":
				return s.saveProfit(ctx, a, parts[1], body)
			}
		}
	}
	return nil, fail(404, "Go 初筛接口尚未实现或不存在")
}

const productProjection = "to_jsonb(p)||jsonb_build_object('monthly_id',m.id,'monthly_revision',m.revision,'stat_month',m.stat_month,'period_type',m.period_type,'period_start',m.period_start,'period_end',m.period_end,'source_type',m.source_type,'sales_scope',m.sales_scope,'price_usd',m.price_usd::text,'reported_monthly_sales',m.monthly_sales,'monthly_sales',CASE WHEN m.sales_scope='child' THEN m.monthly_sales END,'monthly_revenue',CASE WHEN m.sales_scope='child' THEN m.monthly_revenue::text END,'review_count',m.review_count,'rating',m.rating::text,'bsr_rank',m.bsr_rank,'decision',p.machine_decision,'recommendation',p.machine_recommendation,'direction',p.machine_direction,'group_key',p.machine_group_key,'reasons',p.machine_reasons,'profit_output',q.output,'profit_data_revision',q.product_data_revision,'has_reference',EXISTS(SELECT 1 FROM ps_products ref WHERE ref.tenant_id=p.tenant_id AND ref.id<>p.id AND ref.machine_direction=p.machine_direction AND ref.review_status<>'pending'))"
const productJoins = " LEFT JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id LEFT JOIN LATERAL(SELECT output,product_data_revision FROM ps_profit_scenarios q WHERE q.tenant_id=p.tenant_id AND q.product_id=p.id ORDER BY created_at DESC,id DESC LIMIT 1) q ON true "

func (s *service) productView(ctx context.Context, a identity, id string) (Object, error) {
	return s.one(ctx, "SELECT "+productProjection+" FROM ps_products p "+productJoins+" WHERE p.tenant_id=$1 AND p.id=$2::uuid", a.Tenant, id)
}
func (s *service) products(r *http.Request) (any, error) {
	a := actor(r)
	p, n := page(r)
	args := []any{a.Tenant}
	where := "p.tenant_id=$1"
	q := r.URL.Query()
	for key, column := range map[string]string{"review_status": "p.review_status", "decision": "p.machine_decision", "recommendation": "p.machine_recommendation", "category": "p.category", "seller_region": "p.seller_region", "direction": "p.machine_direction"} {
		if value := q.Get(key); value != "" {
			args = append(args, value)
			where += fmt.Sprintf(" AND %s=$%d", column, len(args))
		}
	}
	if search := q.Get("search"); search != "" {
		args = append(args, "%"+search+"%")
		where += fmt.Sprintf(" AND (p.asin ILIKE $%d OR p.title ILIKE $%d)", len(args), len(args))
	}
	var total int64
	if e := s.db.QueryRow(r.Context(), "SELECT count(*) FROM ps_products p WHERE "+where, args...).Scan(&total); e != nil {
		return nil, e
	}
	args = append(args, n, (p-1)*n)
	items, e := s.query(r.Context(), fmt.Sprintf("SELECT %s FROM ps_products p %s WHERE %s ORDER BY p.updated_at DESC,p.id LIMIT $%d OFFSET $%d", productProjection, productJoins, where, len(args)-1, len(args)), args...)
	return Object{"items": items, "page": p, "page_size": n, "total": total}, e
}
func (s *service) family(r *http.Request, id string) (any, error) {
	a := actor(r)
	p, e := s.product(r.Context(), a, id)
	if e != nil {
		return nil, e
	}
	parent := str(p["parent_asin"])
	if parent == "" {
		return Object{"parent_asin": nil, "items": []Object{}, "total": 0, "page": 1, "page_size": 100, "parent_monthly": []Object{}, "known_child_sales_total": nil, "unknown_child_count": 0}, nil
	}
	items, e := s.query(r.Context(), "SELECT "+productProjection+" FROM ps_products p "+productJoins+" WHERE p.tenant_id=$1 AND p.marketplace=$2 AND p.parent_asin=$3 ORDER BY p.asin LIMIT 100", a.Tenant, p["marketplace"], parent)
	if e != nil {
		return nil, e
	}
	parents, e := s.query(r.Context(), "SELECT to_jsonb(m) FROM ps_parent_monthly m WHERE tenant_id=$1 AND parent_asin=$2 ORDER BY stat_month DESC LIMIT 100", a.Tenant, parent)
	return Object{"parent_asin": parent, "items": items, "total": len(items), "page": 1, "page_size": 100, "parent_monthly": parents, "known_child_sales_total": nil, "unknown_child_count": 0, "aggregate_label": "仅可比较同来源、同周期且明确为子体的销量"}, e
}
