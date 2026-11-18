package screening

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
)

func TestLegacyMigrationAdoption(t *testing.T) {
	raw := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set SCREENING_TEST_DATABASE_URL")
	}
	if !strings.Contains(raw, "_test") {
		t.Fatal("test database required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA screening_legacy_test"); e != nil {
		t.Fatal(e)
	}
	pc, e := pgxpool.ParseConfig(raw)
	if e != nil {
		t.Fatal(e)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = "screening_legacy_test"
	pool, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	for _, name := range []string{"001_initial.sql", "002_groups.sql", "003_monthly.sql"} {
		data, e := migrationFiles.ReadFile("migrations/" + name)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, string(data)); e != nil {
			t.Fatalf("legacy migration %s: %v", name, e)
		}
	}
	seed := `
 CREATE TABLE alembic_version(version_num varchar(32) PRIMARY KEY);
 INSERT INTO alembic_version VALUES('screening_0003');
 INSERT INTO ps_policies(tenant_id,id,version,name,config,config_hash,created_by,active) VALUES(42,'00000000-0000-0000-0000-000000000001',1,'旧规则','{}',repeat('a',64),1,true);
 INSERT INTO ps_products(tenant_id,id,asin,title,review_status,review_reason,data_revision) VALUES(42,'00000000-0000-0000-0000-000000000002','B000000001','旧商品','excluded','人工结论必须保留',7);
 INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping_hash,state,created_by,source_period,observed_on,source_type,period_type,period_start,period_end) VALUES(42,'00000000-0000-0000-0000-000000000003','source.csv',repeat('b',64),'test/source',12,repeat('c',64),'completed',1,'2026-10','2026-10-31','sellersprite','calendar_month','2026-10-01','2026-10-31');
 INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,product_id,state,raw_data) VALUES(42,'00000000-0000-0000-0000-000000000004','00000000-0000-0000-0000-000000000003',2,'00000000-0000-0000-0000-000000000002','committed','{"ASIN":"B000000001"}');
 INSERT INTO ps_jobs(tenant_id,id,kind,state,idempotency_key,created_by) VALUES(42,'00000000-0000-0000-0000-000000000005','screen','succeeded','legacy-screen',1);
 INSERT INTO ps_screen_runs(tenant_id,id,job_id,policy_id) VALUES(42,'00000000-0000-0000-0000-000000000006','00000000-0000-0000-0000-000000000005','00000000-0000-0000-0000-000000000001');
 INSERT INTO ps_screen_results(tenant_id,id,run_id,product_id,source_row_id,data_revision,generation,decision,recommendation,direction,group_key,representative_id,reasons,evidence) VALUES(42,'00000000-0000-0000-0000-000000000007','00000000-0000-0000-0000-000000000006','00000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000004',7,0,'passed','regular','工具','tools','00000000-0000-0000-0000-000000000002','["旧机器结果"]','[]');
 UPDATE ps_products SET current_row_id='00000000-0000-0000-0000-000000000004',current_result_id='00000000-0000-0000-0000-000000000007' WHERE tenant_id=42;
 INSERT INTO ps_profit_templates(tenant_id,id,name,version,params,created_by) VALUES(42,'00000000-0000-0000-0000-000000000008','旧模板',1,'{}',1);
 INSERT INTO ps_profit_scenarios(tenant_id,id,product_id,template_id,name,inputs,output,formula_version,product_data_revision,created_by) VALUES(42,'00000000-0000-0000-0000-000000000009','00000000-0000-0000-0000-000000000002','00000000-0000-0000-0000-000000000008','历史利润','{}','{"profit_usd":"10.00"}','fba-us-v1',7,1);
 INSERT INTO ps_review_batches(tenant_id,id,idempotency_key,state,payload,expires_at,created_by) VALUES(42,'00000000-0000-0000-0000-000000000010','legacy-review','preview','{"targets":[]}',now()+interval '15 minutes',1);
 INSERT INTO ps_requests(tenant_id,request_key,request_hash,result) VALUES(42,'legacy-request',repeat('d',64),'{"legacy":true}');`
	if _, e = pool.Exec(ctx, seed); e != nil {
		t.Fatal(e)
	}
	if e = migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='screening_legacy_test' AND table_name LIKE 'ps_%'").Scan(&count); e != nil || count != 9 {
		t.Fatalf("tables=%d %v", count, e)
	}
	var status, reason, machine string
	if e = pool.QueryRow(ctx, "SELECT review_status,review_reason,machine_decision FROM ps_products WHERE tenant_id=42").Scan(&status, &reason, &machine); e != nil || status != "excluded" || reason != "人工结论必须保留" || machine != "passed" {
		t.Fatal(status, reason, machine, e)
	}
	var archived int
	if e = pool.QueryRow(ctx, "SELECT jsonb_array_length(result->'legacy_screen_results') FROM ps_jobs WHERE tenant_id=42 AND kind='screen'").Scan(&archived); e != nil || archived != 1 {
		t.Fatalf("archived=%d %v", archived, e)
	}
	var profit string
	if e = pool.QueryRow(ctx, "SELECT output->>'profit_usd' FROM ps_profit_scenarios WHERE tenant_id=42").Scan(&profit); e != nil || profit != "10.00" {
		t.Fatal(profit, e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM ps_configs WHERE tenant_id=42 AND kind='profit_template'").Scan(&count); e != nil || count != 1 {
		t.Fatal("template missing", e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM ps_jobs WHERE tenant_id=42 AND kind IN ('review','request')").Scan(&count); e != nil || count != 2 {
		t.Fatal("workflow records missing", e)
	}
	if e = migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
}
