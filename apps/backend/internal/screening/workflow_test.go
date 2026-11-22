package screening

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/xuri/excelize/v2"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseFormatsAndLimits(t *testing.T) {
	book := excelize.NewFile()
	book.SetSheetRow("Sheet1", "A1", &[]any{"ASIN", "商品标题"})
	book.SetSheetRow("Sheet1", "A2", &[]any{"b000000001", "manual vacuum bag with hand pump"})
	buffer, e := book.WriteToBuffer()
	book.Close()
	if e != nil {
		t.Fatal(e)
	}
	for _, file := range []struct {
		name string
		data []byte
	}{{"sample.xlsx", buffer.Bytes()}, {"sample.csv", []byte("ASIN,商品标题\nB000000001,manual tool\n")}} {
		rows := 0
		e = parseSource(file.data, file.name, Object{}, func(row sourceRow) error {
			rows++
			if len(row.Errors) > 0 {
				t.Fatal(row.Errors)
			}
			return nil
		})
		if e != nil || rows != 1 {
			t.Fatal(file.name, rows, e)
		}
	}
	var zipbuf bytes.Buffer
	z := zip.NewWriter(&zipbuf)
	f, _ := z.Create("nested.zip")
	f.Write([]byte("bad"))
	z.Close()
	if e = parseSource(zipbuf.Bytes(), "sample.zip", Object{}, func(sourceRow) error { return nil }); e == nil {
		t.Fatal("nested ZIP accepted")
	}
	if e = parseSource(make([]byte, maxUpload+1), "large.csv", Object{}, func(sourceRow) error { return nil }); e == nil {
		t.Fatal("oversized input accepted")
	}
	if _, e = period(Object{"observed_on": "2026-11-20", "source_period": "2026-10", "period_type": "calendar_month"}, true); e != nil {
		t.Fatal(e)
	}
	if _, e = period(Object{}, true); e == nil {
		t.Fatal("missing observation date accepted")
	}
}
func TestImportHTTPWorkflow(t *testing.T) {
	raw := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("test PostgreSQL required")
	}
	ctx := context.Background()
	admin, e := pgxpool.New(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA screening_http_test"); e != nil {
		t.Fatal(e)
	}
	pc, e := pgxpool.ParseConfig(raw)
	if e != nil {
		t.Fatal(e)
	}
	pc.ConnConfig.RuntimeParams["search_path"] = "screening_http_test"
	pool, e := pgxpool.NewWithConfig(ctx, pc)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	if e = migrate(ctx, pool); e != nil {
		t.Fatal(e)
	}
	s := &service{pool, Config{Storage: t.TempDir(), Secret: strings.Repeat("x", 32)}}
	a := identity{Tenant: 501, User: 7, Role: "owner"}
	handler := s.handler()
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- s.worker(workerCtx) }()
	defer func() { cancel(); <-done }()
	call := func(method, path, content string, data []byte) Object {
		t.Helper()
		r := httptest.NewRequest(method, "http://screening/api/v1/screening/"+path, bytes.NewReader(data))
		r.Header.Set("Content-Type", content)
		stamp := fmt.Sprint(time.Now().Unix())
		r.Header.Set("x-seller-user", "7")
		r.Header.Set("x-seller-workspace", "501")
		r.Header.Set("x-seller-role", "owner")
		r.Header.Set("x-screening-time", stamp)
		r.Header.Set("Idempotency-Key", uuid())
		mac := hmac.New(sha256.New, []byte(s.cfg.Secret))
		mac.Write([]byte(strings.Join([]string{stamp, method, r.URL.RequestURI(), "7", "501", "owner"}, "\n")))
		r.Header.Set("x-screening-signature", hex.EncodeToString(mac.Sum(nil)))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code < 200 || w.Code >= 300 {
			t.Fatalf("%s %s HTTP %d %s", method, path, w.Code, w.Body.String())
		}
		out := decodeObject(w.Body.Bytes())
		return out
	}
	wait := func(id string) Object {
		t.Helper()
		deadline := time.Now().Add(20 * time.Second)
		for {
			job, e := s.one(ctx, "SELECT to_jsonb(j) FROM ps_jobs j WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id)
			if e != nil {
				t.Fatal(e)
			}
			if job["state"] == "succeeded" || job["state"] == "partial" {
				return job
			}
			if job["state"] == "failed" || time.Now().After(deadline) {
				t.Fatalf("job %s failed or timed out: %s", id, encoded(job))
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	upload := func(month, observed, price string) Object {
		t.Helper()
		var table bytes.Buffer
		cw := csv.NewWriter(&table)
		cw.Write([]string{"ASIN", "商品标题", "父ASIN", "商品层级", "销量口径", "月销量", "价格($)", "包装重量(g)", "每套数量"})
		cw.Write([]string{"PARENT0001", "", "", "parent", "parent", "1000", "", "", ""})
		cw.Write([]string{"CHILD00001", "manual kitchen tool", "PARENT0001", "child", "child", "120", price, "150", "2"})
		cw.Write([]string{"CHILD00002", "storage accessory", "PARENT0001", "child", "unknown", "900", "20", "", ""})
		cw.Flush()
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		file, e := writer.CreateFormFile("file", "sample.csv")
		if e != nil {
			t.Fatal(e)
		}
		file.Write(table.Bytes())
		writer.WriteField("source_period", month)
		writer.WriteField("observed_on", observed)
		writer.WriteField("period_type", "calendar_month")
		writer.WriteField("source_type", "sellersprite")
		writer.WriteField("default_sales_scope", "unknown")
		writer.Close()
		job := call("POST", "imports", writer.FormDataContentType(), body.Bytes())
		wait(str(job["job_id"]))
		duplicate := call("POST", "imports", writer.FormDataContentType(), body.Bytes())
		if duplicate["import_id"] != job["import_id"] || duplicate["job_id"] != job["job_id"] {
			t.Fatal("duplicate upload created new batch", duplicate, job)
		}
		commit := call("POST", "imports/"+str(job["import_id"])+"/commit", "application/json", nil)
		again := call("POST", "imports/"+str(job["import_id"])+"/commit", "application/json", nil)
		if again["job_id"] != commit["job_id"] {
			t.Fatal("duplicate commit created new task", again, commit)
		}
		finished := wait(str(commit["job_id"]))
		if screen, ok := finished["result"].(map[string]any)["screen"].(map[string]any); ok {
			wait(str(screen["job_id"]))
		}
		return job
	}
	upload("2026-11", "2026-11-18", "19.99")
	products := call("GET", "products?show_merged=true", "", nil)
	if integer(products["total"]) != 2 {
		t.Fatal("parent created fake product", products)
	}
	var first Object
	for _, raw := range products["items"].([]any) {
		p := raw.(map[string]any)
		if p["asin"] == "CHILD00001" {
			first = p
		}
	}
	if first == nil || str(first["price_usd"]) != "19.990000" || integer(first["monthly_sales"]) != 120 {
		t.Fatal(first)
	}
	folded := call("GET", "products?sort=-price_usd", "", nil)
	if integer(folded["total"]) != 1 {
		t.Fatal("parent family not folded", folded)
	}
	representative := folded["items"].([]any)[0].(map[string]any)
	if str(representative["asin"]) != "CHILD00001" || integer(representative["family_count"]) != 2 || representative["family_sales_pending"] != true {
		t.Fatal("unknown or parent sales used for ranking", representative)
	}
	page1 := call("GET", "products/"+str(first["id"])+"/family?page_size=1&page=1", "", nil)
	page2 := call("GET", "products/"+str(first["id"])+"/family?page_size=1&page=2", "", nil)
	if integer(page1["total"]) != 2 || len(page1["items"].([]any)) != 1 || len(page2["items"].([]any)) != 1 || page1["items"].([]any)[0].(map[string]any)["id"] == page2["items"].([]any)[0].(map[string]any)["id"] {
		t.Fatal("family pagination", page1, page2)
	}
	_, e = pool.Exec(ctx, "UPDATE ps_product_monthly m SET sales_scope='child',monthly_sales=240 FROM ps_products p WHERE p.tenant_id=m.tenant_id AND p.id=m.product_id AND p.tenant_id=$1 AND p.asin='CHILD00002'", a.Tenant)
	if e != nil {
		t.Fatal(e)
	}
	best := call("GET", "products?sort=asin", "", nil)
	if str(best["items"].([]any)[0].(map[string]any)["asin"]) != "CHILD00002" {
		t.Fatal("representative depends on list sort rather than sales", best)
	}
	_, e = pool.Exec(ctx, "UPDATE ps_product_monthly m SET sales_scope='unknown',monthly_sales=900 FROM ps_products p WHERE p.tenant_id=m.tenant_id AND p.id=m.product_id AND p.tenant_id=$1 AND p.asin='CHILD00002'", a.Tenant)
	if e != nil {
		t.Fatal(e)
	}
	family := call("GET", "products/"+str(first["id"])+"/family", "", nil)
	if integer(family["known_child_sales_total"]) != 120 || integer(family["unknown_child_count"]) != 1 || len(family["parent_monthly"].([]any)) != 1 {
		t.Fatal(family)
	}
	preview := call("POST", "review-batches/preview", "application/json", encoded(Object{"product_ids": []any{first["id"]}, "scope": "family", "mode": "direct", "status": "excluded", "reason": "人工排除"}))
	applied := call("POST", "review-batches/"+str(preview["id"])+"/apply", "application/json", nil)
	if integer(applied["success"]) != 2 {
		t.Fatal(applied)
	}
	replayed := call("POST", "review-batches/"+str(preview["id"])+"/apply", "application/json", nil)
	if integer(replayed["success"]) != 2 {
		t.Fatal(replayed)
	}
	after, e := s.product(ctx, a, str(first["id"]))
	if e != nil {
		t.Fatal(e)
	}
	call("PATCH", "products/"+str(first["id"])+"/review", "application/json", encoded(Object{"expected_data_revision": after["data_revision"], "expected_review_revision": after["review_revision"], "status": "kept"}))
	undo := call("POST", "review-batches/"+str(preview["id"])+"/undo", "application/json", nil)
	if integer(undo["success"]) != 1 || integer(undo["skipped"]) != 1 {
		t.Fatal(undo)
	}
	upload("2026-10", "2026-10-31", "10.00")
	current := call("GET", "products/"+str(first["id"]), "", nil)
	if str(current["price_usd"]) != "19.990000" || current["review_status"] != "kept" {
		t.Fatal("old import overwrote current or manual fields", current)
	}
	history := call("GET", "products/"+str(first["id"])+"/monthly", "", nil)
	if integer(history["total"]) != 2 {
		t.Fatal(history)
	}
	selected := call("GET", "products?stat_month=2026-10-01&period_type=calendar_month&show_merged=true", "", nil)
	if integer(selected["total"]) != 2 {
		t.Fatal(selected)
	}
	// An older uploaded batch committed later must lose even with a higher row
	// number and the same observation date. Processing order is not source order.
	tx, e := pool.Begin(ctx)
	if e != nil {
		t.Fatal(e)
	}
	oldBatchID, oldRowID := uuid(), uuid()
	_, e = tx.Exec(ctx, "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,mapping_hash,state,created_by,created_at,source_period,observed_on,source_type,period_type,period_start,period_end) VALUES($1,$2,'older-upload.csv',repeat('e',64),'test/reference',1,repeat('f',64),'completed',7,now()-interval '1 day','2026-11','2026-11-18','sellersprite','calendar_month','2026-11-01','2026-11-30')", a.Tenant, oldBatchID)
	if e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	_, e = tx.Exec(ctx, "INSERT INTO ps_import_rows(tenant_id,id,import_id,row_no,product_id,state,raw_data) VALUES($1,$2,$3,999,$4,'valid','{}')", a.Tenant, oldRowID, oldBatchID, first["id"])
	if e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	var oldRaw []byte
	if e = tx.QueryRow(ctx, "SELECT to_jsonb(i) FROM ps_imports i WHERE tenant_id=$1 AND id=$2", a.Tenant, oldBatchID).Scan(&oldRaw); e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	oldBatch := decodeObject(oldRaw)
	oldMeta, e := period(oldBatch, true)
	if e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	_, e = importObservation(ctx, tx, a, oldBatch, oldMeta, Object{"id": oldRowID}, Object{"asin": "CHILD00001", "title": "Old upload must not win", "listing_role": "child", "parent_asin": "PARENT0001", "sales_scope": "child", "sales_scope_explicit": true, "price_usd": "1.00", "monthly_sales": 9999})
	if e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if _, e = tx.Exec(ctx, "UPDATE ps_import_rows SET state='committed' WHERE tenant_id=$1 AND id=$2", a.Tenant, oldRowID); e != nil {
		tx.Rollback(ctx)
		t.Fatal(e)
	}
	if e = tx.Commit(ctx); e != nil {
		t.Fatal(e)
	}
	late := call("GET", "products/"+str(first["id"]), "", nil)
	if late["price_usd"] != current["price_usd"] || integer(late["data_revision"]) != integer(current["data_revision"]) || integer(late["monthly_revision"]) != integer(current["monthly_revision"]) {
		t.Fatal("late commit overwrote newer source", late)
	}
	scenario := call("POST", "products/"+str(first["id"])+"/profit-scenarios", "application/json", encoded(Object{"expected_data_revision": current["data_revision"], "inputs": baseInputs()}))
	if scenario["output"].(map[string]any)["profit_usd"] != "6.64" {
		t.Fatal(scenario)
	}
	batchInputs := baseInputs()
	batchInputs["sale_price_usd"] = nil
	profitBatch := call("POST", "profit/batches", "application/json", encoded(Object{"product_ids": []any{first["id"]}, "filters": Object{"stat_month": "2026-10-01", "period_type": "calendar_month"}, "inputs": batchInputs}))
	wait(str(profitBatch["job_id"]))
	var batchPrice, month string
	if e = pool.QueryRow(ctx, "SELECT q.inputs->>'sale_price_usd',m.stat_month::text FROM ps_profit_scenarios q JOIN ps_product_monthly m ON m.tenant_id=q.tenant_id AND m.id=q.monthly_id WHERE q.tenant_id=$1 AND q.product_id=$2 ORDER BY q.created_at DESC,q.id DESC LIMIT 1", a.Tenant, first["id"]).Scan(&batchPrice, &month); e != nil || batchPrice != "10.000000" || month != "2026-10-01" {
		t.Fatal("batch profit ignored selected month", batchPrice, month, e)
	}
	job := call("POST", "exports", "application/json", encoded(Object{"product_ids": []any{first["id"]}}))
	wait(str(job["job_id"]))
	var key string
	if e = pool.QueryRow(ctx, "SELECT result->>'storage_key' FROM ps_jobs WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, job["job_id"]).Scan(&key); e != nil {
		t.Fatal(e)
	}
	book, e := excelize.OpenFile(s.cfg.Storage + "/" + key)
	if e != nil {
		t.Fatal(e)
	}
	defer book.Close()
	rows, e := book.GetRows("月份数据")
	if e != nil || len(rows) != 3 {
		t.Fatal(rows, e)
	}
	policy, e := s.createConfig(ctx, a, "policies", Object{"name": "新规则", "config": Object{"exclusion_patterns": Object{"电器或电池": "electric"}}})
	if e != nil {
		t.Fatal(e)
	}
	activation := call("POST", "policies/"+str(policy.(Object)["id"])+"/activate", "application/json", nil)
	wait(str(activation["job_id"]))
	summary := call("GET", "summary?stat_month=2026-10-01", "", nil)
	if integer(summary["products"]) != 2 {
		t.Fatal(summary)
	}

	upload("2024-01", "2024-02-01", "8.00")
	allHistory := call("GET", "products/"+str(first["id"])+"/monthly", "", nil)
	recentHistory := call("GET", "products/"+str(first["id"])+"/monthly?recent_months=12", "", nil)
	if integer(allHistory["total"]) != 3 || integer(recentHistory["total"]) != 2 {
		t.Fatal("annual history filter", allHistory, recentHistory)
	}
	for _, item := range recentHistory["items"].([]any) {
		if str(item.(map[string]any)["stat_month"]) < "2025-12-01" {
			t.Fatal("old month leaked", item)
		}
	}
	// Sort numeric values across all pages, keeping missing values last.
	var source string
	if e = pool.QueryRow(ctx, "SELECT id::text FROM ps_import_rows WHERE tenant_id=501 LIMIT 1").Scan(&source); e != nil {
		t.Fatal(e)
	}
	fixtureIDs := []string{uuid(), uuid(), uuid()}
	for i, pid := range fixtureIDs {
		parent := any(nil)
		if i < 2 {
			parent = "SORTPARENT"
		}
		if _, e = pool.Exec(ctx, "INSERT INTO ps_products(tenant_id,id,asin,title,parent_asin) VALUES(501,$1,$2,'sort fixture',$3)", pid, fmt.Sprintf("SORT%06d", i+1), parent); e != nil {
			t.Fatal(e)
		}
		mid := uuid()
		prices := []any{"2", "10", nil}
		sales := []any{100, 9, nil}
		if _, e = pool.Exec(ctx, "INSERT INTO ps_product_monthly(tenant_id,id,product_id,stat_month,period_type,period_start,period_end,observed_on,source_type,source_row_id,price_usd,monthly_sales,sales_scope) VALUES(501,$1,$2,'2026-10-01','calendar_month','2026-10-01','2026-10-31','2026-11-01','sellersprite',$3,$4,$5,'child')", mid, pid, source, prices[i], sales[i]); e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, "UPDATE ps_products SET current_monthly_id=$1,current_monthly_revision=1 WHERE tenant_id=501 AND id=$2", mid, pid); e != nil {
			t.Fatal(e)
		}
		if i < 2 {
			profit := []string{"1.9", "-2"}[i]
			if _, e = pool.Exec(ctx, "INSERT INTO ps_profit_scenarios(tenant_id,id,product_id,name,inputs,output,formula_version,product_data_revision,created_by,monthly_id,monthly_revision) VALUES(501,$1,$2,'test','{}',$3,'test',0,7,$4,1)", uuid(), pid, encoded(Object{"profit_usd": profit}), mid); e != nil {
				t.Fatal(e)
			}
		}
	}
	for sort, want := range map[string][]int{"price_usd": {0, 1, 2}, "-price_usd": {1, 0, 2}, "monthly_sales": {1, 0, 2}, "-monthly_sales": {0, 1, 2}, "profit_usd": {1, 0, 2}, "-profit_usd": {0, 1, 2}} {
		for page, index := range want {
			result := call("GET", fmt.Sprintf("products?q=sort+fixture&show_merged=true&sort=%s&page_size=1&page=%d", sort, page+1), "", nil)
			if str(result["items"].([]any)[0].(map[string]any)["id"]) != fixtureIDs[index] {
				t.Fatal(sort, page, result)
			}
		}
	}
	summary = call("GET", "summary?q=sort+fixture", "", nil)
	if integer(summary["displayed"]) != 2 || integer(summary["products"]) != 3 || integer(summary["pending_displayed"]) != 2 {
		t.Fatal("grouped counts", summary)
	}

}

func TestExplanatorySheets(t *testing.T) {
	book := excelize.NewFile()
	defer book.Close()
	book.SetSheetRow("Sheet1", "A1", &[]any{"ASIN", "商品标题", "父ASIN"})
	book.SetSheetRow("Sheet1", "A2", &[]any{"B000000001", "manual tool", "B000000001"})
	book.NewSheet("Notes")
	book.SetSheetRow("Notes", "A1", &[]any{"说明"})
	book.SetSheetRow("Notes", "A2", &[]any{"字段解释"})
	book.NewSheet("Bad products")
	book.SetSheetRow("Bad products", "A1", &[]any{"ASIN", "商品标题"})
	book.SetSheetRow("Bad products", "A2", &[]any{"bad", "tool"})
	buf, e := book.WriteToBuffer()
	if e != nil {
		t.Fatal(e)
	}
	rows, invalid, skips := 0, 0, 0
	e = parseSourceReport(buf.Bytes(), "products.xlsx", Object{}, func(r sourceRow) error {
		rows++
		if len(r.Errors) > 0 {
			invalid++
		}
		return nil
	}, func(Object) { skips++ })
	if e != nil || rows != 2 || invalid != 1 || skips != 1 {
		t.Fatalf("rows=%d invalid=%d skipped=%d err=%v", rows, invalid, skips, e)
	}
	if explanatorySheet("Notes", []string{"ASIN", "商品标题"}, Object{}) {
		t.Fatal("product sheet named Notes skipped")
	}
}
