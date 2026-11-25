package screening

import (
	"net/url"
	"regexp"
	"strings"
	"time"
)

func validateFilters(values url.Values) error {
	for key, list := range values {
		for _, value := range list {
			if len([]rune(value)) > 2000 {
				return fail(422, "筛选值过长")
			}
			if value == "" {
				continue
			}
			switch key {
			case "stat_month":
				date, e := time.Parse("2006-01-02", value)
				if e != nil || date.Day() != 1 {
					return fail(422, "月份须为该月第一天日期")
				}
			case "period_type":
				if value != "calendar_month" && value != "rolling_30d" && value != "custom" {
					return fail(422, "统计口径无效")
				}
			case "trademark":
				if value != "pending" && !trademarkCodePattern.MatchString(value) {
					return fail(422, "商标分类无效")
				}
			case "source_type":
				if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`).MatchString(value) {
					return fail(422, "数据来源无效")
				}
			case "import_id":
				if !uuidPattern.MatchString(value) {
					return fail(422, "批次ID无效")
				}
			case "child_sales_min":
				if _, err := parseNonnegativeInt(value); err != nil {
					return fail(422, "最低子体销量必须为非负整数")
				}
			case "profit_min", "profit_max":
				if !regexp.MustCompile(`^-?[0-9]{1,12}(\.[0-9]{1,6})?$`).MatchString(value) {
					return fail(422, "利润区间无效")
				}
			case "review_status":
				if !strings.Contains("|pending|kept|excluded|unsure|", "|"+value+"|") {
					return fail(422, "人工状态无效")
				}
			case "machine":
				if !strings.Contains("|passed|excluded|insufficient|pending|unscreened|", "|"+value+"|") {
					return fail(422, "机器状态无效")
				}
			case "recommendation":
				if value != "regular" && value != "exploration" {
					return fail(422, "推荐类型无效")
				}
			case "has_reference", "show_merged":
				if value != "true" && value != "false" {
					return fail(422, "布尔筛选值无效")
				}
			case "page", "page_size", "sort", "q", "category", "seller_region", "direction":
			default:
				return fail(422, "不支持的筛选字段: "+key)
			}
		}
	}
	if min, max := values.Get("profit_min"), values.Get("profit_max"); min != "" && max != "" && ratCompare(min, max) > 0 {
		return fail(422, "利润区间最小值大于最大值")
	}
	return nil
}
