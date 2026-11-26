package screening

import (
	"math/big"
	"strings"
)

var freightFields = []string{"freight_rate_cny_per_kg", "volumetric_divisor_cm3_per_kg"}

func validateProfitExtras(in Object) error {
	return parseProfitInputs(in).validateExtras()
}

// All freight arithmetic uses exact rationals. Measurements come from the
// version-checked product, not arbitrary dimensions supplied by the browser.
func calculateProductProfit(in, p Object) (Object, error) {
	out, err := calculateProductProfitStructured(in, p)
	if err != nil {
		return nil, err
	}
	return out.ToMap(), nil
}

func calculateProductProfitStructured(in, p Object) (*ProfitOutput, error) {
	parsed := parseProfitInputs(in)
	facts := extractProductProfitFacts(p)
	out, err := calculateProductProfitTyped(parsed, facts)
	parsed.applyTouchedToMap(in)
	return out, err
}

func calculateProductProfitTyped(in *ProfitInputs, facts ProductProfitFacts) (*ProfitOutput, error) {
	in.applyDefaults()
	if err := in.validateExtras(); err != nil {
		return nil, err
	}
	storage := resolveStorageFromFacts(in, facts)
	referral := resolveReferralFromFacts(in, facts)
	var volume, billable, derivedFreight *big.Rat
	var volumetricWeightKg, billableWeightKg, freightMissing *string
	divisor, ok := new(big.Rat).SetString(stringValue(in.VolumetricDivisorCM3PerKg))
	if ok && divisor.Sign() > 0 {
		dimensions := big.NewRat(1, 1)
		valid := true
		for _, rawDim := range []string{facts.PackageLengthCM, facts.PackageWidthCM, facts.PackageHeightCM} {
			n, ok := new(big.Rat).SetString(rawDim)
			if !ok || n.Sign() <= 0 {
				valid = false
				break
			}
			dimensions.Mul(dimensions, n)
		}
		if valid {
			volume = div(dimensions, divisor)
			volumetricWeightKg = stringPtr(volume.FloatString(6))
		}
	}
	if weight, ok := new(big.Rat).SetString(facts.PackageWeightG); ok && weight.Sign() >= 0 && volume != nil {
		billable = div(weight, big.NewRat(1000, 1))
		if volume.Cmp(billable) > 0 {
			billable = new(big.Rat).Set(volume)
		}
		billableWeightKg = stringPtr(billable.FloatString(6))
	}
	if raw := stringValue(in.FreightRateCNYPerKg); raw != "" {
		rate, _ := new(big.Rat).SetString(raw)
		// Missing measurements never turn into free shipping.
		in.SetFee("freight_cny", nil)
		if rate != nil && billable != nil {
			derivedFreight = mul(rate, billable)
			in.SetFee("freight_cny", stringPtr(derivedFreight.FloatString(6)))
		}
	}
	out, err := calculateCoreTyped(in, derivedFreight, referral.EffectiveRate)
	if err != nil {
		return nil, err
	}
	price, pok := new(big.Rat).SetString(in.Fee("sale_price_usd"))
	discount, dok := new(big.Rat).SetString(in.Fee("discount_usd"))
	commission, cok := new(big.Rat).SetString(in.Fee("commission_rate"))
	if referral.FeeUSD != nil {
		out.CommissionUSD = referral.FeeUSD
	} else if pok && dok && cok && sub(price, discount).Sign() > 0 {
		out.CommissionUSD = stringPtr(mul(sub(price, discount), commission).FloatString(2))
	}
	if referral.EffectiveRate != nil && out.State == "complete" {
		rat := func(key string) *big.Rat { n, _ := new(big.Rat).SetString(in.Fee(key)); return n }
		freightCost := rat("freight_cny")
		if derivedFreight != nil {
			freightCost = derivedFreight
		}
		cost := div(add(add(rat("purchase_cny"), rat("packaging_cny")), add(freightCost, rat("other_landed_cny"))), rat("fx_cny_per_usd"))
		fixed := add(add(cost, rat("fba_fee_usd")), add(rat("storage_usd"), add(rat("other_usd"), mul(rat("return_rate"), rat("loss_per_return_usd")))))
		out.BreakEvenPriceUSD = nil
		if breakEven := referralBreakEven(referralRules[stringValue(referral.Category)], fixed, rat("ad_rate")); breakEven != nil {
			out.BreakEvenPriceUSD = stringPtr(breakEven.FloatString(2))
		}
	}
	if stringValue(in.FreightRateCNYPerKg) != "" && billable == nil {
		freightMissing = stringPtr(strings.Join([]string{"包装重量", "包装长宽高", "体积重分母"}, "、"))
	}
	out.HasProductMeta = true
	out.Storage = storage
	out.StorageUSD = in.FeePtr("storage_usd")
	out.Referral = referral
	out.VolumetricWeightKg = volumetricWeightKg
	out.BillableWeightKg = billableWeightKg
	if derivedFreight != nil {
		out.FreightCNY = stringPtr(derivedFreight.FloatString(6))
	}
	out.FreightMissing = freightMissing
	return out, nil
}
