package screening

import (
	"encoding/json"
	"reflect"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/rpccompat"
	"testing"
)

func TestBusinessProtoProjection(t *testing.T) {
	original := Object{"items": []Object{{"id": "product", "asin": "B0TEST0001", "review_status": "kept", "review_revision": json.Number("9007199254740993"), "price_usd": "19.990000", "title": "商品", "title_zh": nil, "family_count": json.Number("2"), "family_items": []Object{}, "product_length_cm": json.Number("13.97"), "profit_output": Object{"state": "ready", "formula_version": "v1", "margin": "0.123456", "roi": nil}, "specs": Object{"原始尺寸": "60 x 84 inches"}, "future_source": Object{"zero": 0, "empty": nil}}}, "total": json.Number("1"), "page": 1, "page_size": 50}
	msg := &screeningv1.PageProduct{}
	if err := rpccompat.FromLegacy(msg, original); err != nil {
		t.Fatal(err)
	}
	if msg.Items[0].GetReviewRevision() != 9007199254740993 {
		t.Fatal("int64 precision lost")
	}
	got, err := rpccompat.Legacy(msg)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	json.Unmarshal(encoded(original), &a)
	json.Unmarshal(encoded(got), &b)
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("roundtrip differs original=%s got=%s", encoded(original), encoded(got))
	}
}
