package screening

import (
	"context"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

func txProduct(ctx context.Context, tx pgx.Tx, a identity, id string, lock bool) (Object, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var raw []byte
	e := tx.QueryRow(ctx, "SELECT to_jsonb(p) FROM ps_products p WHERE tenant_id=$1 AND id=$2::uuid"+suffix, a.Tenant, id).Scan(&raw)
	return decodeObject(raw), e
}
func (s *service) previewBatch(ctx context.Context, a identity, b Object) (any, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := receipt(ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	rawIDs, ok := b["product_ids"].([]any)
	if !ok || len(rawIDs) < 1 || len(rawIDs) > 100 {
		return nil, fail(422, "每批须选择1到100个商品")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, raw := range rawIDs {
		id := str(raw)
		if seen[id] {
			return nil, fail(422, "不能重复选择商品")
		}
		seen[id] = true
		ids = append(ids, id)
	}
	scope := str(b["scope"])
	if scope == "" {
		scope = "selected"
	}
	action := str(b["action"])
	if action == "" {
		action = "review"
	}
	if (scope != "selected" && scope != "family" && scope != "selected_families") || (action != "review" && action != "categorize") {
		return nil, fail(422, "作用范围或动作无效")
	}
	payload := Object{"scope": scope, "action": action, "status": str(b["status"]), "reason": str(b["reason"]), "note": str(b["note"]), "category": b["category"]}
	if scope == "family" {
		anchor, e := txProduct(ctx, tx, a, ids[0], false)
		if e != nil {
			return nil, e
		}
		parent := str(anchor["parent_asin"])
		if parent == "" {
			return nil, fail(422, "商品没有已确认父体关系")
		}
		payload["anchor"] = Object{"id": anchor["id"], "parent_asin": parent, "data_revision": anchor["data_revision"]}
		rows, e := tx.Query(ctx, "SELECT id::text FROM ps_products WHERE tenant_id=$1 AND marketplace=$2 AND parent_asin=$3 AND archived_at IS NULL ORDER BY id LIMIT 101", a.Tenant, anchor["marketplace"], parent)
		if e != nil {
			return nil, e
		}
		ids = []string{}
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				break
			}
			ids = append(ids, id)
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return nil, e
		}
		if len(ids) > 100 {
			return nil, fail(422, "同父体超过100个子体，请明确勾选分批复核")
		}
	}
	if str(b["mode"]) == "reference" {
		reference, e := txProduct(ctx, tx, a, str(b["reference_product_id"]), false)
		if e != nil {
			return nil, e
		}
		if action == "review" && reference["review_status"] == "pending" {
			return nil, fail(422, "参考样本尚未人工复核")
		}
		payload["reference"] = Object{"id": reference["id"], "data_revision": reference["data_revision"], "review_revision": reference["review_revision"]}
		payload["status"] = reference["review_status"]
		payload["reason"] = reference["review_reason"]
		payload["note"] = reference["review_note"]
		payload["category"] = reference["manual_category"]
	} else if mode := str(b["mode"]); mode != "" && mode != "direct" {
		return nil, fail(422, "未知复核方式")
	}
	if action == "review" {
		status := str(payload["status"])
		if status != "kept" && status != "excluded" && status != "unsure" {
			return nil, fail(422, "请选择人工结论")
		}
		if status != "kept" && strings.TrimSpace(str(payload["reason"])) == "" {
			return nil, fail(422, "请填写排除或待确认原因")
		}
	} else {
		category := strings.TrimSpace(str(payload["category"]))
		if category == "" || len([]rune(category)) > 200 {
			return nil, fail(422, "请填写有效人工分类")
		}
		payload["category"] = category
	}
	if scope == "selected_families" {
		if action != "review" || str(payload["status"]) != "excluded" {
			return nil, fail(422, "同父体同步仅用于排除")
		}
		var anchors []Object
		ids, anchors, e = expandExcludedFamilies(ctx, tx, a, ids)
		if e != nil {
			return nil, e
		}
		payload["anchors"] = anchors
	}
	targets := []Object{}
	for _, id := range ids {
		p, e := txProduct(ctx, tx, a, id, false)
		if e != nil {
			return nil, e
		}
		eligible := action == "categorize" || p["review_status"] == "pending"
		skip := ""
		if !eligible {
			skip = "已有人工结论，请单条修改"
		}
		targets = append(targets, Object{"id": id, "asin": p["asin"], "title": p["title"], "eligible": eligible, "skip_reason": skip, "data_revision": p["data_revision"], "review_revision": p["review_revision"], "parent_asin": p["parent_asin"], "status": payload["status"], "reason": payload["reason"], "note": payload["note"], "category": payload["category"]})
	}
	id := uuid()
	expires := time.Now().UTC().Add(15 * time.Minute)
	payload["id"] = id
	payload["expires_at"] = expires.Format(time.RFC3339)
	payload["targets"] = targets
	_, e = tx.Exec(ctx, "INSERT INTO ps_jobs(tenant_id,id,kind,state,idempotency_key,payload,expires_at,created_by) VALUES($1,$2::uuid,'review','preview',$2::text,$3,$4,$5)", a.Tenant, id, encoded(payload), expires, a.User)
	if e != nil {
		return nil, e
	}
	if e = saveReceipt(ctx, tx, a, payload); e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return payload, nil
}
func sameRevision(p Object, version Object) bool {
	return integer(p["data_revision"]) == integer(version["data_revision"]) && integer(p["review_revision"]) == integer(version["review_revision"])
}
func (s *service) applyBatch(ctx context.Context, a identity, id string, undo bool) (any, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	var raw []byte
	if e = tx.QueryRow(ctx, "SELECT to_jsonb(j) FROM ps_jobs j WHERE tenant_id=$1 AND id=$2::uuid AND kind='review' FOR UPDATE", a.Tenant, id).Scan(&raw); e != nil {
		return nil, e
	}
	job := decodeObject(raw)
	if integer(job["created_by"]) != a.User {
		return nil, fail(403, "只能执行或撤销自己创建的批次")
	}
	state := str(job["state"])
	if undo && state == "undone" {
		return job["undo_result"], nil
	}
	if !undo && state == "applied" {
		return job["result"], nil
	}
	if (!undo && state != "preview") || (undo && state != "applied") {
		return nil, fail(409, "当前批次状态不允许该操作")
	}
	payload := job["payload"].(map[string]any)
	result := Object{"id": id, "success": 0, "skipped": 0}
	items := []Object{}
	if !undo {
		expires, e := time.Parse(time.RFC3339Nano, str(job["expires_at"]))
		if e != nil || time.Now().After(expires) {
			return nil, fail(409, "操作已过期，请重新提交")
		}
		if ref, ok := payload["reference"].(map[string]any); ok {
			p, e := txProduct(ctx, tx, a, str(ref["id"]), true)
			if e != nil {
				return nil, e
			}
			if !sameRevision(p, ref) {
				return nil, fail(409, "参考样本已变化，请重新提交")
			}
		}
		if anchor, ok := payload["anchor"].(map[string]any); ok {
			p, e := txProduct(ctx, tx, a, str(anchor["id"]), true)
			if e != nil {
				return nil, e
			}
			if p["parent_asin"] != anchor["parent_asin"] || integer(p["data_revision"]) != integer(anchor["data_revision"]) {
				return nil, fail(409, "父体关系或锚点已变化，请重新提交")
			}
		}
		if anchors, ok := payload["anchors"].([]any); ok {
			for _, entry := range anchors {
				anchor := entry.(map[string]any)
				p, err := txProduct(ctx, tx, a, str(anchor["id"]), true)
				if err != nil {
					return nil, err
				}
				if str(p["parent_asin"]) != str(anchor["parent_asin"]) || integer(p["data_revision"]) != integer(anchor["data_revision"]) {
					return nil, fail(409, "父体关系已变化，请重新提交")
				}
			}
		}
		for _, entry := range payload["targets"].([]any) {
			target := entry.(map[string]any)
			item := Object{"id": target["id"], "success": false}
			if target["eligible"] != true {
				item["reason"] = target["skip_reason"]
				items = append(items, item)
				continue
			}
			p, e := txProduct(ctx, tx, a, str(target["id"]), true)
			if e != nil {
				return nil, e
			}
			if !sameRevision(p, target) || ((str(payload["scope"]) == "family" || str(payload["scope"]) == "selected_families") && p["parent_asin"] != target["parent_asin"]) {
				item["reason"] = "商品数据或人工结论已变化"
				items = append(items, item)
				continue
			}
			after := Object{}
			for k, v := range p {
				after[k] = v
			}
			if payload["action"] == "categorize" {
				after["manual_category"] = payload["category"]
			} else {
				after["review_status"] = payload["status"]
				after["review_reason"] = payload["reason"]
				after["review_note"] = payload["note"]
			}
			if e = writeReviewEvent(ctx, tx, a, p, after, id, ""); e != nil {
				return nil, e
			}
			item["success"] = true
			items = append(items, item)
		}
	} else {
		rows, e := tx.Query(ctx, "SELECT to_jsonb(e) FROM ps_review_events e WHERE tenant_id=$1 AND batch_id=$2::uuid AND undo_of IS NULL ORDER BY product_id", a.Tenant, id)
		if e != nil {
			return nil, e
		}
		events := []Object{}
		for rows.Next() {
			var raw []byte
			if e = rows.Scan(&raw); e != nil {
				break
			}
			events = append(events, decodeObject(raw))
		}
		if e == nil {
			e = rows.Err()
		}
		rows.Close()
		if e != nil {
			return nil, e
		}
		for _, event := range events {
			p, e := txProduct(ctx, tx, a, str(event["product_id"]), true)
			if e != nil {
				return nil, e
			}
			item := Object{"id": p["id"], "success": false}
			if integer(p["review_revision"]) != integer(event["after_revision"]) {
				item["reason"] = "之后已被再次修改"
			} else {
				before := event["before_value"].(map[string]any)
				after := Object{}
				for k, v := range p {
					after[k] = v
				}
				for _, key := range []string{"review_status", "review_reason", "review_note", "manual_category"} {
					after[key] = before[key]
				}
				if e = writeReviewEvent(ctx, tx, a, p, after, id, str(event["id"])); e != nil {
					return nil, e
				}
				item["success"] = true
			}
			items = append(items, item)
		}
	}
	success := 0
	for _, item := range items {
		if item["success"] == true {
			success++
		}
	}
	result["items"] = items
	result["success"] = success
	result["skipped"] = len(items) - success
	column := "result"
	next := "applied"
	if undo {
		column = "undo_result"
		next = "undone"
	}
	_, e = tx.Exec(ctx, "UPDATE ps_jobs SET state=$3,"+column+"=$4,progress=100,updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, id, next, encoded(result))
	if e != nil {
		return nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return result, nil
}
func writeReviewEvent(ctx context.Context, tx pgx.Tx, a identity, before, after Object, batchID, undoOf string) error {
	revision := integer(before["review_revision"]) + 1
	after["review_revision"] = revision
	after["reviewed_by"] = a.User
	after["reviewed_at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if _, e := tx.Exec(ctx, "UPDATE ps_products SET review_status=$3,review_reason=$4,review_note=$5,manual_category=$6,review_revision=$7,reviewed_by=$8,reviewed_at=now(),updated_at=now() WHERE tenant_id=$1 AND id=$2::uuid", a.Tenant, before["id"], after["review_status"], after["review_reason"], after["review_note"], str(after["manual_category"]), revision, a.User); e != nil {
		return e
	}
	_, e := tx.Exec(ctx, "INSERT INTO ps_review_events(tenant_id,id,product_id,batch_id,undo_of,actor_id,before_revision,after_revision,before_value,after_value) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", a.Tenant, uuid(), before["id"], nullableString(batchID), nullableString(undoOf), a.User, integer(before["review_revision"]), revision, encoded(before), encoded(after))
	return e
}
