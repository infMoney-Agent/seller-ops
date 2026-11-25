package screening

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func TestImportedMeasurements(t *testing.T) {
	raw := Object{"ASIN": "B000000001", "商品标题": "manual storage tool", "商品尺寸": "9 x 3 x 7 inches", "商品尺寸（单位换算）": "22.86 x 7.62 x 17.78 cm", "商品重量": "0.34 pounds", "商品重量（单位换算）": "154.22 g", "包装尺寸（单位换算）": "25.65 x 21.59 x 8.13 cm", "包装重量（单位换算）": "181.44 g", "FBA($)": "5.91", "毛利率": "70%"}
	n, errs := normalizeRow(raw, Object{})
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	for key, want := range map[string]string{"product_length_cm": "22.860000", "product_width_cm": "7.620000", "product_height_cm": "17.780000", "weight_g": "154.220000", "package_weight_g": "181.440000", "package_length_cm": "25.650000", "imported_fba_fee_usd": "5.910000", "imported_gross_margin_rate": "0.700000"} {
		if str(n[key]) != want {
			t.Fatalf("%s=%v want %s", key, n[key], want)
		}
	}
	if got := measurement("9 x 3 x 7 inches", false); got[0] != "22.860000" {
		t.Fatal(got)
	}
	if got := measurement("0.34 pounds", true); got[0] != "154.221406" {
		t.Fatal(got)
	}
	if got := measurement("9 x 3 x 7", false); got != nil {
		t.Fatal("guessed missing unit", got)
	}
	if got := measurement("2 oz", true); got[0] != "56.699046" {
		t.Fatal(got)
	}
	if sourceDecimal("unknown", false) != nil || sourceDecimal("-5", false) != nil {
		t.Fatal("invalid estimate accepted")
	}
	if str(sourceDecimal("-20%", true)) != "-0.200000" {
		t.Fatal("negative source margin lost")
	}
}
func TestImportedColumnStorage(t *testing.T) {
	url := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("test PostgreSQL required")
	}
	if !strings.Contains(url, "_test") {
		t.Fatal("isolated test database required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA screening_columns_test"); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "screening_columns_test"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, name := range []string{"001_initial.sql", "002_groups.sql", "003_monthly.sql", "004_compact.sql", "005_go_jobs.sql", "006_self_parent.sql"} {
		data, e := migrationFiles.ReadFile("migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(data)); e != nil {
			t.Fatal(name, e)
		}
	}
	if _, err = pool.Exec(ctx, "CREATE TABLE screening_schema_versions(version integer PRIMARY KEY,applied_at timestamptz NOT NULL DEFAULT now());INSERT INTO screening_schema_versions(version) VALUES(6)"); err != nil {
		t.Fatal(err)
	}
	pid, iid, rid, mid := uuid(), uuid(), uuid(), uuid()
	raw := Object{"ASIN": "B000000001", "商品标题": "manual storage tool", "父ASIN": "B000000001", "变体数": "1", "商品尺寸（单位换算）": "22.86 x 7.62 x 17.78 cm", "包装重量（单位换算）": "181.44 g", "商品重量（单位换算）": "154.22 g", "SP广告": "是", "大类BSR": "23", "销量同比增长率": "12%", "FBA($)": "5.91", "毛利率": "70%", "月销量": "100"}
	exec := func(query string, args ...any) {
		t.Helper()
		if _, e := pool.Exec(ctx, query, args...); e != nil {
			t.Fatal(e)
		}
	}
	exec("INSERT INTO ps_products(tenant_id,id,asin,title,weight_g,review_status,review_reason,data_revision) VALUES(711,$1,'B000000001','manual storage tool',999,'excluded','人工决定',3)", pid)
	exec("INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping_hash,state,created_by,source_period,observed_on,source_type,period_type,period_start,period_end) VALUES(711,$1,'Product-US-2026.08.csv',repeat('a',64),'test',1,repeat('b',64),'completed',7,'2026-08','2026-09-01','sellersprite','calendar_month','2026-08-01','2026-08-31')", iid)
	exec("INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,product_id,state,raw_data,normalized_data) VALUES(711,$1,$2,2,$3,'committed',$4,'{}')", rid, iid, pid, encoded(raw))
	exec("INSERT INTO ps_product_monthly(tenant_id,id,product_id,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id) VALUES(711,$1,$2,'2026-08-01','calendar_month','2026-08-01','2026-08-31','2026-09-01','sellersprite',$3)", mid, pid, rid)
	exec("UPDATE ps_products SET current_row_id=$1,current_monthly_id=$2,current_monthly_revision=1 WHERE tenant_id=711 AND id=$3", rid, mid, pid)
	exec(`INSERT INTO ps_profit_scenarios(tenant_id,id,product_id,name,inputs,output,formula_version,product_data_revision,created_by,monthly_id,monthly_revision) VALUES(711,$1,$2,'manual','{}','{"profit_usd":"6.64"}','fba-us-v1',3,7,$3,1)`, uuid(), pid, mid)
	if err = migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	if err = migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	var length, weight, decision, reason string
	var revision int64
	if err = pool.QueryRow(ctx, "SELECT product_length_cm::text,weight_g::text,review_status,review_reason,data_revision FROM ps_products WHERE tenant_id=711 AND id=$1", pid).Scan(&length, &weight, &decision, &reason, &revision); err != nil || length != "22.860000" || weight != "999.000000" || decision != "excluded" || reason != "人工决定" || revision != 4 {
		t.Fatal(length, weight, decision, reason, revision, err)
	}
	var ad, yoy string
	if err = pool.QueryRow(ctx, "SELECT sponsored_products,sales_yoy FROM ps_product_monthly WHERE tenant_id=711 AND id=$1", mid).Scan(&ad, &yoy); err != nil || ad != "是" || yoy != "12%" {
		t.Fatal(ad, yoy, err)
	}
	var count int
	var fba, margin string
	if err = pool.QueryRow(ctx, "SELECT count(*),max(imported_fba_fee_usd)::text,max(imported_gross_margin_rate)::text FROM ps_profit_scenarios WHERE tenant_id=711 AND record_kind='imported'").Scan(&count, &fba, &margin); err != nil || count != 1 || fba != "5.910000" || margin != "0.700000" {
		t.Fatal(count, fba, margin, err)
	}
	service := &service{db: pool}
	p, err := service.one(ctx, "SELECT "+productProjection+" FROM ps_products p LEFT JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id "+profitJoin+" WHERE p.tenant_id=$1 AND p.id=$2::uuid", 711, pid)
	if err != nil {
		t.Fatal(err)
	}
	if str(p["profit_output"].(map[string]any)["profit_usd"]) != "6.64" {
		t.Fatal("source estimate replaced calculated profit", p)
	}
	var unchanged bool
	if err = pool.QueryRow(ctx, "SELECT raw_data=$2::jsonb AND normalized_data='{}'::jsonb FROM ps_import_rows WHERE id=$1", rid, encoded(raw)).Scan(&unchanged); err != nil || !unchanged {
		t.Fatal("source row modified", err)
	}
	// Replaying a source observation cannot duplicate or overwrite the source-profit snapshot.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	n, _ := normalizeRow(raw, Object{})
	if err = saveImportedMonthly(ctx, tx, identity{Tenant: 711, User: 7}, pid, rid, n); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ps_profit_scenarios WHERE tenant_id=711").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}
	// A new source observation writes the same structured fields through the live import path.
	nextImport, nextRow := uuid(), uuid()
	raw["商品尺寸（单位换算）"] = "30 x 20 x 10 cm"
	raw["FBA($)"] = "6.12"
	exec("INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping_hash,state,created_by,source_period,observed_on,source_type,period_type,period_start,period_end) VALUES(711,$1,'next.csv',repeat('c',64),'test-next',1,repeat('d',64),'importing',7,'2026-09','2026-10-01','sellersprite','calendar_month','2026-09-01','2026-09-30')", nextImport)
	n, _ = normalizeRow(raw, Object{})
	exec("INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,state,raw_data,normalized_data) VALUES(711,$1,$2,2,'valid',$3,$4)", nextRow, nextImport, encoded(raw), encoded(n))
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	batch := Object{"id": nextImport, "observed_on": "2026-10-01", "default_sales_scope": "unknown"}
	meta := Object{"stat_month": "2026-09-01", "period_type": "calendar_month", "period_start": "2026-09-01", "period_end": "2026-09-30", "source_type": "sellersprite"}
	if _, err = importObservation(ctx, tx, identity{Tenant: 711, User: 7}, batch, meta, Object{"id": nextRow}, n); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = pool.QueryRow(ctx, "SELECT product_length_cm::text,review_status,review_reason FROM ps_products WHERE tenant_id=711 AND id=$1", pid).Scan(&length, &decision, &reason); err != nil || length != "30.000000" || decision != "excluded" || reason != "人工决定" {
		t.Fatal(length, decision, reason, err)
	}
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ps_profit_scenarios WHERE tenant_id=711 AND record_kind='imported'").Scan(&count); err != nil || count != 2 {
		t.Fatal(count, err)
	}

}
