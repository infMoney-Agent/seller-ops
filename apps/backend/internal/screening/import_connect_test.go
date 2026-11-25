package screening

import (
	"connectrpc.com/connect"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	commonv1 "seller-ops/contracts/sellerops/common/v1"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/contracts/sellerops/screening/v1/screeningv1connect"
	"strings"
	"testing"
	"time"
)

func signImportRequest(headers http.Header, method, path, secret string, tenant int64, stamp string) {
	headers.Set("x-seller-user", "7")
	headers.Set("x-seller-workspace", fmt.Sprint(tenant))
	headers.Set("x-seller-role", "member")
	headers.Set("x-screening-time", stamp)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{stamp, method, path, "7", fmt.Sprint(tenant), "member"}, "\n")))
	headers.Set("x-screening-signature", hex.EncodeToString(mac.Sum(nil)))
}
func TestImportsConnectIdentity(t *testing.T) {
	svc := &service{cfg: Config{Secret: strings.Repeat("s", 32)}}
	server := httptest.NewServer(svc.handler())
	defer server.Close()
	client := screeningv1connect.NewScreeningImportServiceClient(server.Client(), server.URL, connect.WithProtoJSON())
	for _, kind := range []string{"unsigned", "forged", "expired"} {
		req := connect.NewRequest(&screeningv1.ListImportsRequest{})
		if kind != "unsigned" {
			stamp := fmt.Sprint(time.Now().Unix())
			if kind == "expired" {
				stamp = fmt.Sprint(time.Now().Add(-2 * time.Minute).Unix())
			}
			signImportRequest(req.Header(), "POST", screeningv1connect.ScreeningImportServiceListImportsProcedure, svc.cfg.Secret, 501, stamp)
			if kind == "forged" {
				req.Header().Set("x-seller-workspace", "502")
			}
		}
		_, err := client.ListImports(context.Background(), req)
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Fatalf("%s: %v", kind, err)
		}
	}
}
func TestImportReadsConnectIdentity(t *testing.T) {
	svc := &service{cfg: Config{Secret: strings.Repeat("s", 32)}}
	server := httptest.NewServer(svc.handler())
	defer server.Close()
	client := screeningv1connect.NewScreeningImportServiceClient(server.Client(), server.URL, connect.WithProtoJSON())
	for _, method := range []string{"detail", "rows", "job"} {
		for _, kind := range []string{"unsigned", "forged", "expired", "wrong-procedure"} {
			t.Run(method+"/"+kind, func(t *testing.T) {
				sign := func(h http.Header, path string) {
					if kind == "unsigned" {
						return
					}
					stamp := fmt.Sprint(time.Now().Unix())
					if kind == "expired" {
						stamp = fmt.Sprint(time.Now().Add(-2 * time.Minute).Unix())
					}
					if kind == "wrong-procedure" {
						path = screeningv1connect.ScreeningImportServiceListImportsProcedure
					}
					signImportRequest(h, "POST", path, svc.cfg.Secret, 501, stamp)
					if kind == "forged" {
						h.Set("x-seller-workspace", "502")
					}
				}
				var err error
				switch method {
				case "detail":
					req := connect.NewRequest(&screeningv1.GetImportRequest{Id: uuid()})
					sign(req.Header(), screeningv1connect.ScreeningImportServiceGetImportProcedure)
					_, err = client.GetImport(context.Background(), req)
				case "rows":
					req := connect.NewRequest(&screeningv1.ListImportRowsRequest{ImportId: uuid()})
					sign(req.Header(), screeningv1connect.ScreeningImportServiceListImportRowsProcedure)
					_, err = client.ListImportRows(context.Background(), req)
				case "job":
					req := connect.NewRequest(&screeningv1.GetJobRequest{Id: uuid()})
					sign(req.Header(), screeningv1connect.ScreeningImportServiceGetJobProcedure)
					_, err = client.GetJob(context.Background(), req)
				}
				if connect.CodeOf(err) != connect.CodeUnauthenticated {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestImportsConnectParity(t *testing.T) {
	raw := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("isolated PostgreSQL required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(parsed.Path, "_test") {
		t.Fatal("isolated test database required")
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err = admin.Exec(ctx, "CREATE SCHEMA proto_import_test"); err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "proto_import_test"
	db, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err = migrate(ctx, db); err != nil {
		t.Fatal(err)
	}
	states := []string{"uploaded", "parsing", "preview", "importing", "completed", "partial", "failed"}
	for i := 0; i < 107; i++ {
		tenant := int64(501)
		counts := Object{"total": 0, "valid": 0, "headers": []string{"ASIN", "商品标题"}, "future": Object{"note": "保留扩展"}}
		if i == 105 {
			tenant = 502
		}
		if i == 106 {
			counts["data_deleted_at"] = "2026-08-01T00:00:00Z"
		}
		var observed any
		if i%2 == 0 {
			observed = "2026-08-03"
		}
		_, err = db.Exec(ctx, "INSERT INTO ps_imports(tenant_id,id,file_name,file_sha256,storage_key,file_size,state,mapping_hash,created_by,source_period,observed_on,counts,mapping,created_at) VALUES($1,$2,$3,$4,'source.xlsx',30,$5,$6,7,'2026-08',$7,$8::jsonb,$9::jsonb,'2026-08-03T03:04:05.123456+08:00')", tenant, uuid(), fmt.Sprintf("商品-%03d.xlsx", i), fmt.Sprintf("%064x", i+1), states[i%len(states)], strings.Repeat("a", 64), observed, encoded(counts), encoded(Object{"asin": "ASIN"}))
		if err != nil {
			t.Fatal(err)
		}
	}
	svc := &service{db: db, cfg: Config{Secret: strings.Repeat("s", 32)}}
	server := httptest.NewServer(svc.handler())
	defer server.Close()
	client := screeningv1connect.NewScreeningImportServiceClient(server.Client(), server.URL, connect.WithProtoJSON())
	testImportReadRPC(t, svc, server, client)
	testImportMutationRPC(t, svc, server, client)
 testScreeningRPCReadParity(t,svc,server)
	for _, tc := range []struct {
		p, n   int32
		tenant int64
	}{{0, 0, 501}, {1, 1000, 501}, {2, 50, 501}, {3, 50, 501}, {1, 50, 502}, {1, 50, 503}, {-2, -1, 501}} {
		req := connect.NewRequest(&screeningv1.ListImportsRequest{Pagination: &commonv1.PaginationRequest{Page: tc.p, PageSize: tc.n}})
		signImportRequest(req.Header(), "POST", screeningv1connect.ScreeningImportServiceListImportsProcedure, svc.cfg.Secret, tc.tenant, fmt.Sprint(time.Now().Unix()))
		response, err := client.ListImports(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		path := fmt.Sprintf("/api/v1/screening/imports?page=%d&page_size=%d", tc.p, tc.n)
		rest, _ := http.NewRequest("GET", server.URL+path, nil)
		signImportRequest(rest.Header, "GET", path, svc.cfg.Secret, tc.tenant, fmt.Sprint(time.Now().Unix()))
		r, err := server.Client().Do(rest)
		if err != nil {
			t.Fatal(err)
		}
		var old any
		err = json.NewDecoder(r.Body).Decode(&old)
		r.Body.Close()
		if err != nil || r.StatusCode != 200 {
			t.Fatal(err, r.StatusCode)
		}
		oracle, oracleErr := svc.list(rest, "ps_imports", "tenant_id=$1 AND NOT(counts ? 'data_deleted_at')", tc.tenant)
		if oracleErr != nil {
			t.Fatal(oracleErr)
		}
		var original any
		json.Unmarshal(encoded(oracle), &original)
		if !reflect.DeepEqual(old, original) {
			t.Fatal("legacy HTTP projection changed")
		}
		msg := response.Msg
		items := []Object{}
		for _, item := range msg.Items {
			var state string
			for name, value := range importStates {
				if value == item.State {
					state = name
				}
			}
			record := ImportListRecord{ID: item.Id, FileName: item.FileName, State: state, Marketplace: item.Marketplace, SourcePeriod: item.SourcePeriod, SourceType: item.SourceType, PeriodType: item.PeriodType, DefaultSalesScope: item.DefaultSalesScope, ObservedOn: item.ObservedOn, PeriodStart: item.PeriodStart, PeriodEnd: item.PeriodEnd, Counts: item.Counts.AsMap(), Mapping: item.Mapping.AsMap(), LegacyFields: item.LegacyFields.AsMap()}
			items = append(items, record.legacy())
			if item.CreatedAt == nil || item.CreatedAt.CheckValid() != nil {
				t.Fatal("timestamp missing")
			}
		}
		projected := Object{"items": items, "page": msg.Pagination.Page, "page_size": msg.Pagination.PageSize, "total": msg.Pagination.Total}
		var got any
		json.Unmarshal(encoded(projected), &got)
		if !reflect.DeepEqual(old, got) {
			t.Fatalf("REST/RPC mismatch page=%d size=%d tenant=%d", tc.p, tc.n, tc.tenant)
		}
		if tc.tenant == 501 && msg.Pagination.Total != 105 {
			t.Fatal("deleted or other tenant row leaked", msg.Pagination.Total)
		}
	}
}

func legacyRPCImport(item *screeningv1.ImportRecord) Object {
	state := ""
	for k, v := range importStates {
		if v == item.State {
			state = k
		}
	}
	return (ImportListRecord{ID: item.Id, FileName: item.FileName, State: state, Marketplace: item.Marketplace, SourcePeriod: item.SourcePeriod, SourceType: item.SourceType, PeriodType: item.PeriodType, DefaultSalesScope: item.DefaultSalesScope, ObservedOn: item.ObservedOn, PeriodStart: item.PeriodStart, PeriodEnd: item.PeriodEnd, Counts: item.Counts.AsMap(), Mapping: item.Mapping.AsMap(), LegacyFields: item.LegacyFields.AsMap()}).legacy()
}
func testImportReadRPC(t *testing.T, svc *service, server *httptest.Server, client screeningv1connect.ScreeningImportServiceClient) {
	t.Helper()
	ctx := context.Background()
	var id, deleted string
	if err := svc.db.QueryRow(ctx, "SELECT id::text FROM ps_imports WHERE tenant_id=501 AND file_name='商品-000.xlsx'").Scan(&id); err != nil {
		t.Fatal(err)
	}
	if err := svc.db.QueryRow(ctx, "SELECT id::text FROM ps_imports WHERE tenant_id=501 AND counts ? 'data_deleted_at'").Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	for i, state := range []string{"valid", "invalid", "committed"} {
		_, err := svc.db.Exec(ctx, "INSERT INTO ps_import_rows(tenant_id,id,import_id,member_path,sheet_name,row_no,state,raw_data,normalized_data,errors) VALUES(501,$1,$2,'member.csv','中文表',$3,$4,$5::jsonb,$6::jsonb,$7::jsonb)", uuid(), id, i+2, state, encoded(Object{"ASIN": "B0TEST0001", "价格": "19.990000", "blank": nil, "zero": 0}), encoded(Object{"price_usd": "19.990000"}), encoded([]Object{{"message": "缺少商品标题", "column": "商品标题"}}))
		if err != nil {
			t.Fatal(err)
		}
	}
	jobIDs := []string{}
	for _, state := range []string{"queued", "running", "succeeded", "partial", "failed", "preview", "applied", "undone"} {
		job := uuid()
		jobIDs = append(jobIDs, job)
		_, err := svc.db.Exec(ctx, "INSERT INTO ps_jobs(tenant_id,id,kind,state,idempotency_key,payload,result,error,progress,attempts,created_by,lease_token,request_hash) VALUES(501,$1,'parse',$2,$1::uuid::text,$3::jsonb,$4::jsonb,$5::jsonb,0,0,7,$6,'private-hash')", job, state, encoded(Object{"import_id": id, "internal": "never expose"}), encoded(Object{"count": 0}), encoded(Object{"message": "错误详情"}), uuid())
		if err != nil {
			t.Fatal(err)
		}
	}
	hidden := uuid()
	if _, err := svc.db.Exec(ctx, "INSERT INTO ps_jobs(tenant_id,id,kind,idempotency_key,created_by) VALUES(501,$1,'request',$1::uuid::text,7)", hidden); err != nil {
		t.Fatal(err)
	}
	type check struct {
		kind, id, query string
		tenant          int64
		status          int
	}
	cases := []check{{"detail", id, "", 501, 200}, {"rows", id, "?page=2&page_size=2", 501, 200}, {"rows", id, "?page=99&page_size=1000", 501, 200}, {"rows", id, "", 501, 200}, {"detail", id, "", 502, 404}, {"rows", id, "", 502, 404}, {"detail", deleted, "", 501, 404}, {"rows", deleted, "", 501, 404}, {"detail", uuid(), "", 501, 404}, {"detail", "bad-id", "", 501, 422}, {"rows", "bad-id", "", 501, 422}, {"job", hidden, "", 501, 404}, {"job", jobIDs[0], "", 502, 404}, {"job", "bad-id", "", 501, 422}}
	for _, job := range jobIDs {
		cases = append(cases, check{"job", job, "", 501, 200})
	}
	for _, tc := range cases {
		t.Run(tc.kind+"/"+tc.id+tc.query+fmt.Sprint(tc.tenant), func(t *testing.T) {
			sign := func(h http.Header, path string) {
				signImportRequest(h, "POST", path, svc.cfg.Secret, tc.tenant, fmt.Sprint(time.Now().Unix()))
			}
			var got any
			var err error
			restPath := "/api/v1/screening/imports/" + tc.id
			switch tc.kind {
			case "detail":
				req := connect.NewRequest(&screeningv1.GetImportRequest{Id: tc.id})
				sign(req.Header(), screeningv1connect.ScreeningImportServiceGetImportProcedure)
				var out *connect.Response[screeningv1.GetImportResponse]
				out, err = client.GetImport(ctx, req)
				if err == nil {
					got = legacyRPCImport(out.Msg.Item)
				}
			case "rows":
				restPath += "/preview"
				p, n := page(&http.Request{URL: mustImportURL(tc.query)})
				req := connect.NewRequest(&screeningv1.ListImportRowsRequest{ImportId: tc.id, Pagination: &commonv1.PaginationRequest{Page: int32(p), PageSize: int32(n)}})
				sign(req.Header(), screeningv1connect.ScreeningImportServiceListImportRowsProcedure)
				var out *connect.Response[screeningv1.ListImportRowsResponse]
				out, err = client.ListImportRows(ctx, req)
				if err == nil {
					items := []Object{}
					for _, row := range out.Msg.Items {
						item := row.LegacyFields.AsMap()
						item["id"] = row.Id
						item["import_id"] = row.ImportId
						item["member_path"] = row.MemberPath
						item["sheet_name"] = row.SheetName
						item["row_no"] = row.RowNo
						item["product_id"] = nil
						if row.ProductId != nil {
							item["product_id"] = *row.ProductId
						}
						for k, v := range importRowStates {
							if v == row.State {
								item["state"] = k
							}
						}
						item["raw_data"] = row.RawData.AsMap()
						items = append(items, item)
					}
					got = Object{"items": items, "page": out.Msg.Pagination.Page, "page_size": out.Msg.Pagination.PageSize, "total": out.Msg.Pagination.Total}
				}
			case "job":
				restPath = "/api/v1/screening/jobs/" + tc.id
				req := connect.NewRequest(&screeningv1.GetJobRequest{Id: tc.id})
				sign(req.Header(), screeningv1connect.ScreeningImportServiceGetJobProcedure)
				var out *connect.Response[screeningv1.GetJobResponse]
				out, err = client.GetJob(ctx, req)
				if err == nil {
					item := out.Msg.Item
					v := item.LegacyFields.AsMap()
					v["id"] = item.Id
					v["kind"] = item.Kind
					v["progress"] = item.Progress
					v["attempts"] = item.Attempts
					for k, state := range jobStates {
						if state == item.State {
							v["state"] = k
						}
					}
					for _, private := range []string{"payload", "lease_token", "request_hash"} {
						if _, ok := v[private]; ok {
							t.Fatal("private field leaked", private)
						}
					}
					got = v
				}
			}
			restPath += tc.query
			rest, _ := http.NewRequest("GET", server.URL+restPath, nil)
			signImportRequest(rest.Header, "GET", restPath, svc.cfg.Secret, tc.tenant, fmt.Sprint(time.Now().Unix()))
			res, e := server.Client().Do(rest)
			if e != nil {
				t.Fatal(e)
			}
			defer res.Body.Close()
			if res.StatusCode != tc.status {
				t.Fatal("REST status", res.StatusCode, tc.status)
			}
			if tc.status != 200 {
				want := connect.CodeNotFound
				if tc.status == 422 {
					want = connect.CodeInvalidArgument
				}
				if connect.CodeOf(err) != want {
					t.Fatalf("RPC error %v want %v", err, want)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var old, projected any
			if e = json.NewDecoder(res.Body).Decode(&old); e != nil {
				t.Fatal(e)
			}
			json.Unmarshal(encoded(got), &projected)
			if !reflect.DeepEqual(old, projected) {
				t.Fatalf("projection mismatch old=%s got=%s", encoded(old), encoded(projected))
			}
		})
	}
}
func mustImportURL(query string) *url.URL { u, _ := url.Parse("http://test/" + query); return u }
