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
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("code") != "auth-code" || r.Form.Get("code_verifier") != verifier {
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
	mux := http.NewServeMux()
	s.auth.Register(mux)
	makeToken := func(nonce string) string {
		claims, err := json.Marshal(map[string]any{
			"iss": issuer, "sub": subject, "aud": client, "iat": time.Now().Unix(), "exp": time.Now().Add(time.Minute).Unix(),
			"nonce": nonce, "email": "oidc@example.com", "email_verified": true, "name": "OIDC test",
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
	for _, name := range []string{"wrong-state", "wrong-nonce", "success"} {
		t.Run(name, func(t *testing.T) {
			start := httptest.NewRecorder()
			mux.ServeHTTP(start, httptest.NewRequest("GET", "/api/auth/login", nil))
			redirect, err := url.Parse(start.Header().Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			flow := start.Result().Cookies()[0]
			params := redirect.Query()
			parts := strings.Split(flow.Value, ".")
			state, nonce := params.Get("state"), params.Get("nonce")
			verifier = parts[2]
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
			signedToken = makeToken(nonce)
			req := httptest.NewRequest("GET", "/api/auth/callback?code=auth-code&state="+state, nil)
			req.AddCookie(flow)
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, req)
			if response.Code != expected {
				t.Fatalf("got %d, want %d: %s", response.Code, expected, response.Body)
			}
			if name != "success" {
				return
			}
			session := response.Result().Cookies()[1]
			if !session.Secure || !session.HttpOnly || session.SameSite != http.SameSiteLaxMode {
				t.Fatal("session cookie must be secure")
			}
			req = httptest.NewRequest("GET", "/api/auth/session", nil)
			req.AddCookie(session)
			response = httptest.NewRecorder()
			s.session(response, req)
			if response.Code != 200 || !strings.Contains(response.Body.String(), "oidc@example.com") {
				t.Fatalf("session was not created: %d %s", response.Code, response.Body)
			}
		})
	}
}
