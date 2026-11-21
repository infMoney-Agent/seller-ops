package screening

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func workspacePage(raw string) (int, error) {
	if raw == "" {
		return 1, nil
	}
	n, e := strconv.Atoi(raw)
	if e != nil || n < 1 || n > 100000 {
		return 0, fail(422, "分页参数无效")
	}
	return n, nil
}
func (s *service) reviewWorkspace(r *http.Request) (any, error) {
	ctx := r.Context()
	v := r.URL.Query()
	mp, e := workspacePage(v.Get("member_page"))
	if e != nil {
		return nil, e
	}
	sp, e := workspacePage(v.Get("sample_page"))
	if e != nil {
		return nil, e
	}
	q, e := reviewCandidateQuery(r, false)
	if e != nil {
		return nil, e
	}
	counts, e := s.one(ctx, "SELECT jsonb_build_object('products',count(*),'pending',count(*) FILTER(WHERE p.review_status='pending')) FROM ps_products p "+filterJoins(q, false)+" WHERE "+q.where, q.args...)
	if e != nil {
		return nil, e
	}
	total := integer(counts["products"])
	title := "全部商品"
	var path []string
	if v.Get("category_path") != "" {
		if e = json.Unmarshal([]byte(v.Get("category_path")), &path); e != nil {
			return nil, fail(422, "类目路径无效")
		}
		title = strings.Join(categoryDisplayPath(path), " / ")
	}
	// References share the selected category, not a machine-generated type group.
	sampleURL := *r.URL
	sampleValues := url.Values{}
	if len(path) > 0 {
		sampleValues.Set("category_path", string(encoded(path)))
	}
	sampleURL.RawQuery = sampleValues.Encode()
	sampleRequest := r.Clone(ctx)
	sampleRequest.URL = &sampleURL
	sq, e := reviewCandidateQuery(sampleRequest, false)
	if e != nil {
		return nil, e
	}
	sq.where += " AND p.review_status<>'pending'"
	var sampleCount int64
	if e = s.db.QueryRow(ctx, "SELECT count(*) FROM ps_products p WHERE "+sq.where, sq.args...).Scan(&sampleCount); e != nil {
		return nil, e
	}
	maxMember := int((total + 49) / 50)
	if maxMember < 1 {
		maxMember = 1
	}
	if mp > maxMember {
		mp = maxMember
	}
	maxSample := int((sampleCount + 49) / 50)
	if maxSample < 1 {
		maxSample = 1
	}
	if sp > maxSample {
		sp = maxSample
	}
	args := append(append([]any{}, q.args...), 50, (mp-1)*50)
	items, e := s.query(ctx, "SELECT "+productProjection+" FROM ps_products p "+q.joins+" WHERE "+q.where+" ORDER BY (p.review_status='pending') DESC,"+displayChildSalesSQL+fmt.Sprintf(" DESC NULLS LAST,p.asin,p.id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if e != nil {
		return nil, e
	}
	sampleArgs := append(append([]any{}, sq.args...), 50, (sp-1)*50)
	samples, e := s.query(ctx, "SELECT "+productProjection+" FROM ps_products p "+sq.joins+" WHERE "+sq.where+fmt.Sprintf(" ORDER BY p.reviewed_at DESC NULLS LAST,p.id LIMIT $%d OFFSET $%d", len(sampleArgs)-1, len(sampleArgs)), sampleArgs...)
	if e != nil {
		return nil, e
	}
	var group any
	if total > 0 {
		group = Object{"id": shaDigest(encoded(path)), "title": title, "marketplace": "US", "products": total, "pending": counts["pending"], "reviewed": total - integer(counts["pending"]), "samples": sampleCount}
	}
	counts["groups"] = 0
	if total > 0 {
		counts["groups"] = 1
	}
	return Object{"groups": Object{"items": []Object{}, "total": 0, "page": 1, "page_size": 20}, "counts": counts, "group": group, "items": items, "page": mp, "page_size": 50, "samples": Object{"items": samples, "total": sampleCount, "page": sp, "page_size": 50}}, nil
}
