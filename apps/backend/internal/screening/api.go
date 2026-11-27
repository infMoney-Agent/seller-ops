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

func buildIdempotencyHash(method, canonicalPath, rawQuery string, body Object) string {
	uri := "/api/v1/screening/" + strings.Trim(canonicalPath, "/")
	rawQuery = strings.TrimPrefix(rawQuery, "?")
	if rawQuery != "" {
		uri += "?" + rawQuery
	}
	if method == "DELETE" {
		return shaDigest([]byte("DELETE " + uri))
	}
	return shaDigest(append([]byte(method+" "+uri+"\n"), encoded(body)...))
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
		if err := s.validResourceID(parts[1]); err != nil {
			return nil, err
		}
		if s.gormStore != nil {
			return s.gormExportDownload(w, r, parts[1])
		}
		return s.exportDownload(w, r, parts[1])
	}
	return nil, notFound("初筛接口不存在")
}

func (s *service) getProductUsecase(ctx context.Context, a identity, id string) (any, error) {
	if s.gormStore != nil {
		return s.gormGetProductUsecase(ctx, a, id)
	}
	if _, e := s.product(ctx, a, id); e != nil {
		return nil, e
	}
	return s.productView(ctx, a, id)
}
func (s *service) getProductFamilyUsecase(ctx context.Context, a identity, id string, query url.Values) (any, error) {
	if s.gormStore != nil {
		return s.gormGetProductFamilyUsecase(ctx, a, id, query)
	}
	if _, e := s.product(ctx, a, id); e != nil {
		return nil, e
	}
	return s.familyUsecase(ctx, a, query, id)
}
func (s *service) getProductMonthlyUsecase(ctx context.Context, a identity, id string, query url.Values) (any, error) {
	if s.gormStore != nil {
		return s.gormGetProductMonthlyUsecase(ctx, a, id, query)
	}
	if _, e := s.product(ctx, a, id); e != nil {
		return nil, e
	}
	return s.monthlyListUsecase(ctx, a, query, id)
}
func (s *service) listProductObservationsUsecase(ctx context.Context, a identity, id string, query url.Values) (any, error) {
	if s.gormStore != nil {
		return s.gormListProductObservationsUsecase(ctx, a, id, query)
	}
	if _, e := s.product(ctx, a, id); e != nil {
		return nil, e
	}
	p, n := queryPage(query)
	return s.listPage(ctx, p, n, "ps_import_rows", "tenant_id=$1 AND product_id=$2::uuid AND import_id IN (SELECT id FROM ps_imports WHERE tenant_id=$1 AND NOT(counts ? 'data_deleted_at'))", a.Tenant, id)
}
func (s *service) getProductObservationSourceUsecase(ctx context.Context, a identity, id, rowID string) (any, error) {
	if _, e := s.product(ctx, a, id); e != nil {
		return nil, e
	}
	return s.observationSource(ctx, a, id, rowID)
}
func (s *service) listProductHistoryUsecase(ctx context.Context, a identity, id string, query url.Values) (any, error) {
	if s.gormStore != nil {
		return s.gormListProductHistoryUsecase(ctx, a, id, query)
	}
	if _, e := s.product(ctx, a, id); e != nil {
		return nil, e
	}
	p, n := queryPage(query)
	return s.listPage(ctx, p, n, "ps_review_events", "tenant_id=$1 AND product_id=$2::uuid", a.Tenant, id)
}
func (s *service) listProductReferencesUsecase(ctx context.Context, a identity, id string) (any, error) {
	if s.gormStore != nil {
		return s.gormListProductReferencesUsecase(ctx, a, id)
	}
	if _, e := s.product(ctx, a, id); e != nil {
		return nil, e
	}
	items, e := s.query(ctx, "SELECT to_jsonb(p)||jsonb_build_object('direction',machine_direction) FROM ps_products p WHERE tenant_id=$1 AND archived_at IS NULL AND id<>$2::uuid AND review_status<>'pending' AND machine_direction IS NOT DISTINCT FROM (SELECT machine_direction FROM ps_products WHERE tenant_id=$1 AND id=$2::uuid) ORDER BY reviewed_at DESC,id LIMIT 100", a.Tenant, id)
	return Object{"items": items}, e
}
func (s *service) listProductProfitScenariosUsecase(ctx context.Context, a identity, id string, query url.Values) (any, error) {
	if s.gormStore != nil {
		return s.gormListProductProfitScenariosUsecase(ctx, a, id, query)
	}
	if _, e := s.product(ctx, a, id); e != nil {
		return nil, e
	}
	return s.profitScenariosUsecase(ctx, a, query, id)
}
func (s *service) listParentMonthlyUsecase(ctx context.Context, a identity, query url.Values) (any, error) {
	if s.gormStore != nil {
		return s.gormListParentMonthlyUsecase(ctx, a, query)
	}
	p, n := queryPage(query)
	where := "tenant_id=$1"
	args := []any{a.Tenant}
	for _, key := range []string{"parent_asin", "stat_month"} {
		if value := query.Get(key); value != "" {
			args = append(args, value)
			where += fmt.Sprintf(" AND %s=$%d", key, len(args))
		}
	}
	return s.listPage(ctx, p, n, "ps_parent_monthly", where, args...)
}
func (s *service) listMonthsUsecase(ctx context.Context, a identity) (any, error) {
	if s.gormStore != nil {
		return s.gormListMonthsUsecase(ctx, a)
	}
	items, e := s.query(ctx, "SELECT jsonb_build_object('stat_month',stat_month,'period_type',period_type,'source_type',source_type,'products',count(DISTINCT product_id)) FROM ps_product_monthly WHERE tenant_id=$1 GROUP BY stat_month,period_type,source_type ORDER BY stat_month DESC", a.Tenant)
	return Object{"items": items}, e
}
func (s *service) listReviewBatchesUsecase(ctx context.Context, a identity, query url.Values) (any, error) {
	if s.gormStore != nil {
		return s.gormListReviewBatchesPersisted(ctx, a, query)
	}
	p, n := queryPage(query)
	return s.listPage(ctx, p, n, "ps_jobs", "tenant_id=$1 AND kind='review'", a.Tenant)
}
func (s *service) getReviewBatchUsecase(ctx context.Context, a identity, id string) (any, error) {
	if s.gormStore != nil {
		return s.gormGetReviewBatchUsecase(ctx, a, id)
	}
	return s.one(ctx, "SELECT to_jsonb(j)-'lease_token' FROM ps_jobs j WHERE tenant_id=$1 AND id=$2::uuid AND kind='review'", a.Tenant, id)
}
func (s *service) listConfigsUsecase(ctx context.Context, a identity, kind string) (any, error) {
	if s.gormStore != nil {
		return s.gormListConfigsPersisted(ctx, a, kind)
	}
	if kind == "screening" {
		if e := s.ensurePolicy(ctx, a); e != nil {
			return nil, e
		}
	}
	items, e := s.query(ctx, "SELECT to_jsonb(c)||jsonb_build_object('params',config) FROM ps_configs c WHERE tenant_id=$1 AND kind=$2 ORDER BY version DESC", a.Tenant, kind)
	return Object{"items": items}, e
}
func (s *service) createScreenRunUsecase(ctx context.Context, a identity, cmd EnqueueScreenCommand) (*JobSubmission, error) {
	if s.gormStore != nil {
		return s.gormEnqueueScreen(ctx, a, cmd, "")
	}
	if e := s.ensurePolicy(ctx, a); e != nil {
		return nil, e
	}
	return s.enqueueScreen(ctx, a, cmd, "")
}
func (s *service) productView(ctx context.Context, a identity, id string) (Object, error) {
	q := newProductQuery(a, nil)
	q.where += " AND p.id=$2::uuid"
	q.args = append(q.args, id)
	return s.one(ctx, "SELECT "+productProjection+"||"+profitSourceProjection+" FROM ps_products p "+q.joins+" WHERE "+q.where, q.args...)
}

func (s *service) validateMutationBody(body Object) error {
	return validateMutationBody(body, s.gormStore != nil)
}

func validateMutationBody(body Object, numericIDs ...bool) error {
	for _, key := range []string{"expected_data_revision", "expected_review_revision", "expected_monthly_revision"} {
		if raw, present := body[key]; present {
			if raw == nil && key == "expected_monthly_revision" {
				continue
			}
			number, ok := raw.(json.Number)
			if !ok {
				return invalid("版本必须是非负整数")
			}
			if n, e := strconv.ParseInt(string(number), 10, 64); e != nil || n < 0 {
				return invalid("版本必须是非负整数")
			}
		}
	}
	for _, key := range []string{"product_id", "monthly_id", "template_id", "reference_product_id"} {
		if raw := body[key]; raw != nil {
			id, ok := raw.(string)
			valid := ok && uuidPattern.MatchString(id)
			if !valid && ok && len(numericIDs) > 0 && numericIDs[0] {
				n, e := strconv.ParseUint(id, 10, 64)
				valid = e == nil && n > 0 && strconv.FormatUint(n, 10) == id
			}
			if !valid {
				return invalid("商品或引用ID无效")
			}
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
