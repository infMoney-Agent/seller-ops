package screening

import (
	"connectrpc.com/connect"
	"context"
	"google.golang.org/protobuf/types/known/structpb"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"strings"
)

// rpcInput restores the same JSON number representation used by readBody.
func rpcInput(value *structpb.Struct) Object {
	if value == nil {
		return Object{}
	}
	return decodeObject(encoded(value.AsMap()))
}
func prepareImportMutation(ctx context.Context, id, method, suffix string, meta *screeningv1.ImportMutationMetadata, body Object) (identity, error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return a, err
	}
	if err = validImportResourceID(id); err != nil {
		return a, err
	}
	if method != "DELETE" {
		if len(encoded(body)) > 256*1024 {
			return a, fail(413, "JSON 请求超过256KB")
		}
		if err = validateMutationBody(body); err != nil {
			return a, err
		}
	}
	if meta == nil || meta.IdempotencyKey == "" || len(meta.IdempotencyKey) > 200 {
		return a, fail(422, "修改请求需要Idempotency-Key")
	}
	query := meta.OriginalQuery
	if len(query) > 8192 || (query != "" && !strings.HasPrefix(query, "?")) || strings.ContainsAny(query, "\r\n#") {
		return a, fail(422, "请求参数无效")
	}
	a.RequestKey = meta.IdempotencyKey
	uri := "/api/v1/screening/" + suffix + query
	if method == "DELETE" {
		a.RequestHash = shaDigest([]byte(method + " " + uri))
	} else {
		a.RequestHash = shaDigest(append([]byte(method+" "+uri+"\n"), encoded(body)...))
	}
	return a, nil
}
func settingsBody(settings *screeningv1.ImportSettings) (Object, error) {
	if settings == nil {
		return Object{}, nil
	}
	b := rpcInput(settings.LegacyFields)
	fields := map[string]*string{"source_period": settings.SourcePeriod, "observed_on": settings.ObservedOn, "source_type": settings.SourceType, "period_type": settings.PeriodType, "period_start": settings.PeriodStart, "period_end": settings.PeriodEnd, "default_sales_scope": settings.DefaultSalesScope}
	for k, v := range fields {
		if v != nil {
			b[k] = *v
		}
	}
	if settings.Mapping != nil {
		b["mapping"] = rpcInput(settings.Mapping)
	}
	for _, key := range settings.ClearFields {
		if _, ok := fields[key]; !ok && key != "mapping" {
			return nil, fail(422, "清空字段无效")
		}
		b[key] = nil
	}
	return b, nil
}
func rpcImportSubmission(value any) (*screeningv1.ImportJobSubmission, error) {
	row, ok := value.(map[string]any)
	if !ok {
		return nil, fail(500, "任务结果无效")
	}
	extra := Object{}
	for k, v := range row {
		extra[k] = v
	}
	for _, k := range []string{"job_id", "state", "import_id"} {
		delete(extra, k)
	}
	legacy, err := rpcStruct(extra)
	if err != nil {
		return nil, err
	}
	return &screeningv1.ImportJobSubmission{JobId: str(row["job_id"]), State: str(row["state"]), ImportId: optionalImportString(row["import_id"]), LegacyFields: legacy}, nil
}
func (s *service) GetImportRemovalPreview(ctx context.Context, req *connect.Request[screeningv1.GetImportRemovalPreviewRequest]) (*connect.Response[screeningv1.GetImportRemovalPreviewResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	if err = validImportResourceID(req.Msg.Id); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.importRemovalPreview(ctx, a, req.Msg.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	row := value.(Object)
	return connect.NewResponse(&screeningv1.GetImportRemovalPreviewResponse{Products: integer(row["products"]), ExclusiveProducts: integer(row["exclusive_products"]), SourceRows: integer(row["source_rows"])}), nil
}
func (s *service) CommitImport(ctx context.Context, req *connect.Request[screeningv1.CommitImportRequest]) (*connect.Response[screeningv1.CommitImportResponse], error) {
	b := rpcInput(req.Msg.LegacyFields)
	a, err := prepareImportMutation(ctx, req.Msg.Id, "POST", "imports/"+req.Msg.Id+"/commit", req.Msg.Mutation, b)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.editImport(ctx, a, req.Msg.Id, b, true)
	if err != nil {
		return nil, rpcError(err)
	}
	submission, err := rpcImportSubmission(value)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&screeningv1.CommitImportResponse{Submission: submission}), nil
}
func (s *service) UpdateImportSettings(ctx context.Context, req *connect.Request[screeningv1.UpdateImportSettingsRequest]) (*connect.Response[screeningv1.UpdateImportSettingsResponse], error) {
	b, err := settingsBody(req.Msg.Settings)
	if err != nil {
		return nil, rpcError(err)
	}
	a, err := prepareImportMutation(ctx, req.Msg.Id, "PUT", "imports/"+req.Msg.Id+"/mapping", req.Msg.Mutation, b)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.editImport(ctx, a, req.Msg.Id, b, false)
	if err != nil {
		return nil, rpcError(err)
	}
	submission, err := rpcImportSubmission(value)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&screeningv1.UpdateImportSettingsResponse{Submission: submission}), nil
}
func (s *service) RetryImportJob(ctx context.Context, req *connect.Request[screeningv1.RetryImportJobRequest]) (*connect.Response[screeningv1.RetryImportJobResponse], error) {
	b := rpcInput(req.Msg.LegacyFields)
	a, err := prepareImportMutation(ctx, req.Msg.Id, "POST", "jobs/"+req.Msg.Id+"/retry", req.Msg.Mutation, b)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.retryJob(ctx, a, req.Msg.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	submission, err := rpcImportSubmission(value)
	if err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(&screeningv1.RetryImportJobResponse{Submission: submission}), nil
}
func (s *service) DeleteImport(ctx context.Context, req *connect.Request[screeningv1.DeleteImportRequest]) (*connect.Response[screeningv1.DeleteImportResponse], error) {
	a, err := prepareImportMutation(ctx, req.Msg.Id, "DELETE", "imports/"+req.Msg.Id, req.Msg.Mutation, nil)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.removeImportHistory(ctx, a, req.Msg.Id)
	if err != nil {
		return nil, rpcError(err)
	}
	row := value.(Object)
	out := &screeningv1.DeleteImportResponse{Id: str(row["id"]), Deleted: row["deleted"] == true}
	for key, dst := range map[string]**int64{"removed_products": &out.RemovedProducts, "retained_products": &out.RetainedProducts, "removed_monthly": &out.RemovedMonthly} {
		if row[key] != nil {
			n := integer(row[key])
			*dst = &n
		}
	}
	return connect.NewResponse(out), nil
}
