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
