package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

func decodeObject(raw []byte) Object {
	var out Object
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	_ = dec.Decode(&out)
	return out
}
func shaDigest(raw []byte) string { sum := sha256.Sum256(raw); return hex.EncodeToString(sum[:]) }

type rule struct {
	label, pattern string
	mandatory      bool
}

func policyRules(config Object) ([]rule, error) {
	out := []rule{}
	if raw := config["exclusion_patterns"]; raw != nil {
		m, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("排除表达式必须是对象")
		}
		for label, p := range m {
			pattern, ok := p.(string)
			if !ok {
				return nil, fmt.Errorf("表达式必须是文本")
			}
			out = append(out, rule{label, pattern, false})
		}
	}
	for _, key := range []string{"extra_exclusions", "mandatory_exclusions"} {
		if config[key] == nil {
			continue
		}
		rows, ok := config[key].([]any)
		if !ok {
			return nil, fmt.Errorf("%s 必须是列表", key)
		}
		for _, row := range rows {
			pair, ok := row.([]any)
			if !ok || len(pair) != 2 {
				return nil, fmt.Errorf("规则需要名称和表达式")
			}
			label, ok := pair[0].(string)
			if !ok {
				return nil, fmt.Errorf("规则名称无效")
			}
			pattern, ok := pair[1].(string)
			if !ok {
				return nil, fmt.Errorf("表达式必须是文本")
			}
			out = append(out, rule{label, pattern, key == "mandatory_exclusions"})
		}
	}
	return out, nil
}
func validatePolicy(config Object) error {
	if len(encoded(config)) > 128*1024 {
		return fmt.Errorf("规则配置超过128KB")
	}
	for _, key := range []string{"overrides", "exploration_recommendations"} {
		if v := config[key]; v != nil && str(v) != "map[]" && str(v) != "[]" {
			return fmt.Errorf("不允许导入历史ASIN人工覆盖")
		}
	}
	if v := config["minimum_occurrences"]; v != nil {
		if integer(v) < 1 || integer(v) > 120 {
			return fmt.Errorf("来源期数必须在1到120之间")
		}
	}
	if v := config["exclude"]; v != nil {
		m, ok := v.(map[string]any)
		if !ok {
			return fmt.Errorf("排除开关必须是对象")
		}
		for _, enabled := range m {
			if _, ok := enabled.(bool); !ok {
				return fmt.Errorf("排除开关必须为布尔值")
			}
		}
	}
	rules, e := policyRules(config)
	if e != nil {
		return e
	}
	if len(rules) > 300 {
		return fmt.Errorf("最多300条规则")
	}
	for _, r := range rules {
		if len(r.pattern) > 2000 {
			return fmt.Errorf("表达式过长")
		}
		if _, e = regexp.Compile("(?i)" + r.pattern); e != nil {
			return fmt.Errorf("规则 %s 不符合 Go RE2 语法: %w", r.label, e)
		}
	}
	return nil
}
func matches(pattern, text string) bool {
	rx, e := regexp.Compile("(?i)" + pattern)
	return e == nil && rx.MatchString(text)
}
func classify(p, config Object) Object {
	title := str(p["title"])
	text := title + " " + str(p["category"])
	direction := str(p["category"])
	if direction == "" {
		direction = "未校准方向：" + str(p["asin"])
	}
	reasons := []string{}
	evidence := []Object{}
	decision := "passed"
	recommendation := "regular"
	if strings.TrimSpace(title) == "" {
		return Object{"decision": "insufficient", "recommendation": "regular", "direction": direction, "reasons": []string{"缺少标题"}, "evidence": evidence}
	}
	check := regexp.MustCompile(`(?i)\b(?:non[- ]electric|no electric|without electric|no led)\b`).ReplaceAllString(text, "manual")
	bag := matches(`(vacuum|compression).*bag|space saver bags`, text)
	pumpUnknown := bag && matches(`\bpump\b`, check) && !matches(`hand pump|manual pump|without.*pump|no pump|electric|battery|rechargeable`, check)
	container := matches(`\b(dispenser|holder|dish|rack|container|bottle)\b`, title) && !matches(`filled|with soap|with solution|included.*soap`, title)
	passive := matches(`replacement.*(?:heads?|bags?|filters?)|(?:heads?|bags?|filters?).*replacement`, title) && !matches(`with.*(?:motor|battery)|rechargeable`, title)
	rules, _ := policyRules(config)
	mandatory := false
	switches, _ := config["exclude"].(map[string]any)
	for _, r := range rules {
		if !r.mandatory {
			if enabled, ok := switches[r.label].(bool); ok && !enabled {
				continue
			}
			if r.label == "电器或电池" && passive {
				continue
			}
			if r.label == "化学制剂或含配方套装" && container {
				continue
			}
		}
		target := check
		if r.mandatory {
			target = title
		}
		if matches(r.pattern, target) {
			reasons = append(reasons, r.label)
			evidence = append(evidence, Object{"rule": r.label, "mandatory": r.mandatory})
			mandatory = mandatory || r.mandatory
		}
	}
	if len(reasons) > 0 {
		decision = "excluded"
	}
	if pumpUnknown && !mandatory {
		decision = "insufficient"
		reasons = append(reasons, "真空袋泵类型不明")
	}
	if len(reasons) > 0 && !mandatory && (container || passive) {
		recommendation = "exploration"
	}
	if len(reasons) == 0 {
		reasons = append(reasons, "符合当前规则")
	}
	return Object{"decision": decision, "recommendation": recommendation, "direction": direction, "reasons": reasons, "evidence": evidence}
}
