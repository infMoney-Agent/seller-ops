package auth

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type shortLoginConfig struct {
	Base        *url.URL
	Application string
}
type shortLoginFlow struct {
	Flow      []string `json:"flow"`
	Authorize string   `json:"authorize"`
	CSRF      string   `json:"csrf"`
	Username  string   `json:"username,omitempty"`
	Error     string   `json:"error,omitempty"`
}

// EnableShortLogin uses the configured internal identity endpoint, never a URL from a browser.
func (s *Service) EnableShortLogin(identity, application string) error {
	u, err := url.Parse(identity)
	if err != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || application == "" {
		return errors.New("invalid short login identity configuration")
	}
	s.shortLogin = &shortLoginConfig{Base: u, Application: application}
	return nil
}

func (s *Service) storeShortLogin(w http.ResponseWriter, r *http.Request, parts []string, authorize string) error {
	key := NewToken()
	data, err := json.Marshal(shortLoginFlow{Flow: parts, Authorize: authorize, CSRF: NewToken()})
	if err != nil {
		return err
	}
	_, err = s.db.Exec(r.Context(), "INSERT INTO auth_login_flows(token_hash,payload,expires_at) VALUES($1,$2,now()+interval '10 minutes')", HashToken(key), data)
	if err != nil {
		return err
	}
	// Opaque browser cookie: PKCE verifier, nonce and authorization URL remain on the server.
	s.cookie(w, "__Host-seller_oidc", "short_"+key, 600)
	_, _ = s.db.Exec(r.Context(), "DELETE FROM auth_login_flows WHERE expires_at<now()")
	http.Redirect(w, r, "/login", http.StatusFound)
	return nil
}

func (s *Service) shortFlow(r *http.Request, consume, claim bool) (shortLoginFlow, error) {
	var data []byte
	var flow shortLoginFlow
	key := strings.TrimPrefix(s.readCookie(r, "__Host-seller_oidc"), "short_")
	if len(key) != 43 {
		return flow, pgx.ErrNoRows
	}
	query := "SELECT payload FROM auth_login_flows WHERE token_hash=$1 AND expires_at>now()"
	if consume {
		query = "DELETE FROM auth_login_flows WHERE token_hash=$1 AND expires_at>now() RETURNING payload"
	}
	if claim {
		query = "UPDATE auth_login_flows SET submitting=true WHERE token_hash=$1 AND expires_at>now() AND NOT submitting RETURNING payload"
	}
	err := s.db.QueryRow(r.Context(), query, HashToken(key)).Scan(&data)
	if err != nil {
		return flow, err
	}
	err = json.Unmarshal(data, &flow)
	return flow, err
}

func (s *Service) loginPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	// Native form POSTs with no-referrer serialize Origin as null (Fetch Standard).
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if s.shortLogin == nil {
		http.NotFound(w, r)
		return
	}
	flow, err := s.shortFlow(r, false, false)
	if err != nil {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			s.login(w, r)
		} else {
			http.Error(w, "登录已过期，请重新打开 /login。", http.StatusConflict)
		}
		return
	}
	if r.Method == http.MethodPost {
		s.submitShortLogin(w, r, flow)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, POST")
		w.WriteHeader(405)
		return
	}
	query := url.Values{"full": {"1"}}
	if len(flow.Flow) >= 4 && flow.Flow[3] == "invite" {
		query.Set("return_to", "/invite")
	}
	if len(flow.Flow) == 6 && flow.Flow[5] != "auto" {
		query.Set("organization", flow.Flow[5])
	}
	nonce := NewToken()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+nonce+"'; script-src 'nonce-"+nonce+"'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
	_ = shortLoginHTML.Execute(w, struct {
		CSRF, Username, Error, Nonce, FullURL string
		Registration                          bool
	}{flow.CSRF, flow.Username, flow.Error, nonce, "/api/auth/login?" + query.Encode(), s.registration != nil})
}

func (s *Service) submitShortLogin(w http.ResponseWriter, r *http.Request, flow shortLoginFlow) {
	r.Body = http.MaxBytesReader(w, r.Body, 8192)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "登录请求无效", 400)
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(flow.CSRF)) != 1 {
		http.Error(w, "登录验证已失效，请刷新后重试。", 403)
		return
	}
	username, password := strings.TrimSpace(r.PostForm.Get("username")), r.PostForm.Get("password")
	if username == "" || len(username) > 254 || password == "" || len(password) > 1024 {
		http.Error(w, "请填写有效的账号和密码。", 400)
		return
	}
	if _, err := s.shortFlow(r, false, true); err != nil {
		http.Error(w, "正在登录，请勿重复提交。", 409)
		return
	}
	flow.Username = username
	failure := func() {
		flow.Error = "登录未完成，请检查账号和密码；需要额外验证时，请使用其他登录方式。"
		data, _ := json.Marshal(flow)
		key := strings.TrimPrefix(s.readCookie(r, "__Host-seller_oidc"), "short_")
		_, _ = s.db.Exec(r.Context(), "UPDATE auth_login_flows SET payload=$2,submitting=false WHERE token_hash=$1", HashToken(key), data)
		http.Redirect(w, r, "/login", http.StatusSeeOther)
	}
	authorize, err := url.Parse(flow.Authorize)
	if err != nil {
		failure()
		return
	}
	q := authorize.Query()
	parameters := url.Values{"clientId": {q.Get("client_id")}, "responseType": {"code"}, "redirectUri": {q.Get("redirect_uri")}, "type": {"code"}, "scope": {q.Get("scope")}, "state": {q.Get("state")}, "nonce": {q.Get("nonce")}, "code_challenge": {q.Get("code_challenge")}, "code_challenge_method": {q.Get("code_challenge_method")}}
	organization := "built-in"
	if len(flow.Flow) == 6 && flow.Flow[5] != "auto" {
		organization = flow.Flow[5]
	}
	body, _ := json.Marshal(map[string]any{"application": s.shortLogin.Application, "organization": organization, "username": username, "password": password, "type": "code", "signinMethod": "Password", "autoSignin": false, "language": "zh"})
	endpoint := s.shortLogin.Base.ResolveReference(&url.URL{Path: "/api/login", RawQuery: parameters.Encode()})
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, endpoint.String(), bytes.NewReader(body))
	if err != nil {
		failure()
		return
	}
	req.Header.Set("Content-Type", "application/json")
	// The public gateway overwrites forwarding headers; the auth listener is loopback-only.
	if ip := r.Header.Get("X-Forwarded-For"); ip != "" {
		req.Header.Set("X-Forwarded-For", ip)
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(req)
	if err != nil {
		failure()
		return
	}
	defer response.Body.Close()
	var result struct {
		Status string          `json:"status"`
		Data   json.RawMessage `json:"data"`
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result) != nil || result.Status != "ok" {
		failure()
		return
	}
	var code string
	if json.Unmarshal(result.Data, &code) != nil || len(code) < 16 || len(code) > 2048 {
		failure()
		return
	}
	// Complete the same PKCE + signed ID-token callback without exposing its query in the address bar.
	callback := r.Clone(r.Context())
	u := *r.URL
	callback.URL = &u
	callback.Method = http.MethodGet
	callback.URL.RawQuery = url.Values{"state": {q.Get("state")}, "code": {code}}.Encode()
	s.callback(w, callback)
}

var shortLoginHTML = template.Must(template.New("login").Parse(`<!doctype html>
<html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>登录 · Seller Ops</title>
<style nonce="{{.Nonce}}">*{box-sizing:border-box}body{margin:0;background:#fafafa;color:#171717;font:16px/1.6 system-ui,-apple-system,"Segoe UI",sans-serif}main{width:min(420px,calc(100% - 32px));margin:12vh auto 40px;padding:32px;background:#fff;border:1px solid #e5e5e5;border-radius:16px;box-shadow:0 4px 20px #00000004}.brand{display:flex;align-items:center;gap:12px;font-size:24px;font-weight:700}.mark{display:grid;place-items:center;background:#171717;color:#fff;width:44px;height:44px;border-radius:12px;font-size:18px}h1{font-size:24px;margin:28px 0 4px}.hint,footer{color:#737373;font-size:14px}form{margin-top:24px}label{display:block;margin-bottom:6px}input{width:100%;min-height:48px;padding:10px 12px;border:1px solid #d4d4d4;border-radius:8px;font:inherit;background:#fff;color:inherit}.field{margin-bottom:20px}.password{position:relative}.password input{padding-right:66px}.toggle{position:absolute;right:4px;top:2px;background:transparent;color:#525252;border:0;padding:10px;font:inherit;min-height:44px;cursor:pointer}.submit{width:100%;min-height:48px;border:0;border-radius:8px;background:#171717;color:white;font:inherit;font-weight:600;cursor:pointer}button:disabled{opacity:.6}:focus-visible{outline:3px solid #2563eb;outline-offset:3px}.error{border:1px solid #fecaca;border-radius:8px;background:#fff7f7;color:#b91c1c;padding:12px;font-size:14px;margin-top:16px}footer{margin-top:20px;text-align:center}a{color:#525252;text-underline-offset:4px}@media(max-width:480px){main{margin-top:6vh;padding:24px}}</style></head>
<body><main><div class="brand"><span class="mark" aria-hidden="true">SO</span>Seller Ops</div><h1>登录电商运营工作台</h1><p class="hint">使用公司账号登录</p>
{{if .Error}}<div class="error" role="alert">{{.Error}}</div>{{end}}
<form method="post" action="/api/auth/login-page"><input type="hidden" name="csrf" value="{{.CSRF}}">
<div class="field"><label for="username">账号</label><input id="username" name="username" type="text" autocomplete="username" autocorrect="off" autocapitalize="none" spellcheck="false" enterkeyhint="next" maxlength="254" value="{{.Username}}" required></div>
<div class="field"><label for="password">密码</label><div class="password"><input id="password" name="password" type="password" autocomplete="current-password" enterkeyhint="done" maxlength="1024" required><button id="toggle" class="toggle" type="button" aria-controls="password" aria-pressed="false">显示</button></div></div>
<button class="submit" type="submit">登录工作台</button></form><footer><a href="{{.FullURL}}">其他登录方式 / 找回密码</a>{{if .Registration}}<p>还没有账号？<a href="/register">注册账号</a></p>{{else}}<p>新员工请使用公司管理员提供的邀请链接注册</p>{{end}}</footer></main>
<script nonce="{{.Nonce}}">const toggle=document.getElementById('toggle');toggle.addEventListener('click',()=>{const input=document.getElementById('password');const show=input.type==='password';input.type=show?'text':'password';toggle.setAttribute('aria-pressed',String(show));toggle.textContent=show?'隐藏':'显示'});document.querySelector('form').addEventListener('submit',event=>{const button=event.currentTarget.querySelector('[type=submit]');button.disabled=true;button.textContent='正在登录…'});</script></body></html>`))
