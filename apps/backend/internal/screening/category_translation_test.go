package screening

import "testing"

func TestCategoryTranslations(t *testing.T) {
	for _, tc := range [][2]string{{"Home & Kitchen", "Home & Kitchen（家居与厨房）"}, {"Home & Kitchen  家居与厨房", "Home & Kitchen（家居与厨房）"}, {"Kitchen & Dining（厨房和餐厅）", "Kitchen & Dining（厨房和餐厅）"}, {"厨房和餐厅", "Kitchen & Dining（厨房和餐厅）"}, {"未分类", "Uncategorized（未分类）"}, {"AA", "AA（五号电池）"}, {"Toothpaste", "Toothpaste（牙膏）"}, {"Curing Lamps", "Curing Lamps（美甲固化灯）"}, {"Acrylic Nail Kits", "Acrylic Nail Kits（水晶美甲套装）"}} {
		if got := categoryLabel(tc[0]); got != tc[1] {
			t.Fatalf("%q => %q want %q", tc[0], got, tc[1])
		}
	}
}
