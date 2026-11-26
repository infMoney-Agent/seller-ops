package screening

import (
	"connectrpc.com/connect"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	commonv1 "seller-ops/contracts/sellerops/common/v1"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/rpccompat"
	"strings"
	"testing"
)

func TestDomainErrorTransportMappings(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantCode   connect.Code
		wantReason string
		wantHTTP   int
	}{
		{"unauthenticated", unauthenticated("未登录"), connect.CodeUnauthenticated, "UNAUTHENTICATED", http.StatusUnauthorized},
		{"forbidden", forbidden("无权限"), connect.CodePermissionDenied, "PERMISSION_DENIED", http.StatusForbidden},
		{"not_found", notFound("不存在"), connect.CodeNotFound, "NOT_FOUND", http.StatusNotFound},
		{"conflict", conflict("版本冲突"), connect.CodeAborted, "CONFLICT", http.StatusConflict},
		{"payload_too_large", payloadTooLarge("超限"), connect.CodeResourceExhausted, "PAYLOAD_TOO_LARGE", http.StatusRequestEntityTooLarge},
		{"validation", invalid("参数错误"), connect.CodeInvalidArgument, "INVALID_ARGUMENT", http.StatusUnprocessableEntity},
		{"internal", internalFailure("内部失败"), connect.CodeInternal, "INTERNAL", http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rpcErr := rpcError(tc.err)
			if rpcErr.Code() != tc.wantCode {
				t.Fatalf("rpc code = %v, want %v", rpcErr.Code(), tc.wantCode)
			}
			details := rpcErr.Details()
			if len(details) != 1 {
				t.Fatalf("expected 1 error detail, got %d", len(details))
			}
			msg, err := details[0].Value()
			if err != nil {
				t.Fatal(err)
			}
			detail, ok := msg.(*commonv1.ErrorDetail)
			if !ok || detail.Reason != tc.wantReason || detail.RequestId == "" {
				t.Fatalf("unexpected error detail: %#v", msg)
			}

			rec := httptest.NewRecorder()
			respond(rec, nil, tc.err)
			if rec.Code != tc.wantHTTP {
				t.Fatalf("http status = %d, want %d", rec.Code, tc.wantHTTP)
			}
		})
	}
}

func TestDomainCommandValidation(t *testing.T) {
	if err := parseReviewCommand(Object{"status": "kept"}).validate(); err != nil {
		t.Fatalf("kept without reason should pass: %v", err)
	}
	if err := parseReviewCommand(Object{"status": "excluded", "reason": "  "}).validate(); err == nil {
		t.Fatal("excluded without reason should fail")
	}
	if err := parseReviewCommand(Object{"status": "invalid"}).validate(); err == nil {
		t.Fatal("invalid review decision should fail")
	}
	if err := parseCategoryCommand(Object{"category": "  收纳袋  "}).validate(); err != nil {
		t.Fatalf("valid category failed: %v", err)
	}
	if err := parseCategoryCommand(Object{"category": strings.Repeat("字", 201)}).validate(); err == nil {
		t.Fatal("overlong category should fail")
	}

	params := parseParametersCommand(Object{
		"expected_data_revision": json.Number("3"),
		"parent_asin":            "B000123456",
		"material":               nil,
		"package_weight_g":       "120.5",
		"units_per_pack":         json.Number("4"),
	})
	if params.ExpectedDataRevision == nil || *params.ExpectedDataRevision != 3 {
		t.Fatalf("unexpected revision: %#v", params.ExpectedDataRevision)
	}
	set, args, err := params.buildUpdate("updated_at=now()", []any{int64(1), "pid", int64(2)})
	if err != nil {
		t.Fatalf("valid parameters failed: %v", err)
	}
	if !strings.Contains(set, ",parent_asin=$4") || !strings.Contains(set, ",material=$5") || !strings.Contains(set, ",package_weight_g=$6") || !strings.Contains(set, ",units_per_pack=$7") {
		t.Fatalf("unexpected SQL set clause: %s", set)
	}
	if args[4] != nil || args[6] != int64(4) {
		t.Fatalf("unexpected SQL args: %#v", args)
	}

	for name, body := range map[string]Object{
		"unknown-key": {"unknown_column": "1"},
		"bad-asin":    {"parent_asin": "bad"},
		"bad-decimal": {"weight_g": "-1"},
		"bad-pack":    {"units_per_pack": "1.5"},
		"zero-pack":   {"carton_units": 0},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := parseParametersCommand(body).buildUpdate("updated_at=now()", nil); err == nil {
				t.Fatal("expected validation failure")
			}
		})
	}

	ids, err := parsePreviewBatchCommand(Object{"product_ids": []any{"p1", "p2"}, "scope": "selected", "action": "review"}).validateInitial()
	if err != nil || len(ids) != 2 {
		t.Fatalf("valid preview command failed: %v %v", ids, err)
	}
	if _, err := parsePreviewBatchCommand(Object{"product_ids": []any{"p1", "p1"}}).validateInitial(); err == nil {
		t.Fatal("duplicate product_ids should fail")
	}
	if _, err := parsePreviewBatchCommand(Object{"product_ids": []any{"p1"}, "overwrite": "yes"}).validateInitial(); err == nil {
		t.Fatal("non-boolean overwrite should fail")
	}
}

func TestTypedBatchShapesRoundTripViaRPCCompat(t *testing.T) {
	payload := &ReviewBatchPayload{
		ID:        "11111111-1111-1111-1111-111111111111",
		ExpiresAt: "2026-11-26T12:00:00Z",
		Scope:     BatchScopeSelected,
		Action:    BatchActionReview,
		Overwrite: false,
		Status:    string(ReviewExcluded),
		Reason:    "材质不符",
		Note:      "",
		Category:  nil,
		Targets: []BatchTargetSnapshot{{
			ID:             "22222222-2222-2222-2222-222222222222",
			ASIN:           "B000000001",
			Title:          "Test Product",
			Eligible:       true,
			SkipReason:     "",
			DataRevision:   1,
			ReviewRevision: 0,
			ReviewStatus:   "pending",
			ParentASIN:     nil,
			Status:         "excluded",
			Reason:         "材质不符",
			Note:           "",
			Category:       nil,
		}},
	}
	previewResp := &screeningv1.CreateReviewPreviewResponse{}
	if err := rpccompat.FromLegacy(previewResp, Object{"data": publicJSON(payload)}); err != nil {
		t.Fatalf("CreateReviewPreviewResponse FromLegacy failed: %v", err)
	}
	restored, err := rpccompat.Legacy(previewResp)
	if err != nil {
		t.Fatal(err)
	}
	data := restored["data"].(map[string]any)
	if data["id"] != payload.ID || data["category"] != nil {
		t.Fatalf("unexpected preview roundtrip: %#v", data)
	}

	applyRes := &BatchApplyResult{
		ID:      payload.ID,
		Success: 1,
		Skipped: 1,
		Items: []BatchApplyItem{
			{ID: "22222222-2222-2222-2222-222222222222", Success: true},
			{ID: "33333333-3333-3333-3333-333333333333", Success: false, Reason: "已有人工结论，请勾选覆盖后重试"},
		},
	}
	applyResp := &screeningv1.ApplyReviewBatchResponse{}
	if err := rpccompat.FromLegacy(applyResp, Object{"data": publicJSON(applyRes)}); err != nil {
		t.Fatalf("ApplyReviewBatchResponse FromLegacy failed: %v", err)
	}
}
