package ecom

import (
	"connectrpc.com/connect"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
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

func ecomRPCUser(ctx context.Context, body proto.Message) (int64, error) {
	user, ok := ctx.Value(rpcUserKey{}).(int64)
	if !ok {
		return 0, httpError{401, "请先登录"}
	}
	if body != nil && body.ProtoReflect().IsValid() && proto.Size(body) > 64*1024 {
		return 0, httpError{413, "请求内容过大"}
	}
	return user, nil
}

func ecomRPCRequest(ctx context.Context, body proto.Message) (int64, Obj, error) {
	user, err := ecomRPCUser(ctx, body)
	if err != nil {
		return 0, nil, err
	}
	b := Obj{}
	if body != nil && body.ProtoReflect().IsValid() {
		if body.ProtoReflect().Descriptor().FullName() == "google.protobuf.Struct" {
			raw, ok := body.(interface{ AsMap() map[string]any })
			if !ok {
				return 0, nil, httpError{422, "请求结构无效"}
			}
			b = raw.AsMap()
		} else {
			b, err = rpccompat.Legacy(body)
			if err != nil {
				return 0, nil, httpError{422, "请求结构无效"}
			}
		}
	}
	if len(jsonBytes(b)) > 64*1024 {
		return 0, nil, httpError{413, "请求内容过大"}
	}
	return user, b, nil
}

func ecomRPCResponse[T any, PT interface {
	*T
	proto.Message
}](out PT, value any, err error) (*connect.Response[T], error) {
	if err != nil {
		return nil, ecomRPCError(err)
	}
	if err = rpccompat.FromLegacy(out, Obj{"data": value}); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func scoreToProto(v Obj) *ecomv1.Score {
	out := &ecomv1.Score{
		Asin:                 proto.String(text(v["asin"])),
		Title:                proto.String(text(v["title"])),
		Price:                proto.Float64(number(v["price"])),
		BsrCategory:          proto.String(text(v["bsr_category"])),
		ReviewCount:          proto.Int64(int64(number(v["review_count"]))),
		Rating:               proto.Float64(number(v["rating"])),
		AiAnalysis:           proto.String(text(v["ai_analysis"])),
		Recommended:          proto.Bool(v["recommended"] == true),
		Tags:                 stringsList(v["tags"]),
		CompetitionScore:     proto.Float64(number(v["competition_score"])),
		ProfitPotentialScore: proto.Float64(number(v["profit_potential_score"])),
		TrendScore:           proto.Float64(number(v["trend_score"])),
		OverallScore:         proto.Float64(number(v["overall_score"])),
	}
	switch rank := v["bsr_rank"].(type) {
	case *int:
		if rank != nil {
			out.BsrRank = proto.Int64(int64(*rank))
		} else {
			out.LegacyFields = &structpb.Struct{Fields: map[string]*structpb.Value{"bsr_rank": structpb.NewNullValue()}}
		}
	case nil:
		out.LegacyFields = &structpb.Struct{Fields: map[string]*structpb.Value{"bsr_rank": structpb.NewNullValue()}}
	default:
		out.BsrRank = proto.Int64(int64(number(rank)))
	}
	return out
}

func listingToProto(v Obj) *ecomv1.ListingResult {
	counts := map[string]int64{}
	if raw, ok := v["character_counts"].(Obj); ok {
		for k, val := range raw {
			counts[k] = int64(number(val))
		}
	}
	return &ecomv1.ListingResult{
		AsinReference:   proto.String(text(v["asin_reference"])),
		Marketplace:     proto.String(text(v["marketplace"])),
		Language:        proto.String(text(v["language"])),
		Title:           proto.String(text(v["title"])),
		Description:     proto.String(text(v["description"])),
		APlusDraft:      proto.String(text(v["a_plus_draft"])),
		BulletPoints:    stringsList(v["bullet_points"]),
		SearchTerms:     stringsList(v["search_terms"]),
		SubjectMatter:   stringsList(v["subject_matter"]),
		SeoScore:        proto.Float64(number(v["seo_score"])),
		CharacterCounts: counts,
	}
}

func (s *Service) ListResearchCategories(ctx context.Context, req *connect.Request[ecomv1.ListResearchCategoriesRequest]) (*connect.Response[ecomv1.ListResearchCategoriesResponse], error) {
	if _, err := ecomRPCUser(ctx, req.Msg.Body); err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.listResearchCategories()
	if err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(&ecomv1.ListResearchCategoriesResponse{
		Data: &ecomv1.CategoryResult{Categories: stringsList(value["categories"])},
	}), nil
}

func (s *Service) ResearchProducts(ctx context.Context, req *connect.Request[ecomv1.ResearchProductsRequest]) (*connect.Response[ecomv1.ResearchProductsResponse], error) {
	_, body, err := ecomRPCRequest(ctx, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.researchProducts(ctx, body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	rows, _ := value["results"].([]Obj)
	scores := make([]*ecomv1.Score, 0, len(rows))
	for _, row := range rows {
		scores = append(scores, scoreToProto(row))
	}
	return connect.NewResponse(&ecomv1.ResearchProductsResponse{
		Data: &ecomv1.ResearchResult{
			Category: proto.String(text(value["category"])),
			Results:  scores,
			Failed:   proto.Int64(int64(number(value["failed"]))),
		},
	}), nil
}

func (s *Service) ScoreProduct(ctx context.Context, req *connect.Request[ecomv1.ScoreProductRequest]) (*connect.Response[ecomv1.ScoreProductResponse], error) {
	if _, err := ecomRPCUser(ctx, req.Msg.Body); err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.scoreProduct(ctx, req.Msg.GetAsin(), ecomQueryValues(req.Msg.Query))
	if err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(&ecomv1.ScoreProductResponse{Data: scoreToProto(value)}), nil
}

func (s *Service) ListListingLanguages(ctx context.Context, req *connect.Request[ecomv1.ListListingLanguagesRequest]) (*connect.Response[ecomv1.ListListingLanguagesResponse], error) {
	if _, err := ecomRPCUser(ctx, req.Msg.Body); err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(&ecomv1.ListListingLanguagesResponse{
		Data: &ecomv1.LanguageResult{Languages: languages},
	}), nil
}

func (s *Service) GenerateListing(ctx context.Context, req *connect.Request[ecomv1.GenerateListingRequest]) (*connect.Response[ecomv1.GenerateListingResponse], error) {
	_, body, err := ecomRPCRequest(ctx, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.generateListing(ctx, body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(&ecomv1.GenerateListingResponse{Data: listingToProto(value)}), nil
}

func (s *Service) OptimizeListing(ctx context.Context, req *connect.Request[ecomv1.OptimizeListingRequest]) (*connect.Response[ecomv1.OptimizeListingResponse], error) {
	_, body, err := ecomRPCRequest(ctx, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.optimizeListing(ctx, body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(&ecomv1.OptimizeListingResponse{Data: listingToProto(value)}), nil
}

func (s *Service) AnalyzeReviews(ctx context.Context, req *connect.Request[ecomv1.AnalyzeReviewsRequest]) (*connect.Response[ecomv1.AnalyzeReviewsResponse], error) {
	_, body, err := ecomRPCRequest(ctx, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.analyzeReviews(ctx, body)
	return ecomRPCResponse(&ecomv1.AnalyzeReviewsResponse{}, value, err)
}

func (s *Service) AnalyzeReviewsLegacy(ctx context.Context, req *connect.Request[ecomv1.AnalyzeReviewsLegacyRequest]) (*connect.Response[ecomv1.AnalyzeReviewsLegacyResponse], error) {
	if _, err := ecomRPCUser(ctx, req.Msg.Body); err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.analyzeReviewsLegacy(ctx, req.Msg.GetAsin(), ecomQueryValues(req.Msg.Query))
	return ecomRPCResponse(&ecomv1.AnalyzeReviewsLegacyResponse{}, value, err)
}

func (s *Service) OptimizeAds(ctx context.Context, req *connect.Request[ecomv1.OptimizeAdsRequest]) (*connect.Response[ecomv1.OptimizeAdsResponse], error) {
	if _, err := ecomRPCUser(ctx, req.Msg.Body); err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.optimizeAdsRequest(ctx, ecomQueryValues(req.Msg.Query))
	return ecomRPCResponse(&ecomv1.OptimizeAdsResponse{}, value, err)
}

func (s *Service) ListMonitors(ctx context.Context, req *connect.Request[ecomv1.ListMonitorsRequest]) (*connect.Response[ecomv1.ListMonitorsResponse], error) {
	user, err := ecomRPCUser(ctx, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.listMonitors(ctx, user)
	return ecomRPCResponse(&ecomv1.ListMonitorsResponse{}, value, err)
}

func (s *Service) AddMonitor(ctx context.Context, req *connect.Request[ecomv1.AddMonitorRequest]) (*connect.Response[ecomv1.AddMonitorResponse], error) {
	user, body, err := ecomRPCRequest(ctx, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.addMonitor(ctx, user, body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(&ecomv1.AddMonitorResponse{
		Data: &ecomv1.MonitorActionResult{
			Status: proto.String(text(value["status"])),
			Asin:   proto.String(text(value["asin"])),
		},
	}), nil
}

func (s *Service) RemoveMonitor(ctx context.Context, req *connect.Request[ecomv1.RemoveMonitorRequest]) (*connect.Response[ecomv1.RemoveMonitorResponse], error) {
	user, err := ecomRPCUser(ctx, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.removeMonitor(ctx, user, req.Msg.GetAsin())
	if err != nil {
		return nil, ecomRPCError(err)
	}
	return connect.NewResponse(&ecomv1.RemoveMonitorResponse{
		Data: &ecomv1.MonitorActionResult{
			Status: proto.String(text(value["status"])),
			Asin:   proto.String(text(value["asin"])),
		},
	}), nil
}

func (s *Service) ListSnapshots(ctx context.Context, req *connect.Request[ecomv1.ListSnapshotsRequest]) (*connect.Response[ecomv1.ListSnapshotsResponse], error) {
	user, err := ecomRPCUser(ctx, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.listSnapshots(ctx, user, req.Msg.GetAsin(), ecomQueryValues(req.Msg.Query))
	return ecomRPCResponse(&ecomv1.ListSnapshotsResponse{}, value, err)
}

func (s *Service) CreateSnapshot(ctx context.Context, req *connect.Request[ecomv1.CreateSnapshotRequest]) (*connect.Response[ecomv1.CreateSnapshotResponse], error) {
	user, err := ecomRPCUser(ctx, req.Msg.Body)
	if err != nil {
		return nil, ecomRPCError(err)
	}
	value, err := s.createSnapshot(ctx, user, req.Msg.GetAsin())
	return ecomRPCResponse(&ecomv1.CreateSnapshotResponse{}, value, err)
}
