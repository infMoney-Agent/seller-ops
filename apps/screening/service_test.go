package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDatabaseIsolation(t *testing.T) {
	for _, u := range []string{"sqlite:///data.db", "postgres://u@db/seller_ops", "postgres://u@db/casdoor", "postgres://u@db/"} {
		if validateDatabase(u, "", "") == nil {
			t.Fatalf("accepted %s", u)
		}
	}
	if validateDatabase("postgres://u@db/seller_screening", "postgres://auth@db/seller_screening", "") == nil {
		t.Fatal("shared database accepted")
	}
	if e := validateDatabase("postgres://u@db/seller_screening", "postgres://auth@db/seller_ops", ""); e != nil {
		t.Fatal(e)
	}
}
func TestSignedIdentity(t *testing.T) {
	now := time.Unix(1700000000, 0)
	secret := strings.Repeat("s", 32)
	r := httptest.NewRequest("GET", "http://screening/api/v1/screening/products?page=2", nil)
	r.Header.Set("x-seller-user", "7")
	r.Header.Set("x-seller-workspace", "9")
	r.Header.Set("x-seller-role", "owner")
	stamp := fmt.Sprint(now.Unix())
	r.Header.Set("x-screening-time", stamp)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(strings.Join([]string{stamp, "GET", r.URL.RequestURI(), "7", "9", "owner"}, "\n")))
	r.Header.Set("x-screening-signature", hex.EncodeToString(mac.Sum(nil)))
	a, e := verifyIdentity(r, secret, now)
	if e != nil || a.Tenant != 9 || a.User != 7 {
		t.Fatalf("valid request: %v", e)
	}
	r.Header.Set("x-seller-workspace", "10")
	if _, e = verifyIdentity(r, secret, now); e == nil {
		t.Fatal("forged workspace accepted")
	}
	r.Header.Set("x-seller-workspace", "9")
	r.URL.RawQuery = "page=3"
	if _, e = verifyIdentity(r, secret, now); e == nil {
		t.Fatal("changed query accepted")
	}
	r.URL.RawQuery = "page=2"
	if _, e = verifyIdentity(r, secret, now.Add(61*time.Second)); e == nil {
		t.Fatal("expired signature accepted")
	}
}
func baseInputs() Object {
	return Object{"sale_price_usd": "19.99", "discount_usd": "0", "purchase_cny": "18", "packaging_cny": "2", "freight_cny": "8", "other_landed_cny": "0", "fx_cny_per_usd": "7.20", "commission_rate": "0.15", "fba_fee_usd": "4.00", "storage_usd": "0.10", "ad_rate": "0.10", "return_rate": "0.03", "loss_per_return_usd": "12", "other_usd": "0", "monthly_units": 100}
}
func TestProfitExact(t *testing.T) {
	out, e := calculate(baseInputs())
	if e != nil {
		t.Fatal(e)
	}
	if out["profit_usd"] != "6.64" || out["monthly_profit_usd"] != "664.36" || out["margin"] != "0.332347" {
		t.Fatalf("unexpected output: %#v", out)
	}
}
func TestProfitBoundaries(t *testing.T) {
	in := baseInputs()
	delete(in, "purchase_cny")
	out, e := calculate(in)
	if e != nil || out["state"] != "incomplete" {
		t.Fatal(out, e)
	}
	in = baseInputs()
	in["commission_rate"] = "1.1"
	if _, e = calculate(in); e == nil {
		t.Fatal("invalid rate accepted")
	}
	in = baseInputs()
	in["purchase_cny"] = "0"
	in["packaging_cny"] = "0"
	in["freight_cny"] = "0"
	in["commission_rate"] = "1"
	out, e = calculate(in)
	if e != nil || out["roi"] != nil || out["break_even_price_usd"] != nil {
		t.Fatal(out, e)
	}
	in = baseInputs()
	in["fx_cny_per_usd"] = "0"
	if _, e = calculate(in); e == nil {
		t.Fatal("zero FX accepted")
	}
	in = baseInputs()
	in["discount_usd"] = "20"
	if _, e = calculate(in); e == nil {
		t.Fatal("negative net price accepted")
	}
}
func TestRuleBoundaries(t *testing.T) {
	cfg := Object{"exclusion_patterns": Object{"电器或电池": `\belectric\b|battery|rechargeable`, "化学制剂或含配方套装": `\bsoap\b`}}
	if e := validatePolicy(cfg); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ title, want string }{{"vacuum storage bags with hand pump", "passed"}, {"vacuum storage bags with electric pump", "excluded"}, {"vacuum storage bags with pump", "insufficient"}, {"non-electric storage bags with manual pump", "passed"}, {"empty soap dispenser", "passed"}, {"electric toothbrush replacement heads", "passed"}} {
		out := classify(Object{"asin": "B000000001", "title": tc.title}, cfg)
		if out["decision"] != tc.want {
			t.Errorf("%s: %v", tc.title, out)
		}
	}
	if e := validatePolicy(Object{"exclusion_patterns": Object{"invalid": "("}}); e == nil {
		t.Fatal("unsupported expression accepted")
	}
}

func TestDefaultPolicyRegression(t *testing.T) {
	config := decodeObject(defaultPolicyData)
	if e := validatePolicy(config); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct{ title, want string }{{"220V Electric Aquarium Air Pump", "excluded"}, {"Replacement filter bags compatible with electric vacuum", "passed"}, {"Grow light stand bracket only", "passed"}, {"Pool chlorine tablets", "excluded"}, {"Empty chlorine tablet dispenser", "passed"}, {"Unscented cotton cloth", "passed"}, {"Medical first aid gel", "excluded"}} {
		result := classify(Object{"asin": "B000000001", "title": tc.title}, config)
		if result["decision"] != tc.want {
			t.Errorf("%s: %#v", tc.title, result)
		}
	}
}
