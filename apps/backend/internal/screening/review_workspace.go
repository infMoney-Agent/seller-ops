package screening

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// Type browsing intentionally spans sellers; it does not change merge relations
// and never writes a human conclusion without an explicit batch preview.
const reviewTypeTitleSQL = "COALESCE(NULLIF(p.machine_direction,''),NULLIF(p.manual_category,''),'待初筛 / '||p.asin)"
const reviewTypeKeySQL = "md5(jsonb_build_array(p.marketplace," + reviewTypeTitleSQL + ")::text)"

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
	a, ctx := actor(r), r.Context()
	v := r.URL.Query()
	gp, e := workspacePage(v.Get("group_page"))
	if e != nil {
		return nil, e
	}
	mp, e := workspacePage(v.Get("member_page"))
	if e != nil {
		return nil, e
	}
	sp, e := workspacePage(v.Get("sample_page"))
	if e != nil {
		return nil, e
	}
	chosen := v.Get("group_id")
	if chosen != "" && (len(chosen) != 32 || strings.Trim(chosen, "0123456789abcdef") != "") {
		return nil, fail(422, "类型组无效")
	}
	q, e := reviewCandidateQuery(r, false)
	if e != nil {
		return nil, e
	}
	from := " FROM ps_products p " + filterJoins(q, false) + " WHERE " + q.where
	counts, e := s.one(ctx, "SELECT jsonb_build_object('products',count(*),'groups',count(DISTINCT "+reviewTypeKeySQL+"),'pending',count(*) FILTER(WHERE p.review_status='pending'))"+from, q.args...)
	if e != nil {
		return nil, e
	}
	total := integer(counts["groups"])
	last := int((total + 19) / 20)
	if last < 1 {
		last = 1
	}
	if gp > last {
		gp = last
	}
	groupArgs := append(append([]any{}, q.args...), 20, (gp-1)*20)
	sampleKey := strings.ReplaceAll(reviewTypeKeySQL, "p.", "sample.")
	groups, e := s.query(ctx, "SELECT jsonb_build_object('id',g.key,'title',g.title,'marketplace',g.marketplace,'products',g.products,'pending',g.pending,'reviewed',g.products-g.pending,'samples',(SELECT count(*) FROM ps_products sample WHERE sample.tenant_id=$1 AND sample.archived_at IS NULL AND sample.review_status<>'pending' AND "+sampleKey+"=g.key)) FROM (SELECT "+reviewTypeKeySQL+" AS key,"+reviewTypeTitleSQL+" AS title,p.marketplace,count(*) AS products,count(*) FILTER(WHERE p.review_status='pending') AS pending"+from+" GROUP BY 1,2,3) g ORDER BY g.pending DESC,g.title,g.key"+fmt.Sprintf(" LIMIT $%d OFFSET $%d", len(groupArgs)-1, len(groupArgs)), groupArgs...)
	if e != nil {
		return nil, e
	}
	var group Object
	for _, g := range groups {
		if str(g["id"]) == chosen {
			group = g
			break
		}
	}
	if group == nil && len(groups) > 0 {
		group = groups[0]
		if chosen != "" {
			mp = 1
			sp = 1
		}
	}
	out := Object{"groups": Object{"items": groups, "total": total, "page": gp, "page_size": 20}, "counts": counts, "group": group, "items": []Object{}, "page": mp, "page_size": 50, "samples": Object{"items": []Object{}, "total": 0, "page": sp, "page_size": 50}}
	if group == nil {
		out["group"] = nil
		return out, nil
	}
	key := str(group["id"])
	// Resolve the requested key within this filtered group page. Resource identity
	// always comes from the verified tenant; browser IDs never supply a tenant.
	q.args = append(q.args, key)
	q.where += fmt.Sprintf(" AND "+reviewTypeKeySQL+"=$%d", len(q.args))
	maxMember := int((integer(group["products"]) + 49) / 50)
	if maxMember < 1 {
		maxMember = 1
	}
	if mp > maxMember {
		mp = maxMember
	}
	args := append(append([]any{}, q.args...), 50, (mp-1)*50)
	items, e := s.query(ctx, "SELECT "+productProjection+" FROM ps_products p "+q.joins+" WHERE "+q.where+" ORDER BY (p.review_status='pending') DESC,"+displayChildSalesSQL+fmt.Sprintf(" DESC NULLS LAST,p.asin,p.id LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if e != nil {
		return nil, e
	}
	sampleCount := integer(group["samples"])
	maxSample := int((sampleCount + 49) / 50)
	if maxSample < 1 {
		maxSample = 1
	}
	if sp > maxSample {
		sp = maxSample
	}
	sq := newProductQuery(a, nil)
	sq.args = append(sq.args, key)
	sq.where += " AND p.review_status<>'pending' AND " + reviewTypeKeySQL + "=$2"
	samples, e := s.query(ctx, "SELECT "+productProjection+" FROM ps_products p "+sq.joins+" WHERE "+sq.where+" ORDER BY p.reviewed_at DESC NULLS LAST,p.id LIMIT 50 OFFSET $3", a.Tenant, key, (sp-1)*50)
	if e != nil {
		return nil, e
	}
	out["items"], out["page"], out["samples"] = items, mp, Object{"items": samples, "total": sampleCount, "page": sp, "page_size": 50}
	return out, nil
}
