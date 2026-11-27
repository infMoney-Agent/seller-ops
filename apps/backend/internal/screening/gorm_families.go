package screening

import (
	"context"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
	"strconv"
)

func (s *service) gormAttachFamilies(ctx context.Context, ws uint64, items []Object) error {
	parents := []string{}
	seen := map[string]bool{}
	for _, it := range items {
		p := str(it["parent_asin"])
		if p != "" && !seen[p] {
			seen[p] = true
			parents = append(parents, p)
		}
	}
	byParent := map[string][]model.ScreeningItem{}
	if len(parents) > 0 {
		var members []model.ScreeningItem
		if err := s.gormStore.DB().WithContext(ctx).Scopes(gormrepo.WithWorkspace(ws)).Where(clause.IN{Column: clause.Column{Name: "parent_asin"}, Values: stringsToAny(parents)}).Order("latest_sales DESC").Order("id").Find(&members).Error; err != nil {
			return err
		}
		for _, m := range members {
			byParent[m.ParentASIN] = append(byParent[m.ParentASIN], m)
		}
	}
	for _, it := range items {
		members := byParent[str(it["parent_asin"])]
		ids := []string{}
		family := []Object{}
		for _, m := range members {
			ids = append(ids, strconv.FormatUint(m.ID, 10))
			if len(family) < 50 {
				family = append(family, gormItemToObject(gormrepo.ScreeningItemWithDetails{Item: m}))
			}
		}
		if len(ids) == 0 {
			ids = []string{str(it["id"])}
		}
		it["family_count"] = len(ids)
		it["family_member_ids"] = ids
		it["family_items"] = family
	}
	return nil
}
func stringsToAny(values []string) []any {
	out := make([]any, len(values))
	for i, v := range values {
		out[i] = v
	}
	return out
}
