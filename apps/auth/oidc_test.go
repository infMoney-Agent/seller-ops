package main

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"

	"golang.org/x/oauth2"
	"seller-ops/server/internal/auth"
	"seller-ops/server/internal/httpresponse"
)

func verifyOIDCFlow(t *testing.T, db *pgxpool.Pool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const issuer = "https://identity.test"
	const client = "seller-ops-test"
	subject := auth.NewToken()
	var signedToken, verifier string
	acceptedCode := "auth-code"
	var expectedClient string
	var claimedOrg string
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("code") != acceptedCode || r.Form.Get("code_verifier") != verifier || r.Form.Get("client_id") != expectedClient {
			http.Error(w, "invalid authorization exchange", 400)
			return
		}
		httpresponse.JSON(w, 200, map[string]string{"access_token": "test-token", "token_type": "Bearer", "id_token": signedToken})
	}))
	defer tokenServer.Close()
	idTokenVerifier := oidc.NewVerifier(issuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: client})
	s := &server{db: db, auth: auth.New(db, oauth2.Config{
		ClientID: client, ClientSecret: "test-secret", RedirectURL: "https://seller.test/api/auth/callback",
		Endpoint: oauth2.Endpoint{AuthURL: issuer + "/authorize", TokenURL: tokenServer.URL, AuthStyle: oauth2.AuthStyleInParams},
		Scopes:   []string{"openid", "email", "profile"},
	}, idTokenVerifier)}
	legacy := oauth2.Config{ClientID: "seller-ops-legacy-test", ClientSecret: "legacy-secret", RedirectURL: "https://seller.test/api/auth/callback", Endpoint: oauth2.Endpoint{AuthURL: issuer + "/authorize", TokenURL: tokenServer.URL, AuthStyle: oauth2.AuthStyleInParams}, Scopes: []string{"openid", "email", "profile"}}
	s.auth.SetLegacy(legacy, oidc.NewVerifier(issuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: legacy.ClientID}))
	mux := http.NewServeMux()
	s.auth.Register(mux)
	makeToken := func(nonce, audience string) string {
		claims, err := json.Marshal(map[string]any{
			"iss": issuer, "sub": subject, "aud": audience, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
			"nonce": nonce, "email": "oidc@example.com", "email_verified": true, "name": "OIDC test",
			"owner": claimedOrg,
		})
		if err != nil {
			t.Fatal(err)
		}
		encode := base64.RawURLEncoding.EncodeToString
		content := encode([]byte(`{"alg":"RS256"}`)) + "." + encode(claims)
		digest := sha256.Sum256([]byte(content))
		signature, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
		if err != nil {
			t.Fatal(err)
		}
		return content + "." + encode(signature)
	}
	for _, name := range []string{"wrong-state", "wrong-nonce", "success", "invite-return", "legacy-success", "legacy-existing-flow", "wrong-audience"} {
		t.Run(name, func(t *testing.T) {
			start := httptest.NewRecorder()
			loginPath := "/api/auth/login"
			if strings.HasPrefix(name, "legacy-") {
				loginPath += "?provider=legacy"
			}
			if name == "invite-return" {
				loginPath += "?return_to=%2Finvite"
			}
			mux.ServeHTTP(start, httptest.NewRequest("GET", loginPath, nil))
			redirect, err := url.Parse(start.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			flow := start.Result().Cookies()[0]
			params := redirect.Query()
			expectedClient = params.Get("client_id")
			parts := strings.Split(flow.Value, ".")
			state, nonce := params.Get("state"), params.Get("nonce")
			verifier = parts[2]
			if name == "legacy-existing-flow" {
				flow.Value = strings.Join(parts[:3], ".")
			}
			if !flow.Secure || !flow.HttpOnly || state == "" || nonce == "" || params.Get("code_challenge_method") != "S256" {
				t.Fatal("login flow must use a secure cookie and PKCE")
			}
			expected := http.StatusSeeOther
			if name == "wrong-state" {
				state = "forged"
				expected = 400
			}
			if name == "wrong-nonce" {
				nonce = "forged"
				expected = 401
			}
			audience := params.Get("client_id")
			if name == "wrong-audience" {
				audience = legacy.ClientID
				expected = 401
			}
			signedToken = makeToken(nonce, audience)
			req := httptest.NewRequest("GET", "/api/auth/callback?code=auth-code&state="+state, nil)
			req.AddCookie(flow)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			if response.Code != expected {
				t.Fatalf("got %d, want %d: %s", response.Code, expected, response.Body)
			}
			if name != "success" && name != "invite-return" && !strings.HasPrefix(name, "legacy-") {
				return
			}
			destination := "/company-required"
			if name == "invite-return" {
				destination = "/invite"
			}
			if response.Header().Get("Location") != destination {
				t.Fatal("unbound login must return to onboarding or invitation", response.Header().Get("Location"))
			}
			session := response.Result().Cookies()[1]
			if !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
				t.Fatal("session cookie must be secure")
			}
			req = httptest.NewRequest("GET", "/api/auth/session", nil)
			req.AddCookie(session)
			response = httptest.NewRecorder()
			s.session(response, req)
			var account map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &account); err != nil || account["workspaceId"] != float64(0) {
				t.Fatal("new accounts must not automatically join or create a company", response.Body)
			}
			if response.Code != 200 || !strings.Contains(response.Body.String(), "oidc@example.com") {
				t.Fatalf("session was not created: %d %s", response.Code, response.Body)
			}
		})
	}
	// Shared clients must bind organization, audience and existing company together.
	orgA, orgB := "company_a_"+auth.NewToken()[:8], "company_b_"+auth.NewToken()[:8]
	var companyA int64
	if err := db.QueryRow(t.Context(), "INSERT INTO tenants(name,casdoor_org) VALUES('Shared A',$1) RETURNING id", orgA).Scan(&companyA); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(t.Context(), "INSERT INTO tenants(name,casdoor_org) VALUES('Shared B',$1)", orgB); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(t.Context(), "INSERT INTO memberships(tenant_id,user_id,role) SELECT $1,id,'owner' FROM users WHERE issuer=$2 AND subject=$3", companyA, issuer, subject); err != nil {
		t.Fatal(err)
	}
	s.auth.EnableOrganizations(orgA, func(c string) *oidc.IDTokenVerifier {
		return oidc.NewVerifier(issuer, &oidc.StaticKeySet{PublicKeys: []crypto.PublicKey{&key.PublicKey}}, &oidc.Config{ClientID: c})
	})
	for _, test := range []struct {
		name, org, owner, audience string
		expected                   int
	}{
		{"shared-success", orgA, orgA, client + "-org-" + orgA, 303},
		{"shared-wrong-owner", orgA, orgB, client + "-org-" + orgA, 403},
		{"shared-wrong-audience", orgA, orgA, client + "-org-" + orgB, 401},
		{"shared-cross-company", orgB, orgB, client + "-org-" + orgB, 403},
	} {
		t.Run(test.name, func(t *testing.T) {
			start := httptest.NewRecorder()
			mux.ServeHTTP(start, httptest.NewRequest("GET", "/api/auth/login?organization="+test.org, nil))
			if start.Code != 302 {
				t.Fatal(start.Code, start.Body)
			}
			destination, _ := url.Parse(start.Header().Get("Location"))
			p := destination.Query()
			cookie := start.Result().Cookies()[0]
			parts := strings.Split(cookie.Value, ".")
			verifier = parts[2]
			expectedClient = p.Get("client_id")
			claimedOrg = test.owner
			signedToken = makeToken(p.Get("nonce"), test.audience)
			if len(parts) != 6 || parts[5] != test.org || expectedClient != client+"-org-"+test.org {
				t.Fatal("Organization not bound to flow")
			}
			req := httptest.NewRequest("GET", "/api/auth/callback?state="+p.Get("state")+"&code=auth-code", nil)
			req.AddCookie(cookie)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			if response.Code != test.expected {
				t.Fatalf("got %d want %d: %s", response.Code, test.expected, response.Body)
			}
			if test.expected == 303 && response.Header().Get("Location") != "/dashboard" {
				t.Fatal("Mapped member did not enter company")
			}
		})
	}
	t.Run("shared-auto-organization", func(t *testing.T) {
		start := httptest.NewRecorder()
		mux.ServeHTTP(start, httptest.NewRequest("GET", "/api/auth/login", nil))
		loc, _ := url.Parse(start.Header().Get("Location"))
		p := loc.Query()
		cookie := start.Result().Cookies()[0]
		parts := strings.Split(cookie.Value, ".")
		verifier = parts[2]
		expectedClient = client
		claimedOrg = orgA
		signedToken = makeToken(p.Get("nonce"), client+"-org-"+orgA)
		if p.Get("client_id") != client || parts[5] != "auto" {
			t.Fatal("Auto login selected a company before identifying account")
		}
		req := httptest.NewRequest("GET", "/api/auth/callback?state="+p.Get("state")+"&code=auth-code", nil)
		req.AddCookie(cookie)
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, req)
		if r.Code != 303 || r.Header().Get("Location") != "/dashboard" {
			t.Fatal(r.Code, r.Body)
		}
	})
	t.Run("shared-unknown-organization", func(t *testing.T) {
		r := httptest.NewRecorder()
		mux.ServeHTTP(r, httptest.NewRequest("GET", "/api/auth/login?organization=built-in", nil))
		if r.Code != 400 {
			t.Fatal(r.Code)
		}
	})
	acceptedCode = "short-authorization-code"
	identityCalls := 0
	identityServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identityCalls++
		var body map[string]any
		if r.Method != "POST" || json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("Invalid identity form")
		}
		if body["password"] == "incorrect" {
			httpresponse.JSON(w, 200, map[string]any{"status": "error", "msg": "invalid password"})
			return
		}
		if r.URL.Query().Get("code_challenge_method") != "S256" || r.URL.Query().Get("nonce") == "" {
			t.Error("Lost PKCE or nonce")
		}
		httpresponse.JSON(w, 200, map[string]any{"status": "ok", "data": acceptedCode})
	}))
	defer identityServer.Close()
	if err := s.auth.EnableShortLogin(identityServer.URL, "shared-app"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"short-success", "short-invalid-csrf", "short-password-failure", "short-full-fallback"} {
		t.Run(name, func(t *testing.T) {
			start := httptest.NewRecorder()
			mux.ServeHTTP(start, httptest.NewRequest("GET", "/api/auth/login", nil))
			if start.Code != 302 || start.Header().Get("Location") != "/login" {
				t.Fatal("Short URL not used", start.Code, start.Header())
			}
			cookie := start.Result().Cookies()[0]
			if !strings.HasPrefix(cookie.Value, "short_") || strings.Contains(cookie.Value, ".") {
				t.Fatal("Browser must only hold opaque flow handle")
			}
			var raw []byte
			if err := db.QueryRow(t.Context(), "SELECT payload FROM auth_login_flows WHERE token_hash=$1", auth.HashToken(strings.TrimPrefix(cookie.Value, "short_"))).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var flow struct {
				Flow            []string
				Authorize, CSRF string
			}
			if json.Unmarshal(raw, &flow) != nil {
				t.Fatal("Invalid stored flow")
			}
			if name == "short-full-fallback" {
				full := httptest.NewRecorder()
				mux.ServeHTTP(full, httptest.NewRequest("GET", "/api/auth/login?full=1", nil))
				if full.Code != 302 || full.Header().Get("Location") == "/login" {
					t.Fatal("Additional authentication methods must remain available")
				}
				return
			}
			page := httptest.NewRecorder()
			get := httptest.NewRequest("GET", "/api/auth/login-page", nil)
			get.AddCookie(cookie)
			mux.ServeHTTP(page, get)
			if page.Header().Get("Referrer-Policy") != "same-origin" {
				t.Fatal("Native same-origin form must retain its Origin header")
			}
			if page.Code != 200 || !strings.Contains(page.Body.String(), `autocomplete="current-password"`) || strings.Contains(page.Body.String(), "code_challenge=") {
				t.Fatal("Invalid compact sign-in form", page.Code)
			}
			q, _ := url.Parse(flow.Authorize)
			verifier = flow.Flow[2]
			expectedClient = client
			claimedOrg = orgA
			signedToken = makeToken(q.Query().Get("nonce"), client+"-org-"+orgA)
			csrf, password := flow.CSRF, "valid-secret"
			if name == "short-invalid-csrf" {
				csrf = "forged"
			}
			if name == "short-password-failure" {
				password = "incorrect"
			}
			before := identityCalls
			body := url.Values{"username": {"test-account"}, "password": {password}, "csrf": {csrf}}
			req := httptest.NewRequest("POST", "/api/auth/login-page", strings.NewReader(body.Encode()))
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.AddCookie(cookie)
			result := httptest.NewRecorder()
			mux.ServeHTTP(result, req)
			if name == "short-invalid-csrf" {
				if result.Code != 403 || identityCalls != before {
					t.Fatal("Invalid CSRF reached identity service")
				}
				return
			}
			if result.Code != 303 {
				t.Fatal(result.Code, result.Body)
			}
			if name == "short-success" {
				if result.Header().Get("Location") != "/dashboard" {
					t.Fatal("Valid login did not reach company")
				}
				replay := httptest.NewRequest("POST", "/api/auth/login-page", strings.NewReader(body.Encode()))
				replay.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				replay.AddCookie(cookie)
				blocked := httptest.NewRecorder()
				mux.ServeHTTP(blocked, replay)
				if blocked.Code != 409 {
					t.Fatal("Consumed short flow replayed")
				}
			} else {
				if result.Header().Get("Location") != "/login" {
					t.Fatal("Failure must return to clean login URL")
				}
				if err := db.QueryRow(t.Context(), "SELECT payload FROM auth_login_flows WHERE token_hash=$1", auth.HashToken(strings.TrimPrefix(cookie.Value, "short_"))).Scan(&raw); err != nil || strings.Contains(string(raw), password) {
					t.Fatal("Password persisted in login flow")
				}
			}
		})
	}
}
