package screening

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

func (s *service) selection(ctx context.Context, a identity, b Object) ([]string, error) {
	ids := []string{}
	if raw, ok := b["product_ids"].([]any); ok {
		if len(raw) == 0 || len(raw) > 100000 {
			return nil, fail(422, "商品集合须在1到10万之间")
		}
		for _, v := range raw {
			if !uuidPattern.MatchString(str(v)) {
				return nil, fail(422, "商品ID无效")
			}
			ids = append(ids, str(v))
		}
		var total int
		if e := s.db.QueryRow(ctx, "SELECT count(*) FROM ps_products WHERE tenant_id=$1 AND id=ANY($2::uuid[])", a.Tenant, ids).Scan(&total); e != nil {
			return nil, e
		}
		if total != len(ids) {
			return nil, fail(404, "部分商品不存在或不属于当前工作区")
		}
		sort.Strings(ids)
		return ids, nil
	}
	filters := url.Values{}
	if raw, ok := b["filters"].(map[string]any); ok {
		for k, v := range raw {
			filters.Set(k, str(v))
		}
	}
	if e := validateFilters(filters); e != nil {
		return nil, e
	}
	q := newProductQuery(a, filters)
	rows, e := s.db.Query(ctx, "SELECT p.id::text FROM ps_products p "+filterJoins(q, false)+" WHERE "+q.where+" ORDER BY p.id LIMIT 100001", q.args...)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			return nil, e
		}
		ids = append(ids, id)
	}
	if len(ids) > 100000 {
		return nil, fail(422, "一次最多处理10万商品")
	}
	return ids, rows.Err()
}
func (s *service) queueExport(ctx context.Context, a identity, b Object) (any, error) {
	ids, e := s.selection(ctx, a, b)
	if e != nil {
		return nil, e
	}
	return s.queue(ctx, a, "export", Object{"product_ids": ids})
}
func (s *service) queueProfit(ctx context.Context, a identity, b Object) (any, error) {
	ids, e := s.selection(ctx, a, b)
	if e != nil {
		return nil, e
	}
	input, ok := b["inputs"].(map[string]any)
	if !ok {
		return nil, fail(422, "缺少测算输入")
	}
	if _, e = calculate(input); e != nil {
		return nil, fail(422, e.Error())
	}
	if id := str(b["template_id"]); id != "" {
		if _, e = s.one(ctx, "SELECT to_jsonb(c) FROM ps_configs c WHERE tenant_id=$1 AND id=$2::uuid AND kind='profit_template'", a.Tenant, id); e != nil {
			return nil, e
		}
	}
	values := url.Values{}
	if filters, ok := b["filters"].(map[string]any); ok {
		for _, key := range []string{"stat_month", "period_type", "source_type"} {
			if value := str(filters[key]); value != "" {
				values.Set(key, value)
			}
		}
	}
	if e = validateFilters(values); e != nil {
		return nil, e
	}
	q := newProductQuery(a, values)
	q.args = append(q.args, ids)
	targets, e := s.query(ctx, fmt.Sprintf("SELECT jsonb_build_object('id',p.id,'data_revision',p.data_revision,'monthly_id',m.id,'monthly_revision',m.revision,'price_usd',m.price_usd::text,'missing_period',m.id IS NULL AND $%d) FROM ps_products p %s WHERE p.tenant_id=$1 AND p.id=ANY($%d::uuid[]) ORDER BY p.id", len(q.args)+1, strings.TrimSuffix(q.joins, profitJoin), len(q.args)), append(q.args, len(values) > 0)...)
	if e != nil {
		return nil, e
	}
	return s.queue(ctx, a, "profit_batch", Object{"product_ids": ids, "targets": targets, "inputs": input, "template_id": b["template_id"]})
}
func (s *service) queue(ctx context.Context, a identity, kind string, payload Object) (any, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := receipt(ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	result, e := insertJob(ctx, tx, a, kind, payload)
	if e != nil {
		return nil, e
	}
	if e = saveReceipt(ctx, tx, a, result); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}
func excelText(value any) any {
	if value == nil {
		return ""
	}
	if v, ok := value.(string); ok {
		trim := strings.TrimLeft(v, " \t\r\n")
		if trim != "" && strings.ContainsRune("=+-@", rune(trim[0])) {
			return "'" + v
		}
		if len(v) > 32000 {
			v = v[:32000]
			for !utf8.ValidString(v) {
				v = v[:len(v)-1]
			}
		}
		return v
	}
	switch value.(type) {
	case map[string]any, []any:
		return excelText(string(encoded(value)))
	}
	return value
}
func streamSheet(ctx context.Context, tx pgx.Tx, book *excelize.File, name string, headers []string, sql string, args ...any) error {
	if _, e := book.NewSheet(name); e != nil {
		return e
	}
	writer, e := book.NewStreamWriter(name)
	if e != nil {
		return e
	}
	title := make([]any, len(headers))
	for i, v := range headers {
		title[i] = v
	}
	if e = writer.SetRow("A1", title); e != nil {
		return e
	}
	rows, e := tx.Query(ctx, sql, args...)
	if e != nil {
		return e
	}
	defer rows.Close()
	index := 2
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return e
		}
		item := decodeObject(raw)
		values := make([]any, len(headers))
		for i, key := range headers {
			values[i] = excelText(item[key])
		}
		cell, _ := excelize.CoordinatesToCellName(1, index)
		if e = writer.SetRow(cell, values); e != nil {
			return e
		}
		index++
	}
	if e = rows.Err(); e != nil {
		return e
	}
	return writer.Flush()
}
func (s *service) runExport(ctx context.Context, job Object, token string) error {
	payload := job["payload"].(map[string]any)
	ids := []string{}
	for _, raw := range payload["product_ids"].([]any) {
		ids = append(ids, str(raw))
	}
	tenant := integer(job["tenant_id"])
	tx, e := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	book := excelize.NewFile()
	defer book.Close()
	productFields := []string{"asin", "parent_asin", "title", "title_zh", "category", "manual_category", "brand", "seller_id", "seller_name", "seller_region", "material", "package_weight_g", "package_length_cm", "package_width_cm", "package_height_cm", "units_per_pack", "review_status", "review_reason", "review_note", "machine_decision", "machine_recommendation", "machine_direction", "machine_reasons", "data_revision"}
	for _, field := range importedColumns {
		if field.Table == "product" && !containsColumn(productFields, field.Key) {
			productFields = append(productFields, field.Key)
		}
	}
	args := []any{tenant, ids}
	if e = streamSheet(ctx, tx, book, "商品", productFields, "SELECT to_jsonb(p) FROM ps_products p WHERE tenant_id=$1 AND id=ANY($2::uuid[]) ORDER BY asin", args...); e != nil {
		return e
	}
	specs := []struct {
		name    string
		headers []string
		sql     string
	}{
		{"月份数据", []string{"asin", "parent_asin_snapshot", "stat_month", "period_type", "period_start", "period_end", "observed_on", "source_type", "sales_scope", "price_usd", "monthly_sales", "monthly_revenue", "review_count", "rating", "bsr_rank", "revision"}, "SELECT to_jsonb(m)||jsonb_build_object('asin',p.asin) FROM ps_product_monthly m JOIN ps_products p ON p.tenant_id=m.tenant_id AND p.id=m.product_id WHERE m.tenant_id=$1 AND m.product_id=ANY($2::uuid[]) ORDER BY p.asin,m.stat_month DESC,m.id"},
		{"来源行", []string{"product_id", "import_id", "member_path", "sheet_name", "row_no", "raw_data", "normalized_data", "state", "errors"}, "SELECT to_jsonb(r) FROM ps_import_rows r WHERE tenant_id=$1 AND product_id=ANY($2::uuid[]) ORDER BY product_id,import_id,row_no"},
		{"利润方案", []string{"product_id", "name", "inputs", "output", "formula_version", "product_data_revision", "monthly_id", "monthly_revision", "monthly_snapshot", "product_snapshot", "template_snapshot", "created_at"}, "SELECT to_jsonb(q) FROM ps_profit_scenarios q WHERE tenant_id=$1 AND product_id=ANY($2::uuid[]) ORDER BY product_id,created_at,id"},
		{"父体销量", []string{"parent_asin", "stat_month", "period_type", "period_start", "period_end", "source_type", "monthly_sales", "monthly_revenue", "observed_on"}, "SELECT to_jsonb(m) FROM ps_parent_monthly m WHERE tenant_id=$1 AND parent_asin IN (SELECT parent_asin FROM ps_products WHERE tenant_id=$1 AND id=ANY($2::uuid[])) ORDER BY parent_asin,stat_month,id"},
	}
	for _, spec := range specs {
		for _, field := range importedColumns {
			if (spec.name == "月份数据" && field.Table == "monthly") || (spec.name == "利润方案" && field.Table == "profit") {
				spec.headers = append(spec.headers, field.Key)
			}
		}
		if spec.name == "利润方案" {
			spec.headers = append(spec.headers, "record_kind", "source_row_id")
		}
		if e = streamSheet(ctx, tx, book, spec.name, spec.headers, spec.sql, args...); e != nil {
			return e
		}
	}
	book.DeleteSheet("Sheet1")
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	key := fmt.Sprintf("%d/exports/%s.xlsx", tenant, str(job["id"]))
	target := filepath.Join(s.cfg.Storage, key)
	if e = os.MkdirAll(filepath.Dir(target), 0750); e != nil {
		return e
	}
	temp := target + "." + token + ".xlsx"
	defer os.Remove(temp)
	if e = book.SaveAs(temp); e != nil {
		return e
	}
	if e = os.Rename(temp, target); e != nil {
		return e
	}
	return s.finishJob(ctx, job, token, Object{"storage_key": key, "products": len(ids), "snapshot": "repeatable_read"}, "succeeded")
}
func (s *service) runProfitBatch(ctx context.Context, job Object, token string) error {
	payload := job["payload"].(map[string]any)
	a := identity{Tenant: integer(job["tenant_id"]), User: integer(job["created_by"])}
	targets, _ := payload["targets"].([]any)
	if targets == nil {
		for _, raw := range payload["product_ids"].([]any) {
			p, e := s.productView(ctx, a, str(raw))
			if e != nil {
				return e
			}
			targets = append(targets, p)
		}
	}
	items := []Object{}
	success := 0
	for _, raw := range targets {
		target := raw.(map[string]any)
		id := str(target["id"])
		item := Object{"id": id, "success": false}
		if target["missing_period"] == true {
			item["reason"] = "所选统计周期没有商品数据"
			items = append(items, item)
			continue
		}
		input := Object{}
		for k, v := range payload["inputs"].(map[string]any) {
			input[k] = v
		}
		if str(input["sale_price_usd"]) == "" {
			input["sale_price_usd"] = target["price_usd"]
		}
		body := Object{"expected_data_revision": target["data_revision"], "inputs": input, "template_id": payload["template_id"], "monthly_id": target["monthly_id"], "expected_monthly_revision": target["monthly_revision"]}
		a.RequestKey = "profit-job:" + str(job["id"]) + ":" + id
		a.RequestHash = shaDigest(encoded(body))
		out, e := s.saveProfit(ctx, a, id, body)
		if e != nil {
			item["reason"] = "商品或月份已变化，或测算输入无效"
		} else {
			item["success"] = true
			item["scenario"] = out
			success++
		}
		items = append(items, item)
	}
	state := "succeeded"
	if success < len(targets) {
		state = "partial"
		if success == 0 {
			state = "failed"
		}
	}
	return s.finishJob(ctx, job, token, Object{"items": items, "total": len(targets), "success": success, "skipped": len(targets) - success}, state)
}
func (s *service) serveStored(w http.ResponseWriter, r *http.Request, key, name string) error {
	root, e := filepath.Abs(s.cfg.Storage)
	if e != nil {
		return e
	}
	target := filepath.Join(root, filepath.FromSlash(key))
	relative, e := filepath.Rel(root, target)
	if e != nil || strings.HasPrefix(relative, "..") || filepath.IsAbs(relative) {
		return fail(404, "文件不存在")
	}
	file, e := os.Open(target)
	if e != nil {
		return fail(404, "文件不存在")
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil {
		return e
	}
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(name)}))
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, name, info.ModTime(), file)
	return nil
}
func (s *service) importDownload(w http.ResponseWriter, r *http.Request, id, kind string) (any, error) {
	a := actor(r)
	batch, e := s.one(r.Context(), "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id)
	if e != nil {
		return nil, e
	}
	if kind == "source" {
		return nil, s.serveStored(w, r, str(batch["storage_key"]), str(batch["file_name"]))
	}
	if kind != "errors" {
		return nil, fail(404, "下载不存在")
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=import-errors.csv")
	writer := csv.NewWriter(w)
	writer.Write([]string{"文件", "工作表", "原行号", "错误", "原始数据"})
	rows, e := s.db.Query(r.Context(), "SELECT to_jsonb(r) FROM ps_import_rows r WHERE tenant_id=$1 AND import_id=$2::uuid AND state='invalid' ORDER BY member_path,sheet_name,row_no", a.Tenant, id)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	for rows.Next() {
		var raw []byte
		if e = rows.Scan(&raw); e != nil {
			return nil, e
		}
		row := decodeObject(raw)
		writer.Write([]string{str(excelText(row["member_path"])), str(excelText(row["sheet_name"])), str(row["row_no"]), string(encoded(row["errors"])), string(encoded(row["raw_data"]))})
	}
	writer.Flush()
	return nil, rows.Err()
}
func (s *service) exportDownload(w http.ResponseWriter, r *http.Request, id string) (any, error) {
	a := actor(r)
	job, e := s.one(r.Context(), "SELECT to_jsonb(j) FROM ps_jobs j WHERE tenant_id=$1 AND id=$2::uuid AND kind='export'", a.Tenant, id)
	if e != nil {
		return nil, e
	}
	if job["state"] != "succeeded" {
		return nil, fail(409, "导出尚未完成")
	}
	result := job["result"].(map[string]any)
	return nil, s.serveStored(w, r, str(result["storage_key"]), "产品初筛.xlsx")
}
func (s *service) retryJob(ctx context.Context, a identity, id string) (any, error) {
	var kind string
	if e := s.db.QueryRow(ctx, "SELECT kind FROM ps_jobs WHERE tenant_id=$1 AND id=$2::uuid AND payload->>'engine'=$3 AND state='failed'", a.Tenant, id, goEngine).Scan(&kind); e != nil {
		return nil, e
	}
	if _, e := s.db.Exec(ctx, "WITH retried AS (UPDATE ps_jobs SET state='queued',attempts=0,error=NULL,lease_until=NULL,lease_token=NULL,updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND state='failed' RETURNING tenant_id,kind,payload) UPDATE ps_imports i SET state=CASE j.kind WHEN 'parse' THEN 'parsing' ELSE 'importing' END FROM retried j WHERE j.kind IN ('parse','import') AND i.tenant_id=j.tenant_id AND i.id::text=j.payload->>'import_id'", a.Tenant, id); e != nil {
		return nil, e
	}
	return Object{"job_id": id, "state": "queued"}, nil
}
