package screening

import (
	"fmt"
	"math/big"
	"net/url"
	"strings"
)

var freightFields = []string{"freight_rate_cny_per_kg", "volumetric_divisor_cm3_per_kg"}

func validateProfitExtras(in Object) error {
	if err := validateReferralInputs(in); err != nil {
		return err
	}
	if err := validStorageInputs(in); err != nil {
		return err
	}
	for _, key := range freightFields {
		if in[key] == nil || str(in[key]) == "" {
			continue
		}
		raw := str(in[key])
		n, ok := new(big.Rat).SetString(raw)
		if !decimalPattern.MatchString(raw) || !ok || n.Sign() < 0 || n.Cmp(big.NewRat(999999999999, 1)) > 0 {
			return fmt.Errorf("%s 数值无效", key)
		}
		if key == "volumetric_divisor_cm3_per_kg" && n.Sign() == 0 {
			return fmt.Errorf("体积重分母必须大于零")
		}
	}
	if raw := in["supplier_url"]; raw != nil && str(raw) != "" {
		text, ok := raw.(string)
		if !ok || len(text) > 2000 {
			return fmt.Errorf("供应商链接无效")
		}
		u, err := url.Parse(text)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			return fmt.Errorf("供应商链接须为有效的 HTTP/HTTPS 地址")
		}
	}
	return nil
}

// All freight arithmetic uses exact rationals. Measurements come from the
// version-checked product, not arbitrary dimensions supplied by the browser.
func calculateProductProfit(in, p Object) (Object, error) {
	profitDefaults(in)
	if err := validateProfitExtras(in); err != nil {
		return nil, err
	}
	metadata := resolveProductStorage(in, p)
	referralMeta, effectiveCommission := resolveReferral(in, p)
	for key, value := range referralMeta {
		metadata[key] = value
	}
	var volume, billable, derivedFreight *big.Rat
	divisor, ok := new(big.Rat).SetString(str(in["volumetric_divisor_cm3_per_kg"]))
	if ok && divisor.Sign() > 0 {
		dimensions := big.NewRat(1, 1)
		valid := true
		for _, key := range []string{"package_length_cm", "package_width_cm", "package_height_cm"} {
			n, ok := new(big.Rat).SetString(str(p[key]))
			if !ok || n.Sign() <= 0 {
				valid = false
				break
			}
			dimensions.Mul(dimensions, n)
		}
		if valid {
			volume = div(dimensions, divisor)
			metadata["volumetric_weight_kg"] = volume.FloatString(6)
		}
	}
	if weight, ok := new(big.Rat).SetString(str(p["package_weight_g"])); ok && weight.Sign() >= 0 && volume != nil {
		billable = div(weight, big.NewRat(1000, 1))
		if volume.Cmp(billable) > 0 {
			billable = new(big.Rat).Set(volume)
		}
		metadata["billable_weight_kg"] = billable.FloatString(6)
	}
	if raw := str(in["freight_rate_cny_per_kg"]); raw != "" {
		rate, _ := new(big.Rat).SetString(raw)
		// Missing measurements never turn into free shipping.
		in["freight_cny"] = nil
		if rate != nil && billable != nil {
			derivedFreight = mul(rate, billable)
			in["freight_cny"] = derivedFreight.FloatString(6)
		}
	}
	out, err := calculateWithReferral(in, derivedFreight, effectiveCommission)
	if err != nil {
		return nil, err
	}
	price, pok := new(big.Rat).SetString(str(in["sale_price_usd"]))
	discount, dok := new(big.Rat).SetString(str(in["discount_usd"]))
	commission, cok := new(big.Rat).SetString(str(in["commission_rate"]))
	if pok && dok && cok && sub(price, discount).Sign() > 0 {
		out["commission_usd"] = mul(sub(price, discount), commission).FloatString(2)
	}
	if effectiveCommission != nil && out["state"] == "complete" {
		rat := func(key string) *big.Rat { n, _ := new(big.Rat).SetString(str(in[key])); return n }
		freightCost := rat("freight_cny")
		if derivedFreight != nil {
			freightCost = derivedFreight
		}
		cost := div(add(add(rat("purchase_cny"), rat("packaging_cny")), add(freightCost, rat("other_landed_cny"))), rat("fx_cny_per_usd"))
		fixed := add(add(cost, rat("fba_fee_usd")), add(rat("storage_usd"), add(rat("other_usd"), mul(rat("return_rate"), rat("loss_per_return_usd")))))
		out["break_even_price_usd"] = nil
		if price := referralBreakEven(referralRules[str(referralMeta["commission_category"])], fixed, rat("ad_rate")); price != nil {
			out["break_even_price_usd"] = price.FloatString(2)
		}
	}
	metadata["storage_usd"] = in["storage_usd"]
	if derivedFreight != nil {
		metadata["freight_cny"] = derivedFreight.FloatString(6)
	}
	for key, value := range metadata {
		out[key] = value
	}
	if str(in["freight_rate_cny_per_kg"]) != "" && billable == nil {
		out["freight_missing"] = strings.Join([]string{"包装重量", "包装长宽高", "体积重分母"}, "、")
	}
	return out, nil
}
