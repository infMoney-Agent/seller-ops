package screening

import (
	"regexp"
	"time"
)

var filenameMonthPattern = regexp.MustCompile(`(?:^|[^0-9])((?:19|20)[0-9]{2})[._-](0[1-9]|1[0-2])`)
var sourceMonthPattern = regexp.MustCompile(`^(?:19|20)[0-9]{2}-(0[1-9]|1[0-2])$`)
var importDayLocation = time.FixedZone("Asia/Shanghai", 8*60*60)

func sourcePeriodFromFilename(name string) string {
	months := map[string]bool{}
	for _, match := range filenameMonthPattern.FindAllStringSubmatchIndex(name, -1) {
		end := match[1]
		if end < len(name) && name[end] >= '0' && name[end] <= '9' {
			continue
		}
		months[name[match[2]:match[3]]+"-"+name[match[4]:match[5]]] = true
	}
	if len(months) != 1 {
		return ""
	}
	for month := range months {
		return month
	}
	return ""
}

// Explicit form values win. The source month and observation date have separate
// meanings: an old source month must never be replaced by today's month.
func applyImportDefaults(values Object, name string, now time.Time) {
	if str(values["observed_on"]) == "" {
		values["observed_on"] = now.In(importDayLocation).Format("2006-01-02")
	}
	if str(values["source_period"]) == "" && (str(values["period_type"]) == "" || values["period_type"] == "calendar_month") {
		values["source_period"] = sourcePeriodFromFilename(name)
	}
}

func pendingImportPeriod(b Object, kind, observed string) (Object, error) {
	source := str(b["source_type"])
	if source == "" {
		source = "sellersprite"
	}
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,50}$`).MatchString(source) {
		return nil, fail(422, "来源标识无效")
	}
	scope := str(b["default_sales_scope"])
	if scope == "" {
		scope = "unknown"
	}
	if scope != "unknown" && scope != "child" && scope != "parent" {
		return nil, fail(422, "销量口径无效")
	}
	return Object{"period_type": kind, "observed_on": nullableString(observed), "source_period": "", "period_start": nil, "period_end": nil, "source_type": source, "default_sales_scope": scope}, nil
}
