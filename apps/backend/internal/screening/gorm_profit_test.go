package screening

import (
	"connectrpc.com/connect"
	"context"
	"encoding/json"
	"fmt"
	"gorm.io/gorm/clause"
	"net/http"
	"net/http/httptest"
	"net/url"
	"seller-ops/backend/internal/db"
	"seller-ops/backend/internal/model"
	"seller-ops/backend/internal/storage/gormrepo"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/contracts/sellerops/screening/v1/screeningv1connect"
	"seller-ops/rpccompat"
	"strconv"
	"testing"
	"time"
)

func TestGORMProfitParityAndSave(t *testing.T) {
	cfg, err := db.LoadConfigWithOptions(db.EnvTest, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	g, err := db.OpenMySQL(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer db.CloseMySQL(g)
	if _, err = db.Migrate(ctx, g, db.MigrateOptions{AppEnv: db.EnvTest, Mode: db.MigrateModeApply}); err != nil {
		t.Fatal(err)
	}
	store := gormrepo.NewStore(g)
	tenant := time.Now().UnixNano()%1000000000 + 1000000
	ws := &model.Workspace{Code: fmt.Sprintf("ws-tenant-%d", tenant), Name: "profit parity", CreatedBy: "test"}
	if err = store.Workspaces().UpsertWorkspace(ctx, ws); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, m := range []any{&model.ProfitCalculation{}, &model.ScreeningItem{}} {
			g.Unscoped().Scopes(gormrepo.WithWorkspace(ws.ID)).Delete(m)
		}
		g.Unscoped().Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: ws.ID}).Delete(&model.Workspace{})
	})
	base := encoded(Object{"price_usd": "20.00", "package_length_cm": "20", "package_width_cm": "10", "package_height_cm": "10", "package_weight_g": "300"})
	complete := encoded(Object{"scenario": Object{"inputs": profitTableFixture()}, "imported_fees": Object{"fba_fee_usd": "9.99"}})
	incomplete := encoded(Object{"scenario": nil, "imported_fees": Object{"fba_fee_usd": "5.50"}})
	items := []model.ScreeningItem{{ASIN: "B0PROFIT01", ParentASIN: "B0FAMILY00", ManualStatus: "approved", SourceFamilyRank: 1, SourceData: base, ProfitSourceData: complete}, {ASIN: "B0PROFIT02", ParentASIN: "B0FAMILY00", ManualStatus: "approved", SourceFamilyRank: 2, SourceData: base, ProfitSourceData: incomplete}, {ASIN: "B0PROFIT03", ManualStatus: "approved", SourceData: base, ProfitSourceData: complete}, {ASIN: "B0EXCLUDE1", ManualStatus: "rejected", SourceData: base, ProfitSourceData: complete}}
	// Insert directly because migration/ingestion upserts intentionally have a
	// separate supplementary snapshot backfill.
	for i := range items {
		items[i].WorkspaceID = ws.ID
	}
	if err = g.Create(&items).Error; err != nil {
		t.Fatal(err)
	}
	const secret = "profit-test-secret-0123456789abcdef0123456789"
	svc := &service{gormStore: store, cfg: Config{Secret: secret}}
	a := identity{Tenant: tenant, User: 19, Role: "owner"}
	cases := []struct {
		completion       string
		groups, products int
	}{{"", 2, 3}, {"complete", 2, 2}, {"incomplete", 1, 1}}
	for _, tc := range cases {
		value, e := svc.gormProfitProductsUsecase(ctx, a, url.Values{"group_by_parent": {"true"}, "completion": {tc.completion}})
		if e != nil {
			t.Fatal(e)
		}
		data := value.(Object)
		if int(integer(data["total"])) != tc.groups || int(integer(data["product_total"])) != tc.products {
			t.Fatalf("completion %q counts %+v", tc.completion, data)
		}
		if e = rpccompat.FromLegacy(&screeningv1.ListProfitProductsResponse{}, Object{"data": publicJSON(data)}); e != nil {
			t.Fatal(e)
		}
	}
	first, e := store.Screening().GetByASIN(ctx, ws.ID, "B0PROFIT01")
	if e != nil {
		t.Fatal(e)
	}
	focus := strconv.FormatUint(first.ID, 10)
	before, e := svc.gormProfitProductsUsecase(ctx, a, url.Values{"product_id": {focus}})
	if e != nil {
		t.Fatal(e)
	}
	row := before.(Object)["items"].([]Object)[0]
	in := row["draft_inputs"].(map[string]any)
	if in["sale_price_usd"] != "100" || in["fba_fee_usd"] != "4" || in["purchase_cny"] != "70" {
		t.Fatalf("lost source inputs %+v", in)
	}
	changed := profitTableFixture()
	changed["sale_price_usd"] = "123.45"
	changed["fba_fee_usd"] = "6.789"
	changed["purchase_cny"] = "87"
	changed["supplier_url"] = "https://example.com/new-source"
	if _, e = svc.gormSaveProfit(ctx, a, focus, SaveProfitScenarioCommand{HasInputs: true, Inputs: changed}); e != nil {
		t.Fatal(e)
	}
	after, e := svc.gormProfitProductsUsecase(ctx, a, url.Values{"product_id": {focus}})
	if e != nil {
		t.Fatal(e)
	}
	row = after.(Object)["items"].([]Object)[0]
	in = row["draft_inputs"].(map[string]any)
	for key, want := range map[string]string{"sale_price_usd": "123.45", "fba_fee_usd": "6.789", "purchase_cny": "87", "supplier_url": "https://example.com/new-source"} {
		if str(in[key]) != want {
			t.Fatalf("saved %s = %v want %s", key, in[key], want)
		}
	}
	archived, e := svc.gormListProductProfitScenariosUsecase(ctx, a, focus, url.Values{})
	if e != nil {
		t.Fatal(e)
	}
	if integer(archived.(Object)["total"]) != 1 {
		t.Fatal("saved scenario missing from history")
	}

	srv := httptest.NewServer(svc.handler())
	defer srv.Close()
	client := screeningv1connect.NewScreeningServiceClient(&http.Client{Transport: &signedTransport{base: srv.Client().Transport, secret: secret, user: 19, tenant: tenant, role: "owner"}}, srv.URL)
	calcBody := &screeningv1.CalculateProfitInput{}
	if e = rpccompat.FromLegacy(calcBody, Object{"product_id": focus, "expected_data_revision": 1, "scenarios": []Object{changed}}); e != nil {
		t.Fatal(e)
	}
	if _, e = client.CalculateProfit(ctx, connect.NewRequest(&screeningv1.CalculateProfitRequest{Body: calcBody})); e != nil {
		t.Fatalf("numeric product ID trial via RPC: %v", e)
	}
	changed["fba_fee_usd"] = "7.1234"
	saveBody := &screeningv1.SaveProfitInput{}
	if e = rpccompat.FromLegacy(saveBody, Object{"expected_data_revision": 1, "inputs": changed}); e != nil {
		t.Fatal(e)
	}
	if _, e = client.SaveProfitScenario(ctx, connect.NewRequest(&screeningv1.SaveProfitScenarioRequest{Id: &focus, Body: saveBody, Mutation: &screeningv1.ImportMutationMetadata{IdempotencyKey: "profit-test-save"}})); e != nil {
		t.Fatalf("save via RPC: %v", e)
	}
	value, e := svc.gormProfitProductsUsecase(ctx, a, url.Values{"product_id": {focus}})
	if e != nil {
		t.Fatal(e)
	}
	newest := value.(Object)["items"].([]Object)[0]["draft_inputs"].(map[string]any)
	if newest["fba_fee_usd"] != "7.1234" {
		t.Fatalf("RPC save rounded/lost input %+v", newest)
	}
	expanded, e := svc.gormProfitProductsUsecase(ctx, a, url.Values{"family_product_id": {focus}})
	if e != nil {
		t.Fatal(e)
	}
	if integer(expanded.(Object)["total"]) != 2 {
		t.Fatal("family expansion omitted a retained child")
	}
	if e = validateMutationBody(Object{"product_id": focus}, true); e != nil {
		t.Fatal(e)
	}
	if e = validateMutationBody(Object{"product_id": focus}); e == nil {
		t.Fatal("PostgreSQL mode must retain UUID validation")
	}
	for _, bad := range []string{"0", "-1", "00", "5327x", "18446744073709551616"} {
		if e = validateMutationBody(Object{"product_id": bad}, true); e == nil {
			t.Fatalf("accepted invalid ID %s", bad)
		}
	}
	payload, _ := json.Marshal(after)
	if len(payload) == 0 {
		t.Fatal("empty response")
	}
}
