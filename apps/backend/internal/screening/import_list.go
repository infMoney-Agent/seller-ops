package screening

import (
	"context"
	"encoding/json"
	"fmt"
)

// ImportListRecord is transport-independent; legacy fields preserve the existing
// REST projection while callers migrate to typed metadata.
type ImportListRecord struct {
	ID, FileName, State, Marketplace, SourcePeriod, SourceType, PeriodType, DefaultSalesScope string
	ObservedOn, PeriodStart, PeriodEnd                                                        *string
	Counts, Mapping, LegacyFields                                                             Object
	CreatedAt, CommittedAt                                                                    string
}
type ImportList struct {
	Items          []ImportListRecord
	Page, PageSize int
	Total          int64
}

func optionalImportString(v any) *string {
	if v == nil {
		return nil
	}
	value := str(v)
	return &value
}
func importObject(v any) Object {
	if value, ok := v.(map[string]any); ok {
		return value
	}
	return Object{}
}
func importRecord(row Object) ImportListRecord {
	extra := Object{}
	for key, value := range row {
		extra[key] = value
	}
	for _, key := range []string{"id", "file_name", "state", "marketplace", "source_period", "source_type", "period_type", "default_sales_scope", "observed_on", "period_start", "period_end", "counts", "mapping"} {
		delete(extra, key)
	}
	return ImportListRecord{ID: str(row["id"]), FileName: str(row["file_name"]), State: str(row["state"]), Marketplace: str(row["marketplace"]), SourcePeriod: str(row["source_period"]), SourceType: str(row["source_type"]), PeriodType: str(row["period_type"]), DefaultSalesScope: str(row["default_sales_scope"]), ObservedOn: optionalImportString(row["observed_on"]), PeriodStart: optionalImportString(row["period_start"]), PeriodEnd: optionalImportString(row["period_end"]), Counts: importObject(row["counts"]), Mapping: importObject(row["mapping"]), LegacyFields: extra, CreatedAt: str(row["created_at"]), CommittedAt: str(row["committed_at"])}
}
func normalizeImportPage(p, n int) (int, int) {
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
func (s *service) listImports(ctx context.Context, a identity, p, n int) (*ImportList, error) {
	p, n = normalizeImportPage(p, n)
	var total int64
	const where = "tenant_id=$1 AND NOT(counts ? 'data_deleted_at')"
	if err := s.db.QueryRow(ctx, "SELECT count(*) FROM ps_imports WHERE "+where, a.Tenant).Scan(&total); err != nil {
		return nil, err
	}
	rows, err := s.query(ctx, "SELECT to_jsonb(t) FROM ps_imports t WHERE "+where+" ORDER BY created_at DESC,id DESC LIMIT $2 OFFSET $3", a.Tenant, n, (p-1)*n)
	if err != nil {
		return nil, err
	}
	out := &ImportList{Items: []ImportListRecord{}, Page: p, PageSize: n, Total: total}
	for _, row := range rows {
		out.Items = append(out.Items, importRecord(row))
	}
	return out, nil
}
func (r ImportListRecord) legacy() Object {
	out := Object{}
	for key, value := range r.LegacyFields {
		out[key] = value
	}
	for key, value := range (Object{"id": r.ID, "file_name": r.FileName, "state": r.State, "marketplace": r.Marketplace, "source_period": r.SourcePeriod, "source_type": r.SourceType, "period_type": r.PeriodType, "default_sales_scope": r.DefaultSalesScope, "counts": r.Counts, "mapping": r.Mapping}) {
		out[key] = value
	}
	for key, value := range map[string]*string{"observed_on": r.ObservedOn, "period_start": r.PeriodStart, "period_end": r.PeriodEnd} {
		if value == nil {
			out[key] = nil
		} else {
			out[key] = *value
		}
	}
	return out
}
func (r *ImportList) legacy() Object {
	items := []Object{}
	for _, item := range r.Items {
		items = append(items, item.legacy())
	}
	return Object{"items": items, "page": r.Page, "page_size": r.PageSize, "total": r.Total}
}
func importStructMap(value Object) (map[string]any, error) {
	// pgx returns json.Number; Struct accepts JSON-compatible Go primitives.
	var out map[string]any
	if err := json.Unmarshal(encoded(value), &out); err != nil {
		return nil, fmt.Errorf("invalid import extension: %w", err)
	}
	return out, nil
}

func validImportResourceID(id string) error {
	if !uuidPattern.MatchString(id) {
		return invalid("资源ID无效")
	}
	return nil
}
func (s *service) getImport(ctx context.Context, a identity, id string) (Object, error) {
	if err := validImportResourceID(id); err != nil {
		return nil, err
	}
	return s.one(ctx, "SELECT to_jsonb(i)||jsonb_build_object('latest_job_id',(SELECT j.id FROM ps_jobs j WHERE j.tenant_id=i.tenant_id AND j.kind IN ('parse','import') AND j.payload->>'import_id'=i.id::text ORDER BY j.updated_at DESC,j.id DESC LIMIT 1)) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid AND NOT(counts ? 'data_deleted_at')", a.Tenant, id)
}
func (s *service) importRows(ctx context.Context, a identity, id string, p, n int) (any, error) {
	if err := validImportResourceID(id); err != nil {
		return nil, err
	}
	if _, err := s.one(ctx, "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid AND NOT(counts ? 'data_deleted_at')", a.Tenant, id); err != nil {
		return nil, err
	}
	return s.listPage(ctx, p, n, "ps_import_rows", "tenant_id=$1 AND import_id=$2::uuid", a.Tenant, id)
}
func (s *service) getPublicJob(ctx context.Context, a identity, id string) (Object, error) {
	if err := validImportResourceID(id); err != nil {
		return nil, err
	}
	return s.one(ctx, "SELECT to_jsonb(j)-'payload'-'lease_token'-'request_hash' FROM ps_jobs j WHERE tenant_id=$1 AND id=$2::uuid AND kind<>'request'", a.Tenant, id)
}
