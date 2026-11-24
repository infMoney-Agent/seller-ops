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
	// Excluded products remain available as samples, not review candidates.
	q.where += " AND p.review_status<>'excluded'"
	expanded := v.Get("family_product_id") != ""
	if expanded {
		id := v.Get("family_product_id")
		if !uuidPattern.MatchString(id) {
			return nil, fail(422, "商品ID无效")
		}
		anchor, err := s.product(ctx, actor(r), id)
		if err != nil {
			return nil, err
		}
		if str(anchor["parent_asin"]) == "" {
			q.args = append(q.args, id)
			q.where += fmt.Sprintf(" AND p.id=$%d::uuid", len(q.args))
		} else {
			q.args = append(q.args, anchor["parent_asin"], anchor["marketplace"])
			q.where += fmt.Sprintf(" AND p.parent_asin=$%d AND p.marketplace=$%d", len(q.args)-1, len(q.args))
		}
	}
	counts, e := s.one(ctx, "SELECT jsonb_build_object('products',count(*),'families',count(DISTINCT "+parentFamilySQL+"),'pending',count(DISTINCT "+parentFamilySQL+") FILTER(WHERE p.review_status='pending'),'pending_products',count(*) FILTER(WHERE p.review_status='pending')) FROM ps_products p "+filterJoins(q, false)+" WHERE "+q.where, q.args...)
	if e != nil {
		return nil, e
	}
	total := integer(counts["products"])
	displayed := integer(counts["families"])
	if expanded {
		displayed = total
	}
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
	maxMember := int((displayed + 49) / 50)
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
	itemSQL := "SELECT " + productProjection + " FROM ps_products p " + q.joins + " WHERE " + q.where + " ORDER BY " + familyRankSQL + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args))
	if !expanded {
		itemSQL = "WITH candidates AS (SELECT p.id," + displayChildSalesSQL + " AS sales,(p.review_status='pending') AS pending,count(*) OVER(PARTITION BY " + parentFamilySQL + ") AS family_count,row_number() OVER(PARTITION BY " + parentFamilySQL + " ORDER BY " + familyRankSQL + ") AS family_rank FROM ps_products p " + q.joins + " WHERE " + q.where + "), chosen AS (SELECT * FROM candidates WHERE family_rank=1 ORDER BY pending DESC,sales DESC NULLS LAST,id" + fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(args)-1, len(args)) + ") SELECT " + productProjection + "||jsonb_build_object('family_count',chosen.family_count) FROM chosen JOIN ps_products p ON p.tenant_id=$1 AND p.id=chosen.id " + q.joins + " ORDER BY chosen.pending DESC,chosen.sales DESC NULLS LAST,p.id"
	}
	items, e := s.query(ctx, itemSQL, args...)
	if e != nil {
		return nil, e
	}
	if !expanded {
		if e = s.loadReviewFamilies(ctx, q, items); e != nil {
			return nil, e
		}
	}
	sampleArgs := append(append([]any{}, sq.args...), 50, (sp-1)*50)
	samples, e := s.query(ctx, "SELECT "+productProjection+" FROM ps_products p "+sq.joins+" WHERE "+sq.where+fmt.Sprintf(" ORDER BY p.reviewed_at DESC NULLS LAST,p.id LIMIT $%d OFFSET $%d", len(sampleArgs)-1, len(sampleArgs)), sampleArgs...)
	if e != nil {
		return nil, e
	}
	var group any
	if total > 0 {
		group = Object{"id": shaDigest(encoded(path)), "title": title, "marketplace": "US", "products": total, "display_products": displayed, "pending": counts["pending"], "reviewed": total - integer(counts["pending_products"]), "samples": sampleCount}
	}
	counts["groups"] = 0
	if total > 0 {
		counts["groups"] = 1
	}
	return Object{"groups": Object{"items": []Object{}, "total": 0, "page": 1, "page_size": 20}, "counts": counts, "group": group, "items": items, "page": mp, "page_size": 50, "samples": Object{"items": samples, "total": sampleCount, "page": sp, "page_size": 50}}, nil
}
