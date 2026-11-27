package screening

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
)

// gormUpdateProductParameters 使用纯 GORM API 校验并更新商品规格/父体参数，并自动同步父体 ChildCount。
func (s *service) gormUpdateProductParameters(ctx context.Context, a identity, id string, cmd UpdateParametersCommand) (Object, error) {
	if len(cmd.UnknownKeys) > 0 {
		return nil, invalid("包含不支持的参数字段")
	}
	if len(cmd.Patches) == 0 {
		return nil, invalid("未提供可更新的参数")
	}
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	existing, err := s.gormStore.Screening().GetByID(ctx, wsID, itemID)
	if err != nil {
		return nil, err
	}

	updates := map[string]any{}
	parentChanged := false

	parseDimVal := func(p ParameterPatch) (string, error) {
		if p.Clear {
			return "", nil
		}
		v := strings.TrimSpace(p.Raw)
		if v == "" {
			return "", nil
		}
		if !decimalPattern.MatchString(v) {
			return "", invalid("尺寸或重量参数必须为非负数字")
		}
		return v, nil
	}

	if p, ok := cmd.Patches["parent_asin"]; ok {
		if p.Clear || strings.TrimSpace(p.Raw) == "" {
			updates["parent_asin"] = ""
			updates["is_parent"] = true
			parentChanged = true
		} else {
			v := strings.ToUpper(strings.TrimSpace(p.Raw))
			if !asinPattern.MatchString(v) {
				return nil, invalid("父体ASIN格式无效")
			}
			updates["parent_asin"] = v
			updates["is_parent"] = (v == existing.ASIN)
			parentChanged = true
		}
	}
	if p, ok := cmd.Patches["weight_g"]; ok {
		if p.Clear || strings.TrimSpace(p.Raw) == "" {
			updates["net_weight_grams"] = float64(0)
		} else {
			raw, err := parseDimVal(p)
			if err != nil {
				return nil, err
			}
			f, _ := strconv.ParseFloat(raw, 64)
			updates["net_weight_grams"] = f
		}
	}
	if p, ok := cmd.Patches["package_weight_g"]; ok {
		if p.Clear || strings.TrimSpace(p.Raw) == "" {
			updates["gross_weight_grams"] = float64(0)
		} else {
			raw, err := parseDimVal(p)
			if err != nil {
				return nil, err
			}
			f, _ := strconv.ParseFloat(raw, 64)
			updates["gross_weight_grams"] = f
		}
	}

	// 组合更新商品尺寸与包装尺寸字符串
	buildDims := func(lKey, wKey, hKey string) (string, bool, error) {
		lp, lok := cmd.Patches[lKey]
		wp, wok := cmd.Patches[wKey]
		hp, hok := cmd.Patches[hKey]
		if !lok && !wok && !hok {
			return "", false, nil
		}
		var parts []string
		for _, p := range []ParameterPatch{lp, wp, hp} {
			if !p.Present {
				continue
			}
			val, err := parseDimVal(p)
			if err != nil {
				return "", false, err
			}
			if val != "" {
				parts = append(parts, val)
			}
		}
		if len(parts) == 0 {
			return "", true, nil
		}
		return strings.Join(parts, " x ") + " cm", true, nil
	}
	if dims, changed, err := buildDims("product_length_cm", "product_width_cm", "product_height_cm"); err != nil {
		return nil, err
	} else if changed {
		updates["item_dimensions"] = dims
	}
	if dims, changed, err := buildDims("package_length_cm", "package_width_cm", "package_height_cm"); err != nil {
		return nil, err
	} else if changed {
		updates["package_dimensions"] = dims
	}

	// 其余数值参数仅做合法性校验（如 units_per_pack、carton_*）
	for _, key := range []string{"units_per_pack", "carton_units", "carton_weight_g", "carton_length_cm", "carton_width_cm", "carton_height_cm"} {
		if p, ok := cmd.Patches[key]; ok && !p.Clear && strings.TrimSpace(p.Raw) != "" {
			if _, err := parseDimVal(p); err != nil {
				return nil, err
			}
		}
	}

	if len(updates) > 0 {
		if err := s.gormStore.DB().WithContext(ctx).
			Model(&model.ScreeningItem{}).
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: itemID}).
			Updates(updates).Error; err != nil {
			return nil, err
		}
	}
	if parentChanged {
		_ = s.gormStore.Screening().RecalculateParentChildCounts(ctx, wsID)
	}
	val, err := s.gormGetProductUsecase(ctx, a, id)
	if err != nil {
		return nil, err
	}
	return val.(Object), nil
}

// ---------------------------------------------------------------------------
// 批量人工复核：Preview / Apply / Undo / List / Get
// ---------------------------------------------------------------------------

func (s *service) gormPreviewBatch(ctx context.Context, a identity, cmd PreviewBatchCommand) (*ReviewBatchPayload, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	ids, err := cmd.validateInitial()
	if err != nil {
		return nil, err
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

	gdb := s.gormStore.DB().WithContext(ctx)

	// 解析目标商品集合
	var targetItems []model.ScreeningItem
	if cmd.Scope == BatchScopeFamily {
		anchorID, err := parseUintID(ids[0])
		if err != nil {
			return nil, err
		}
		anchor, err := s.gormStore.Screening().GetByID(ctx, wsID, anchorID)
		if err != nil {
			return nil, err
		}
		parent := strings.TrimSpace(anchor.ParentASIN)
		if parent == "" {
			return nil, invalid("商品没有已确认父体关系")
		}
		parentCopy := parent
		payload.Anchor = &BatchAnchorSnapshot{
			ID:           strconv.FormatUint(anchor.ID, 10),
			ParentASIN:   &parentCopy,
			DataRevision: 1,
		}
		if err := gdb.Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "parent_asin"}, Value: parent}).
			Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}).
			Limit(101).
			Find(&targetItems).Error; err != nil {
			return nil, err
		}
		if len(targetItems) > 100 {
			return nil, invalid("同父体超过100个子体，请明确勾选分批复核")
		}
	} else {
		itemIDs := make([]any, 0, len(ids))
		for _, rawID := range ids {
			uid, err := parseUintID(rawID)
			if err != nil {
				return nil, err
			}
			itemIDs = append(itemIDs, uid)
		}
		var selected []model.ScreeningItem
		if err := gdb.Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: itemIDs}).
			Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}).
			Find(&selected).Error; err != nil {
			return nil, err
		}
		if len(selected) != len(ids) {
			return nil, notFound("部分商品不存在或不属于当前工作区")
		}
		if cmd.Scope == BatchScopeSelectedFamilies {
			parentSet := map[string]struct{}{}
			for _, it := range selected {
				p := strings.TrimSpace(it.ParentASIN)
				if p != "" {
					parentSet[p] = struct{}{}
					pCopy := p
					payload.Anchors = append(payload.Anchors, BatchAnchorSnapshot{
						ID:           strconv.FormatUint(it.ID, 10),
						ParentASIN:   &pCopy,
						DataRevision: 1,
					})
				}
			}
			if len(parentSet) > 0 {
				parentVals := make([]any, 0, len(parentSet))
				for p := range parentSet {
					parentVals = append(parentVals, p)
				}
				var siblings []model.ScreeningItem
				if err := gdb.Scopes(gormrepo.WithWorkspace(wsID)).
					Where(clause.IN{Column: clause.Column{Name: "parent_asin"}, Values: parentVals}).
					Find(&siblings).Error; err != nil {
					return nil, err
				}
				merged := map[uint64]model.ScreeningItem{}
				for _, it := range selected {
					merged[it.ID] = it
				}
				for _, it := range siblings {
					merged[it.ID] = it
				}
				for _, it := range merged {
					targetItems = append(targetItems, it)
				}
				sort.Slice(targetItems, func(i, j int) bool { return targetItems[i].ID < targetItems[j].ID })
			} else {
				targetItems = selected
			}
		} else {
			targetItems = selected
		}
	}

	if cmd.Mode == BatchModeReference {
		refID, err := parseUintID(cmd.ReferenceProductID)
		if err != nil {
			return nil, err
		}
		ref, err := s.gormStore.Screening().GetByID(ctx, wsID, refID)
		if err != nil {
			return nil, err
		}
		if cmd.Action == BatchActionReview && ref.ManualStatus == "pending" {
			return nil, invalid("参考样本尚未人工复核")
		}
		payload.Reference = &BatchReferenceSnapshot{
			ID:             strconv.FormatUint(ref.ID, 10),
			DataRevision:   1,
			ReviewRevision: 1,
		}
		payload.Status = mapGORMToLegacyManual(ref.ManualStatus)
		payload.Reason = ref.ManualNote
		payload.Note = ref.ManualNote
		cat := ref.CategoryZh
		payload.Category = &cat
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
		if cmd.Scope == BatchScopeSelectedFamilies && decision != ReviewExcluded {
			return nil, invalid("同父体同步仅用于排除")
		}
	} else {
		category := strings.TrimSpace(stringValue(payload.Category))
		if category == "" || len([]rune(category)) > 200 {
			return nil, invalid("请填写有效人工分类")
		}
		payload.Category = &category
	}

	targets := make([]BatchTargetSnapshot, 0, len(targetItems))
	for _, it := range targetItems {
		legacyStatus := mapGORMToLegacyManual(it.ManualStatus)
		eligible := true
		skipReason := ""
		if cmd.Action == BatchActionReview && !cmd.Overwrite && it.ManualStatus != "pending" {
			eligible = false
			skipReason = "已有人工结论且未开启覆盖"
		}
		if cmd.Action != BatchActionReview && !cmd.Overwrite && strings.TrimSpace(it.CategoryZh) != "" {
			eligible = false
			skipReason = "已有人工分类且未开启覆盖"
		}
		var parentPtr *string
		if it.ParentASIN != "" {
			p := it.ParentASIN
			parentPtr = &p
		}
		catCopy := it.CategoryZh
		title := it.TitleZh
		if title == "" {
			title = it.TitleEn
		}
		targets = append(targets, BatchTargetSnapshot{
			ID:             strconv.FormatUint(it.ID, 10),
			ASIN:           it.ASIN,
			Title:          title,
			Eligible:       eligible,
			SkipReason:     skipReason,
			DataRevision:   1,
			ReviewRevision: 1,
			ReviewStatus:   legacyStatus,
			ParentASIN:     parentPtr,
			Status:         legacyStatus,
			Reason:         it.ManualNote,
			Note:           it.ManualNote,
			Category:       &catCopy,
		})
	}
	payload.Targets = targets

	expiresAt := time.Now().Add(30 * time.Minute)
	payload.ExpiresAt = expiresAt.UTC().Format(time.RFC3339Nano)

	job := &model.ScreeningJob{
		WorkspaceID:    wsID,
		Kind:           "review",
		State:          "preview",
		IdempotencyKey: a.RequestKey,
		Payload:        datatypes.JSON(encoded(payload)),
		ExpiresAt:      &expiresAt,
		CreatedBy:      fmt.Sprintf("user-%d", a.User),
	}
	if err := gdb.Create(job).Error; err != nil {
		return nil, err
	}
	payload.ID = strconv.FormatUint(job.ID, 10)
	_ = gdb.Model(&model.ScreeningJob{}).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: job.ID}).
		Updates(map[string]any{"payload": datatypes.JSON(encoded(payload))}).Error

	return payload, nil
}

func (s *service) gormApplyBatch(ctx context.Context, a identity, id string, undo bool) (*BatchApplyResult, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	jobID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}

	var result *BatchApplyResult
	err = s.gormStore.Transaction(ctx, func(txStore *gormrepo.Store) error {
		tx := txStore.DB()
		var job model.ScreeningJob
		if err := tx.Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: jobID}).
			Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: "review"}).
			First(&job).Error; err != nil {
			return err
		}

		var payload ReviewBatchPayload
		if err := json.Unmarshal(job.Payload, &payload); err != nil {
			return internalFailure("批次快照无效")
		}

		res := &BatchApplyResult{ID: id, Items: []BatchApplyItem{}}
		now := time.Now()
		operator := fmt.Sprintf("user-%d", a.User)
		batchTag := fmt.Sprintf("gorm-batch-%d", job.ID)

		if !undo {
			if job.State == "applied" && len(job.Result) > 0 {
				var prev BatchApplyResult
				if json.Unmarshal(job.Result, &prev) == nil {
					result = &prev
					return nil
				}
			}
			if job.State != "preview" {
				return conflict("该批次已执行或已撤销")
			}
			if job.ExpiresAt != nil && now.After(*job.ExpiresAt) {
				return conflict("批次预览已过期，请重新预览")
			}

			nextStatus := mapLegacyToGORMManual(payload.Status)
			note := strings.TrimSpace(payload.Note)
			if note == "" {
				note = strings.TrimSpace(payload.Reason)
			}
			var logs []model.ScreeningReviewLog

			for _, t := range payload.Targets {
				if !t.Eligible {
					res.Skipped++
					res.Items = append(res.Items, BatchApplyItem{ID: t.ID, Success: false, Reason: t.SkipReason})
					continue
				}
				tid, err := parseUintID(t.ID)
				if err != nil {
					res.Skipped++
					res.Items = append(res.Items, BatchApplyItem{ID: t.ID, Success: false, Reason: "商品ID无效"})
					continue
				}
				updates := map[string]any{
					"reviewed_by": operator,
					"reviewed_at": &now,
				}
				if payload.Action != BatchActionReview {
					updates["category_zh"] = stringValue(payload.Category)
				} else {
					updates["manual_status"] = nextStatus
					updates["manual_note"] = note
				}
				r := tx.Model(&model.ScreeningItem{}).
					Scopes(gormrepo.WithWorkspace(wsID)).
					Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: tid}).
					Updates(updates)
				if r.Error != nil {
					return r.Error
				}
				if r.RowsAffected == 0 {
					res.Skipped++
					res.Items = append(res.Items, BatchApplyItem{ID: t.ID, Success: false, Reason: "商品不存在"})
					continue
				}
				res.Success++
				res.Items = append(res.Items, BatchApplyItem{ID: t.ID, Success: true})
				if payload.Action == BatchActionReview {
					logs = append(logs, model.ScreeningReviewLog{
						WorkspaceID: wsID,
						ItemID:      tid,
						ASIN:        t.ASIN,
						BatchID:     batchTag,
						ActionType:  "batch",
						PrevStatus:  mapLegacyToGORMManual(t.ReviewStatus),
						NextStatus:  nextStatus,
						Note:        note,
						OperatorID:  operator,
					})
				}
			}
			if len(logs) > 0 {
				if err := tx.CreateInBatches(logs, gormrepo.DefaultBatchSize).Error; err != nil {
					return err
				}
			}
			if err := tx.Model(&model.ScreeningJob{}).
				Scopes(gormrepo.WithWorkspace(wsID)).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: job.ID}).
				Updates(map[string]any{
					"state":    "applied",
					"progress": 100,
					"result":   datatypes.JSON(encoded(res)),
				}).Error; err != nil {
				return err
			}
			result = res
			return nil
		}

		// Undo 路径：恢复每条已应用商品到批次前的原始人工结论/备注/分类
		if job.State == "undone" && len(job.UndoResult) > 0 {
			var prev BatchApplyResult
			if json.Unmarshal(job.UndoResult, &prev) == nil {
				result = &prev
				return nil
			}
		}
		if job.State != "applied" {
			return conflict("只有已应用的批次才能撤销")
		}

		appliedSet := map[string]bool{}
		if len(job.Result) > 0 {
			var applyRes BatchApplyResult
			if json.Unmarshal(job.Result, &applyRes) == nil {
				for _, it := range applyRes.Items {
					if it.Success {
						appliedSet[it.ID] = true
					}
				}
			}
		}

		var undoLogs []model.ScreeningReviewLog
		for _, t := range payload.Targets {
			if !appliedSet[t.ID] {
				res.Skipped++
				res.Items = append(res.Items, BatchApplyItem{ID: t.ID, Success: false, Reason: "原批次未修改该商品"})
				continue
			}
			tid, err := parseUintID(t.ID)
			if err != nil {
				continue
			}
			prevGORMStatus := mapLegacyToGORMManual(t.ReviewStatus)
			if prevGORMStatus == "" {
				prevGORMStatus = "pending"
			}
			updates := map[string]any{
				"reviewed_by": operator,
				"reviewed_at": &now,
			}
			if payload.Action != BatchActionReview {
				updates["category_zh"] = stringValue(t.Category)
			} else {
				updates["manual_status"] = prevGORMStatus
				updates["manual_note"] = t.Note
			}
			if err := tx.Model(&model.ScreeningItem{}).
				Scopes(gormrepo.WithWorkspace(wsID)).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: tid}).
				Updates(updates).Error; err != nil {
				return err
			}
			res.Success++
			res.Items = append(res.Items, BatchApplyItem{ID: t.ID, Success: true})
			if payload.Action == BatchActionReview {
				undoLogs = append(undoLogs, model.ScreeningReviewLog{
					WorkspaceID: wsID,
					ItemID:      tid,
					ASIN:        t.ASIN,
					BatchID:     batchTag + "-undo",
					ActionType:  "undo",
					PrevStatus:  mapLegacyToGORMManual(payload.Status),
					NextStatus:  prevGORMStatus,
					Note:        "撤销批次 " + id,
					OperatorID:  operator,
				})
			}
		}
		if len(undoLogs) > 0 {
			if err := tx.CreateInBatches(undoLogs, gormrepo.DefaultBatchSize).Error; err != nil {
				return err
			}
		}
		if err := tx.Model(&model.ScreeningJob{}).
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: job.ID}).
			Updates(map[string]any{
				"state":       "undone",
				"undo_result": datatypes.JSON(encoded(res)),
			}).Error; err != nil {
			return err
		}
		result = res
		return nil
	})
	return result, err
}

func gormJobToObject(j model.ScreeningJob) Object {
	var payloadObj, resultObj, undoObj any
	if len(j.Payload) > 0 {
		_ = json.Unmarshal(j.Payload, &payloadObj)
	}
	if len(j.Result) > 0 {
		_ = json.Unmarshal(j.Result, &resultObj)
	}
	if len(j.UndoResult) > 0 {
		_ = json.Unmarshal(j.UndoResult, &undoObj)
	}
	var expiresAt any
	if j.ExpiresAt != nil {
		expiresAt = j.ExpiresAt.UTC().Format(time.RFC3339Nano)
	}
	var errObj any
	if j.ErrorMessage != "" {
		errObj = Object{"message": j.ErrorMessage}
	}
	return Object{
		"id":          strconv.FormatUint(j.ID, 10),
		"kind":        j.Kind,
		"state":       j.State,
		"progress":    j.Progress,
		"attempts":    j.Attempts,
		"payload":     payloadObj,
		"result":      resultObj,
		"undo_result": undoObj,
		"error":       errObj,
		"expires_at":  expiresAt,
		"created_by":  j.CreatedBy,
		"created_at":  j.CreatedAt.UTC().Format(time.RFC3339Nano),
		"updated_at":  j.UpdatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (s *service) gormListReviewBatchesPersisted(ctx context.Context, a identity, params url.Values) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	p, n := queryPage(params)
	q := s.gormStore.DB().WithContext(ctx).
		Model(&model.ScreeningJob{}).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: "review"})
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, err
	}
	var rows []model.ScreeningJob
	if err := q.
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}, Desc: true}}}).
		Scopes(gormrepo.WithPagination(p, n)).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]Object, 0, len(rows))
	for _, j := range rows {
		items = append(items, gormJobToObject(j))
	}
	return Object{"items": items, "total": total, "page": p, "page_size": n}, nil
}

func (s *service) gormGetReviewBatchUsecase(ctx context.Context, a identity, id string) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	jobID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	var job model.ScreeningJob
	if err := s.gormStore.DB().WithContext(ctx).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: jobID}).
		Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: "review"}).
		First(&job).Error; err != nil {
		return nil, err
	}
	return gormJobToObject(job), nil
}

// ---------------------------------------------------------------------------
// 规则策略 & 利润模板配置（screening_configs）
// ---------------------------------------------------------------------------

func (s *service) gormEnsurePolicy(ctx context.Context, wsID uint64, actorUser int64) error {
	var count int64
	if err := s.gormStore.DB().WithContext(ctx).
		Model(&model.ScreeningConfig{}).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: string(ConfigScreening)}).
		Where(clause.Eq{Column: clause.Column{Name: "active"}, Value: true}).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	cfg := &model.ScreeningConfig{
		WorkspaceID: wsID,
		Kind:        string(ConfigScreening),
		Version:     1,
		Name:        "通用初筛规则",
		Config:      datatypes.JSON(defaultPolicyData),
		ConfigHash:  shaDigest(defaultPolicyData),
		Active:      true,
		CreatedBy:   fmt.Sprintf("user-%d", actorUser),
	}
	return s.gormStore.DB().WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "kind"}, {Name: "version"}},
		DoNothing: true,
	}).Create(cfg).Error
}

func gormConfigToObject(c model.ScreeningConfig) Object {
	cfgObj := decodeObject(c.Config)
	return Object{
		"id":          strconv.FormatUint(c.ID, 10),
		"kind":        c.Kind,
		"version":     c.Version,
		"name":        c.Name,
		"config":      cfgObj,
		"params":      cfgObj,
		"config_hash": c.ConfigHash,
		"active":      c.Active,
		"created_by":  c.CreatedBy,
		"created_at":  c.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
}

func (s *service) gormListConfigsPersisted(ctx context.Context, a identity, kind string) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	if kind == string(ConfigScreening) {
		if err := s.gormEnsurePolicy(ctx, wsID, a.User); err != nil {
			return nil, err
		}
	}
	var rows []model.ScreeningConfig
	if err := s.gormStore.DB().WithContext(ctx).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: kind}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "version"}, Desc: true}}}).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]Object, 0, len(rows))
	for _, r := range rows {
		items = append(items, gormConfigToObject(r))
	}
	return Object{"items": items}, nil
}

func (s *service) gormCreateConfig(ctx context.Context, a identity, cmd CreateConfigCommand) (*ConfigRecord, error) {
	if a.Role != "owner" {
		return nil, forbidden("只有所有者可管理公共配置")
	}
	if cmd.Name == "" || len([]rune(cmd.Name)) > 100 {
		return nil, invalid("名称无效")
	}
	if !cmd.HasConfig {
		return nil, invalid("配置无效")
	}
	if cmd.Kind == ConfigProfitTemplate {
		if _, err := calculate(cmd.Config); err != nil {
			return nil, invalid(err.Error())
		}
	}
	if cmd.Kind == ConfigScreening {
		if err := validatePolicy(cmd.Config); err != nil {
			return nil, invalid(err.Error())
		}
	}
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}

	var created *model.ScreeningConfig
	err = s.gormStore.Transaction(ctx, func(txStore *gormrepo.Store) error {
		tx := txStore.DB()
		var latest model.ScreeningConfig
		nextVer := 1
		err := tx.Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: string(cmd.Kind)}).
			Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "version"}, Desc: true}}}).
			First(&latest).Error
		if err == nil {
			nextVer = latest.Version + 1
		} else if err != gorm.ErrRecordNotFound {
			return err
		}
		raw := encoded(cmd.Config)
		rec := &model.ScreeningConfig{
			WorkspaceID: wsID,
			Kind:        string(cmd.Kind),
			Version:     nextVer,
			Name:        cmd.Name,
			Config:      datatypes.JSON(raw),
			ConfigHash:  shaDigest(raw),
			Active:      false,
			CreatedBy:   fmt.Sprintf("user-%d", a.User),
		}
		if err := tx.Create(rec).Error; err != nil {
			return err
		}
		created = rec
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &ConfigRecord{
		ID:      strconv.FormatUint(created.ID, 10),
		Name:    created.Name,
		Version: created.Version,
		Config:  cmd.Config,
		Params:  cmd.Config,
		Active:  created.Active,
	}, nil
}

func (s *service) gormLoadValidatedPolicy(ctx context.Context, wsID uint64, id string) (*model.ScreeningConfig, Object, error) {
	cfgID, err := parseUintID(id)
	if err != nil {
		return nil, nil, err
	}
	var rec model.ScreeningConfig
	if err := s.gormStore.DB().WithContext(ctx).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: cfgID}).
		Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: string(ConfigScreening)}).
		First(&rec).Error; err != nil {
		return nil, nil, err
	}
	cfgObj := decodeObject(rec.Config)
	if err := validatePolicy(cfgObj); err != nil {
		return nil, nil, invalid(err.Error())
	}
	return &rec, cfgObj, nil
}

func (s *service) gormTestPolicy(ctx context.Context, a identity, id string, cmd TestPolicyCommand) (*PolicyTestResult, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	_, cfgObj, err := s.gormLoadValidatedPolicy(ctx, wsID, id)
	if err != nil {
		return nil, err
	}
	if !cmd.ValidList || len(cmd.ProductIDs) < 1 || len(cmd.ProductIDs) > 100 {
		return nil, invalid("试运行需要1到100个明确商品")
	}
	items := make([]Object, 0, len(cmd.ProductIDs))
	for _, pid := range cmd.ProductIDs {
		val, err := s.gormGetProductUsecase(ctx, a, pid)
		if err != nil {
			return nil, err
		}
		out := classify(val.(Object), cfgObj)
		out["id"] = pid
		items = append(items, out)
	}
	return &PolicyTestResult{Items: items}, nil
}

func (s *service) gormActivatePolicy(ctx context.Context, a identity, id string) (*JobSubmission, error) {
	if a.Role != "owner" {
		return nil, forbidden("只有所有者可启用规则")
	}
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	rec, _, err := s.gormLoadValidatedPolicy(ctx, wsID, id)
	if err != nil {
		return nil, err
	}

	if err := s.gormStore.Transaction(ctx, func(txStore *gormrepo.Store) error {
		tx := txStore.DB()
		if err := tx.Model(&model.ScreeningConfig{}).
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: string(ConfigScreening)}).
			Where(clause.Eq{Column: clause.Column{Name: "active"}, Value: true}).
			Updates(map[string]any{"active": false}).Error; err != nil {
			return err
		}
		return tx.Model(&model.ScreeningConfig{}).
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: rec.ID}).
			Updates(map[string]any{"active": true}).Error
	}); err != nil {
		return nil, err
	}

	return s.gormEnqueueScreen(ctx, a, EnqueueScreenCommand{}, id)
}
