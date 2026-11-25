package ecom

import (
	"connectrpc.com/connect"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	"net/http"
	"net/url"
	commonv1 "seller-ops/contracts/sellerops/common/v1"
	ecomv1 "seller-ops/contracts/sellerops/ecom/v1"
	"seller-ops/contracts/sellerops/ecom/v1/ecomv1connect"
	"seller-ops/rpccompat"
)

const RPCPrefix = "/sellerops.ecom.v1.EcomService/"

type rpcUserKey struct{}

func ecomRPCError(err error) *connect.Error {
	status, message := 502, "外部服务处理失败，请检查配置或稍后重试"
	var h httpError
	if errors.As(err, &h) {
		status, message = h.status, h.message
	}
	code := connect.CodeUnavailable
	switch status {
	case 401:
		code = connect.CodeUnauthenticated
	case 403:
		code = connect.CodePermissionDenied
	case 404:
		code = connect.CodeNotFound
	case 409:
		code = connect.CodeAborted
	case 400, 422:
		code = connect.CodeInvalidArgument
	case 413:
		code = connect.CodeResourceExhausted
	}
	if errors.Is(err, context.Canceled) {
		code = connect.CodeCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		code = connect.CodeDeadlineExceeded
	}
	out := connect.NewError(code, errors.New(message))
	var id [16]byte
	_, _ = rand.Read(id[:])
	detail, _ := connect.NewErrorDetail(&commonv1.ErrorDetail{Reason: fmt.Sprintf("HTTP_%d", status), RequestId: hex.EncodeToString(id[:])})
	if detail != nil {
		out.AddDetail(detail)
	}
	return out
}
func (s *Service) registerRPC(mux *http.ServeMux) {
	path, handler := ecomv1connect.NewEcomServiceHandler(s, connect.WithReadMaxBytes(160*1024))
	writer := connect.NewErrorWriter()
	mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		user, err := s.authenticate(r)
		if err != nil {
			_ = writer.Write(w, r, ecomRPCError(err))
			return
		}
		handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), rpcUserKey{}, user)))
	}))
}
func ecomQueryValues(q *ecomv1.EcomQuery) url.Values {
	v := url.Values{}
	if q == nil {
		return v
	}
	m := q.ProtoReflect()
	fields := m.Descriptor().Fields()
	for i := 0; i < fields.Len(); i++ {
		f := fields.Get(i)
		if !f.IsList() {
			continue
		}
		list := m.Get(f).List()
		for j := 0; j < list.Len(); j++ {
			v.Add(string(f.Name()), list.Get(j).String())
		}
	}
	for k, x := range q.Extra {
		v[k] = append(v[k], x.Values...)
	}
	return v
}
func (s *Service) executeRPC(ctx context.Context, method, path string, q *ecomv1.EcomQuery, body proto.Message) (any, error) {
	user, ok := ctx.Value(rpcUserKey{}).(int64)
	if !ok {
		return nil, httpError{401, "请先登录"}
	}
	b := Obj{}
	var err error
	if body != nil && body.ProtoReflect().IsValid() {
		if body.ProtoReflect().Descriptor().FullName() == "google.protobuf.Struct" {
			raw, ok := body.(interface{ AsMap() map[string]any })
			if !ok {
				return nil, httpError{422, "请求结构无效"}
			}
			b = raw.AsMap()
		} else {
			b, err = rpccompat.Legacy(body)
			if err != nil {
				return nil, httpError{422, "请求结构无效"}
			}
		}
	}
	if len(jsonBytes(b)) > 64*1024 {
		return nil, httpError{413, "请求内容过大"}
	}
	return s.executeEcom(ctx, user, method, path, ecomQueryValues(q), b)
}

func (s *Service) ListResearchCategories(ctx context.Context, req *connect.Request[ecomv1.ListResearchCategoriesRequest]) (*connect.Response[ecomv1.ListResearchCategoriesResponse], error) {
	value, err := s.executeRPC(ctx, "GET", "product-research/categories", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.ListResearchCategoriesResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ResearchProducts(ctx context.Context, req *connect.Request[ecomv1.ResearchProductsRequest]) (*connect.Response[ecomv1.ResearchProductsResponse], error) {
	value, err := s.executeRPC(ctx, "POST", "product-research/research", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.ResearchProductsResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ScoreProduct(ctx context.Context, req *connect.Request[ecomv1.ScoreProductRequest]) (*connect.Response[ecomv1.ScoreProductResponse], error) {
	asin, validationErr := validateASIN(req.Msg.GetAsin())
	if validationErr != nil {
		return nil, ecomRPCError(validationErr)
	}

	value, err := s.executeRPC(ctx, "GET", "product-research/score/"+asin+"", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.ScoreProductResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ListListingLanguages(ctx context.Context, req *connect.Request[ecomv1.ListListingLanguagesRequest]) (*connect.Response[ecomv1.ListListingLanguagesResponse], error) {
	value, err := s.executeRPC(ctx, "GET", "listing/languages", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.ListListingLanguagesResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) GenerateListing(ctx context.Context, req *connect.Request[ecomv1.GenerateListingRequest]) (*connect.Response[ecomv1.GenerateListingResponse], error) {
	value, err := s.executeRPC(ctx, "POST", "listing/generate", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.GenerateListingResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) OptimizeListing(ctx context.Context, req *connect.Request[ecomv1.OptimizeListingRequest]) (*connect.Response[ecomv1.OptimizeListingResponse], error) {
	value, err := s.executeRPC(ctx, "POST", "listing/optimize", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.OptimizeListingResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) AnalyzeReviews(ctx context.Context, req *connect.Request[ecomv1.AnalyzeReviewsRequest]) (*connect.Response[ecomv1.AnalyzeReviewsResponse], error) {
	value, err := s.executeRPC(ctx, "POST", "reviews/analyze", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.AnalyzeReviewsResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) AnalyzeReviewsLegacy(ctx context.Context, req *connect.Request[ecomv1.AnalyzeReviewsLegacyRequest]) (*connect.Response[ecomv1.AnalyzeReviewsLegacyResponse], error) {
	asin, validationErr := validateASIN(req.Msg.GetAsin())
	if validationErr != nil {
		return nil, ecomRPCError(validationErr)
	}

	value, err := s.executeRPC(ctx, "GET", "reviews/analyze/"+asin+"", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.AnalyzeReviewsLegacyResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) OptimizeAds(ctx context.Context, req *connect.Request[ecomv1.OptimizeAdsRequest]) (*connect.Response[ecomv1.OptimizeAdsResponse], error) {
	value, err := s.executeRPC(ctx, "POST", "ads/optimize", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.OptimizeAdsResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ListMonitors(ctx context.Context, req *connect.Request[ecomv1.ListMonitorsRequest]) (*connect.Response[ecomv1.ListMonitorsResponse], error) {
	value, err := s.executeRPC(ctx, "GET", "monitor/list", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.ListMonitorsResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) AddMonitor(ctx context.Context, req *connect.Request[ecomv1.AddMonitorRequest]) (*connect.Response[ecomv1.AddMonitorResponse], error) {
	value, err := s.executeRPC(ctx, "POST", "monitor/add", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.AddMonitorResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) RemoveMonitor(ctx context.Context, req *connect.Request[ecomv1.RemoveMonitorRequest]) (*connect.Response[ecomv1.RemoveMonitorResponse], error) {
	asin, validationErr := validateASIN(req.Msg.GetAsin())
	if validationErr != nil {
		return nil, ecomRPCError(validationErr)
	}

	value, err := s.executeRPC(ctx, "DELETE", "monitor/remove/"+asin+"", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.RemoveMonitorResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) ListSnapshots(ctx context.Context, req *connect.Request[ecomv1.ListSnapshotsRequest]) (*connect.Response[ecomv1.ListSnapshotsResponse], error) {
	asin, validationErr := validateASIN(req.Msg.GetAsin())
	if validationErr != nil {
		return nil, ecomRPCError(validationErr)
	}

	value, err := s.executeRPC(ctx, "GET", "monitor/snapshots/"+asin+"", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.ListSnapshotsResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *Service) CreateSnapshot(ctx context.Context, req *connect.Request[ecomv1.CreateSnapshotRequest]) (*connect.Response[ecomv1.CreateSnapshotResponse], error) {
	asin, validationErr := validateASIN(req.Msg.GetAsin())
	if validationErr != nil {
		return nil, ecomRPCError(validationErr)
	}

	value, err := s.executeRPC(ctx, "POST", "monitor/snapshot/"+asin+"", req.Msg.Query, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	out := &ecomv1.CreateSnapshotResponse{}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}
