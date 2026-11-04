package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"

	"seller-ops/server/internal/httpresponse"
)

type Service struct {
	db       *pgxpool.Pool
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func New(db *pgxpool.Pool, oauth oauth2.Config, verifier *oidc.IDTokenVerifier) *Service {
	return &Service{db: db, oauth: oauth, verifier: verifier}
}

func (s *Service) Register(mux *http.ServeMux) {
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

func cookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}

func readCookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Service) login(w http.ResponseWriter, r *http.Request) {
	state, nonce, verifier := NewToken(), NewToken(), oauth2.GenerateVerifier()
	cookie(w, "__Host-seller_oidc", strings.Join([]string{state, nonce, verifier}, "."), 600)
	http.Redirect(w, r, s.oauth.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

func (s *Service) callback(w http.ResponseWriter, r *http.Request) {
	flow := strings.Split(readCookie(r, "__Host-seller_oidc"), ".")
	cookie(w, "__Host-seller_oidc", "", -1)
	code := r.URL.Query().Get("code")
	if len(flow) != 3 || flow[0] == "" || flow[0] != r.URL.Query().Get("state") || code == "" {
		httpresponse.Fail(w, 400, "invalid login callback")
		return
	}
	ctx := r.Context()
	token, err := s.oauth.Exchange(ctx, code, oauth2.VerifierOption(flow[2]))
	if err != nil {
		httpresponse.Fail(w, 502, "identity provider token exchange failed")
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		httpresponse.Fail(w, 502, "missing identity token")
		return
	}
	identity, err := s.verifier.Verify(ctx, raw)
	if err != nil || identity.Nonce != flow[1] || identity.Subject == "" {
		httpresponse.Fail(w, 401, "invalid identity token")
		return
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Name     string `json:"name"`
	}
	if err := identity.Claims(&claims); err != nil {
		httpresponse.Fail(w, 401, "invalid identity claims")
		return
	}
	if claims.Name == "" {
		claims.Name = claims.Email
	}
	key := NewToken()
	_, err = s.db.Exec(ctx, `WITH account AS (
  INSERT INTO users(issuer,subject,email,email_verified,name) VALUES($1,$2,$3,$4,$5)
  ON CONFLICT(issuer,subject) DO UPDATE SET email=excluded.email,email_verified=excluded.email_verified,name=excluded.name
  RETURNING id
 ) INSERT INTO sessions(token_hash,user_id,tenant_id,expires_at)
 SELECT $6,account.id,(SELECT min(tenant_id) FROM memberships WHERE user_id=account.id),now()+interval '12 hours' FROM account`,
		identity.Issuer, identity.Subject, claims.Email, claims.Verified, claims.Name, HashToken(key))
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	cookie(w, SessionCookie, key, 43200)
	http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
}

func (s *Service) logout(w http.ResponseWriter, r *http.Request) {
	_, err := s.db.Exec(r.Context(), "DELETE FROM sessions WHERE token_hash=$1", HashToken(readCookie(r, SessionCookie)))
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	cookie(w, SessionCookie, "", -1)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) Authenticate(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	var a Principal
	token := readCookie(r, SessionCookie)
	if token == "" {
		httpresponse.Fail(w, 401, "sign in required")
		return a, false
	}
	err := s.db.QueryRow(r.Context(), `SELECT s.user_id,coalesce(m.tenant_id,0),coalesce(m.role,'')
 FROM sessions s LEFT JOIN memberships m ON (m.tenant_id,m.user_id)=(s.tenant_id,s.user_id)
 WHERE s.token_hash=$1 AND s.expires_at>now()`, HashToken(token)).Scan(&a.UserID, &a.TenantID, &a.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		httpresponse.Fail(w, 401, "session expired")
		return a, false
	}
	if err != nil {
		httpresponse.Internal(w, err)
		return a, false
	}
	return a, true
}
