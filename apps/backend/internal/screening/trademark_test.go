package screening

import (
	"net/url"
	"strings"
	"testing"
)

func TestTrademarkFilters(t *testing.T) {
	for _, v := range []string{"01", "21", "34", "pending", ""} {
		if e := validateFilters(url.Values{"trademark": {v}}); e != nil {
			t.Fatal(v, e)
		}
	}
	for _, v := range []string{"00", "35", "21 OR 1=1", "1", "%"} {
		if e := validateFilters(url.Values{"trademark": {v}}); e == nil {
			t.Fatal("accepted", v)
		}
	}
	q := newProductQuery(identity{Tenant: 7}, url.Values{"trademark": {"21"}})
	if !strings.Contains(q.where, "strpos(") || len(q.args) != 2 || q.args[0] != int64(7) || q.args[1] != "第21类" {
		t.Fatal(q)
	}
	pending := newProductQuery(identity{Tenant: 7}, url.Values{"trademark": {"pending"}})
	if !strings.Contains(pending.where, "待核实") || !strings.Contains(pending.where, "='' OR") {
		t.Fatal(pending)
	}
}
func TestTrademarkFacets(t *testing.T) {
	rows := []Object{{"label": "第21类（初判）", "count": int64(4)}, {"label": "多类待核实（第08类／第21类／第21类）", "count": int64(2)}, {"label": "", "count": int64(3)}}
	out := trademarkFacets(rows)
	if len(out) != 3 || out[0]["value"] != "08" || out[0]["count"] != int64(2) || out[1]["count"] != int64(6) || out[2]["value"] != "pending" || out[2]["count"] != int64(5) {
		t.Fatal(out)
	}
}
