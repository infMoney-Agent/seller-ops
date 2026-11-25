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
func (s *service) screeningRPC(ctx context.Context, method, path string, q *screeningv1.ScreeningQuery, meta *screeningv1.ImportMutationMetadata, body proto.Message) (any, error) {
	a, err := rpcActor(ctx)
	if err != nil {
		return nil, err
	}
	b := Object{}
	if body != nil && body.ProtoReflect().IsValid() {
		b, err = rpccompat.Legacy(body)
		if err != nil {
			return nil, fail(422, "请求结构无效")
		}
	}
	if len(encoded(b)) > 256*1024 {
		return nil, fail(413, "JSON 请求超过256KB")
	}
	query := screeningQueryValues(q)
	raw := query.Encode()
	key := ""
	if meta != nil {
		key = meta.IdempotencyKey
		original := meta.OriginalQuery
		if len(original) > 8192 || (original != "" && !strings.HasPrefix(original, "?")) || strings.ContainsAny(original, "\r\n#") {
			return nil, fail(422, "请求参数无效")
		}
		raw = strings.TrimPrefix(original, "?")
	}
	return s.executeScreening(ctx, a, screeningCommand{Method: method, Path: path, Query: query, RawQuery: raw, Key: key, Body: b})
}
func (s *service) registerScreeningRPC(mux *http.ServeMux) {
	path, handler := screeningv1connect.NewScreeningServiceHandler(s, connect.WithReadMaxBytes(512*1024))
	mux.Handle(path, s.authenticatedRPC(handler))
}

func (s *service) ListProducts(ctx context.Context, req *connect.Request[screeningv1.ListProductsRequest]) (*connect.Response[screeningv1.ListProductsResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "products", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListProductsResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) GetSummary(ctx context.Context, req *connect.Request[screeningv1.GetSummaryRequest]) (*connect.Response[screeningv1.GetSummaryResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "summary", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.GetSummaryResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) GetProduct(ctx context.Context, req *connect.Request[screeningv1.GetProductRequest]) (*connect.Response[screeningv1.GetProductResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "products/"+req.Msg.GetId()+"", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.GetProductResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) GetProductFamily(ctx context.Context, req *connect.Request[screeningv1.GetProductFamilyRequest]) (*connect.Response[screeningv1.GetProductFamilyResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "products/"+req.Msg.GetId()+"/family", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.GetProductFamilyResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListProductMonthly(ctx context.Context, req *connect.Request[screeningv1.ListProductMonthlyRequest]) (*connect.Response[screeningv1.ListProductMonthlyResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "products/"+req.Msg.GetId()+"/monthly", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListProductMonthlyResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListProductObservations(ctx context.Context, req *connect.Request[screeningv1.ListProductObservationsRequest]) (*connect.Response[screeningv1.ListProductObservationsResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "products/"+req.Msg.GetId()+"/observations", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListProductObservationsResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) GetObservationSource(ctx context.Context, req *connect.Request[screeningv1.GetObservationSourceRequest]) (*connect.Response[screeningv1.GetObservationSourceResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId(), req.Msg.GetRowId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "products/"+req.Msg.GetId()+"/observations/"+req.Msg.GetRowId()+"", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.GetObservationSourceResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListProductHistory(ctx context.Context, req *connect.Request[screeningv1.ListProductHistoryRequest]) (*connect.Response[screeningv1.ListProductHistoryResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "products/"+req.Msg.GetId()+"/history", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListProductHistoryResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListProductReferences(ctx context.Context, req *connect.Request[screeningv1.ListProductReferencesRequest]) (*connect.Response[screeningv1.ListProductReferencesResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "products/"+req.Msg.GetId()+"/references", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListProductReferencesResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListProfitScenarios(ctx context.Context, req *connect.Request[screeningv1.ListProfitScenariosRequest]) (*connect.Response[screeningv1.ListProfitScenariosResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "products/"+req.Msg.GetId()+"/profit-scenarios", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListProfitScenariosResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListGroupProducts(ctx context.Context, req *connect.Request[screeningv1.ListGroupProductsRequest]) (*connect.Response[screeningv1.ListGroupProductsResponse], error) {
	if err := validateScreeningGroup(ctx, req.Msg.GetGroupKey()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "groups/"+req.Msg.GetGroupKey()+"/products", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListGroupProductsResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListParentMonthly(ctx context.Context, req *connect.Request[screeningv1.ListParentMonthlyRequest]) (*connect.Response[screeningv1.ListParentMonthlyResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "parents/monthly", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListParentMonthlyResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListMonths(ctx context.Context, req *connect.Request[screeningv1.ListMonthsRequest]) (*connect.Response[screeningv1.ListMonthsResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "months", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListMonthsResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListTrademarkOptions(ctx context.Context, req *connect.Request[screeningv1.ListTrademarkOptionsRequest]) (*connect.Response[screeningv1.ListTrademarkOptionsResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "trademark-options", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListTrademarkOptionsResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListReviewCategories(ctx context.Context, req *connect.Request[screeningv1.ListReviewCategoriesRequest]) (*connect.Response[screeningv1.ListReviewCategoriesResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "review-categories", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListReviewCategoriesResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) GetReviewWorkspace(ctx context.Context, req *connect.Request[screeningv1.GetReviewWorkspaceRequest]) (*connect.Response[screeningv1.GetReviewWorkspaceResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "review-workspace", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.GetReviewWorkspaceResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListReviewBatches(ctx context.Context, req *connect.Request[screeningv1.ListReviewBatchesRequest]) (*connect.Response[screeningv1.ListReviewBatchesResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "review-batches", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListReviewBatchesResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) GetReviewBatch(ctx context.Context, req *connect.Request[screeningv1.GetReviewBatchRequest]) (*connect.Response[screeningv1.GetReviewBatchResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "review-batches/"+req.Msg.GetId()+"", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.GetReviewBatchResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListPolicies(ctx context.Context, req *connect.Request[screeningv1.ListPoliciesRequest]) (*connect.Response[screeningv1.ListPoliciesResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "policies", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListPoliciesResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListProfitTemplates(ctx context.Context, req *connect.Request[screeningv1.ListProfitTemplatesRequest]) (*connect.Response[screeningv1.ListProfitTemplatesResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "profit/templates", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListProfitTemplatesResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ListProfitProducts(ctx context.Context, req *connect.Request[screeningv1.ListProfitProductsRequest]) (*connect.Response[screeningv1.ListProfitProductsResponse], error) {
	value, err := s.screeningRPC(ctx, "GET", "profit/products", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ListProfitProductsResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) GetScreenRun(ctx context.Context, req *connect.Request[screeningv1.GetScreenRunRequest]) (*connect.Response[screeningv1.GetScreenRunResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "GET", "screen-runs/"+req.Msg.GetId()+"", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.GetScreenRunResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ReviewProduct(ctx context.Context, req *connect.Request[screeningv1.ReviewProductRequest]) (*connect.Response[screeningv1.ReviewProductResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "PATCH", "products/"+req.Msg.GetId()+"/review", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ReviewProductResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) UpdateProductCategory(ctx context.Context, req *connect.Request[screeningv1.UpdateProductCategoryRequest]) (*connect.Response[screeningv1.UpdateProductCategoryResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "PATCH", "products/"+req.Msg.GetId()+"/category", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.UpdateProductCategoryResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) UpdateProductParameters(ctx context.Context, req *connect.Request[screeningv1.UpdateProductParametersRequest]) (*connect.Response[screeningv1.UpdateProductParametersResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "PATCH", "products/"+req.Msg.GetId()+"/parameters", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.UpdateProductParametersResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) SaveProfitScenario(ctx context.Context, req *connect.Request[screeningv1.SaveProfitScenarioRequest]) (*connect.Response[screeningv1.SaveProfitScenarioResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "POST", "products/"+req.Msg.GetId()+"/profit-scenarios", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.SaveProfitScenarioResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) CalculateProfit(ctx context.Context, req *connect.Request[screeningv1.CalculateProfitRequest]) (*connect.Response[screeningv1.CalculateProfitResponse], error) {
	value, err := s.screeningRPC(ctx, "POST", "profit/calculate", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.CalculateProfitResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) CreateReviewPreview(ctx context.Context, req *connect.Request[screeningv1.CreateReviewPreviewRequest]) (*connect.Response[screeningv1.CreateReviewPreviewResponse], error) {
	value, err := s.screeningRPC(ctx, "POST", "review-batches/preview", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.CreateReviewPreviewResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ApplyReviewBatch(ctx context.Context, req *connect.Request[screeningv1.ApplyReviewBatchRequest]) (*connect.Response[screeningv1.ApplyReviewBatchResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "POST", "review-batches/"+req.Msg.GetId()+"/apply", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ApplyReviewBatchResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) UndoReviewBatch(ctx context.Context, req *connect.Request[screeningv1.UndoReviewBatchRequest]) (*connect.Response[screeningv1.UndoReviewBatchResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "POST", "review-batches/"+req.Msg.GetId()+"/undo", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.UndoReviewBatchResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) CreatePolicy(ctx context.Context, req *connect.Request[screeningv1.CreatePolicyRequest]) (*connect.Response[screeningv1.CreatePolicyResponse], error) {
	value, err := s.screeningRPC(ctx, "POST", "policies", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.CreatePolicyResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) CreateProfitTemplate(ctx context.Context, req *connect.Request[screeningv1.CreateProfitTemplateRequest]) (*connect.Response[screeningv1.CreateProfitTemplateResponse], error) {
	value, err := s.screeningRPC(ctx, "POST", "profit/templates", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.CreateProfitTemplateResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) TestPolicy(ctx context.Context, req *connect.Request[screeningv1.TestPolicyRequest]) (*connect.Response[screeningv1.TestPolicyResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "POST", "policies/"+req.Msg.GetId()+"/test", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.TestPolicyResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) ActivatePolicy(ctx context.Context, req *connect.Request[screeningv1.ActivatePolicyRequest]) (*connect.Response[screeningv1.ActivatePolicyResponse], error) {
	if err := validateScreeningResourceArgs(ctx, req.Msg.GetId()); err != nil {
		return nil, rpcError(err)
	}
	value, err := s.screeningRPC(ctx, "POST", "policies/"+req.Msg.GetId()+"/activate", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.ActivatePolicyResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) CreateScreenRun(ctx context.Context, req *connect.Request[screeningv1.CreateScreenRunRequest]) (*connect.Response[screeningv1.CreateScreenRunResponse], error) {
	value, err := s.screeningRPC(ctx, "POST", "screen-runs", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.CreateScreenRunResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) CreateExport(ctx context.Context, req *connect.Request[screeningv1.CreateExportRequest]) (*connect.Response[screeningv1.CreateExportResponse], error) {
	value, err := s.screeningRPC(ctx, "POST", "exports", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.CreateExportResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *service) CreateProfitBatch(ctx context.Context, req *connect.Request[screeningv1.CreateProfitBatchRequest]) (*connect.Response[screeningv1.CreateProfitBatchResponse], error) {
	value, err := s.screeningRPC(ctx, "POST", "profit/batches", req.Msg.Query, req.Msg.Mutation, req.Msg.Body)
	if err != nil {
		return nil, rpcError(err)
	}
	out := &screeningv1.CreateProfitBatchResponse{}
	if err = rpccompat.FromLegacy(out, Object{"data": publicJSON(value)}); err != nil {
		return nil, rpcError(err)
	}
	return connect.NewResponse(out), nil
}

func validateScreeningResourceArgs(ctx context.Context, ids ...string) error {
	if _, err := rpcActor(ctx); err != nil {
		return err
	}
	for _, id := range ids {
		if err := validImportResourceID(id); err != nil {
			return err
		}
	}
	return nil
}
func validateScreeningGroup(ctx context.Context, key string) error {
	if _, err := rpcActor(ctx); err != nil {
		return err
	}
	if len(key) != 64 {
		return fail(422, "分组ID无效")
	}
	for _, c := range key {
		if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
			return fail(422, "分组ID无效")
		}
	}
	return nil
}
