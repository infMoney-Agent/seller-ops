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

func TestReviewWorkspace(t *testing.T) {
	raw := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("test PostgreSQL required")
	}
	if !strings.Contains(raw, "_test") {
		t.Fatal("isolated database required")
	}
	ctx := context.Background()
	cfg, e := pgxpool.ParseConfig(raw)
	if e != nil {
		t.Fatal(e)
	}
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer admin.Close()
	if _, e = admin.Exec(ctx, "CREATE SCHEMA review_workspace_test"); e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "review_workspace_test"
	db, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if e = migrate(ctx, db); e != nil {
		t.Fatal(e)
	}
	n := 0
	add := func(tenant int64, direction, status string) string {
		t.Helper()
		n++
		id := uuid()
		_, e := db.Exec(ctx, "INSERT INTO ps_products(tenant_id,id,asin,title,machine_direction,review_status,review_reason,seller_id) VALUES($1,$2,$3,'test tool',NULLIF($4,''),$5,'checked',$6)", tenant, id, fmt.Sprintf("B%09d", n), direction, status, fmt.Sprintf("SELLER%04d", n))
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	ids := []string{}
	for i := 0; i < 51; i++ {
		ids = append(ids, add(801, "工具 / 金属 / 同类型", "pending"))
	}
	for i := 0; i < 51; i++ {
		add(801, "工具 / 金属 / 同类型", "kept")
	}
	for i := 0; i < 20; i++ {
		add(801, fmt.Sprintf("其他类型%02d", i), "pending")
	}
	add(801, "", "pending")
	add(801, "", "pending")
	otherTenant := add(802, "工具 / 金属 / 同类型", "excluded")
	add(801, "其他独立样本", "excluded")
	svc := &service{db: db}
	call := func(values url.Values) Object {
		t.Helper()
		r := httptest.NewRequest("GET", "http://screening/api/v1/screening/review-workspace?"+values.Encode(), nil).WithContext(context.WithValue(ctx, identityKey{}, identity{Tenant: 801, User: 1, Role: "member"}))
		v, e := svc.reviewWorkspace(r)
		if e != nil {
			t.Fatal(e)
		}
		return v.(Object)
	}
	out := call(url.Values{"review_status": {"pending"}})
	groups := out["groups"].(Object)
	g := out["group"].(Object)
	if integer(groups["total"]) != 0 || integer(g["products"]) != 73 || integer(g["samples"]) != 52 || len(out["items"].([]Object)) != 50 {
		t.Fatal(out)
	}
	samples := out["samples"].(Object)
	if integer(samples["total"]) != 52 || len(samples["items"].([]Object)) != 50 {
		t.Fatal(samples)
	}
	key := str(g["id"])
	next := call(url.Values{"review_status": {"pending"}, "group_id": {key}, "member_page": {"2"}, "sample_page": {"2"}})
	if len(next["items"].([]Object)) != 23 || len(next["samples"].(Object)["items"].([]Object)) != 2 {
		t.Fatal(next)
	}
	last := call(url.Values{"review_status": {"pending"}, "group_page": {"2"}})
	if len(last["groups"].(Object)["items"].([]Object)) != 0 {
		t.Fatal(last)
	}
	subset := call(url.Values{"selected_ids": {strings.Join(ids[:2], ",")}, "review_status": {"pending"}})
	if integer(subset["counts"].(Object)["products"]) != 2 || integer(subset["group"].(Object)["samples"]) != 52 {
		t.Fatal(subset)
	}
	isolated := call(url.Values{"selected_ids": {otherTenant}})
	if isolated["group"] != nil || integer(isolated["counts"].(Object)["products"]) != 0 {
		t.Fatal("tenant leaked", isolated)
	}
	// Independently unclassified products must not become one enormous type group.
	if integer(last["groups"].(Object)["total"]) != 0 {
		t.Fatal(last)
	}
	mixed := add(801, "不同用途 / 不同材质", "pending")
	if _, e = db.Exec(ctx, "UPDATE ps_products SET category_path='Home & Kitchen:Kitchen & Dining:Storage:Hooks:Wall Hooks' WHERE tenant_id=801 AND id=$1", mixed); e != nil {
		t.Fatal(e)
	}
	// Real category paths retain every level; selecting a parent includes descendants.
	if _, e = db.Exec(ctx, "UPDATE ps_products SET category_path='Home & Kitchen:Kitchen & Dining:Storage:Hooks:Wall Hooks' WHERE tenant_id=801 AND id=ANY($1::uuid[])", ids[:2]); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, "UPDATE ps_products SET category_path='Home & Kitchen:Bath:Hooks' WHERE tenant_id=801 AND id=$1", ids[2]); e != nil {
		t.Fatal(e)
	}
	req := httptest.NewRequest("GET", "http://screening/api/v1/screening/review-categories", nil).WithContext(context.WithValue(ctx, identityKey{}, identity{Tenant: 801, User: 1, Role: "member"}))
	tree, e := svc.reviewCategories(req)
	if e != nil {
		t.Fatal(e)
	}
	var kitchen *reviewCategoryNode
	for _, node := range tree.(Object)["items"].([]*reviewCategoryNode) {
		if node.Name == "Home & Kitchen" {
			kitchen = node
		}
	}
	if kitchen == nil || kitchen.Products != 4 || len(kitchen.Children) != 2 {
		t.Fatal("invalid tree", tree)
	}
	branch := call(url.Values{"category_path": {string(encoded([]string{"Home & Kitchen", "Kitchen & Dining"}))}})
	if integer(branch["counts"].(Object)["products"]) != 3 || len(branch["items"].([]Object)) != 3 {
		t.Fatal("descendant filter failed", branch)
	}
	leaf := call(url.Values{"category_path": {string(encoded([]string{"Home & Kitchen", "Kitchen & Dining", "Storage", "Hooks", "Wall Hooks"}))}})
	if integer(leaf["counts"].(Object)["products"]) != 3 || len(leaf["items"].([]Object)) != 3 {
		t.Fatal("deep category lost", leaf)
	}

}
