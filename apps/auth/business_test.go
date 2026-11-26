package main

import (
	"connectrpc.com/connect"
	"context"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"golang.org/x/oauth2"
	authv1 "seller-ops/contracts/sellerops/auth/v1"
	"seller-ops/contracts/sellerops/auth/v1/authv1connect"
	"seller-ops/rpccompat"
	"seller-ops/server/internal/auth"
)

func TestWorkspaceIsolation(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL must point to an empty, disposable database")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.Exec(ctx, schema)
	must(err)
	runSQL := func(query string, args ...any) {
		t.Helper()
		_, err := db.Exec(ctx, query, args...)
		must(err)
	}
	makeUser := func(name string) int64 {
		t.Helper()
		var id int64
		must(db.QueryRow(ctx, `INSERT INTO users(issuer,subject,email,email_verified,name) VALUES('test',$1,$2,true,$3) RETURNING id`, auth.NewToken(), name+"@example.com", name).Scan(&id))
		return id
	}
	makeWorkspace := func(name string) int64 {
		t.Helper()
		var id int64
		must(db.QueryRow(ctx, "INSERT INTO tenants(name) VALUES($1) RETURNING id", name).Scan(&id))
		return id
	}
	alice, bob, member := makeUser("alice"), makeUser("bob"), makeUser("member")
	first, second := makeWorkspace("first"), makeWorkspace("second")
	runSQL(`INSERT INTO memberships(tenant_id,user_id,role) VALUES($1,$2,'owner'),($3,$4,'owner'),($1,$5,'member')`, first, alice, second, bob, member)
	makeSession := func(user, tenant int64) string {
		t.Helper()
		token := auth.NewToken()
		runSQL(`INSERT INTO sessions(token_hash,user_id,tenant_id,expires_at) VALUES($1,$2,NULLIF($3::bigint,0),now()+interval '1 hour')`, auth.HashToken(token), user, tenant)
		return token
	}
	a, b, m := makeSession(alice, first), makeSession(bob, second), makeSession(member, first)
	s := &server{db: db, origins: []string{"https://test.example.com"}, auth: auth.New(db, oauth2.Config{}, nil)}
	mux := http.NewServeMux()
	s.auth.Register(mux)
	s.registerAuthRPC(mux)
	testAuthRPC(t, s, mux, a, b, m, first, alice)
	rpcServer := httptest.NewServer(mux)
	defer rpcServer.Close()
	client := authv1connect.NewAuthServiceClient(rpcServer.Client(), rpcServer.URL, connect.WithProtoJSON())
	withSession := func(h http.Header, token string) {
		if token != "" {
			h.Set("Cookie", auth.SessionCookie+"="+token)
		}
		h.Set("Origin", "https://test.example.com")
	}
	expectStatus := func(label string, got int, want int, err error) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: got %d, want %d (%v)", label, got, want, err)
		}
	}
	callHTTP := func(method, path, token string, want int) {
		t.Helper()
		req := httptest.NewRequest(method, path, nil)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: token})
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != want {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, response.Code, want, response.Body)
		}
	}

	productInputMsg := func() *authv1.ManagedProductInput {
		in := &authv1.ManagedProductInput{}
		must(rpccompat.FromLegacy(in, map[string]any{"name": "Private product", "category": "Books", "price": 42.10, "description": "Only first workspace"}))
		return in
	}
	createReq := connect.NewRequest(&authv1.CreateManagedProductRequest{Body: productInputMsg()})
	withSession(createReq.Header(), a)
	created, err := client.CreateManagedProduct(ctx, createReq)
	expectStatus("create product", authErrHTTPStatus(err), 200, err)
	if created.Msg.StatusCode != 201 {
		t.Fatalf("create product status=%d", created.Msg.StatusCode)
	}
	prodID := strconv.FormatInt(created.Msg.Data.Product.GetId(), 10)

	getProduct := func(token string, want int) {
		t.Helper()
		req := connect.NewRequest(&authv1.GetManagedProductRequest{Id: &prodID})
		withSession(req.Header(), token)
		_, err := client.GetManagedProduct(ctx, req)
		expectStatus("get product", authErrHTTPStatus(err), want, err)
	}
	getProduct(a, 200)
	getProduct(b, 404)
	updateReq := connect.NewRequest(&authv1.UpdateManagedProductRequest{Id: &prodID, Body: productInputMsg()})
	withSession(updateReq.Header(), b)
	_, err = client.UpdateManagedProduct(ctx, updateReq)
	expectStatus("cross-tenant update", authErrHTTPStatus(err), 404, err)
	delReq := connect.NewRequest(&authv1.DeleteManagedProductRequest{Id: &prodID})
	withSession(delReq.Header(), b)
	_, err = client.DeleteManagedProduct(ctx, delReq)
	expectStatus("cross-tenant delete", authErrHTTPStatus(err), 404, err)
	getProduct(m, 200)
	memberCreate := connect.NewRequest(&authv1.CreateManagedProductRequest{Body: productInputMsg()})
	withSession(memberCreate.Header(), m)
	_, err = client.CreateManagedProduct(ctx, memberCreate)
	expectStatus("member create product", authErrHTTPStatus(err), 403, err)
	anonList := connect.NewRequest(&authv1.ListManagedProductsRequest{})
	_, err = client.ListManagedProducts(ctx, anonList)
	expectStatus("anon list products", authErrHTTPStatus(err), 401, err)

	listSearch := func(token string) int64 {
		t.Helper()
		req := connect.NewRequest(&authv1.ListManagedProductsRequest{Query: &authv1.AuthQuery{Search: []string{"Private"}}})
		withSession(req.Header(), token)
		res, err := client.ListManagedProducts(ctx, req)
		must(err)
		return res.Msg.Data.GetTotalProducts()
	}
	if got := listSearch(b); got != 0 {
		t.Fatalf("bob saw %d private products", got)
	}
	if got := listSearch(a); got != 1 {
		t.Fatalf("alice saw %d private products", got)
	}

	removeMember := func(target int64, token string, want int) {
		t.Helper()
		tid := strconv.FormatInt(target, 10)
		req := connect.NewRequest(&authv1.RemoveMemberRequest{Id: &tid})
		withSession(req.Header(), token)
		res, err := client.RemoveMember(ctx, req)
		if err != nil {
			expectStatus("remove member", authErrHTTPStatus(err), want, err)
			return
		}
		expectStatus("remove member", int(res.Msg.StatusCode), want, nil)
	}
	removeMember(alice, a, 409)
	removeMember(member, a, 204)
	getProduct(m, 403)

	createInvite := func(token, email string) string {
		t.Helper()
		req := connect.NewRequest(&authv1.CreateInvitationRequest{Body: &authv1.InvitationInput{Email: &email}})
		withSession(req.Header(), token)
		res, err := client.CreateInvitation(ctx, req)
		must(err)
		if res.Msg.StatusCode != 201 {
			t.Fatalf("invite status=%d", res.Msg.StatusCode)
		}
		return strings.SplitN(res.Msg.Data.GetUrl(), "#", 2)[1]
	}
	acceptInvite := func(token, inviteToken string, want int) {
		t.Helper()
		req := connect.NewRequest(&authv1.AcceptInvitationRequest{Body: &authv1.InvitationToken{Token: &inviteToken}})
		withSession(req.Header(), token)
		res, err := client.AcceptInvitation(ctx, req)
		if err != nil {
			expectStatus("accept invite", authErrHTTPStatus(err), want, err)
			return
		}
		expectStatus("accept invite", int(res.Msg.StatusCode), want, nil)
	}
	inspectInvite := func(inviteToken string, want int) *authv1.InvitationInspection {
		t.Helper()
		req := connect.NewRequest(&authv1.InspectInvitationRequest{Body: &authv1.InvitationToken{Token: &inviteToken}})
		res, err := client.InspectInvitation(ctx, req)
		if err != nil {
			expectStatus("inspect invite", authErrHTTPStatus(err), want, err)
			return nil
		}
		expectStatus("inspect invite", int(res.Msg.StatusCode), want, nil)
		return res.Msg.Data
	}

	invited := makeSession(member, 0)
	token := createInvite(a, "member@example.com")
	acceptInvite(b, token, 404)
	acceptInvite(invited, token, 200)
	acceptInvite(invited, token, 404)
	getProduct(invited, 200)

	token = createInvite(a, "member@example.com")
	revokeID := hex.EncodeToString(auth.HashToken(token))
	revokeReq := connect.NewRequest(&authv1.RevokeInvitationRequest{Id: &revokeID})
	withSession(revokeReq.Header(), a)
	revokeRes, err := client.RevokeInvitation(ctx, revokeReq)
	must(err)
	expectStatus("revoke invite", int(revokeRes.Msg.StatusCode), 204, nil)
	acceptInvite(invited, token, 404)

	// A valid invite never moves an account away from an existing company.
	blockedToken := createInvite(b, "alice@example.com")
	acceptInvite(a, blockedToken, 409)
	preview := inspectInvite(blockedToken, 200)
	if strings.Contains(preview.GetEmailHint(), "alice@example.com") {
		t.Fatal("invitation preview leaked full email")
	}
	var company int64
	must(db.QueryRow(ctx, "SELECT tenant_id FROM memberships WHERE user_id=$1", alice).Scan(&company))
	if company != first {
		t.Fatal("invitation changed existing company")
	}
	if _, err = db.Exec(ctx, "INSERT INTO memberships(tenant_id,user_id,role) VALUES($1,$2,'member')", second, alice); err == nil {
		t.Fatal("database allowed multiple companies")
	}
	// Accepting a same-company invite preserves owner privileges.
	ownerToken := createInvite(a, "alice@example.com")
	acceptInvite(a, ownerToken, 200)
	var ownerRole string
	must(db.QueryRow(ctx, "SELECT role FROM memberships WHERE user_id=$1", alice).Scan(&ownerRole))
	if ownerRole != "owner" {
		t.Fatal("invitation downgraded owner")
	}
	getProduct(m, 200)
	// Verification is required and a failed attempt does not consume the invitation.
	unverified := makeUser("unverified")
	runSQL("UPDATE users SET email_verified=false WHERE id=$1", unverified)
	unverifiedSession := makeSession(unverified, 0)
	verificationToken := createInvite(a, "unverified@example.com")
	acceptInvite(unverifiedSession, verificationToken, 403)
	inspectInvite(verificationToken, 200)
	s.allowUnverifiedInvites = true
	acceptInvite(b, verificationToken, 404)
	acceptInvite(unverifiedSession, verificationToken, 200)
	var emailStillUnverified bool
	must(db.QueryRow(ctx, "SELECT NOT email_verified FROM users WHERE id=$1", unverified).Scan(&emailStillUnverified))
	if !emailStillUnverified {
		t.Fatal("verification bypass must not forge email ownership")
	}
	crossToken := createInvite(b, "unverified@example.com")
	acceptInvite(unverifiedSession, crossToken, 409)
	s.allowUnverifiedInvites = false

	runSQL("UPDATE sessions SET expires_at=now() WHERE token_hash=$1", auth.HashToken(invited))
	getProduct(invited, 401)
	callHTTP("POST", "/api/auth/logout", a, 204)
	getProduct(a, 401)
	verifyOIDCFlow(t, db)
}
