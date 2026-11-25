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

func (s *server) GetSession(ctx context.Context, req *connect.Request[authv1.GetSessionRequest]) (*connect.Response[authv1.GetSessionResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.sessionUsecase(ctx, a)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.GetSessionResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) ListManagedProducts(ctx context.Context, req *connect.Request[authv1.ListManagedProductsRequest]) (*connect.Response[authv1.ListManagedProductsResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.productsUsecase(ctx, a, "GET", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.ListManagedProductsResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) GetManagedProduct(ctx context.Context, req *connect.Request[authv1.GetManagedProductRequest]) (*connect.Response[authv1.GetManagedProductResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.productsUsecase(ctx, a, "GET", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.GetManagedProductResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) CreateManagedProduct(ctx context.Context, req *connect.Request[authv1.CreateManagedProductRequest]) (*connect.Response[authv1.CreateManagedProductResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	if a.Role != "owner" {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.productsUsecase(ctx, a, "POST", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.CreateManagedProductResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) UpdateManagedProduct(ctx context.Context, req *connect.Request[authv1.UpdateManagedProductRequest]) (*connect.Response[authv1.UpdateManagedProductResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	if a.Role != "owner" {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.productsUsecase(ctx, a, "PUT", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.UpdateManagedProductResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) PatchManagedProduct(ctx context.Context, req *connect.Request[authv1.PatchManagedProductRequest]) (*connect.Response[authv1.PatchManagedProductResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	if a.Role != "owner" {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.productsUsecase(ctx, a, "PATCH", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.PatchManagedProductResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) DeleteManagedProduct(ctx context.Context, req *connect.Request[authv1.DeleteManagedProductRequest]) (*connect.Response[authv1.DeleteManagedProductResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	if a.Role != "owner" {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.productsUsecase(ctx, a, "DELETE", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.DeleteManagedProductResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) ListMembers(ctx context.Context, req *connect.Request[authv1.ListMembersRequest]) (*connect.Response[authv1.ListMembersResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.membersUsecase(ctx, a, "GET", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	data = map[string]any{"items": data}
	out := &authv1.ListMembersResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) UpdateMember(ctx context.Context, req *connect.Request[authv1.UpdateMemberRequest]) (*connect.Response[authv1.UpdateMemberResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	if a.Role != "owner" {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.changeMemberUsecase(ctx, a, "PATCH", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.UpdateMemberResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) RemoveMember(ctx context.Context, req *connect.Request[authv1.RemoveMemberRequest]) (*connect.Response[authv1.RemoveMemberResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	if a.Role != "owner" {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.changeMemberUsecase(ctx, a, "DELETE", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.RemoveMemberResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) ListInvitations(ctx context.Context, req *connect.Request[authv1.ListInvitationsRequest]) (*connect.Response[authv1.ListInvitationsResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	if a.Role != "owner" {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.invitationsUsecase(ctx, a, "GET", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	data = map[string]any{"items": data}
	out := &authv1.ListInvitationsResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) CreateInvitation(ctx context.Context, req *connect.Request[authv1.CreateInvitationRequest]) (*connect.Response[authv1.CreateInvitationResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	if a.Role != "owner" {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.invitationsUsecase(ctx, a, "POST", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.CreateInvitationResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) RevokeInvitation(ctx context.Context, req *connect.Request[authv1.RevokeInvitationRequest]) (*connect.Response[authv1.RevokeInvitationResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if a.TenantID == 0 {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	if a.Role != "owner" {
		return nil, authRPCError(auth.APIError{Status: 403, Message: "workspace access denied"})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.invitationsUsecase(ctx, a, "DELETE", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.RevokeInvitationResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) InspectInvitation(ctx context.Context, req *connect.Request[authv1.InspectInvitationRequest]) (*connect.Response[authv1.InspectInvitationResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (false && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.inspectInvitationUsecase(ctx, a, "POST", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.InspectInvitationResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) AcceptInvitation(ctx context.Context, req *connect.Request[authv1.AcceptInvitationRequest]) (*connect.Response[authv1.AcceptInvitationResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.acceptUsecase(ctx, a, "POST", req.Msg.GetId(), authQueryValues(req.Msg.Query), body, identity.Hash)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.AcceptInvitationResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) GetCompany(ctx context.Context, req *connect.Request[authv1.GetCompanyRequest]) (*connect.Response[authv1.GetCompanyResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.auth.CompanyInfoUsecase(ctx, a, req.Msg.GetId(), req.Msg.GetAction(), body)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.GetCompanyResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) GetRegistrationStatus(ctx context.Context, req *connect.Request[authv1.GetRegistrationStatusRequest]) (*connect.Response[authv1.GetRegistrationStatusResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.auth.RegistrationStatusUsecase(ctx, a, req.Msg.GetId(), req.Msg.GetAction(), body)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.GetRegistrationStatusResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) ListJoinRequests(ctx context.Context, req *connect.Request[authv1.ListJoinRequestsRequest]) (*connect.Response[authv1.ListJoinRequestsResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.auth.JoinRequestsUsecase(ctx, a, req.Msg.GetId(), req.Msg.GetAction(), body)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	data = map[string]any{"items": data}
	out := &authv1.ListJoinRequestsResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) ApplyToCompany(ctx context.Context, req *connect.Request[authv1.ApplyToCompanyRequest]) (*connect.Response[authv1.ApplyToCompanyResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	rawBody, _ := json.Marshal(body)
	if len(rawBody) > 4096 {
		return nil, authRPCError(auth.APIError{Status: 400, Message: "请求无效", Plain: true})
	}
	result, err := s.auth.ApplyToCompanyUsecase(ctx, a, req.Msg.GetId(), req.Msg.GetAction(), body)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.ApplyToCompanyResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}

func (s *server) DecideJoinRequest(ctx context.Context, req *connect.Request[authv1.DecideJoinRequestRequest]) (*connect.Response[authv1.DecideJoinRequestResponse], error) {
	identity, ok := ctx.Value(authRPCKey{}).(authRPCIdentity)
	if !ok || (true && identity.Principal.UserID == 0) {
		return nil, authRPCError(auth.APIError{Status: 401, Message: "sign in required"})
	}
	a := identity.Principal
	if !s.auth.RegistrationEnabled() {
		return nil, authRPCError(auth.APIError{Status: 404, Message: "endpoint not found", Plain: true})
	}
	body, err := authRPCBody(req.Msg.Body)
	_ = body
	if err != nil {
		return nil, authRPCError(err)
	}
	result, err := s.auth.DecideJoinRequestUsecase(ctx, a, req.Msg.GetId(), req.Msg.GetAction(), body)
	if err != nil {
		return nil, authRPCError(err)
	}
	data := result.Data
	if result.Status == 204 {
		data = map[string]any{}
	}
	out := &authv1.DecideJoinRequestResponse{}
	if err = rpccompat.FromLegacy(out, map[string]any{"data": data, "statusCode": result.Status}); err != nil {
		return nil, authRPCError(err)
	}
	return connect.NewResponse(out), nil
}
