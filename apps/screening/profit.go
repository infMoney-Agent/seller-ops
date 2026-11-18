package main

import (
	"fmt"
	"math/big"
	"strings"
)

const formulaVersion = "fba-us-v1"

var profitFields = []string{"sale_price_usd", "discount_usd", "purchase_cny", "packaging_cny", "freight_cny", "other_landed_cny", "fx_cny_per_usd", "commission_rate", "fba_fee_usd", "storage_usd", "ad_rate", "return_rate", "loss_per_return_usd", "other_usd"}

func add(a, b *big.Rat) *big.Rat { return new(big.Rat).Add(a, b) }
func sub(a, b *big.Rat) *big.Rat { return new(big.Rat).Sub(a, b) }
func mul(a, b *big.Rat) *big.Rat { return new(big.Rat).Mul(a, b) }
func div(a, b *big.Rat) *big.Rat { return new(big.Rat).Quo(a, b) }

// Rational arithmetic keeps decimal inputs exact until formatting final outputs.
func calculate(in map[string]any) (map[string]any, error) {
	allowed := map[string]bool{"name": true, "monthly_units": true, "fee_source": true, "note": true}
	for _, field := range profitFields {
		allowed[field] = true
	}
	for field := range in {
		if !allowed[field] {
			return nil, fmt.Errorf("未知测算字段: %s", field)
		}
	}
	if in["name"] == nil {
		in["name"] = "基准"
	}
	if name, ok := in["name"].(string); !ok || len([]rune(name)) == 0 || len([]rune(name)) > 100 {
		return nil, fmt.Errorf("方案名称无效")
	}
	for _, key := range []string{"fee_source", "note"} {
		if in[key] != nil {
			if value, ok := in[key].(string); !ok || len([]rune(value)) > 2000 {
				return nil, fmt.Errorf("%s 无效", key)
			}
		}
	}
	vals := map[string]*big.Rat{}
	missing := []string{}
	one := big.NewRat(1, 1)
	for _, key := range profitFields {
		raw := in[key]
		if raw == nil || raw == "" {
			missing = append(missing, key)
			continue
		}
		text := fmt.Sprint(raw)
		if !decimalPattern.MatchString(text) {
			return nil, fmt.Errorf("%s 数值无效，最多6位小数", key)
		}
		n, ok := new(big.Rat).SetString(text)
		if !ok || n.Sign() < 0 || n.Cmp(big.NewRat(999999999999, 1)) > 0 {
			return nil, fmt.Errorf("%s 数值无效", key)
		}
		if strings.HasSuffix(key, "_rate") && n.Cmp(one) > 0 {
			return nil, fmt.Errorf("%s 必须在0到1之间", key)
		}
		if (key == "fx_cny_per_usd" || key == "sale_price_usd") && n.Sign() <= 0 {
			return nil, fmt.Errorf("%s 必须大于零", key)
		}
		vals[key] = n
	}
	if raw := in["monthly_units"]; raw != nil {
		n, ok := new(big.Rat).SetString(fmt.Sprint(raw))
		if !ok || !n.IsInt() || n.Sign() < 0 || n.Cmp(big.NewRat(1000000000, 1)) > 0 {
			return nil, fmt.Errorf("预计月销量无效")
		}
	}
	if vals["sale_price_usd"] != nil && vals["discount_usd"] != nil && sub(vals["sale_price_usd"], vals["discount_usd"]).Sign() <= 0 {
		return nil, fmt.Errorf("优惠后售价必须大于零")
	}
	out := map[string]any{"formula_version": formulaVersion}
	if len(missing) > 0 {
		out["state"] = "incomplete"
		out["missing"] = missing
		return out, nil
	}
	p := sub(vals["sale_price_usd"], vals["discount_usd"])
	if p.Sign() <= 0 {
		return nil, fmt.Errorf("优惠后售价必须大于零")
	}
	cost := div(add(add(vals["purchase_cny"], vals["packaging_cny"]), add(vals["freight_cny"], vals["other_landed_cny"])), vals["fx_cny_per_usd"])
	f := add(add(cost, vals["fba_fee_usd"]), add(vals["storage_usd"], add(vals["other_usd"], mul(vals["return_rate"], vals["loss_per_return_usd"]))))
	denom := sub(sub(one, vals["commission_rate"]), vals["ad_rate"])
	profit := sub(mul(p, denom), f)
	out["state"] = "complete"
	out["profit_usd"] = profit.FloatString(2)
	out["margin"] = div(profit, p).FloatString(6)
	out["roi"] = nil
	if cost.Sign() > 0 {
		out["roi"] = div(profit, cost).FloatString(6)
	}
	out["break_even_price_usd"] = nil
	if denom.Sign() > 0 {
		out["break_even_price_usd"] = div(f, denom).FloatString(2)
	}
	out["break_even_ad_rate"] = sub(sub(one, vals["commission_rate"]), div(f, p)).FloatString(6)
	if raw := in["monthly_units"]; raw != nil {
		n, ok := new(big.Rat).SetString(fmt.Sprint(raw))
		if !ok || !n.IsInt() || n.Sign() < 0 || n.Cmp(big.NewRat(1000000000, 1)) > 0 {
			return nil, fmt.Errorf("预计月销量无效")
		}
		out["monthly_profit_usd"] = mul(profit, n).FloatString(2)
	}
	return out, nil
}
