package screening

import (
	_ "embed"
	"encoding/json"
	"sort"
	"strings"
	"unicode"
)

//go:embed data/category_translations.json
var categoryTranslationData []byte
var categoryEnglishChinese, categoryChineseEnglish = loadCategoryTranslations()

func loadCategoryTranslations() (map[string]string, map[string]string) {
	var source map[string]string
	if e := json.Unmarshal(categoryTranslationData, &source); e != nil {
		panic(e)
	}
	en, zh := map[string]string{}, map[string]string{}
	keys := []string{}
	for key := range source {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := strings.TrimSpace(source[key])
		en[strings.ToLower(strings.Join(strings.Fields(key), " "))] = value
		if _, ok := zh[value]; !ok {
			zh[value] = key
		}
	}
	for key, value := range map[string]string{"家居与厨房": "Home & Kitchen", "厨房和餐厅": "Kitchen & Dining", "厨房用具和小工具": "Kitchen Utensils & Gadgets", "滤网": "Colanders", "滤网和食物滤网": "Colanders & Food Strainers", "未分类": "Uncategorized"} {
		zh[key] = value
	}
	return en, zh
}
func categoryLabel(raw string) string {
	name := strings.Join(strings.Fields(raw), " ")
	if name == "" {
		return "Uncategorized（未分类）"
	}
	english, chinese := name, ""
	at := strings.IndexFunc(name, func(r rune) bool { return unicode.Is(unicode.Han, r) })
	if at >= 0 {
		english = strings.TrimSpace(strings.TrimRight(strings.TrimSpace(name[:at]), "(（"))
		chinese = strings.Trim(strings.TrimSpace(name[at:]), ")） ")
		if english == "" {
			english = categoryChineseEnglish[chinese]
			if english == "" {
				return chinese
			}
		}
	}
	if chinese == "" {
		chinese = categoryEnglishChinese[strings.ToLower(english)]
	}
	if chinese == "" {
		return english
	}
	return english + "（" + chinese + "）"
}
func categoryDisplayPath(path []string) []string {
	labels := make([]string, len(path))
	for i, p := range path {
		labels[i] = categoryLabel(p)
	}
	return labels
}
