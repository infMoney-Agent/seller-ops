package screening

import (
	"encoding/json"
	"fmt"
	"net/http"
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

func reviewCandidateQuery(r *http.Request, ignoreCategory bool) (productQuery, error) {
	v := r.URL.Query()
	selected, category := v.Get("selected_ids"), v.Get("category_path")
	for _, k := range []string{"group_page", "member_page", "sample_page", "group_id", "selected_ids", "category_path"} {
		v.Del(k)
	}
	if e := validateFilters(v); e != nil {
		return productQuery{}, e
	}
	q := newProductQuery(actor(r), v)
	if selected != "" {
		ids := strings.Split(selected, ",")
		if len(ids) > 100 {
			return q, fail(422, "最多选择100个商品")
		}
		for _, id := range ids {
			if !uuidPattern.MatchString(id) {
				return q, fail(422, "商品ID无效")
			}
		}
		q.args = append(q.args, ids)
		q.where += fmt.Sprintf(" AND p.id=ANY($%d::uuid[])", len(q.args))
	}
	if category != "" && !ignoreCategory {
		var parts []string
		if len(category) > 4000 || json.Unmarshal([]byte(category), &parts) != nil || len(parts) < 1 || len(parts) > 20 {
			return q, fail(422, "类目路径无效")
		}
		for _, part := range parts {
			if strings.TrimSpace(part) == "" || len(part) > 500 {
				return q, fail(422, "类目路径无效")
			}
		}
		q.args = append(q.args, parts)
		q.where += fmt.Sprintf(" AND ("+reviewCategoryPathSQL+")[1:%d]=$%d::text[]", len(parts), len(q.args))
	}
	return q, nil
}
func (s *service) reviewCategories(r *http.Request) (any, error) {
	q, e := reviewCandidateQuery(r, true)
	if e != nil {
		return nil, e
	}
	rows, e := s.query(r.Context(), "SELECT jsonb_build_object('path',"+reviewCategoryPathSQL+",'products',count(*),'pending',count(*) FILTER(WHERE p.review_status='pending')) FROM ps_products p "+filterJoins(q, false)+" WHERE "+q.where+" GROUP BY "+reviewCategoryPathSQL, q.args...)
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
			node.Products += integer(row["products"])
			node.Pending += integer(row["pending"])
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
	order(roots)
	return Object{"items": roots}, nil
}
