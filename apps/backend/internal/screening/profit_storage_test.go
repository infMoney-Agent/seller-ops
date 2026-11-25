package screening

import "testing"

func TestAutomaticStorageAndFreight(t *testing.T) {
	product := Object{"marketplace": "US", "package_length_cm": "30.48", "package_width_cm": "30.48", "package_height_cm": "30.48", "package_weight_g": "1000", "profit_source": Object{"tier": "大号标准尺寸"}}
	in := profitTableFixture()
	delete(in, "storage_usd")
	delete(in, "freight_cny")
	delete(in, "freight_rate_cny_per_kg")
	delete(in, "volumetric_divisor_cm3_per_kg")
	in["storage_month"] = "2026-11"
	out, err := calculateProductProfit(in, product)
	if err != nil {
		t.Fatal(err)
	}
	if in["freight_rate_cny_per_kg"] != "9" || in["volumetric_divisor_cm3_per_kg"] != "5000" || in["storage_usd"] != "2.400000" || out["storage_source"] != "estimate" {
		t.Fatal(in, out)
	}
	in["storage_days"] = "15"
	out, err = calculateProductProfit(in, product)
	if err != nil || in["storage_usd"] != "1.200000" {
		t.Fatal(in, out, err)
	}
	in["storage_month"] = "2026-02"
	in["storage_days"] = "28"
	_, err = calculateProductProfit(in, product)
	if err != nil || in["storage_usd"] != "0.780000" {
		t.Fatal(in, err)
	}
	product["profit_source"] = Object{"tier": "超大件"}
	_, err = calculateProductProfit(in, product)
	if err != nil || in["storage_usd"] != "0.560000" {
		t.Fatal(in, err)
	}
	product["profit_source"] = Object{"tier": "标准", "storage_usd": "$0.37"}
	out, err = calculateProductProfit(in, product)
	if err != nil || in["storage_usd"] != "0.370000" || out["storage_source"] != "import" {
		t.Fatal(in, out, err)
	}
	in["storage_mode"] = "manual"
	in["storage_usd"] = "0"
	out, err = calculateProductProfit(in, product)
	if err != nil || in["storage_usd"] != "0" || out["storage_source"] != "manual" {
		t.Fatal(in, out, err)
	}
	in["storage_mode"] = "auto"
	product["profit_source"] = Object{}
	delete(product, "package_height_cm")
	out, err = calculateProductProfit(in, product)
	if err != nil || in["storage_usd"] != nil || out["freight_cny"] != nil {
		t.Fatal("missing size should remain incomplete", in, out, err)
	}
	for _, bad := range []Object{{"storage_month": "2026-13"}, {"storage_days": "0"}, {"storage_days": "3651"}, {"storage_mode": "free"}} {
		if validateProfitExtras(bad) == nil {
			t.Fatal("invalid storage config accepted", bad)
		}
	}
}

func TestStorageTierFallbackAndSavedManual(t *testing.T) {
	p := Object{"package_length_cm": "20", "package_width_cm": "10", "package_height_cm": "10", "package_weight_g": "300"}
	in := Object{"storage_mode": "auto", "storage_month": "2026-11", "storage_days": "30"}
	meta := resolveProductStorage(in, p)
	if meta["storage_tier"] != "standard" {
		t.Fatal(meta)
	}
	p["package_weight_g"] = "9100"
	meta = resolveProductStorage(in, p)
	if meta["storage_tier"] != "oversize" {
		t.Fatal(meta)
	}
	in = Object{"freight_cny": "12", "storage_usd": "0.21"}
	profitDefaults(in)
	if in["freight_rate_cny_per_kg"] != nil || in["storage_mode"] != "manual" || in["storage_usd"] != "0.21" {
		t.Fatal("existing manual values changed", in)
	}
}

func TestDerivedFreightWithIncompleteProfit(t *testing.T) {
	p := Object{"package_length_cm": "20", "package_width_cm": "10", "package_height_cm": "10", "package_weight_g": "300"}
	in := Object{"name": "test"}
	out, err := calculateProductProfit(in, p)
	if err != nil || out["state"] != "incomplete" || out["freight_cny"] != "3.600000" {
		t.Fatal(out, err)
	}
	in["freight_rate_cny_per_kg"] = "10"
	out, err = calculateProductProfit(in, p)
	if err != nil || out["freight_cny"] != "4.000000" {
		t.Fatal(out, err)
	}
}
