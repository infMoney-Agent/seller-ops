package screening

import (
	"context"
	"fmt"
	"gorm.io/gorm/clause"
	"net/url"
	"seller-ops/backend/internal/db"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/rpccompat"
	"strconv"
	"testing"
	"time"
)

func reviewFixture(t *testing.T) (context.Context, *service, identity, uint64) {
	t.Helper()
	ctx := context.Background()
	cfg, err := db.LoadConfigWithOptions(db.EnvTest, "")
	if err != nil {
		t.Fatal(err)
	}
	g, err := db.OpenMySQL(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Migrate(ctx, g, db.MigrateOptions{AppEnv: db.EnvTest, Mode: db.MigrateModeApply}); err != nil {
		t.Fatal(err)
	}
	store := gormrepo.NewStore(g)
	tenant := time.Now().UnixNano()%1000000000 + 1000000
	ws := &model.Workspace{Code: fmt.Sprintf("ws-tenant-%d", tenant), Name: "review fixture", CreatedBy: "test"}
	if err = store.Workspaces().UpsertWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		g.Unscoped().Scopes(gormrepo.WithWorkspace(ws.ID)).Delete(&model.ScreeningItem{})
		g.Unscoped().Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: ws.ID}).Delete(&model.Workspace{})
		db.CloseMySQL(g)
	})
	return ctx, &service{gormStore: store}, identity{Tenant: tenant, User: 19, Role: "owner"}, ws.ID
}
func reviewItem(ws uint64, index int, parent, status, path string, sales int) model.ScreeningItem {
	when := time.Date(2026, 11, 27, 10, index, 0, 0, time.UTC)
	data := Object{"id": fmt.Sprintf("legacy-%03d", index), "category_path": path, "main_category": "Main", "price_usd": "20.00", "rating": "4.8", "stat_month": "2026-10-01", "display_child_sales": sales, "sales_scope": "child", "monthly_sales": sales, "title": "review fixture", "source_monthly_history": []Object{{"id": fmt.Sprintf("month-%d-oct", index), "revision": 1, "stat_month": "2026-10-01", "period_type": "calendar_month", "period_end": "2026-10-31", "source_type": "sellersprite", "price_usd": "20.00", "monthly_sales": sales, "display_child_sales": sales, "sales_scope": "child", "rating": 4.8}, {"id": fmt.Sprintf("month-%d-sep", index), "revision": 1, "stat_month": "2026-09-01", "period_type": "calendar_month", "period_end": "2026-09-30", "source_type": "sellersprite", "price_usd": "10.00", "monthly_sales": sales, "display_child_sales": sales, "sales_scope": "child", "rating": 4.6}}}

	if index == 1 || index == 2 {
		data["direction"] = "shared"
	}
	return model.ScreeningItem{WorkspaceID: ws, ASIN: fmt.Sprintf("B0REVIEW%02d", index), ParentASIN: parent, ManualStatus: status, MachineResult: "passed", SourceFamilyRank: index, ChildSales: &sales, ReviewedAt: &when, CategoryPath: path, CategoryZh: "Sub", SourceData: encoded(data)}
}
func TestGORMReviewWorkspaceContractAndScope(t *testing.T) {
	ctx, s, a, ws := reviewFixture(t)
	rows := []model.ScreeningItem{reviewItem(ws, 1, "B0FAMILYA", "pending", "Root:Leaf", 100), reviewItem(ws, 2, "B0FAMILYA", "approved", "Root:Leaf", 20), reviewItem(ws, 3, "B0FAMILYA", "rejected", "Root:Leaf", 50), reviewItem(ws, 4, "B0FAMILYB", "pending", "Root:Other", 40), reviewItem(ws, 5, "B0FAMILYB", "unsure", "Root:Other", 10), reviewItem(ws, 6, "", "approved", "Root:Finished", 1), reviewItem(ws, 7, "", "pending", "", 2)}
	if err := s.gormStore.DB().Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	call := func(q url.Values) Object {
		t.Helper()
		v, err := s.gormReviewWorkspaceUsecase(ctx, a, q)
		if err != nil {
			t.Fatal(err)
		}
		if err = rpccompat.FromLegacy(&screeningv1.GetReviewWorkspaceResponse{}, Object{"data": publicJSON(v)}); err != nil {
			t.Fatal(err)
		}
		return v.(Object)
	}
	data := call(url.Values{})
	counts := data["counts"].(Object)
	samples := data["samples"].(Object)
	if integer(counts["products"]) != 6 || integer(counts["families"]) != 4 || integer(counts["pending"]) != 3 || integer(samples["total"]) != 4 {
		t.Fatalf("wrong scope/counts %+v / %+v", counts, samples)
	}
	seenExcluded, seenUnsure := false, false
	for _, r := range samples["items"].([]Object) {
		seenExcluded = seenExcluded || r["review_status"] == "excluded"
		seenUnsure = seenUnsure || r["review_status"] == "unsure"
	}
	if !seenExcluded || !seenUnsure {
		t.Fatal("reference samples omitted excluded/unsure decisions")
	}
	for _, r := range data["items"].([]Object) {
		if r["parent_asin"] == "B0FAMILYA" {
			if integer(r["family_count"]) != 2 || len(r["family_items"].([]Object)) != 2 {
				t.Fatal("family must contain only eligible filtered members")
			}
		}
	}
	leaf := call(url.Values{"category_path": {`["Root","Leaf"]`}})
	if integer(leaf["counts"].(Object)["products"]) != 2 || integer(leaf["samples"].(Object)["total"]) != 2 {
		t.Fatal("category scope was ignored")
	}
	family := call(url.Values{"family_product_id": {strconv.FormatUint(rows[0].ID, 10)}})
	if len(family["items"].([]Object)) != 2 || integer(family["group"].(Object)["display_products"]) != 2 {
		t.Fatal("family expansion ignored anchor")
	}
	selected := call(url.Values{"selected_ids": {strconv.FormatUint(rows[0].ID, 10)}})
	if integer(selected["counts"].(Object)["products"]) != 1 || integer(selected["samples"].(Object)["total"]) != 4 {
		t.Fatal("selection must scope candidates, not reference samples")
	}

	oldMonth := call(url.Values{"stat_month": {"2026-09-01"}, "category_path": {`["Root","Leaf"]`}})
	if oldMonth["items"].([]Object)[0]["price_usd"] != "10.00" {
		t.Fatal("selected month price missing")
	}
	for _, sample := range oldMonth["samples"].(Object)["items"].([]Object) {
		if sample["price_usd"] != "20.00" {
			t.Fatal("candidate month mutated reference sample")
		}
	}

	referenced := call(url.Values{"has_reference": {"true"}})
	if integer(referenced["counts"].(Object)["products"]) != 1 {
		t.Fatal("reference filtering counted self or unrelated directions")
	}
	unreferenced := call(url.Values{"has_reference": {"false"}})
	if integer(unreferenced["counts"].(Object)["products"]) != 5 {
		t.Fatal("reference exclusion omitted null directions")
	}
	clamped := call(url.Values{"member_page": {"999"}, "sample_page": {"999"}})
	if integer(clamped["page"]) != 1 || integer(clamped["samples"].(Object)["page"]) != 1 {
		t.Fatal("pagination was not clamped")
	}
	categories, err := s.gormReviewCategoriesUsecase(ctx, a, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	for _, node := range categories.(Object)["items"].([]*reviewCategoryNode) {
		if node.Name == "Root" {
			if node.Products != 5 || node.Pending != 2 {
				t.Fatalf("ancestor totals changed %+v", node)
			}
			for _, child := range node.Children {
				if child.Name == "Finished" {
					t.Fatal("completed category not pruned")
				}
			}
		}
	}

	if uint64(a.Tenant) != ws {
		if _, e := s.resolveWorkspaceID(ctx, identity{Tenant: int64(ws), User: 19, Role: "owner"}); e == nil {
			t.Fatal("unmapped auth tenant borrowed an unrelated MySQL workspace ID")
		}
	}
	for _, q := range []url.Values{{"category_path": {"bad"}}, {"category_path": {`[]`}}, {"member_page": {"0"}}, {"sample_page": {"-1"}}, {"selected_ids": {"0"}}} {
		if _, err = s.gormReviewWorkspaceUsecase(ctx, a, q); err == nil {
			t.Fatalf("accepted bad query %+v", q)
		}
	}
}
func TestGORMReviewFamilyPagination(t *testing.T) {
	ctx, s, a, ws := reviewFixture(t)
	rows := []model.ScreeningItem{}
	for i := 1; i <= 53; i++ {
		rows = append(rows, reviewItem(ws, i, "B0LARGEFAM", "pending", "Root:Large", 100-i))
	}
	if err := s.gormStore.DB().Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	value, err := s.gormReviewWorkspaceUsecase(ctx, a, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	data := value.(Object)
	first := data["items"].([]Object)[0]
	if integer(first["family_count"]) != 53 || len(first["family_items"].([]Object)) != 50 {
		t.Fatal("family preload not capped at 50")
	}
	value, err = s.gormReviewWorkspaceUsecase(ctx, a, url.Values{"family_product_id": {strconv.FormatUint(rows[0].ID, 10)}, "member_page": {"2"}})
	if err != nil {
		t.Fatal(err)
	}
	data = value.(Object)
	if integer(data["page"]) != 2 || len(data["items"].([]Object)) != 3 {
		t.Fatal("expanded second page omitted members")
	}
	if err = rpccompat.FromLegacy(&screeningv1.GetReviewWorkspaceResponse{}, Object{"data": publicJSON(data)}); err != nil {
		t.Fatal(err)
	}
}
