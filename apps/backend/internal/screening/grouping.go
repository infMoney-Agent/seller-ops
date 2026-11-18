package screening

import (
	"regexp"
	"strings"
)

func groupDirection(p, config Object) (string, string) {
	data := normalConfig
	title := strings.ToLower(str(p["title"]))
	category := str(p["category"])
	base := ""
	if grouping, ok := config["grouping"].(map[string]any); ok {
		if rules, ok := grouping["custom_rules"].([]any); ok {
			for _, raw := range rules {
				r, ok := raw.(map[string]any)
				if ok && matches(str(r["pattern"]), title+" "+category) {
					base = str(r["group"])
					break
				}
			}
		}
	}
	if base == "" {
		for _, cat := range strings.Split(strings.ReplaceAll(category, "\r", ""), "\n") {
			if name := data.Simple[strings.TrimSpace(cat)]; name != "" {
				base = name
				break
			}
		}
	}
	if base == "" {
		switch {
		case matches(`cheese\s?cloth`, title):
			base = "食品过滤纱布"
		case matches(`(vacuum|compression).*\bbags?\b`, title):
			base = "衣物真空压缩袋"
		case matches(`\btrash bags?\b`, title):
			base = "垃圾袋"
		default:
			base = strings.ReplaceAll(category, "\n", " / ")
		}
	}
	material := "材质未注明"
	preferred := str(p["material"])
	if preferred == "" {
		if specs, ok := p["specs"].(map[string]any); ok {
			attributes := regexp.MustCompile(`(?i)(?:^|\|)\s*(?:material|fabric type|frame material|blade material|bristle material):([^|]*)`).FindStringSubmatch(str(specs["详细参数"]))
			if len(attributes) > 1 {
				preferred = attributes[1]
			}
		}
	}
	for _, text := range []string{preferred, title} {
		for _, m := range data.Materials {
			if matches(m[1], text) {
				material = m[0]
				break
			}
		}
		if material != "材质未注明" {
			break
		}
	}
	fingerprint := title
	if brand := strings.ToLower(str(p["brand"])); len(brand) > 1 {
		fingerprint = strings.ReplaceAll(fingerprint, brand, " ")
	}
	structural := regexp.MustCompile(`\b(\d+)\s*[- ]?(compartment|tier|layer|ring|pocket|ply|blade|prong)s?\b`)
	fingerprint = structural.ReplaceAllStringFunc(fingerprint, func(s string) string {
		m := structural.FindStringSubmatch(s)
		digits := ""
		for _, digit := range m[1] {
			digits += string('a' + digit - '0')
		}
		return " structural" + m[2] + digits + " "
	})
	colors := regexp.MustCompile(`\b(?:black|white|blue|red|pink|green|yellow|purple|grey|gray|orange|brown|beige|ivory|teal|navy|silver|gold|clear|transparent|assorted|multicolor|colour|color|champagne)\b`)
	fingerprint = colors.ReplaceAllString(fingerprint, " ")
	quantities := regexp.MustCompile(`\b\d+(?:[.,/-]\d+)*(?:\s*[x×]\s*\d+(?:\.\d+)?)*(?:\s*(?:inch(?:es)?|in|ft|feet|cm|mm|ml|liters?|litres?|oz|ounces?|gallons?|yards?|yds?|packs?|counts?|ct|pcs?|pieces?|rolls?|sheets?|pages?|lb|lbs|sq|g|kg))?\b`)
	fingerprint = quantities.ReplaceAllString(fingerprint, " ")
	fingerprint = regexp.MustCompile(`\b(?:pack of|set of|count|pack|pcs|pieces|rolls|sheets|pages|size|large|small|medium|extra large|xl|xxl)\b`).ReplaceAllString(fingerprint, " ")
	fingerprint = strings.Join(regexp.MustCompile(`[a-z]+[a-z0-9]*|[0-9]+[a-z][a-z0-9]*`).FindAllString(fingerprint, -1), " ")
	if base == "" {
		base = "未校准方向"
	}
	if len(fingerprint) < 16 {
		fingerprint = str(p["asin"])
	}
	direction := base + " / " + material + " / " + fingerprint
	return direction, "用途、主体材质与保留结构词的标题精确归一；同卖家才归并"
}
func groupKey(p, result Object) string {
	seller := strings.ToUpper(strings.TrimSpace(str(p["seller_id"])))
	if !regexp.MustCompile(`^[A-Z0-9]{8,32}$`).MatchString(seller) {
		seller = "unknown:" + str(p["asin"])
	}
	return shaDigest([]byte(str(p["marketplace"]) + "|" + str(result["direction"]) + "|" + seller))
}
