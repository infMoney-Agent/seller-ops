package screening

import (
	"context"
	"encoding/csv"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/xuri/excelize/v2"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

func (s *service) selection(ctx context.Context, a identity, cmd ProductSelectionCommand) ([]string, error) {
	ids := []string{}
	if cmd.HasProductIDs {
		if len(cmd.ProductIDs) == 0 || len(cmd.ProductIDs) > 100000 {
			return nil, invalid("商品集合须在1到10万之间")
		}
		for _, id := range cmd.ProductIDs {
			if !uuidPattern.MatchString(id) {
				return nil, invalid("商品ID无效")
			}
			ids = append(ids, id)
		}
		var total int
		if e := s.db.QueryRow(ctx, "SELECT count(*) FROM ps_products WHERE tenant_id=$1 AND archived_at IS NULL AND id=ANY($2::uuid[])", a.Tenant, ids).Scan(&total); e != nil {
			return nil, e
		}
		if total != len(ids) {
			return nil, notFound("部分商品不存在或不属于当前工作区")
		}
		sort.Strings(ids)
		return ids, nil
	}
	if e := validateFilters(cmd.Filters); e != nil {
		return nil, e
	}
	q := newProductQuery(a, cmd.Filters)
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
		return nil, invalid("一次最多处理10万商品")
	}
	return ids, rows.Err()
}
func (s *service) queueExport(ctx context.Context, a identity, cmd ProductSelectionCommand) (*JobSubmission, error) {
	if s.gormStore != nil {
		return s.gormQueueExport(ctx, a, cmd)
	}
	ids, e := s.selection(ctx, a, cmd)
	if e != nil {
		return nil, e
	}
	return s.queue(ctx, a, "export", ExportJobPayload{Engine: goEngine, ProductIDs: ids})
}
func (s *service) queueProfit(ctx context.Context, a identity, cmd QueueProfitBatchCommand) (*JobSubmission, error) {
	if s.gormStore != nil {
		return s.gormQueueProfit(ctx, a, cmd)
	}
	ids, e := s.selection(ctx, a, cmd.Selection)
	if e != nil {
		return nil, e
	}
	if !cmd.HasInputs {
		return nil, invalid("缺少测算输入")
	}
	if _, e = calculate(cmd.Inputs); e != nil {
		return nil, invalid(e.Error())
	}
	if tid := stringValue(cmd.TemplateID); tid != "" {
		if _, e = s.one(ctx, "SELECT to_jsonb(c) FROM ps_configs c WHERE tenant_id=$1 AND id=$2::uuid AND kind='profit_template'", a.Tenant, tid); e != nil {
			return nil, e
		}
	}
	if e = validateFilters(cmd.PeriodFilters); e != nil {
		return nil, e
	}
	q := newProductQuery(a, cmd.PeriodFilters)
	q.args = append(q.args, ids)
	rawTargets, e := s.query(ctx, fmt.Sprintf("SELECT jsonb_build_object('id',p.id,'data_revision',p.data_revision,'monthly_id',m.id,'monthly_revision',m.revision,'price_usd',m.price_usd::text,'missing_period',m.id IS NULL AND $%d) FROM ps_products p %s WHERE p.tenant_id=$1 AND p.id=ANY($%d::uuid[]) ORDER BY p.id", len(q.args)+1, strings.TrimSuffix(q.joins, profitJoin), len(q.args)), append(q.args, len(cmd.PeriodFilters) > 0)...)
	if e != nil {
		return nil, e
	}
	targets := make([]ProfitBatchTarget, 0, len(rawTargets))
	for _, row := range rawTargets {
		targets = append(targets, ProfitBatchTarget{
			ID:              str(row["id"]),
			DataRevision:    integer(row["data_revision"]),
			MonthlyID:       optionalImportString(row["monthly_id"]),
			MonthlyRevision: optionalRevision(row, "monthly_revision"),
			PriceUSD:        optionalImportString(row["price_usd"]),
			MissingPeriod:   row["missing_period"] == true,
		})
	}
	return s.queue(ctx, a, "profit_batch", ProfitBatchJobPayload{
		Engine:     goEngine,
		ProductIDs: ids,
		Targets:    targets,
		Inputs:     cmd.Inputs,
		TemplateID: cmd.TemplateID,
	})
}
func (s *service) queue(ctx context.Context, a identity, kind string, payload any) (*JobSubmission, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := typedReceipt[JobSubmission](ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	result, e := insertJobPayload(ctx, tx, a, kind, payload)
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
	payload, e := decodeJobPayload[ExportJobPayload](job)
	if e != nil {
		return e
	}
	ids := payload.ProductIDs
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
		{"来源行", []string{"product_id", "import_id", "member_path", "sheet_name", "row_no", "raw_data", "normalized_data", "state", "errors"}, "SELECT to_jsonb(r) FROM ps_import_rows r WHERE tenant_id=$1 AND product_id=ANY($2::uuid[]) AND import_id IN(SELECT id FROM ps_imports WHERE tenant_id=$1 AND NOT(counts ? 'data_deleted_at')) ORDER BY product_id,import_id,row_no"},
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
	payload, err := decodeJobPayload[ProfitBatchJobPayload](job)
	if err != nil {
		return err
	}
	a := identity{Tenant: integer(job["tenant_id"]), User: integer(job["created_by"])}
	targets := payload.Targets
	if len(targets) == 0 {
		for _, pid := range payload.ProductIDs {
			p, e := s.productView(ctx, a, pid)
			if e != nil {
				return e
			}
			targets = append(targets, ProfitBatchTarget{
				ID:              str(p["id"]),
				DataRevision:    integer(p["data_revision"]),
				MonthlyID:       optionalImportString(p["monthly_id"]),
				MonthlyRevision: optionalRevision(p, "monthly_revision"),
				PriceUSD:        optionalImportString(p["price_usd"]),
			})
		}
	}
	items := []Object{}
	success := 0
	for _, target := range targets {
		id := target.ID
		item := Object{"id": id, "success": false}
		if target.MissingPeriod {
			item["reason"] = "所选统计周期没有商品数据"
			items = append(items, item)
			continue
		}
		input := Object{}
		for k, v := range payload.Inputs {
			input[k] = v
		}
		if str(input["sale_price_usd"]) == "" && target.PriceUSD != nil {
			input["sale_price_usd"] = *target.PriceUSD
		}
		var templateID, monthlyID, monthlyRevision any
		if payload.TemplateID != nil {
			templateID = *payload.TemplateID
		}
		if target.MonthlyID != nil {
			monthlyID = *target.MonthlyID
		}
		if target.MonthlyRevision != nil {
			monthlyRevision = *target.MonthlyRevision
		}
		body := Object{"expected_data_revision": target.DataRevision, "inputs": input, "template_id": templateID, "monthly_id": monthlyID, "expected_monthly_revision": monthlyRevision}
		a.RequestKey = "profit-job:" + str(job["id"]) + ":" + id
		a.RequestHash = shaDigest(encoded(body))
		out, e := s.saveProfit(ctx, a, id, parseSaveProfitCommand(body))
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
		return notFound("文件不存在")
	}
	file, e := os.Open(target)
	if e != nil {
		return notFound("文件不存在")
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
	if s.gormStore != nil {
		return s.gormImportDownload(w, r, id, kind)
	}
	a := actor(r)
	batch, e := s.one(r.Context(), "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id)
	if e != nil {
		return nil, e
	}
	if kind == "source" {
		return nil, s.serveStored(w, r, str(batch["storage_key"]), str(batch["file_name"]))
	}
	if kind != "errors" {
		return nil, notFound("下载不存在")
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
		return nil, conflict("导出尚未完成")
	}
	result := job["result"].(map[string]any)
	return nil, s.serveStored(w, r, str(result["storage_key"]), "产品初筛.xlsx")
}
func (s *service) retryJob(ctx context.Context, a identity, id string) (any, error) {
	if s.gormStore != nil {
		return s.gormRetryJob(ctx, a, id)
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if previous, err := receipt(ctx, tx, a); err != nil || previous != nil {
		return previous, err
	}
	var kind string
	if err = tx.QueryRow(ctx, "SELECT kind FROM ps_jobs WHERE tenant_id=$1 AND id=$2::uuid AND payload->>'engine'=$3 AND state='failed' FOR UPDATE", a.Tenant, id, goEngine).Scan(&kind); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "WITH retried AS (UPDATE ps_jobs SET state='queued',attempts=0,error=NULL,lease_until=NULL,lease_token=NULL,updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND state='failed' RETURNING tenant_id,kind,payload) UPDATE ps_imports i SET state=CASE j.kind WHEN 'parse' THEN 'parsing' ELSE 'importing' END FROM retried j WHERE j.kind IN ('parse','import') AND i.tenant_id=j.tenant_id AND i.id::text=j.payload->>'import_id'", a.Tenant, id); err != nil {
		return nil, err
	}
	out := &JobSubmission{JobID: id, State: "queued"}
	if err = saveReceipt(ctx, tx, a, out); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}
