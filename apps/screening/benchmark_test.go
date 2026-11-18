package main

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"math"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"testing"
	"time"
)

// Reuses explicitly designated synthetic fixtures; never seeds or mutates a
// running business database. A release must record the environment and counts.
func TestLargeCatalogPagination(t *testing.T) {
	raw := os.Getenv("SCREENING_BENCH_DATABASE_URL")
	if raw == "" {
		t.Skip("set SCREENING_BENCH_DATABASE_URL to the synthetic *_test fixture")
	}
	if !strings.Contains(raw, "_test") {
		t.Fatal("test database required")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, raw)
	if e != nil {
		t.Fatal(e)
	}
	defer pool.Close()
	a := identity{Tenant: 1900000001, User: 1, Role: "owner"}
	var products, observations int64
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM ps_products WHERE tenant_id=$1", a.Tenant).Scan(&products); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, "SELECT count(*) FROM ps_import_rows WHERE tenant_id=$1", a.Tenant).Scan(&observations); e != nil {
		t.Fatal(e)
	}
	if products < 100000 || observations < 500000 {
		t.Fatal("benchmark fixture is smaller than acceptance target")
	}
	s := &service{db: pool}
	for _, query := range []string{"page=1", "page=20&category=Trays", "page=1&stat_month=2026-01-01&period_type=calendar_month"} {
		samples := []time.Duration{}
		for i := 0; i < 23; i++ {
			r := httptest.NewRequest("GET", "http://screening/api/v1/screening/products?"+query, nil)
			r = r.WithContext(context.WithValue(ctx, identityKey{}, a))
			start := time.Now()
			result, e := s.products(r)
			if e != nil {
				t.Fatal(query, e)
			}
			if len(result.(Object)["items"].([]Object)) == 0 {
				t.Fatal("empty benchmark page")
			}
			if i >= 3 {
				samples = append(samples, time.Since(start))
			}
		}
		sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
		p95 := samples[int(math.Ceil(float64(len(samples))*.95))-1]
		t.Logf("products=%d observations=%d query=%s P95=%s", products, observations, query, p95)
		if p95 > time.Second {
			t.Errorf("pagination exceeded 1 second target: %s", p95)
		}
	}
}
