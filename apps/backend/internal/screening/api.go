package screening

import (
	"context"
	"encoding/json"
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
	var total int64
	if e := s.db.QueryRow(r.Context(), "SELECT count(*) FROM "+table+" WHERE "+where, args...).Scan(&total); e != nil {
		return nil, e
	}
	order := "created_at DESC,id DESC"
	if table == "ps_import_rows" {
		order = "member_path,sheet_name,row_no,id"
	}
	args = append(args, n, (p-1)*n)
	items, e := s.query(r.Context(), fmt.Sprintf("SELECT to_jsonb(t) FROM %s t WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d", table, where, order, len(args)-1, len(args)), args...)
	return Object{"items": items, "total": total, "page": p, "page_size": n}, e
}
func (s *service) route(w http.ResponseWriter, r *http.Request) (any, error) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/screening/"), "/")
	parts := strings.Split(path, "/")
	a := actor(r)
	ctx := r.Context()
	if len(parts) > 1 && parts[0] != "profit" && parts[0] != "parents" && parts[0] != "groups" && parts[1] != "preview" && !uuidPattern.MatchString(parts[1]) {
		return nil, fail(422, "资源ID无效")
	}
	if r.Method == "POST" && path == "imports" {
		return s.upload(w, r)
	}
	if r.Method == "DELETE" && len(parts) == 2 && parts[0] == "imports" {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 200 {
			return nil, fail(422, "修改请求需要Idempotency-Key")
		}
		a.RequestKey = key
		a.RequestHash = shaDigest([]byte("DELETE " + r.URL.RequestURI()))
		return s.removeImportHistory(ctx, a, parts[1])
	}
	if r.Method == "GET" {
		switch path {
		case "products":
			return s.products(r)
		case "summary":
			return s.summary(r)
		case "imports":
			return s.list(r, "ps_imports", "tenant_id=$1 AND NOT(counts ? 'history_deleted_at')", a.Tenant)
		case "review-batches":
			return s.list(r, "ps_jobs", "tenant_id=$1 AND kind='review'", a.Tenant)
		case "policies", "profit/templates":
			kind := "screening"
			if path == "profit/templates" {
				kind = "profit_template"
			} else if e := s.ensurePolicy(ctx, a); e != nil {
				return nil, e
			}
			items, e := s.query(ctx, "SELECT to_jsonb(c)||jsonb_build_object('params',config) FROM ps_configs c WHERE tenant_id=$1 AND kind=$2 ORDER BY version DESC", a.Tenant, kind)
			return Object{"items": items}, e
		case "parents/monthly":
			where := "tenant_id=$1"
			args := []any{a.Tenant}
			for _, key := range []string{"parent_asin", "stat_month"} {
				if value := r.URL.Query().Get(key); value != "" {
					args = append(args, value)
					where += fmt.Sprintf(" AND %s=$%d", key, len(args))
				}
			}
			return s.list(r, "ps_parent_monthly", where, args...)
		case "months":
			items, e := s.query(ctx, "SELECT jsonb_build_object('stat_month',stat_month,'period_type',period_type,'source_type',source_type,'products',count(DISTINCT product_id)) FROM ps_product_monthly WHERE tenant_id=$1 GROUP BY stat_month,period_type,source_type ORDER BY stat_month DESC", a.Tenant)
			return Object{"items": items}, e
		}
		if len(parts) >= 2 {
			id := parts[1]
			switch parts[0] {
			case "products":
				if _, e := s.product(ctx, a, id); e != nil {
					return nil, e
				}
				if len(parts) == 2 {
					return s.productView(ctx, a, id)
				}
				switch parts[2] {
				case "monthly":
					return s.monthlyList(r, id)
				case "observations":
					return s.list(r, "ps_import_rows", "tenant_id=$1 AND product_id=$2::uuid", a.Tenant, id)
				case "history":
					return s.list(r, "ps_review_events", "tenant_id=$1 AND product_id=$2::uuid", a.Tenant, id)
				case "references":
					items, e := s.query(ctx, "SELECT to_jsonb(p)||jsonb_build_object('direction',machine_direction) FROM ps_products p WHERE tenant_id=$1 AND id<>$2::uuid AND review_status<>'pending' AND machine_direction IS NOT DISTINCT FROM (SELECT machine_direction FROM ps_products WHERE tenant_id=$1 AND id=$2::uuid) ORDER BY reviewed_at DESC,id LIMIT 100", a.Tenant, id)
					return Object{"items": items}, e
				case "family":
					return s.family(r, id)
				case "profit-scenarios":
					return s.profitScenarios(r, id)
				}
			case "imports":
				if len(parts) == 2 {
					return s.one(ctx, "SELECT to_jsonb(i)||jsonb_build_object('latest_job_id',(SELECT j.id FROM ps_jobs j WHERE j.tenant_id=i.tenant_id AND j.kind IN ('parse','import') AND j.payload->>'import_id'=i.id::text ORDER BY j.updated_at DESC,j.id DESC LIMIT 1)) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id)
				}
				if _, e := s.one(ctx, "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id); e != nil {
					return nil, e
				}
				if parts[2] == "preview" {
					return s.list(r, "ps_import_rows", "tenant_id=$1 AND import_id=$2::uuid", a.Tenant, id)
				}
				return s.importDownload(w, r, id, parts[2])
			case "jobs", "screen-runs":
				return s.one(ctx, "SELECT to_jsonb(j)-'payload'-'lease_token'-'request_hash' FROM ps_jobs j WHERE tenant_id=$1 AND id=$2::uuid AND kind<>'request'", a.Tenant, id)
			case "review-batches":
				return s.one(ctx, "SELECT to_jsonb(j)-'lease_token' FROM ps_jobs j WHERE tenant_id=$1 AND id=$2::uuid AND kind='review'", a.Tenant, id)
			case "exports":
				if len(parts) == 3 && parts[2] == "download" {
					return s.exportDownload(w, r, id)
				}
			case "groups":
				if len(parts) == 3 && parts[2] == "products" {
					return s.products(r)
				}
			}
		}
	}
	if r.Method == "POST" || r.Method == "PATCH" || r.Method == "PUT" {
		body, e := readBody(w, r)
		if e != nil {
			return nil, e
		}
		for _, key := range []string{"expected_data_revision", "expected_review_revision", "expected_monthly_revision"} {
			if raw, present := body[key]; present {
				if raw == nil && key == "expected_monthly_revision" {
					continue
				}
				number, ok := raw.(json.Number)
				if !ok {
					return nil, fail(422, "版本必须是非负整数")
				}
				if n, e := strconv.ParseInt(string(number), 10, 64); e != nil || n < 0 {
					return nil, fail(422, "版本必须是非负整数")
				}
			}
		}
		for _, key := range []string{"product_id", "monthly_id", "template_id", "reference_product_id"} {
			if raw := body[key]; raw != nil && !uuidPattern.MatchString(str(raw)) {
				return nil, fail(422, "商品或引用ID无效")
			}
		}
		if path == "profit/calculate" {
			return s.calculateRequest(ctx, a, body)
		}
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 200 {
			return nil, fail(422, "修改请求需要Idempotency-Key")
		}
		a.RequestKey = key
		a.RequestHash = shaDigest(append([]byte(r.Method+" "+r.URL.RequestURI()+"\n"), encoded(body)...))
		switch path {
		case "policies", "profit/templates":
			return s.createConfig(ctx, a, path, body)
		case "screen-runs":
			if e = s.ensurePolicy(ctx, a); e != nil {
				return nil, e
			}
			return s.enqueueScreen(ctx, a, body, "")
		case "review-batches/preview":
			return s.previewBatch(ctx, a, body)
		case "exports":
			return s.queueExport(ctx, a, body)
		case "profit/batches":
			return s.queueProfit(ctx, a, body)
		}
		if len(parts) == 3 {
			switch parts[0] {
			case "products":
				switch parts[2] {
				case "review", "category", "parameters":
					return s.mutateProduct(ctx, a, parts[1], parts[2], body)
				case "profit-scenarios":
					return s.saveProfit(ctx, a, parts[1], body)
				}
			case "imports":
				if parts[2] == "mapping" || parts[2] == "commit" {
					return s.editImport(ctx, a, parts[1], body, parts[2] == "commit")
				}
			case "policies":
				if parts[2] == "activate" || parts[2] == "test" {
					return s.policyAction(ctx, a, parts[1], parts[2], body)
				}
			case "review-batches":
				if parts[2] == "apply" || parts[2] == "undo" {
					return s.applyBatch(ctx, a, parts[1], parts[2] == "undo")
				}
			case "jobs":
				if parts[2] == "retry" {
					return s.retryJob(ctx, a, parts[1])
				}
			}
		}
	}
	return nil, fail(404, "初筛接口不存在")
}
func (s *service) productView(ctx context.Context, a identity, id string) (Object, error) {
	q := newProductQuery(a, nil)
	q.where += " AND p.id=$2::uuid"
	q.args = append(q.args, id)
	return s.one(ctx, "SELECT "+productProjection+" FROM ps_products p "+q.joins+" WHERE "+q.where, q.args...)
}
