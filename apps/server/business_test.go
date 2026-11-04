package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"golang.org/x/oauth2"
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
	s := &server{db: db, origin: "https://test.example.com", auth: auth.New(db, oauth2.Config{}, nil)}
	mux := http.NewServeMux()
	s.registerBusiness(mux)
	mux.HandleFunc("POST /api/auth/workspace", s.switchWorkspace)
	s.auth.Register(mux)
	call := func(method, path, token, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if token != "" {
			req.AddCookie(&http.Cookie{Name: auth.SessionCookie, Value: token})
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != status {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, response.Code, status, response.Body)
		}
		return response
	}
	body := `{"name":"Private product","category":"Books","price":42.10,"description":"Only first workspace"}`
	result := call("POST", "/api/products", a, body, 201)
	var created struct{ Product product }
	must(json.Unmarshal(result.Body.Bytes(), &created))
	path := "/api/products/" + strconv.FormatInt(created.Product.ID, 10)
	call("GET", path, a, "", 200)
	call("GET", path, b, "", 404)
	call("PUT", path, b, body, 404)
	call("DELETE", path, b, "", 404)
	call("GET", path, m, "", 200)
	call("POST", "/api/products", m, body, 403)
	call("GET", "/api/products", "", "", 401)
	result = call("GET", "/api/products?search=Private", b, "", 200)
	if !strings.Contains(result.Body.String(), `"total_products":0`) {
		t.Fatal(result.Body.String())
	}
	result = call("GET", "/api/products?search=Private", a, "", 200)
	if !strings.Contains(result.Body.String(), `"total_products":1`) {
		t.Fatal(result.Body.String())
	}
	call("POST", "/api/auth/workspace", a, `{"workspaceId":`+strconv.FormatInt(second, 10)+`}`, 404)
	call("DELETE", "/api/users/"+strconv.FormatInt(alice, 10), a, "", 409)
	call("DELETE", "/api/users/"+strconv.FormatInt(member, 10), a, "", 204)
	call("GET", path, m, "", 403)
	invited := makeSession(member, 0)
	result = call("POST", "/api/invitations", a, `{"email":"member@example.com"}`, 201)
	var invite struct{ URL string }
	must(json.Unmarshal(result.Body.Bytes(), &invite))
	token := strings.SplitN(invite.URL, "#", 2)[1]
	inviteBody, err := json.Marshal(map[string]string{"token": token})
	must(err)
	call("POST", "/api/invitations/accept", b, string(inviteBody), 404)
	call("POST", "/api/invitations/accept", invited, string(inviteBody), 200)
	call("POST", "/api/invitations/accept", invited, string(inviteBody), 404)
	call("GET", path, invited, "", 200)
	result = call("POST", "/api/invitations", a, `{"email":"member@example.com"}`, 201)
	must(json.Unmarshal(result.Body.Bytes(), &invite))
	token = strings.SplitN(invite.URL, "#", 2)[1]
	call("DELETE", "/api/invitations/"+hex.EncodeToString(auth.HashToken(token)), a, "", 204)
	inviteBody, err = json.Marshal(map[string]string{"token": token})
	must(err)
	call("POST", "/api/invitations/accept", invited, string(inviteBody), 404)
	runSQL("UPDATE sessions SET expires_at=now() WHERE token_hash=$1", auth.HashToken(invited))
	call("GET", path, invited, "", 401)
	call("POST", "/api/auth/logout", a, "", 204)
	call("GET", path, a, "", 401)
	verifyOIDCFlow(t, db)
}
