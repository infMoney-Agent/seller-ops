package screening

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func TestSalesNormalization(t *testing.T) {
	tests := []struct {
		name, parent, variants string
		child, sales           any
		scope                  string
		wantChild, wantParent  any
	}{
		{"single self parent", "B000000001", "1", nil, "1,165", "child", int64(1165), int64(1165)},
		{"multiple self parent", "B000000001", "4", nil, "6454", "unknown", nil, nil},
		{"different parent", "PARENT0001", "1", nil, "500", "unknown", nil, nil},
		{"missing variants", "B000000001", "", nil, "500", "unknown", nil, nil},
		{"explicit sellersprite child", "PARENT0001", "4", "200 ", "3292", "child", int64(200), int64(3292)},
		{"zero child is known", "B000000001", "1", "0", "100", "child", int64(0), int64(0)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			raw := Object{"ASIN": "B000000001", "父ASIN": tc.parent, "商品标题": "manual storage tool", "变体数": tc.variants, "月销量": tc.sales, "子体销量": tc.child}
			n, errs := normalizeRow(raw, Object{})
			if len(errs) > 0 {
				t.Fatal(errs)
			}
			if str(n["sales_scope"]) != tc.scope || n["child_monthly_sales"] != tc.wantChild || n["parent_monthly_sales"] != tc.wantParent {
				t.Fatalf("unexpected sales normalization: %#v", n)
			}
		})
	}
	n, errs := normalizeRow(Object{"ASIN": "B000000001", "商品标题": "tool", "子体销量": "100", "子体销售额($)": "1,299 "}, Object{})
	if len(errs) > 0 || n["monthly_revenue"] != "1299" {
		t.Fatal(n, errs)
	}
}

func TestSingleVariantObservation(t *testing.T) {
	url := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("dedicated test database required")
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("invalid test database configuration")
	}
	if !strings.HasSuffix(cfg.ConnConfig.Database, "_test") {
		t.Fatal("test database required")
	}
	ctx := context.Background()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err = migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	batchID, rowID := uuid(), uuid()
	_, err = tx.Exec(ctx, "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping,mapping_hash,state,created_by,source_period,observed_on,source_type,period_type,period_start,period_end) VALUES(987,$1,'single.csv',repeat('1',64),'test.csv',0,'{}',repeat('2',64),'importing',1,'2026-08','2026-11-19','sellersprite','calendar_month','2026-08-01','2026-08-31')", batchID)
	if err != nil {
		t.Fatal(err)
	}
	raw := Object{"ASIN": "B000000001", "父ASIN": "B000000001", "商品标题": "manual tool", "变体数": "1", "月销量": "1,165"}
	n, errs := normalizeRow(raw, Object{})
	if len(errs) > 0 {
		t.Fatal(errs)
	}
	_, err = tx.Exec(ctx, "INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,state,raw_data,normalized_data) VALUES(987,$1,$2,2,'valid',$3,$4)", rowID, batchID, encoded(raw), encoded(n))
	if err != nil {
		t.Fatal(err)
	}
	batch := Object{"observed_on": "2026-11-19", "default_sales_scope": "unknown"}
	meta := Object{"stat_month": "2026-08-01", "period_type": "calendar_month", "period_start": "2026-08-01", "period_end": "2026-08-31", "source_type": "sellersprite"}
	id, err := importObservation(ctx, tx, identity{Tenant: 987, User: 1}, batch, meta, Object{"id": rowID}, n)
	if err != nil {
		t.Fatal(err)
	}
	var child, parent int64
	var scope string
	err = tx.QueryRow(ctx, "SELECT c.monthly_sales,p.monthly_sales,c.sales_scope FROM ps_product_monthly c JOIN ps_parent_monthly p ON p.tenant_id=c.tenant_id AND p.parent_asin=c.parent_asin_snapshot WHERE c.tenant_id=987 AND c.product_id=$1", id).Scan(&child, &parent, &scope)
	if err != nil || child != 1165 || parent != 1165 || scope != "child" {
		t.Fatal(child, parent, scope, err)
	}
}
