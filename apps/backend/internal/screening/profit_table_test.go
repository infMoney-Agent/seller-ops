package screening

import (
	"context"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

func profitTableFixture() Object {
	in := Object{"name": "table test", "fee_source": "fixture", "note": "", "monthly_units": 100, "supplier_url": "https://example.com/supply", "freight_rate_cny_per_kg": "9", "volumetric_divisor_cm3_per_kg": "5000"}
	for _, key := range profitFields {
		in[key] = "0"
	}
	in["sale_price_usd"] = "100"
	in["purchase_cny"] = "70"
	in["fx_cny_per_usd"] = "7"
	in["commission_rate"] = "0.15"
	in["ad_rate"] = "0.10"
	in["fba_fee_usd"] = "4"
	in["storage_usd"] = "0.10"
	return in
}
func TestProfitTableFreight(t *testing.T) {
	p := Object{"package_length_cm": "20", "package_width_cm": "10", "package_height_cm": "10", "package_weight_g": "300"}
	in := profitTableFixture()
	out, err := calculateProductProfit(in, p)
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"volumetric_weight_kg": "0.400000", "billable_weight_kg": "0.400000", "freight_cny": "3.600000", "profit_usd": "60.39", "profit_cny": "422.70"} {
		if str(out[key]) != want {
			t.Fatalf("%s: got %v want %s", key, out[key], want)
		}
	}
	// CNY must be calculated before USD display rounding (60.39 * 7 = 422.73).
	p["package_weight_g"] = "600"
	out, err = calculateProductProfit(profitTableFixture(), p)
	if err != nil || out["billable_weight_kg"] != "0.600000" {
		t.Fatal("actual weight should win", out, err)
	}
	delete(p, "package_weight_g")
	in = profitTableFixture()
	in["freight_cny"] = "999"
	out, err = calculateProductProfit(in, p)
	if err != nil || out["state"] != "incomplete" || out["profit_cny"] != nil || in["freight_cny"] != nil {
		t.Fatal("missing package facts must not reuse stale freight or become zero", out, err)
	}
	in = profitTableFixture()
	in["supplier_url"] = "javascript:alert(1)"
	if _, err = calculateProductProfit(in, p); err == nil {
		t.Fatal("unsafe supplier URL accepted")
	}
	in = profitTableFixture()
	in["volumetric_divisor_cm3_per_kg"] = "0"
	if _, err = calculateProductProfit(in, p); err == nil {
		t.Fatal("zero divisor accepted")
	}
	in = profitTableFixture()
	in["package_weight_g"] = "1"
	if _, err = calculateProductProfit(in, p); err == nil {
		t.Fatal("client measurement override accepted")
	}
}

func TestProfitTableQueries(t *testing.T) {
	raw := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("isolated PostgreSQL required")
	}
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(u.Path, "/"), "_test") {
		t.Fatal("isolated test database required")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "profit_table_test"
	cfg.ConnConfig.RuntimeParams["jit"] = "off"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, "CREATE SCHEMA profit_table_test"); err != nil {
		t.Fatal(err)
	}
	if err = migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	svc := &service{db: pool}
	actor := identity{Tenant: 9821, User: 1, Role: "owner"}
	counter := 0
	add := func(tenant int64, status string) string {
		t.Helper()
		counter++
		id := uuid()
		_, e := pool.Exec(ctx, "INSERT INTO ps_products(tenant_id,id,asin,title,parent_asin,review_status,review_reason,data_revision,package_length_cm,package_width_cm,package_height_cm,package_weight_g) VALUES($1,$2,$3,'profit table','PARENT0100',$4,'fixture',1,20,10,10,300)", tenant, id, fmt.Sprintf("T%09d", counter), status)
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	first, second, missing := add(actor.Tenant, "kept"), add(actor.Tenant, "kept"), add(actor.Tenant, "kept")
	pending := add(actor.Tenant, "pending")
	add(actor.Tenant, "excluded")
	other := add(9822, "kept")
	save := func(id, purchase string) Object {
		t.Helper()
		in := profitTableFixture()
		in["purchase_cny"] = purchase
		a := actor
		a.RequestKey = uuid()
		a.RequestHash = shaDigest(encoded(in))
		v, e := svc.saveProfit(ctx, a, id, Object{"expected_data_revision": 1, "inputs": in})
		if e != nil {
			t.Fatal(e)
		}
		return v.(Object)
	}
	if _, err := pool.Exec(ctx, "UPDATE ps_products SET specs=$1 WHERE tenant_id=$2 AND id=$3::uuid", encoded(Object{"material": "steel", "legacy_site_snapshot": Object{"raw": "must stay in database"}}), actor.Tenant, first); err != nil {
		t.Fatal(err)
	}
	old := save(first, "70")
	save(second, "7")
	list := func(query string) Object {
		t.Helper()
		r := httptest.NewRequest("GET", "http://screening/api/v1/screening/profit/products?"+query, nil).WithContext(context.WithValue(ctx, identityKey{}, actor))
		v, e := svc.profitProducts(r)
		if e != nil {
			t.Fatal(e)
		}
		return v.(Object)
	}
	for page, id := range []string{second, first, missing} {
		v := list(fmt.Sprintf("sort=-profit_cny&page=%d&page_size=1", page+1))
		if integer(v["total"]) != 3 || len(v["items"].([]Object)) != 1 || str(v["items"].([]Object)[0]["id"]) != id {
			t.Fatal("must sort all child ASINs before pagination and leave missing results last", v)
		}
	}
	row := list("product_id=" + first)["items"].([]Object)[0]
	specs := row["specs"].(map[string]any)
	if specs["material"] != "steel" || specs["legacy_site_snapshot"] != nil {
		t.Fatal("list leaked raw source or removed actual specifications", specs)
	}
	var sourcePreserved bool
	if err := pool.QueryRow(ctx, "SELECT specs ? 'legacy_site_snapshot' FROM ps_products WHERE tenant_id=$1 AND id=$2::uuid", actor.Tenant, first).Scan(&sourcePreserved); err != nil || !sourcePreserved {
		t.Fatal("source history was modified", err)
	}
	if integer(list("product_id=" + other)["total"]) != 0 {
		t.Fatal("tenant leaked")
	}
	if integer(list("product_id=" + pending)["total"]) != 1 {
		t.Fatal("explicit catalog selection must remain calculable")
	}
	latest := save(first, "60")
	var count int
	var supplier string
	if err = pool.QueryRow(ctx, "SELECT count(*),max(inputs->>'supplier_url') FROM ps_profit_scenarios WHERE tenant_id=$1 AND product_id=$2", actor.Tenant, first).Scan(&count, &supplier); err != nil || count != 2 || supplier != "https://example.com/supply" {
		t.Fatal("snapshot history or supplier missing", count, supplier, err)
	}
	v := list("product_id=" + first)["items"].([]Object)[0]
	if v["scenario"].(map[string]any)["id"] != latest["id"] || latest["id"] == old["id"] {
		t.Fatal("latest saved scenario not selected", v)
	}
}
