package screening

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
func calculate(in map[string]any) (map[string]any, error) { return calculateWithFreight(in, nil) }
func calculateWithFreight(in map[string]any, freightOverride *big.Rat) (map[string]any, error) {
	return calculateWithReferral(in, freightOverride, nil)
}
func calculateWithReferral(in map[string]any, freightOverride, commissionOverride *big.Rat) (map[string]any, error) {
	parsed := parseProfitInputs(in)
	out, err := calculateCoreTyped(parsed, freightOverride, commissionOverride)
	parsed.applyTouchedToMap(in)
	if err != nil {
		return nil, err
	}
	return out.ToMap(), nil
}

func calculateCoreTyped(in *ProfitInputs, freightOverride, commissionOverride *big.Rat) (*ProfitOutput, error) {
	if err := in.validateExtras(); err != nil {
		return nil, err
	}
	if in.unknownKey != "" {
		return nil, fmt.Errorf("未知测算字段: %s", in.unknownKey)
	}
	if in.invalidName {
		return nil, fmt.Errorf("方案名称无效")
	}
	if in.Name == nil {
		in.Name = stringPtr("基准")
		in.markTouched("name", in.Name)
	}
	if name := stringValue(in.Name); len([]rune(name)) == 0 || len([]rune(name)) > 100 {
		return nil, fmt.Errorf("方案名称无效")
	}
	if in.invalidFieldErr != nil {
		return nil, in.invalidFieldErr
	}
	vals := map[string]*big.Rat{}
	missing := []string{}
	one := big.NewRat(1, 1)
	for _, key := range profitFields {
		if key == "freight_cny" && freightOverride != nil {
			if freightOverride.Sign() < 0 || freightOverride.Cmp(big.NewRat(999999999999, 1)) > 0 {
				return nil, fmt.Errorf("头程成本超出允许范围")
			}
			vals[key] = new(big.Rat).Set(freightOverride)
			continue
		}
		text := in.Fee(key)
		if in.FeePtr(key) == nil || text == "" {
			missing = append(missing, key)
			continue
		}
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
	if in.MonthlyUnits != nil {
		n, ok := new(big.Rat).SetString(*in.MonthlyUnits)
		if !ok || !n.IsInt() || n.Sign() < 0 || n.Cmp(big.NewRat(1000000000, 1)) > 0 {
			return nil, fmt.Errorf("预计月销量无效")
		}
	}
	if vals["sale_price_usd"] != nil && vals["discount_usd"] != nil && sub(vals["sale_price_usd"], vals["discount_usd"]).Sign() <= 0 {
		return nil, fmt.Errorf("优惠后售价必须大于零")
	}
	out := &ProfitOutput{FormulaVersion: formulaVersion}
	if len(missing) > 0 {
		out.State = "incomplete"
		out.Missing = missing
		return out, nil
	}
	p := sub(vals["sale_price_usd"], vals["discount_usd"])
	if p.Sign() <= 0 {
		return nil, fmt.Errorf("优惠后售价必须大于零")
	}
	cost := div(add(add(vals["purchase_cny"], vals["packaging_cny"]), add(vals["freight_cny"], vals["other_landed_cny"])), vals["fx_cny_per_usd"])
	f := add(add(cost, vals["fba_fee_usd"]), add(vals["storage_usd"], add(vals["other_usd"], mul(vals["return_rate"], vals["loss_per_return_usd"]))))
	if commissionOverride != nil {
		vals["commission_rate"] = commissionOverride
	}
	denom := sub(sub(one, vals["commission_rate"]), vals["ad_rate"])
	profit := sub(mul(p, denom), f)
	out.State = "complete"
	out.ProfitUSD = profit.FloatString(2)
	out.ProfitCNY = mul(profit, vals["fx_cny_per_usd"]).FloatString(2)
	out.CommissionUSD = stringPtr(mul(p, vals["commission_rate"]).FloatString(2))
	out.FreightCNY = stringPtr(vals["freight_cny"].FloatString(6))
	out.Margin = div(profit, p).FloatString(6)
	if cost.Sign() > 0 {
		out.ROI = stringPtr(div(profit, cost).FloatString(6))
	}
	if denom.Sign() > 0 {
		out.BreakEvenPriceUSD = stringPtr(div(f, denom).FloatString(2))
	}
	out.BreakEvenAdRate = sub(sub(one, vals["commission_rate"]), div(f, p)).FloatString(6)
	if in.MonthlyUnits != nil {
		n, _ := new(big.Rat).SetString(*in.MonthlyUnits)
		out.MonthlyProfitUSD = stringPtr(mul(profit, n).FloatString(2))
	}
	return out, nil
}
