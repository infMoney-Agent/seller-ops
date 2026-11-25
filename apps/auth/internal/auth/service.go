package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"seller-ops/server/internal/httpresponse"
)

type Service struct {
	registration   *registrationConfig
	shortLogin     *shortLoginConfig
	db             *pgxpool.Pool
	oauth          oauth2.Config
	verifier       *oidc.IDTokenVerifier
	insecureHTTP   bool
	legacyOAuth    *oauth2.Config
	legacyVerifier *oidc.IDTokenVerifier
	sharedVerifier func(string) *oidc.IDTokenVerifier
	defaultOrg     string
}

// EnableOrganizations selects a server-mapped organization before the OAuth exchange.
// The verifier must enforce the exact organization-specific audience.
func (s *Service) EnableOrganizations(defaultOrg string, verifier func(string) *oidc.IDTokenVerifier) {
	s.defaultOrg, s.sharedVerifier = defaultOrg, verifier
}

func (s *Service) mappedOrganization(r *http.Request, org string) bool {
	if org == "" || org == "built-in" || strings.Contains(org, ".") || strings.Contains(org, "-org-") {
		return false
	}
	for _, c := range org {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	var exists bool
	err := s.db.QueryRow(r.Context(), "SELECT EXISTS(SELECT 1 FROM tenants WHERE casdoor_org=$1)", org).Scan(&exists)
	return err == nil && exists
}

func New(db *pgxpool.Pool, oauth oauth2.Config, verifier *oidc.IDTokenVerifier) *Service {
	return &Service{db: db, oauth: oauth, verifier: verifier}
}

// SetLegacy preserves the original administrator application during a client migration.
func (s *Service) SetLegacy(config oauth2.Config, verifier *oidc.IDTokenVerifier) {
	s.legacyOAuth = &config
	s.legacyVerifier = verifier
}

// EnableHTTP is an explicit deployment opt-in; request headers never control cookie security.
func (s *Service) EnableHTTP() { s.insecureHTTP = true }
func (s *Service) cookieName(name string) string {
	if s.insecureHTTP {
		return strings.TrimPrefix(name, "__Host-")
	}
	return name
}
func (s *Service) readCookie(r *http.Request, name string) string {
	value := readCookie(r, s.cookieName(name))
	if value == "" && s.insecureHTTP {
		return readCookie(r, name)
	}
	return value
}
func (s *Service) SessionHash(r *http.Request) []byte {
	return HashToken(s.readCookie(r, SessionCookie))
}

func (s *Service) Register(mux *http.ServeMux) {
	if s.registration != nil {
		s.registerRegistration(mux)
	}
	mux.HandleFunc("GET /api/auth/login-page", s.loginPage)
	mux.HandleFunc("POST /api/auth/login-page", s.loginPage)
	mux.HandleFunc("GET /api/auth/login", s.login)
	mux.HandleFunc("GET /api/auth/callback", s.callback)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
}

func SessionHash(r *http.Request) []byte {
	return HashToken(readCookie(r, SessionCookie))
}

const SessionCookie = "__Host-seller_session"

type Principal struct {
	UserID   int64
	TenantID int64
	Role     string
}

func NewToken() string {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(raw[:])
}

func HashToken(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}

func (s *Service) cookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: s.cookieName(name), Value: value, Path: "/", Secure: !s.insecureHTTP, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func readCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	if s.registration != nil && r.URL.Query().Get("mode") == "signup" {
		http.Redirect(w, r, "/register", http.StatusFound)
		return
	}
	selected := s.oauth
	provider := "primary"
	if r.URL.Query().Get("provider") == "legacy" {
		if s.legacyOAuth == nil {
			httpresponse.Fail(w, 404, "administrator login unavailable")
			return
		}
		selected = *s.legacyOAuth
		provider = "legacy"
	}
	state, nonce, verifier := NewToken(), NewToken(), oauth2.GenerateVerifier()
	organization := ""
	if s.sharedVerifier != nil && provider == "primary" {
		organization = r.URL.Query().Get("organization")
		if organization == "" {
			organization = "auto"
		}
		if organization == "auto" && r.URL.Query().Get("mode") == "signup" {
			organization = s.defaultOrg
		}
		if organization != "auto" {
			if !s.mappedOrganization(r, organization) {
				httpresponse.Fail(w, 400, "unknown company organization")
				return
			}
			selected.ClientID += "-org-" + organization
		}
	}
	flow := []string{state, nonce, verifier}
	if r.URL.Query().Get("return_to") == "/invite" {
		flow = append(flow, "invite")
	}
	if s.legacyOAuth != nil || organization != "" {
		if len(flow) == 3 {
			flow = append(flow, "")
		}
		flow = append(flow, provider)
	}
	if organization != "" {
		flow = append(flow, organization)
	}
	destination := selected.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier))
	if r.URL.Query().Get("mode") == "signup" {
		target, err := url.Parse(destination)
		if err == nil && strings.HasSuffix(target.Path, "/login/oauth/authorize") {
			target.Path = strings.TrimSuffix(target.Path, "/login/oauth/authorize") + "/signup/oauth/authorize"
			destination = target.String()
		}
	}
	if s.shortLogin != nil && r.URL.Query().Get("full") != "1" && r.URL.Query().Get("mode") != "signup" && provider == "primary" {
		if err := s.storeShortLogin(w, r, flow, destination); err != nil {
			httpresponse.Internal(w, err)
		}
		return
	}
	s.cookie(w, "__Host-seller_oidc", strings.Join(flow, "."), 600)
	http.Redirect(w, r, destination, http.StatusFound)
}

func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	flow := strings.Split(s.readCookie(r, "__Host-seller_oidc"), ".")
	if strings.HasPrefix(s.readCookie(r, "__Host-seller_oidc"), "short_") {
		stored, err := s.shortFlow(r, true, false)
		if err != nil {
			httpresponse.Fail(w, 400, "login expired; please sign in again")
			return
		}
		flow = stored.Flow
	}
	s.cookie(w, "__Host-seller_oidc", "", -1)
	code := r.URL.Query().Get("code")
	if (len(flow) != 3 && len(flow) != 4 && len(flow) != 5 && len(flow) != 6) || flow[0] == "" || flow[0] != r.URL.Query().Get("state") || code == "" {
		reason := "state_mismatch"
		if len(flow) == 1 && flow[0] == "" {
			reason = "missing_flow_cookie"
		} else if len(flow) != 3 && len(flow) != 4 && len(flow) != 5 {
			reason = "malformed_flow_cookie"
		} else if r.URL.Query().Get("state") == "" {
			reason = "missing_state"
		} else if code == "" {
			reason = "missing_code"
		}
		log.Printf("oidc callback rejected reason=%s flow_parts=%d state_present=%t code_present=%t", reason, len(flow), r.URL.Query().Get("state") != "", code != "")
		httpresponse.Fail(w, 400, "invalid login callback")
		return
	}
	selected, idVerifier := s.oauth, s.verifier
	organization := ""
	if len(flow) == 6 {
		organization = flow[5]
		if s.sharedVerifier == nil || flow[4] != "primary" || (organization != "auto" && !s.mappedOrganization(r, organization)) {
			httpresponse.Fail(w, 400, "invalid company login")
			return
		}
		if organization != "auto" {
			selected.ClientID += "-org-" + organization
			idVerifier = s.sharedVerifier(selected.ClientID)
		}
	} else if s.sharedVerifier != nil && !(len(flow) == 5 && flow[4] == "legacy") {
		httpresponse.Fail(w, 400, "login configuration changed; please sign in again")
		return
	}
	if len(flow) >= 5 && flow[4] != "primary" && flow[4] != "legacy" {
		httpresponse.Fail(w, 400, "invalid login callback")
		return
	}
	if s.legacyOAuth != nil && (len(flow) < 5 || flow[4] == "legacy") {
		selected = *s.legacyOAuth
		idVerifier = s.legacyVerifier
	}
	if len(flow) == 5 && flow[4] == "legacy" && s.legacyOAuth == nil {
		httpresponse.Fail(w, 400, "invalid login callback")
		return
	}
	ctx := r.Context()
	token, err := selected.Exchange(ctx, code, oauth2.VerifierOption(flow[2]))
	if err != nil {
		httpresponse.Fail(w, 502, "identity provider token exchange failed")
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		httpresponse.Fail(w, 502, "missing identity token")
		return
	}
	// The unverified owner only selects a server-allowlisted strict verifier.
	// No identity or authorization is accepted until signature, issuer, exact audience and nonce pass.
	if organization == "auto" {
		parts := strings.Split(raw, ".")
		var candidate struct {
			Owner string `json:"owner"`
		}
		if len(parts) != 3 || len(parts[1]) > 131072 {
			httpresponse.Fail(w, 401, "invalid identity token")
			return
		}
		payload, decodeErr := base64.RawURLEncoding.DecodeString(parts[1])
		if decodeErr != nil || json.Unmarshal(payload, &candidate) != nil || !s.mappedOrganization(r, candidate.Owner) {
			httpresponse.Fail(w, 403, "identity organization is not a business company")
			return
		}
		organization = candidate.Owner
		idVerifier = s.sharedVerifier(s.oauth.ClientID + "-org-" + organization)
	}
	identity, err := idVerifier.Verify(ctx, raw)
	if err != nil || identity.Nonce != flow[1] || identity.Subject == "" {
		httpresponse.Fail(w, 401, "invalid identity token")
		return
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Name     string `json:"name"`
		Owner    string `json:"owner"`
	}
	if err := identity.Claims(&claims); err != nil {
		httpresponse.Fail(w, 401, "invalid identity claims")
		return
	}
	if claims.Name == "" {
		claims.Name = claims.Email
	}
	if organization != "" {
		if claims.Owner != organization {
			httpresponse.Fail(w, 403, "identity organization mismatch")
			return
		}
		var conflict bool
		err = s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM users u JOIN memberships m ON m.user_id=u.id JOIN tenants t ON t.id=m.tenant_id WHERE u.issuer=$1 AND u.subject=$2 AND t.casdoor_org IS DISTINCT FROM $3)`, identity.Issuer, identity.Subject, organization).Scan(&conflict)
		if err != nil {
			httpresponse.Internal(w, err)
			return
		}
		if conflict {
			httpresponse.Fail(w, 403, "account belongs to another company")
			return
		}
	}
	key := NewToken()
	var tenant *int64
	err = s.db.QueryRow(ctx, `WITH account AS (
 INSERT INTO users(issuer,subject,email,email_verified,name,casdoor_org) VALUES($1,$2,$3,$4,$5,$7)
 ON CONFLICT(issuer,subject) DO UPDATE SET email=excluded.email,email_verified=excluded.email_verified,name=excluded.name,casdoor_org=CASE WHEN excluded.casdoor_org='' THEN users.casdoor_org ELSE excluded.casdoor_org END
 RETURNING id
) INSERT INTO sessions(token_hash,user_id,tenant_id,expires_at)
SELECT $6,account.id,(SELECT tenant_id FROM memberships WHERE user_id=account.id),now()+interval '12 hours'
FROM account RETURNING tenant_id`,
		identity.Issuer, identity.Subject, claims.Email, claims.Verified, claims.Name, HashToken(key), organization).Scan(&tenant)
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	s.cookie(w, SessionCookie, key, 43200)
	destination := "/dashboard"
	if tenant == nil {
		destination = "/company-required"
	}
	if len(flow) >= 4 && flow[3] == "invite" {
		destination = "/invite"
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	_, err := s.db.Exec(r.Context(), "DELETE FROM sessions WHERE token_hash=$1", HashToken(s.readCookie(r, SessionCookie)))
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	s.cookie(w, SessionCookie, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) Authenticate(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	a, err := s.AuthenticateToken(r.Context(), s.SessionToken(r))
	if err == nil {
		return a, true
	}
	var e APIError
	if errors.As(err, &e) {
		if e.ClearSession {
			s.cookie(w, SessionCookie, "", -1)
		}
		httpresponse.Fail(w, e.Status, e.Message)
	} else {
		httpresponse.Internal(w, err)
	}
	return a, false
}
