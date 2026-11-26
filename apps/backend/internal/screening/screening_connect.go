package screening

import (
	"connectrpc.com/connect"
	"context"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/url"
	screeningv1 "seller-ops/contracts/sellerops/screening/v1"
	"seller-ops/contracts/sellerops/screening/v1/screeningv1connect"
	"seller-ops/rpccompat"
	"strings"
)

const ScreeningRPCPrefix = "/sellerops.screening.v1.ScreeningService/"

func screeningQueryValues(q *screeningv1.ScreeningQuery) url.Values {
	out := url.Values{}
	if q == nil {
		return out
	}
	msg := q.ProtoReflect()
	fields := msg.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if !f.IsList() {
			continue
		}
		list := msg.Get(f).List()
		for j := 0; j < list.Len(); j++ {
			out.Add(string(f.Name()), list.Get(j).String())
		}
	}
	for k, v := range q.Extra {
		out[k] = append(out[k], v.Values...)
	}
	return out
}
func decodeScreeningBody(body proto.Message) (Object, error) {
	b := Object{}
	if body != nil && body.ProtoReflect().IsValid() {
		var err error
		b, err = rpccompat.Legacy(body)
		if err != nil {
			return nil, invalid("请求结构无效")
		}
	}
	if len(encoded(b)) > 256*1024 {
		return nil, payloadTooLarge("JSON 请求超过256KB")
	}
	return b, nil
}
func prepareScreeningBody(ctx context.Context, body proto.Message) (identity, Object, error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return a, nil, err
	}
	b, err := decodeScreeningBody(body)
	if err != nil {
		return a, nil, err
	}
	if err = validateMutationBody(b); err != nil {
		return a, nil, err
	}
	return a, b, nil
}
func prepareScreeningMutation(ctx context.Context, method, canonicalPath string, q *screeningv1.ScreeningQuery, meta *screeningv1.ImportMutationMetadata, body proto.Message) (identity, Object, error) {
	a, b, err := prepareScreeningBody(ctx, body)
	if err != nil {
		return a, nil, err
	}
	raw := screeningQueryValues(q).Encode()
	key := ""
	if meta != nil {
		key = meta.IdempotencyKey
		original := meta.OriginalQuery
		if len(original) > 8192 || (original != "" && !strings.HasPrefix(original, "?")) || strings.ContainsAny(original, "\r\n#") {
			return a, nil, invalid("请求参数无效")
		}
		raw = strings.TrimPrefix(original, "?")
	}
	if key == "" || len(key) > 200 {
		return a, nil, invalid("修改请求需要Idempotency-Key")
	}
	a.RequestKey = key
	a.RequestHash = buildIdempotencyHash(method, canonicalPath, raw, b)
	return a, b, nil
}
func screeningRPCResponse[T any, PT interface {
	*T
	proto.Message
}](out PT, value any, err error) (*connect.Response[T], error) {
	if err != nil {
		return nil, rpcError(err)
	}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}
func (s *service) registerScreeningRPC(mux *http.ServeMux) {
	path, handler := screeningv1connect.NewScreeningServiceHandler(s, connect.WithReadMaxBytes(512*1024))
	mux.Handle(path, s.authenticatedRPC(handler))
}

func (s *service) ListProducts(ctx context.Context, req *connect.Request[screeningv1.ListProductsRequest]) (*connect.Response[screeningv1.ListProductsResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.productsUsecase(ctx, a, screeningQueryValues(req.Msg.Query), "")
	return screeningRPCResponse(&screeningv1.ListProductsResponse{}, value, err)
}

func (s *service) GetSummary(ctx context.Context, req *connect.Request[screeningv1.GetSummaryRequest]) (*connect.Response[screeningv1.GetSummaryResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.summaryUsecase(ctx, a, screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.GetSummaryResponse{}, value, err)
}

func (s *service) GetProduct(ctx context.Context, req *connect.Request[screeningv1.GetProductRequest]) (*connect.Response[screeningv1.GetProductResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.getProductUsecase(ctx, a, req.Msg.GetId())
	return screeningRPCResponse(&screeningv1.GetProductResponse{}, value, err)
}

func (s *service) GetProductFamily(ctx context.Context, req *connect.Request[screeningv1.GetProductFamilyRequest]) (*connect.Response[screeningv1.GetProductFamilyResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.getProductFamilyUsecase(ctx, a, req.Msg.GetId(), screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.GetProductFamilyResponse{}, value, err)
}

func (s *service) ListProductMonthly(ctx context.Context, req *connect.Request[screeningv1.ListProductMonthlyRequest]) (*connect.Response[screeningv1.ListProductMonthlyResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.getProductMonthlyUsecase(ctx, a, req.Msg.GetId(), screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.ListProductMonthlyResponse{}, value, err)
}

func (s *service) ListProductObservations(ctx context.Context, req *connect.Request[screeningv1.ListProductObservationsRequest]) (*connect.Response[screeningv1.ListProductObservationsResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.listProductObservationsUsecase(ctx, a, req.Msg.GetId(), screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.ListProductObservationsResponse{}, value, err)
}

func (s *service) GetObservationSource(ctx context.Context, req *connect.Request[screeningv1.GetObservationSourceRequest]) (*connect.Response[screeningv1.GetObservationSourceResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId(), req.Msg.GetRowId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.getProductObservationSourceUsecase(ctx, a, req.Msg.GetId(), req.Msg.GetRowId())
	return screeningRPCResponse(&screeningv1.GetObservationSourceResponse{}, value, err)
}

func (s *service) ListProductHistory(ctx context.Context, req *connect.Request[screeningv1.ListProductHistoryRequest]) (*connect.Response[screeningv1.ListProductHistoryResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.listProductHistoryUsecase(ctx, a, req.Msg.GetId(), screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.ListProductHistoryResponse{}, value, err)
}

func (s *service) ListProductReferences(ctx context.Context, req *connect.Request[screeningv1.ListProductReferencesRequest]) (*connect.Response[screeningv1.ListProductReferencesResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.listProductReferencesUsecase(ctx, a, req.Msg.GetId())
	return screeningRPCResponse(&screeningv1.ListProductReferencesResponse{}, value, err)
}

func (s *service) ListProfitScenarios(ctx context.Context, req *connect.Request[screeningv1.ListProfitScenariosRequest]) (*connect.Response[screeningv1.ListProfitScenariosResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.listProductProfitScenariosUsecase(ctx, a, req.Msg.GetId(), screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.ListProfitScenariosResponse{}, value, err)
}

func (s *service) ListGroupProducts(ctx context.Context, req *connect.Request[screeningv1.ListGroupProductsRequest]) (*connect.Response[screeningv1.ListGroupProductsResponse], error) {
	a, err := validateScreeningGroup(ctx, req.Msg.GetGroupKey())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.productsUsecase(ctx, a, screeningQueryValues(req.Msg.Query), req.Msg.GetGroupKey())
	return screeningRPCResponse(&screeningv1.ListGroupProductsResponse{}, value, err)
}

func (s *service) ListParentMonthly(ctx context.Context, req *connect.Request[screeningv1.ListParentMonthlyRequest]) (*connect.Response[screeningv1.ListParentMonthlyResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.listParentMonthlyUsecase(ctx, a, screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.ListParentMonthlyResponse{}, value, err)
}

func (s *service) ListMonths(ctx context.Context, req *connect.Request[screeningv1.ListMonthsRequest]) (*connect.Response[screeningv1.ListMonthsResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.listMonthsUsecase(ctx, a)
	return screeningRPCResponse(&screeningv1.ListMonthsResponse{}, value, err)
}

func (s *service) ListTrademarkOptions(ctx context.Context, req *connect.Request[screeningv1.ListTrademarkOptionsRequest]) (*connect.Response[screeningv1.ListTrademarkOptionsResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.trademarkOptionsUsecase(ctx, a, screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.ListTrademarkOptionsResponse{}, value, err)
}

func (s *service) ListReviewCategories(ctx context.Context, req *connect.Request[screeningv1.ListReviewCategoriesRequest]) (*connect.Response[screeningv1.ListReviewCategoriesResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.reviewCategoriesUsecase(ctx, a, screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.ListReviewCategoriesResponse{}, value, err)
}

func (s *service) GetReviewWorkspace(ctx context.Context, req *connect.Request[screeningv1.GetReviewWorkspaceRequest]) (*connect.Response[screeningv1.GetReviewWorkspaceResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.reviewWorkspaceUsecase(ctx, a, screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.GetReviewWorkspaceResponse{}, value, err)
}

func (s *service) ListReviewBatches(ctx context.Context, req *connect.Request[screeningv1.ListReviewBatchesRequest]) (*connect.Response[screeningv1.ListReviewBatchesResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.listReviewBatchesUsecase(ctx, a, screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.ListReviewBatchesResponse{}, value, err)
}

func (s *service) GetReviewBatch(ctx context.Context, req *connect.Request[screeningv1.GetReviewBatchRequest]) (*connect.Response[screeningv1.GetReviewBatchResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.getReviewBatchUsecase(ctx, a, req.Msg.GetId())
	return screeningRPCResponse(&screeningv1.GetReviewBatchResponse{}, value, err)
}

func (s *service) ListPolicies(ctx context.Context, req *connect.Request[screeningv1.ListPoliciesRequest]) (*connect.Response[screeningv1.ListPoliciesResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.listConfigsUsecase(ctx, a, string(ConfigScreening))
	return screeningRPCResponse(&screeningv1.ListPoliciesResponse{}, value, err)
}

func (s *service) ListProfitTemplates(ctx context.Context, req *connect.Request[screeningv1.ListProfitTemplatesRequest]) (*connect.Response[screeningv1.ListProfitTemplatesResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.listConfigsUsecase(ctx, a, string(ConfigProfitTemplate))
	return screeningRPCResponse(&screeningv1.ListProfitTemplatesResponse{}, value, err)
}

func (s *service) ListProfitProducts(ctx context.Context, req *connect.Request[screeningv1.ListProfitProductsRequest]) (*connect.Response[screeningv1.ListProfitProductsResponse], error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.profitProductsUsecase(ctx, a, screeningQueryValues(req.Msg.Query))
	return screeningRPCResponse(&screeningv1.ListProfitProductsResponse{}, value, err)
}

func (s *service) GetScreenRun(ctx context.Context, req *connect.Request[screeningv1.GetScreenRunRequest]) (*connect.Response[screeningv1.GetScreenRunResponse], error) {
	a, err := validateScreeningResourceArgs(ctx, req.Msg.GetId())
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.getPublicJob(ctx, a, req.Msg.GetId())
	return screeningRPCResponse(&screeningv1.GetScreenRunResponse{}, value, err)
}

func (s *service) ReviewProduct(ctx context.Context, req *connect.Request[screeningv1.ReviewProductRequest]) (*connect.Response[screeningv1.ReviewProductResponse], error) {
	id := req.Msg.GetId()
	if _, err := validateScreeningResourceArgs(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	a, body, err := prepareScreeningMutation(ctx, "PATCH", "products/"+id+"/review", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.reviewProduct(ctx, a, id, parseReviewCommand(body))
	return screeningRPCResponse(&screeningv1.ReviewProductResponse{}, value, err)
}

func (s *service) UpdateProductCategory(ctx context.Context, req *connect.Request[screeningv1.UpdateProductCategoryRequest]) (*connect.Response[screeningv1.UpdateProductCategoryResponse], error) {
	id := req.Msg.GetId()
	if _, err := validateScreeningResourceArgs(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	a, body, err := prepareScreeningMutation(ctx, "PATCH", "products/"+id+"/category", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.updateProductCategory(ctx, a, id, parseCategoryCommand(body))
	return screeningRPCResponse(&screeningv1.UpdateProductCategoryResponse{}, value, err)
}

func (s *service) UpdateProductParameters(ctx context.Context, req *connect.Request[screeningv1.UpdateProductParametersRequest]) (*connect.Response[screeningv1.UpdateProductParametersResponse], error) {
	id := req.Msg.GetId()
	if _, err := validateScreeningResourceArgs(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	a, body, err := prepareScreeningMutation(ctx, "PATCH", "products/"+id+"/parameters", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.updateProductParameters(ctx, a, id, parseParametersCommand(body))
	return screeningRPCResponse(&screeningv1.UpdateProductParametersResponse{}, value, err)
}

func (s *service) SaveProfitScenario(ctx context.Context, req *connect.Request[screeningv1.SaveProfitScenarioRequest]) (*connect.Response[screeningv1.SaveProfitScenarioResponse], error) {
	id := req.Msg.GetId()
	if _, err := validateScreeningResourceArgs(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	a, body, err := prepareScreeningMutation(ctx, "POST", "products/"+id+"/profit-scenarios", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.saveProfit(ctx, a, id, parseSaveProfitCommand(body))
	return screeningRPCResponse(&screeningv1.SaveProfitScenarioResponse{}, value, err)
}

func (s *service) CalculateProfit(ctx context.Context, req *connect.Request[screeningv1.CalculateProfitRequest]) (*connect.Response[screeningv1.CalculateProfitResponse], error) {
	a, body, err := prepareScreeningBody(ctx, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.calculateProfit(ctx, a, parseCalculateProfitCommand(body))
	return screeningRPCResponse(&screeningv1.CalculateProfitResponse{}, value, err)
}

func (s *service) CreateReviewPreview(ctx context.Context, req *connect.Request[screeningv1.CreateReviewPreviewRequest]) (*connect.Response[screeningv1.CreateReviewPreviewResponse], error) {
	a, body, err := prepareScreeningMutation(ctx, "POST", "review-batches/preview", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.previewBatch(ctx, a, parsePreviewBatchCommand(body))
	return screeningRPCResponse(&screeningv1.CreateReviewPreviewResponse{}, value, err)
}

func (s *service) ApplyReviewBatch(ctx context.Context, req *connect.Request[screeningv1.ApplyReviewBatchRequest]) (*connect.Response[screeningv1.ApplyReviewBatchResponse], error) {
	id := req.Msg.GetId()
	if _, err := validateScreeningResourceArgs(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	a, _, err := prepareScreeningMutation(ctx, "POST", "review-batches/"+id+"/apply", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.applyBatch(ctx, a, id, false)
	return screeningRPCResponse(&screeningv1.ApplyReviewBatchResponse{}, value, err)
}

func (s *service) UndoReviewBatch(ctx context.Context, req *connect.Request[screeningv1.UndoReviewBatchRequest]) (*connect.Response[screeningv1.UndoReviewBatchResponse], error) {
	id := req.Msg.GetId()
	if _, err := validateScreeningResourceArgs(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	a, _, err := prepareScreeningMutation(ctx, "POST", "review-batches/"+id+"/undo", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.applyBatch(ctx, a, id, true)
	return screeningRPCResponse(&screeningv1.UndoReviewBatchResponse{}, value, err)
}

func (s *service) CreatePolicy(ctx context.Context, req *connect.Request[screeningv1.CreatePolicyRequest]) (*connect.Response[screeningv1.CreatePolicyResponse], error) {
	a, body, err := prepareScreeningMutation(ctx, "POST", "policies", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.createConfig(ctx, a, parseCreateConfigCommand(ConfigScreening, body))
	return screeningRPCResponse(&screeningv1.CreatePolicyResponse{}, value, err)
}

func (s *service) CreateProfitTemplate(ctx context.Context, req *connect.Request[screeningv1.CreateProfitTemplateRequest]) (*connect.Response[screeningv1.CreateProfitTemplateResponse], error) {
	a, body, err := prepareScreeningMutation(ctx, "POST", "profit/templates", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.createConfig(ctx, a, parseCreateConfigCommand(ConfigProfitTemplate, body))
	return screeningRPCResponse(&screeningv1.CreateProfitTemplateResponse{}, value, err)
}

func (s *service) TestPolicy(ctx context.Context, req *connect.Request[screeningv1.TestPolicyRequest]) (*connect.Response[screeningv1.TestPolicyResponse], error) {
	id := req.Msg.GetId()
	if _, err := validateScreeningResourceArgs(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	a, body, err := prepareScreeningMutation(ctx, "POST", "policies/"+id+"/test", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.testPolicy(ctx, a, id, parseTestPolicyCommand(body))
	return screeningRPCResponse(&screeningv1.TestPolicyResponse{}, value, err)
}

func (s *service) ActivatePolicy(ctx context.Context, req *connect.Request[screeningv1.ActivatePolicyRequest]) (*connect.Response[screeningv1.ActivatePolicyResponse], error) {
	id := req.Msg.GetId()
	if _, err := validateScreeningResourceArgs(ctx, id); err != nil {
		return nil, rpcError(err)
	}
	a, _, err := prepareScreeningMutation(ctx, "POST", "policies/"+id+"/activate", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.activatePolicy(ctx, a, id)
	return screeningRPCResponse(&screeningv1.ActivatePolicyResponse{}, value, err)
}

func (s *service) CreateScreenRun(ctx context.Context, req *connect.Request[screeningv1.CreateScreenRunRequest]) (*connect.Response[screeningv1.CreateScreenRunResponse], error) {
	a, body, err := prepareScreeningMutation(ctx, "POST", "screen-runs", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.createScreenRunUsecase(ctx, a, parseEnqueueScreenCommand(body))
	return screeningRPCResponse(&screeningv1.CreateScreenRunResponse{}, value, err)
}

func (s *service) CreateExport(ctx context.Context, req *connect.Request[screeningv1.CreateExportRequest]) (*connect.Response[screeningv1.CreateExportResponse], error) {
	a, body, err := prepareScreeningMutation(ctx, "POST", "exports", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.queueExport(ctx, a, parseSelectionCommand(body))
	return screeningRPCResponse(&screeningv1.CreateExportResponse{}, value, err)
}

func (s *service) CreateProfitBatch(ctx context.Context, req *connect.Request[screeningv1.CreateProfitBatchRequest]) (*connect.Response[screeningv1.CreateProfitBatchResponse], error) {
	a, body, err := prepareScreeningMutation(ctx, "POST", "profit/batches", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	value, err := s.queueProfit(ctx, a, parseQueueProfitCommand(body))
	return screeningRPCResponse(&screeningv1.CreateProfitBatchResponse{}, value, err)
}

func validateScreeningResourceArgs(ctx context.Context, ids ...string) (identity, error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return a, err
	}
	for _, id := range ids {
		if err := validImportResourceID(id); err != nil {
			return a, err
		}
	}
	return a, nil
}
func validateScreeningGroup(ctx context.Context, key string) (identity, error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return a, err
	}
	if len(key) != 64 {
		return a, invalid("分组ID无效")
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return a, invalid("分组ID无效")
		}
	}
	return a, nil
}
