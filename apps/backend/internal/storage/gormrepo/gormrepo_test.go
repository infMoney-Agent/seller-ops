package gormrepo_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"seller-ops/backend/internal/db"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
)

func openTestStore(t *testing.T) *gormrepo.Store {
	t.Helper()
	cfg, err := db.LoadConfigWithOptions(db.EnvTest, "")
	if err != nil {
		t.Skipf("skipping MySQL GORM integration test (no test config): %v", err)
	}
	ctx := context.Background()
	gdb, err := db.OpenMySQL(ctx, cfg)
	if err != nil {
		t.Skipf("skipping MySQL GORM integration test (MySQL unavailable): %v", err)
	}
	t.Cleanup(func() {
		_ = db.CloseMySQL(gdb)
	})

	if _, err := db.Migrate(ctx, gdb, db.MigrateOptions{
		AppEnv: db.EnvTest,
		Mode:   db.MigrateModeApply,
	}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}
	return gormrepo.NewStore(gdb)
}

func cleanupTestWorkspace(t *testing.T, gdb *gorm.DB, wsID uint64) {
	t.Helper()
	t.Cleanup(func() {
		for _, m := range []any{
			&model.ProfitCalculation{},
			&model.ScreeningReviewLog{},
			&model.ScreeningItemMonthlyMetric{},
			&model.ScreeningItem{},
			&model.ExportTask{},
			&model.ImportRecord{},
			&model.ImportTask{},
			&model.WorkspaceMember{},
			&model.ScreeningConfig{},
			&model.ScreeningJob{},
		} {
			_ = gdb.Unscoped().Scopes(gormrepo.WithWorkspace(wsID)).Delete(m).Error
		}
		_ = gdb.Unscoped().Where("id = ?", wsID).Delete(&model.Workspace{}).Error
	})
}

func intPtr(v int) *int           { return &v }
func floatPtr(v float64) *float64 { return &v }

func TestWorkspaceIsolationAndRepos(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	ws1 := &model.Workspace{Code: "ws-alpha-" + suffix, Name: "Alpha Team", CreatedBy: "u-1"}
	ws2 := &model.Workspace{Code: "ws-beta-" + suffix, Name: "Beta Team", CreatedBy: "u-2"}
	if err := store.Workspaces().UpsertWorkspace(ctx, ws1); err != nil {
		t.Fatalf("upsert ws1: %v", err)
	}
	cleanupTestWorkspace(t, store.DB(), ws1.ID)
	if err := store.Workspaces().UpsertWorkspace(ctx, ws2); err != nil {
		t.Fatalf("upsert ws2: %v", err)
	}
	cleanupTestWorkspace(t, store.DB(), ws2.ID)

	// 幂等重复写入
	ws1Dup := &model.Workspace{Code: ws1.Code, Name: "Alpha Team Updated", CreatedBy: "u-1"}
	if err := store.Workspaces().UpsertWorkspace(ctx, ws1Dup); err != nil {
		t.Fatalf("upsert ws1 dup: %v", err)
	}
	if ws1Dup.ID != ws1.ID {
		t.Fatalf("expected idempotent workspace ID %d, got %d", ws1.ID, ws1Dup.ID)
	}

	if err := store.Workspaces().UpsertMember(ctx, &model.WorkspaceMember{
		WorkspaceID: ws1.ID, UserID: "u-1", Role: "owner",
	}); err != nil {
		t.Fatalf("upsert member: %v", err)
	}

	// 导入任务与原始行
	task := &model.ImportTask{
		WorkspaceID: ws1.ID,
		TaskNo:      "IMP-TEST-" + suffix,
		FileName:    "test.xlsx",
		SourceType:  "excel",
		Status:      "completed",
		TotalRows:   2,
		SuccessRows: 2,
		OperatorID:  "u-1",
	}
	if err := store.Imports().UpsertTask(ctx, task); err != nil {
		t.Fatalf("upsert import task: %v", err)
	}
	if err := store.Imports().BatchUpsertRecords(ctx, ws1.ID, []model.ImportRecord{
		{TaskID: task.ID, RowNo: 1, ASIN: "B000000001", RawPayload: datatypes.JSON(`{"asin":"B000000001"}`), Status: "success"},
		{TaskID: task.ID, RowNo: 2, ASIN: "B000000002", RawPayload: datatypes.JSON(`{"asin":"B000000002"}`), Status: "success"},
	}); err != nil {
		t.Fatalf("batch upsert import records: %v", err)
	}

	// 写入 ws1 与 ws2 商品，验证工作空间强隔离
	if err := store.Screening().BatchUpsertItems(ctx, ws1.ID, []model.ScreeningItem{
		{
			ASIN: "B000000001", ParentASIN: "B000000001", IsParent: true,
			TitleZh: "便携咖啡机", TitleEn: "Portable Espresso Maker", Brand: "BrewCo", BrandCategory: "已注册",
			MachineResult: "passed", ManualStatus: "pending", LatestPrice: 49.99, LatestSales: 500,
			LastImportTaskID: &task.ID,
		},
		{
			ASIN: "B000000002", ParentASIN: "B000000001", IsParent: false,
			TitleZh: "便携咖啡机 黑色", TitleEn: "Portable Espresso Maker Black", Brand: "BrewCo", BrandCategory: "已注册",
			MachineResult: "passed", ManualStatus: "pending", LatestPrice: 52.00, LatestSales: 200,
			LastImportTaskID: &task.ID,
		},
	}); err != nil {
		t.Fatalf("batch upsert ws1 items: %v", err)
	}
	if err := store.Screening().BatchUpsertItems(ctx, ws2.ID, []model.ScreeningItem{
		{
			ASIN: "B000000001", ParentASIN: "B000000001", IsParent: true,
			TitleZh: "Beta空间隔离商品", Brand: "OtherBrand",
			MachineResult: "rejected", ManualStatus: "rejected", LatestPrice: 19.99, LatestSales: 50,
		},
	}); err != nil {
		t.Fatalf("batch upsert ws2 items: %v", err)
	}

	// 验证父体 ChildCount 回填
	if err := store.Screening().RecalculateParentChildCounts(ctx, ws1.ID); err != nil {
		t.Fatalf("recalculate parent child counts: %v", err)
	}
	parentItem, err := store.Screening().GetByASIN(ctx, ws1.ID, "B000000001")
	if err != nil {
		t.Fatalf("get parent item: %v", err)
	}
	if parentItem.ChildCount != 1 {
		t.Fatalf("expected ChildCount=1, got %d", parentItem.ChildCount)
	}

	// 验证跨工作空间隔离：ws2 看不到 ws1 的商品 ID
	if _, err := store.Screening().GetByID(ctx, ws2.ID, parentItem.ID); err == nil {
		t.Fatalf("expected cross-workspace GetByID to fail, but succeeded")
	}

	// 写入多月份售价销量并验证 SyncLatestToItems 与按月筛选
	if err := store.MonthlyMetrics().BatchUpsert(ctx, ws1.ID, []model.ScreeningItemMonthlyMetric{
		{ItemID: parentItem.ID, ASIN: parentItem.ASIN, ParentASIN: parentItem.ParentASIN, StatMonth: "2026-09", SellingPrice: 45.00, MonthlySales: 280, ChildSales: intPtr(120)},
		{ItemID: parentItem.ID, ASIN: parentItem.ASIN, ParentASIN: parentItem.ParentASIN, StatMonth: "2026-10", SellingPrice: 59.90, MonthlySales: 620, ChildSales: intPtr(210)},
	}); err != nil {
		t.Fatalf("batch upsert monthly metrics: %v", err)
	}
	// 再次幂等写入 2026-10，更新售价
	if err := store.MonthlyMetrics().BatchUpsert(ctx, ws1.ID, []model.ScreeningItemMonthlyMetric{
		{ItemID: parentItem.ID, ASIN: parentItem.ASIN, ParentASIN: parentItem.ParentASIN, StatMonth: "2026-10", SellingPrice: 61.50, MonthlySales: 650, ChildSales: intPtr(220)},
	}); err != nil {
		t.Fatalf("idempotent upsert monthly metric: %v", err)
	}
	if err := store.MonthlyMetrics().SyncLatestToItems(ctx, ws1.ID, []uint64{parentItem.ID}); err != nil {
		t.Fatalf("sync latest to items: %v", err)
	}
	reloaded, err := store.Screening().GetByID(ctx, ws1.ID, parentItem.ID)
	if err != nil {
		t.Fatalf("reload parent item: %v", err)
	}
	if reloaded.LatestPrice != 61.50 || reloaded.LatestSales != 650 {
		t.Fatalf("expected latest price=61.50 sales=650, got price=%.2f sales=%d", reloaded.LatestPrice, reloaded.LatestSales)
	}

	// 按指定月份 2026-10 且 monthly_sales >= 300 筛选
	filtered, err := store.Screening().ListWithDetails(ctx, ws1.ID, gormrepo.ScreeningFilter{
		StatMonth:       "2026-10",
		MinMonthlySales: intPtr(300),
		ParentOnly:      true,
		BrandCategory:   "已注册",
		MachineResult:   "passed",
		Page:            1,
		PageSize:        20,
	})
	if err != nil {
		t.Fatalf("list with monthly filter: %v", err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 {
		t.Fatalf("expected 1 filtered item, got total=%d len=%d", filtered.Total, len(filtered.Items))
	}
	if len(filtered.Items[0].MonthlyMetrics) != 2 || filtered.Items[0].MonthlyMetrics[0].StatMonth != "2026-10" {
		t.Fatalf("unexpected monthly metrics order/count: %+v", filtered.Items[0].MonthlyMetrics)
	}

	// 验证批量人工复核事务 + 审计流水
	updated, err := store.Screening().BatchManualReview(ctx, ws1.ID, gormrepo.BatchManualReviewRequest{
		ItemIDs:    []uint64{parentItem.ID},
		NextStatus: "approved",
		Note:       "核心爆款保留",
		OperatorID: "u-1",
		BatchID:    "BATCH-001",
		ActionType: "batch",
		ReviewedAt: time.Now(),
	})
	if err != nil || updated != 1 {
		t.Fatalf("batch manual review: updated=%d err=%v", updated, err)
	}
	logs, logTotal, err := store.Screening().ListReviewLogs(ctx, ws1.ID, parentItem.ID, 1, 10)
	if err != nil || logTotal != 1 || logs[0].PrevStatus != "pending" || logs[0].NextStatus != "approved" {
		t.Fatalf("unexpected review logs: total=%d logs=%+v err=%v", logTotal, logs, err)
	}

	// 验证利润测算事务：保存版本 1 与版本 2，同步更新主表且旧版本失活
	calc1 := &model.ProfitCalculation{
		ItemID: parentItem.ID, ASIN: parentItem.ASIN, RefStatMonth: "2026-10",
		SellingPrice: 61.50, PurchaseCost: 15.00, FirstLegFreight: 5.00,
		ReferralFee: 9.23, FulfillmentFee: 6.50, AdsAndOtherCost: 5.00,
		ProfitMargin: 0.3377, EstimatedProfit: 20.77, CalculatedBy: "u-1",
	}
	if err := store.ProfitCalcs().SaveActiveCalculation(ctx, ws1.ID, calc1); err != nil {
		t.Fatalf("save profit calc v1: %v", err)
	}
	calc2 := &model.ProfitCalculation{
		ItemID: parentItem.ID, ASIN: parentItem.ASIN, RefStatMonth: "2026-10",
		SellingPrice: 61.50, PurchaseCost: 18.00, FirstLegFreight: 5.00,
		ReferralFee: 9.23, FulfillmentFee: 6.50, AdsAndOtherCost: 5.00,
		ProfitMargin: 0.2889, EstimatedProfit: 17.77, CalculatedBy: "u-1",
	}
	if err := store.ProfitCalcs().SaveActiveCalculation(ctx, ws1.ID, calc2); err != nil {
		t.Fatalf("save profit calc v2: %v", err)
	}
	if calc2.Version != 2 {
		t.Fatalf("expected version 2, got %d", calc2.Version)
	}
	reloadedAfterCalc, err := store.Screening().GetByID(ctx, ws1.ID, parentItem.ID)
	if err != nil {
		t.Fatalf("reload after profit calc: %v", err)
	}
	if reloadedAfterCalc.ProfitCalcStatus != "calculated" || reloadedAfterCalc.LatestCalcID == nil || *reloadedAfterCalc.LatestCalcID != calc2.ID {
		t.Fatalf("expected profit snapshot synced to calc2, got %+v", reloadedAfterCalc)
	}

	// 导出任务
	expTask := &model.ExportTask{
		WorkspaceID:  ws1.ID,
		TaskNo:       "EXP-TEST-" + suffix,
		FilterParams: datatypes.JSON(`{"manual_status":"approved"}`),
		Status:       "completed",
		ExportedRows: 1,
		FilePath:     "/exports/exp-test-001.xlsx",
		OperatorID:   "u-1",
	}
	if err := store.Exports().UpsertTask(ctx, expTask); err != nil {
		t.Fatalf("upsert export task: %v", err)
	}
}

// TestListWithDetailsNoNPlusOne 验证列表页批量加载月度指标与利润测算不存在 N+1 查询。
func TestListWithDetailsNoNPlusOne(t *testing.T) {
	store := openTestStore(t)
	ctx := context.Background()

	ws := &model.Workspace{Code: fmt.Sprintf("ws-nplus1-%d", time.Now().UnixNano()), Name: "Perf Team", CreatedBy: "u-perf"}
	if err := store.Workspaces().UpsertWorkspace(ctx, ws); err != nil {
		t.Fatalf("upsert ws: %v", err)
	}
	cleanupTestWorkspace(t, store.DB(), ws.ID)

	const itemCount = 20
	items := make([]model.ScreeningItem, itemCount)
	for i := 0; i < itemCount; i++ {
		asin := fmt.Sprintf("B09PERF%03d", i)
		items[i] = model.ScreeningItem{
			ASIN: asin, ParentASIN: asin, IsParent: true,
			TitleZh: fmt.Sprintf("测试商品 %d", i), LatestPrice: 29.99 + float64(i), LatestSales: 100 + i*10,
			MachineResult: "passed", ManualStatus: "pending",
		}
	}
	if err := store.Screening().BatchUpsertItems(ctx, ws.ID, items); err != nil {
		t.Fatalf("batch upsert items: %v", err)
	}

	asins := make([]string, itemCount)
	for i, it := range items {
		asins[i] = it.ASIN
	}
	byASIN, err := store.Screening().ListByASINs(ctx, ws.ID, asins)
	if err != nil {
		t.Fatalf("list by asins: %v", err)
	}

	var metrics []model.ScreeningItemMonthlyMetric
	for _, it := range byASIN {
		metrics = append(metrics,
			model.ScreeningItemMonthlyMetric{ItemID: it.ID, ASIN: it.ASIN, StatMonth: "2026-09", SellingPrice: 28.0, MonthlySales: 110},
			model.ScreeningItemMonthlyMetric{ItemID: it.ID, ASIN: it.ASIN, StatMonth: "2026-10", SellingPrice: 30.0, MonthlySales: 150},
		)
	}
	if err := store.MonthlyMetrics().BatchUpsert(ctx, ws.ID, metrics); err != nil {
		t.Fatalf("batch upsert metrics: %v", err)
	}

	// 注册 GORM query 回调计数，确认加载 20 个商品的查询次数为常数 4 次（COUNT + ITEMS + BATCH METRICS + BATCH PROFITS）
	var queryCount int64
	cbName := "test:count_queries"
	if err := store.DB().Callback().Query().Before("gorm:query").Register(cbName, func(tx *gorm.DB) {
		atomic.AddInt64(&queryCount, 1)
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	t.Cleanup(func() {
		_ = store.DB().Callback().Query().Remove(cbName)
	})

	res, err := store.Screening().ListWithDetails(ctx, ws.ID, gormrepo.ScreeningFilter{
		Page:         1,
		PageSize:     20,
		RecentMonths: 3,
		MinPrice:     floatPtr(10),
	})
	if err != nil {
		t.Fatalf("list with details: %v", err)
	}
	if res.Total != itemCount || len(res.Items) != itemCount {
		t.Fatalf("expected %d items, got total=%d len=%d", itemCount, res.Total, len(res.Items))
	}
	for _, d := range res.Items {
		if len(d.MonthlyMetrics) != 2 {
			t.Fatalf("expected 2 monthly metrics for %s, got %d", d.Item.ASIN, len(d.MonthlyMetrics))
		}
	}
	gotQueries := atomic.LoadInt64(&queryCount)
	if gotQueries > 4 {
		t.Fatalf("detected N+1 queries: expected <= 4 queries for %d items, got %d", itemCount, gotQueries)
	}
}
