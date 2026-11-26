package ecom

import (
	"bytes"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/http/httptest"
	ecomv1 "seller-ops/contracts/sellerops/ecom/v1"
	"seller-ops/rpccompat"
	"sort"
	"strings"
	"testing"
)

func TestEcomRPCContractsAndLegacyRemoved(t *testing.T) {
	s := &Service{adapter: &fixtureAdapter{price: 19.99}, model: &fixtureModel{}, secret: strings.Repeat("s", 32)}
	keys := []string{}
	for k := range categories {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cases := []struct {
		name, input string
		response    proto.Message
		check       func(t *testing.T, projected Obj)
	}{
		{"ListResearchCategories", `{}`, &ecomv1.ListResearchCategoriesResponse{}, func(t *testing.T, p Obj) {
			if len(p["categories"].([]any)) != len(keys) {
				t.Fatal("unexpected categories", p)
			}
		}},
		{"ListListingLanguages", `{}`, &ecomv1.ListListingLanguagesResponse{}, func(t *testing.T, p Obj) {
			if len(p["languages"].(map[string]any)) == 0 {
				t.Fatal("missing languages", p)
			}
		}},
		{"ScoreProduct", `{"asin":"B000000001"}`, &ecomv1.ScoreProductResponse{}, func(t *testing.T, p Obj) {
			if p["asin"] != "B000000001" {
				t.Fatal("unexpected score", p)
			}
		}},
		{"ResearchProducts", string(jsonBytes(Obj{"body": Obj{"category": keys[0], "limit": 2}})), &ecomv1.ResearchProductsResponse{}, func(t *testing.T, p Obj) {
			if p["category"] != keys[0] {
				t.Fatal("unexpected research", p)
			}
		}},
		{"GenerateListing", `{"body":{"keyword":"tool"}}`, &ecomv1.GenerateListingResponse{}, func(t *testing.T, p Obj) {
			if p["title"] == "" {
				t.Fatal("empty title", p)
			}
		}},
		{"OptimizeListing", `{"body":{"asin":"B000000001"}}`, &ecomv1.OptimizeListingResponse{}, func(t *testing.T, p Obj) {
			if p["asin_reference"] != "B000000001" {
				t.Fatal("unexpected optimize listing", p)
			}
		}},
		{"AnalyzeReviews", `{"body":{"asin":"B000000001"}}`, &ecomv1.AnalyzeReviewsResponse{}, func(t *testing.T, p Obj) {
			if p["asin"] != "B000000001" {
				t.Fatal("unexpected reviews", p)
			}
		}},
		{"AnalyzeReviewsLegacy", `{"asin":"B000000001"}`, &ecomv1.AnalyzeReviewsLegacyResponse{}, func(t *testing.T, p Obj) {
			if p["asin"] != "B000000001" {
				t.Fatal("unexpected legacy reviews", p)
			}
		}},
		{"OptimizeAds", `{}`, &ecomv1.OptimizeAdsResponse{}, func(t *testing.T, p Obj) {
			if number(p["campaign_count"]) != 1 {
				t.Fatal("unexpected ads", p)
			}
		}},
	}
	handler := s.Handler()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			procedure := RPCPrefix + tc.name
			req := httptest.NewRequest("POST", procedure, bytes.NewBufferString(tc.input))
			req.Header = signed(s, "POST", procedure, 1, nil).Header
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Connect-Protocol-Version", "1")
			result := httptest.NewRecorder()
			handler.ServeHTTP(result, req)
			if result.Code != 200 {
				t.Fatal("RPC", result.Code, result.Body.String())
			}
			if err := protojson.Unmarshal(result.Body.Bytes(), tc.response); err != nil {
				t.Fatal(err)
			}
			data := tc.response.ProtoReflect().Get(tc.response.ProtoReflect().Descriptor().Fields().ByName("data")).Message().Interface()
			projected, err := rpccompat.Legacy(data)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, projected)
		})
	}
	legacy := httptest.NewRecorder()
	handler.ServeHTTP(legacy, signed(s, "GET", "/api/v1/product-research/categories", 1, nil))
	if legacy.Code != 404 {
		t.Fatal("legacy ecom HTTP endpoint still exposed", legacy.Code)
	}
	req := httptest.NewRequest(http.MethodPost, RPCPrefix+"ListMonitors", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("unsigned RPC accepted", w.Code)
	}
}

func TestRPCResourceASINRejectsPathSegments(t *testing.T) {
	svc := &Service{secret: strings.Repeat("s", 32)}
	procedure := RPCPrefix + "ScoreProduct"
	for _, asin := range []string{"", "B000000001/other", "not-an-asin"} {
		r := httptest.NewRequest("POST", procedure, bytes.NewReader(jsonBytes(Obj{"asin": asin})))
		r.Header = signed(svc, "POST", procedure, 1, nil).Header
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		svc.Handler().ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("invalid ASIN accepted", asin, w.Code, w.Body.String())
		}
	}
}
