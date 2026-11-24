package screening

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
)

// Preserve the old library's product-type suggestions; these are not brand registrations.
// A direct classification can supersede the immutable migration snapshot.
const trademarkClassSQL = "COALESCE(NULLIF(p.specs->>'trademark_class',''),p.specs#>>'{legacy_site_snapshot,p,trademark_class}','')"

var trademarkCodePattern = regexp.MustCompile("^(0[1-9]|[12][0-9]|3[0-4])$")
var trademarkLabelPattern = regexp.MustCompile("第([0-9]{2})类")

func trademarkFacets(rows []Object) []Object {
	counts := map[string]int64{}
	for _, row := range rows {
		label := str(row["label"])
		keys := map[string]bool{}
		for _, m := range trademarkLabelPattern.FindAllStringSubmatch(label, -1) {
			if trademarkCodePattern.MatchString(m[1]) {
				keys[m[1]] = true
			}
		}
		if label == "" || strings.Contains(label, "待核实") {
			keys["pending"] = true
		}
		for key := range keys {
			counts[key] += integer(row["count"])
		}
	}
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	items := make([]Object, 0, len(keys))
	for _, key := range keys {
		label := "第" + key + "类"
		if key == "pending" {
			label = "待核实"
		}
		items = append(items, Object{"value": key, "label": label, "count": counts[key]})
	}
	return items
}

func (s *service) trademarkOptions(r *http.Request) (any, error) {
	values := r.URL.Query()
	values.Del("trademark")
	values.Del("sort")
	if e := validateFilters(values); e != nil {
		return nil, e
	}
	q := newProductQuery(actor(r), values)
	rows, e := s.query(r.Context(), "SELECT jsonb_build_object('label',"+trademarkClassSQL+",'count',count(*)) FROM ps_products p "+filterJoins(q, false)+" WHERE "+q.where+" GROUP BY "+trademarkClassSQL, q.args...)
	if e != nil {
		return nil, e
	}
	return Object{"items": trademarkFacets(rows)}, nil
}
