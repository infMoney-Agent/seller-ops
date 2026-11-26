package screening

import (
	"connectrpc.com/connect"
	"context"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/http/httptest"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/rpccompat"
	"strings"
	"testing"
	"time"
)

func testScreeningRPCReads(t *testing.T, svc *service, server *httptest.Server) {
	t.Helper()
	id := uuid()
	if _, err := svc.db.Exec(context.Background(), "INSERT INTO ps_products(tenant_id,id,asin,title)VALUES(501,$1,'RPC0000001','RPC fixture product')", id); err != nil {
		t.Fatal(err)
	}
	legacyPath := "/api/v1/screening/products"
	legacyReq, _ := http.NewRequest("GET", server.URL+legacyPath, nil)
	signImportRequest(legacyReq.Header, "GET", legacyPath, svc.cfg.Secret, 501, fmt.Sprint(time.Now().Unix()))
	legacyResp, err := server.Client().Do(legacyReq)
	if err != nil {
		t.Fatal(err)
	}
	legacyResp.Body.Close()
	if legacyResp.StatusCode != 404 {
		t.Fatalf("legacy screening metadata route still exposed: %d", legacyResp.StatusCode)
	}
	cases := []struct {
		name, input string
		response    proto.Message
	}{
		{"ListProducts", `{}`, &screeningv1.ListProductsResponse{}}, {"GetSummary", `{}`, &screeningv1.GetSummaryResponse{}}, {"GetProduct", string(encoded(Object{"id": id})), &screeningv1.GetProductResponse{}}, {"GetProductFamily", string(encoded(Object{"id": id})), &screeningv1.GetProductFamilyResponse{}},
		{"ListProductMonthly", string(encoded(Object{"id": id})), &screeningv1.ListProductMonthlyResponse{}}, {"ListProductObservations", string(encoded(Object{"id": id})), &screeningv1.ListProductObservationsResponse{}}, {"ListProductHistory", string(encoded(Object{"id": id})), &screeningv1.ListProductHistoryResponse{}}, {"ListProductReferences", string(encoded(Object{"id": id})), &screeningv1.ListProductReferencesResponse{}}, {"ListProfitScenarios", string(encoded(Object{"id": id})), &screeningv1.ListProfitScenariosResponse{}},
		{"ListParentMonthly", `{}`, &screeningv1.ListParentMonthlyResponse{}}, {"ListMonths", `{}`, &screeningv1.ListMonthsResponse{}}, {"ListTrademarkOptions", `{}`, &screeningv1.ListTrademarkOptionsResponse{}}, {"ListReviewCategories", `{}`, &screeningv1.ListReviewCategoriesResponse{}}, {"GetReviewWorkspace", `{}`, &screeningv1.GetReviewWorkspaceResponse{}}, {"ListReviewBatches", `{}`, &screeningv1.ListReviewBatchesResponse{}}, {"ListPolicies", `{}`, &screeningv1.ListPoliciesResponse{}}, {"ListProfitTemplates", `{}`, &screeningv1.ListProfitTemplatesResponse{}}, {"ListProfitProducts", `{}`, &screeningv1.ListProfitProductsResponse{}},
	}
	for _, tc := range cases {
		t.Run("business/"+tc.name, func(t *testing.T) {
			procedure := ScreeningRPCPrefix + tc.name
			req, _ := http.NewRequest("POST", server.URL+procedure, strings.NewReader(tc.input))
			signImportRequest(req.Header, "POST", procedure, svc.cfg.Secret, 501, fmt.Sprint(time.Now().Unix()))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Connect-Protocol-Version", "1")
			result, err := server.Client().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer result.Body.Close()
			if result.StatusCode != 200 {
				var message any
				json.NewDecoder(result.Body).Decode(&message)
				t.Fatal("RPC", result.StatusCode, message)
			}
			var raw json.RawMessage
			if err = json.NewDecoder(result.Body).Decode(&raw); err != nil {
				t.Fatal(err)
			}
			if err = protojson.Unmarshal(raw, tc.response); err != nil {
				t.Fatal(err)
			}
			field := tc.response.ProtoReflect().Descriptor().Fields().ByName("data")
			data := tc.response.ProtoReflect().Get(field).Message().Interface()
			if _, err = rpccompat.Legacy(data); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBuildIdempotencyHash(t *testing.T) {
	body := Object{"status": "kept"}
	if got, want := buildIdempotencyHash("PATCH", "products/abc/review", "?a=1", body), shaDigest(append([]byte("PATCH /api/v1/screening/products/abc/review?a=1\n"), encoded(body)...)); got != want {
		t.Fatalf("patch hash mismatch: got %s want %s", got, want)
	}
	if got, want := buildIdempotencyHash("PATCH", "products/abc/review", "a=1", body), shaDigest(append([]byte("PATCH /api/v1/screening/products/abc/review?a=1\n"), encoded(body)...)); got != want {
		t.Fatalf("normalized query hash mismatch: got %s want %s", got, want)
	}
	if got, want := buildIdempotencyHash("DELETE", "imports/abc", "?audit=1", nil), shaDigest([]byte("DELETE /api/v1/screening/imports/abc?audit=1")); got != want {
		t.Fatalf("delete hash mismatch: got %s want %s", got, want)
	}
}

func TestLegacyScreeningMetadataRoutesReturn404(t *testing.T) {
	svc := &service{cfg: Config{Secret: strings.Repeat("s", 32)}}
	handler := svc.handler()
	id := uuid()
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/v1/screening/products"},
		{"GET", "/api/v1/screening/summary"},
		{"GET", "/api/v1/screening/imports"},
		{"GET", "/api/v1/screening/imports/" + id},
		{"POST", "/api/v1/screening/imports/" + id + "/commit"},
		{"DELETE", "/api/v1/screening/imports/" + id},
		{"PATCH", "/api/v1/screening/products/" + id + "/review"},
		{"POST", "/api/v1/screening/profit/calculate"},
	} {
		req := httptest.NewRequest(tc.method, "http://screening"+tc.path, nil)
		signImportRequest(req.Header, tc.method, tc.path, svc.cfg.Secret, 501, fmt.Sprint(time.Now().Unix()))
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		if w.Code != http.StatusNotFound {
			t.Fatalf("legacy route %s %s returned %d, want 404", tc.method, tc.path, w.Code)
		}
	}
}

func TestRPCResourceFieldsCannotSelectAnotherOperation(t *testing.T) {
	ctx := context.WithValue(context.Background(), identityKey{}, identity{Tenant: 501, User: 7, Role: "member"})
	svc := &service{}
	for _, id := range []string{"", uuid() + "/history", strings.Repeat("x", 36)} {
		req := connect.NewRequest(&screeningv1.GetProductRequest{Id: &id})
		if _, err := svc.GetProduct(ctx, req); connect.CodeOf(err) != connect.CodeInvalidArgument {
			t.Fatal("invalid resource was not rejected", id, err)
		}
	}
	key := strings.Repeat("a", 64) + "/products"
	if _, err := svc.ListGroupProducts(ctx, connect.NewRequest(&screeningv1.ListGroupProductsRequest{GroupKey: &key})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatal("group path injection accepted", err)
	}
}

func TestNumericStringProjection(t *testing.T) {
	original := Object{"total": "9007199254740993", "page": 1, "page_size": 50, "items": []any{}}
	var msg screeningv1.PageProduct
	if err := rpccompat.FromLegacy(&msg, original); err != nil {
		t.Fatal(err)
	}
	result, err := rpccompat.Legacy(&msg)
	if err != nil {
		t.Fatal(err)
	}
	if result["total"] != original["total"] {
		t.Fatalf("numeric string changed: %#v", result["total"])
	}
}
