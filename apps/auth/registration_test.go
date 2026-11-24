package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
	"seller-ops/server/internal/auth"
)

func TestCompanyRegistration(t *testing.T) {
	connection := os.Getenv("TEST_DATABASE_URL")
	if connection == "" {
		t.Skip("disposable TEST_DATABASE_URL required")
	}
	ctx := context.Background()
	db, e := pgxpool.New(ctx, connection)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(ctx, schema); e != nil {
		t.Fatal(e)
	}
	var mu sync.Mutex
	organizations := map[string]map[string]any{}
	accounts := map[string]map[string]any{}
	failUser := false
	identity := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		client, secret, ok := r.BasicAuth()
		if !ok || client != "shared" || secret != "private" {
			t.Error("Identity credentials missing")
		}
		var body map[string]any
		if r.Method == "POST" {
			_ = json.NewDecoder(r.Body).Decode(&body)
		}
		var data any
		status, msg := "ok", ""
		switch r.URL.Path {
		case "/api/add-organization":
			name := body["name"].(string)
			organizations[name] = body
			data = "Affected"
		case "/api/add-user":
			if failUser {
				failUser = false
				status = "error"
				msg = "injected failure"
				break
			}
			for _, user := range accounts {
				if user["name"] == body["name"] {
					status = "error"
					msg = "Username already exists"
				}
			}
			if status == "ok" {
				if body["isAdmin"] != false {
					t.Error("Company owner became Casdoor administrator")
				}
				accounts[body["owner"].(string)+"/"+body["name"].(string)] = body
				data = "Affected"
			}
		case "/api/get-user":
			data = accounts[r.URL.Query().Get("id")]
		case "/api/delete-user":
			delete(accounts, body["owner"].(string)+"/"+body["name"].(string))
			data = "Affected"
		case "/api/get-users":
			list := []any{}
			for _, user := range accounts {
				if user["owner"] == r.URL.Query().Get("owner") {
					list = append(list, user)
				}
			}
			data = list
		case "/api/get-organization":
			data = organizations[strings.TrimPrefix(r.URL.Query().Get("id"), "admin/")]
		case "/api/delete-organization":
			delete(organizations, body["name"].(string))
			data = "Affected"
		default:
			t.Error("Unexpected identity endpoint", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"status": status, "msg": msg, "data": data})
	}))
	defer identity.Close()
	service := auth.New(db, oauth2.Config{}, nil)
	service.EnableOrganizations("unused", func(string) *oidc.IDTokenVerifier { return nil })
	if e = service.EnableRegistration(identity.URL, "app", "shared", "private", "https://registration.test"); e != nil {
		t.Fatal(e)
	}
	s := &server{db: db, auth: service, origins: []string{"https://seller.test"}, allowUnverifiedInvites: true}
	mux := http.NewServeMux()
	s.auth.Register(mux)
	s.registerBusiness(mux)
	mux.HandleFunc("GET /api/auth/session", s.session)
	call := func(method, path, body string, cookies []*http.Cookie, headers map[string]string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, "https://seller.test"+path, strings.NewReader(body))
		for _, cookie := range cookies {
			req.AddCookie(cookie)
		}
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)
		return w
	}
	formHeaders := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	csrfPattern := regexp.MustCompile(`name="csrf" value="([A-Za-z0-9_-]+)"`)
	start := func(mode string) ([]*http.Cookie, string) {
		t.Helper()
		page := call("GET", "/api/auth/register-page?mode="+mode, "", nil, nil)
		if page.Code != 200 {
			t.Fatal(page.Code, page.Body)
		}
		matched := csrfPattern.FindStringSubmatch(page.Body.String())
		if len(matched) != 2 {
			t.Fatal("Missing CSRF")
		}
		return page.Result().Cookies(), matched[1]
	}
	submit := func(cookies []*http.Cookie, csrf, mode, username, email, company, target string) *httptest.ResponseRecorder {
		t.Helper()
		return call("POST", "/api/auth/register-page", url.Values{"csrf": {csrf}, "mode": {mode}, "username": {username}, "email": {email}, "company": {company}, "target": {target}, "password": {"S3cure-password"}, "confirm": {"S3cure-password"}, "role": {"owner"}}.Encode(), cookies, formHeaders)
	}
	type userSession struct {
		ID, WorkspaceID int64
		Role            string
	}
	account := func(response *httptest.ResponseRecorder) ([]*http.Cookie, userSession) {
		t.Helper()
		if response.Code != 303 {
			t.Fatal(response.Code, response.Body)
		}
		cookies := response.Result().Cookies()
		result := call("GET", "/api/auth/session", "", cookies, nil)
		var session userSession
		if result.Code != 200 || json.Unmarshal(result.Body.Bytes(), &session) != nil {
			t.Fatal(result.Code, result.Body)
		}
		return cookies, session
	}
	suffix := strings.ToLower(auth.NewToken()[:8])
	suffix = strings.NewReplacer("-", "a", "_", "b").Replace(suffix)
	companyName := "Registration " + suffix
	cookies, csrf := start("create")
	ownerResponse := submit(cookies, csrf, "create", "owner"+suffix, "owner"+suffix+"@example.com", companyName, "")
	ownerCookies, owner := account(ownerResponse)
	if owner.Role != "owner" || owner.WorkspaceID == 0 || ownerResponse.Header().Get("Location") != "/dashboard" {
		t.Fatal("New company owner was not created")
	}
	var code string
	code = fmt.Sprintf("C%d", owner.WorkspaceID)
	beforeOrg := len(organizations)
	replay := submit(cookies, csrf, "create", "owner"+suffix, "owner"+suffix+"@example.com", companyName, "")
	if replay.Code != 303 || len(organizations) != beforeOrg {
		t.Fatal("Duplicate registration created another company")
	}
	second, secondCSRF := start("create")
	otherResponse := submit(second, secondCSRF, "create", "other"+suffix, "other"+suffix+"@example.com", "Other "+suffix, "")
	otherCookies, _ := account(otherResponse)
	join, joinCSRF := start("join")
	pendingResponse := submit(join, joinCSRF, "join", "pending"+suffix, "pending"+suffix+"@example.com", "", code)
	pendingCookies, pending := account(pendingResponse)
	if pending.WorkspaceID != 0 || pending.Role != "" || pendingResponse.Header().Get("Location") != "/company-required" {
		t.Fatal("Applicant received access before approval")
	}
	if got := call("GET", "/api/products", "", pendingCookies, nil); got.Code != 403 {
		t.Fatal("Pending applicant can access company products")
	}
	list := call("GET", "/api/auth/join-requests", "", ownerCookies, nil)
	var requests []struct{ ID int64 }
	_ = json.Unmarshal(list.Body.Bytes(), &requests)
	if len(requests) != 1 {
		t.Fatal("Owner did not receive request", list.Body)
	}
	requestID := requests[0].ID
	path := fmt.Sprintf("/api/auth/join-requests/%d/approve", requestID)
	if got := call("POST", path, "{}", otherCookies, nil); got.Code != 404 {
		t.Fatal("Foreign owner approved request")
	}
	if got := call("POST", path, "{}", pendingCookies, nil); got.Code != 403 {
		t.Fatal("Applicant approved themselves")
	}
	for i := 0; i < 2; i++ {
		if got := call("POST", path, "{}", ownerCookies, nil); got.Code != 200 {
			t.Fatal(got.Code, got.Body)
		}
	}
	approved := call("GET", "/api/auth/session", "", pendingCookies, nil)
	var member userSession
	_ = json.Unmarshal(approved.Body.Bytes(), &member)
	if member.WorkspaceID != owner.WorkspaceID || member.Role != "member" {
		t.Fatal("Approval did not grant only member access", approved.Body)
	}
	rejectCookies, rejectCSRF := start("join")
	rejectedResponse := submit(rejectCookies, rejectCSRF, "join", "reject"+suffix, "reject"+suffix+"@example.com", "", code)
	rejectedCookies, _ := account(rejectedResponse)
	list = call("GET", "/api/auth/join-requests", "", ownerCookies, nil)
	_ = json.Unmarshal(list.Body.Bytes(), &requests)
	rejectPath := fmt.Sprintf("/api/auth/join-requests/%d/reject", requests[0].ID)
	if got := call("POST", rejectPath, "{}", ownerCookies, nil); got.Code != 200 {
		t.Fatal(got.Code, got.Body)
	}
	if got := call("GET", "/api/products", "", rejectedCookies, nil); got.Code != 403 {
		t.Fatal("Rejected applicant gained access")
	}
	invitation := call("POST", "/api/invitations", `{"email":"invite`+suffix+`@example.com"}`, ownerCookies, map[string]string{"Content-Type": "application/json"})
	var link struct{ URL string }
	_ = json.Unmarshal(invitation.Body.Bytes(), &link)
	inviteURL, _ := url.Parse(link.URL)
	invited, invitedCSRF := start("create")
	prepared := call("POST", "/api/auth/registration/prepare", `{"token":"`+inviteURL.Fragment+`"}`, invited, map[string]string{"Content-Type": "application/json", "X-Registration-CSRF": invitedCSRF})
	if prepared.Code != 200 {
		t.Fatal(prepared.Code, prepared.Body)
	}
	beforeOrg = len(organizations)
	invitedResponse := submit(invited, invitedCSRF, "create", "invite"+suffix, "invite"+suffix+"@example.com", "FORGED COMPANY", "")
	_, invitedUser := account(invitedResponse)
	if invitedUser.Role != "member" || invitedUser.WorkspaceID != owner.WorkspaceID || len(organizations) != beforeOrg {
		t.Fatal("Invitation created a company or owner role")
	}
	blocked, blockedCSRF := start("create")
	invalid := submit(blocked, "forged", "create", "invalid"+suffix, "invalid"+suffix+"@example.com", "x", "")
	if invalid.Code != 403 {
		t.Fatal("Missing CSRF protection")
	}
	beforeOrg = len(organizations)
	duplicate := submit(blocked, blockedCSRF, "create", "owner"+suffix, "duplicate"+suffix+"@example.com", "Duplicate", "")
	if duplicate.Code != 303 || len(organizations) != beforeOrg {
		t.Fatal("Duplicate account left an orphan organization")
	}
	failed, failedCSRF := start("create")
	mu.Lock()
	failUser = true
	mu.Unlock()
	beforeOrg = len(organizations)
	failure := submit(failed, failedCSRF, "create", "failure"+suffix, "failure"+suffix+"@example.com", "Injected failure", "")
	if failure.Code != 303 || len(organizations) != beforeOrg {
		t.Fatal("Failed account creation left an organization")
	}
	var staleUser int64
	if e = db.QueryRow(ctx, "INSERT INTO users(issuer,subject,email,email_verified,name,casdoor_org) VALUES('test',$1,'stale@example.invalid',false,'stale','retired') RETURNING id", auth.NewToken()).Scan(&staleUser); e != nil {
		t.Fatal(e)
	}
	staleToken := auth.NewToken()
	if _, e = db.Exec(ctx, "INSERT INTO memberships(tenant_id,user_id,role) VALUES($1,$2,'member')", owner.WorkspaceID, staleUser); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(ctx, "INSERT INTO sessions(token_hash,user_id,tenant_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')", auth.HashToken(staleToken), staleUser, owner.WorkspaceID); e != nil {
		t.Fatal(e)
	}
	staleResponse := call("GET", "/api/auth/session", "", []*http.Cookie{{Name: auth.SessionCookie, Value: staleToken}}, nil)
	if staleResponse.Code != 401 || len(staleResponse.Result().Cookies()) == 0 || staleResponse.Result().Cookies()[0].MaxAge >= 0 {
		t.Fatal("Invalid company session must be expired and request fresh login", staleResponse.Code)
	}
	var leaked bool
	if e = db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM registration_flows WHERE payload::text LIKE '%S3cure-password%')").Scan(&leaked); e != nil || leaked {
		t.Fatal("Password stored in registration flow", e)
	}
}
