package screening

import (
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"
	_ "time/tzdata"
)

const storageRateVersion = "us-nondangerous-base-0.78-2.40-0.56-1.40"

func profitDefaults(in Object) {
	parsed := parseProfitInputs(in)
	parsed.applyDefaults()
	parsed.applyTouchedToMap(in)
}

func (in *ProfitInputs) applyDefaults() {
	if stringValue(in.VolumetricDivisorCM3PerKg) == "" {
		in.VolumetricDivisorCM3PerKg = stringPtr("5000")
		in.markTouched("volumetric_divisor_cm3_per_kg", in.VolumetricDivisorCM3PerKg)
	}
	if stringValue(in.FreightRateCNYPerKg) == "" && in.Fee("freight_cny") == "" {
		in.FreightRateCNYPerKg = stringPtr("9")
		in.markTouched("freight_rate_cny_per_kg", in.FreightRateCNYPerKg)
	}
	if stringValue(in.StorageMode) == "" {
		if in.Fee("storage_usd") == "" {
			in.StorageMode = stringPtr("auto")
		} else {
			in.StorageMode = stringPtr("manual")
		}
		in.markTouched("storage_mode", in.StorageMode)
	}
	if stringValue(in.StorageDays) == "" {
		in.StorageDays = stringPtr("30")
		in.markTouched("storage_days", in.StorageDays)
	}
	if stringValue(in.StorageMonth) == "" {
		loc, _ := time.LoadLocation("America/Los_Angeles")
		in.StorageMonth = stringPtr(time.Now().In(loc).Format("2006-01"))
		in.markTouched("storage_month", in.StorageMonth)
	}
}

func validateStorageConfig(mode, month, rawDays string) error {
	if mode != "" && mode != "auto" && mode != "manual" {
		return fmt.Errorf("仓租模式无效")
	}
	if month != "" {
		if _, err := time.Parse("2006-01", month); err != nil {
			return fmt.Errorf("仓租月份须为YYYY-MM")
		}
	}
	if rawDays != "" {
		v, ok := new(big.Rat).SetString(rawDays)
		if !ok || !decimalPattern.MatchString(rawDays) || v.Cmp(big.NewRat(1, 1)) < 0 || v.Cmp(big.NewRat(3650, 1)) > 0 {
			return fmt.Errorf("预计在库天数须为1至3650")
		}
	}
	return nil
}

// Mirrors the product library's ordinary US base-storage estimate. This is
// separate from aged inventory, utilization and dangerous-goods surcharges.
func resolveProductStorage(in, p Object) Object {
	parsed := parseProfitInputs(in)
	res := resolveStorageFromFacts(parsed, extractProductProfitFacts(p))
	parsed.applyTouchedToMap(in)
	out := Object{"storage_source": res.Source}
	if res.RateUSDFt3 != nil {
		out["storage_rate_usd_ft3"] = *res.RateUSDFt3
	}
	if res.Month != nil {
		out["storage_month"] = *res.Month
	}
	if res.Days != nil {
		out["storage_days"] = *res.Days
	}
	if res.Tier != nil {
		out["storage_tier"] = *res.Tier
	}
	if res.RateVersion != nil {
		out["storage_rate_version"] = *res.RateVersion
	}
	return out
}

func resolveStorageFromFacts(in *ProfitInputs, facts ProductProfitFacts) StorageResolution {
	res := StorageResolution{Source: "manual"}
	if stringValue(in.StorageMode) != "auto" {
		return res
	}
	in.SetFee("storage_usd", nil)
	raw := strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(facts.ImportedStorage, "$"), ",", ""))
	if n, ok := new(big.Rat).SetString(raw); ok && decimalPattern.MatchString(raw) && n.Sign() >= 0 {
		in.SetFee("storage_usd", stringPtr(n.FloatString(6)))
		res.Source = "import"
		return res
	}
	res.Source = "missing"
	if facts.Marketplace != "" && facts.Marketplace != "US" {
		return res
	}
	axes := []*big.Rat{}
	volume := big.NewRat(1, 1)
	for _, rawDim := range []string{facts.PackageLengthCM, facts.PackageWidthCM, facts.PackageHeightCM} {
		n, ok := new(big.Rat).SetString(rawDim)
		if !ok || n.Sign() <= 0 {
			return res
		}
		axes = append(axes, n)
		volume.Mul(volume, n)
	}
	sort.Slice(axes, func(i, j int) bool { return axes[i].Cmp(axes[j]) > 0 })
	tier := ""
	label := strings.ToLower(facts.ImportedSizeTier)
	for _, word := range []string{"oversize", "bulky", "extra large", "extra-large", "大件", "超大", "特大"} {
		if strings.Contains(label, word) {
			tier = "oversize"
		}
	}
	if tier == "" && (strings.Contains(label, "standard") || strings.Contains(label, "标准")) {
		tier = "standard"
	}
	if tier == "" {
		weight, ok := new(big.Rat).SetString(facts.PackageWeightG)
		if axes[0].Cmp(big.NewRat(4572, 100)) > 0 || axes[1].Cmp(big.NewRat(3556, 100)) > 0 || axes[2].Cmp(big.NewRat(2032, 100)) > 0 || (ok && weight.Cmp(big.NewRat(90718474, 10000)) > 0) {
			tier = "oversize"
		} else if ok && weight.Sign() > 0 {
			tier = "standard"
		} else {
			return res
		}
	}
	month, err := time.Parse("2006-01", stringValue(in.StorageMonth))
	if err != nil {
		return res
	}
	days, _ := new(big.Rat).SetString(stringValue(in.StorageDays))
	if days == nil {
		return res
	}
	rate := big.NewRat(78, 100)
	if tier == "oversize" {
		rate = big.NewRat(56, 100)
	}
	if month.Month() >= 10 {
		if tier == "standard" {
			rate = big.NewRat(240, 100)
		} else {
			rate = big.NewRat(140, 100)
		}
	}
	cube := big.NewRat(3048, 100)
	cube = mul(mul(cube, cube), cube)
	ft3 := div(volume, cube)
	monthDays := month.AddDate(0, 1, -1).Day()
	amount := div(mul(mul(ft3, rate), days), big.NewRat(int64(monthDays), 1))
	in.SetFee("storage_usd", stringPtr(amount.FloatString(6)))
	res.Source = "estimate"
	res.RateUSDFt3 = stringPtr(rate.FloatString(2))
	res.Month = stringPtr(month.Format("2006-01"))
	res.Days = stringPtr(days.FloatString(0))
	res.Tier = stringPtr(tier)
	res.RateVersion = stringPtr(storageRateVersion)
	return res
}

const profitSourceProjection = `jsonb_build_object('profit_source',(SELECT jsonb_build_object('storage_usd',coalesce(r.raw_data->>'亚马逊仓租($)',r.raw_data->>'仓租($)',r.raw_data->>'Storage Fee($)'),'tier',r.raw_data->>'包装尺寸分段') FROM ps_import_rows r WHERE r.tenant_id=p.tenant_id AND r.id=p.current_row_id))`
