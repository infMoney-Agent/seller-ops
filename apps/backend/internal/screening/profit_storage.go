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
	if str(in["volumetric_divisor_cm3_per_kg"]) == "" {
		in["volumetric_divisor_cm3_per_kg"] = "5000"
	}
	if str(in["freight_rate_cny_per_kg"]) == "" && str(in["freight_cny"]) == "" {
		in["freight_rate_cny_per_kg"] = "9"
	}
	if str(in["storage_mode"]) == "" {
		if str(in["storage_usd"]) == "" {
			in["storage_mode"] = "auto"
		} else {
			in["storage_mode"] = "manual"
		}
	}
	if str(in["storage_days"]) == "" {
		in["storage_days"] = "30"
	}
	if str(in["storage_month"]) == "" {
		loc, _ := time.LoadLocation("America/Los_Angeles")
		in["storage_month"] = time.Now().In(loc).Format("2006-01")
	}
}

func validStorageInputs(in Object) error {
	if mode := str(in["storage_mode"]); mode != "" && mode != "auto" && mode != "manual" {
		return fmt.Errorf("仓租模式无效")
	}
	if month := str(in["storage_month"]); month != "" {
		if _, err := time.Parse("2006-01", month); err != nil {
			return fmt.Errorf("仓租月份须为YYYY-MM")
		}
	}
	if raw := str(in["storage_days"]); raw != "" {
		v, ok := new(big.Rat).SetString(raw)
		if !ok || !decimalPattern.MatchString(raw) || v.Cmp(big.NewRat(1, 1)) < 0 || v.Cmp(big.NewRat(3650, 1)) > 0 {
			return fmt.Errorf("预计在库天数须为1至3650")
		}
	}
	return nil
}

// Mirrors the product library's ordinary US base-storage estimate. This is
// separate from aged inventory, utilization and dangerous-goods surcharges.
func resolveProductStorage(in, p Object) Object {
	meta := Object{"storage_source": "manual"}
	if str(in["storage_mode"]) != "auto" {
		return meta
	}
	in["storage_usd"] = nil
	source, _ := p["profit_source"].(map[string]any)
	raw := strings.TrimSpace(strings.ReplaceAll(strings.TrimPrefix(str(source["storage_usd"]), "$"), ",", ""))
	if n, ok := new(big.Rat).SetString(raw); ok && decimalPattern.MatchString(raw) && n.Sign() >= 0 {
		in["storage_usd"] = n.FloatString(6)
		meta["storage_source"] = "import"
		return meta
	}
	meta["storage_source"] = "missing"
	if marketplace := str(p["marketplace"]); marketplace != "" && marketplace != "US" {
		return meta
	}
	axes := []*big.Rat{}
	volume := big.NewRat(1, 1)
	for _, key := range []string{"package_length_cm", "package_width_cm", "package_height_cm"} {
		n, ok := new(big.Rat).SetString(str(p[key]))
		if !ok || n.Sign() <= 0 {
			return meta
		}
		axes = append(axes, n)
		volume.Mul(volume, n)
	}
	sort.Slice(axes, func(i, j int) bool { return axes[i].Cmp(axes[j]) > 0 })
	tier := ""
	label := strings.ToLower(str(source["tier"]))
	for _, word := range []string{"oversize", "bulky", "extra large", "extra-large", "大件", "超大", "特大"} {
		if strings.Contains(label, word) {
			tier = "oversize"
		}
	}
	if tier == "" && (strings.Contains(label, "standard") || strings.Contains(label, "标准")) {
		tier = "standard"
	}
	if tier == "" {
		weight, ok := new(big.Rat).SetString(str(p["package_weight_g"]))
		if axes[0].Cmp(big.NewRat(4572, 100)) > 0 || axes[1].Cmp(big.NewRat(3556, 100)) > 0 || axes[2].Cmp(big.NewRat(2032, 100)) > 0 || (ok && weight.Cmp(big.NewRat(90718474, 10000)) > 0) {
			tier = "oversize"
		} else if ok && weight.Sign() > 0 {
			tier = "standard"
		} else {
			return meta
		}
	}
	month, err := time.Parse("2006-01", str(in["storage_month"]))
	if err != nil {
		return meta
	}
	days, _ := new(big.Rat).SetString(str(in["storage_days"]))
	if days == nil {
		return meta
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
	in["storage_usd"] = amount.FloatString(6)
	meta["storage_source"] = "estimate"
	meta["storage_rate_usd_ft3"] = rate.FloatString(2)
	meta["storage_month"] = month.Format("2006-01")
	meta["storage_days"] = days.FloatString(0)
	meta["storage_tier"] = tier
	meta["storage_rate_version"] = storageRateVersion
	return meta
}

const profitSourceProjection = `jsonb_build_object('profit_source',(SELECT jsonb_build_object('storage_usd',coalesce(r.raw_data->>'亚马逊仓租($)',r.raw_data->>'仓租($)',r.raw_data->>'Storage Fee($)'),'tier',r.raw_data->>'包装尺寸分段') FROM ps_import_rows r WHERE r.tenant_id=p.tenant_id AND r.id=p.current_row_id))`
