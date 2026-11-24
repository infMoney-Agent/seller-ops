package screening

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5"
)

// One query supplies the first child page for every visible family. Expanding
// all rows must not turn a workspace request into fifty more HTTP requests.
func (s *service) loadReviewFamilies(ctx context.Context, q productQuery, items []Object) error {
	parents := []string{}
	seen := map[string]bool{}
	for _, item := range items {
		item["family_items"] = []Object{}
		parent := str(item["parent_asin"])
		if parent != "" && integer(item["family_count"]) > 1 && !seen[parent] {
			seen[parent] = true
			parents = append(parents, parent)
		}
	}
	if len(parents) == 0 {
		return nil
	}
	args := append(append([]any{}, q.args...), parents)
	sql := "WITH members AS(SELECT p.id,p.parent_asin,row_number() OVER(PARTITION BY p.marketplace,p.parent_asin ORDER BY " + familyRankSQL + ") AS member_rank FROM ps_products p " + filterJoins(q, true) + " WHERE " + q.where + fmt.Sprintf(" AND p.parent_asin=ANY($%d::text[])", len(args)) + ") SELECT " + productProjection + " FROM members JOIN ps_products p ON p.tenant_id=$1 AND p.id=members.id " + q.joins + " WHERE members.member_rank<=50 ORDER BY members.parent_asin,members.member_rank"
	members, err := s.query(ctx, sql, args...)
	if err != nil {
		return err
	}
	byParent := map[string][]Object{}
	for _, member := range members {
		parent := str(member["parent_asin"])
		byParent[parent] = append(byParent[parent], member)
	}
	for _, item := range items {
		if family := byParent[str(item["parent_asin"])]; family != nil {
			item["family_items"] = family
		}
	}
	return nil
}

// Expansion happens at preview time and is frozen in the batch. Apply/undo
// keep the existing revision checks and audit events for each actual child.
func expandExcludedFamilies(ctx context.Context, tx pgx.Tx, a identity, selected []string, overwrite bool) ([]string, []Object, error) {
	anchors := []Object{}
	for _, id := range selected {
		p, err := txProduct(ctx, tx, a, id, false)
		if err != nil {
			return nil, nil, err
		}
		if p["archived_at"] != nil {
			return nil, nil, fail(404, "商品不存在")
		}
		anchors = append(anchors, Object{"id": id, "parent_asin": p["parent_asin"], "data_revision": p["data_revision"]})
	}
	rows, err := tx.Query(ctx, `SELECT p.id::text FROM ps_products p WHERE p.tenant_id=$1 AND p.archived_at IS NULL AND
 (p.id=ANY($2::uuid[]) OR (($3::boolean OR p.review_status='pending') AND EXISTS(
 SELECT 1 FROM ps_products anchor WHERE anchor.tenant_id=p.tenant_id AND anchor.id=ANY($2::uuid[])
 AND anchor.archived_at IS NULL AND anchor.marketplace=p.marketplace AND NULLIF(anchor.parent_asin,'') IS NOT NULL AND anchor.parent_asin=p.parent_asin)))
 ORDER BY p.id LIMIT 101`, a.Tenant, selected, overwrite)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
	}
	if err = rows.Err(); err != nil {
		return nil, nil, err
	}
	if len(ids) > 100 {
		return nil, nil, fail(422, "包含同父体待复核子体后超过100项，请减少选择；超大父体请在商品库分批复核")
	}
	return ids, anchors, nil
}
