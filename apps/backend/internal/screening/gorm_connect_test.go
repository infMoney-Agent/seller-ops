package screening

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/db"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
	commonv1 "seller-ops/contracts/sellerops/common/v1"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/contracts/sellerops/screening/v1/screeningv1connect"
	"seller-ops/rpccompat"
)

type signedTransport struct {
	base   http.RoundTripper
	secret string
	user   int64
	tenant int64
	role   string
}

func (t *signedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	stamp := strconv.FormatInt(time.Now().Unix(), 10)
	u := strconv.FormatInt(t.user, 10)
	ws := strconv.FormatInt(t.tenant, 10)
	clone.Header.Set("x-seller-user", u)
	clone.Header.Set("x-seller-workspace", ws)
	clone.Header.Set("x-seller-role", t.role)
	clone.Header.Set("x-screening-time", stamp)
	msg := strings.Join([]string{stamp, clone.Method, clone.URL.RequestURI(), u, ws, t.role}, "\n")
	mac := hmac.New(sha256.New, []byte(t.secret))
	mac.Write([]byte(msg))
	clone.Header.Set("x-screening-signature", hex.EncodeToString(mac.Sum(nil)))
	return t.base.RoundTrip(clone)
}

func TestGORMConnectEndToEnd(t *testing.T) {
	cfg, err := db.LoadConfigWithOptions(db.EnvTest, "")
	if err != nil {
		t.Skipf("skipping GORM Connect E2E test: %v", err)
	}
	ctx := context.Background()
	gdb, err := db.OpenMySQL(ctx, cfg)
	if err != nil {
		t.Skipf("skipping GORM Connect E2E test (MySQL unavailable): %v", err)
	}
	t.Cleanup(func() { _ = db.CloseMySQL(gdb) })

	if _, err := db.Migrate(ctx, gdb, db.MigrateOptions{AppEnv: db.EnvTest, Mode: db.MigrateModeApply}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	store := gormrepo.NewStore(gdb)
	tenantID := int64(900000 + time.Now().UnixNano()%90000)
	ws := &model.Workspace{Code: fmt.Sprintf("tenant-%d", tenantID), Name: "E2E Workspace", CreatedBy: "user-1"}
	if err := store.Workspaces().UpsertWorkspace(ctx, ws); err != nil {
		t.Fatalf("upsert ws: %v", err)
	}
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
		} {
			_ = gdb.Unscoped().Scopes(gormrepo.WithWorkspace(ws.ID)).Delete(m).Error
		}
		_ = gdb.Unscoped().Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: ws.ID}).Delete(&model.Workspace{}).Error
	})

	impTask := &model.ImportTask{
		WorkspaceID: ws.ID, TaskNo: fmt.Sprintf("IMP-E2E-%d", tenantID), FileName: "sample.xlsx",
		SourceType: "excel", Status: "completed", TotalRows: 1, SuccessRows: 1, OperatorID: "user-1",
	}
	if err := store.Imports().UpsertTask(ctx, impTask); err != nil {
		t.Fatalf("upsert import task: %v", err)
	}
	if err := store.Imports().BatchUpsertRecords(ctx, ws.ID, []model.ImportRecord{
		{TaskID: impTask.ID, RowNo: 1, ASIN: "B0E2ETEST1", RawPayload: datatypes.JSON(`{"ASIN":"B0E2ETEST1"}`), Status: "success"},
	}); err != nil {
		t.Fatalf("upsert import records: %v", err)
	}

	if err := store.Screening().BatchUpsertItems(ctx, ws.ID, []model.ScreeningItem{
		{
			ASIN: "B0E2ETEST1", ParentASIN: "B0E2ETEST1", IsParent: true,
			TitleZh: "E2E测试保温杯", TitleEn: "E2E Insulated Tumbler", Brand: "HydroTest",
			MachineResult: "passed", ManualStatus: "pending", LatestPrice: 29.99, LatestSales: 800,
			LastImportTaskID: &impTask.ID,
		},
	}); err != nil {
		t.Fatalf("upsert screening item: %v", err)
	}
	item, err := store.Screening().GetByASIN(ctx, ws.ID, "B0E2ETEST1")
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if err := store.MonthlyMetrics().BatchUpsert(ctx, ws.ID, []model.ScreeningItemMonthlyMetric{
		{ItemID: item.ID, ASIN: item.ASIN, ParentASIN: item.ParentASIN, StatMonth: "2026-09", SellingPrice: 28.99, MonthlySales: 720},
		{ItemID: item.ID, ASIN: item.ASIN, ParentASIN: item.ParentASIN, StatMonth: "2026-10", SellingPrice: 29.99, MonthlySales: 800},
	}); err != nil {
		t.Fatalf("upsert metrics: %v", err)
	}

	const secret = "test-secret-0123456789abcdef0123456789abcdef"
	svc := &service{gormStore: store, cfg: Config{Secret: secret}}
	srv := httptest.NewServer(svc.handler())
	t.Cleanup(srv.Close)

	httpClient := &http.Client{
		Transport: &signedTransport{base: srv.Client().Transport, secret: secret, user: 1, tenant: tenantID, role: "owner"},
	}
	screenClient := screeningv1connect.NewScreeningServiceClient(httpClient, srv.URL)
	importClient := screeningv1connect.NewScreeningImportServiceClient(httpClient, srv.URL)

	// 1. ListProducts（带月度指标批量返回）
	listResp, err := screenClient.ListProducts(ctx, connect.NewRequest(&screeningv1.ListProductsRequest{
		Query: &screeningv1.ScreeningQuery{StatMonth: []string{"2026-10"}},
	}))
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	listMap, err := rpccompat.Legacy(listResp.Msg)
	if err != nil {
		t.Fatalf("decode ListProducts: %v", err)
	}
	listData := listMap["data"].(map[string]any)
	if integer(listData["total"]) != 1 {
		t.Fatalf("expected total=1, got %v", listData["total"])
	}

	// 2. GetSummary
	sumResp, err := screenClient.GetSummary(ctx, connect.NewRequest(&screeningv1.GetSummaryRequest{}))
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	sumMap, err := rpccompat.Legacy(sumResp.Msg)
	if err != nil {
		t.Fatalf("decode GetSummary: %v", err)
	}
	sumData := sumMap["data"].(map[string]any)
	if integer(sumData["products"]) != 1 || integer(sumData["pending"]) != 1 {
		t.Fatalf("unexpected summary: %+v", sumData)
	}

	// 3. ListMonths
	monthsResp, err := screenClient.ListMonths(ctx, connect.NewRequest(&screeningv1.ListMonthsRequest{}))
	if err != nil {
		t.Fatalf("ListMonths: %v", err)
	}
	monthsMap, err := rpccompat.Legacy(monthsResp.Msg)
	if err != nil {
		t.Fatalf("decode ListMonths: %v", err)
	}
	monthItems := monthsMap["data"].(map[string]any)["items"].([]any)
	if len(monthItems) != 2 {
		t.Fatalf("expected 2 months, got %d", len(monthItems))
	}

	// 4. ReviewProduct（事务更新主表 + 写审计流水）
	itemIDStr := strconv.FormatUint(item.ID, 10)
	reviewBody := &screeningv1.ReviewInput{}
	if err := rpccompat.FromLegacy(reviewBody, Object{
		"status": "kept",
		"reason": "利润空间好",
		"note":   "Connect GORM E2E 保留",
	}); err != nil {
		t.Fatalf("encode review body: %v", err)
	}
	revResp, err := screenClient.ReviewProduct(ctx, connect.NewRequest(&screeningv1.ReviewProductRequest{
		Id:       &itemIDStr,
		Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "e2e-rev-1"},
		Body:     reviewBody,
	}))
	if err != nil {
		t.Fatalf("ReviewProduct: %v", err)
	}
	revMap, err := rpccompat.Legacy(revResp.Msg)
	if err != nil {
		t.Fatalf("decode ReviewProduct: %v", err)
	}
	if str(revMap["data"].(map[string]any)["review_status"]) != "kept" {
		t.Fatalf("expected review_status=kept, got %v", revMap["data"].(map[string]any)["review_status"])
	}

	// 5. ListProductHistory 验证审计流水落库
	histResp, err := screenClient.ListProductHistory(ctx, connect.NewRequest(&screeningv1.ListProductHistoryRequest{Id: &itemIDStr}))
	if err != nil {
		t.Fatalf("ListProductHistory: %v", err)
	}
	histMap, err := rpccompat.Legacy(histResp.Msg)
	if err != nil {
		t.Fatalf("decode ListProductHistory: %v", err)
	}
	if integer(histMap["data"].(map[string]any)["total"]) != 1 {
		t.Fatalf("expected 1 review log, got %v", histMap["data"].(map[string]any)["total"])
	}

	// 6. SaveProfitScenario（事务保存版本并同步主表利润快照）
	profitBody := &screeningv1.SaveProfitInput{}
	if err := rpccompat.FromLegacy(profitBody, Object{
		"inputs": Object{
			"name":                "基准方案",
			"sale_price_usd":      "29.99",
			"purchase_cny":        "35",
			"freight_cny":         "12",
			"fba_fee_usd":         "5.40",
			"commission_rate":     "0.15",
			"fx_cny_per_usd":      "7.2",
			"storage_month":       "2026-10",
			"storage_usd":         "0.50",
			"ad_rate":             "0.10",
			"return_rate":         "0.03",
			"loss_per_return_usd": "3.00",
			"discount_usd":        "0",
			"packaging_cny":       "2",
			"other_landed_cny":    "0",
			"other_usd":           "0",
		},
	}); err != nil {
		t.Fatalf("encode profit body: %v", err)
	}
	if _, err := screenClient.SaveProfitScenario(ctx, connect.NewRequest(&screeningv1.SaveProfitScenarioRequest{
		Id:       &itemIDStr,
		Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "e2e-profit-1"},
		Body:     profitBody,
	})); err != nil {
		t.Fatalf("SaveProfitScenario: %v", err)
	}
	prodResp, err := screenClient.GetProduct(ctx, connect.NewRequest(&screeningv1.GetProductRequest{Id: &itemIDStr}))
	if err != nil {
		t.Fatalf("GetProduct after profit save: %v", err)
	}
	prodMap, err := rpccompat.Legacy(prodResp.Msg)
	if err != nil {
		t.Fatalf("decode GetProduct: %v", err)
	}
	if str(prodMap["data"].(map[string]any)["profit_calc_status"]) != "calculated" {
		t.Fatalf("expected profit_calc_status=calculated, got %v", prodMap["data"].(map[string]any)["profit_calc_status"])
	}

	// 7. ListImports & ListImportRows
	impListResp, err := importClient.ListImports(ctx, connect.NewRequest(&screeningv1.ListImportsRequest{
		Pagination: &commonv1.PaginationRequest{Page: 1, PageSize: 20},
	}))
	if err != nil {
		t.Fatalf("ListImports: %v", err)
	}
	if impListResp.Msg.GetPagination().GetTotal() != 1 {
		t.Fatalf("expected 1 import task, got %d", impListResp.Msg.GetPagination().GetTotal())
	}
	impRowsResp, err := importClient.ListImportRows(ctx, connect.NewRequest(&screeningv1.ListImportRowsRequest{
		ImportId:   strconv.FormatUint(impTask.ID, 10),
		Pagination: &commonv1.PaginationRequest{Page: 1, PageSize: 20},
	}))
	if err != nil {
		t.Fatalf("ListImportRows: %v", err)
	}
	if impRowsResp.Msg.GetPagination().GetTotal() != 1 {
		t.Fatalf("expected 1 import row, got %d", impRowsResp.Msg.GetPagination().GetTotal())
	}
}

func TestGORMFullRoutesValidationAndPermissions(t *testing.T) {
	cfg, err := db.LoadConfigWithOptions(db.EnvTest, "")
	if err != nil {
		t.Skipf("skipping GORM full routes test: %v", err)
	}
	ctx := context.Background()
	gdb, err := db.OpenMySQL(ctx, cfg)
	if err != nil {
		t.Skipf("skipping GORM full routes test (MySQL unavailable): %v", err)
	}
	t.Cleanup(func() { _ = db.CloseMySQL(gdb) })

	if _, err := db.Migrate(ctx, gdb, db.MigrateOptions{AppEnv: db.EnvTest, Mode: db.MigrateModeApply}); err != nil {
		t.Fatalf("migrate test db: %v", err)
	}

	store := gormrepo.NewStore(gdb)
	tenantID := int64(800000 + time.Now().UnixNano()%90000)
	ws := &model.Workspace{Code: fmt.Sprintf("ws-tenant-%d", tenantID), Name: "Full Routes WS", CreatedBy: "user-1"}
	if err := store.Workspaces().UpsertWorkspace(ctx, ws); err != nil {
		t.Fatalf("upsert ws: %v", err)
	}
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
			_ = gdb.Unscoped().Scopes(gormrepo.WithWorkspace(ws.ID)).Delete(m).Error
		}
		_ = gdb.Unscoped().Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: ws.ID}).Delete(&model.Workspace{}).Error
	})

	if err := store.Screening().BatchUpsertItems(ctx, ws.ID, []model.ScreeningItem{
		{
			ASIN: "B0FULL0001", ParentASIN: "B0PARENT01", IsParent: false,
			TitleZh: "测试商品A", TitleEn: "Test Item A", Brand: "BrandA",
			MachineResult: "passed", ManualStatus: "pending", LatestPrice: 25.00, LatestSales: 400,
		},
		{
			ASIN: "B0FULL0002", ParentASIN: "B0PARENT01", IsParent: false,
			TitleZh: "测试商品B", TitleEn: "Test Item B", Brand: "BrandA",
			MachineResult: "passed", ManualStatus: "pending", LatestPrice: 30.00, LatestSales: 600,
		},
	}); err != nil {
		t.Fatalf("seed items: %v", err)
	}
	it1, _ := store.Screening().GetByASIN(ctx, ws.ID, "B0FULL0001")
	it2, _ := store.Screening().GetByASIN(ctx, ws.ID, "B0FULL0002")
	id1 := strconv.FormatUint(it1.ID, 10)
	id2 := strconv.FormatUint(it2.ID, 10)

	const secret = "test-secret-0123456789abcdef0123456789abcdef"
	svc := &service{gormStore: store, cfg: Config{Secret: secret, Storage: t.TempDir()}}
	srv := httptest.NewServer(svc.handler())
	t.Cleanup(srv.Close)

	ownerHTTP := &http.Client{Transport: &signedTransport{base: srv.Client().Transport, secret: secret, user: 1, tenant: tenantID, role: "owner"}}
	memberHTTP := &http.Client{Transport: &signedTransport{base: srv.Client().Transport, secret: secret, user: 2, tenant: tenantID, role: "member"}}
	ownerClient := screeningv1connect.NewScreeningServiceClient(ownerHTTP, srv.URL)
	memberClient := screeningv1connect.NewScreeningServiceClient(memberHTTP, srv.URL)
	importClient := screeningv1connect.NewScreeningImportServiceClient(ownerHTTP, srv.URL)

	// 1. 验证父体折叠数量：2 个子体共享同一个未入库父体 B0PARENT01 -> displayed=1, ListProducts total=1
	sumResp, err := ownerClient.GetSummary(ctx, connect.NewRequest(&screeningv1.GetSummaryRequest{}))
	if err != nil {
		t.Fatalf("GetSummary: %v", err)
	}
	sumMap, _ := rpccompat.Legacy(sumResp.Msg)
	sumData := sumMap["data"].(map[string]any)
	if integer(sumData["products"]) != 2 || integer(sumData["displayed"]) != 1 {
		t.Fatalf("expected products=2 displayed=1, got %+v", sumData)
	}
	listResp, err := ownerClient.ListProducts(ctx, connect.NewRequest(&screeningv1.ListProductsRequest{}))
	if err != nil {
		t.Fatalf("ListProducts: %v", err)
	}
	listMap, _ := rpccompat.Legacy(listResp.Msg)
	if integer(listMap["data"].(map[string]any)["total"]) != 1 {
		t.Fatalf("expected folded ListProducts total=1, got %v", listMap["data"].(map[string]any)["total"])
	}

	// 2. 参数保存：正常更新 + 非法父体 ASIN 校验失败
	badParams := &screeningv1.ParametersInput{}
	_ = rpccompat.FromLegacy(badParams, Object{"parent_asin": "BAD"})
	if _, err := ownerClient.UpdateProductParameters(ctx, connect.NewRequest(&screeningv1.UpdateProductParametersRequest{
		Id: &id1, Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "param-bad"}, Body: badParams,
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument for bad parent_asin, got %v", err)
	}
	goodParams := &screeningv1.ParametersInput{}
	_ = rpccompat.FromLegacy(goodParams, Object{"weight_g": "320.5", "package_weight_g": "410", "product_length_cm": "10", "product_width_cm": "8", "product_height_cm": "5"})
	if _, err := ownerClient.UpdateProductParameters(ctx, connect.NewRequest(&screeningv1.UpdateProductParametersRequest{
		Id: &id1, Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "param-good"}, Body: goodParams,
	})); err != nil {
		t.Fatalf("UpdateProductParameters: %v", err)
	}

	// 3. 批量复核：校验失败 -> Preview -> Apply -> Undo -> 重复撤销报错
	badPreview := &screeningv1.ReviewBatchInput{}
	_ = rpccompat.FromLegacy(badPreview, Object{"product_ids": []string{id1, id2}, "scope": "selected", "action": "review", "mode": "direct", "status": "excluded", "reason": ""})
	if _, err := ownerClient.CreateReviewPreview(ctx, connect.NewRequest(&screeningv1.CreateReviewPreviewRequest{
		Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "batch-bad"}, Body: badPreview,
	})); connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument for empty exclude reason, got %v", err)
	}

	goodPreview := &screeningv1.ReviewBatchInput{}
	_ = rpccompat.FromLegacy(goodPreview, Object{"product_ids": []string{id1, id2}, "scope": "selected", "action": "review", "mode": "direct", "status": "kept", "note": "批量保留"})
	prevResp, err := ownerClient.CreateReviewPreview(ctx, connect.NewRequest(&screeningv1.CreateReviewPreviewRequest{
		Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "batch-good"}, Body: goodPreview,
	}))
	if err != nil {
		t.Fatalf("CreateReviewPreview: %v", err)
	}
	prevMap, _ := rpccompat.Legacy(prevResp.Msg)
	batchID := str(prevMap["data"].(map[string]any)["id"])
	if batchID == "" {
		t.Fatalf("expected non-empty batch ID")
	}

	applyResp, err := ownerClient.ApplyReviewBatch(ctx, connect.NewRequest(&screeningv1.ApplyReviewBatchRequest{
		Id: &batchID, Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "batch-apply"},
	}))
	if err != nil {
		t.Fatalf("ApplyReviewBatch: %v", err)
	}
	applyMap, _ := rpccompat.Legacy(applyResp.Msg)
	if integer(applyMap["data"].(map[string]any)["success"]) != 2 {
		t.Fatalf("expected 2 applied, got %+v", applyMap["data"])
	}
	afterApply, _ := store.Screening().GetByID(ctx, ws.ID, it1.ID)
	if afterApply.ManualStatus != "approved" {
		t.Fatalf("expected item1 approved after apply, got %s", afterApply.ManualStatus)
	}

	undoResp, err := ownerClient.UndoReviewBatch(ctx, connect.NewRequest(&screeningv1.UndoReviewBatchRequest{
		Id: &batchID, Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "batch-undo"},
	}))
	if err != nil {
		t.Fatalf("UndoReviewBatch: %v", err)
	}
	undoMap, _ := rpccompat.Legacy(undoResp.Msg)
	if integer(undoMap["data"].(map[string]any)["success"]) != 2 {
		t.Fatalf("expected 2 undone, got %+v", undoMap["data"])
	}
	afterUndo, _ := store.Screening().GetByID(ctx, ws.ID, it1.ID)
	if afterUndo.ManualStatus != "pending" {
		t.Fatalf("expected item1 restored to pending after undo, got %s", afterUndo.ManualStatus)
	}

	// 4. 权限边界：member 禁止创建规则/模板；owner 可创建、试运行并启用
	policyIn := &screeningv1.ConfigInput{}
	_ = rpccompat.FromLegacy(policyIn, Object{"name": "严格规则", "config": decodeObject(defaultPolicyData)})
	if _, err := memberClient.CreatePolicy(ctx, connect.NewRequest(&screeningv1.CreatePolicyRequest{
		Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "pol-member"}, Body: policyIn,
	})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("expected PermissionDenied for member CreatePolicy, got %v", err)
	}
	polResp, err := ownerClient.CreatePolicy(ctx, connect.NewRequest(&screeningv1.CreatePolicyRequest{
		Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "pol-owner"}, Body: policyIn,
	}))
	if err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}
	polMap, _ := rpccompat.Legacy(polResp.Msg)
	polID := str(polMap["data"].(map[string]any)["id"])

	testPolIn := &screeningv1.PolicyTestInput{}
	_ = rpccompat.FromLegacy(testPolIn, Object{"product_ids": []string{id1}})
	if _, err := ownerClient.TestPolicy(ctx, connect.NewRequest(&screeningv1.TestPolicyRequest{
		Id: &polID, Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "pol-test"}, Body: testPolIn,
	})); err != nil {
		t.Fatalf("TestPolicy: %v", err)
	}
	if _, err := ownerClient.ActivatePolicy(ctx, connect.NewRequest(&screeningv1.ActivatePolicyRequest{
		Id: &polID, Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "pol-activate"},
	})); err != nil {
		t.Fatalf("ActivatePolicy: %v", err)
	}

	// 5. 导出任务 + MySQL Worker 执行 + 下载验证
	expIn := &screeningv1.SelectionInput{}
	_ = rpccompat.FromLegacy(expIn, Object{"product_ids": []string{id1, id2}})
	expResp, err := ownerClient.CreateExport(ctx, connect.NewRequest(&screeningv1.CreateExportRequest{
		Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "exp-1"}, Body: expIn,
	}))
	if err != nil {
		t.Fatalf("CreateExport: %v", err)
	}
	expMap, _ := rpccompat.Legacy(expResp.Msg)
	expJobID := str(expMap["data"].(map[string]any)["job_id"])

	// 驱动 MySQL 后台任务循环消费 screen + export 任务
	for i := 0; i < 5; i++ {
		processed, err := svc.gormProcessNextJob(ctx)
		if err != nil {
			t.Fatalf("gormProcessNextJob: %v", err)
		}
		if !processed {
			break
		}
	}
	jobResp, err := importClient.GetJob(ctx, connect.NewRequest(&screeningv1.GetJobRequest{Id: expJobID}))
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if jobResp.Msg.GetItem().GetState() != screeningv1.JobState_JOB_STATE_SUCCEEDED {
		t.Fatalf("expected export job succeeded, got %v", jobResp.Msg.GetItem().GetState())
	}
	dlResp, err := ownerHTTP.Get(srv.URL + "/api/v1/screening/exports/" + expJobID + "/download")
	if err != nil || dlResp.StatusCode != 200 {
		t.Fatalf("export download failed: status=%v err=%v", dlResp.StatusCode, err)
	}
	_ = dlResp.Body.Close()
}
