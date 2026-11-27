package screening

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"testing"
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/db"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/rpccompat"
)

func TestGORMFidelityProjection(t *testing.T) {
	payload := datatypes.JSON(`{"seller_name":"原卖家","listing_date":"2025-05-20","price_usd":null,"monthly_sales":null,"display_child_sales":345,"sales_scope":"parent","review_status":"excluded","source_monthly_history":[{"price_usd":"9.99"}]}`)
	out := gormItemToObject(gormrepo.ScreeningItemWithDetails{Item: model.ScreeningItem{BaseModel: model.BaseModel{ID: 123}, ASIN: "B0EXAMPLE", ManualStatus: "approved", MachineResult: "insufficient", SourceData: payload}})
	if out["seller_name"] != "原卖家" || out["listing_date"] != "2025-05-20" || out["price_usd"] != nil || out["monthly_sales"] != nil || integer(out["display_child_sales"]) != 345 {
		t.Fatalf("source projection lost fields: %+v", out)
	}
	if out["review_status"] != "kept" || out["decision"] != "insufficient" || out["id"] != "123" {
		t.Fatalf("source overwrote mutable fields: %+v", out)
	}
	if _, ok := out["source_monthly_history"]; ok {
		t.Fatal("private archive leaked into list")
	}
	for _, v := range []string{"pending", "kept", "excluded", "unsure"} {
		if got := mapGORMToLegacyManual(mapLegacyToGORMManual(v)); got != v {
			t.Fatalf("manual %s became %s", v, got)
		}
	}
	for _, v := range []string{"passed", "excluded", "insufficient", "pending"} {
		if got := mapGORMToLegacyMachine(mapLegacyToGORMMachine(v)); got != v {
			t.Fatalf("machine %s became %s", v, got)
		}
	}
}

func TestGORMFidelityMissingParent(t *testing.T) {
	cfg, err := db.LoadConfigWithOptions(db.EnvTest, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	gdb, err := db.OpenMySQL(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.CloseMySQL(gdb)
	if _, err = db.Migrate(ctx, gdb, db.MigrateOptions{AppEnv: db.EnvTest, Mode: db.MigrateModeApply}); err != nil {
		t.Fatal(err)
	}
	store := gormrepo.NewStore(gdb)
	tenant := time.Now().UnixNano()%1000000000 + 1000000
	ws := &model.Workspace{Code: fmt.Sprintf("ws-tenant-%d", tenant), Name: "fidelity test", CreatedBy: "test"}
	if err = store.Workspaces().UpsertWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		gdb.Unscoped().Scopes(gormrepo.WithWorkspace(ws.ID)).Delete(&model.ScreeningItem{})
		gdb.Unscoped().Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: ws.ID}).Delete(&model.Workspace{})
	})
	snapshot, _ := json.Marshal(Object{"seller_name": "原卖家", "listing_date": "2025-05-20", "price_usd": "24.99", "monthly_sales": nil, "sales_scope": "parent", "display_child_sales": 123})
	items := []model.ScreeningItem{
		{ASIN: "B0CHILD001", ParentASIN: "B0MISSINGP", ManualStatus: "rejected", MachineResult: "rejected", SourceData: snapshot},
		{ASIN: "B0CHILD002", ParentASIN: "B0MISSINGP", ManualStatus: "pending", MachineResult: "passed", SourceData: snapshot},
		{ASIN: "B0SINGLE01", ManualStatus: "unsure", MachineResult: "insufficient", SourceData: snapshot},
	}
	if err = store.Screening().BatchUpsertItems(ctx, ws.ID, items); err != nil {
		t.Fatal(err)
	}
	svc := &service{gormStore: store}
	actor := identity{Tenant: tenant, User: 1, Role: "owner"}
	all, err := svc.gormProductsUsecase(ctx, actor, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	allRows := all.(Object)
	if integer(allRows["total"]) != 2 {
		t.Fatalf("expected 2 families without physical parent, got %+v", allRows)
	}
	pending, err := svc.gormProductsUsecase(ctx, actor, url.Values{"review_status": {"pending"}, "machine": {"passed"}})
	if err != nil {
		t.Fatal(err)
	}
	rows := pending.(Object)["items"].([]Object)
	if len(rows) != 1 || rows[0]["asin"] != "B0CHILD002" || integer(rows[0]["family_count"]) != 2 {
		t.Fatalf("filter must precede folding: %+v", pending)
	}
	summary, err := svc.gormSummaryUsecase(ctx, actor, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	sum := summary.(Object)
	if integer(sum["products"]) != 3 || integer(sum["displayed"]) != 2 || integer(sum["pending_displayed"]) != 1 || integer(sum["pending"]) != 1 {
		t.Fatalf("wrong family summary %+v", sum)
	}
	unsure, err := svc.gormProductsUsecase(ctx, actor, url.Values{"review_status": {"unsure"}, "machine": {"insufficient"}})
	if err != nil {
		t.Fatal(err)
	}
	if integer(unsure.(Object)["total"]) != 1 {
		t.Fatalf("lost distinct statuses: %+v", unsure)
	}
}

func TestArchivedMonthlyDecimalRPC(t *testing.T) {
	source := Object{"id": "legacy-row", "price_usd": "24.9900", "monthly_revenue": json.Number("2741.0000"), "rating": 4.8, "monthly_sales": 0, "review_count": 0, "display_child_sales": nil, "product_id": "legacy-product"}
	row := gormArchivedMonthlyToObject(source, "5327")
	var record screeningv1.MonthlyRecord
	if err := rpccompat.FromLegacy(&record, row); err != nil {
		t.Fatal(err)
	}
	back, err := rpccompat.Legacy(&record)
	if err != nil {
		t.Fatal(err)
	}
	if back["price_usd"] != "24.9900" || back["monthly_revenue"] != "2741.0000" || back["rating"] != "4.8" || back["display_child_sales"] != nil || integer(back["monthly_sales"]) != 0 || back["product_id"] != "5327" {
		t.Fatalf("monthly values changed %+v", back)
	}
	if source["product_id"] != "legacy-product" {
		t.Fatal("source snapshot mutated")
	}
	nulls := gormArchivedMonthlyToObject(Object{"rating": nil, "monthly_revenue": nil, "price_usd": nil}, "5327")
	if err = rpccompat.FromLegacy(&record, nulls); err != nil {
		t.Fatal(err)
	}
}
