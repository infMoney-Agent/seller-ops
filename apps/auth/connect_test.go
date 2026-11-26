package main

import (
	"connectrpc.com/connect"
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	authv1 "seller-ops/contracts/sellerops/auth/v1"
	"seller-ops/contracts/sellerops/auth/v1/authv1connect"
	commonv1 "seller-ops/contracts/sellerops/common/v1"
	"seller-ops/rpccompat"
	"seller-ops/server/internal/auth"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

func authErrHTTPStatus(err error) int {
	if err == nil {
		return 200
	}
	var ce *connect.Error
	if errors.As(err, &ce) {
		for _, d := range ce.Details() {
			if msg, e := d.Value(); e == nil {
				if detail, ok := msg.(*commonv1.ErrorDetail); ok {
					var code int
					if _, e := fmt.Sscanf(detail.Reason, "HTTP_%d", &code); e == nil && code > 0 {
						return code
					}
				}
			}
		}
	}
	return 500
}

func TestLegacyAuthJSONRoutesRemoved(t *testing.T) {
	svc := auth.New(nil, oauth2.Config{}, nil)
	if err := svc.EnableRegistration("http://127.0.0.1:18000", "app", "client", "secret", "https://auth.example.test"); err != nil {
		t.Fatal(err)
	}
	s := &server{origins: []string{"https://test.example.com"}, auth: svc}
	mux := http.NewServeMux()
	s.auth.Register(mux)
	s.registerAuthRPC(mux)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/auth/session"},
		{"GET", "/api/products"},
		{"POST", "/api/products"},
		{"GET", "/api/products/1"},
		{"GET", "/api/users"},
		{"DELETE", "/api/users/1"},
		{"GET", "/api/invitations"},
		{"POST", "/api/invitations"},
		{"POST", "/api/invitations/inspect"},
		{"POST", "/api/invitations/accept"},
		{"GET", "/api/auth/registration/status"},
		{"GET", "/api/auth/company"},
		{"GET", "/api/auth/join-requests"},
		{"POST", "/api/auth/join-requests"},
		{"POST", "/api/auth/join-requests/1/approve"},
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(tc.method, tc.path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatalf("legacy route %s %s still registered: %d", tc.method, tc.path, w.Code)
		}
	}
}

func testAuthRPC(t *testing.T, s *server, mux *http.ServeMux, a, b, member string, tenant, user int64) {
	t.Helper()
	server := httptest.NewServer(mux)
	defer server.Close()
	client := authv1connect.NewAuthServiceClient(server.Client(), server.URL, connect.WithProtoJSON())
	ctx := context.Background()
	cookie := func(h http.Header, token string) {
		h.Set("Cookie", auth.SessionCookie+"="+token)
		h.Set("Origin", "https://test.example.com")
	}
	req := connect.NewRequest(&authv1.GetSessionRequest{})
	cookie(req.Header(), a)
	req.Header().Set("x-seller-user", "999999")
	req.Header().Set("x-seller-workspace", "999999")
	session, err := client.GetSession(ctx, req)
	if err != nil || session.Msg.Data.GetId() != user || session.Msg.Data.GetWorkspaceId() != tenant || session.Msg.StatusCode != 200 {
		t.Fatal("session identity not server derived", err)
	}
	noSession := connect.NewRequest(&authv1.GetSessionRequest{})
	if _, err = client.GetSession(ctx, noSession); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatal("anonymous session accepted", err)
	}
	raw := map[string]any{"name": "RPC record", "category": "Books", "price": 18.25, "description": "isolated RPC fixture"}
	input := &authv1.ManagedProductInput{}
	if err = rpccompat.FromLegacy(input, raw); err != nil {
		t.Fatal(err)
	}
	create := connect.NewRequest(&authv1.CreateManagedProductRequest{Body: input})
	cookie(create.Header(), member)
	if _, err = client.CreateManagedProduct(ctx, create); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatal("member created product", err)
	}
	cookie(create.Header(), a)
	created, err := client.CreateManagedProduct(ctx, create)
	if err != nil || created.Msg.StatusCode != 201 {
		t.Fatal("create", err)
	}
	id := fmt.Sprint(created.Msg.Data.Product.GetId())
	get := connect.NewRequest(&authv1.GetManagedProductRequest{Id: &id})
	cookie(get.Header(), b)
	if _, err = client.GetManagedProduct(ctx, get); connect.CodeOf(err) != connect.CodeNotFound {
		t.Fatal("cross-company product leaked", err)
	}
	cookie(get.Header(), a)
	got, err := client.GetManagedProduct(ctx, get)
	if err != nil || got.Msg.Data.Product.GetName() != "RPC record" {
		t.Fatal(err)
	}
	list := connect.NewRequest(&authv1.ListMembersRequest{})
	cookie(list.Header(), a)
	members, err := client.ListMembers(ctx, list)
	if err != nil || len(members.Msg.Data.Items) != 2 {
		t.Fatal("members", err)
	}
	ownerID := fmt.Sprint(user)
	removeOwner := connect.NewRequest(&authv1.RemoveMemberRequest{Id: &ownerID})
	cookie(removeOwner.Header(), a)
	if _, err = client.RemoveMember(ctx, removeOwner); connect.CodeOf(err) != connect.CodeAborted {
		t.Fatal("last owner removed", err)
	}
	email := "rpc-invited@example.test"
	invite := connect.NewRequest(&authv1.CreateInvitationRequest{Body: &authv1.InvitationInput{Email: &email}})
	cookie(invite.Header(), a)
	invited, err := client.CreateInvitation(ctx, invite)
	if err != nil || invited.Msg.StatusCode != 201 {
		t.Fatal("invite", err)
	}
	token := strings.SplitN(invited.Msg.Data.GetUrl(), "#", 2)[1]
	inspect := connect.NewRequest(&authv1.InspectInvitationRequest{Body: &authv1.InvitationToken{Token: &token}})
	checked, err := client.InspectInvitation(ctx, inspect)
	if err != nil || checked.Msg.Data.GetCompanyName() == "" {
		t.Fatal("anonymous inspect failed", err)
	}
	var hash string
	if err = s.db.QueryRow(ctx, "SELECT encode(token_hash,'hex') FROM invitations WHERE tenant_id=$1 AND email=$2", tenant, email).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	revoke := connect.NewRequest(&authv1.RevokeInvitationRequest{Id: &hash})
	cookie(revoke.Header(), a)
	revoked, err := client.RevokeInvitation(ctx, revoke)
	if err != nil || revoked.Msg.StatusCode != 204 {
		t.Fatal("revoke", err)
	}
	del := connect.NewRequest(&authv1.DeleteManagedProductRequest{Id: &id})
	cookie(del.Header(), a)
	if _, err = client.DeleteManagedProduct(ctx, del); err != nil {
		t.Fatal("cleanup product", err)
	}
}
