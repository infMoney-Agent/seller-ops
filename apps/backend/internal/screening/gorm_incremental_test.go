package screening

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/db"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
)

func TestGORMIncrementalImportLossless(t *testing.T) {
	cfg, err := db.LoadConfigWithOptions(db.EnvTest, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	gdb, err := db.OpenMySQL(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.CloseMySQL(gdb) })
	if _, err = db.Migrate(ctx, gdb, db.MigrateOptions{AppEnv: db.EnvTest, Mode: db.MigrateModeApply}); err != nil {
		t.Fatal(err)
	}

	store := gormrepo.NewStore(gdb)
	tenant := time.Now().UnixNano()%1000000000 + 2000000
	ws := &model.Workspace{Code: fmt.Sprintf("ws-tenant-%d", tenant), Name: "incremental lossless test", CreatedBy: "test"}
	if err := store.Workspaces().UpsertWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, m := range []any{
			&model.ScreeningReviewLog{},
			&model.ProfitCalculation{},
			&model.ScreeningItemMonthlyMetric{},
			&model.ScreeningItem{},
			&model.ImportRecord{},
			&model.ImportTask{},
			&model.ExportTask{},
			&model.ScreeningConfig{},
			&model.ScreeningJob{},
		} {
			_ = gdb.Unscoped().Scopes(gormrepo.WithWorkspace(ws.ID)).Delete(m).Error
		}
		_ = gdb.Unscoped().Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: ws.ID}).Delete(&model.Workspace{}).Error
	})

	// 1. 模拟已迁移商品：包含人工复核结论、利润测算结果与历史 SourceData 快照
	margin := 0.3125
	estProfit := 12.50
	reviewedAt := time.Now().Add(-24 * time.Hour).UTC()
	migratedSource, _ := json.Marshal(Object{
		"seller_name":   "原迁移卖家",
		"listing_date":  "2024-01-15",
		"category":      "旧类目",
		"category_path": "Home:Old",
		"price_usd":     "19.99",
		"monthly_sales": 100,
		"stat_month":    "2026-09-01",
		"source_monthly_history": []Object{
			{
				"id":            "migrated-2026-09",
				"stat_month":    "2026-09-01",
				"period_type":   "calendar_month",
				"source_type":   "sellersprite",
				"price_usd":     "19.99",
				"monthly_sales": 100,
			},
		},
	})
	migratedItem := model.ScreeningItem{
		WorkspaceID:      ws.ID,
		ASIN:             "B0MIGR0001",
		ParentASIN:       "B0MIGR0001",
		IsParent:         true,
		TitleEn:          "Migrated Tumbler",
		CategoryZh:       "旧类目",
		LatestPrice:      19.99,
		LatestSales:      100,
		MachineResult:    "passed",
		ManualStatus:     "approved",
		ManualNote:       "已人工确认保留",
		ReviewedBy:       "user-99",
		ReviewedAt:       &reviewedAt,
		ProfitCalcStatus: "calculated",
		ProfitMargin:     &margin,
		EstimatedProfit:  &estProfit,
		SourceData:       datatypes.JSON(migratedSource),
	}
	if err := gdb.Create(&migratedItem).Error; err != nil {
		t.Fatalf("seed migrated item: %v", err)
	}
	if err := store.MonthlyMetrics().BatchUpsert(ctx, ws.ID, []model.ScreeningItemMonthlyMetric{
		{
			WorkspaceID:  ws.ID,
			ItemID:       migratedItem.ID,
			ASIN:         "B0MIGR0001",
			ParentASIN:   "B0MIGR0001",
			StatMonth:    "2026-09",
			SellingPrice: 19.99,
			MonthlySales: 100,
		},
	}); err != nil {
		t.Fatalf("seed migrated metric: %v", err)
	}

	svc := &service{
		gormStore: store,
		cfg:       Config{Storage: t.TempDir()},
	}
	actorID := identity{Tenant: tenant, User: 7, Role: "owner"}

	uploadCSV := func(filename, statPeriod, csvContent string) *JobSubmission {
		t.Helper()
		var body bytes.Buffer
		mw := multipart.NewWriter(&body)
		fw, err := mw.CreateFormFile("file", filename)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(csvContent)); err != nil {
			t.Fatal(err)
		}
		_ = mw.WriteField("source_period", statPeriod)
		_ = mw.WriteField("period_type", "calendar_month")
		_ = mw.WriteField("source_type", "sellersprite")
		_ = mw.WriteField("default_sales_scope", "child")
		if err := mw.Close(); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/screening/imports", &body)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req = req.WithContext(context.WithValue(ctx, identityKey{}, actorID))
		rec := httptest.NewRecorder()
		res, err := svc.upload(rec, req)
		if err != nil {
			t.Fatalf("upload failed: %v", err)
		}
		return res.(*JobSubmission)
	}

	runJobToCompletion := func(jobIDStr string) {
		t.Helper()
		jid, err := parseUintID(jobIDStr)
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 20; i++ {
			var j model.ScreeningJob
			if err := gdb.Scopes(gormrepo.WithWorkspace(ws.ID)).Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: jid}).First(&j).Error; err != nil {
				t.Fatalf("load job %s: %v", jobIDStr, err)
			}
			if j.State == "succeeded" {
				return
			}
			if j.State == "failed" {
				t.Fatalf("job %s (%s) failed: %s", jobIDStr, j.Kind, j.ErrorMessage)
			}
			processed, _ := svc.gormProcessNextJob(ctx)
			if !processed {
				t.Fatalf("no queued job found while waiting for job %s (state=%s)", jobIDStr, j.State)
			}
		}
		t.Fatalf("job %s did not complete within 20 iterations", jobIDStr)
	}

	// 2. 上传增量 CSV（含已有 ASIN 更新 + 新增 ASIN + 1 条非法行）
	csv1 := strings.Join([]string{
		"ASIN,商品标题,价格($),月销量,评分数,评分,品牌",
		"B0MIGR0001,Migrated Tumbler Updated,29.99,350,120,4.7,BrewCo",
		"B0NEW00001,Brand New Mug,24.50,210,45,4.6,NewBrand",
		",Missing ASIN Row,10.00,5,1,4.0,NoBrand",
	}, "\n")
	sub1 := uploadCSV("inc-2026-11.csv", "2026-11", csv1)
	if sub1.ImportID == "" || sub1.JobID == "" {
		t.Fatalf("unexpected upload submission: %+v", sub1)
	}

	// 执行 parse 任务
	runJobToCompletion(sub1.JobID)
	impObj, err := svc.gormGetImport(ctx, actorID, sub1.ImportID)
	if err != nil {
		t.Fatalf("get import after parse: %v", err)
	}
	if str(impObj["state"]) != "preview" {
		t.Fatalf("expected preview state after parse, got %+v", impObj)
	}
	if str(impObj["source_period"]) != "2026-11" || str(impObj["latest_job_id"]) == "" {
		t.Fatalf("expected enriched import preview metadata, got %+v", impObj)
	}
	if counts, ok := impObj["counts"].(Object); !ok || integer(counts["valid"]) != 2 {
		t.Fatalf("expected counts.valid=2 in import preview, got %+v", impObj["counts"])
	}

	// 提交入库并执行 import 任务
	commitSub, err := svc.gormEditImport(ctx, actorID, sub1.ImportID, Object{}, true)
	if err != nil {
		t.Fatalf("commit import: %v", err)
	}
	if commitSub.State != "queued" {
		t.Fatalf("expected queued import job, got %+v", commitSub)
	}
	runJobToCompletion(commitSub.JobID)

	// 3. 验证已迁移商品无损保留人工结论、利润结论与旧 SourceData，同时合并了新月份
	updatedMigrated, err := store.Screening().GetByASIN(ctx, ws.ID, "B0MIGR0001")
	if err != nil {
		t.Fatalf("reload migrated item: %v", err)
	}
	if updatedMigrated.ManualStatus != "approved" || updatedMigrated.ManualNote != "已人工确认保留" || updatedMigrated.ReviewedBy != "user-99" {
		t.Fatalf("manual review conclusion was overwritten: %+v", updatedMigrated)
	}
	if updatedMigrated.ProfitCalcStatus != "calculated" || updatedMigrated.ProfitMargin == nil || *updatedMigrated.ProfitMargin != 0.3125 || updatedMigrated.EstimatedProfit == nil || *updatedMigrated.EstimatedProfit != 12.50 {
		t.Fatalf("profit calculation snapshot was overwritten: %+v", updatedMigrated)
	}
	if updatedMigrated.LatestPrice != 29.99 || updatedMigrated.LatestSales != 350 {
		t.Fatalf("expected updated latest price/sales (29.99/350), got %.2f/%d", updatedMigrated.LatestPrice, updatedMigrated.LatestSales)
	}

	migratedIDStr := strconv.FormatUint(updatedMigrated.ID, 10)
	prodViewAny, err := svc.gormGetProductUsecase(ctx, actorID, migratedIDStr)
	if err != nil {
		t.Fatalf("get product view: %v", err)
	}
	prodView := prodViewAny.(Object)
	if str(prodView["seller_name"]) != "原迁移卖家" || str(prodView["listing_date"]) != "2024-01-15" {
		t.Fatalf("migrated SourceData fields lost: %+v", prodView)
	}
	if str(prodView["review_status"]) != "kept" {
		t.Fatalf("expected review_status=kept, got %+v", prodView["review_status"])
	}

	// 验证月度历史同时包含迁移的 2026-09 和新增的 2026-11
	monthlyAny, err := svc.gormGetProductMonthlyUsecase(ctx, actorID, migratedIDStr, url.Values{})
	if err != nil {
		t.Fatalf("get product monthly: %v", err)
	}
	monthlyItems := monthlyAny.(Object)["items"].([]Object)
	if len(monthlyItems) != 2 {
		t.Fatalf("expected 2 merged monthly records (2026-11 & 2026-09), got %d: %+v", len(monthlyItems), monthlyItems)
	}
	if normalizeQueryMonth(str(monthlyItems[0]["stat_month"])) != "2026-11" || normalizeQueryMonth(str(monthlyItems[1]["stat_month"])) != "2026-09" {
		t.Fatalf("unexpected monthly order/content: %+v", monthlyItems)
	}

	// 4. 验证新增商品 + 观测溯源 + 源文件/错误 CSV 下载
	newItem, err := store.Screening().GetByASIN(ctx, ws.ID, "B0NEW00001")
	if err != nil {
		t.Fatalf("new item not created: %v", err)
	}
	newIDStr := strconv.FormatUint(newItem.ID, 10)
	obsListAny, err := svc.gormListProductObservationsUsecase(ctx, actorID, newIDStr, url.Values{})
	if err != nil {
		t.Fatalf("list observations: %v", err)
	}
	obsItems := obsListAny.(Object)["items"].([]Object)
	if len(obsItems) != 1 {
		t.Fatalf("expected 1 observation for new item, got %d", len(obsItems))
	}
	obsSrcAny, err := svc.getProductObservationSourceUsecase(ctx, actorID, newIDStr, str(obsItems[0]["id"]))
	if err != nil {
		t.Fatalf("get observation source: %v", err)
	}
	obsSrc := obsSrcAny.(Object)
	if str(obsSrc["file_name"]) != "inc-2026-11.csv" {
		t.Fatalf("unexpected observation source: %+v", obsSrc)
	}

	// 通过 route() 验证数字 ID 下载源文件与错误明细
	dlSrcReq := httptest.NewRequest(http.MethodGet, "/api/v1/screening/imports/"+sub1.ImportID+"/source", nil)
	dlSrcReq = dlSrcReq.WithContext(context.WithValue(ctx, identityKey{}, actorID))
	dlSrcRec := httptest.NewRecorder()
	if _, err := svc.route(dlSrcRec, dlSrcReq); err != nil {
		t.Fatalf("route download source failed: %v", err)
	}
	if !strings.Contains(dlSrcRec.Body.String(), "B0NEW00001") {
		t.Fatalf("downloaded source missing content: %q", dlSrcRec.Body.String())
	}

	dlErrReq := httptest.NewRequest(http.MethodGet, "/api/v1/screening/imports/"+sub1.ImportID+"/errors", nil)
	dlErrReq = dlErrReq.WithContext(context.WithValue(ctx, identityKey{}, actorID))
	dlErrRec := httptest.NewRecorder()
	if _, err := svc.route(dlErrRec, dlErrReq); err != nil {
		t.Fatalf("route download errors failed: %v", err)
	}
	if !strings.Contains(dlErrRec.Body.String(), "Missing ASIN Row") {
		t.Fatalf("downloaded errors missing invalid row: %q", dlErrRec.Body.String())
	}

	// 5. 验证修改类目与参数不会被旧 SourceData 遮盖，且不丢失原有字段
	catView, err := svc.gormUpdateProductCategory(ctx, actorID, migratedIDStr, UpdateCategoryCommand{Category: "新户外水杯"})
	if err != nil {
		t.Fatalf("update category: %v", err)
	}
	if str(catView["category"]) != "新户外水杯" || str(catView["seller_name"]) != "原迁移卖家" {
		t.Fatalf("category update shadowed or lost SourceData: %+v", catView)
	}

	paramView, err := svc.gormUpdateProductParameters(ctx, actorID, migratedIDStr, UpdateParametersCommand{
		Patches: map[string]ParameterPatch{
			"weight_g":       {Present: true, Raw: "250"},
			"units_per_pack": {Present: true, Raw: "2"},
		},
	})
	if err != nil {
		t.Fatalf("update parameters: %v", err)
	}
	if parseFloatAny(paramView["weight_g"]) != 250 || str(paramView["units_per_pack"]) != "2" || str(paramView["seller_name"]) != "原迁移卖家" {
		t.Fatalf("parameter update shadowed or lost SourceData: %+v", paramView)
	}

	// 6. 验证删除导入历史回滚最新月度快照，且重新导入同月后能从软删除恢复可见
	if _, err := svc.gormRemoveImportHistory(ctx, actorID, sub1.ImportID); err != nil {
		t.Fatalf("remove import history: %v", err)
	}
	afterRemove, err := store.Screening().GetByASIN(ctx, ws.ID, "B0MIGR0001")
	if err != nil {
		t.Fatalf("reload after remove: %v", err)
	}
	if afterRemove.LatestPrice != 19.99 || afterRemove.LatestSales != 100 {
		t.Fatalf("expected rollback to 2026-09 (19.99/100), got %.2f/%d", afterRemove.LatestPrice, afterRemove.LatestSales)
	}
	afterRemoveViewAny, err := svc.gormGetProductUsecase(ctx, actorID, migratedIDStr)
	if err != nil {
		t.Fatalf("get product view after remove: %v", err)
	}
	afterRemoveView := afterRemoveViewAny.(Object)
	if str(afterRemoveView["price_usd"]) != "19.99" || integer(afterRemoveView["monthly_sales"]) != 100 {
		t.Fatalf("SourceData not rolled back after remove: price=%v sales=%v", afterRemoveView["price_usd"], afterRemoveView["monthly_sales"])
	}

	// 重导同月 2026-11
	csv2 := strings.Join([]string{
		"ASIN,商品标题,价格($),月销量",
		"B0MIGR0001,Migrated Tumbler Reimported,31.50,400",
	}, "\n")
	sub2 := uploadCSV("reimport-2026-11.csv", "2026-11", csv2)
	runJobToCompletion(sub2.JobID)
	commitSub2, err := svc.gormEditImport(ctx, actorID, sub2.ImportID, Object{}, true)
	if err != nil {
		t.Fatalf("reimport commit: %v", err)
	}
	runJobToCompletion(commitSub2.JobID)

	reimported, err := store.Screening().GetByASIN(ctx, ws.ID, "B0MIGR0001")
	if err != nil {
		t.Fatalf("reload after reimport: %v", err)
	}
	if reimported.LatestPrice != 31.50 || reimported.LatestSales != 400 {
		t.Fatalf("reimported month stayed soft-deleted or did not sync: %.2f/%d", reimported.LatestPrice, reimported.LatestSales)
	}
	if reimported.ManualStatus != "approved" || reimported.ProfitCalcStatus != "calculated" {
		t.Fatalf("reimport overwrote manual/profit status: %+v", reimported)
	}

	// 7. 验证直接调用 BatchUpsertItems 也不会覆盖已有人工结论和利润结论
	if err := store.Screening().BatchUpsertItems(ctx, ws.ID, []model.ScreeningItem{
		{
			ASIN:             "B0MIGR0001",
			TitleEn:          "Direct Upsert Title",
			ManualStatus:     "pending",
			ProfitCalcStatus: "pending",
		},
	}); err != nil {
		t.Fatalf("direct BatchUpsertItems: %v", err)
	}
	afterDirect, err := store.Screening().GetByASIN(ctx, ws.ID, "B0MIGR0001")
	if err != nil {
		t.Fatalf("reload after direct upsert: %v", err)
	}
	if afterDirect.ManualStatus != "approved" || afterDirect.ManualNote != "已人工确认保留" || afterDirect.ProfitCalcStatus != "calculated" {
		t.Fatalf("direct BatchUpsertItems overwrote manual/profit fields: %+v", afterDirect)
	}
}
