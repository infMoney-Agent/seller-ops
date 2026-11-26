package ecom

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/http/httptest"
	"os"
	ecomv1 "seller-ops/contracts/sellerops/ecom/v1"
	"seller-ops/rpccompat"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type fixtureAdapter struct {
	calls atomic.Int64
	price float64
}

func (f *fixtureAdapter) Product(ctx context.Context, asin string) (Product, error) {
	f.calls.Add(1)
	rank := 100
	return Product{ASIN: asin, Title: "测试商品", Price: f.price, BSR: &rank, Category: "home", ReviewCount: 12, Rating: 4.5, Bullets: []string{"feature"}}, nil
}
func (f *fixtureAdapter) BestSellers(ctx context.Context, c string, n int) ([]Product, error) {
	rank := 1
	return []Product{{ASIN: "B000000001", Title: "榜单商品", Price: 19.99, BSR: &rank, Category: c}}, nil
}
func (f *fixtureAdapter) Reviews(ctx context.Context, id string, p int) ([]Review, error) {
	return []Review{{ASIN: id, Rating: 5, Title: "Good", Body: "Great", Verified: true}, {ASIN: id, Rating: 1, Title: "Bad", Body: "Broke"}}, nil
}
func (f *fixtureAdapter) Campaigns(ctx context.Context) ([]Obj, error) {
	return []Obj{{"campaign_id": "1", "spend": 10, "sales": 40, "metrics_available": true}}, nil
}

type fixtureModel struct{ calls atomic.Int64 }

func (f *fixtureModel) Complete(ctx context.Context, p, system string, temp float64, tokens int) (Obj, error) {
	f.calls.Add(1)
	switch {
	case strings.Contains(system, "product researcher"):
		return Obj{"competition_score": 8.0, "profit_potential_score": 7.0, "trend_score": 6.0, "overall_score": 9.25, "analysis": "模型直接评分", "recommended": true, "tags": []any{"低竞争"}}, nil
	case strings.Contains(system, "copywriter"):
		return Obj{"title": "测试标题", "description": "产品描述", "bullet_points": []any{"a", "b", "c", "d", "e"}, "search_terms": []any{"keyword"}, "seo_score": 8}, nil
	case strings.Contains(system, "review analysis"):
		return Obj{"summary": "有好评和质量问题", "top_pain_points": []any{"容易损坏"}}, nil
	default:
		return Obj{"executive_summary": "优化建议", "keyword_recommendations": []any{}}, nil
	}
}
func signed(s *Service, method, path string, user int64, body any) *http.Request {
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader(jsonBytes(body))
	}
	r := httptest.NewRequest(method, "http://ecom"+path, reader)
	stamp := fmt.Sprint(time.Now().Unix())
	r.Header.Set("x-seller-user", fmt.Sprint(user))
	r.Header.Set("x-ecom-time", stamp)
	mac := hmac.New(sha256.New, []byte(s.secret))
	mac.Write([]byte(strings.Join([]string{stamp, method, path, fmt.Sprint(user)}, "\n")))
	r.Header.Set("x-ecom-signature", hex.EncodeToString(mac.Sum(nil)))
	return r
}
func performRPC(t *testing.T, s *Service, procedure string, user int64, input any, outMsg proto.Message) Obj {
	t.Helper()
	r := signed(s, "POST", RPCPrefix+procedure, user, input)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Connect-Protocol-Version", "1")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("RPC %s HTTP %d %s", procedure, w.Code, w.Body.String())
	}
	if err := protojson.Unmarshal(w.Body.Bytes(), outMsg); err != nil {
		t.Fatal(err)
	}
	data := outMsg.ProtoReflect().Get(outMsg.ProtoReflect().Descriptor().Fields().ByName("data")).Message().Interface()
	projected, err := rpccompat.Legacy(data)
	if err != nil {
		t.Fatal(err)
	}
	var out Obj
	if err = json.Unmarshal(jsonBytes(projected), &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func TestFiveModuleContracts(t *testing.T) {
	adapter := &fixtureAdapter{price: 19.99}
	model := &fixtureModel{}
	s := &Service{adapter: adapter, model: model, secret: strings.Repeat("s", 32)}
	ctx := context.Background()
	out := performRPC(t, s, "ResearchProducts", 1, Obj{"body": Obj{"category": "home", "limit": 3, "min_score": 6}}, &ecomv1.ResearchProductsResponse{})
	results := out["results"].([]any)
	if len(results) != 1 || number(results[0].(map[string]any)["overall_score"]) != 9.25 || adapter.calls.Load() != 0 {
		t.Fatal("research changed model score or fetched product detail", out)
	}
	out = performRPC(t, s, "GenerateListing", 1, Obj{"body": Obj{"keyword": "tool", "asin": "B000000001", "language": "de"}}, &ecomv1.GenerateListingResponse{})
	if out["language"] != "de" || len(out["bullet_points"].([]any)) != 5 {
		t.Fatal(out)
	}
	out = performRPC(t, s, "OptimizeListing", 1, Obj{"body": Obj{"keyword": "tool", "asin": "B000000001", "language": "de"}}, &ecomv1.OptimizeListingResponse{})
	if out["language"] != "de" || len(out["bullet_points"].([]any)) != 5 {
		t.Fatal(out)
	}
	out = performRPC(t, s, "AnalyzeReviews", 1, Obj{"body": Obj{"asin": "B000000001", "max_pages": 2}}, &ecomv1.AnalyzeReviewsResponse{})
	if number(out["total_reviews"]) != 2 || number(out["avg_rating"]) != 3 || number(out["verified_purchase_ratio"]) != 0.5 {
		t.Fatal(out)
	}
	out = performRPC(t, s, "OptimizeAds", 1, Obj{"query": Obj{"target_acos": []string{"20"}}}, &ecomv1.OptimizeAdsResponse{})
	if number(out["overall_acos"]) != 25 || number(out["overall_roas"]) != 4 {
		t.Fatal(out)
	}
	before := model.calls.Load()
	empty, e := s.analyze(ctx, "B000000001", nil)
	if e != nil || number(empty["total_reviews"]) != 0 || model.calls.Load() != before {
		t.Fatal("empty reviews invoked model", e)
	}
	r := signed(s, "POST", RPCPrefix+"ListResearchCategories", 1, Obj{})
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("x-seller-user", "2")
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("forged user accepted")
	}
}
func TestLLMProviders(t *testing.T) {
	for _, provider := range []string{"openai", "anthropic", "gemini", "ollama"} {
		t.Run(provider, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var b Obj
				if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
					t.Fatal(e)
				}
				if r.Method != "POST" {
					t.Error("method")
				}
				switch provider {
				case "openai":
					if r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer test" {
						t.Error("openai contract")
					}
					json.NewEncoder(w).Encode(Obj{"choices": []Obj{{"message": Obj{"content": "```json\n{\"valid\":true}\n```"}}}})
				case "anthropic":
					if r.URL.Path != "/messages" || r.Header.Get("x-api-key") != "test" || b["system"] != "system" {
						t.Error("anthropic contract")
					}
					json.NewEncoder(w).Encode(Obj{"content": []Obj{{"text": "{\"valid\":true}"}}})
				case "gemini":
					if r.URL.Path != "/models/test:generateContent" || r.Header.Get("x-goog-api-key") != "test" {
						t.Error("gemini contract")
					}
					json.NewEncoder(w).Encode(Obj{"candidates": []Obj{{"content": Obj{"parts": []Obj{{"text": "{\"valid\":true}"}}}}}})
				case "ollama":
					if r.URL.Path != "/api/chat" || b["stream"] != false {
						t.Error("ollama contract")
					}
					json.NewEncoder(w).Encode(Obj{"message": Obj{"content": "{\"valid\":true}"}})
				}
			}))
			defer server.Close()
			model := &LLM{provider, "test", "test", server.URL, server.Client()}
			out, e := model.Complete(context.Background(), "prompt", "system", 0.3, 1000)
			if e != nil || out["valid"] != true {
				t.Fatal(out, e)
			}
		})
	}
}
func TestScraperFixtures(t *testing.T) {
	html := `<html><span id="productTitle">Example product</span><span class="a-price"><span class="a-offscreen">$19.99</span></span><div id="detailBulletsWrapper_feature_div">#1,234 in Home (see more)</div><span id="acrCustomerReviewText">1,200 ratings</span><span class="a-icon-alt">4.5 out of 5 stars</span></html>`
	p, e := parseProduct(html, "B000000001", "US")
	if e != nil || p.Price != 19.99 || p.ReviewCount != 1200 || p.BSR == nil || *p.BSR != 1234 {
		t.Fatal(p, e)
	}
	raw := `<div class="zg-grid-general-faceout"><a href="/dp/B000000001"><img alt="Loaded product"></a><span class="zg-bdg-text">#2</span><span class="p13n-sc-price">$12.00</span></div>`
	rows, e := parseBestSellers(raw, "home", "US", 50)
	if e != nil || len(rows) != 1 || rows[0].Title != "Loaded product" {
		t.Fatal(rows, e)
	}
	r, e := parseReviews(`<div data-hook="review" id="r1"><span data-hook="review-star-rating"><span class="a-icon-alt">5.0 out of 5</span></span><span data-hook="review-body">good</span><span data-hook="avp-badge">Verified Purchase</span></div>`, "B000000001")
	if e != nil || len(r) != 1 || !r[0].Verified || r[0].Rating != 5 {
		t.Fatal(r, e)
	}
}
func TestMonitorRedisCompatibility(t *testing.T) {
	raw := os.Getenv("ECOM_TEST_REDIS_URL")
	if raw == "" {
		t.Skip("set ECOM_TEST_REDIS_URL to isolated test Redis DB 15")
	}
	options, e := redis.ParseURL(raw)
	if e != nil || options.DB != 15 {
		t.Fatal("isolated test Redis database 15 required")
	}
	client := redis.NewClient(options)
	defer client.Close()
	s := &Service{redis: client, adapter: &fixtureAdapter{price: 80}, secret: strings.Repeat("s", 32)}
	ctx := context.Background()
	user := time.Now().UnixNano()
	prefix := fmt.Sprintf("seller:%d", user)
	asin := "B000000001"
	keys := []string{prefix + ":monitor:" + asin, prefix + ":snapshots:" + asin}
	defer client.Del(ctx, keys...)
	// Seed exactly the previous Python key format and serialized shape.
	if e = client.Set(ctx, keys[0], `{"asin":"B000000001","platform":"amazon","label":"旧监控","check_interval_minutes":60,"alert_rules":{"price_drop_pct":10}}`, 0).Err(); e != nil {
		t.Fatal(e)
	}
	client.LPush(ctx, keys[1], jsonBytes(Obj{"price": 100, "bsr_rank": 200, "review_count": 10, "timestamp": time.Now().Add(-2 * time.Hour).UTC().Format(time.RFC3339Nano)}))
	out := performRPC(t, s, "ListMonitors", user, Obj{}, &ecomv1.ListMonitorsResponse{})
	if len(out["monitors"].([]any)) != 1 {
		t.Fatal("old monitor missing", out)
	}
	out = performRPC(t, s, "ListMonitors", user+1, Obj{}, &ecomv1.ListMonitorsResponse{})
	if len(out["monitors"].([]any)) != 0 {
		t.Fatal("account isolation failed")
	}
	snapshot, e := s.snapshot(ctx, prefix, asin, true)
	if e != nil || snapshot["alert_triggered"] != true || !strings.Contains(text(snapshot["alert_reason"]), "20.0%") {
		t.Fatal(snapshot, e)
	}
	if snap, e := s.snapshot(ctx, prefix, asin, true); e != nil || snap != nil {
		t.Fatal("interval was ignored", snap, e)
	}
	out = performRPC(t, s, "ListSnapshots", user, Obj{"asin": asin}, &ecomv1.ListSnapshotsResponse{})
	if len(out["snapshots"].([]any)) != 2 {
		t.Fatal(out)
	}
}
