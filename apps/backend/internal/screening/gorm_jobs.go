package screening

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
)

// gormSelection 在当前工作空间内通过纯 GORM API 解析明确商品 ID 集合或筛选条件对应的商品 ID 列表。
func (s *service) gormSelection(ctx context.Context, wsID uint64, cmd ProductSelectionCommand) ([]uint64, error) {
	gdb := s.gormStore.DB().WithContext(ctx)
	if cmd.HasProductIDs {
		if len(cmd.ProductIDs) == 0 || len(cmd.ProductIDs) > 100000 {
			return nil, invalid("商品集合须在1到10万之间")
		}
		ids := make([]uint64, 0, len(cmd.ProductIDs))
		vals := make([]any, 0, len(cmd.ProductIDs))
		seen := map[uint64]bool{}
		for _, raw := range cmd.ProductIDs {
			uid, err := parseUintID(raw)
			if err != nil {
				return nil, invalid("商品ID无效")
			}
			if !seen[uid] {
				seen[uid] = true
				ids = append(ids, uid)
				vals = append(vals, uid)
			}
		}
		var count int64
		if err := gdb.Model(&model.ScreeningItem{}).
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: vals}).
			Count(&count).Error; err != nil {
			return nil, err
		}
		if int(count) != len(ids) {
			return nil, notFound("部分商品不存在或不属于当前工作区")
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		return ids, nil
	}

	f := buildGORMScreeningFilter(cmd.Filters)
	var rows []model.ScreeningItem
	if err := gdb.Model(&model.ScreeningItem{}).
		Select("id").
		Scopes(
			gormrepo.WithWorkspace(wsID),
			gormrepo.WithKeyword(f.Keyword),
			gormrepo.WithBrandCategory(f.BrandCategory),
			gormrepo.WithMachineResult(f.MachineResult),
			gormrepo.WithManualStatus(f.ManualStatus),
			gormrepo.WithProfitCalcStatus(f.ProfitCalcStatus),
			gormrepo.WithPriceRange(f.MinPrice, f.MaxPrice),
			gormrepo.WithSalesRange(f.MinSales, f.MaxSales),
			gormrepo.WithMonthlyMetricFilter(wsID, f.StatMonth, f.MinMonthlySales, f.MaxMonthlySales, f.MinMonthlyPrice, f.MaxMonthlyPrice),
		).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}).
		Limit(100001).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) > 100000 {
		return nil, invalid("一次最多处理10万商品")
	}
	ids := make([]uint64, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids, nil
}

func (s *service) gormQueueExport(ctx context.Context, a identity, cmd ProductSelectionCommand) (*JobSubmission, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	ids, err := s.gormSelection(ctx, wsID, cmd)
	if err != nil {
		return nil, err
	}
	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = strconv.FormatUint(id, 10)
	}

	var jobID uint64
	err = s.gormStore.Transaction(ctx, func(txStore *gormrepo.Store) error {
		tx := txStore.DB()
		taskNo := fmt.Sprintf("EXP-%d-%d", wsID, time.Now().UnixNano())
		expTask := &model.ExportTask{
			WorkspaceID:  wsID,
			TaskNo:       taskNo,
			FilterParams: datatypes.JSON(encoded(Object{"product_ids": strIDs})),
			Status:       "pending",
			OperatorID:   fmt.Sprintf("user-%d", a.User),
		}
		if err := txStore.Exports().UpsertTask(ctx, expTask); err != nil {
			return err
		}
		job := &model.ScreeningJob{
			WorkspaceID:    wsID,
			Kind:           "export",
			State:          "queued",
			IdempotencyKey: a.RequestKey,
			Payload: datatypes.JSON(encoded(Object{
				"engine":         goEngine,
				"export_task_id": expTask.ID,
				"product_ids":    strIDs,
			})),
			CreatedBy: fmt.Sprintf("user-%d", a.User),
		}
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		jobID = job.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &JobSubmission{JobID: strconv.FormatUint(jobID, 10), State: "queued"}, nil
}

func (s *service) gormExportDownload(w http.ResponseWriter, r *http.Request, id string) (any, error) {
	a := actor(r)
	wsID, err := s.resolveWorkspaceID(r.Context(), a)
	if err != nil {
		return nil, err
	}
	jobID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	var job model.ScreeningJob
	if err := s.gormStore.DB().WithContext(r.Context()).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: jobID}).
		Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: "export"}).
		First(&job).Error; err != nil {
		return nil, err
	}
	if job.State != "succeeded" {
		return nil, conflict("导出文件尚未就绪")
	}
	resObj := decodeObject(job.Result)
	filePath := str(resObj["file_path"])
	if filePath == "" {
		return nil, notFound("导出文件不存在")
	}
	cleanBase := filepath.Clean(s.cfg.Storage)
	cleanTarget := filepath.Clean(filePath)
	if !strings.HasPrefix(cleanTarget, cleanBase) {
		return nil, forbidden("导出文件路径无效")
	}
	data, err := os.ReadFile(cleanTarget)
	if err != nil {
		return nil, notFound("导出文件不存在")
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="screening-export-%d.xlsx"`, job.ID))
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
	return nil, nil
}

func (s *service) gormQueueProfit(ctx context.Context, a identity, cmd QueueProfitBatchCommand) (*JobSubmission, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	ids, err := s.gormSelection(ctx, wsID, cmd.Selection)
	if err != nil {
		return nil, err
	}
	if !cmd.HasInputs {
		return nil, invalid("缺少测算输入")
	}
	if _, err := calculate(cmd.Inputs); err != nil {
		return nil, invalid(err.Error())
	}
	if tid := stringValue(cmd.TemplateID); tid != "" {
		cfgID, err := parseUintID(tid)
		if err != nil {
			return nil, invalid("模板ID无效")
		}
		var tmpl model.ScreeningConfig
		if err := s.gormStore.DB().WithContext(ctx).
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: cfgID}).
			Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: string(ConfigProfitTemplate)}).
			First(&tmpl).Error; err != nil {
			return nil, err
		}
	}

	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = strconv.FormatUint(id, 10)
	}
	job := &model.ScreeningJob{
		WorkspaceID:    wsID,
		Kind:           "profit_batch",
		State:          "queued",
		IdempotencyKey: a.RequestKey,
		Payload: datatypes.JSON(encoded(Object{
			"engine":      goEngine,
			"product_ids": strIDs,
			"inputs":      cmd.Inputs,
			"template_id": cmd.TemplateID,
		})),
		CreatedBy: fmt.Sprintf("user-%d", a.User),
	}
	if err := s.gormStore.DB().WithContext(ctx).Create(job).Error; err != nil {
		return nil, err
	}
	return &JobSubmission{JobID: strconv.FormatUint(job.ID, 10), State: "queued"}, nil
}

func (s *service) gormEnqueueScreen(ctx context.Context, a identity, cmd EnqueueScreenCommand, policyID string) (*JobSubmission, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	if err := s.gormEnsurePolicy(ctx, wsID, a.User); err != nil {
		return nil, err
	}

	var policyRec model.ScreeningConfig
	if policyID != "" {
		rec, _, err := s.gormLoadValidatedPolicy(ctx, wsID, policyID)
		if err != nil {
			return nil, err
		}
		policyRec = *rec
	} else {
		if err := s.gormStore.DB().WithContext(ctx).
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: string(ConfigScreening)}).
			Where(clause.Eq{Column: clause.Column{Name: "active"}, Value: true}).
			First(&policyRec).Error; err != nil {
			return nil, err
		}
	}

	var ids []uint64
	if cmd.Selection.HasProductIDs || cmd.Selection.HasFilterKeys || len(cmd.Selection.Filters) > 0 {
		ids, err = s.gormSelection(ctx, wsID, cmd.Selection)
		if err != nil {
			return nil, err
		}
	}
	strIDs := make([]string, len(ids))
	for i, id := range ids {
		strIDs[i] = strconv.FormatUint(id, 10)
	}

	job := &model.ScreeningJob{
		WorkspaceID:    wsID,
		Kind:           "screen",
		State:          "queued",
		IdempotencyKey: a.RequestKey,
		Payload: datatypes.JSON(encoded(Object{
			"engine":      goEngine,
			"policy_id":   strconv.FormatUint(policyRec.ID, 10),
			"policy":      gormConfigToObject(policyRec),
			"product_ids": strIDs,
		})),
		CreatedBy: fmt.Sprintf("user-%d", a.User),
	}
	if err := s.gormStore.DB().WithContext(ctx).Create(job).Error; err != nil {
		return nil, err
	}
	idStr := strconv.FormatUint(job.ID, 10)
	return &JobSubmission{JobID: idStr, RunID: idStr, State: "queued"}, nil
}

func (s *service) gormGetPublicJob(ctx context.Context, a identity, id string) (Object, error) {
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
		First(&job).Error; err != nil {
		return nil, err
	}
	obj := gormJobToObject(job)
	delete(obj, "payload")
	return obj, nil
}

// ---------------------------------------------------------------------------
// 导入历史变更（RemovalPreview / DeleteImport / RetryJob / EditImport）
// ---------------------------------------------------------------------------

func (s *service) gormImportRemovalPreview(ctx context.Context, a identity, id string) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	taskID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	if _, err := s.gormStore.Imports().GetTaskByID(ctx, wsID, taskID); err != nil {
		return nil, err
	}
	gdb := s.gormStore.DB().WithContext(ctx)
	var sourceRows int64
	if err := gdb.Model(&model.ImportRecord{}).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: taskID}).
		Count(&sourceRows).Error; err != nil {
		return nil, err
	}
	var exclusiveProducts int64
	if err := gdb.Model(&model.ScreeningItem{}).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "last_import_task_id"}, Value: taskID}).
		Count(&exclusiveProducts).Error; err != nil {
		return nil, err
	}
	return Object{
		"products":           exclusiveProducts,
		"exclusive_products": exclusiveProducts,
		"source_rows":        sourceRows,
	}, nil
}

func (s *service) gormRemoveImportHistory(ctx context.Context, a identity, id string) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	taskID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	if _, err := s.gormStore.Imports().GetTaskByID(ctx, wsID, taskID); err != nil {
		return nil, err
	}

	var removedMonthly int64
	var affectedItemIDs []uint64
	err = s.gormStore.Transaction(ctx, func(txStore *gormrepo.Store) error {
		tx := txStore.DB()
		var metricItems []model.ScreeningItemMonthlyMetric
		if err := tx.Select("DISTINCT item_id").
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "source_task_id"}, Value: taskID}).
			Find(&metricItems).Error; err != nil {
			return err
		}
		for _, m := range metricItems {
			if m.ItemID > 0 {
				affectedItemIDs = append(affectedItemIDs, m.ItemID)
			}
		}

		resM := tx.Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "source_task_id"}, Value: taskID}).
			Delete(&model.ScreeningItemMonthlyMetric{})
		if resM.Error != nil {
			return resM.Error
		}
		removedMonthly = resM.RowsAffected

		if err := tx.Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: taskID}).
			Delete(&model.ImportRecord{}).Error; err != nil {
			return err
		}
		if err := tx.Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: taskID}).
			Delete(&model.ImportTask{}).Error; err != nil {
			return err
		}
		if len(affectedItemIDs) > 0 {
			vals := make([]any, len(affectedItemIDs))
			for i, itemID := range affectedItemIDs {
				vals[i] = itemID
			}
			var items []model.ScreeningItem
			if err := tx.Scopes(gormrepo.WithWorkspace(wsID)).
				Where(clause.IN{Column: clause.Column{Name: "id"}, Values: vals}).
				Find(&items).Error; err != nil {
				return err
			}
			for _, it := range items {
				if len(it.SourceData) == 0 {
					continue
				}
				src := decodeObject(it.SourceData)
				if removeImportFromSourceMonthlyHistory(src, id) {
					if err := tx.Model(&model.ScreeningItem{}).
						Scopes(gormrepo.WithWorkspace(wsID)).
						Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: it.ID}).
						Updates(map[string]any{"source_data": datatypes.JSON(encoded(src))}).Error; err != nil {
						return err
					}
				}
			}
			if err := txStore.MonthlyMetrics().SyncLatestToItems(ctx, wsID, affectedItemIDs); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return Object{
		"id":                id,
		"deleted":           true,
		"removed_products":  int64(0),
		"retained_products": int64(0),
		"removed_monthly":   removedMonthly,
	}, nil
}

func (s *service) gormRetryJob(ctx context.Context, a identity, id string) (*JobSubmission, error) {
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
		First(&job).Error; err != nil {
		return nil, err
	}
	if job.State != "failed" {
		return nil, conflict("仅失败任务允许重试")
	}
	if err := s.gormStore.DB().WithContext(ctx).
		Model(&model.ScreeningJob{}).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: jobID}).
		Updates(map[string]any{
			"state":         "queued",
			"progress":      0,
			"error_message": "",
		}).Error; err != nil {
		return nil, err
	}
	return &JobSubmission{JobID: id, State: "queued"}, nil
}

// ---------------------------------------------------------------------------
// MySQL GORM 后台任务处理循环（parse / import / export / profit_batch / screen）
// ---------------------------------------------------------------------------

func (s *service) gormWorker(ctx context.Context) error {
	ticker := time.NewTicker(800 * time.Millisecond)
	defer ticker.Stop()
	for {
		processed, err := s.gormProcessNextJob(ctx)
		if err != nil && ctx.Err() == nil {
			log.Printf("screening gorm worker error: %v", err)
		}
		if processed {
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (s *service) gormProcessNextJob(ctx context.Context) (bool, error) {
	gdb := s.gormStore.DB().WithContext(ctx)
	var job model.ScreeningJob
	err := gdb.
		Where(clause.Eq{Column: clause.Column{Name: "state"}, Value: "queued"}).
		Where(clause.IN{Column: clause.Column{Name: "kind"}, Values: []any{"parse", "import", "export", "profit_batch", "screen"}}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}).
		First(&job).Error
	if err != nil {
		return false, nil
	}

	// 原子抢占任务（仅当 state 仍为 queued 时置为 running）
	claimed := gdb.Model(&model.ScreeningJob{}).
		Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: job.ID}).
		Where(clause.Eq{Column: clause.Column{Name: "state"}, Value: "queued"}).
		Updates(map[string]any{
			"state":    "running",
			"attempts": job.Attempts + 1,
		})
	if claimed.Error != nil || claimed.RowsAffected == 0 {
		return false, claimed.Error
	}

	var runErr error
	var result Object
	switch job.Kind {
	case "parse":
		result, runErr = s.gormExecuteParseJob(ctx, job)
	case "import":
		result, runErr = s.gormExecuteImportJob(ctx, job)
	case "export":
		result, runErr = s.gormExecuteExportJob(ctx, job)
	case "profit_batch":
		result, runErr = s.gormExecuteProfitBatchJob(ctx, job)
	case "screen":
		result, runErr = s.gormExecuteScreenJob(ctx, job)
	}

	if runErr != nil {
		_ = gdb.Model(&model.ScreeningJob{}).
			Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: job.ID}).
			Updates(map[string]any{
				"state":         "failed",
				"error_message": runErr.Error(),
			}).Error
		return true, runErr
	}

	_ = gdb.Model(&model.ScreeningJob{}).
		Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: job.ID}).
		Updates(map[string]any{
			"state":    "succeeded",
			"progress": 100,
			"result":   datatypes.JSON(encoded(result)),
		}).Error
	return true, nil
}

func (s *service) gormExecuteExportJob(ctx context.Context, job model.ScreeningJob) (Object, error) {
	payload := decodeObject(job.Payload)
	rawIDs, _ := payload["product_ids"].([]any)
	exportTaskID := uint64(integer(payload["export_task_id"]))

	ids := make([]uint64, 0, len(rawIDs))
	vals := make([]any, 0, len(rawIDs))
	for _, r := range rawIDs {
		if uid, err := parseUintID(str(r)); err == nil {
			ids = append(ids, uid)
			vals = append(vals, uid)
		}
	}

	var items []model.ScreeningItem
	if len(vals) > 0 {
		if err := s.gormStore.DB().WithContext(ctx).
			Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
			Where(clause.IN{Column: clause.Column{Name: "id"}, Values: vals}).
			Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}).
			Find(&items).Error; err != nil {
			return nil, err
		}
	}

	f := excelize.NewFile()
	sheet := f.GetSheetName(0)
	headers := []string{"ASIN", "父ASIN", "中文标题", "英文标题", "品牌", "类目", "最新售价(USD)", "最新月销量", "评分", "评论数", "机器结论", "人工结论", "利润率", "预计利润(USD)"}
	for col, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(col+1, 1)
		_ = f.SetCellValue(sheet, cell, h)
	}
	for rowIdx, it := range items {
		r := rowIdx + 2
		marginStr, profitStr := "", ""
		if it.ProfitMargin != nil {
			marginStr = fmt.Sprintf("%.4f", *it.ProfitMargin)
		}
		if it.EstimatedProfit != nil {
			profitStr = fmt.Sprintf("%.2f", *it.EstimatedProfit)
		}
		row := []any{
			it.ASIN, it.ParentASIN, it.TitleZh, it.TitleEn, it.Brand, it.CategoryZh,
			it.LatestPrice, it.LatestSales, it.Rating, it.ReviewCount,
			it.MachineResult, it.ManualStatus, marginStr, profitStr,
		}
		for col, v := range row {
			cell, _ := excelize.CoordinatesToCellName(col+1, r)
			_ = f.SetCellValue(sheet, cell, v)
		}
	}

	storageDir := s.cfg.Storage
	if storageDir == "" {
		storageDir = os.TempDir()
	}
	exportDir := filepath.Join(storageDir, "exports")
	if err := os.MkdirAll(exportDir, 0o750); err != nil {
		return nil, err
	}
	outPath := filepath.Join(exportDir, fmt.Sprintf("export-%d.xlsx", job.ID))
	if err := f.SaveAs(outPath); err != nil {
		return nil, err
	}
	_ = f.Close()

	now := time.Now()
	if exportTaskID > 0 {
		_ = s.gormStore.Exports().UpdateTaskStatus(ctx, job.WorkspaceID, exportTaskID, "completed", len(items), outPath, "", &now)
	}
	return Object{
		"exported_rows": len(items),
		"file_path":     outPath,
		"download_url":  fmt.Sprintf("/api/v1/screening/exports/%d/download", job.ID),
	}, nil
}

func (s *service) gormExecuteProfitBatchJob(ctx context.Context, job model.ScreeningJob) (Object, error) {
	payload := decodeObject(job.Payload)
	rawIDs, _ := payload["product_ids"].([]any)
	inputs, _ := payload["inputs"].(map[string]any)
	if inputs == nil {
		return nil, fmt.Errorf("missing inputs in profit batch payload")
	}

	var succeeded, failed int
	for _, r := range rawIDs {
		uid, err := parseUintID(str(r))
		if err != nil {
			failed++
			continue
		}
		item, err := s.gormStore.Screening().GetByID(ctx, job.WorkspaceID, uid)
		if err != nil {
			failed++
			continue
		}
		inCopy := Object{}
		for k, v := range inputs {
			inCopy[k] = v
		}
		if parseFloatAny(inCopy["sale_price_usd"]) <= 0 && item.LatestPrice > 0 {
			inCopy["sale_price_usd"] = fmt.Sprintf("%.2f", item.LatestPrice)
		}
		itemObj := gormItemToObject(gormrepo.ScreeningItemWithDetails{Item: *item})
		out, err := calculateProductProfitStructured(inCopy, itemObj)
		if err != nil {
			failed++
			continue
		}
		fx := parseFloatAny(inCopy["fx_cny_per_usd"])
		if fx <= 0 {
			fx = 7.2
		}
		sellingPrice := parseFloatAny(inCopy["sale_price_usd"])
		var referralFee, storageUSD float64
		if out.CommissionUSD != nil {
			referralFee = parseFloatAny(*out.CommissionUSD)
		}
		if out.StorageUSD != nil {
			storageUSD = parseFloatAny(*out.StorageUSD)
		}
		calc := &model.ProfitCalculation{
			WorkspaceID:     job.WorkspaceID,
			ItemID:          item.ID,
			ASIN:            item.ASIN,
			RefStatMonth:    normalizeQueryMonth(str(inCopy["storage_month"])),
			SellingPrice:    sellingPrice,
			PurchaseCost:    parseFloatAny(inCopy["purchase_cny"]) / fx,
			FirstLegFreight: parseFloatAny(inCopy["freight_cny"]) / fx,
			ReferralFee:     referralFee,
			FulfillmentFee:  parseFloatAny(inCopy["fba_fee_usd"]),
			AdsAndOtherCost: sellingPrice*parseFloatAny(inCopy["ad_rate"]) + storageUSD + parseFloatAny(inCopy["other_usd"]),
			ProfitMargin:    parseFloatAny(out.Margin),
			EstimatedProfit: parseFloatAny(out.ProfitUSD),
			Currency:        item.Currency,
			CalculatedBy:    job.CreatedBy,
		}
		if err := s.gormStore.ProfitCalcs().SaveActiveCalculation(ctx, job.WorkspaceID, calc); err != nil {
			failed++
			continue
		}
		succeeded++
	}
	return Object{"succeeded": succeeded, "failed": failed}, nil
}

func (s *service) gormExecuteScreenJob(ctx context.Context, job model.ScreeningJob) (Object, error) {
	payload := decodeObject(job.Payload)
	policyMap, _ := payload["policy"].(map[string]any)
	cfgObj, _ := policyMap["config"].(map[string]any)
	if cfgObj == nil {
		cfgObj = decodeObject(defaultPolicyData)
	}
	policyID := str(payload["policy_id"])
	rawIDs, _ := payload["product_ids"].([]any)

	q := s.gormStore.DB().WithContext(ctx).
		Model(&model.ScreeningItem{}).
		Scopes(gormrepo.WithWorkspace(job.WorkspaceID))
	if len(rawIDs) > 0 {
		vals := make([]any, 0, len(rawIDs))
		for _, r := range rawIDs {
			if uid, err := parseUintID(str(r)); err == nil {
				vals = append(vals, uid)
			}
		}
		q = q.Where(clause.IN{Column: clause.Column{Name: "id"}, Values: vals})
	}

	var items []model.ScreeningItem
	if err := q.Find(&items).Error; err != nil {
		return nil, err
	}

	var passed, rejected int
	for _, it := range items {
		itemObj := gormItemToObject(gormrepo.ScreeningItemWithDetails{Item: it})
		res := classify(itemObj, cfgObj)
		mappedDecision := mapLegacyToGORMMachine(str(res["decision"]))
		if mappedDecision == "passed" {
			passed++
		} else if mappedDecision == "rejected" {
			rejected++
		}
		reasonsJSON := datatypes.JSON(encoded(res["reasons"]))
		if !json.Valid(reasonsJSON) {
			reasonsJSON = datatypes.JSON("[]")
		}
		if err := s.gormStore.DB().WithContext(ctx).
			Model(&model.ScreeningItem{}).
			Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: it.ID}).
			Updates(map[string]any{
				"machine_result":       mappedDecision,
				"machine_rule_version": policyID,
				"machine_reasons":      reasonsJSON,
			}).Error; err != nil {
			return nil, err
		}
	}
	return Object{"total": len(items), "passed": passed, "rejected": rejected}, nil
}
