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

func TestGroupedFamilyReview(t *testing.T) {
	raw := os.Getenv("SCREENING_TEST_DATABASE_URL")
	if raw == "" {
		t.Skip("isolated PostgreSQL required")
	}
	parsed, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(strings.TrimPrefix(parsed.Path, "/"), "_test") {
		t.Fatal("isolated test database required")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(raw)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = "family_review_test"
	cfg.ConnConfig.RuntimeParams["jit"] = "off"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err = pool.Exec(ctx, "CREATE SCHEMA family_review_test"); err != nil {
		t.Fatal(err)
	}
	if err = migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	svc := &service{db: pool}
	a := identity{Tenant: 9911, User: 1, Role: "owner"}
	counter := 0
	add := func(tenant int64, parent, status string, archived bool) string {
		t.Helper()
		counter++
		id := uuid()
		_, e := pool.Exec(ctx, "INSERT INTO ps_products(tenant_id,id,asin,title,parent_asin,review_status,review_reason,archived_at) VALUES($1,$2,$3,'family test',$4,$5,'checked',CASE WHEN $6 THEN now() END)", tenant, id, fmt.Sprintf("B%09d", counter), parent, status, archived)
		if e != nil {
			t.Fatal(e)
		}
		return id
	}
	a1 := add(a.Tenant, "PARENT0001", "pending", false)
	a2 := add(a.Tenant, "PARENT0001", "pending", false)
	kept := add(a.Tenant, "PARENT0001", "kept", false)
	b1 := add(a.Tenant, "PARENT0002", "pending", false)
	b2 := add(a.Tenant, "PARENT0002", "pending", false)
	other := add(9912, "PARENT0001", "pending", false)
	archived := add(a.Tenant, "PARENT0001", "pending", true)
	req := httptest.NewRequest("GET", "http://screening/api/v1/screening/review-workspace", nil).WithContext(context.WithValue(ctx, identityKey{}, a))
	catalog, err := svc.products(req)
	if err != nil {
		t.Fatal(err)
	}
	wantMembers := map[string]bool{a1: true, a2: true, kept: true, b1: true, b2: true}
	gotMembers := map[string]bool{}
	for _, item := range catalog.(Object)["items"].([]Object) {
		ids, ok := item["family_member_ids"].([]any)
		if !ok || int64(len(ids)) != integer(item["family_count"]) {
			t.Fatal("catalog selection must include all live family members", item)
		}
		for _, id := range ids {
			if !wantMembers[str(id)] {
				t.Fatal("selection leaked an archived or other-tenant product", id)
			}
			gotMembers[str(id)] = true
		}
	}
	if len(gotMembers) != len(wantMembers) {
		t.Fatal("collapsed family selection omitted children", gotMembers)
	}
	workspace, err := svc.reviewWorkspace(req)
	if err != nil {
		t.Fatal(err)
	}
	counts := workspace.(Object)["counts"].(Object)
	if integer(counts["pending"]) != 2 || integer(counts["pending_products"]) != 4 {
		t.Fatal("pending must count parent families while retaining child totals", counts)
	}
	// A family spanning two leaf categories must count once at their common ancestor.
	for i, id := range []string{a1, a2, kept, b1, b2} {
		path := "Root:Leaf A"
		if i%2 == 1 {
			path = "Root:Leaf B"
		}
		if _, err := pool.Exec(ctx, "UPDATE ps_products SET category_path=$1 WHERE id=$2", path, id); err != nil {
			t.Fatal(err)
		}
	}
	categoryResult, err := svc.reviewCategories(req)
	if err != nil {
		t.Fatal(err)
	}
	roots := categoryResult.(Object)["items"].([]*reviewCategoryNode)
	if len(roots) != 1 || roots[0].Pending != 2 || roots[0].Products != 5 || len(roots[0].Children) != 2 {
		t.Fatalf("category parent must deduplicate across leaves: %#v", roots)
	}
	items := workspace.(Object)["items"].([]Object)
	if len(items) != 2 {
		t.Fatal("families must be paged as two representatives", workspace)
	}
	members := 0
	for _, item := range items {
		children := item["family_items"].([]Object)
		if len(children) != int(integer(item["family_count"])) {
			t.Fatal("missing initial family members", item)
		}
		members += len(children)
	}
	if members != 5 {
		t.Fatal("wrong visible family member count", members)
	}
	preview := func(ids []any, mode, ref string) (Object, error) {
		body := Object{"product_ids": ids, "scope": "selected_families", "mode": mode, "status": "excluded", "reason": "same parent"}
		if ref != "" {
			body["reference_product_id"] = ref
		}
		actor := a
		actor.RequestKey = uuid()
		actor.RequestHash = shaDigest(encoded(body))
		v, e := svc.previewBatch(ctx, actor, body)
		if e != nil {
			return nil, e
		}
		return v.(Object), nil
	}
	p, err := preview([]any{a1, b1}, "direct", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(p["targets"].([]Object)) != 4 {
		t.Fatal("did not expand both selected families", p)
	}
	result, err := svc.applyBatch(ctx, a, str(p["id"]), false)
	if err != nil || integer(result.(Object)["success"]) != 4 {
		t.Fatal(result, err)
	}
	for _, id := range []string{a1, a2, b1, b2} {
		got, e := svc.product(ctx, a, id)
		if e != nil || got["review_status"] != "excluded" {
			t.Fatal("child not excluded", id, e)
		}
	}
	for _, id := range []string{kept, other, archived} {
		var status string
		if err = pool.QueryRow(ctx, "SELECT review_status FROM ps_products WHERE id=$1", id).Scan(&status); err != nil {
			t.Fatal(err)
		}
		want := "pending"
		if id == kept {
			want = "kept"
		}
		if status != want {
			t.Fatal("review/tenant/archive was overwritten", id, status)
		}
	}
	afterWorkspace, e := svc.reviewWorkspace(req)
	if e != nil {
		t.Fatal(e)
	}
	remaining := afterWorkspace.(Object)
	if integer(remaining["counts"].(Object)["products"]) != 1 || integer(remaining["counts"].(Object)["families"]) != 1 {
		t.Fatal("excluded families still affect counts", remaining)
	}
	for _, item := range remaining["items"].([]Object) {
		if item["review_status"] == "excluded" {
			t.Fatal("excluded representative visible")
		}
		for _, child := range item["family_items"].([]Object) {
			if child["review_status"] == "excluded" {
				t.Fatal("excluded child visible")
			}
		}
	}
	if integer(remaining["samples"].(Object)["total"]) != 5 {
		t.Fatal("excluded reference samples were hidden")
	}
	expandedRequest := httptest.NewRequest("GET", "http://screening/api/v1/screening/review-workspace?family_product_id="+a1, nil).WithContext(context.WithValue(ctx, identityKey{}, a))
	expandedResult, e := svc.reviewWorkspace(expandedRequest)
	if e != nil || integer(expandedResult.(Object)["counts"].(Object)["products"]) != 1 {
		t.Fatal("expanded child pagination includes exclusions", expandedResult, e)
	}

	result, err = svc.applyBatch(ctx, a, str(p["id"]), true)
	if err != nil || integer(result.(Object)["success"]) != 4 {
		t.Fatal("conditional undo failed", result, err)
	}
	if _, err = pool.Exec(ctx, "UPDATE ps_products SET review_status='excluded',review_reason='reference' WHERE id=$1", a1); err != nil {
		t.Fatal(err)
	}
	p, err = preview([]any{b1}, "reference", a1)
	if err != nil || len(p["targets"].([]Object)) != 2 {
		t.Fatal("reference result must resolve before family expansion", p, err)
	}
	if _, err = pool.Exec(ctx, "UPDATE ps_products SET parent_asin='PARENT0003',data_revision=data_revision+1 WHERE id=$1", b1); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.applyBatch(ctx, a, str(p["id"]), false); err == nil {
		t.Fatal("changed anchor must invalidate the preview")
	}
	if _, err = preview([]any{a2}, "reference", kept); err == nil {
		t.Fatal("family automatic scope must be exclusion only")
	}
	first := ""
	for i := 0; i < 101; i++ {
		id := add(a.Tenant, "PARENT0099", "pending", false)
		if i == 0 {
			first = id
		}
	}
	if _, err = preview([]any{first}, "direct", ""); err == nil {
		t.Fatal("expanded batches must respect the 100 child limit")
	}
}
