package screening

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
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

// unpackImportRecordPayload 兼容迁移后的扁平原始 JSON 与新增的包装快照格式。
func unpackImportRecordPayload(raw datatypes.JSON) (rawData Object, normalized Object, memberPath, sheetName string, origRowNo int, errs []string) {
	rawData = Object{}
	obj := decodeObject(raw)
	if innerRaw, ok := obj["raw"].(map[string]any); ok {
		rawData = innerRaw
		if n, ok := obj["normalized"].(map[string]any); ok {
			normalized = n
		}
		memberPath = str(obj["member_path"])
		sheetName = str(obj["sheet_name"])
		origRowNo = int(integer(obj["row_no"]))
		if rawErrs, ok := obj["errors"].([]any); ok {
			for _, e := range rawErrs {
				if s := str(e); s != "" {
					errs = append(errs, s)
				}
			}
		}
		return
	}
	rawData = obj
	return
}

func (s *service) gormLatestParseJob(ctx context.Context, wsID uint64, importID string) (*model.ScreeningJob, Object, error) {
	var jobs []model.ScreeningJob
	if err := s.gormStore.DB().WithContext(ctx).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: "parse"}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}, Desc: true}}}).
		Limit(200).
		Find(&jobs).Error; err != nil {
		return nil, nil, err
	}
	for i := range jobs {
		payload := decodeObject(jobs[i].Payload)
		if str(payload["import_id"]) == importID {
			return &jobs[i], payload, nil
		}
	}
	return nil, nil, gorm.ErrRecordNotFound
}

func (s *service) gormLatestImportJob(ctx context.Context, wsID uint64, importID string) (*model.ScreeningJob, error) {
	var jobs []model.ScreeningJob
	if err := s.gormStore.DB().WithContext(ctx).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "kind"}, Value: "import"}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}, Desc: true}}}).
		Limit(200).
		Find(&jobs).Error; err != nil {
		return nil, err
	}
	for i := range jobs {
		payload := decodeObject(jobs[i].Payload)
		if str(payload["import_id"]) == importID {
			return &jobs[i], nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

// gormUpload 处理 MySQL GORM 模式下的文件上传并入队异步解析任务。
func (s *service) gormUpload(w http.ResponseWriter, r *http.Request) (any, error) {
	a := actor(r)
	wsID, err := s.resolveWorkspaceID(r.Context(), a)
	if err != nil {
		return nil, err
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload+64*1024)
	if e := r.ParseMultipartForm(1024 * 1024); e != nil {
		return nil, payloadTooLarge("文件请求无效或超过30MB")
	}
	defer r.MultipartForm.RemoveAll()
	f, header, e := r.FormFile("file")
	if e != nil {
		return nil, invalid("请选择文件")
	}
	defer f.Close()
	name := filepath.Base(header.Filename)
	ext := strings.ToLower(filepath.Ext(name))
	if ext != ".zip" && ext != ".xlsx" && ext != ".csv" {
		return nil, invalid("只支持XLSX、CSV和ZIP")
	}
	data, e := io.ReadAll(io.LimitReader(f, maxUpload+1))
	if e != nil {
		return nil, e
	}
	if len(data) > maxUpload {
		return nil, payloadTooLarge("文件超过30MB")
	}
	b := Object{}
	for _, key := range []string{"source_period", "observed_on", "period_type", "period_start", "period_end", "source_type", "default_sales_scope"} {
		b[key] = r.FormValue(key)
	}
	applyImportDefaults(b, name, time.Now())
	meta, e := resolveImportPeriod(b, false)
	if e != nil {
		return nil, e
	}
	meta.ApplyToBatch(b)
	if b["source_type"] == nil || str(b["source_type"]) == "" {
		b["source_type"] = "sellersprite"
	}
	if b["default_sales_scope"] == nil || str(b["default_sales_scope"]) == "" {
		b["default_sales_scope"] = "unknown"
	}
	for _, key := range []string{"observed_on", "period_start", "period_end"} {
		if str(b[key]) == "" {
			b[key] = nil
		}
	}

	hash := shaDigest(data)
	mapping := Object{}
	mappingHash := shaDigest(encoded(mapping))
	storageUID := uuid()
	key := fmt.Sprintf("%d/imports/%s%s", wsID, storageUID, ext)
	target := filepath.Join(s.cfg.Storage, filepath.FromSlash(key))
	if e = os.MkdirAll(filepath.Dir(target), 0750); e != nil {
		return nil, e
	}
	if e = os.WriteFile(target, data, 0640); e != nil {
		return nil, e
	}

	var taskID, jobID uint64
	err = s.gormStore.Transaction(r.Context(), func(txStore *gormrepo.Store) error {
		task := &model.ImportTask{
			WorkspaceID: wsID,
			TaskNo:      fmt.Sprintf("IMP-%d-%s", wsID, strings.ReplaceAll(storageUID, "-", "")[:12]),
			FileName:    name,
			SourceType:  str(b["source_type"]),
			Status:      "parsing",
			OperatorID:  fmt.Sprintf("user-%d", a.User),
		}
		if err := txStore.DB().Create(task).Error; err != nil {
			return err
		}
		taskID = task.ID
		importIDStr := strconv.FormatUint(task.ID, 10)
		payload := Object{
			"engine":              goEngine,
			"import_id":           importIDStr,
			"storage_key":         key,
			"file_name":           name,
			"file_sha256":         hash,
			"file_size":           len(data),
			"mapping":             mapping,
			"mapping_hash":        mappingHash,
			"source_period":       str(b["source_period"]),
			"observed_on":         b["observed_on"],
			"source_type":         str(b["source_type"]),
			"period_type":         str(b["period_type"]),
			"period_start":        b["period_start"],
			"period_end":          b["period_end"],
			"default_sales_scope": str(b["default_sales_scope"]),
			"generation":          int64(1),
		}
		job := &model.ScreeningJob{
			WorkspaceID:    wsID,
			Kind:           "parse",
			State:          "queued",
			IdempotencyKey: a.RequestKey,
			Payload:        datatypes.JSON(encoded(payload)),
			CreatedBy:      fmt.Sprintf("user-%d", a.User),
		}
		if err := txStore.DB().Create(job).Error; err != nil {
			return err
		}
		jobID = job.ID
		return nil
	})
	if err != nil {
		_ = os.Remove(target)
		return nil, err
	}
	return &JobSubmission{
		ImportID: strconv.FormatUint(taskID, 10),
		JobID:    strconv.FormatUint(jobID, 10),
		State:    "queued",
	}, nil
}

// gormExecuteParseJob 在 MySQL GORM 模式下解析已上传文件并写入待提交的 ImportRecord。
func (s *service) gormExecuteParseJob(ctx context.Context, job model.ScreeningJob) (Object, error) {
	payload := decodeObject(job.Payload)
	importIDStr := str(payload["import_id"])
	taskID, err := parseUintID(importIDStr)
	if err != nil {
		return nil, invalid("导入任务ID无效")
	}
	task, err := s.gormStore.Imports().GetTaskByID(ctx, job.WorkspaceID, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Object{"deleted": true}, nil
		}
		return nil, err
	}
	if task.Status != "parsing" && task.Status != "failed" && task.Status != "pending" {
		return Object{"skipped": true}, nil
	}
	if latestJob, latestPayload, err := s.gormLatestParseJob(ctx, job.WorkspaceID, importIDStr); err == nil && latestJob != nil {
		if integer(latestPayload["generation"]) > integer(payload["generation"]) {
			return Object{"superseded": true}, nil
		}
	}

	storageKey := str(payload["storage_key"])
	if storageKey == "" {
		return nil, invalid("缺少导入源文件存储路径")
	}
	cleanBase := filepath.Clean(s.cfg.Storage)
	target := filepath.Clean(filepath.Join(cleanBase, filepath.FromSlash(storageKey)))
	if !strings.HasPrefix(target, cleanBase) {
		return nil, forbidden("导入文件路径无效")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	mapping, _ := payload["mapping"].(map[string]any)
	fileName := str(payload["file_name"])
	if fileName == "" {
		fileName = task.FileName
	}

	total, validCount, invalidCount := 0, 0, 0
	headers := map[string]bool{}
	skipped := []Object{}
	records := make([]model.ImportRecord, 0, 256)
	seq := 0
	parseErr := parseSourceReport(data, fileName, mapping, func(row sourceRow) error {
		total++
		seq++
		for header := range row.Raw {
			headers[header] = true
		}
		recStatus := "valid"
		errMsg := ""
		if len(row.Errors) > 0 {
			recStatus = "failed"
			invalidCount++
			errMsg = strings.Join(row.Errors, "; ")
		} else {
			validCount++
		}
		asin := ""
		if row.Normalized != nil {
			asin = strings.ToUpper(strings.TrimSpace(str(row.Normalized["asin"])))
		}
		wrapped := Object{
			"raw":         row.Raw,
			"normalized":  row.Normalized,
			"member_path": row.Member,
			"sheet_name":  row.Sheet,
			"row_no":      row.Number,
			"errors":      row.Errors,
		}
		records = append(records, model.ImportRecord{
			WorkspaceID:  job.WorkspaceID,
			TaskID:       taskID,
			RowNo:        seq,
			ASIN:         asin,
			RawPayload:   datatypes.JSON(encoded(wrapped)),
			Status:       recStatus,
			ErrorMessage: errMsg,
		})
		return nil
	}, func(sheet Object) { skipped = append(skipped, sheet) })
	if parseErr != nil {
		_ = s.gormStore.Imports().UpdateTaskProgress(ctx, job.WorkspaceID, taskID, "failed", total, validCount, invalidCount, 0, parseErr.Error(), nil)
		return nil, parseErr
	}

	if err := s.gormStore.Transaction(ctx, func(txStore *gormrepo.Store) error {
		tx := txStore.DB()
		if err := tx.Unscoped().
			Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
			Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: taskID}).
			Where(clause.Neq{Column: clause.Column{Name: "status"}, Value: "success"}).
			Delete(&model.ImportRecord{}).Error; err != nil {
			return err
		}
		if err := txStore.Imports().BatchUpsertRecords(ctx, job.WorkspaceID, records); err != nil {
			return err
		}
		return txStore.Imports().UpdateTaskProgress(ctx, job.WorkspaceID, taskID, "preview", total, validCount, invalidCount, 0, "", nil)
	}); err != nil {
		return nil, err
	}

	return Object{
		"import_id":      importIDStr,
		"total":          total,
		"valid":          validCount,
		"invalid":        invalidCount,
		"skipped_sheets": skipped,
		"headers":        sortedHeaders(headers),
	}, nil
}

// gormEditImport 处理 MySQL GORM 模式下的映射/周期更新（重新入队 parse）与提交入库（入队 import）。
func (s *service) gormEditImport(ctx context.Context, a identity, id string, b Object, commit bool) (*JobSubmission, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	taskID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	task, err := s.gormStore.Imports().GetTaskByID(ctx, wsID, taskID)
	if err != nil {
		return nil, err
	}
	_, latestPayload, parseErr := s.gormLatestParseJob(ctx, wsID, id)
	batchMeta := Object{}
	if parseErr == nil && latestPayload != nil {
		for k, v := range latestPayload {
			batchMeta[k] = v
		}
	}
	if str(batchMeta["file_name"]) == "" {
		batchMeta["file_name"] = task.FileName
	}
	if str(batchMeta["source_type"]) == "" {
		batchMeta["source_type"] = task.SourceType
	}

	if commit {
		if task.Status == "importing" || task.Status == "completed" || task.Status == "partial" {
			if existingJob, err := s.gormLatestImportJob(ctx, wsID, id); err == nil && existingJob != nil {
				return &JobSubmission{
					JobID:    strconv.FormatUint(existingJob.ID, 10),
					ImportID: id,
					State:    existingJob.State,
				}, nil
			}
			return &JobSubmission{ImportID: id, State: task.Status}, nil
		}
		if task.Status != "preview" {
			return nil, conflict("请等待解析完成")
		}
		meta, err := resolveImportPeriod(batchMeta, true)
		if err != nil {
			return nil, err
		}
		meta.ApplyToBatch(batchMeta)

		var jobID uint64
		err = s.gormStore.Transaction(ctx, func(txStore *gormrepo.Store) error {
			if err := txStore.DB().Model(&model.ImportTask{}).
				Scopes(gormrepo.WithWorkspace(wsID)).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: taskID}).
				Updates(map[string]any{"status": "importing"}).Error; err != nil {
				return err
			}
			payload := Object{
				"engine":              goEngine,
				"import_id":           id,
				"stat_month":          meta.StatMonth,
				"period_type":         meta.PeriodType,
				"period_start":        meta.PeriodStart,
				"period_end":          meta.PeriodEnd,
				"observed_on":         meta.ObservedOn,
				"source_type":         meta.SourceType,
				"source_period":       meta.SourcePeriod,
				"default_sales_scope": meta.DefaultSalesScope,
			}
			job := &model.ScreeningJob{
				WorkspaceID:    wsID,
				Kind:           "import",
				State:          "queued",
				IdempotencyKey: a.RequestKey,
				Payload:        datatypes.JSON(encoded(payload)),
				CreatedBy:      fmt.Sprintf("user-%d", a.User),
			}
			if err := txStore.DB().Create(job).Error; err != nil {
				return err
			}
			jobID = job.ID
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &JobSubmission{JobID: strconv.FormatUint(jobID, 10), ImportID: id, State: "queued"}, nil
	}

	if task.Status != "preview" && task.Status != "failed" {
		return nil, conflict("当前状态不能调整映射")
	}
	var committedCount int64
	if err := s.gormStore.DB().WithContext(ctx).
		Model(&model.ImportRecord{}).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: taskID}).
		Where(clause.Eq{Column: clause.Column{Name: "status"}, Value: "success"}).
		Count(&committedCount).Error; err != nil {
		return nil, err
	}
	if committedCount > 0 {
		return nil, conflict("该批次已有入库数据，请重试原导入任务；调整映射请新建导入")
	}
	for key, value := range b {
		batchMeta[key] = value
	}
	applyImportDefaults(batchMeta, task.FileName, time.Now())
	meta, err := resolveImportPeriod(batchMeta, false)
	if err != nil {
		return nil, err
	}
	meta.ApplyToBatch(batchMeta)
	mapping, ok := batchMeta["mapping"].(map[string]any)
	if !ok {
		mapping = Object{}
		batchMeta["mapping"] = mapping
	}
	if len(mapping) > len(normalConfig.Aliases) {
		return nil, invalid("映射无效")
	}
	for key, value := range mapping {
		if normalConfig.Aliases[key] == nil || len(str(value)) > 200 {
			return nil, invalid("映射字段无效")
		}
	}
	nextGen := integer(batchMeta["generation"]) + 1
	batchMeta["engine"] = goEngine
	batchMeta["import_id"] = id
	batchMeta["mapping_hash"] = shaDigest(encoded(mapping))
	batchMeta["generation"] = nextGen

	var jobID uint64
	err = s.gormStore.Transaction(ctx, func(txStore *gormrepo.Store) error {
		if err := txStore.DB().Model(&model.ImportTask{}).
			Scopes(gormrepo.WithWorkspace(wsID)).
			Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: taskID}).
			Updates(map[string]any{"status": "parsing"}).Error; err != nil {
			return err
		}
		job := &model.ScreeningJob{
			WorkspaceID:    wsID,
			Kind:           "parse",
			State:          "queued",
			IdempotencyKey: a.RequestKey,
			Payload:        datatypes.JSON(encoded(batchMeta)),
			CreatedBy:      fmt.Sprintf("user-%d", a.User),
		}
		if err := txStore.DB().Create(job).Error; err != nil {
			return err
		}
		jobID = job.ID
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &JobSubmission{JobID: strconv.FormatUint(jobID, 10), ImportID: id, State: "queued"}, nil
}

// mergeSourceMonthlyHistory 将增量月份观测无损合并进 SourceData["source_monthly_history"]，按 stat_month 降序排列。
func mergeSourceMonthlyHistory(source Object, incoming []Object) {
	byMonth := map[string]Object{}
	if existing, ok := source["source_monthly_history"].([]any); ok {
		for _, v := range existing {
			row, ok := v.(map[string]any)
			if !ok {
				continue
			}
			mKey := normalizeQueryMonth(str(row["stat_month"]))
			if mKey != "" {
				byMonth[mKey] = row
			}
		}
	}
	for _, row := range incoming {
		mKey := normalizeQueryMonth(str(row["stat_month"]))
		if mKey == "" {
			continue
		}
		if prev, ok := byMonth[mKey]; ok {
			merged := Object{}
			for k, v := range prev {
				merged[k] = v
			}
			for k, v := range row {
				if v != nil && str(v) != "" {
					merged[k] = v
				}
			}
			byMonth[mKey] = merged
		} else {
			byMonth[mKey] = row
		}
	}
	keys := make([]string, 0, len(byMonth))
	for k := range byMonth {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] > keys[j] })
	list := make([]any, 0, len(keys))
	for _, k := range keys {
		list = append(list, byMonth[k])
	}
	source["source_monthly_history"] = list
}

func latestMonthInSource(source Object) string {
	best := ""
	if existing, ok := source["source_monthly_history"].([]any); ok {
		for _, v := range existing {
			if row, ok := v.(map[string]any); ok {
				m := normalizeQueryMonth(str(row["stat_month"]))
				if m > best {
					best = m
				}
			}
		}
	}
	if m := normalizeQueryMonth(str(source["stat_month"])); m > best {
		best = m
	}
	return best
}

// gormExecuteImportJob 在 MySQL GORM 模式下执行增量导入提交，严禁覆盖已有人工复核与利润测算结论，且保留迁移快照字段。
func (s *service) gormExecuteImportJob(ctx context.Context, job model.ScreeningJob) (Object, error) {
	payload := decodeObject(job.Payload)
	importIDStr := str(payload["import_id"])
	taskID, err := parseUintID(importIDStr)
	if err != nil {
		return nil, invalid("导入任务ID无效")
	}
	task, err := s.gormStore.Imports().GetTaskByID(ctx, job.WorkspaceID, taskID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return Object{"deleted": true}, nil
		}
		return nil, err
	}
	batchMeta := Object{}
	if _, parsePayload, err := s.gormLatestParseJob(ctx, job.WorkspaceID, importIDStr); err == nil && parsePayload != nil {
		for k, v := range parsePayload {
			batchMeta[k] = v
		}
	}
	for k, v := range payload {
		if v != nil && str(v) != "" {
			batchMeta[k] = v
		}
	}
	meta, err := resolveImportPeriod(batchMeta, true)
	if err != nil {
		return nil, err
	}
	statMonthYM := normalizeQueryMonth(meta.StatMonth)
	if statMonthYM == "" {
		statMonthYM = time.Now().UTC().Format("2006-01")
	}
	statMonthFull := statMonthYM + "-01"

	var validRows []model.ImportRecord
	if err := s.gormStore.DB().WithContext(ctx).
		Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: taskID}).
		Where(clause.Eq{Column: clause.Column{Name: "status"}, Value: "valid"}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "row_no"}}}}).
		Find(&validRows).Error; err != nil {
		return nil, err
	}

	affectedItemSet := map[uint64]bool{}
	committedCount := 0
	for _, rec := range validRows {
		_, normalized, _, _, _, _ := unpackImportRecordPayload(rec.RawPayload)
		if normalized == nil {
			continue
		}
		if str(normalized["listing_role"]) == "parent" {
			_ = s.gormStore.DB().WithContext(ctx).Model(&model.ImportRecord{}).
				Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: rec.ID}).
				Updates(map[string]any{"status": "success"}).Error
			continue
		}
		asin := strings.ToUpper(strings.TrimSpace(str(normalized["asin"])))
		if asin == "" {
			continue
		}

		scope := str(normalized["sales_scope"])
		if normalized["sales_scope_explicit"] != true && meta.DefaultSalesScope != "" && meta.DefaultSalesScope != "unknown" {
			scope = meta.DefaultSalesScope
		}
		parentASIN := strings.ToUpper(strings.TrimSpace(str(normalized["parent_asin"])))
		price := parseFloatAny(normalized["price_usd"])
		monthlySales := int(integer(normalized["monthly_sales"]))
		var childSalesPtr *int
		if scope == "child" && normalized["monthly_sales"] != nil {
			v := monthlySales
			childSalesPtr = &v
		} else if normalized["display_child_sales"] != nil {
			v := int(integer(normalized["display_child_sales"]))
			childSalesPtr = &v
		}
		var bsrPtr *int
		if normalized["bsr_rank"] != nil {
			v := int(integer(normalized["bsr_rank"]))
			bsrPtr = &v
		}
		rating := parseFloatAny(normalized["rating"])
		reviewCount := int(integer(normalized["review_count"]))

		var itemID uint64
		err := s.gormStore.Transaction(ctx, func(txStore *gormrepo.Store) error {
			tx := txStore.DB()
			var existing model.ScreeningItem
			findErr := tx.Unscoped().
				Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
				Where(clause.Eq{Column: clause.Column{Name: "asin"}, Value: asin}).
				First(&existing).Error
			isNew := errors.Is(findErr, gorm.ErrRecordNotFound)
			if findErr != nil && !isNew {
				return findErr
			}

			source := Object{}
			if !isNew && len(existing.SourceData) > 0 {
				source = decodeObject(existing.SourceData)
			}
			prevLatestMonth := latestMonthInSource(source)

			incomingMonthly := []Object{}
			metricsToUpsert := []model.ScreeningItemMonthlyMetric{}

			primaryHist := Object{
				"id":                  fmt.Sprintf("imp-%d-row-%d", taskID, rec.ID),
				"import_id":           importIDStr,
				"stat_month":          statMonthFull,
				"period_type":         meta.PeriodType,
				"period_start":        meta.PeriodStart,
				"period_end":          meta.PeriodEnd,
				"observed_on":         meta.ObservedOn,
				"source_type":         meta.SourceType,
				"sales_scope":         scope,
				"monthly_sales":       monthlySales,
				"display_child_sales": childSalesPtr,
				"review_count":        reviewCount,
				"bsr_rank":            bsrPtr,
				"revision":            1,
			}
			if normalized["price_usd"] != nil {
				primaryHist["price_usd"] = fmt.Sprintf("%.2f", price)
			}
			if normalized["monthly_revenue"] != nil {
				primaryHist["monthly_revenue"] = str(normalized["monthly_revenue"])
			}
			if normalized["rating"] != nil {
				primaryHist["rating"] = fmt.Sprintf("%.2f", rating)
			}
			incomingMonthly = append(incomingMonthly, primaryHist)

			if rawHist, ok := normalized["imported_monthly"].([]any); ok {
				for _, entry := range rawHist {
					mObj, ok := entry.(map[string]any)
					if !ok {
						continue
					}
					ym := normalizeQueryMonth(str(mObj["stat_month"]))
					if ym == "" {
						continue
					}
					mPrice := parseFloatAny(mObj["price_usd"])
					mSales := int(integer(mObj["monthly_sales"]))
					var mChild *int
					if mObj["sales_scope"] == "child" && mObj["monthly_sales"] != nil {
						v := mSales
						mChild = &v
					}
					var mBSR *int
					if mObj["bsr_rank"] != nil {
						v := int(integer(mObj["bsr_rank"]))
						mBSR = &v
					}
					hRow := Object{
						"id":                  fmt.Sprintf("imp-%d-row-%d-%s", taskID, rec.ID, ym),
						"import_id":           importIDStr,
						"stat_month":          ym + "-01",
						"period_type":         "calendar_month",
						"source_type":         meta.SourceType,
						"sales_scope":         str(mObj["sales_scope"]),
						"monthly_sales":       mSales,
						"display_child_sales": mChild,
						"bsr_rank":            mBSR,
						"revision":            1,
					}
					if mObj["price_usd"] != nil {
						hRow["price_usd"] = fmt.Sprintf("%.2f", mPrice)
					}
					incomingMonthly = append(incomingMonthly, hRow)
					metricsToUpsert = append(metricsToUpsert, model.ScreeningItemMonthlyMetric{
						WorkspaceID:  job.WorkspaceID,
						ASIN:         asin,
						ParentASIN:   parentASIN,
						StatMonth:    ym,
						SellingPrice: mPrice,
						Currency:     "USD",
						MonthlySales: mSales,
						ChildSales:   mChild,
						BSRRank:      mBSR,
						SourceTaskID: &taskID,
					})
				}
			}

			mergeSourceMonthlyHistory(source, incomingMonthly)
			isLatestOrNew := isNew || (normalized["restore_monthly_only"] != true && (prevLatestMonth == "" || statMonthYM >= prevLatestMonth))

			if isLatestOrNew {
				for _, k := range []string{
					"title", "title_zh", "image_url", "category", "category_path", "brand", "brand_category",
					"seller_id", "seller_name", "seller_region", "listing_date", "specs", "sales_scope",
					"product_dimensions", "package_dimensions", "weight_g", "package_weight_g",
					"product_length_cm", "product_width_cm", "product_height_cm",
					"package_length_cm", "package_width_cm", "package_height_cm",
					"units_per_pack", "carton_units", "carton_weight_g", "carton_length_cm", "carton_width_cm", "carton_height_cm",
				} {
					if v := normalized[k]; v != nil && str(v) != "" {
						source[k] = v
					}
				}
				source["stat_month"] = statMonthFull
				if normalized["price_usd"] != nil {
					source["price_usd"] = fmt.Sprintf("%.2f", price)
				}
				if normalized["monthly_sales"] != nil {
					source["monthly_sales"] = monthlySales
				}
				if childSalesPtr != nil {
					source["display_child_sales"] = *childSalesPtr
				}
				if normalized["rating"] != nil {
					source["rating"] = fmt.Sprintf("%.2f", rating)
				}
				if normalized["review_count"] != nil {
					source["review_count"] = reviewCount
				}
			}

			if isNew {
				newItem := &model.ScreeningItem{
					WorkspaceID:       job.WorkspaceID,
					ASIN:              asin,
					ParentASIN:        parentASIN,
					IsParent:          parentASIN == "" || parentASIN == asin,
					TitleEn:           str(normalized["title"]),
					TitleZh:           str(normalized["title_zh"]),
					Brand:             str(normalized["brand"]),
					BrandCategory:     str(normalized["brand_category"]),
					CategoryPath:      str(normalized["category_path"]),
					CategoryZh:        str(normalized["category"]),
					ImageURL:          str(normalized["image_url"]),
					ListingPeriod:     str(normalized["listing_date"]),
					LatestPrice:       price,
					Currency:          "USD",
					LatestSales:       monthlySales,
					ChildSales:        childSalesPtr,
					Rating:            rating,
					ReviewCount:       reviewCount,
					ItemDimensions:    str(normalized["product_dimensions"]),
					PackageDimensions: str(normalized["package_dimensions"]),
					NetWeightGrams:    parseFloatAny(normalized["weight_g"]),
					GrossWeightGrams:  parseFloatAny(normalized["package_weight_g"]),
					MachineResult:     "pending",
					ManualStatus:      "pending",
					ProfitCalcStatus:  "pending",
					LastImportTaskID:  &taskID,
					SourceData:        datatypes.JSON(encoded(source)),
				}
				if err := tx.Create(newItem).Error; err != nil {
					return err
				}
				itemID = newItem.ID
			} else {
				itemID = existing.ID
				// 仅更新商品属性与 source_data，严禁触碰 manual_status/manual_note/reviewed_by/reviewed_at/profit_*
				updates := map[string]any{
					"deleted_at":          nil,
					"last_import_task_id": &taskID,
					"source_data":         datatypes.JSON(encoded(source)),
				}
				if isLatestOrNew {
					if parentASIN != "" {
						updates["parent_asin"] = parentASIN
						updates["is_parent"] = (parentASIN == asin)
					}
					if v := str(normalized["title"]); v != "" {
						updates["title_en"] = v
					}
					if v := str(normalized["title_zh"]); v != "" {
						updates["title_zh"] = v
					}
					if v := str(normalized["brand"]); v != "" {
						updates["brand"] = v
					}
					if v := str(normalized["brand_category"]); v != "" {
						updates["brand_category"] = v
					}
					if v := str(normalized["category_path"]); v != "" {
						updates["category_path"] = v
					}
					if v := str(normalized["category"]); v != "" {
						updates["category_zh"] = v
					}
					if v := str(normalized["image_url"]); v != "" {
						updates["image_url"] = v
					}
					if v := str(normalized["listing_date"]); v != "" {
						updates["listing_period"] = v
					}
					if normalized["price_usd"] != nil {
						updates["latest_price"] = price
					}
					if normalized["monthly_sales"] != nil {
						updates["latest_sales"] = monthlySales
					}
					if childSalesPtr != nil {
						updates["child_sales"] = childSalesPtr
					}
					if normalized["rating"] != nil {
						updates["rating"] = rating
					}
					if normalized["review_count"] != nil {
						updates["review_count"] = reviewCount
					}
					if v := str(normalized["product_dimensions"]); v != "" {
						updates["item_dimensions"] = v
					}
					if v := str(normalized["package_dimensions"]); v != "" {
						updates["package_dimensions"] = v
					}
					if normalized["weight_g"] != nil {
						updates["net_weight_grams"] = parseFloatAny(normalized["weight_g"])
					}
					if normalized["package_weight_g"] != nil {
						updates["gross_weight_grams"] = parseFloatAny(normalized["package_weight_g"])
					}
				}
				if err := tx.Unscoped().
					Model(&model.ScreeningItem{}).
					Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
					Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: itemID}).
					Updates(updates).Error; err != nil {
					return err
				}
			}

			primaryMetric := model.ScreeningItemMonthlyMetric{
				WorkspaceID:  job.WorkspaceID,
				ItemID:       itemID,
				ASIN:         asin,
				ParentASIN:   parentASIN,
				StatMonth:    statMonthYM,
				SellingPrice: price,
				Currency:     "USD",
				MonthlySales: monthlySales,
				ChildSales:   childSalesPtr,
				BSRRank:      bsrPtr,
				SourceTaskID: &taskID,
			}
			for i := range metricsToUpsert {
				metricsToUpsert[i].ItemID = itemID
			}
			metricsToUpsert = append(metricsToUpsert, primaryMetric)
			if err := txStore.MonthlyMetrics().BatchUpsert(ctx, job.WorkspaceID, metricsToUpsert); err != nil {
				return err
			}

			return tx.Model(&model.ImportRecord{}).
				Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
				Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: rec.ID}).
				Updates(map[string]any{"status": "success"}).Error
		})
		if err != nil {
			return nil, err
		}
		affectedItemSet[itemID] = true
		committedCount++
	}

	affectedIDs := make([]uint64, 0, len(affectedItemSet))
	affectedStrIDs := make([]string, 0, len(affectedItemSet))
	for id := range affectedItemSet {
		affectedIDs = append(affectedIDs, id)
	}
	sort.Slice(affectedIDs, func(i, j int) bool { return affectedIDs[i] < affectedIDs[j] })
	for _, id := range affectedIDs {
		affectedStrIDs = append(affectedStrIDs, strconv.FormatUint(id, 10))
	}

	if len(affectedIDs) > 0 {
		if err := s.gormStore.MonthlyMetrics().SyncLatestToItems(ctx, job.WorkspaceID, affectedIDs); err != nil {
			return nil, err
		}
		if err := s.gormStore.Screening().RecalculateParentChildCounts(ctx, job.WorkspaceID); err != nil {
			return nil, err
		}
	}

	var failedCount int64
	_ = s.gormStore.DB().WithContext(ctx).
		Model(&model.ImportRecord{}).
		Scopes(gormrepo.WithWorkspace(job.WorkspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: taskID}).
		Where(clause.Eq{Column: clause.Column{Name: "status"}, Value: "failed"}).
		Count(&failedCount).Error

	finalStatus := "completed"
	if failedCount > 0 {
		finalStatus = "partial"
	}
	now := time.Now()
	totalRows := task.TotalRows
	if totalRows < committedCount+int(failedCount) {
		totalRows = committedCount + int(failedCount)
	}
	if err := s.gormStore.Imports().UpdateTaskProgress(ctx, job.WorkspaceID, taskID, finalStatus, totalRows, committedCount, int(failedCount), task.SkippedRows, "", &now); err != nil {
		return nil, err
	}

	result := Object{
		"import_id": importIDStr,
		"products":  len(affectedIDs),
		"committed": committedCount,
		"invalid":   failedCount,
	}
	if len(affectedStrIDs) > 0 {
		if ws, err := s.gormStore.Workspaces().GetByID(ctx, job.WorkspaceID); err == nil && ws != nil {
			tenantID := parseTenantFromWorkspaceCode(ws.Code)
			if tenantID > 0 {
				actorIdentity := identity{
					Tenant:     tenantID,
					User:       parseUserIDFromActor(job.CreatedBy),
					Role:       "member",
					RequestKey: "auto-screen-import:" + importIDStr,
				}
				if sub, err := s.gormEnqueueScreen(ctx, actorIdentity, EnqueueScreenCommand{
					Selection: ProductSelectionCommand{ProductIDs: affectedStrIDs, HasProductIDs: true},
				}, ""); err == nil {
					result["screen"] = sub
				}
			}
		}
	}
	return result, nil
}

func parseTenantFromWorkspaceCode(code string) int64 {
	if strings.HasPrefix(code, "ws-tenant-") {
		v, _ := strconv.ParseInt(strings.TrimPrefix(code, "ws-tenant-"), 10, 64)
		return v
	}
	return 0
}

func parseUserIDFromActor(createdBy string) int64 {
	if strings.HasPrefix(createdBy, "user-") {
		v, _ := strconv.ParseInt(strings.TrimPrefix(createdBy, "user-"), 10, 64)
		return v
	}
	return 0
}

// gormImportDownload 处理 MySQL GORM 模式下的导入源文件与错误明细 CSV 下载。
func (s *service) gormImportDownload(w http.ResponseWriter, r *http.Request, id, kind string) (any, error) {
	a := actor(r)
	wsID, err := s.resolveWorkspaceID(r.Context(), a)
	if err != nil {
		return nil, err
	}
	taskID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	task, err := s.gormStore.Imports().GetTaskByID(r.Context(), wsID, taskID)
	if err != nil {
		return nil, err
	}
	if kind == "source" {
		_, payload, err := s.gormLatestParseJob(r.Context(), wsID, id)
		if err != nil {
			return nil, notFound("源文件不存在")
		}
		storageKey := str(payload["storage_key"])
		if storageKey == "" {
			return nil, notFound("源文件不存在")
		}
		return nil, s.serveStored(w, r, storageKey, task.FileName)
	}
	if kind != "errors" {
		return nil, notFound("下载不存在")
	}
	var failedRows []model.ImportRecord
	if err := s.gormStore.DB().WithContext(r.Context()).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: taskID}).
		Where(clause.Eq{Column: clause.Column{Name: "status"}, Value: "failed"}).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "row_no"}}}}).
		Find(&failedRows).Error; err != nil {
		return nil, err
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", "attachment; filename=import-errors.csv")
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"文件", "工作表", "原行号", "错误", "原始数据"})
	for _, row := range failedRows {
		rawData, _, memberPath, sheetName, origRowNo, errs := unpackImportRecordPayload(row.RawPayload)
		rowNum := origRowNo
		if rowNum == 0 {
			rowNum = row.RowNo
		}
		errCol := row.ErrorMessage
		if len(errs) > 0 {
			errCol = string(encoded(errs))
		}
		_ = writer.Write([]string{memberPath, sheetName, strconv.Itoa(rowNum), errCol, string(encoded(rawData))})
	}
	writer.Flush()
	return nil, nil
}

// gormGetProductObservationSourceUsecase 返回 MySQL GORM 模式下指定商品观测行的原始来源数据。
func (s *service) gormGetProductObservationSourceUsecase(ctx context.Context, a identity, id, rowID string) (any, error) {
	wsID, err := s.resolveWorkspaceID(ctx, a)
	if err != nil {
		return nil, err
	}
	itemID, err := parseUintID(id)
	if err != nil {
		return nil, err
	}
	recID, err := parseUintID(rowID)
	if err != nil {
		return nil, err
	}
	item, err := s.gormStore.Screening().GetByID(ctx, wsID, itemID)
	if err != nil {
		return nil, err
	}
	var rec model.ImportRecord
	if err := s.gormStore.DB().WithContext(ctx).
		Scopes(gormrepo.WithWorkspace(wsID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: recID}).
		Where(clause.Eq{Column: clause.Column{Name: "asin"}, Value: item.ASIN}).
		First(&rec).Error; err != nil {
		return nil, err
	}
	fileName := ""
	if task, err := s.gormStore.Imports().GetTaskByID(ctx, wsID, rec.TaskID); err == nil && task != nil {
		fileName = task.FileName
	}
	rawData, _, memberPath, sheetName, origRowNo, _ := unpackImportRecordPayload(rec.RawPayload)
	if origRowNo == 0 {
		origRowNo = rec.RowNo
	}
	return Object{
		"id":          strconv.FormatUint(rec.ID, 10),
		"import_id":   strconv.FormatUint(rec.TaskID, 10),
		"file_name":   fileName,
		"member_path": memberPath,
		"sheet_name":  sheetName,
		"row_no":      origRowNo,
		"raw_data":    rawData,
	}, nil
}

// enrichGORMImportObject 将最新 parse/import 任务中的周期、映射、表头与 latest_job_id 补齐到导入详情对象中。
func (s *service) enrichGORMImportObject(ctx context.Context, wsID uint64, id string, obj Object) {
	var latestJobID uint64
	if pJob, pPayload, err := s.gormLatestParseJob(ctx, wsID, id); err == nil && pJob != nil {
		latestJobID = pJob.ID
		for _, k := range []string{"source_period", "observed_on", "source_type", "period_type", "period_start", "period_end", "default_sales_scope"} {
			if v, ok := pPayload[k]; ok && v != nil && str(v) != "" {
				obj[k] = v
			}
		}
		if m, ok := pPayload["mapping"].(map[string]any); ok {
			obj["mapping"] = m
		}
		pResult := decodeObject(pJob.Result)
		if counts, ok := obj["counts"].(Object); ok {
			if v, ok := pResult["valid"]; ok {
				counts["valid"] = v
			}
			if v, ok := pResult["headers"]; ok {
				counts["headers"] = v
			}
			if v, ok := pResult["skipped_sheets"]; ok {
				counts["skipped_sheets"] = v
			}
		}
	}
	if iJob, err := s.gormLatestImportJob(ctx, wsID, id); err == nil && iJob != nil && iJob.ID > latestJobID {
		latestJobID = iJob.ID
	}
	if latestJobID > 0 {
		obj["latest_job_id"] = strconv.FormatUint(latestJobID, 10)
	}
}

// removeImportFromSourceMonthlyHistory 从商品的 SourceData["source_monthly_history"] 中仅移除指定 import_id 的条目，并将顶层最新价/量回退到剩余最新月份。
func removeImportFromSourceMonthlyHistory(source Object, importID string) bool {
	existing, ok := source["source_monthly_history"].([]any)
	if !ok || len(existing) == 0 {
		return false
	}
	kept := make([]any, 0, len(existing))
	changed := false
	for _, v := range existing {
		row, ok := v.(map[string]any)
		if ok && str(row["import_id"]) == importID {
			changed = true
			continue
		}
		kept = append(kept, v)
	}
	if changed {
		source["source_monthly_history"] = kept
		if len(kept) > 0 {
			if latest, ok := kept[0].(map[string]any); ok {
				for _, k := range []string{"stat_month", "price_usd", "monthly_sales", "display_child_sales", "rating", "review_count"} {
					if v, exists := latest[k]; exists {
						source[k] = v
					}
				}
			}
		}
	}
	return changed
}

// Ensure json import is always referenced.
var _ = json.Marshal
