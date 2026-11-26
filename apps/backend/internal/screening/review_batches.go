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

func (s *service) previewBatch(ctx context.Context, a identity, cmd PreviewBatchCommand) (*ReviewBatchPayload, error) {
	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(ctx)
	if previous, e := typedReceipt[ReviewBatchPayload](ctx, tx, a); e != nil || previous != nil {
		return previous, e
	}
	ids, e := cmd.validateInitial()
	if e != nil {
		return nil, e
	}
	payload := &ReviewBatchPayload{
		Scope:     cmd.Scope,
		Action:    cmd.Action,
		Overwrite: cmd.Overwrite,
		Status:    string(cmd.Status),
		Reason:    cmd.Reason,
		Note:      cmd.Note,
		Category:  cmd.Category,
	}
	if cmd.Scope == BatchScopeFamily {
		anchor, e := txProduct(ctx, tx, a, ids[0], false)
		if e != nil {
			return nil, e
		}
		parent := str(anchor["parent_asin"])
		if parent == "" {
			return nil, invalid("商品没有已确认父体关系")
		}
		parentCopy := parent
		payload.Anchor = &BatchAnchorSnapshot{
			ID:           str(anchor["id"]),
			ParentASIN:   &parentCopy,
			DataRevision: integer(anchor["data_revision"]),
		}
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
			return nil, invalid("同父体超过100个子体，请明确勾选分批复核")
		}
	}
	if cmd.Mode == BatchModeReference {
		reference, e := txProduct(ctx, tx, a, cmd.ReferenceProductID, false)
		if e != nil {
			return nil, e
		}
		if cmd.Action == BatchActionReview && reference["review_status"] == "pending" {
			return nil, invalid("参考样本尚未人工复核")
		}
		payload.Reference = &BatchReferenceSnapshot{
			ID:             str(reference["id"]),
			DataRevision:   integer(reference["data_revision"]),
			ReviewRevision: integer(reference["review_revision"]),
		}
		payload.Status = str(reference["review_status"])
		payload.Reason = str(reference["review_reason"])
		payload.Note = str(reference["review_note"])
		payload.Category = optionalImportString(reference["manual_category"])
	} else if cmd.Mode != BatchModeDirect {
		return nil, invalid("未知复核方式")
	}
	if cmd.Action == BatchActionReview {
		decision := ReviewDecision(payload.Status)
		if !decision.valid() {
			return nil, invalid("请选择人工结论")
		}
		if decision != ReviewKept && strings.TrimSpace(payload.Reason) == "" {
			return nil, invalid("请填写排除或待确认原因")
		}
	} else {
		category := strings.TrimSpace(stringValue(payload.Category))
		if category == "" || len([]rune(category)) > 200 {
			return nil, invalid("请填写有效人工分类")
		}
		payload.Category = &category
	}
	if cmd.Scope == BatchScopeSelectedFamilies {
		if cmd.Action != BatchActionReview || ReviewDecision(payload.Status) != ReviewExcluded {
			return nil, invalid("同父体同步仅用于排除")
		}
		var anchors []BatchAnchorSnapshot
		ids, anchors, e = expandExcludedFamilies(ctx, tx, a, ids, cmd.Overwrite)
		if e != nil {
			return nil, e
		}
		payload.Anchors = anchors
	}
	targets := make([]BatchTargetSnapshot, 0, len(ids))
	for _, id := range ids {
		p, e := txProduct(ctx, tx, a, id, false)
		if e != nil {
			return nil, e
		}
		eligible := cmd.Action == BatchActionCategorize || cmd.Overwrite || p["review_status"] == "pending"
		skip := ""
		if !eligible {
			skip = "已有人工结论，请勾选覆盖后重试"
		}
		targets = append(targets, BatchTargetSnapshot{
			ID:             id,
			ASIN:           str(p["asin"]),
			Title:          str(p["title"]),
			Eligible:       eligible,
			SkipReason:     skip,
			DataRevision:   integer(p["data_revision"]),
			ReviewRevision: integer(p["review_revision"]),
			ReviewStatus:   str(p["review_status"]),
			ParentASIN:     optionalImportString(p["parent_asin"]),
			Status:         payload.Status,
			Reason:         payload.Reason,
			Note:           payload.Note,
			Category:       payload.Category,
		})
	}
	id := uuid()
	expires := time.Now().UTC().Add(15 * time.Minute)
	payload.ID = id
	payload.ExpiresAt = expires.Format(time.RFC3339)
	payload.Targets = targets
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

func sameProductRevision(p Object, dataRevision, reviewRevision int64) bool {
	return integer(p["data_revision"]) == dataRevision && integer(p["review_revision"]) == reviewRevision
}

func decodeBatchResult(raw any) (*BatchApplyResult, error) {
	var out BatchApplyResult
	if err := unmarshal(encoded(raw), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (s *service) applyBatch(ctx context.Context, a identity, id string, undo bool) (*BatchApplyResult, error) {
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
		return nil, forbidden("只能执行或撤销自己创建的批次")
	}
	state := str(job["state"])
	if undo && state == "undone" {
		return decodeBatchResult(job["undo_result"])
	}
	if !undo && state == "applied" {
		return decodeBatchResult(job["result"])
	}
	if (!undo && state != "preview") || (undo && state != "applied") {
		return nil, conflict("当前批次状态不允许该操作")
	}
	var payload ReviewBatchPayload
	if e = unmarshal(encoded(job["payload"]), &payload); e != nil {
		return nil, e
	}
	items := []BatchApplyItem{}
	if !undo {
		expires, e := time.Parse(time.RFC3339Nano, str(job["expires_at"]))
		if e != nil || time.Now().After(expires) {
			return nil, conflict("操作已过期，请重新提交")
		}
		if payload.Reference != nil {
			p, e := txProduct(ctx, tx, a, payload.Reference.ID, true)
			if e != nil {
				return nil, e
			}
			if !sameProductRevision(p, payload.Reference.DataRevision, payload.Reference.ReviewRevision) {
				return nil, conflict("参考样本已变化，请重新提交")
			}
		}
		if payload.Anchor != nil {
			p, e := txProduct(ctx, tx, a, payload.Anchor.ID, true)
			if e != nil {
				return nil, e
			}
			if !sameOptionalString(optionalImportString(p["parent_asin"]), payload.Anchor.ParentASIN) || integer(p["data_revision"]) != payload.Anchor.DataRevision {
				return nil, conflict("父体关系或锚点已变化，请重新提交")
			}
		}
		for _, anchor := range payload.Anchors {
			p, err := txProduct(ctx, tx, a, anchor.ID, true)
			if err != nil {
				return nil, err
			}
			if str(p["parent_asin"]) != stringValue(anchor.ParentASIN) || integer(p["data_revision"]) != anchor.DataRevision {
				return nil, conflict("父体关系已变化，请重新提交")
			}
		}
		for _, target := range payload.Targets {
			item := BatchApplyItem{ID: target.ID, Success: false}
			if !target.Eligible {
				item.Reason = target.SkipReason
				items = append(items, item)
				continue
			}
			p, e := txProduct(ctx, tx, a, target.ID, true)
			if e != nil {
				return nil, e
			}
			if !sameProductRevision(p, target.DataRevision, target.ReviewRevision) || ((payload.Scope == BatchScopeFamily || payload.Scope == BatchScopeSelectedFamilies) && !sameOptionalString(optionalImportString(p["parent_asin"]), target.ParentASIN)) {
				item.Reason = "商品数据或人工结论已变化"
				items = append(items, item)
				continue
			}
			after := Object{}
			for k, v := range p {
				after[k] = v
			}
			if payload.Action == BatchActionCategorize {
				after["manual_category"] = nullableString(stringValue(payload.Category))
			} else {
				after["review_status"] = payload.Status
				after["review_reason"] = payload.Reason
				after["review_note"] = payload.Note
			}
			if e = writeReviewEvent(ctx, tx, a, p, after, id, ""); e != nil {
				return nil, e
			}
			item.Success = true
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
			item := BatchApplyItem{ID: str(p["id"]), Success: false}
			if integer(p["review_revision"]) != integer(event["after_revision"]) {
				item.Reason = "之后已被再次修改"
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
				item.Success = true
			}
			items = append(items, item)
		}
	}
	success := 0
	for _, item := range items {
		if item.Success {
			success++
		}
	}
	result := &BatchApplyResult{
		ID:      id,
		Success: success,
		Skipped: len(items) - success,
		Items:   items,
	}
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
