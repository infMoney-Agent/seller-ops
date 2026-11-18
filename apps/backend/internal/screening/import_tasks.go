package screening

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

func newRat(s string) (*big.Rat, bool) { return new(big.Rat).SetString(s) }
func (s *service) runParse(ctx context.Context, job Object, token string) error {
	a := identity{Tenant: integer(job["tenant_id"]), User: integer(job["created_by"])}
	payload := job["payload"].(map[string]any)
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	var raw []byte
	if e = tx.QueryRow(ctx, "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid FOR UPDATE", a.Tenant, payload["import_id"]).Scan(&raw); e != nil {
		return e
	}
	batch := decodeObject(raw)
	if integer(batch["parse_generation"]) != integer(payload["generation"]) {
		return s.finishJob(ctx, job, token, Object{"superseded": true}, "succeeded")
	}
	if str(batch["state"]) != "parsing" && str(batch["state"]) != "failed" {
		return s.finishJob(ctx, job, token, Object{"skipped": true}, "succeeded")
	}
	data, e := os.ReadFile(filepath.Join(s.cfg.Storage, str(batch["storage_key"])))
	if e != nil {
		return e
	}
	mapping, _ := batch["mapping"].(map[string]any)
	if _, e = tx.Exec(ctx, "DELETE FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$2::uuid AND state<>'committed'", a.Tenant, batch["id"]); e != nil {
		return e
	}
	total, valid, invalid := 0, 0, 0
	skipped := []Object{}
	e = parseSourceReport(data, str(batch["file_name"]), mapping, func(row sourceRow) error {
		total++
		state := "valid"
		if len(row.Errors) > 0 {
			state = "invalid"
			invalid++
		} else {
			valid++
		}
		_, e := tx.Exec(ctx, "INSERT INTO ps_import_rows(tenant_id,id,import_id,member_path,sheet_name,row_no,state,raw_data,normalized_data,errors) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", a.Tenant, uuid(), batch["id"], row.Member, row.Sheet, row.Number, state, encoded(row.Raw), nullableJSON(row.Normalized), encoded(row.Errors))
		return e
	}, func(sheet Object) { skipped = append(skipped, sheet) })
	if e != nil {
		return e
	}
	var owner bool
	if e = tx.QueryRow(ctx, "SELECT true FROM ps_jobs WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid AND state='running' FOR UPDATE", a.Tenant, job["id"], token).Scan(&owner); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, "UPDATE ps_imports SET state='preview',counts=$3 WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, batch["id"], encoded(Object{"total": total, "valid": valid, "invalid": invalid, "skipped_sheets": skipped}))
	if e != nil {
		return e
	}
	if e = tx.Commit(ctx); e != nil {
		return e
	}
	return s.finishJob(ctx, job, token, Object{"total": total, "valid": valid, "invalid": invalid}, "succeeded")
}
func (s *service) finishJob(ctx context.Context, job Object, token string, result Object, state string) error {
	_, e := s.db.Exec(ctx, "UPDATE ps_jobs SET state=$4,result=$5,error=NULL,progress=100,lease_until=NULL,lease_token=NULL,updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid", integer(job["tenant_id"]), job["id"], token, state, encoded(result))
	return e
}
func (s *service) editImport(ctx context.Context, a identity, id string, b Object, commit bool) (any, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := receipt(ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	var raw []byte
	if e = tx.QueryRow(ctx, "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid FOR UPDATE", a.Tenant, id).Scan(&raw); e != nil {
		return nil, e
	}
	batch := decodeObject(raw)
	if commit {
		state := str(batch["state"])
		if state == "importing" || state == "completed" || state == "partial" {
			var jobRaw []byte
			if e = tx.QueryRow(ctx, "SELECT jsonb_build_object('job_id',id,'state',state,'import_id',$2::text) FROM ps_jobs WHERE tenant_id=$1 AND kind='import' AND payload->>'import_id'=$2 ORDER BY created_at DESC LIMIT 1", a.Tenant, id).Scan(&jobRaw); e != nil {
				return nil, e
			}
			return decodeObject(jobRaw), nil
		}
		if state != "preview" {
			return nil, fail(409, "请等待解析完成")
		}
		if _, e = period(batch, true); e != nil {
			return nil, e
		}
		if _, e = tx.Exec(ctx, "UPDATE ps_imports SET state='importing' WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id); e != nil {
			return nil, e
		}
		result, e := insertJob(ctx, tx, a, "import", Object{"import_id": id})
		if e != nil {
			return nil, e
		}
		result["import_id"] = id
		if e = saveReceipt(ctx, tx, a, result); e != nil {
			return nil, e
		}
		if e = tx.Commit(ctx); e != nil {
			return nil, e
		}
		return result, nil
	}
	if str(batch["state"]) != "preview" && str(batch["state"]) != "failed" {
		return nil, fail(409, "当前状态不能调整映射")
	}
	var committed bool
	if e = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$2::uuid AND state='committed')", a.Tenant, id).Scan(&committed); e != nil {
		return nil, e
	}
	if committed {
		return nil, fail(409, "该批次已有入库数据，请重试原导入任务；调整映射请新建导入")
	}
	for key, value := range b {
		batch[key] = value
	}
	applyImportDefaults(batch, str(batch["file_name"]), time.Now())
	meta, e := period(batch, false)
	if e != nil {
		return nil, e
	}
	for k, v := range meta {
		batch[k] = v
	}
	mapping, ok := batch["mapping"].(map[string]any)
	if !ok || len(mapping) > 60 {
		return nil, fail(422, "映射无效")
	}
	var aliases struct {
		Aliases map[string][]string `json:"ALIASES"`
	}
	unmarshal(normalizationData, &aliases)
	for key, value := range mapping {
		if aliases.Aliases[key] == nil || len(str(value)) > 200 {
			return nil, fail(422, "映射字段无效")
		}
	}
	_, e = tx.Exec(ctx, "UPDATE ps_imports SET mapping=$3,mapping_hash=$4,source_period=$5,observed_on=$6,source_type=$7,period_type=$8,period_start=$9,period_end=$10,default_sales_scope=$11,parse_generation=parse_generation+1,state='parsing' WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id, encoded(mapping), shaDigest(encoded(mapping)), str(batch["source_period"]), batch["observed_on"], batch["source_type"], batch["period_type"], batch["period_start"], batch["period_end"], batch["default_sales_scope"])
	if e != nil {
		return nil, e
	}
	result, e := insertJob(ctx, tx, a, "parse", Object{"import_id": id, "generation": integer(batch["parse_generation"]) + 1})
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
func (s *service) runImport(ctx context.Context, job Object, token string) error {
	a := identity{Tenant: integer(job["tenant_id"]), User: integer(job["created_by"]), Role: "member"}
	payload := job["payload"].(map[string]any)
	batch, e := s.one(ctx, "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, payload["import_id"])
	if e != nil {
		return e
	}
	meta, e := period(batch, true)
	if e != nil {
		return e
	}
	products := []string{}
	for {
		tx, e := s.db.Begin(ctx)
		if e != nil {
			return e
		}
		var owner bool
		if e = tx.QueryRow(ctx, "SELECT true FROM ps_jobs WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid AND state='running' FOR UPDATE", a.Tenant, job["id"], token).Scan(&owner); e != nil {
			tx.Rollback(ctx)
			return e
		}
		rows, e := tx.Query(ctx, "SELECT to_jsonb(r) FROM ps_import_rows r WHERE tenant_id=$1 AND import_id=$2::uuid AND state='valid' ORDER BY member_path,sheet_name,row_no,id LIMIT 500 FOR UPDATE", a.Tenant, batch["id"])
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		items := []Object{}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				break
			}
			items = append(items, decodeObject(raw))
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if len(items) == 0 {
			tx.Rollback(ctx)
			break
		}
		for _, row := range items {
			normalized, ok := row["normalized_data"].(map[string]any)
			if !ok {
				tx.Rollback(ctx)
				return fmt.Errorf("missing normalized row")
			}
			pid, e := importObservation(ctx, tx, a, batch, meta, row, normalized)
			if e != nil {
				tx.Rollback(ctx)
				return e
			}
			if pid != "" {
				products = append(products, pid)
			}
			if _, e = tx.Exec(ctx, "UPDATE ps_import_rows SET state='committed',product_id=$3::uuid WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, row["id"], nullableString(pid)); e != nil {
				tx.Rollback(ctx)
				return e
			}
		}
		_, e = tx.Exec(ctx, "UPDATE ps_jobs SET result=jsonb_build_object('committed',(SELECT count(*) FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$4::uuid AND state='committed')),progress=LEAST(99,(SELECT (100*count(*) FILTER (WHERE state='committed')/GREATEST(1,count(*) FILTER (WHERE state IN ('committed','valid'))))::integer FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$4::uuid)),updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid AND lease_token=$3::uuid", a.Tenant, job["id"], token, batch["id"])
		if e != nil {
			tx.Rollback(ctx)
			return e
		}
		if e = tx.Commit(ctx); e != nil {
			return e
		}
	}
	var invalid int
	if e = s.db.QueryRow(ctx, "SELECT count(*) FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$2::uuid AND state='invalid'", a.Tenant, batch["id"]).Scan(&invalid); e != nil {
		return e
	}
	state := "completed"
	jobState := "succeeded"
	if invalid > 0 {
		state = "partial"
		jobState = "partial"
	}
	if _, e = s.db.Exec(ctx, "UPDATE ps_imports SET state=$3,committed_at=now() WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, batch["id"], state); e != nil {
		return e
	}
	// Recover the complete imported product set after a worker restart.
	rows, e := s.db.Query(ctx, "SELECT DISTINCT product_id::text FROM ps_import_rows WHERE tenant_id=$1 AND import_id=$2::uuid AND product_id IS NOT NULL ORDER BY product_id::text", a.Tenant, batch["id"])
	if e != nil {
		return e
	}
	ids := []any{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			break
		}
		ids = append(ids, id)
	}
	if e == nil {
		e = rows.Err()
	}
	rows.Close()
	if e != nil {
		return e
	}
	result := Object{"import_id": batch["id"], "products": len(ids), "invalid": invalid}
	if len(ids) > 0 {
		if e = s.ensurePolicy(ctx, a); e != nil {
			return e
		}
		a.RequestKey = "auto-screen-import:" + str(batch["id"])
		a.RequestHash = shaDigest(encoded(ids))
		screen, e := s.enqueueScreen(ctx, a, Object{"product_ids": ids}, "")
		if e != nil {
			return e
		}
		result["screen"] = screen
	}
	return s.finishJob(ctx, job, token, result, jobState)
}
func importObservation(ctx context.Context, tx pgx.Tx, a identity, batch, meta, row, n Object) (string, error) {
	scope := str(n["sales_scope"])
	if n["sales_scope_explicit"] != true && str(batch["default_sales_scope"]) != "unknown" {
		scope = str(batch["default_sales_scope"])
	}
	parent := str(n["parent_asin"])
	if scope == "parent" && parent == "" {
		return "", fail(422, "父体销量必须有父ASIN")
	}
	var parentSales, parentRevenue any
	if scope == "parent" {
		parentSales = n["monthly_sales"]
		parentRevenue = n["monthly_revenue"]
	}
	if n["parent_monthly_sales"] != nil {
		parentSales = n["parent_monthly_sales"]
	}
	if n["parent_monthly_revenue"] != nil {
		parentRevenue = n["parent_monthly_revenue"]
	}
	if parent != "" && (parentSales != nil || parentRevenue != nil) {
		_, e := tx.Exec(ctx, `INSERT INTO ps_parent_monthly(tenant_id,id,parent_asin,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,monthly_sales,monthly_revenue) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12) ON CONFLICT(tenant_id,marketplace,parent_asin,source_type,period_type,period_start,period_end) DO UPDATE SET observed_on=EXCLUDED.observed_on,source_row_id=EXCLUDED.source_row_id,monthly_sales=EXCLUDED.monthly_sales,monthly_revenue=EXCLUDED.monthly_revenue,revision=ps_parent_monthly.revision+1,updated_at=now() WHERE ROW(EXCLUDED.observed_on,(SELECT ROW(i.created_at,r.member_path,r.sheet_name,r.row_no,r.id) FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.tenant_id=EXCLUDED.tenant_id AND r.id=EXCLUDED.source_row_id)) > ROW(ps_parent_monthly.observed_on,(SELECT ROW(i.created_at,r.member_path,r.sheet_name,r.row_no,r.id) FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.tenant_id=ps_parent_monthly.tenant_id AND r.id=ps_parent_monthly.source_row_id))`, a.Tenant, uuid(), parent, meta["stat_month"], meta["period_type"], meta["period_start"], meta["period_end"], batch["observed_on"], meta["source_type"], row["id"], parentSales, parentRevenue)
		if e != nil {
			return "", e
		}
	}
	if str(n["listing_role"]) == "parent" {
		return "", nil
	}
	id := uuid()
	if e := tx.QueryRow(ctx, "INSERT INTO ps_products(tenant_id,id,asin,title) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,marketplace,asin) DO UPDATE SET asin=EXCLUDED.asin RETURNING id::text", a.Tenant, id, n["asin"], n["title"]).Scan(&id); e != nil {
		return "", e
	}
	_, e := tx.Exec(ctx, `INSERT INTO ps_product_monthly(tenant_id,id,product_id,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,price_usd,monthly_sales,monthly_revenue,review_count,rating,bsr_rank,sales_scope,parent_asin_snapshot) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) ON CONFLICT(tenant_id,product_id,source_type,period_type,period_start,period_end) DO UPDATE SET observed_on=EXCLUDED.observed_on,source_row_id=EXCLUDED.source_row_id,price_usd=EXCLUDED.price_usd,monthly_sales=EXCLUDED.monthly_sales,monthly_revenue=EXCLUDED.monthly_revenue,review_count=EXCLUDED.review_count,rating=EXCLUDED.rating,bsr_rank=EXCLUDED.bsr_rank,sales_scope=EXCLUDED.sales_scope,parent_asin_snapshot=EXCLUDED.parent_asin_snapshot,revision=ps_product_monthly.revision+1,updated_at=now() WHERE ROW(EXCLUDED.observed_on,(SELECT ROW(i.created_at,r.member_path,r.sheet_name,r.row_no,r.id) FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.tenant_id=EXCLUDED.tenant_id AND r.id=EXCLUDED.source_row_id)) > ROW(ps_product_monthly.observed_on,(SELECT ROW(i.created_at,r.member_path,r.sheet_name,r.row_no,r.id) FROM ps_import_rows r JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE r.tenant_id=ps_product_monthly.tenant_id AND r.id=ps_product_monthly.source_row_id))`, a.Tenant, uuid(), id, meta["stat_month"], meta["period_type"], meta["period_start"], meta["period_end"], batch["observed_on"], meta["source_type"], row["id"], n["price_usd"], n["monthly_sales"], n["monthly_revenue"], n["review_count"], n["rating"], n["bsr_rank"], scope, nullableString(parent))
	if e != nil {
		return "", e
	}
	var currentRow string
	var monthlyID string
	var monthlyRevision int64
	if e = tx.QueryRow(ctx, "SELECT m.source_row_id::text,m.id::text,m.revision FROM ps_product_monthly m JOIN ps_import_rows r ON r.tenant_id=m.tenant_id AND r.id=m.source_row_id JOIN ps_imports i ON i.tenant_id=r.tenant_id AND i.id=r.import_id WHERE m.tenant_id=$1 AND m.product_id=$2::uuid ORDER BY m.stat_month DESC,m.period_end DESC,m.observed_on DESC,i.created_at DESC,r.member_path DESC,r.sheet_name DESC,r.row_no DESC,r.id DESC LIMIT 1", a.Tenant, id).Scan(&currentRow, &monthlyID, &monthlyRevision); e != nil {
		return "", e
	}
	if _, e = tx.Exec(ctx, "UPDATE ps_products SET current_monthly_id=$3::uuid,current_monthly_revision=$4 WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id, monthlyID, monthlyRevision); e != nil {
		return "", e
	}
	if currentRow != str(row["id"]) {
		return id, nil
	}
	args := []any{a.Tenant, id, row["id"]}
	set := "current_row_id=$3::uuid,data_revision=data_revision+1,screen_generation=screen_generation+1,machine_decision=NULL,machine_group_key=NULL,current_group_rep_id=NULL,updated_at=now()"
	columns := append([]string{"title", "title_zh", "image_url", "category", "brand", "seller_id", "seller_name", "seller_region", "specs"}, parameterColumns...)
	for _, column := range columns {
		v := n[column]
		if v == nil || str(v) == "" {
			continue
		}
		if column == "specs" {
			v = encoded(v)
		}
		args = append(args, v)
		set += fmt.Sprintf(",%s=$%d", column, len(args))
	}
	_, e = tx.Exec(ctx, "UPDATE ps_products SET "+set+" WHERE tenant_id=$1 AND id=$2::uuid", args...)
	return id, e
}
