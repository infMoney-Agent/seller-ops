package screening

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
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
	return s.listPage(r.Context(), p, n, table, where, args...)
}
func (s *service) listPage(ctx context.Context, p, n int, table, where string, args ...any) (any, error) {
	p, n = normalizeImportPage(p, n)
	var total int64
	if e := s.db.QueryRow(ctx, "SELECT count(*) FROM "+table+" WHERE "+where, args...).Scan(&total); e != nil {
		return nil, e
	}
	order := "created_at DESC,id DESC"
	if table == "ps_import_rows" {
		order = "member_path,sheet_name,row_no,id"
	}
	args = append(args, n, (p-1)*n)
	items, e := s.query(ctx, fmt.Sprintf("SELECT to_jsonb(t) FROM %s t WHERE %s ORDER BY %s LIMIT $%d OFFSET $%d", table, where, order, len(args)-1, len(args)), args...)
	return Object{"items": items, "total": total, "page": p, "page_size": n}, e
}

type screeningCommand struct {
	Method, Path, RawQuery, Key, OriginalURI string
	Query                                    url.Values
	Body                                     Object
}

func screeningCommandURI(c screeningCommand) string {
	if c.OriginalURI != "" {
		return c.OriginalURI
	}
	uri := "/api/v1/screening/" + c.Path
	if c.RawQuery != "" {
		uri += "?" + c.RawQuery
	}
	return uri
}
func (s *service) route(w http.ResponseWriter, r *http.Request) (any, error) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/screening/"), "/")
	parts := strings.Split(path, "/")
	a := actor(r)
	ctx := r.Context()
	if r.Method == "POST" && path == "imports" {
		return s.upload(w, r)
	}
	if r.Method == "GET" && len(parts) == 3 && parts[0] == "imports" && (parts[2] == "source" || parts[2] == "errors") {
		if err := validImportResourceID(parts[1]); err != nil {
			return nil, err
		}
		if _, err := s.getImport(ctx, a, parts[1]); err != nil {
			return nil, err
		}
		return s.importDownload(w, r, parts[1], parts[2])
	}
	if r.Method == "GET" && len(parts) == 3 && parts[0] == "exports" && parts[2] == "download" {
		if err := validImportResourceID(parts[1]); err != nil {
			return nil, err
		}
		return s.exportDownload(w, r, parts[1])
	}
	var body Object
	if r.Method == "POST" || r.Method == "PATCH" || r.Method == "PUT" {
		var err error
		body, err = readBody(w, r)
		if err != nil {
			return nil, err
		}
	}
	return s.executeScreening(ctx, a, screeningCommand{Method: r.Method, Path: path, RawQuery: r.URL.RawQuery, OriginalURI: r.URL.RequestURI(), Query: r.URL.Query(), Key: r.Header.Get("Idempotency-Key"), Body: body})
}
func (s *service) executeScreening(ctx context.Context, a identity, c screeningCommand) (any, error) {
	p, n := queryPage(c.Query)
	path := strings.Trim(c.Path, "/")
	parts := strings.Split(path, "/")
	if len(parts) > 1 && parts[0] != "profit" && parts[0] != "parents" && parts[0] != "groups" && parts[1] != "preview" && !uuidPattern.MatchString(parts[1]) {
		return nil, fail(422, "资源ID无效")
	}
	if c.Method == "DELETE" && len(parts) == 2 && parts[0] == "imports" {
		key := c.Key
		if key == "" || len(key) > 200 {
			return nil, fail(422, "修改请求需要Idempotency-Key")
		}
		a.RequestKey = key
		a.RequestHash = shaDigest([]byte("DELETE " + screeningCommandURI(c)))
		return s.removeImportHistory(ctx, a, parts[1])
	}
	if c.Method == "GET" {
		switch path {
		case "products":
			return s.productsUsecase(ctx, a, c.Query, "")
		case "profit/products":
			return s.profitProductsUsecase(ctx, a, c.Query)
		case "trademark-options":
			return s.trademarkOptionsUsecase(ctx, a, c.Query)
		case "summary":
			return s.summaryUsecase(ctx, a, c.Query)
		case "imports":
			p, n := queryPage(c.Query)
			result, err := s.listImports(ctx, a, p, n)
			if err != nil {
				return nil, err
			}
			return result.legacy(), nil
		case "review-categories":
			return s.reviewCategoriesUsecase(ctx, a, c.Query)
		case "review-workspace":
			return s.reviewWorkspaceUsecase(ctx, a, c.Query)
		case "review-batches":
			return s.listPage(ctx, p, n, "ps_jobs", "tenant_id=$1 AND kind='review'", a.Tenant)
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
				if value := c.Query.Get(key); value != "" {
					args = append(args, value)
					where += fmt.Sprintf(" AND %s=$%d", key, len(args))
				}
			}
			return s.listPage(ctx, p, n, "ps_parent_monthly", where, args...)
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
					return s.monthlyListUsecase(ctx, a, c.Query, id)
				case "observations":
					if len(parts) == 4 {
						if !uuidPattern.MatchString(parts[3]) {
							return nil, fail(422, "来源行ID无效")
						}
						return s.observationSource(ctx, a, id, parts[3])
					}
					return s.listPage(ctx, p, n, "ps_import_rows", "tenant_id=$1 AND product_id=$2::uuid AND import_id IN (SELECT id FROM ps_imports WHERE tenant_id=$1 AND NOT(counts ? 'data_deleted_at'))", a.Tenant, id)
				case "history":
					return s.listPage(ctx, p, n, "ps_review_events", "tenant_id=$1 AND product_id=$2::uuid", a.Tenant, id)
				case "references":
					items, e := s.query(ctx, "SELECT to_jsonb(p)||jsonb_build_object('direction',machine_direction) FROM ps_products p WHERE tenant_id=$1 AND archived_at IS NULL AND id<>$2::uuid AND review_status<>'pending' AND machine_direction IS NOT DISTINCT FROM (SELECT machine_direction FROM ps_products WHERE tenant_id=$1 AND id=$2::uuid) ORDER BY reviewed_at DESC,id LIMIT 100", a.Tenant, id)
					return Object{"items": items}, e
				case "family":
					return s.familyUsecase(ctx, a, c.Query, id)
				case "profit-scenarios":
					return s.profitScenariosUsecase(ctx, a, c.Query, id)
				}
			case "imports":
				if len(parts) == 3 && parts[2] == "removal-preview" {
					return s.importRemovalPreview(ctx, a, id)
				}
				if len(parts) == 2 {
					return s.getImport(ctx, a, id)
				}
				if parts[2] == "preview" {
					p, n := queryPage(c.Query)
					return s.importRows(ctx, a, id, p, n)
				}
				if _, e := s.one(ctx, "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid AND NOT(counts ? 'data_deleted_at')", a.Tenant, id); e != nil {
					return nil, e
				}
				return nil, fail(404, "文件端点仅支持HTTP")
			case "jobs", "screen-runs":
				return s.getPublicJob(ctx, a, id)
			case "review-batches":
				return s.one(ctx, "SELECT to_jsonb(j)-'lease_token' FROM ps_jobs j WHERE tenant_id=$1 AND id=$2::uuid AND kind='review'", a.Tenant, id)
			case "exports":
				if len(parts) == 3 && parts[2] == "download" {
					return nil, fail(404, "文件端点仅支持HTTP")
				}
			case "groups":
				if len(parts) == 3 && parts[2] == "products" {
					return s.productsUsecase(ctx, a, c.Query, parts[1])
				}
			}
		}
	}
	if c.Method == "POST" || c.Method == "PATCH" || c.Method == "PUT" {
		body := c.Body
		if body == nil {
			body = Object{}
		}
		var e error
		if e := validateMutationBody(body); e != nil {
			return nil, e
		}
		if path == "profit/calculate" {
			return s.calculateRequest(ctx, a, body)
		}
		key := c.Key
		if key == "" || len(key) > 200 {
			return nil, fail(422, "修改请求需要Idempotency-Key")
		}
		a.RequestKey = key
		a.RequestHash = shaDigest(append([]byte(c.Method+" "+screeningCommandURI(c)+"\n"), encoded(body)...))
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
	return s.one(ctx, "SELECT "+productProjection+"||"+profitSourceProjection+" FROM ps_products p "+q.joins+" WHERE "+q.where, q.args...)
}

func validateMutationBody(body Object) error {
	for _, key := range []string{"expected_data_revision", "expected_review_revision", "expected_monthly_revision"} {
		if raw, present := body[key]; present {
			if raw == nil && key == "expected_monthly_revision" {
				continue
			}
			number, ok := raw.(json.Number)
			if !ok {
				return fail(422, "版本必须是非负整数")
			}
			if n, e := strconv.ParseInt(string(number), 10, 64); e != nil || n < 0 {
				return fail(422, "版本必须是非负整数")
			}
		}
	}
	for _, key := range []string{"product_id", "monthly_id", "template_id", "reference_product_id"} {
		if raw := body[key]; raw != nil && !uuidPattern.MatchString(str(raw)) {
			return fail(422, "商品或引用ID无效")
		}
	}
	return nil
}

func queryPage(params url.Values) (int, int) {
	p, _ := strconv.Atoi(params.Get("page"))
	n, _ := strconv.Atoi(params.Get("page_size"))
	return normalizeImportPage(p, n)
}
func cloneQuery(params url.Values) url.Values {
	out := url.Values{}
	for k, v := range params {
		out[k] = append([]string{}, v...)
	}
	return out
}
