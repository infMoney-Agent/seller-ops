package screening

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func TestSingleVariantSalesDisplay(t *testing.T) {
	url := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("test PostgreSQL required")
	}
	if !strings.Contains(url, "_test") {
		t.Fatal("isolated database required")
	}
	ctx := context.Background()
	cfg, e := pgxpool.ParseConfig(url)
	if e != nil {
		t.Fatal(e)
	}
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA sales_display_test"); e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "sales_display_test"
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, e := db.Exec(ctx, q, args...); e != nil {
			t.Fatal(e)
		}
	}
	iid := uuid()
	exec("INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping_hash,state,created_by,source_period,observed_on,source_type,period_type,period_start,period_end) VALUES(901,$1,'test.csv',repeat('a',64),'test',1,repeat('b',64),'completed',7,'2026-08','2026-09-01','sellersprite','calendar_month','2026-08-01','2026-08-31')", iid)
	pid, rid, mid := uuid(), uuid(), uuid()
	exec("INSERT INTO ps_products(tenant_id,id,asin,parent_asin,title) VALUES(901,$1,'B0CTHVNTMH','B089KGGW1R','tablecloth')", pid)
	normalized := Object{"variant_count": 1, "listing_role": "child", "source_monthly_sales": 7465}
	exec("INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,product_id,state,raw_data,normalized_data) VALUES(901,$1,$2,2,$3,'committed','{}',$4)", rid, iid, pid, encoded(normalized))
	exec("INSERT INTO ps_product_monthly(tenant_id,id,product_id,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,parent_asin_snapshot,sales_scope,monthly_sales) VALUES(901,$1,$2,'2026-08-01','calendar_month','2026-08-01','2026-08-31','2026-09-01','sellersprite',$3,'B089KGGW1R','unknown',7465)", mid, pid, rid)
	check := func(want any) {
		t.Helper()
		var got any
		if e := db.QueryRow(ctx, "SELECT "+displayChildSalesSQL+" FROM ps_products p JOIN ps_product_monthly m ON m.tenant_id=p.tenant_id AND m.product_id=p.id WHERE p.tenant_id=901 AND p.id=$1", pid).Scan(&got); e != nil {
			t.Fatal(e)
		}
		if want == nil {
			if got != nil {
				t.Fatalf("got %v want null", got)
			}
		} else if integer(got) != integer(want) {
			t.Fatalf("got %v want %v", got, want)
		}
	}
	check(7465)
	exec("UPDATE ps_product_monthly SET sales_scope='child',monthly_sales=0 WHERE id=$1", mid)
	check(0)
	exec("UPDATE ps_product_monthly SET sales_scope='unknown',monthly_sales=7465 WHERE id=$1", mid)
	for _, v := range []any{2, nil} {
		normalized["variant_count"] = v
		exec("UPDATE ps_import_rows SET normalized_data=$2 WHERE id=$1", rid, encoded(normalized))
		check(nil)
	}
	normalized["variant_count"] = 1
	exec("UPDATE ps_import_rows SET normalized_data=$2 WHERE id=$1", rid, encoded(normalized))
	// A sibling from another period must not invalidate this observation.
	sid, srid, smid := uuid(), uuid(), uuid()
	exec("INSERT INTO ps_products(tenant_id,id,asin,parent_asin,title) VALUES(901,$1,'B000000002','B089KGGW1R','sibling')", sid)
	exec("INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,product_id,state,raw_data,normalized_data) VALUES(901,$1,$2,3,$3,'committed','{}',$4)", srid, iid, sid, encoded(normalized))
	exec("INSERT INTO ps_product_monthly(tenant_id,id,product_id,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,parent_asin_snapshot) VALUES(901,$1,$2,'2026-07-01','calendar_month','2026-07-01','2026-07-31','2026-09-01','sellersprite',$3,'B089KGGW1R')", smid, sid, srid)
	check(7465)
	exec("UPDATE ps_product_monthly SET stat_month='2026-08-01',period_start='2026-08-01',period_end='2026-08-31' WHERE id=$1", smid)
	check(nil)
	exec("UPDATE ps_products SET archived_at=now() WHERE id=$1", sid)
	check(7465)
	// The fallback must not rewrite the underlying unknown sales scope or source.
	var unchanged bool
	if e = db.QueryRow(ctx, "SELECT m.sales_scope='unknown' AND m.monthly_sales=7465 AND r.normalized_data=$2::jsonb FROM ps_product_monthly m JOIN ps_import_rows r ON r.id=m.source_row_id AND r.tenant_id=m.tenant_id WHERE m.id=$1", mid, encoded(normalized)).Scan(&unchanged); e != nil || !unchanged {
		t.Fatal("raw values changed", e)
	}
}
