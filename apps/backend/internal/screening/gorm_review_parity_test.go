package screening

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/url"
	"os"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/rpccompat"
	"testing"
	"time"
)

func reviewObjectRows(value any) []Object {
	if rows, ok := value.([]Object); ok {
		return rows
	}
	out := []Object{}
	if rows, ok := value.([]any); ok {
		for _, v := range rows {
			out = append(out, v.(map[string]any))
		}
	}
	return out
}

func TestGORMReviewParityReadOnly(t *testing.T) {
	if os.Getenv("SCREENING_FIDELITY_READONLY_AUDIT") != "1" {
		t.Skip("explicit read-only audit only")
	}
	ctx := context.Background()
	r, err := NewGORMRuntime(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	pool, err := pgxpool.New(ctx, os.Getenv("SCREENING_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	old := &service{db: pool}
	a := identity{Tenant: 4, User: 19, Role: "owner"}
	check := func(label string, oq, nq url.Values) (Object, Object) {
		t.Helper()
		start := time.Now()
		left, err := old.reviewWorkspaceUsecase(ctx, a, oq)
		if err != nil {
			t.Fatalf("%s source %v", label, err)
		}
		right, err := r.service.reviewWorkspaceUsecase(ctx, a, nq)
		if err != nil {
			t.Fatalf("%s native %v", label, err)
		}
		want, got := left.(Object), right.(Object)
		if err = rpccompat.FromLegacy(&screeningv1.GetReviewWorkspaceResponse{}, Object{"data": publicJSON(got)}); err != nil {
			t.Fatalf("%s schema %v", label, err)
		}
		wc, gc := want["counts"].(map[string]any), got["counts"].(Object)
		for _, key := range []string{"products", "families", "pending", "pending_products", "groups"} {
			if integer(wc[key]) != integer(gc[key]) {
				t.Fatalf("%s counts %s got %v want %v", label, key, gc[key], wc[key])
			}
		}
		ws, gs := want["samples"].(Object), got["samples"].(Object)
		for _, key := range []string{"total", "page", "page_size"} {
			if integer(ws[key]) != integer(gs[key]) {
				t.Fatalf("%s sample %s got %v want %v", label, key, gs[key], ws[key])
			}
		}
		if integer(want["page"]) != integer(got["page"]) {
			t.Fatalf("%s page mismatch", label)
		}
		if want["group"] != nil {
			wg, gg := want["group"].(Object), got["group"].(Object)
			for _, key := range []string{"id", "title", "marketplace", "products", "display_products", "pending", "reviewed", "samples"} {
				if str(wg[key]) != str(gg[key]) {
					t.Fatalf("%s group %s got %v want %v", label, key, gg[key], wg[key])
				}
			}
		} else if got["group"] != nil {
			t.Fatalf("%s empty group fabricated", label)
		}
		for _, pair := range []struct {
			name        string
			left, right []Object
		}{{"candidates", reviewObjectRows(want["items"]), reviewObjectRows(got["items"])}, {"samples", reviewObjectRows(ws["items"]), reviewObjectRows(gs["items"])}} {
			if len(pair.left) != len(pair.right) {
				t.Fatalf("%s %s page lengths %d/%d", label, pair.name, len(pair.right), len(pair.left))
			}
			for i, w := range pair.left {
				g := pair.right[i]
				for _, key := range []string{"asin", "parent_asin", "review_status", "stat_month", "period_type", "source_type", "display_child_sales"} {
					if str(w[key]) != str(g[key]) {
						t.Fatalf("%s %s row %d %s got %v want %v (ASIN %v/%v)", label, pair.name, i, key, g[key], w[key], g["asin"], w["asin"])
					}
				}
				if pair.name == "candidates" {
					if integer(w["family_count"]) != integer(g["family_count"]) {
						t.Fatalf("%s family count differs for %v", label, w["asin"])
					}
					wi, gi := reviewObjectRows(w["family_items"]), reviewObjectRows(g["family_items"])
					if len(wi) != len(gi) {
						t.Fatalf("%s preload differs", label)
					}
					for j := range wi {
						if wi[j]["asin"] != gi[j]["asin"] {
							t.Fatalf("%s preload order differs", label)
						}
					}
				}
			}
		}
		t.Logf("%s: products=%v families=%v pending=%v samples=%v duration=%s", label, gc["products"], gc["families"], gc["pending"], gs["total"], time.Since(start).Round(time.Millisecond))
		return want, got
	}
	oq, nq := url.Values{}, url.Values{}
	firstOld, firstNative := check("default", oq, nq)
	cases := []struct {
		name string
		q    url.Values
	}{{"second pages", url.Values{"member_page": {"2"}, "sample_page": {"2"}}}, {"clamped pages", url.Values{"member_page": {"999"}, "sample_page": {"999"}}}, {"machine passed", url.Values{"machine": {"passed"}}}, {"machine excluded", url.Values{"machine": {"excluded"}}}, {"machine insufficient", url.Values{"machine": {"insufficient"}}}, {"machine pending", url.Values{"machine": {"pending"}}}, {"historical month", url.Values{"stat_month": {"2026-09-01"}}}, {"minimum child sales", url.Values{"child_sales_min": {"100"}}}, {"trademark", url.Values{"trademark": {"20"}}}, {"has reference", url.Values{"has_reference": {"true"}}}}
	for _, tc := range cases {
		check(tc.name, tc.q, tc.q)
	}
	before, err := old.reviewCategoriesUsecase(ctx, a, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	after, err := r.service.reviewCategoriesUsecase(ctx, a, url.Values{})
	if err != nil {
		t.Fatal(err)
	}
	left, _ := json.Marshal(before)
	right, _ := json.Marshal(after)
	if string(left) != string(right) {
		t.Fatal("category tree differs from source")
	}
	nodes := after.(Object)["items"].([]*reviewCategoryNode)
	if len(nodes) > 0 {
		path, _ := json.Marshal(nodes[0].Path)
		q := url.Values{"category_path": {string(path)}}
		check("category prefix", q, q)
		for len(nodes[0].Children) > 0 {
			nodes = nodes[0].Children
		}
		path, _ = json.Marshal(nodes[0].Path)
		q = url.Values{"category_path": {string(path)}}
		check("category leaf", q, q)
	}
	oldRows, newRows := reviewObjectRows(firstOld["items"]), reviewObjectRows(firstNative["items"])
	oldIDs, newIDs := []string{}, []string{}
	for i := 0; i < 3 && i < len(oldRows); i++ {
		oldIDs = append(oldIDs, str(oldRows[i]["id"]))
		newIDs = append(newIDs, str(newRows[i]["id"]))
	}
	check("selected IDs", url.Values{"selected_ids": {joinReviewIDs(oldIDs)}}, url.Values{"selected_ids": {joinReviewIDs(newIDs)}})
	for i, w := range oldRows {
		if integer(w["family_count"]) > 1 {
			check("family expansion", url.Values{"family_product_id": {str(w["id"])}}, url.Values{"family_product_id": {str(newRows[i]["id"])}})
			break
		}
	}
	if path := os.Getenv("SCREENING_REVIEW_REPORT"); path != "" {
		body := Object{"counts": firstNative["counts"], "group": firstNative["group"], "samples_total": firstNative["samples"].(Object)["total"], "cases_verified": len(cases) + 5}
		raw, _ := json.MarshalIndent(body, "", "  ")
		if err = os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
func joinReviewIDs(ids []string) string {
	out := ""
	for i, id := range ids {
		if i > 0 {
			out += ","
		}
		out += fmt.Sprint(id)
	}
	return out
}
