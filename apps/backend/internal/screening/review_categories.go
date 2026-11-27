package screening

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

const reviewCategoryPathSQL = "regexp_split_to_array(COALESCE(NULLIF(trim(p.category_path),''),NULLIF(concat_ws(':',NULLIF(trim(p.main_category),''),NULLIF(trim(p.category),'')),''),'未分类'),'\\s*[:>›]\\s*')"

type reviewCategoryNode struct {
	ID       string                `json:"id"`
	Label    string                `json:"label"`
	Name     string                `json:"name"`
	Path     []string              `json:"path"`
	Products int64                 `json:"products"`
	Pending  int64                 `json:"pending"`
	Children []*reviewCategoryNode `json:"children"`
}

func reviewCandidateValues(a identity, params url.Values, ignoreCategory bool) (productQuery, error) {
	v := cloneQuery(params)
	selected, category := v.Get("selected_ids"), v.Get("category_path")
	for _, k := range []string{"group_page", "member_page", "sample_page", "group_id", "selected_ids", "category_path", "family_product_id"} {
		v.Del(k)
	}
	if e := validateFilters(v); e != nil {
		return productQuery{}, e
	}
	q := newProductQuery(a, v)
	if selected != "" {
		ids := strings.Split(selected, ",")
		if len(ids) > 100 {
			return q, invalid("最多选择100个商品")
		}
		for _, id := range ids {
			if !uuidPattern.MatchString(id) {
				return q, invalid("商品ID无效")
			}
		}
		q.args = append(q.args, ids)
		q.where += fmt.Sprintf(" AND p.id=ANY($%d::uuid[])", len(q.args))
	}
	if category != "" && !ignoreCategory {
		var parts []string
		if len(category) > 4000 || json.Unmarshal([]byte(category), &parts) != nil || len(parts) < 1 || len(parts) > 20 {
			return q, invalid("类目路径无效")
		}
		for _, part := range parts {
			if strings.TrimSpace(part) == "" || len(part) > 500 {
				return q, invalid("类目路径无效")
			}
		}
		q.args = append(q.args, parts)
		q.where += fmt.Sprintf(" AND ("+reviewCategoryPathSQL+")[1:%d]=$%d::text[]", len(parts), len(q.args))
	}
	return q, nil
}
func (s *service) reviewCategoriesUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	if s.gormStore != nil {
		return s.gormReviewCategoriesUsecase(ctx, a, params)
	}
	q, e := reviewCandidateValues(a, params, true)
	if e != nil {
		return nil, e
	}
	// Excluded products remain available as samples, not review candidates.
	q.where += " AND p.review_status<>'excluded'"
	rows, e := s.query(ctx, "WITH candidates AS (SELECT "+reviewCategoryPathSQL+" AS path,"+parentFamilySQL+" AS family,p.review_status FROM ps_products p "+filterJoins(q, false)+" WHERE "+q.where+") SELECT jsonb_build_object('path',path[1:depth],'products',count(*),'pending',count(DISTINCT family) FILTER(WHERE review_status='pending')) FROM candidates CROSS JOIN LATERAL generate_subscripts(path,1) AS depth GROUP BY path[1:depth] ORDER BY cardinality(path[1:depth])", q.args...)
	if e != nil {
		return nil, e
	}
	roots := []*reviewCategoryNode{}
	index := map[string]*reviewCategoryNode{}
	for _, row := range rows {
		raw, _ := row["path"].([]any)
		path := []string{}
		var parent *reviewCategoryNode
		for _, part := range raw {
			label := strings.TrimSpace(str(part))
			if label == "" {
				continue
			}
			path = append(path, label)
			id := shaDigest(encoded(path))
			node := index[id]
			if node == nil {
				node = &reviewCategoryNode{ID: id, Name: label, Label: categoryLabel(label), Path: append([]string{}, path...), Children: []*reviewCategoryNode{}}
				index[id] = node
				if parent == nil {
					roots = append(roots, node)
				} else {
					parent.Children = append(parent.Children, node)
				}
			}
			// Every prefix is aggregated independently so families spanning child categories count once.
			if len(path) == len(raw) {
				node.Products = integer(row["products"])
				node.Pending = integer(row["pending"])
			}
			parent = node
		}
	}
	var order func([]*reviewCategoryNode)
	order = func(nodes []*reviewCategoryNode) {
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
		for _, n := range nodes {
			order(n.Children)
		}
	}
	roots = pendingReviewCategories(roots)
	order(roots)
	return Object{"items": roots}, nil
}

// Keep ancestor totals intact while removing completed branches from the review queue.
func pendingReviewCategories(nodes []*reviewCategoryNode) []*reviewCategoryNode {
	pending := make([]*reviewCategoryNode, 0, len(nodes))
	for _, node := range nodes {
		if node.Pending == 0 {
			continue
		}
		node.Children = pendingReviewCategories(node.Children)
		pending = append(pending, node)
	}
	return pending
}
