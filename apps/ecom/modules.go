package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

//go:embed prompts.json
var promptData []byte
var languages = map[string]string{"en": "English (US)", "de": "German", "fr": "French", "es": "Spanish", "it": "Italian", "jp": "Japanese"}

func prompt(module, key string, values map[string]string) string {
	var prompts map[string]map[string]string
	if e := json.Unmarshal(promptData, &prompts); e != nil {
		panic(e)
	}
	out := prompts[module][key]
	for k, v := range values {
		out = strings.ReplaceAll(out, "{"+k+"}", v)
	}
	return strings.ReplaceAll(strings.ReplaceAll(out, "{{", "{"), "}}", "}")
}
func (s *Service) route(w http.ResponseWriter, r *http.Request) (any, error) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/")
	parts := strings.Split(path, "/")
	if len(parts) < 2 {
		return nil, httpError{404, "接口不存在"}
	}
	if parts[0] == "monitor" {
		return s.monitor(w, r, parts[1:])
	}
	if r.Method == "GET" {
		switch path {
		case "product-research/categories":
			keys := []string{}
			for k := range categories {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			return Obj{"categories": keys}, nil
		case "listing/languages":
			return Obj{"languages": languages}, nil
		}
		if len(parts) == 3 && parts[0] == "product-research" && parts[1] == "score" {
			asin, e := validateASIN(parts[2])
			if e != nil {
				return nil, e
			}
			if p := r.URL.Query().Get("platform"); p != "" && p != "amazon" {
				return nil, httpError{422, "不支持的平台"}
			}
			product, e := s.adapter.Product(r.Context(), asin)
			if e != nil {
				return nil, e
			}
			return s.score(r.Context(), product)
		}
	}
	if r.Method == "GET" && len(parts) == 3 && parts[0] == "reviews" && parts[1] == "analyze" {
		asin, e := validateASIN(parts[2])
		if e != nil {
			return nil, e
		}
		pages := 3
		if value := r.URL.Query().Get("max_pages"); value != "" {
			pages, e = strconv.Atoi(value)
			if e != nil || pages < 1 || pages > 20 {
				return nil, httpError{422, "评论页数无效"}
			}
		}
		if value := r.URL.Query().Get("platform"); value != "" && value != "amazon" {
			return nil, httpError{422, "不支持的平台"}
		}
		rows, e := s.adapter.Reviews(r.Context(), asin, pages)
		if e != nil {
			return nil, e
		}
		return s.analyze(r.Context(), asin, rows)
	}
	if r.Method != "POST" {
		return nil, httpError{404, "接口不存在"}
	}
	b := Obj{}
	if path != "ads/optimize" {
		var e error
		b, e = decode(w, r)
		if e != nil {
			return nil, e
		}
	}
	if platform := text(b["platform"]); platform != "" && platform != "amazon" {
		return nil, httpError{422, "不支持的平台"}
	}
	switch path {
	case "product-research/research":
		category := text(b["category"])
		if _, ok := categories[category]; !ok {
			return nil, httpError{422, "不支持的类目"}
		}
		limit := 50
		if b["limit"] != nil {
			limit = int(number(b["limit"]))
		}
		min := 6.0
		if b["min_score"] != nil {
			min = number(b["min_score"])
		}
		if limit < 1 || limit > 100 || min < 0 || min > 10 {
			return nil, httpError{422, "数量或最低分无效"}
		}
		products, e := s.adapter.BestSellers(r.Context(), category, limit)
		if e != nil {
			return nil, e
		}
		results := []Obj{}
		failed := 0
		for _, p := range products {
			if e := r.Context().Err(); e != nil {
				return nil, e
			}
			v, e := s.score(r.Context(), p)
			if e != nil {
				failed++
				continue
			}
			if number(v["overall_score"]) >= min {
				results = append(results, v)
			}
		}
		if len(products) > 0 && failed == len(products) {
			return nil, httpError{502, "商品已采集，但模型评分全部失败，请检查模型配置"}
		}
		sort.SliceStable(results, func(i, j int) bool { return number(results[i]["overall_score"]) > number(results[j]["overall_score"]) })
		return Obj{"category": category, "results": results, "failed": failed}, nil
	case "listing/generate", "listing/optimize":
		return s.listing(r.Context(), path, b)
	case "reviews/analyze":
		asin, e := validateASIN(text(b["asin"]))
		if e != nil {
			return nil, e
		}
		pages := 5
		if b["max_pages"] != nil {
			pages = int(number(b["max_pages"]))
		}
		if pages < 1 || pages > 20 {
			return nil, httpError{422, "评论页数须在1到20之间"}
		}
		rows, e := s.adapter.Reviews(r.Context(), asin, pages)
		if e != nil {
			return nil, e
		}
		return s.analyze(r.Context(), asin, rows)
	case "ads/optimize":
		if p := r.URL.Query().Get("platform"); p != "" && p != "amazon" {
			return nil, httpError{422, "不支持的平台"}
		}
		target := 25.0
		if raw := r.URL.Query().Get("target_acos"); raw != "" {
			var e error
			target, e = strconv.ParseFloat(raw, 64)
			if e != nil || math.IsNaN(target) || math.IsInf(target, 0) || target <= 0 || target > 100 {
				return nil, httpError{422, "目标ACoS须大于0且不超过100"}
			}
		}
		return s.optimizeAds(r.Context(), target)
	}
	return nil, httpError{404, "接口不存在"}
}
func (s *Service) score(ctx context.Context, p Product) (Obj, error) {
	data := Obj{"asin": p.ASIN, "title": p.Title, "price_usd": p.Price, "bsr_rank": p.BSR, "bsr_category": p.Category, "review_count": p.ReviewCount, "rating": p.Rating, "num_bullet_points": len(p.Bullets)}
	raw, e := s.model.Complete(ctx, prompt("research", "ANALYSIS_TEMPLATE", map[string]string{"product_json": string(jsonBytes(data))}), prompt("research", "SYSTEM_PROMPT", nil), 0.3, 4096)
	if e != nil {
		return nil, e
	}
	out := Obj{"asin": p.ASIN, "title": p.Title, "price": p.Price, "bsr_rank": p.BSR, "bsr_category": p.Category, "review_count": p.ReviewCount, "rating": p.Rating, "ai_analysis": text(raw["analysis"]), "recommended": raw["recommended"] == true, "tags": stringsList(raw["tags"])}
	for _, key := range []string{"competition_score", "profit_potential_score", "trend_score", "overall_score"} {
		value, ok := raw[key]
		n, parseErr := strconv.ParseFloat(text(value), 64)
		if !ok || parseErr != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 0 || n > 10 {
			return nil, fmt.Errorf("invalid model score %s", key)
		}
		out[key] = n
	}
	return out, nil
}
func stringsList(value any) []string {
	out := []string{}
	switch rows := value.(type) {
	case []any:
		for _, v := range rows {
			if s, ok := v.(string); ok {
				out = append(out, s)
			}
		}
	case []string:
		return rows
	}
	return out
}
func (s *Service) listing(ctx context.Context, path string, b Obj) (Obj, error) {
	language := text(b["language"])
	if language == "" {
		language = "en"
	}
	name, ok := languages[language]
	if !ok {
		return nil, httpError{422, "不支持的输出语言"}
	}
	market := text(b["marketplace"])
	if market == "" {
		market = "amazon.com"
	}
	asin := ""
	var p string
	if path == "listing/optimize" {
		var e error
		asin, e = validateASIN(text(b["asin"]))
		if e != nil {
			return nil, e
		}
		product, e := s.adapter.Product(ctx, asin)
		if e != nil {
			return nil, e
		}
		p = prompt("listing", "LISTING_TEMPLATE", map[string]string{"product_json": string(jsonBytes(product)), "marketplace": market, "language_name": name}) + "\nOptimize the existing listing above; preserve factual claims. Do not invent competitor data."
	} else {
		details, ok := b["product_details"].(map[string]any)
		if !ok {
			details = Obj{}
		}
		keyword := strings.TrimSpace(text(b["keyword"]))
		if keyword == "" {
			return nil, httpError{422, "关键词不能为空"}
		}
		if details["primary_keyword"] == nil {
			details["primary_keyword"] = keyword
		}
		asin = text(details["asin"])
		p = prompt("listing", "LISTING_TEMPLATE", map[string]string{"product_json": string(jsonBytes(details)), "marketplace": market, "language_name": name})
	}
	raw, e := s.model.Complete(ctx, p, prompt("listing", "SYSTEM_PROMPT", nil), 0.6, 2000)
	if e != nil {
		return nil, e
	}
	title, description := text(raw["title"]), text(raw["description"])
	bullets := stringsList(raw["bullet_points"])
	if len(bullets) != 5 || title == "" {
		return nil, fmt.Errorf("model returned incomplete listing")
	}
	terms := stringsList(raw["search_terms"])
	count := utf8.RuneCountInString(strings.Join(terms, " "))
	if count > 250 {
		count = 250
	}
	return Obj{"asin_reference": asin, "marketplace": market, "language": language, "title": title, "description": description, "bullet_points": bullets, "search_terms": terms, "subject_matter": stringsList(raw["subject_matter"]), "a_plus_draft": text(raw["a_plus_draft"]), "seo_score": number(raw["seo_score"]), "character_counts": Obj{"title": utf8.RuneCountInString(title), "description": utf8.RuneCountInString(description), "search_terms": count}}, nil
}
func (s *Service) analyze(ctx context.Context, asin string, rows []Review) (Obj, error) {
	dist := Obj{}
	sum, verified := 0, 0
	samples := []string{}
	for _, group := range []struct{ low, high, max int }{{4, 5, 16}, {1, 2, 16}, {3, 3, 8}} {
		n := 0
		for _, r := range rows {
			if r.Rating >= group.low && r.Rating <= group.high && n < group.max {
				body := []rune(r.Body)
				if len(body) > 300 {
					body = body[:300]
				}
				samples = append(samples, fmt.Sprintf("[%d★] %s: %s", r.Rating, r.Title, string(body)))
				n++
			}
		}
	}
	for _, r := range rows {
		sum += r.Rating
		if r.Verified {
			verified++
		}
		key := strconv.Itoa(r.Rating)
		dist[key] = int(number(dist[key])) + 1
	}
	avg, ratio := 0.0, 0.0
	if len(rows) > 0 {
		avg = math.Round(float64(sum)/float64(len(rows))*100) / 100
		ratio = math.Round(float64(verified)/float64(len(rows))*1000) / 1000
	}
	out := Obj{"asin": asin, "total_reviews": len(rows), "avg_rating": avg, "rating_distribution": dist, "verified_purchase_ratio": ratio, "sentiment_breakdown": Obj{"positive": 0, "negative": 0, "neutral": 0}, "summary": "未找到评论。"}
	for _, key := range []string{"top_pain_points", "top_praise_points", "improvement_suggestions", "common_keywords", "listing_recommendations"} {
		out[key] = []any{}
	}
	if len(rows) == 0 {
		return out, nil
	}
	p := prompt("reviews", "ANALYSIS_TEMPLATE", map[string]string{"asin": asin, "sample_count": strconv.Itoa(len(samples)), "total": strconv.Itoa(len(rows)), "avg_rating:.1f": fmt.Sprintf("%.1f", avg), "rating_dist": string(jsonBytes(dist)), "review_samples": strings.Join(samples, "\n\n")})
	raw, e := s.model.Complete(ctx, p, prompt("reviews", "SYSTEM_PROMPT", nil), 0.3, 2500)
	if e != nil {
		return nil, e
	}
	for _, key := range []string{"sentiment_breakdown", "top_pain_points", "top_praise_points", "improvement_suggestions", "common_keywords", "summary", "listing_recommendations"} {
		if v, ok := raw[key]; ok {
			out[key] = v
		}
	}
	return out, nil
}
func (s *Service) optimizeAds(ctx context.Context, target float64) (Obj, error) {
	campaigns, e := s.adapter.Campaigns(ctx)
	if e != nil {
		return nil, e
	}
	out := Obj{"profile_id": osEnv("AMAZON_ADS_PROFILE_ID"), "campaign_count": len(campaigns), "total_spend": 0, "total_sales": 0, "overall_acos": 0, "overall_roas": 0, "keyword_recommendations": []any{}, "budget_recommendations": []any{}, "negative_keyword_suggestions": []any{}, "executive_summary": "未找到广告活动。", "estimated_monthly_savings": 0, "estimated_monthly_sales_increase": 0}
	if len(campaigns) == 0 {
		return out, nil
	}
	metrics := true
	for _, c := range campaigns {
		if c["metrics_available"] == false {
			metrics = false
		}
	}
	if !metrics {
		out["metrics_available"] = false
		out["executive_summary"] = "已读取广告活动配置，但尚无花费、销售和关键词绩效数据，暂不生成出价或收益预测。"
		return out, nil
	}
	spend, sales := 0.0, 0.0
	for _, c := range campaigns {
		spend += number(c["spend"])
		sales += number(c["sales"])
	}
	out["total_spend"] = spend
	out["total_sales"] = sales
	if sales > 0 {
		out["overall_acos"] = spend / sales * 100
	}
	if spend > 0 {
		out["overall_roas"] = sales / spend
	}
	p := prompt("ads", "OPTIMIZATION_TEMPLATE", map[string]string{"campaigns_json": string(jsonBytes(campaigns)), "target_acos": fmt.Sprint(target)})
	raw, e := s.model.Complete(ctx, p, prompt("ads", "SYSTEM_PROMPT", nil), 0.3, 3000)
	if e != nil {
		return nil, e
	}
	for _, key := range []string{"keyword_recommendations", "budget_recommendations", "negative_keyword_suggestions", "executive_summary", "estimated_monthly_savings", "estimated_monthly_sales_increase"} {
		if v, ok := raw[key]; ok {
			out[key] = v
		}
	}
	return out, nil
}
