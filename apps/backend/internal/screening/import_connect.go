package screening

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		case 413:
			code, reason = connect.CodeResourceExhausted, "PAYLOAD_TOO_LARGE"
		case 422:
			code, reason = connect.CodeInvalidArgument, "INVALID_ARGUMENT"
		}
	}
	var pg *pgconn.PgError
	if errors.As(err, &pg) && (pg.Code == "23505" || pg.Code == "23514" || pg.Code == "23503") {
		code, message, reason = connect.CodeAborted, str(workError(err)["message"]), "CONFLICT"
	}
	if errors.Is(err, pgx.ErrNoRows) {
		code, message, reason = connect.CodeNotFound, "资源不存在", "NOT_FOUND"
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

func rpcActor(ctx context.Context) (identity, error) {
	a, ok := ctx.Value(identityKey{}).(identity)
	if !ok {
		return a, fail(401, "身份签名无效")
	}
	return a, nil
}
func rpcStruct(row Object) (*structpb.Struct, error) {
	m, err := importStructMap(row)
	if err != nil {
		return nil, err
	}
	return structpb.NewStruct(m)
}
func (s *service) GetImport(ctx context.Context, req *connect.Request[screeningv1.GetImportRequest]) (*connect.Response[screeningv1.GetImportResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	row, err := s.getImport(ctx, a, req.Msg.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	item, err := rpcImport(importRecord(row))
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&screeningv1.GetImportResponse{Item: item, LatestJobId: optionalImportString(row["latest_job_id"])}), nil
}

var importRowStates = map[string]screeningv1.ImportRowState{"valid": screeningv1.ImportRowState_IMPORT_ROW_STATE_VALID, "invalid": screeningv1.ImportRowState_IMPORT_ROW_STATE_INVALID, "committed": screeningv1.ImportRowState_IMPORT_ROW_STATE_COMMITTED}

func rpcImportRow(row Object) (*screeningv1.ImportPreviewRow, error) {
	state, ok := importRowStates[str(row["state"])]
	if !ok {
		return nil, errors.New("unknown import row state")
	}
	raw, err := rpcStruct(importObject(row["raw_data"]))
	if err != nil {
		return nil, err
	}
	extra := Object{}
	for k, v := range row {
		extra[k] = v
	}
	for _, k := range []string{"id", "import_id", "member_path", "sheet_name", "row_no", "product_id", "state", "raw_data"} {
		delete(extra, k)
	}
	legacy, err := rpcStruct(extra)
	if err != nil {
		return nil, err
	}
	return &screeningv1.ImportPreviewRow{Id: str(row["id"]), ImportId: str(row["import_id"]), MemberPath: str(row["member_path"]), SheetName: str(row["sheet_name"]), RowNo: int32(integer(row["row_no"])), ProductId: optionalImportString(row["product_id"]), State: state, RawData: raw, LegacyFields: legacy}, nil
}
func (s *service) ListImportRows(ctx context.Context, req *connect.Request[screeningv1.ListImportRowsRequest]) (*connect.Response[screeningv1.ListImportRowsResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.importRows(ctx, a, req.Msg.ImportId, int(req.Msg.GetPagination().GetPage()), int(req.Msg.GetPagination().GetPageSize()))
	if err != nil {
		return nil, rpcError(err)
	}
	result := value.(Object)
	out := &screeningv1.ListImportRowsResponse{Items: []*screeningv1.ImportPreviewRow{}, Pagination: &commonv1.PaginationResponse{Page: int32(result["page"].(int)), PageSize: int32(result["page_size"].(int)), Total: result["total"].(int64)}}
	for _, row := range result["items"].([]Object) {
		item, err := rpcImportRow(row)
		if err != nil {
			return nil, rpcError(err)
		}
		out.Items = append(out.Items, item)
	}
	return connect.NewResponse(out), nil
}

var jobStates = map[string]screeningv1.JobState{"queued": screeningv1.JobState_JOB_STATE_QUEUED, "running": screeningv1.JobState_JOB_STATE_RUNNING, "succeeded": screeningv1.JobState_JOB_STATE_SUCCEEDED, "partial": screeningv1.JobState_JOB_STATE_PARTIAL, "failed": screeningv1.JobState_JOB_STATE_FAILED, "preview": screeningv1.JobState_JOB_STATE_PREVIEW, "applied": screeningv1.JobState_JOB_STATE_APPLIED, "undone": screeningv1.JobState_JOB_STATE_UNDONE}

func (s *service) GetJob(ctx context.Context, req *connect.Request[screeningv1.GetJobRequest]) (*connect.Response[screeningv1.GetJobResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	row, err := s.getPublicJob(ctx, a, req.Msg.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	state, ok := jobStates[str(row["state"])]
	if !ok {
		return nil, rpcError(errors.New("unknown task state"))
	}
	extra := Object{}
	for k, v := range row {
		extra[k] = v
	}
	for _, k := range []string{"id", "kind", "state", "progress", "attempts"} {
		delete(extra, k)
	}
	legacy, err := rpcStruct(extra)
	if err != nil {
		return nil, rpcError(err)
	}
	created, err := importTimestamp(str(row["created_at"]))
	if err != nil {
		return nil, rpcError(err)
	}
	updated, err := importTimestamp(str(row["updated_at"]))
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&screeningv1.GetJobResponse{Item: &screeningv1.JobRecord{Id: str(row["id"]), Kind: str(row["kind"]), State: state, Progress: int32(integer(row["progress"])), Attempts: int32(integer(row["attempts"])), CreatedAt: created, UpdatedAt: updated, LegacyFields: legacy}}), nil
}
