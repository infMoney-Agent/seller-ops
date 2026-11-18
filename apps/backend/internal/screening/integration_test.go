package screening

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

func TestPostgresWorkflow(t *testing.T) {
	raw := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("set SCREENING_TEST_DATABASE_URL to an empty dedicated *_test database")
	}
	if !strings.Contains(raw, "_test") {
		t.Fatal("integration tests require dedicated *_test database")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	var existing int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name LIKE 'ps_%'").Scan(&existing); e != nil {
		t.Fatal(e)
	}
	if existing != 0 {
		t.Fatal("integration fixture must start in an empty test database")
	}
	if e = migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	if e = migrate(ctx, pool); e != nil {
		t.Fatalf("migration replay: %v", e)
	}
	var count int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM information_schema.tables WHERE table_schema='public' AND table_name LIKE 'ps_%'").Scan(&count); e != nil || count != 9 {
		t.Fatalf("business tables=%d error=%v", count, e)
	}
	if _, e = pool.Exec(ctx, "INSERT INTO ps_products(tenant_id,id,asin,parent_asin,title) VALUES(999,$1,'SELF000001','SELF000001','independent product')", uuid()); e != nil {
		t.Fatalf("self parent rejected: %v", e)
	}
	s := &service{db: pool}
	// A terminal task failure must update its import atomically; retry resumes it.
	batchID, jobID, token := uuid(), uuid(), uuid()
	_, e = pool.Exec(ctx, "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping,mapping_hash,state,created_by,source_period) VALUES(888,$1,'retry.csv',repeat('0',64),'test/retry.csv',0,'{}',repeat('0',64),'importing',1,'2026-08')", batchID)
	if e != nil {
		t.Fatal(e)
	}
	_, e = pool.Exec(ctx, "INSERT INTO ps_jobs(tenant_id,id,kind,idempotency_key,payload,created_by,state,attempts,lease_token) VALUES(888,$1::uuid,'import',$1::uuid::text,$2,1,'running',5,$3)", jobID, encoded(Object{"engine": goEngine, "import_id": batchID}), token)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.recordWorkFailure(ctx, 888, jobID, token, Object{"message": "constraint failed"}); e != nil {
		t.Fatal(e)
	}
	var batchState, jobState string
	if e = pool.QueryRow(ctx, "SELECT i.state,j.state FROM ps_imports i JOIN ps_jobs j ON j.tenant_id=i.tenant_id WHERE i.id=$1 AND j.id=$2", batchID, jobID).Scan(&batchState, &jobState); e != nil || batchState != "failed" || jobState != "failed" {
		t.Fatal(batchState, jobState, e)
	}
	if _, e = s.retryJob(ctx, identity{Tenant: 888, User: 1, Role: "owner"}, jobID); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT i.state,j.state FROM ps_imports i JOIN ps_jobs j ON j.tenant_id=i.tenant_id WHERE i.id=$1 AND j.id=$2", batchID, jobID).Scan(&batchState, &jobState); e != nil || batchState != "importing" || jobState != "queued" {
		t.Fatal(batchState, jobState, e)
	}

	a := identity{Tenant: 1001, User: 1, Role: "owner"}
	id := uuid()
	other := uuid()
	for _, p := range []struct {
		tenant int64
		id     string
	}{{a.Tenant, id}, {1002, other}} {
		if _, e = pool.Exec(ctx, "INSERT INTO ps_products(tenant_id,id,asin,title) VALUES($1,$2,'B000000001','vacuum storage bags with hand pump')", p.tenant, p.id); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = s.product(ctx, a, other); e == nil {
		t.Fatal("cross-workspace read succeeded")
	}
	before, e := s.product(ctx, a, id)
	if e != nil {
		t.Fatal(e)
	}
	review := Object{"expected_data_revision": before["data_revision"], "expected_review_revision": before["review_revision"], "status": "excluded", "reason": "人工排除", "note": "保留历史"}
	keyed := a
	keyed.RequestKey = "review-request-1"
	keyed.RequestHash = shaDigest(encoded(review))
	if _, e = s.mutateProduct(ctx, keyed, id, "review", review); e != nil {
		t.Fatal(e)
	}
	if _, e = s.mutateProduct(ctx, keyed, id, "review", review); e != nil {
		t.Fatalf("idempotent replay: %v", e)
	}
	keyed.RequestHash = shaDigest([]byte("different request"))
	if _, e = s.mutateProduct(ctx, keyed, id, "review", review); e == nil {
		t.Fatal("conflicting receipt succeeded")
	}
	if _, e = s.mutateProduct(ctx, a, id, "review", review); e == nil {
		t.Fatal("stale review succeeded")
	}
	if _, e = s.mutateProduct(ctx, a, id, "parameters", Object{"expected_data_revision": 0, "parent_asin": "PARENT0001", "package_weight_g": "100.123456", "units_per_pack": 2}); e != nil {
		t.Fatal(e)
	}
	if _, e = s.saveProfit(ctx, a, id, Object{"expected_data_revision": 1, "inputs": baseInputs()}); e != nil {
		t.Fatal(e)
	}
	var snapshot []byte
	if e = pool.QueryRow(ctx, "SELECT product_snapshot FROM ps_profit_scenarios WHERE tenant_id=$1 AND product_id=$2", a.Tenant, id).Scan(&snapshot); e != nil {
		t.Fatal(e)
	}
	if str(decodeObject(snapshot)["parent_asin"]) != "PARENT0001" {
		t.Fatal("missing parameter snapshot")
	}
	if _, e = s.createConfig(ctx, identity{Tenant: a.Tenant, User: 2, Role: "member"}, "profit/templates", Object{"name": "禁止", "params": baseInputs()}); e == nil {
		t.Fatal("member wrote public template")
	}
	policy, e := s.createConfig(ctx, a, "policies", Object{"name": "基础规则", "config": Object{"exclusion_patterns": Object{"电器或电池": `\belectric\b|battery`}}})
	if e != nil {
		t.Fatal(e)
	}
	pid := policy.(Object)["id"]
	if _, e = pool.Exec(ctx, "UPDATE ps_configs SET active=true WHERE tenant_id=$1 AND id=$2", a.Tenant, pid); e != nil {
		t.Fatal(e)
	}
	created, e := s.enqueueScreen(ctx, a, Object{"product_ids": []any{id}}, "")
	if e != nil {
		t.Fatal(e)
	}
	jid := created.(Object)["job_id"]
	if _, e = pool.Exec(ctx, "UPDATE ps_jobs SET state='running',attempts=1,lease_until=now()-interval '1 minute',lease_token=$3 WHERE tenant_id=$1 AND id=$2", a.Tenant, jid, uuid()); e != nil {
		t.Fatal(e)
	}
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- s.worker(workerCtx) }()
	defer func() { cancel(); <-done }()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var state string
		if e = pool.QueryRow(ctx, "SELECT state FROM ps_jobs WHERE tenant_id=$1 AND id=$2", a.Tenant, jid).Scan(&state); e != nil {
			t.Fatal(e)
		}
		if state == "succeeded" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("screening job did not finish")
		}
		time.Sleep(100 * time.Millisecond)
	}
	after, e := s.product(ctx, a, id)
	if e != nil {
		t.Fatal(e)
	}
	if after["machine_decision"] != "passed" || after["review_status"] != "excluded" || after["review_reason"] != "人工排除" {
		t.Fatalf("machine overwrote human result: %v", after)
	}
	var events int
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM ps_review_events WHERE tenant_id=$1 AND product_id=$2", a.Tenant, id).Scan(&events); e != nil || events != 2 {
		t.Fatalf("review events: %d %v", events, e)
	}
}
