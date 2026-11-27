package gormrepo

import (
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
)

// WithWorkspace 强制注入工作空间隔离条件，杜绝跨空间越权读写。
func WithWorkspace(workspaceID uint64) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		return db.Where(clause.Eq{
			Column: clause.Column{Name: "workspace_id"},
			Value:  workspaceID,
		})
	}
}

// WithKeyword 按 ASIN、父体 ASIN、中英文标题或品牌做模糊筛选。
func WithKeyword(keyword string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		kw := strings.TrimSpace(keyword)
		if kw == "" {
			return db
		}
		pattern := "%" + kw + "%"
		return db.Where(clause.Or(
			clause.Like{Column: clause.Column{Name: "asin"}, Value: pattern},
			clause.Like{Column: clause.Column{Name: "parent_asin"}, Value: pattern},
			clause.Like{Column: clause.Column{Name: "title_zh"}, Value: pattern},
			clause.Like{Column: clause.Column{Name: "title_en"}, Value: pattern},
			clause.Like{Column: clause.Column{Name: "brand"}, Value: pattern},
		))
	}
}

// WithBrandCategory 按商标分类过滤。
func WithBrandCategory(brandCategory string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		v := strings.TrimSpace(brandCategory)
		if v == "" {
			return db
		}
		return db.Where(clause.Eq{Column: clause.Column{Name: "brand_category"}, Value: v})
	}
}

// WithMachineResult 按机器初筛结论过滤（pending / passed / rejected）。
func WithMachineResult(result string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		v := strings.TrimSpace(result)
		if v == "" {
			return db
		}
		return db.Where(clause.Eq{Column: clause.Column{Name: "machine_result"}, Value: v})
	}
}

// WithManualStatus 按人工复核结论过滤（pending / approved / rejected）。
func WithManualStatus(status string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		v := strings.TrimSpace(status)
		if v == "" {
			return db
		}
		return db.Where(clause.Eq{Column: clause.Column{Name: "manual_status"}, Value: v})
	}
}

// WithProfitCalcStatus 按利润测算状态过滤（pending / calculated）。
func WithProfitCalcStatus(status string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		v := strings.TrimSpace(status)
		if v == "" {
			return db
		}
		return db.Where(clause.Eq{Column: clause.Column{Name: "profit_calc_status"}, Value: v})
	}
}

// WithPriceRange 按最新售价区间过滤。
func WithPriceRange(minPrice, maxPrice *float64) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if minPrice != nil {
			db = db.Where(clause.Gte{Column: clause.Column{Name: "latest_price"}, Value: *minPrice})
		}
		if maxPrice != nil {
			db = db.Where(clause.Lte{Column: clause.Column{Name: "latest_price"}, Value: *maxPrice})
		}
		return db
	}
}

// WithSalesRange 按最新销量区间过滤。
func WithSalesRange(minSales, maxSales *int) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if minSales != nil {
			db = db.Where(clause.Gte{Column: clause.Column{Name: "latest_sales"}, Value: *minSales})
		}
		if maxSales != nil {
			db = db.Where(clause.Lte{Column: clause.Column{Name: "latest_sales"}, Value: *maxSales})
		}
		return db
	}
}

// WithParentFold 控制父子体折叠视图或按指定父体 ASIN 展开子体。
func WithParentFold(parentOnly bool, parentASIN string) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		if p := strings.TrimSpace(parentASIN); p != "" {
			return db.Where(clause.Or(clause.Eq{Column: clause.Column{Name: "parent_asin"}, Value: p}, clause.Eq{Column: clause.Column{Name: "asin"}, Value: p}))
		}
		if parentOnly {
			// Group the filtered candidates, including families with no physical parent row.
			ranked := db.Session(&gorm.Session{}).Select("id, ROW_NUMBER() OVER (PARTITION BY CASE WHEN parent_asin='' THEN CONCAT('item:',id) ELSE CONCAT('parent:',parent_asin) END ORDER BY source_family_rank,id) AS family_rank")
			sub := db.Session(&gorm.Session{NewDB: true}).Table("(?) AS ranked", ranked).Select("id").Where("family_rank = ?", 1)
			return db.Where("id IN (?)", sub)
		}
		return db
	}
}

// WithMonthlyMetricFilter 通过 GORM 子查询按指定月份（YYYY-MM）及当月售价/销量区间筛选商品，不拼接原生 SQL。
func WithMonthlyMetricFilter(workspaceID uint64, statMonth string, minSales, maxSales *int, minPrice, maxPrice *float64) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		month := strings.TrimSpace(statMonth)
		if month == "" {
			return db
		}
		sub := db.Session(&gorm.Session{NewDB: true}).
			Model(&model.ScreeningItemMonthlyMetric{}).
			Select("item_id").
			Scopes(WithWorkspace(workspaceID)).
			Where(clause.Eq{Column: clause.Column{Name: "stat_month"}, Value: month})

		if minSales != nil {
			sub = sub.Where(clause.Gte{Column: clause.Column{Name: "monthly_sales"}, Value: *minSales})
		}
		if maxSales != nil {
			sub = sub.Where(clause.Lte{Column: clause.Column{Name: "monthly_sales"}, Value: *maxSales})
		}
		if minPrice != nil {
			sub = sub.Where(clause.Gte{Column: clause.Column{Name: "selling_price"}, Value: *minPrice})
		}
		if maxPrice != nil {
			sub = sub.Where(clause.Lte{Column: clause.Column{Name: "selling_price"}, Value: *maxPrice})
		}
		return db.Where("id IN (?)", sub)
	}
}

// WithPagination 统一分页 Scope。
func WithPagination(page, pageSize int) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		p, n := normalizePagination(page, pageSize)
		return db.Offset((p - 1) * n).Limit(n)
	}
}

var allowedSortColumns = map[string]string{
	"id":               "id",
	"asin":             "asin",
	"latest_price":     "latest_price",
	"latest_sales":     "latest_sales",
	"rating":           "rating",
	"review_count":     "review_count",
	"profit_margin":    "profit_margin",
	"estimated_profit": "estimated_profit",
	"created_at":       "created_at",
	"updated_at":       "updated_at",
}

// WithSort 白名单排序 Scope，防止任意列名注入。
func WithSort(sortBy string, desc bool) func(*gorm.DB) *gorm.DB {
	return func(db *gorm.DB) *gorm.DB {
		col, ok := allowedSortColumns[strings.ToLower(strings.TrimSpace(sortBy))]
		if !ok {
			col = "id"
		}
		return db.Clauses(clause.OrderBy{
			Columns: []clause.OrderByColumn{
				{Column: clause.Column{Name: col}, Desc: desc},
				{Column: clause.Column{Name: "id"}, Desc: desc},
			},
		})
	}
}

func normalizePagination(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}
	return page, pageSize
}
