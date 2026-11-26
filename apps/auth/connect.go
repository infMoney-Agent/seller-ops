package main

import (
	"connectrpc.com/connect"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	"log"
	"net/http"
	"net/url"
	authv1 "seller-ops/contracts/sellerops/auth/v1"
	"seller-ops/contracts/sellerops/auth/v1/authv1connect"
	commonv1 "seller-ops/contracts/sellerops/common/v1"
	"seller-ops/rpccompat"
	"seller-ops/server/internal/auth"
)

const AuthRPCPrefix = "/sellerops.auth.v1.AuthService/"

type authRPCIdentity struct {
	Principal auth.Principal
	Hash      []byte
}
type authRPCKey struct{}

func authRPCError(err error) *connect.Error {
	status, message, plain := 500, "internal server error", false
	var e auth.APIError
	if errors.As(err, &e) {
		status, message, plain = e.Status, e.Message, e.Plain
	} else {
		log.Printf("auth RPC failure: %T", err)
	}
	code := connect.CodeInternal
	switch status {
	case 400, 422:
		code = connect.CodeInvalidArgument
	case 401:
		code = connect.CodeUnauthenticated
	case 403:
		code = connect.CodePermissionDenied
	case 404:
		code = connect.CodeNotFound
	case 409:
		code = connect.CodeAborted
	case 413:
		code = connect.CodeResourceExhausted
	case 503:
		code = connect.CodeUnavailable
	}
	out := connect.NewError(code, errors.New(message))
	reason := fmt.Sprintf("HTTP_%d", status)
	if plain {
		reason += "_PLAIN"
	}
	var id [16]byte
	_, _ = rand.Read(id[:])
	detail, _ := connect.NewErrorDetail(&commonv1.ErrorDetail{Reason: reason, RequestId: hex.EncodeToString(id[:])})
	if detail != nil {
		out.AddDetail(detail)
	}
	return out
}

func (s *server) registerAuthRPC(mux *http.ServeMux) {
	path, handler := authv1connect.NewAuthServiceHandler(s, connect.WithReadMaxBytes(160*1024))
	writer := connect.NewErrorWriter()
	mux.Handle(path, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		identity := authRPCIdentity{}
		if r.URL.Path != authv1connect.AuthServiceInspectInvitationProcedure {
			token := s.auth.SessionToken(r)
			a, err := s.auth.AuthenticateToken(r.Context(), token)
			if err != nil {
				var e auth.APIError
				if errors.As(err, &e) && e.ClearSession {
					s.auth.ClearSessionCookie(w)
				}
				_ = writer.Write(w, r, authRPCError(err))
				return
			}
			identity = authRPCIdentity{Principal: a, Hash: auth.HashToken(token)}
		}
		handler.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), authRPCKey{}, identity)))
	}))
}

func authQueryValues(q *authv1.AuthQuery) url.Values {
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

func authRPCBody(body proto.Message) (map[string]any, error) {
	b := map[string]any{}
	if body != nil && body.ProtoReflect().IsValid() {
		if raw, ok := body.(interface{ AsMap() map[string]any }); ok {
			b = raw.AsMap()
		} else {
			var err error
			b, err = rpccompat.Legacy(body)
			if err != nil {
				return nil, auth.APIError{Status: 400, Message: "invalid request"}
			}
		}
	}
	data, err := json.Marshal(b)
	if err != nil {
		return nil, auth.APIError{Status: 400, Message: "invalid request"}
	}
	if len(data) > 64*1024 {
		return nil, auth.APIError{Status: 413, Message: "Request too large"}
	}
	return b, nil
}

func requireAuthIdentity(ctx context.Context, requireTenant, requireOwner bool) (authRPCIdentity, error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || identity.Principal.UserID == 0 {
		return authRPCIdentity{}, auth.APIError{Status: 401, Message: "sign in required"}
	}
	if requireTenant && identity.Principal.TenantID == 0 {
		return authRPCIdentity{}, auth.APIError{Status: 403, Message: "workspace access denied"}
	}
	if requireOwner && identity.Principal.Role != "owner" {
		return authRPCIdentity{}, auth.APIError{Status: 403, Message: "workspace access denied"}
	}
	return identity, nil
}

func authRPCResponse[T any, PT interface {
	*T
	proto.Message
}](out PT, result *auth.Result, err error, wrapItems bool) (*connect.Response[T], error) {
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	if wrapItems {
		data = map[string]any{"items": data}
	}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) GetSession(ctx context.Context, req *connect.Request[authv1.GetSessionRequest]) (*connect.Response[authv1.GetSessionResponse], error) {
	identity, err := requireAuthIdentity(ctx, false, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.sessionUsecase(ctx, identity.Principal)
	return authRPCResponse(&authv1.GetSessionResponse{}, result, err, false)
}

func (s *server) ListManagedProducts(ctx context.Context, req *connect.Request[authv1.ListManagedProductsRequest]) (*connect.Response[authv1.ListManagedProductsResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.listProductsUsecase(ctx, identity.Principal, authQueryValues(req.Msg.Query))
	return authRPCResponse(&authv1.ListManagedProductsResponse{}, result, err, false)
}

func (s *server) GetManagedProduct(ctx context.Context, req *connect.Request[authv1.GetManagedProductRequest]) (*connect.Response[authv1.GetManagedProductResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.getProductUsecase(ctx, identity.Principal, req.Msg.GetId())
	return authRPCResponse(&authv1.GetManagedProductResponse{}, result, err, false)
}

func (s *server) CreateManagedProduct(ctx context.Context, req *connect.Request[authv1.CreateManagedProductRequest]) (*connect.Response[authv1.CreateManagedProductResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, true)
	if err != nil {
		return nil, authRPCError(err)
	}
	body, err := authRPCBody(req.Msg.Body)
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.createProductUsecase(ctx, identity.Principal, body)
	return authRPCResponse(&authv1.CreateManagedProductResponse{}, result, err, false)
}

func (s *server) UpdateManagedProduct(ctx context.Context, req *connect.Request[authv1.UpdateManagedProductRequest]) (*connect.Response[authv1.UpdateManagedProductResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, true)
	if err != nil {
		return nil, authRPCError(err)
	}
	body, err := authRPCBody(req.Msg.Body)
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.updateProductUsecase(ctx, identity.Principal, req.Msg.GetId(), body)
	return authRPCResponse(&authv1.UpdateManagedProductResponse{}, result, err, false)
}

func (s *server) PatchManagedProduct(ctx context.Context, req *connect.Request[authv1.PatchManagedProductRequest]) (*connect.Response[authv1.PatchManagedProductResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, true)
	if err != nil {
		return nil, authRPCError(err)
	}
	body, err := authRPCBody(req.Msg.Body)
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.updateProductUsecase(ctx, identity.Principal, req.Msg.GetId(), body)
	return authRPCResponse(&authv1.PatchManagedProductResponse{}, result, err, false)
}

func (s *server) DeleteManagedProduct(ctx context.Context, req *connect.Request[authv1.DeleteManagedProductRequest]) (*connect.Response[authv1.DeleteManagedProductResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, true)
	if err != nil {
		return nil, authRPCError(err)
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.deleteProductUsecase(ctx, identity.Principal, req.Msg.GetId())
	return authRPCResponse(&authv1.DeleteManagedProductResponse{}, result, err, false)
}

func (s *server) ListMembers(ctx context.Context, req *connect.Request[authv1.ListMembersRequest]) (*connect.Response[authv1.ListMembersResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.listMembersUsecase(ctx, identity.Principal)
	return authRPCResponse(&authv1.ListMembersResponse{}, result, err, true)
}

func (s *server) UpdateMember(ctx context.Context, req *connect.Request[authv1.UpdateMemberRequest]) (*connect.Response[authv1.UpdateMemberResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, true)
	if err != nil {
		return nil, authRPCError(err)
	}
	body, err := authRPCBody(req.Msg.Body)
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.updateMemberUsecase(ctx, identity.Principal, req.Msg.GetId(), body)
	return authRPCResponse(&authv1.UpdateMemberResponse{}, result, err, false)
}

func (s *server) RemoveMember(ctx context.Context, req *connect.Request[authv1.RemoveMemberRequest]) (*connect.Response[authv1.RemoveMemberResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, true)
	if err != nil {
		return nil, authRPCError(err)
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.removeMemberUsecase(ctx, identity.Principal, req.Msg.GetId())
	return authRPCResponse(&authv1.RemoveMemberResponse{}, result, err, false)
}

func (s *server) ListInvitations(ctx context.Context, req *connect.Request[authv1.ListInvitationsRequest]) (*connect.Response[authv1.ListInvitationsResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, true)
	if err != nil {
		return nil, authRPCError(err)
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.listInvitationsUsecase(ctx, identity.Principal)
	return authRPCResponse(&authv1.ListInvitationsResponse{}, result, err, true)
}

func (s *server) CreateInvitation(ctx context.Context, req *connect.Request[authv1.CreateInvitationRequest]) (*connect.Response[authv1.CreateInvitationResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, true)
	if err != nil {
		return nil, authRPCError(err)
	}
	body, err := authRPCBody(req.Msg.Body)
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.createInvitationUsecase(ctx, identity.Principal, body)
	return authRPCResponse(&authv1.CreateInvitationResponse{}, result, err, false)
}

func (s *server) RevokeInvitation(ctx context.Context, req *connect.Request[authv1.RevokeInvitationRequest]) (*connect.Response[authv1.RevokeInvitationResponse], error) {
	identity, err := requireAuthIdentity(ctx, true, true)
	if err != nil {
		return nil, authRPCError(err)
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.revokeInvitationUsecase(ctx, identity.Principal, req.Msg.GetId())
	return authRPCResponse(&authv1.RevokeInvitationResponse{}, result, err, false)
}

func (s *server) InspectInvitation(ctx context.Context, req *connect.Request[authv1.InspectInvitationRequest]) (*connect.Response[authv1.InspectInvitationResponse], error) {
	if _, ok := ctx.Value(authRPCKey{}).(authRPCIdentity); !ok {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	body, err := authRPCBody(req.Msg.Body)
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.inspectInvitationUsecase(ctx, body)
	return authRPCResponse(&authv1.InspectInvitationResponse{}, result, err, false)
}

func (s *server) AcceptInvitation(ctx context.Context, req *connect.Request[authv1.AcceptInvitationRequest]) (*connect.Response[authv1.AcceptInvitationResponse], error) {
	identity, err := requireAuthIdentity(ctx, false, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	body, err := authRPCBody(req.Msg.Body)
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.acceptInvitationUsecase(ctx, identity.Principal, body, identity.Hash)
	return authRPCResponse(&authv1.AcceptInvitationResponse{}, result, err, false)
}

func (s *server) GetCompany(ctx context.Context, req *connect.Request[authv1.GetCompanyRequest]) (*connect.Response[authv1.GetCompanyResponse], error) {
	identity, err := requireAuthIdentity(ctx, false, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.auth.CompanyInfoUsecase(ctx, identity.Principal)
	return authRPCResponse(&authv1.GetCompanyResponse{}, result, err, false)
}

func (s *server) GetRegistrationStatus(ctx context.Context, req *connect.Request[authv1.GetRegistrationStatusRequest]) (*connect.Response[authv1.GetRegistrationStatusResponse], error) {
	identity, err := requireAuthIdentity(ctx, false, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.auth.RegistrationStatusUsecase(ctx, identity.Principal)
	return authRPCResponse(&authv1.GetRegistrationStatusResponse{}, result, err, false)
}

func (s *server) ListJoinRequests(ctx context.Context, req *connect.Request[authv1.ListJoinRequestsRequest]) (*connect.Response[authv1.ListJoinRequestsResponse], error) {
	identity, err := requireAuthIdentity(ctx, false, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.auth.JoinRequestsUsecase(ctx, identity.Principal)
	return authRPCResponse(&authv1.ListJoinRequestsResponse{}, result, err, true)
}

func (s *server) ApplyToCompany(ctx context.Context, req *connect.Request[authv1.ApplyToCompanyRequest]) (*connect.Response[authv1.ApplyToCompanyResponse], error) {
	identity, err := requireAuthIdentity(ctx, false, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	body, err := authRPCBody(req.Msg.Body)
	if err != nil {
		return nil, authRPCError(err)
	}
	rawBody, _ := json.Marshal(body)
	if len(rawBody) > 4096 {
		return nil, authRPCError(auth.APIError{Status: 400, Message: "请求无效", Plain: true})
	}
	result, err := s.auth.ApplyToCompanyUsecase(ctx, identity.Principal, body)
	return authRPCResponse(&authv1.ApplyToCompanyResponse{}, result, err, false)
}

func (s *server) DecideJoinRequest(ctx context.Context, req *connect.Request[authv1.DecideJoinRequestRequest]) (*connect.Response[authv1.DecideJoinRequestResponse], error) {
	identity, err := requireAuthIdentity(ctx, false, false)
	if err != nil {
		return nil, authRPCError(err)
	}
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	if _, err = authRPCBody(req.Msg.Body); err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.auth.DecideJoinRequestUsecase(ctx, identity.Principal, req.Msg.GetId(), req.Msg.GetAction())
	return authRPCResponse(&authv1.DecideJoinRequestResponse{}, result, err, false)
}
