package screening

import (
	"math/big"
	"testing"
)

func TestReferralCategoriesAndPriceBrackets(t *testing.T) {
	for _, test := range []struct{ category, price, rate, fee string }{{"beauty", "10", "0.080000", "0.80"}, {"beauty", "10.01", "0.150000", "1.50"}, {"clothing", "15", "0.050000", "0.75"}, {"clothing", "20", "0.100000", "2.00"}, {"clothing", "20.01", "0.170000", "3.40"}, {"furniture", "300", "0.133333", "40.00"}, {"home", "1", "0.150000", "0.30"}, {"art", "1", "0.200000", "1.00"}, {"grocery", "1", "0.080000", "0.08"}, {"watches", "2000", "0.127500", "255.00"}} {
		price, _ := new(big.Rat).SetString(test.price)
		rate, fee := referralFeeFor(referralRules[test.category], price)
		if rate.FloatString(6) != test.rate || fee.FloatString(2) != test.fee {
			t.Fatalf("%s %s: %s %s", test.category, test.price, rate.FloatString(6), fee.FloatString(2))
		}
	}
	for _, test := range []struct{ path, want string }{{"Home & Kitchen > Furniture > Mattresses", "mattress"}, {"Health & Household > Household Supplies", "beauty"}, {"Clothing, Shoes & Jewelry > Shoes", "shoes"}, {"Office Products", "office"}, {"Unrecognized category", ""}} {
		if got := inferReferral(Object{"category_path": test.path}); got != test.want {
			t.Fatalf("%s = %s want %s", test.path, got, test.want)
		}
	}
}
func TestAutomaticReferralProfitAndManualOverride(t *testing.T) {
	p := Object{"category_path": "Home & Kitchen > Furniture"}
	in := profitTableFixture()
	in["commission_mode"] = "auto"
	in["sale_price_usd"] = "300"
	in["freight_rate_cny_per_kg"] = nil
	in["freight_cny"] = "0"
	in["fba_fee_usd"] = "0"
	in["storage_usd"] = "0"
	in["ad_rate"] = "0"
	in["purchase_cny"] = "0"
	out, err := calculateProductProfit(in, p)
	if err != nil || out["commission_usd"] != "40.00" || out["profit_usd"] != "260.00" {
		t.Fatal(out, err)
	}
	in["commission_mode"] = "manual"
	in["commission_rate"] = "0.10"
	out, err = calculateProductProfit(in, p)
	if err != nil || out["commission_usd"] != "30.00" {
		t.Fatal(out, err)
	}
	in["commission_mode"] = "auto"
	in["commission_category"] = "home"
	in["sale_price_usd"] = "1"
	out, err = calculateProductProfit(in, p)
	if err != nil || out["commission_usd"] != "0.30" || out["profit_usd"] != "0.70" {
		t.Fatal(out, err)
	}
	in["commission_category"] = ""
	p["category_path"] = "Unknown"
	out, err = calculateProductProfit(in, p)
	if err != nil || out["state"] != "incomplete" || in["commission_rate"] != nil {
		t.Fatal("unknown category must not silently default", out, err)
	}
	if validateProfitExtras(Object{"commission_category": "invalid"}) == nil || validateProfitExtras(Object{"commission_mode": "invalid"}) == nil {
		t.Fatal("invalid config accepted")
	}
}
func TestReferralBreakEvenTierAndMinimum(t *testing.T) {
	for _, test := range []struct {
		category string
		fixed    *big.Rat
		want     string
	}{{"home", big.NewRat(1, 100), "0.31"}, {"furniture", big.NewRat(250, 1), "288.89"}, {"clothing", big.NewRat(15, 1), "16.67"}} {
		p := referralBreakEven(referralRules[test.category], test.fixed, big.NewRat(0, 1))
		if p == nil || p.FloatString(2) != test.want {
			t.Fatal(test.category, p, test.want)
		}
	}
}
