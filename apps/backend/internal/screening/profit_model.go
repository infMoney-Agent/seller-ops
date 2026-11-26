package screening

import (
	"encoding/json"
	"fmt"
	"math/big"
	"net/url"
)

var allowedProfitMetaKeys = map[string]bool{
	"name":                          true,
	"monthly_units":                 true,
	"fee_source":                    true,
	"note":                          true,
	"commission_mode":               true,
	"commission_category":           true,
	"storage_mode":                  true,
	"storage_days":                  true,
	"storage_month":                 true,
	"supplier_url":                  true,
	"freight_rate_cny_per_kg":       true,
	"volumetric_divisor_cm3_per_kg": true,
}

// ProfitInputs models all fee, rate, freight, referral, and storage inputs as
// explicit Go fields instead of untyped nested maps.
type ProfitInputs struct {
	Name                      *string
	MonthlyUnits              *string
	FeeSource                 *string
	Note                      *string
	SupplierURL               *string
	CommissionMode            *string
	CommissionCategory        *string
	StorageMode               *string
	StorageDays               *string
	StorageMonth              *string
	FreightRateCNYPerKg       *string
	VolumetricDivisorCM3PerKg *string
	Fees                      map[string]*string

	invalidName     bool
	invalidFieldErr error
	unknownKey      string
	touched         map[string]any
}

func stringPtr(s string) *string {
	v := s
	return &v
}

func optionalText(v any) *string {
	if v == nil {
		return nil
	}
	s := str(v)
	return &s
}

func (in *ProfitInputs) markTouched(key string, val *string) {
	if in.touched == nil {
		in.touched = map[string]any{}
	}
	if val == nil {
		in.touched[key] = nil
	} else {
		in.touched[key] = *val
	}
}

func (in *ProfitInputs) Fee(key string) string {
	if in == nil || in.Fees == nil {
		return ""
	}
	return stringValue(in.Fees[key])
}

func (in *ProfitInputs) FeePtr(key string) *string {
	if in == nil || in.Fees == nil {
		return nil
	}
	return in.Fees[key]
}

func (in *ProfitInputs) SetFee(key string, val *string) {
	if in.Fees == nil {
		in.Fees = map[string]*string{}
	}
	in.Fees[key] = val
	in.markTouched(key, val)
}

func parseProfitInputs(raw Object) *ProfitInputs {
	out := &ProfitInputs{
		Fees:    make(map[string]*string, len(profitFields)),
		touched: map[string]any{},
	}
	knownFees := make(map[string]bool, len(profitFields))
	for _, f := range profitFields {
		knownFees[f] = true
	}
	for k := range raw {
		if !allowedProfitMetaKeys[k] && !knownFees[k] && out.unknownKey == "" {
			out.unknownKey = k
		}
	}
	if v, present := raw["name"]; present && v != nil {
		s, ok := v.(string)
		if !ok {
			out.invalidName = true
		} else {
			out.Name = stringPtr(s)
		}
	}
	for _, key := range []string{"fee_source", "note"} {
		if v := raw[key]; v != nil {
			s, ok := v.(string)
			if !ok || len([]rune(s)) > 2000 {
				if out.invalidFieldErr == nil {
					out.invalidFieldErr = fmt.Errorf("%s 无效", key)
				}
			} else if key == "fee_source" {
				out.FeeSource = stringPtr(s)
			} else {
				out.Note = stringPtr(s)
			}
		}
	}
	if v := raw["supplier_url"]; v != nil && str(v) != "" {
		s, ok := v.(string)
		if !ok {
			out.SupplierURL = stringPtr(str(v))
			if out.invalidFieldErr == nil {
				out.invalidFieldErr = fmt.Errorf("供应商链接无效")
			}
		} else {
			out.SupplierURL = stringPtr(s)
		}
	} else if v != nil {
		out.SupplierURL = stringPtr("")
	}
	out.MonthlyUnits = optionalText(raw["monthly_units"])
	out.CommissionMode = optionalText(raw["commission_mode"])
	out.CommissionCategory = optionalText(raw["commission_category"])
	out.StorageMode = optionalText(raw["storage_mode"])
	out.StorageDays = optionalText(raw["storage_days"])
	out.StorageMonth = optionalText(raw["storage_month"])
	out.FreightRateCNYPerKg = optionalText(raw["freight_rate_cny_per_kg"])
	out.VolumetricDivisorCM3PerKg = optionalText(raw["volumetric_divisor_cm3_per_kg"])
	for _, key := range profitFields {
		out.Fees[key] = optionalText(raw[key])
	}
	return out
}

func (in *ProfitInputs) validateExtras() error {
	if err := validateReferralMode(stringValue(in.CommissionMode), stringValue(in.CommissionCategory)); err != nil {
		return err
	}
	if err := validateStorageConfig(stringValue(in.StorageMode), stringValue(in.StorageMonth), stringValue(in.StorageDays)); err != nil {
		return err
	}
	for _, pair := range [][2]string{
		{"freight_rate_cny_per_kg", stringValue(in.FreightRateCNYPerKg)},
		{"volumetric_divisor_cm3_per_kg", stringValue(in.VolumetricDivisorCM3PerKg)},
	} {
		key, raw := pair[0], pair[1]
		if raw == "" {
			continue
		}
		n, ok := new(big.Rat).SetString(raw)
		if !decimalPattern.MatchString(raw) || !ok || n.Sign() < 0 || n.Cmp(big.NewRat(999999999999, 1)) > 0 {
			return fmt.Errorf("%s 数值无效", key)
		}
		if key == "volumetric_divisor_cm3_per_kg" && n.Sign() == 0 {
			return fmt.Errorf("体积重分母必须大于零")
		}
	}
	if raw := stringValue(in.SupplierURL); raw != "" {
		if in.invalidFieldErr != nil && in.invalidFieldErr.Error() == "供应商链接无效" {
			return in.invalidFieldErr
		}
		if len(raw) > 2000 {
			return fmt.Errorf("供应商链接无效")
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			return fmt.Errorf("供应商链接须为有效的 HTTP/HTTPS 地址")
		}
	}
	return nil
}

func (in *ProfitInputs) applyTouchedToMap(dst Object) {
	if dst == nil {
		return
	}
	for k, v := range in.touched {
		dst[k] = v
	}
}

// ProductProfitFacts captures immutable product measurements and category facts
// used during storage, referral, and volumetric freight calculation.
type ProductProfitFacts struct {
	Marketplace      string
	CategoryPath     string
	Category         string
	MainCategory     string
	PackageLengthCM  string
	PackageWidthCM   string
	PackageHeightCM  string
	PackageWeightG   string
	ImportedStorage  string
	ImportedSizeTier string
}

func extractProductProfitFacts(p Object) ProductProfitFacts {
	source, _ := p["profit_source"].(map[string]any)
	return ProductProfitFacts{
		Marketplace:      str(p["marketplace"]),
		CategoryPath:     str(p["category_path"]),
		Category:         str(p["category"]),
		MainCategory:     str(p["main_category"]),
		PackageLengthCM:  str(p["package_length_cm"]),
		PackageWidthCM:   str(p["package_width_cm"]),
		PackageHeightCM:  str(p["package_height_cm"]),
		PackageWeightG:   str(p["package_weight_g"]),
		ImportedStorage:  str(source["storage_usd"]),
		ImportedSizeTier: str(source["tier"]),
	}
}

type StorageResolution struct {
	Source      string
	RateUSDFt3  *string
	Month       *string
	Days        *string
	Tier        *string
	RateVersion *string
}

type ReferralResolution struct {
	Source        string
	Category      *string
	CategoryLabel *string
	RuleVersion   *string
	Rate          *string
	MinimumUSD    *string
	FeeUSD        *string
	EffectiveRate *big.Rat
}

// ProfitOutput is the strongly typed domain result of a profit calculation.
type ProfitOutput struct {
	FormulaVersion    string
	State             string
	Missing           []string
	ProfitUSD         string
	ProfitCNY         string
	CommissionUSD     *string
	FreightCNY        *string
	Margin            string
	ROI               *string
	BreakEvenPriceUSD *string
	BreakEvenAdRate   string
	MonthlyProfitUSD  *string

	HasProductMeta     bool
	Storage            StorageResolution
	StorageUSD         *string
	Referral           ReferralResolution
	VolumetricWeightKg *string
	BillableWeightKg   *string
	FreightMissing     *string
}

func (o *ProfitOutput) ToMap() Object {
	if o == nil {
		return nil
	}
	out := Object{
		"formula_version": o.FormulaVersion,
		"state":           o.State,
	}
	if o.State == "incomplete" {
		missing := o.Missing
		if missing == nil {
			missing = []string{}
		}
		out["missing"] = missing
	} else {
		out["profit_usd"] = o.ProfitUSD
		out["profit_cny"] = o.ProfitCNY
		if o.CommissionUSD != nil {
			out["commission_usd"] = *o.CommissionUSD
		}
		if o.FreightCNY != nil {
			out["freight_cny"] = *o.FreightCNY
		}
		out["margin"] = o.Margin
		if o.ROI != nil {
			out["roi"] = *o.ROI
		} else {
			out["roi"] = nil
		}
		if o.BreakEvenPriceUSD != nil {
			out["break_even_price_usd"] = *o.BreakEvenPriceUSD
		} else {
			out["break_even_price_usd"] = nil
		}
		out["break_even_ad_rate"] = o.BreakEvenAdRate
		if o.MonthlyProfitUSD != nil {
			out["monthly_profit_usd"] = *o.MonthlyProfitUSD
		}
	}
	if o.CommissionUSD != nil {
		out["commission_usd"] = *o.CommissionUSD
	}
	if o.HasProductMeta {
		out["storage_source"] = o.Storage.Source
		if o.Storage.RateUSDFt3 != nil {
			out["storage_rate_usd_ft3"] = *o.Storage.RateUSDFt3
		}
		if o.Storage.Month != nil {
			out["storage_month"] = *o.Storage.Month
		}
		if o.Storage.Days != nil {
			out["storage_days"] = *o.Storage.Days
		}
		if o.Storage.Tier != nil {
			out["storage_tier"] = *o.Storage.Tier
		}
		if o.Storage.RateVersion != nil {
			out["storage_rate_version"] = *o.Storage.RateVersion
		}
		if o.StorageUSD != nil {
			out["storage_usd"] = *o.StorageUSD
		} else {
			out["storage_usd"] = nil
		}
		out["commission_source"] = o.Referral.Source
		if o.Referral.Category != nil {
			out["commission_category"] = *o.Referral.Category
		}
		if o.Referral.CategoryLabel != nil {
			out["commission_category_label"] = *o.Referral.CategoryLabel
		}
		if o.Referral.RuleVersion != nil {
			out["commission_rule_version"] = *o.Referral.RuleVersion
		}
		if o.Referral.Rate != nil {
			out["commission_rate"] = *o.Referral.Rate
		}
		if o.Referral.MinimumUSD != nil {
			out["commission_minimum_usd"] = *o.Referral.MinimumUSD
		}
		if o.Referral.FeeUSD != nil {
			out["commission_usd"] = *o.Referral.FeeUSD
		}
		if o.CommissionUSD != nil {
			out["commission_usd"] = *o.CommissionUSD
		}
		if o.VolumetricWeightKg != nil {
			out["volumetric_weight_kg"] = *o.VolumetricWeightKg
		}
		if o.BillableWeightKg != nil {
			out["billable_weight_kg"] = *o.BillableWeightKg
		}
		if o.FreightCNY != nil {
			out["freight_cny"] = *o.FreightCNY
		}
		if o.FreightMissing != nil {
			out["freight_missing"] = *o.FreightMissing
		}
	}
	return out
}

func (o ProfitOutput) MarshalJSON() ([]byte, error) {
	return json.Marshal(o.ToMap())
}
