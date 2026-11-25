package ecom

import (
	"bytes"
	"encoding/json"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/http/httptest"
	"reflect"
	ecomv1 "seller-ops/contracts/sellerops/ecom/v1"
	"seller-ops/rpccompat"
	"sort"
	"strings"
	"testing"
)

func TestEcomRPCParity(t *testing.T) {
	s := &Service{adapter: &fixtureAdapter{price: 19.99}, model: &fixtureModel{}, secret: strings.Repeat("s", 32)}
	keys := []string{}
	for k := range categories {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	cases := []struct {
		name, method, path, input string
		body                      any
		response                  proto.Message
	}{
		{"ListResearchCategories", "GET", "product-research/categories", `{}`, nil, &ecomv1.ListResearchCategoriesResponse{}},
		{"ListListingLanguages", "GET", "listing/languages", `{}`, nil, &ecomv1.ListListingLanguagesResponse{}},
		{"ScoreProduct", "GET", "product-research/score/B000000001", `{"asin":"B000000001"}`, nil, &ecomv1.ScoreProductResponse{}},
		{"ResearchProducts", "POST", "product-research/research", string(jsonBytes(Obj{"body": Obj{"category": keys[0], "limit": 2}})), Obj{"category": keys[0], "limit": 2}, &ecomv1.ResearchProductsResponse{}},
		{"GenerateListing", "POST", "listing/generate", `{"body":{"keyword":"tool"}}`, Obj{"keyword": "tool"}, &ecomv1.GenerateListingResponse{}},
		{"OptimizeListing", "POST", "listing/optimize", `{"body":{"asin":"B000000001"}}`, Obj{"asin": "B000000001"}, &ecomv1.OptimizeListingResponse{}},
		{"AnalyzeReviews", "POST", "reviews/analyze", `{"body":{"asin":"B000000001"}}`, Obj{"asin": "B000000001"}, &ecomv1.AnalyzeReviewsResponse{}},
		{"OptimizeAds", "POST", "ads/optimize", `{}`, nil, &ecomv1.OptimizeAdsResponse{}},
	}
	handler := s.Handler()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			legacy := httptest.NewRecorder()
			handler.ServeHTTP(legacy, signed(s, tc.method, "/api/v1/"+tc.path, 1, tc.body))
			if legacy.Code != 200 {
				t.Fatal("legacy", legacy.Code, legacy.Body.String())
			}
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
			var want, got any
			json.Unmarshal(legacy.Body.Bytes(), &want)
			json.Unmarshal(jsonBytes(projected), &got)
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("projection mismatch %s %s", legacy.Body.String(), jsonBytes(projected))
			}
		})
	}
	req := httptest.NewRequest(http.MethodPost, RPCPrefix+"ListMonitors", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != 401 {
		t.Fatal("unsigned RPC accepted", w.Code)
	}
}
