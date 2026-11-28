package screening

import (
	"context"
	"encoding/json"
	"gorm.io/gorm/clause"
	"net/url"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
	"sort"
	"strconv"
	"strings"
	"time"
)

type gormReviewMember struct {
	ID                 uint64
	ASIN               string
	ParentASIN         string
	SourceID           string
	ManualStatus       string
	SourceFamilyRank   int
	ChildSales         *int
	ReviewedAt         *time.Time
	UpdatedAt          time.Time
	CategoryPath       string
	CategoryZh         string
	MainCategory       string
	SourceData         []byte
	ReferenceDirection *string
	Month              Object `gorm:"-"`
}

func (r gormReviewMember) family() string {
	if r.ParentASIN != "" {
		return "parent:" + r.ParentASIN
	}
	return "item:" + strconv.FormatUint(r.ID, 10)
}
func (r gormReviewMember) path() []string {
	path := strings.TrimSpace(r.CategoryPath)
	if path == "" {
		parts := []string{}
		for _, part := range []string{r.MainCategory, r.CategoryZh} {
			if part = strings.TrimSpace(part); part != "" {
				parts = append(parts, part)
			}
		}
		path = strings.Join(parts, ":")
	}
	return splitCategoryPath(path)
}
func reviewPath(params url.Values) ([]string, error) {
	raw := params.Get("category_path")
	if raw == "" {
		return nil, nil
	}
	var path []string
	if len(raw) > 4000 || json.Unmarshal([]byte(raw), &path) != nil || len(path) < 1 || len(path) > 20 {
		return nil, invalid("类目路径无效")
	}
	for _, part := range path {
		if strings.TrimSpace(part) == "" || len(part) > 500 {
			return nil, invalid("类目路径无效")
		}
	}
	return path, nil
}
func reviewPathMatches(actual, prefix []string) bool {
	if len(actual) < len(prefix) {
		return false
	}
	for i, p := range prefix {
		if actual[i] != p {
			return false
		}
	}
	return true
}

func (s *service) gormReviewMembers(ctx context.Context, ws uint64, params url.Values, samples bool) ([]gormReviewMember, error) {
	q := s.gormStore.DB().WithContext(ctx).Model(&model.ScreeningItem{}).Scopes(gormrepo.WithWorkspace(ws))
	v := cloneQuery(params)
	for _, k := range []string{"group_page", "member_page", "sample_page", "group_id", "selected_ids", "category_path", "family_product_id"} {
		v.Del(k)
	}
	importID := v.Get("import_id")
	v.Del("import_id")
	if err := validateFilters(v); err != nil {
		return nil, err
	}
	temporal := false
	if samples {
		q = q.Where("manual_status <> ?", "pending")
	} else {
		q = q.Where("manual_status <> ?", "rejected")
		if value := params.Get("review_status"); value != "" {
			q = q.Scopes(gormrepo.WithManualStatus(mapLegacyToGORMManual(value)))
		}
		if value := params.Get("machine"); value != "" {
			q = q.Scopes(gormrepo.WithMachineResult(mapLegacyToGORMMachine(value)))
		}
		if value := params.Get("q"); value != "" {
			pattern := "%" + value + "%"
			q = q.Where("(asin LIKE ? OR title_en LIKE ? OR JSON_UNQUOTE(JSON_EXTRACT(source_data,'$.title')) LIKE ?)", pattern, pattern, pattern)
		}
		for _, pair := range [][2]string{{"recommendation", "recommendation"}, {"seller_region", "seller_region"}, {"direction", "direction"}} {
			if value := params.Get(pair[0]); value != "" {
				q = q.Where("JSON_UNQUOTE(JSON_EXTRACT(source_data,?)) = ?", "$."+pair[1], value)
			}
		}
		if value := params.Get("category"); value != "" {
			q = q.Where("category_zh = ?", value)
		}
		if value := params.Get("trademark"); value != "" {
			label := "COALESCE(NULLIF(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(source_data,'$.specs.trademark_class')),'null'),''),NULLIF(JSON_UNQUOTE(JSON_EXTRACT(source_data,'$.specs.legacy_site_snapshot.p.trademark_class')),'null'),'')"
			if value == "pending" {
				q = q.Where("("+label+" = '' OR "+label+" LIKE ?)", "%待核实%")
			} else {
				q = q.Where(label+" LIKE ?", "%第"+value+"类%")
			}
		}
		if selected := params.Get("selected_ids"); selected != "" {
			parts := strings.Split(selected, ",")
			if len(parts) > 100 {
				return nil, invalid("最多选择100个商品")
			}
			ids := []any{}
			for _, part := range parts {
				id, err := parseUintID(part)
				if err != nil {
					return nil, err
				}
				ids = append(ids, id)
			}
			q = q.Where(clause.IN{Column: clause.Column{Name: "id"}, Values: ids})
		}
		if importID != "" {
			id, err := parseUintID(importID)
			if err != nil {
				return nil, err
			}
			sub := s.gormStore.DB().WithContext(ctx).Model(&model.ImportRecord{}).Select("asin").Scopes(gormrepo.WithWorkspace(ws)).Where("task_id = ?", id)
			q = q.Where("asin IN (?)", sub)
		}
		for _, key := range []string{"stat_month", "period_type", "source_type"} {
			if params.Get(key) != "" {
				temporal = true
			}
		}
		if temporal {
			parts := []string{}
			args := []any{}
			for _, key := range []string{"stat_month", "period_type", "source_type"} {
				if value := params.Get(key); value != "" {
					parts = append(parts, "observation."+key+" = ?")
					args = append(args, value)
				}
			}

			observations := s.gormStore.DB().WithContext(ctx).Table("screening_items AS observation_owner").Select("observation_owner.id").Joins("JOIN JSON_TABLE(observation_owner.source_data, '$.source_monthly_history[*]' COLUMNS (stat_month VARCHAR(10) PATH '$.stat_month',period_type VARCHAR(32) PATH '$.period_type',source_type VARCHAR(64) PATH '$.source_type')) AS observation ON TRUE").Where("observation_owner.workspace_id = ? AND observation_owner.deleted_at IS NULL", ws).Where(strings.Join(parts, " AND "), args...)
			q = q.Where("id IN (?)", observations)

		}
		if !temporal {
			if value := params.Get("child_sales_min"); value != "" {
				q = q.Where("child_sales >= ?", value)
			}
		}
		for _, pair := range [][2]string{{"profit_min", ">="}, {"profit_max", "<="}} {
			if value := params.Get(pair[0]); value != "" {
				q = q.Where("COALESCE(estimated_profit, CAST(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(source_data,'$.profit_output.profit_usd')),'null') AS DECIMAL(20,6))) "+pair[1]+" CAST(? AS DECIMAL(20,6))", value)
			}
		}
	}
	columns := "id,asin,parent_asin,manual_status,source_family_rank,child_sales,reviewed_at,updated_at,category_zh,COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(source_data,'$.id')),'null'),CAST(id AS CHAR)) AS source_id,COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(source_data,'$.category_path')),'null'),category_path) AS category_path,COALESCE(NULLIF(JSON_UNQUOTE(JSON_EXTRACT(source_data,'$.main_category')),'null'),'') AS main_category"
	if temporal {
		columns += ",source_data"
	}
	directionExpr := "CASE WHEN JSON_TYPE(JSON_EXTRACT(source_data,'$.direction'))='STRING' THEN JSON_UNQUOTE(JSON_EXTRACT(source_data,'$.direction')) ELSE NULL END"
	if !samples && params.Get("has_reference") != "" {
		columns += "," + directionExpr + " AS reference_direction"
	}

	var rows []gormReviewMember
	if err := q.Select(columns).Find(&rows).Error; err != nil {
		return nil, err
	}
	if temporal {
		filtered := rows[:0]
		for _, r := range rows {
			var source Object
			if json.Unmarshal(r.SourceData, &source) != nil {
				continue
			}
			history, _ := source["source_monthly_history"].([]any)
			for _, v := range history {
				row := v.(map[string]any)
				match := true
				for _, key := range []string{"stat_month", "period_type", "source_type"} {
					if want := params.Get(key); want != "" && str(row[key]) != want {
						match = false
					}
				}
				if match {
					r.Month = row
					r.ChildSales = nil
					if value := row["display_child_sales"]; value != nil {
						v := int(integer(value))
						r.ChildSales = &v
					}
					break
				}
			}
			if r.Month == nil {
				continue
			}
			if value := params.Get("child_sales_min"); value != "" {
				n, _ := strconv.Atoi(value)
				if r.ChildSales == nil || *r.ChildSales < n {
					continue
				}
			}
			r.SourceData = nil
			filtered = append(filtered, r)
		}
		rows = filtered
	}
	if !samples && params.Get("has_reference") != "" {
		var references []struct {
			ID        uint64
			Direction *string
		}
		if err := s.gormStore.DB().WithContext(ctx).Model(&model.ScreeningItem{}).Scopes(gormrepo.WithWorkspace(ws)).Select("id,"+directionExpr+" AS direction").Where("manual_status <> ?", "pending").Find(&references).Error; err != nil {
			return nil, err
		}
		type refCount struct {
			count int
			id    uint64
		}
		counts := map[string]refCount{}
		for _, r := range references {
			if r.Direction != nil {
				v := counts[*r.Direction]
				v.count++
				v.id = r.ID
				counts[*r.Direction] = v
			}
		}
		filtered := rows[:0]
		wanted := params.Get("has_reference") == "true"
		for _, r := range rows {
			exists := false
			if r.ReferenceDirection != nil {
				v := counts[*r.ReferenceDirection]
				exists = v.count > 1 || (v.count == 1 && v.id != r.ID)
			}
			if exists == wanted {
				filtered = append(filtered, r)
			}
		}
		rows = filtered
	}

	return rows, nil
}

func reviewMemberLess(a, b gormReviewMember) bool {
	if a.Month != nil || b.Month != nil {
		for _, key := range []string{"stat_month", "period_end"} {
			if cmp := strings.Compare(str(a.Month[key]), str(b.Month[key])); cmp != 0 {
				return cmp > 0
			}
		}
		for _, key := range []string{"period_type", "source_type"} {
			if cmp := strings.Compare(str(a.Month[key]), str(b.Month[key])); cmp != 0 {
				return cmp < 0
			}
		}
		if a.ChildSales != nil || b.ChildSales != nil {
			if a.ChildSales == nil {
				return false
			}
			if b.ChildSales == nil {
				return true
			}
			if *a.ChildSales != *b.ChildSales {
				return *a.ChildSales > *b.ChildSales
			}
		}
		if !a.UpdatedAt.Equal(b.UpdatedAt) {
			return a.UpdatedAt.After(b.UpdatedAt)
		}
	} else if a.SourceFamilyRank != b.SourceFamilyRank {
		return a.SourceFamilyRank < b.SourceFamilyRank
	}
	return a.SourceID < b.SourceID
}
func reviewClampPage(page, total int) int {
	last := (total + 49) / 50
	if last < 1 {
		last = 1
	}
	if page > last {
		return last
	}
	return page
}
func reviewPageMembers(rows []gormReviewMember, page int) []gormReviewMember {
	start := (page - 1) * 50
	if start > len(rows) {
		start = len(rows)
	}
	end := start + 50
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end]
}

func (s *service) gormReviewObjects(ctx context.Context, ws uint64, members []gormReviewMember) (map[uint64]Object, error) {
	ids := []uint64{}
	seen := map[uint64]bool{}
	for _, r := range members {
		if !seen[r.ID] {
			seen[r.ID] = true
			ids = append(ids, r.ID)
		}
	}
	out := map[uint64]Object{}
	for start := 0; start < len(ids); start += 200 {
		end := start + 200
		if end > len(ids) {
			end = len(ids)
		}
		res, err := s.gormStore.Screening().ListWithDetails(ctx, ws, gormrepo.ScreeningFilter{ItemIDs: ids[start:end], Page: 1, PageSize: 200})
		if err != nil {
			return nil, err
		}
		for _, d := range res.Items {
			out[d.Item.ID] = gormItemToObject(d)
		}
	}
	return out, nil
}

func gormReviewObject(r gormReviewMember, base Object) Object {
	row := Object{}
	for key, value := range base {
		row[key] = value
	}

	delete(row, "family_count")
	delete(row, "family_items")
	delete(row, "family_member_ids")
	if r.Month != nil {
		m := r.Month
		row["monthly_id"] = m["id"]
		row["monthly_revision"] = m["revision"]
		for _, key := range []string{"stat_month", "period_type", "period_start", "period_end", "source_type", "sales_scope", "sales_quantity_kind", "child_sales_raw", "price_usd", "review_count", "bsr_rank", "display_child_sales"} {
			row[key] = m[key]
		}
		row["reported_monthly_sales"] = m["monthly_sales"]
		row["monthly_sales"] = nil
		row["monthly_revenue"] = nil
		if m["sales_scope"] == "child" {
			row["monthly_sales"] = m["monthly_sales"]
			if m["monthly_revenue"] != nil {
				row["monthly_revenue"] = str(m["monthly_revenue"])
			}
		}
		row["rating"] = nil
		if m["rating"] != nil {
			row["rating"] = str(m["rating"])
		}
	}
	return row
}

func (s *service) gormReviewWorkspaceUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	ws, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	mp, err := workspacePage(params.Get("member_page"))
	if err != nil {
		return nil, err
	}
	sp, err := workspacePage(params.Get("sample_page"))
	if err != nil {
		return nil, err
	}
	path, err := reviewPath(params)
	if err != nil {
		return nil, err
	}
	all, err := s.gormReviewMembers(ctx, ws, params, false)
	if err != nil {
		return nil, err
	}
	var anchor *model.ScreeningItem
	if family := params.Get("family_product_id"); family != "" {
		id, e := parseUintID(family)
		if e != nil {
			return nil, e
		}
		anchor, e = s.gormStore.Screening().GetByID(ctx, ws, id)
		if e != nil {
			return nil, e
		}
	}
	candidates := []gormReviewMember{}
	pendingProducts := 0
	pendingFamilies := map[string]bool{}
	families := map[string][]gormReviewMember{}
	for _, r := range all {
		if !reviewPathMatches(r.path(), path) {
			continue
		}
		if anchor != nil {
			if anchor.ParentASIN == "" {
				if r.ID != anchor.ID {
					continue
				}
			} else if r.ParentASIN != anchor.ParentASIN {
				continue
			}
		}
		candidates = append(candidates, r)
		families[r.family()] = append(families[r.family()], r)
		if r.ManualStatus == "pending" {
			pendingProducts++
			pendingFamilies[r.family()] = true
		}
	}
	for key := range families {
		sort.SliceStable(families[key], func(i, j int) bool { return reviewMemberLess(families[key][i], families[key][j]) })
	}
	display := []gormReviewMember{}
	if anchor != nil {
		display = append(display, candidates...)
		sort.SliceStable(display, func(i, j int) bool { return reviewMemberLess(display[i], display[j]) })
	} else {
		for _, members := range families {
			display = append(display, members[0])
		}
		sort.SliceStable(display, func(i, j int) bool {
			left, right := display[i], display[j]
			lp, rp := left.ManualStatus == "pending", right.ManualStatus == "pending"
			if lp != rp {
				return lp
			}
			if left.ChildSales != nil || right.ChildSales != nil {
				if left.ChildSales == nil {
					return false
				}
				if right.ChildSales == nil {
					return true
				}
				if *left.ChildSales != *right.ChildSales {
					return *left.ChildSales > *right.ChildSales
				}
			}
			return left.SourceID < right.SourceID
		})
	}
	sampleRows, err := s.gormReviewMembers(ctx, ws, url.Values{}, true)
	if err != nil {
		return nil, err
	}
	samples := []gormReviewMember{}
	for _, r := range sampleRows {
		if reviewPathMatches(r.path(), path) {
			samples = append(samples, r)
		}
	}
	sort.SliceStable(samples, func(i, j int) bool {
		left, right := samples[i], samples[j]
		if left.ReviewedAt != nil || right.ReviewedAt != nil {
			if left.ReviewedAt == nil {
				return false
			}
			if right.ReviewedAt == nil {
				return true
			}
			if !left.ReviewedAt.Equal(*right.ReviewedAt) {
				return left.ReviewedAt.After(*right.ReviewedAt)
			}
		}
		return left.SourceID < right.SourceID
	})
	mp = reviewClampPage(mp, len(display))
	sp = reviewClampPage(sp, len(samples))
	visible := reviewPageMembers(display, mp)
	visibleSamples := reviewPageMembers(samples, sp)
	load := append(append([]gormReviewMember{}, visible...), visibleSamples...)
	if anchor == nil {
		for _, r := range visible {
			members := families[r.family()]
			if len(members) > 1 && r.ParentASIN != "" {
				end := len(members)
				if end > 50 {
					end = 50
				}
				load = append(load, members[:end]...)
			}
		}
	}
	objects, err := s.gormReviewObjects(ctx, ws, load)
	if err != nil {
		return nil, err
	}
	items := []Object{}
	sampleItems := []Object{}
	for _, r := range visible {
		item := gormReviewObject(r, objects[r.ID])
		if anchor == nil {
			members := families[r.family()]
			item["family_count"] = len(members)
			item["family_items"] = []Object{}
			if len(members) > 1 && r.ParentASIN != "" {
				family := []Object{}
				end := len(members)
				if end > 50 {
					end = 50
				}
				for _, m := range members[:end] {
					child := gormReviewObject(m, objects[m.ID])
					family = append(family, child)
				}
				item["family_items"] = family
			}
		}
		items = append(items, item)
	}
	for _, r := range visibleSamples {
		sampleItems = append(sampleItems, gormReviewObject(r, objects[r.ID]))
	}
	counts := Object{"products": len(candidates), "families": len(families), "pending": len(pendingFamilies), "pending_products": pendingProducts, "groups": 0}
	var group any
	if len(candidates) > 0 {
		counts["groups"] = 1
		title := "全部商品"
		if len(path) > 0 {
			title = strings.Join(categoryDisplayPath(path), " / ")
		}
		group = Object{"id": shaDigest(encoded(path)), "title": title, "marketplace": "US", "products": len(candidates), "display_products": len(display), "pending": len(pendingFamilies), "reviewed": len(candidates) - pendingProducts, "samples": len(samples)}
	}
	return Object{"groups": Object{"items": []Object{}, "total": 0, "page": 1, "page_size": 20}, "counts": counts, "group": group, "items": items, "page": mp, "page_size": 50, "samples": Object{"items": sampleItems, "total": len(samples), "page": sp, "page_size": 50}}, nil
}

func (s *service) gormReviewCategoriesUsecase(ctx context.Context, a identity, params url.Values) (any, error) {
	ws, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	rows, err := s.gormReviewMembers(ctx, ws, params, false)
	if err != nil {
		return nil, err
	}
	roots := []*reviewCategoryNode{}
	index := map[string]*reviewCategoryNode{}
	pending := map[string]map[string]bool{}
	for _, r := range rows {
		var path []string
		var parent *reviewCategoryNode
		for _, part := range r.path() {
			path = append(path, part)
			id := shaDigest(encoded(path))
			node := index[id]
			if node == nil {
				node = &reviewCategoryNode{ID: id, Name: part, Label: categoryLabel(part), Path: append([]string{}, path...), Children: []*reviewCategoryNode{}}
				index[id] = node
				if parent == nil {
					roots = append(roots, node)
				} else {
					parent.Children = append(parent.Children, node)
				}
			}
			node.Products++
			if r.ManualStatus == "pending" {
				if pending[id] == nil {
					pending[id] = map[string]bool{}
				}
				pending[id][r.family()] = true
			}
			parent = node
		}
	}
	for id, keys := range pending {
		index[id].Pending = int64(len(keys))
	}
	roots = pendingReviewCategories(roots)
	var order func([]*reviewCategoryNode)
	order = func(nodes []*reviewCategoryNode) {
		sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
		for _, node := range nodes {
			order(node.Children)
		}
	}
	order(roots)
	return Object{"items": roots}, nil
}
