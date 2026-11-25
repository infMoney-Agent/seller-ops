package screening

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"net/http"
	commonv1 "seller-ops/contracts/sellerops/common/v1"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/contracts/sellerops/screening/v1/screeningv1connect"
	"time"
)

const ImportsRPCPrefix = "/sellerops.screening.v1.ScreeningImportService/"

var importStates = map[string]screeningv1.ImportState{"uploaded": screeningv1.ImportState_IMPORT_STATE_UPLOADED, "parsing": screeningv1.ImportState_IMPORT_STATE_PARSING, "preview": screeningv1.ImportState_IMPORT_STATE_PREVIEW, "importing": screeningv1.ImportState_IMPORT_STATE_IMPORTING, "completed": screeningv1.ImportState_IMPORT_STATE_COMPLETED, "partial": screeningv1.ImportState_IMPORT_STATE_PARTIAL, "failed": screeningv1.ImportState_IMPORT_STATE_FAILED}

func rpcError(err error) *connect.Error {
	code, message, reason := connect.CodeInternal, "初筛服务处理失败", "INTERNAL"
	var a apiError
	if errors.As(err, &a) {
		message = a.message
		switch a.status {
		case 401:
			code, reason = connect.CodeUnauthenticated, "UNAUTHENTICATED"
		case 403:
			code, reason = connect.CodePermissionDenied, "PERMISSION_DENIED"
		case 404:
			code, reason = connect.CodeNotFound, "NOT_FOUND"
		case 409:
			code, reason = connect.CodeAborted, "CONFLICT"
		case 422:
			code, reason = connect.CodeInvalidArgument, "INVALID_ARGUMENT"
		}
	}
	if errors.Is(err, context.Canceled) {
		code, message, reason = connect.CodeCanceled, "请求已取消", "CANCELED"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code, message, reason = connect.CodeDeadlineExceeded, "请求超时", "DEADLINE_EXCEEDED"
	}
	out := connect.NewError(code, errors.New(message))
	if detail, e := connect.NewErrorDetail(&commonv1.ErrorDetail{Reason: reason, RequestId: uuid()}); e == nil {
		out.AddDetail(detail)
	}
	return out
}
func importTimestamp(raw string) (*timestamppb.Timestamp, error) {
	if raw == "" {
		return nil, nil
	}
	v, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return nil, err
	}
	ts := timestamppb.New(v)
	return ts, ts.CheckValid()
}
func rpcImport(row ImportListRecord) (*screeningv1.ImportRecord, error) {
	state, ok := importStates[row.State]
	if !ok {
		return nil, errors.New("unknown import state")
	}
	counts, err := importStructMap(row.Counts)
	if err != nil {
		return nil, err
	}
	mapping, err := importStructMap(row.Mapping)
	if err != nil {
		return nil, err
	}
	extra, err := importStructMap(row.LegacyFields)
	if err != nil {
		return nil, err
	}
	c, err := structpb.NewStruct(counts)
	if err != nil {
		return nil, err
	}
	m, err := structpb.NewStruct(mapping)
	if err != nil {
		return nil, err
	}
	x, err := structpb.NewStruct(extra)
	if err != nil {
		return nil, err
	}
	created, err := importTimestamp(row.CreatedAt)
	if err != nil {
		return nil, err
	}
	committed, err := importTimestamp(row.CommittedAt)
	if err != nil {
		return nil, err
	}
	return &screeningv1.ImportRecord{Id: row.ID, FileName: row.FileName, State: state, Marketplace: row.Marketplace, SourcePeriod: row.SourcePeriod, ObservedOn: row.ObservedOn, SourceType: row.SourceType, PeriodType: row.PeriodType, PeriodStart: row.PeriodStart, PeriodEnd: row.PeriodEnd, DefaultSalesScope: row.DefaultSalesScope, Counts: c, Mapping: m, CreatedAt: created, CommittedAt: committed, LegacyFields: x}, nil
}
func (s *service) ListImports(ctx context.Context, req *connect.Request[screeningv1.ListImportsRequest]) (*connect.Response[screeningv1.ListImportsResponse], error) {
	a, ok := ctx.Value(identityKey{}).(identity)
	if !ok {
		return nil, rpcError(fail(401, "身份签名无效"))
	}
	p, n := req.Msg.GetPagination().GetPage(), req.Msg.GetPagination().GetPageSize()
	result, err := s.listImports(ctx, a, int(p), int(n))
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListImportsResponse{Items: []*screeningv1.ImportRecord{}, Pagination: &commonv1.PaginationResponse{Page: int32(result.Page), PageSize: int32(result.PageSize), Total: result.Total}}
	for _, row := range result.Items {
		item, err := rpcImport(row)
		if err != nil {
			return nil, rpcError(err)
		}
		out.Items = append(out.Items, item)
	}
	response := connect.NewResponse(out)
	response.Header().Set("Cache-Control", "no-store")
	return response, nil
}
func (s *service) registerImportsRPC(mux *http.ServeMux) {
	path, handler := screeningv1connect.NewScreeningImportServiceHandler(s, connect.WithReadMaxBytes(256*1024))
	writer := connect.NewErrorWriter()
	mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		a, err := verifyIdentity(r, s.cfg.Secret, time.Now())
		if err != nil {
			_ = writer.Write(w, r, rpcError(err))
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), identityKey{}, a))
		handler.ServeHTTP(w, r)
	}))
}
