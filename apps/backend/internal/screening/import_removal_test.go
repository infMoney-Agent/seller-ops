package screening

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func TestBatchDataRemoval(t *testing.T) {
	url := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("isolated PostgreSQL required")
	}
	if !strings.Contains(url, "_test") {
		t.Fatal("test database required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, url)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA removal_test"); e != nil {
		t.Fatal(e)
	}
	cfg, _ := pgxpool.ParseConfig(url)
	cfg.ConnConfig.RuntimeParams["search_path"] = "removal_test"
	pool, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if e = migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	s := &service{db: pool}
	a := identity{Tenant: 622, User: 7, Role: "member"}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(month, observed, price string, orphan bool) (string, string) {
		t.Helper()
		bid := uuid()
		exec("INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping_hash,state,created_by,source_period,observed_on,source_type,period_type,period_start,period_end) VALUES(622,$1,'source.csv',$2,'test',1,repeat('a',64),'completed',7,$3,$4::date,'sellersprite','calendar_month',($3||'-01')::date,(($3||'-01')::date+interval '1 month - 1 day')::date)", bid, shaDigest([]byte(observed)), month, observed)
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		var shared string
		asins := []string{"SHARED0001"}
		if orphan {
			asins = append(asins, "ONLY000001")
		}
		batch := Object{"observed_on": observed, "source_period": month, "period_type": "calendar_month", "source_type": "sellersprite", "default_sales_scope": "unknown"}
		meta, err := period(batch, true)
		if err != nil {
			t.Fatal(err)
		}
		for index, asin := range asins {
			rowID := uuid()
			raw := Object{"ASIN": asin, "商品标题": "manual storage tool", "父ASIN": "PARENT0001", "价格($)": price, "月销量": "900", "子体销量": "200", "商品尺寸（单位换算）": price + " x 2 x 3 cm", "FBA($)": "3.50"}
			n, errs := normalizeRow(raw, Object{})
			if len(errs) > 0 {
				t.Fatal(errs)
			}
			if _, err = tx.Exec(ctx, "INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,state,raw_data,normalized_data) VALUES(622,$1,$2,$3,'committed',$4,$5)", rowID, bid, index+2, encoded(raw), encoded(n)); err != nil {
				t.Fatal(err)
			}
			pid, err := importObservation(ctx, tx, a, batch, meta, Object{"id": rowID}, n)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = tx.Exec(ctx, "UPDATE ps_import_rows SET product_id=$2 WHERE tenant_id=622 AND id=$1", rowID, pid); err != nil {
				t.Fatal(err)
			}
			if asin == "SHARED0001" {
				shared = pid
			}
		}
		if err = tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		return bid, shared
	}
	older, pid := seed("2026-08", "2026-09-01", "10", false)
	removed, _ := seed("2026-08", "2026-09-02", "20", true)
	_, _ = seed("2026-09", "2026-10-01", "30", false)
	p, err := s.product(ctx, a, pid)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.mutateProduct(ctx, a, pid, "parameters", Object{"expected_data_revision": p["data_revision"], "package_length_cm": "99"}); err != nil {
		t.Fatal(err)
	}
	p, _ = s.product(ctx, a, pid)
	if _, err = s.mutateProduct(ctx, a, pid, "review", Object{"expected_data_revision": p["data_revision"], "expected_review_revision": p["review_revision"], "status": "kept", "reason": "人工核对", "note": "必须保留"}); err != nil {
		t.Fatal(err)
	}
	var monthly string
	if err = pool.QueryRow(ctx, "SELECT id::text FROM ps_product_monthly WHERE tenant_id=622 AND product_id=$1 AND stat_month='2026-08-01'", pid).Scan(&monthly); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO ps_profit_scenarios(tenant_id,id,product_id,name,inputs,output,formula_version,product_data_revision,created_by,monthly_id,monthly_revision) VALUES(622,$1,$2,'saved','{}','{"profit_usd":"6.64"}','test',0,7,$3,1)`, uuid(), pid, monthly)

	var sourceRow string
	if err = pool.QueryRow(ctx, "SELECT id::text FROM ps_import_rows WHERE tenant_id=622 AND import_id=$1 AND product_id=$2", removed, pid).Scan(&sourceRow); err != nil {
		t.Fatal(err)
	}
	source, err := s.observationSource(ctx, a, pid, sourceRow)
	if err != nil || str(source["file_name"]) != "source.csv" || integer(source["row_no"]) != 2 || str(source["raw_data"].(map[string]any)["ASIN"]) != "SHARED0001" {
		t.Fatal(source, err)
	}
	if _, err = s.observationSource(ctx, identity{Tenant: 999}, pid, sourceRow); err == nil {
		t.Fatal("cross tenant source row exposed")
	}
	if _, err = s.observationSource(ctx, a, uuid(), sourceRow); err == nil {
		t.Fatal("unrelated product source row exposed")
	}
	impact, err := s.importRemovalPreview(ctx, a, removed)
	if err != nil || integer(impact.(Object)["exclusive_products"]) != 1 {
		t.Fatal(impact, err)
	}
	if _, err = s.importRemovalPreview(ctx, identity{Tenant: 999}, removed); err == nil {
		t.Fatal("cross tenant access")
	}
	key := a
	key.RequestKey = "remove-batch"
	key.RequestHash = shaDigest([]byte(removed))
	out, err := s.removeImportHistory(ctx, key, removed)
	if err != nil {
		t.Fatal(err)
	}
	if integer(out.(Object)["removed_products"]) != 1 {
		t.Fatal(out)
	}
	if _, err = s.removeImportHistory(ctx, key, removed); err != nil {
		t.Fatal(err)
	}
	if _, err = s.observationSource(ctx, a, pid, sourceRow); err == nil {
		t.Fatal("removed source row exposed")
	}
	var price, parentPrice, length, status, note string
	if err = pool.QueryRow(ctx, "SELECT m.price_usd::text,p.package_length_cm::text,p.review_status,p.review_note FROM ps_products p JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.id=p.current_monthly_id WHERE p.tenant_id=622 AND p.id=$1", pid).Scan(&price, &length, &status, &note); err != nil || price != "30.000000" || length != "99.000000" || status != "kept" || note != "必须保留" {
		t.Fatal(price, length, status, note, err)
	}
	if err = pool.QueryRow(ctx, "SELECT price_usd::text FROM ps_product_monthly WHERE tenant_id=622 AND product_id=$1 AND stat_month='2026-08-01'", pid).Scan(&price); err != nil || price != "10.000000" {
		t.Fatal("same-month fallback failed", price, err)
	}
	if err = pool.QueryRow(ctx, "SELECT monthly_sales::text FROM ps_parent_monthly WHERE tenant_id=622 AND parent_asin='PARENT0001' AND stat_month='2026-08-01'").Scan(&parentPrice); err != nil || parentPrice != "900" {
		t.Fatal(parentPrice, err)
	}
	var active int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ps_products WHERE tenant_id=622 AND archived_at IS NULL").Scan(&active); err != nil || active != 1 {
		t.Fatal(active, err)
	}
	var output string
	var detached bool
	if err = pool.QueryRow(ctx, "SELECT output->>'profit_usd',monthly_id IS NULL FROM ps_profit_scenarios WHERE tenant_id=622 AND record_kind='calculated'").Scan(&output, &detached); err != nil || output != "6.64" || !detached {
		t.Fatal(output, detached, err)
	}
	var remaining int
	if err = pool.QueryRow(ctx, "SELECT count(*) FROM ps_product_monthly m JOIN ps_import_rows r ON r.tenant_id=m.tenant_id AND r.id=m.source_row_id WHERE r.import_id=$1", removed).Scan(&remaining); err != nil || remaining != 0 {
		t.Fatal(remaining, err)
	}
	// Deleted fingerprints permit a new import, rather than restoring removed business data implicitly.
	fresh := uuid()
	exec("INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping_hash,state,created_by,source_period,observed_on,source_type,period_type,period_start,period_end,default_sales_scope) SELECT tenant_id,$1,file_name,file_sha256,storage_key,file_size,mapping_hash,'preview',created_by,source_period,observed_on,source_type,period_type,period_start,period_end,default_sales_scope FROM ps_imports WHERE id=$2", fresh, removed)
	// Removing the remaining data eventually archives the shared product too.
	_, err = s.removeImportHistory(ctx, a, older)
	if err != nil {
		t.Fatal(err)
	}
}
