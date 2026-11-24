package screening

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strings"
)

// Source: product-library-site/public/profit-referral.mjs, checked against
// https://sell.amazon.com/pricing on 2026-11-22.
//
//go:embed data/referral_rules.json
var referralRulesJSON []byte

type referralRule struct {
	Label    string       `json:"label"`
	Tiers    [][]*float64 `json:"tiers"`
	Portion  bool         `json:"portion"`
	Minimum  string       `json:"minimum"`
	Aliases  []string     `json:"aliases"`
	Specific bool         `json:"specific"`
}

var referralRules = func() map[string]referralRule {
	var rules map[string]referralRule
	if err := json.Unmarshal(referralRulesJSON, &rules); err != nil {
		panic(err)
	}
	return rules
}()
var referralPathSeparator = regexp.MustCompile(`[:：>›\n/|]+`)

func referralNormalize(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

var referralAliases = func() map[string]string {
	aliases := map[string]string{}
	for key, rule := range referralRules {
		for _, alias := range rule.Aliases {
			aliases[referralNormalize(alias)] = key
		}
	}
	return aliases
}()

func inferReferral(p Object) string {
	aliases := referralAliases
	for _, field := range []string{"category_path", "category", "main_category"} {
		var first, specific string
		for _, node := range referralPathSeparator.Split(str(p[field]), -1) {
			key := aliases[referralNormalize(node)]
			if key == "" {
				continue
			}
			if first == "" {
				first = key
			}
			if referralRules[key].Specific {
				specific = key
			}
		}
		if specific != "" {
			return specific
		}
		if first != "" {
			return first
		}
	}
	return ""
}
func referralOptions() []Object {
	items := []Object{}
	for key, rule := range referralRules {
		items = append(items, Object{"id": key, "label": rule.Label})
	}
	sort.Slice(items, func(i, j int) bool { return str(items[i]["id"]) < str(items[j]["id"]) })
	return items
}
func validateReferralInputs(in Object) error {
	if mode := str(in["commission_mode"]); mode != "" && mode != "auto" && mode != "manual" {
		return fmt.Errorf("佣金模式无效")
	}
	if key := str(in["commission_category"]); key != "" {
		if _, ok := referralRules[key]; !ok {
			return fmt.Errorf("佣金类目无效")
		}
	}
	return nil
}
func referralTierRate(tier []*float64) *big.Rat { return big.NewRat(int64(*tier[1]), 100) }
func referralFeeFor(rule referralRule, price *big.Rat) (rate, fee *big.Rat) {
	lower, total := big.NewRat(0, 1), big.NewRat(0, 1)
	for _, tier := range rule.Tiers {
		r := referralTierRate(tier)
		upper := price
		if tier[0] != nil {
			upper = big.NewRat(int64(*tier[0]), 1)
		}
		if !rule.Portion {
			if price.Cmp(upper) <= 0 {
				total = mul(price, r)
				break
			}
		} else {
			end := price
			if end.Cmp(upper) > 0 {
				end = upper
			}
			if end.Cmp(lower) > 0 {
				total = add(total, mul(sub(end, lower), r))
			}
			if price.Cmp(upper) <= 0 {
				break
			}
		}
		lower = upper
	}
	rate = div(total, price)
	minimum, _ := new(big.Rat).SetString(rule.Minimum)
	fee = new(big.Rat).Set(total)
	if fee.Cmp(minimum) < 0 {
		fee = minimum
	}
	return
}

// Resolve only explicit known categories. Unknown browse paths remain unfilled.
func resolveReferral(in, p Object) (Object, *big.Rat) {
	meta := Object{"commission_source": "manual"}
	if str(in["commission_mode"]) == "" {
		if str(in["commission_rate"]) == "" {
			in["commission_mode"] = "auto"
		} else {
			in["commission_mode"] = "manual"
		}
	}
	if str(in["commission_mode"]) != "auto" {
		return meta, nil
	}
	key := str(in["commission_category"])
	if key == "" {
		key = inferReferral(p)
	}
	in["commission_rate"] = nil
	meta["commission_source"] = "unmatched"
	rule, ok := referralRules[key]
	if !ok {
		return meta, nil
	}
	meta["commission_category"] = key
	meta["commission_category_label"] = rule.Label
	meta["commission_rule_version"] = "us-referral-2026-11-22"
	meta["commission_source"] = "auto"
	price, pok := new(big.Rat).SetString(str(in["sale_price_usd"]))
	discount, dok := new(big.Rat).SetString(str(in["discount_usd"]))
	if !pok {
		return meta, nil
	}
	if !dok {
		discount = big.NewRat(0, 1)
	}
	price = sub(price, discount)
	if price.Sign() <= 0 {
		return meta, nil
	}
	rate, fee := referralFeeFor(rule, price)
	in["commission_rate"] = rate.FloatString(6)
	meta["commission_rate"] = rate.FloatString(6)
	meta["commission_minimum_usd"] = rule.Minimum
	meta["commission_usd"] = fee.FloatString(2)
	return meta, div(fee, price)
}

// Solve each price bracket and minimum-fee branch, then verify the candidate
// against the real schedule. No frozen-price blended rate is used for break-even.
func referralBreakEven(rule referralRule, f, ad *big.Rat) *big.Rat {
	var best *big.Rat
	lower, accrued := big.NewRat(0, 1), big.NewRat(0, 1)
	minimum, _ := new(big.Rat).SetString(rule.Minimum)
	check := func(candidate, upper *big.Rat) {
		scaled := mul(candidate, big.NewRat(100, 1))
		cents := new(big.Int).Quo(scaled.Num(), scaled.Denom())
		if new(big.Rat).SetInt(cents).Cmp(scaled) < 0 {
			cents.Add(cents, big.NewInt(1))
		}
		price := div(new(big.Rat).SetInt(cents), big.NewRat(100, 1))
		if price.Cmp(lower) <= 0 {
			price = add(lower, big.NewRat(1, 100))
		}
		if price.Sign() <= 0 || (upper != nil && price.Cmp(upper) > 0) {
			return
		}
		_, fee := referralFeeFor(rule, price)
		if sub(sub(mul(price, sub(big.NewRat(1, 1), ad)), fee), f).Sign() >= 0 && (best == nil || price.Cmp(best) < 0) {
			best = price
		}
	}
	for _, tier := range rule.Tiers {
		r := referralTierRate(tier)
		var upper *big.Rat
		if tier[0] != nil {
			upper = big.NewRat(int64(*tier[0]), 1)
		}
		offset := big.NewRat(0, 1)
		if rule.Portion {
			offset = sub(accrued, mul(lower, r))
		}
		for _, branch := range [][2]*big.Rat{{r, offset}, {big.NewRat(0, 1), minimum}} {
			denom := sub(sub(big.NewRat(1, 1), ad), branch[0])
			if denom.Sign() > 0 {
				check(div(add(f, branch[1]), denom), upper)
			}
		}
		check(add(lower, big.NewRat(1, 100)), upper)
		if upper != nil {
			if rule.Portion {
				accrued = add(accrued, mul(sub(upper, lower), r))
			}
			lower = upper
		}
	}
	return best
}
