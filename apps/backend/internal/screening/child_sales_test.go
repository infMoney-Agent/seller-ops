package screening

import "testing"

func TestChildSalesKinds(t *testing.T) {
	for _, tc := range []struct {
		raw   string
		kind  string
		value int64
	}{{"50,000+", "lower_bound", 50000}, {"10000", "lower_bound", 10000}, {"0", "lower_bound", 0}, {"—", "missing", 0}} {
		n, err := normalizeRow(Object{"ASIN": "B000000001", "商品标题": "manual tool", "父ASIN": "PARENT0001", "月销量": "68329", "子体销量": tc.raw}, Object{})
		if len(err) > 0 || n["sales_quantity_kind"] != tc.kind {
			t.Fatal(tc, n, err)
		}
		if tc.kind != "missing" && integer(n["monthly_sales"]) != tc.value {
			t.Fatal(n)
		}
	}
}
func TestChildSalesThreshold(t *testing.T) {
	for _, tc := range []struct {
		scope, kind string
		sales       any
		want        string
	}{{"child", "lower_bound", 1000, "insufficient"}, {"child", "lower_bound", 2000, "passed"}, {"unknown", "missing", 9000, "insufficient"}, {"child", "reported", 1000, "excluded"}, {"child", "single_variant_estimate", 1000, "insufficient"}} {
		p := Object{"asin": "B000000001", "title": "manual tool", "sales_scope": tc.scope, "sales_quantity_kind": tc.kind, "monthly_sales": tc.sales}
		out := classify(p, Object{"child_sales_min": 1500})
		if out["decision"] != tc.want {
			t.Fatal(tc, out)
		}
	}
}
