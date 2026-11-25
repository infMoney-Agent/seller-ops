package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
)

type registrationConfig struct {
	Base                                *url.URL
	Application, Client, Secret, Issuer string
	mu                                  sync.Mutex
	attempts                            map[string]signupRate
}
type signupRate struct {
	At    time.Time
	Count int
}
type registrationFlow struct {
	CSRF, Mode, State, Error string
	Target                   *int64
	Invitation               []byte
	Payload                  []byte
	Result                   []byte
}
type signupPayload struct {
	Mode, Username, Email, Company, Target, Organization, Subject string
	NewOrganization                                               bool
}

func (s *Service) EnableRegistration(base, app, client, secret, issuer string) error {
	u, e := url.Parse(base)
	if e != nil || u.Host == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || app == "" || client == "" || secret == "" || issuer == "" {
		return errors.New("invalid registration configuration")
	}
	s.registration = &registrationConfig{Base: u, Application: app, Client: client, Secret: secret, Issuer: issuer, attempts: map[string]signupRate{}}
	return nil
}
func (s *Service) registerRegistration(m *http.ServeMux) {
	m.HandleFunc("GET /api/auth/register-page", s.registrationPage)
	m.HandleFunc("POST /api/auth/register-page", s.registrationPage)
	m.HandleFunc("POST /api/auth/registration/prepare", s.prepareRegistrationInvitation)
	m.HandleFunc("GET /api/auth/registration/status", s.registrationStatus)
	m.HandleFunc("GET /api/auth/company", s.companyInfo)
	m.HandleFunc("GET /api/auth/join-requests", s.joinRequests)
	m.HandleFunc("POST /api/auth/join-requests", s.applyToCompany)
	m.HandleFunc("POST /api/auth/join-requests/{id}/{action}", s.decideJoinRequest)
}
func regJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func regError(w http.ResponseWriter, status int, message string) {
	regJSON(w, status, map[string]string{"message": message})
}
func (s *Service) identityAPI(ctx context.Context, path string, body any) (json.RawMessage, error) {
	method := http.MethodGet
	var data []byte
	if body != nil {
		method = http.MethodPost
		data, _ = json.Marshal(body)
	}
	relative, e := url.Parse(path)
	if e != nil {
		return nil, e
	}
	endpoint := s.registration.Base.ResolveReference(relative)
	req, e := http.NewRequestWithContext(ctx, method, endpoint.String(), bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	req.SetBasicAuth(s.registration.Client, s.registration.Secret)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, e := client.Do(req)
	if e != nil {
		return nil, errors.New("账号服务暂不可用")
	}
	defer response.Body.Close()
	var result struct {
		Status, Msg string
		Data        json.RawMessage
	}
	if response.StatusCode != 200 || json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&result) != nil {
		return nil, errors.New("账号服务返回异常")
	}
	if result.Status != "ok" {
		if strings.Contains(strings.ToLower(result.Msg), "already exists") || strings.Contains(result.Msg, "账号已被使用") {
			return nil, errors.New("账号或邮箱已被使用，请更换后重试")
		}
		return nil, errors.New("账号服务未能完成操作")
	}
	return result.Data, nil
}
func (s *Service) identityAction(ctx context.Context, path string, body any) error {
	data, e := s.identityAPI(ctx, path, body)
	if e != nil {
		return e
	}
	var status string
	_ = json.Unmarshal(data, &status)
	if status != "Affected" {
		return errors.New("账号服务未完成保存")
	}
	return nil
}
func (s *Service) readRegistration(r *http.Request) (registrationFlow, error) {
	var f registrationFlow
	key := s.readCookie(r, "__Host-seller_register")
	if len(key) != 43 {
		return f, pgx.ErrNoRows
	}
	e := s.db.QueryRow(r.Context(), "SELECT csrf,mode,state,error,target_tenant,invitation_hash,payload,result FROM registration_flows WHERE token_hash=$1 AND expires_at>now()", HashToken(key)).Scan(&f.CSRF, &f.Mode, &f.State, &f.Error, &f.Target, &f.Invitation, &f.Payload, &f.Result)
	return f, e
}
func (s *Service) newRegistration(w http.ResponseWriter, r *http.Request) (registrationFlow, error) {
	key, csrf := NewToken(), NewToken()
	_, e := s.db.Exec(r.Context(), "INSERT INTO registration_flows(token_hash,csrf,expires_at) VALUES($1,$2,now()+interval '30 minutes')", HashToken(key), csrf)
	if e == nil {
		s.cookie(w, "__Host-seller_register", key, 1800)
	}
	return registrationFlow{CSRF: csrf, Mode: "create", State: "editing"}, e
}
func registrationHeaders(w http.ResponseWriter, nonce string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'nonce-"+nonce+"'; script-src 'nonce-"+nonce+"'; connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
}
func (s *Service) registrationPage(w http.ResponseWriter, r *http.Request) {
	f, e := s.readRegistration(r)
	if e != nil {
		if r.Method != http.MethodGet {
			regError(w, 409, "注册页面已过期，请重新打开 /register")
			return
		}
		f, e = s.newRegistration(w, r)
		if e != nil {
			regError(w, 503, "暂时无法创建注册页面")
			return
		}
	}
	if r.Method == http.MethodPost {
		s.submitRegistration(w, r, f)
		return
	}
	if f.State == "done" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	mode := r.URL.Query().Get("mode")
	if mode != "join" {
		mode = "create"
	}
	if f.Mode == "invite" {
		mode = "invite"
	}
	var input signupPayload
	_ = json.Unmarshal(f.Payload, &input)
	if mode == "invite" && f.Target != nil {
		_ = s.db.QueryRow(r.Context(), "SELECT t.name,i.email FROM invitations i JOIN tenants t ON t.id=i.tenant_id WHERE i.token_hash=$1 AND i.tenant_id=$2 AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at>now()", f.Invitation, *f.Target).Scan(&input.Company, &input.Email)
	}
	nonce := NewToken()
	registrationHeaders(w, nonce)
	_ = registrationHTML.Execute(w, struct {
		Mode, CSRF, Nonce, Error, Username, Email, Company, Target string
		Bootstrap                                                  bool
	}{mode, f.CSRF, nonce, f.Error, input.Username, input.Email, input.Company, input.Target, r.URL.Query().Get("invited") == "1" && mode != "invite"})
}
func (s *Service) prepareRegistrationInvitation(w http.ResponseWriter, r *http.Request) {
	f, e := s.readRegistration(r)
	if e != nil || f.State != "editing" {
		regError(w, 409, "注册页面已过期")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Registration-CSRF")), []byte(f.CSRF)) != 1 {
		regError(w, 403, "注册验证失败，请刷新重试")
		return
	}
	var in struct{ Token string }
	if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&in) != nil || len(in.Token) != 43 {
		regError(w, 404, "邀请无效或已过期")
		return
	}
	var tenant int64
	var org string
	e = s.db.QueryRow(r.Context(), "SELECT i.tenant_id,t.casdoor_org FROM invitations i JOIN tenants t ON t.id=i.tenant_id WHERE i.token_hash=$1 AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at>now() AND t.casdoor_org IS NOT NULL", HashToken(in.Token)).Scan(&tenant, &org)
	if e != nil {
		regError(w, 404, "邀请无效或已过期")
		return
	}
	tag, e := s.db.Exec(r.Context(), "UPDATE registration_flows SET mode='invite',target_tenant=$2,invitation_hash=$3,payload='{}',error='' WHERE token_hash=$1 AND state='editing'", HashToken(s.readCookie(r, "__Host-seller_register")), tenant, HashToken(in.Token))
	if e != nil || tag.RowsAffected() != 1 {
		regError(w, 409, "注册流程已变化，请重新打开邀请链接")
		return
	}
	regJSON(w, 200, map[string]bool{"ready": true})
}

var registrationUsername = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:[-_][A-Za-z0-9]+)*$`)

func validateSignup(in signupPayload, password, confirm string) error {
	if len(in.Username) < 2 || len(in.Username) > 64 || !registrationUsername.MatchString(in.Username) {
		return errors.New("账号需为2–64位字母、数字、下划线或连字符，并以字母开头")
	}
	address, e := mail.ParseAddress(in.Email)
	if e != nil || address.Address != in.Email || len(in.Email) > 254 {
		return errors.New("请填写有效邮箱")
	}
	if len(password) < 8 || len(password) > 512 {
		return errors.New("密码需为8–512字节")
	}
	if password != confirm {
		return errors.New("两次输入的密码不一致")
	}
	if in.Mode == "create" && (len([]rune(in.Company)) < 1 || len([]rune(in.Company)) > 80 || strings.ContainsAny(in.Company, "\r\n\x00")) {
		return errors.New("公司名称需为1–80个字")
	}
	if in.Mode == "join" && (in.Target == "" || len(in.Target) > 128) {
		return errors.New("请填写公司名称或编号")
	}
	return nil
}
func (s *Service) resolveCompany(ctx context.Context, target string) (int64, string, error) {
	rows, e := s.db.Query(ctx, "SELECT id,casdoor_org FROM tenants WHERE casdoor_org IS NOT NULL AND (upper('C'||id::text)=upper($1) OR lower(name)=lower($1)) LIMIT 2", target)
	if e != nil {
		return 0, "", e
	}
	defer rows.Close()
	var id int64
	var org string
	count := 0
	for rows.Next() {
		if e = rows.Scan(&id, &org); e != nil {
			return 0, "", e
		}
		count++
	}
	if rows.Err() != nil {
		return 0, "", rows.Err()
	}
	if count == 0 {
		return 0, "", errors.New("未找到公司，请向管理员核对公司编号")
	}
	if count > 1 {
		return 0, "", errors.New("公司名称重复，请使用公司编号")
	}
	return id, org, nil
}
func randomIdentifier() string {
	b := make([]byte, 16)
	_, e := rand.Read(b)
	if e != nil {
		panic(e)
	}
	return hex.EncodeToString(b)
}

func (s *Service) registrationRate(r *http.Request) bool {
	key := r.Header.Get("X-Forwarded-For")
	if key == "" {
		key = r.RemoteAddr
	}
	key = strings.Split(key, ",")[0]
	c := s.registration
	c.mu.Lock()
	defer c.mu.Unlock()
	now := time.Now()
	if len(c.attempts) > 2048 {
		for k, v := range c.attempts {
			if now.Sub(v.At) > time.Hour {
				delete(c.attempts, k)
			}
		}
	}
	v := c.attempts[key]
	if now.Sub(v.At) > time.Hour {
		v = signupRate{At: now}
	}
	v.Count++
	c.attempts[key] = v
	return v.Count <= 20
}
func (s *Service) cleanupSignup(in signupPayload) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	raw, e := s.identityAPI(ctx, "/api/get-user?id="+url.QueryEscape(in.Organization+"/"+in.Username), nil)
	if e != nil {
		return e
	}
	var user struct {
		ID string `json:"id"`
	}
	if len(raw) > 0 && string(raw) != "null" {
		if e = json.Unmarshal(raw, &user); e != nil {
			return e
		}
		if user.ID == in.Subject {
			if e = s.identityAction(ctx, "/api/delete-user", map[string]string{"owner": in.Organization, "name": in.Username, "id": in.Subject}); e != nil {
				return e
			}
		}
	}
	if !in.NewOrganization {
		return nil
	}
	users, e := s.identityAPI(ctx, "/api/get-users?owner="+url.QueryEscape(in.Organization), nil)
	if e != nil {
		return e
	}
	var members []json.RawMessage
	if json.Unmarshal(users, &members) != nil || len(members) > 0 {
		return errors.New("new organization has users; cleanup needs review")
	}
	raw, e = s.identityAPI(ctx, "/api/get-organization?id="+url.QueryEscape("admin/"+in.Organization), nil)
	if e != nil {
		return e
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var organization struct{ Name, DefaultApplication string }
	if json.Unmarshal(raw, &organization) != nil || organization.Name != in.Organization || organization.DefaultApplication != s.registration.Application {
		return errors.New("organization ownership check failed")
	}
	return s.identityAction(ctx, "/api/delete-organization", map[string]string{"owner": "admin", "name": in.Organization})
}
func (s *Service) submitRegistration(w http.ResponseWriter, r *http.Request, f registrationFlow) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if r.ParseForm() != nil {
		regError(w, 400, "注册请求无效")
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.PostForm.Get("csrf")), []byte(f.CSRF)) != 1 {
		regError(w, 403, "注册验证失败，请刷新重试")
		return
	}
	if f.State == "done" {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	if f.State != "editing" {
		regError(w, 409, "注册正在处理，请勿重复提交；长时间未完成请联系管理员")
		return
	}
	in := signupPayload{Mode: r.PostForm.Get("mode"), Username: strings.ToLower(strings.TrimSpace(r.PostForm.Get("username"))), Email: strings.ToLower(strings.TrimSpace(r.PostForm.Get("email"))), Company: strings.TrimSpace(r.PostForm.Get("company")), Target: strings.TrimSpace(r.PostForm.Get("target"))}
	if f.Mode == "invite" {
		in.Mode = "invite"
	}
	if in.Mode != "create" && in.Mode != "join" && in.Mode != "invite" {
		regError(w, 400, "请选择有效的注册方式")
		return
	}
	key := HashToken(s.readCookie(r, "__Host-seller_register"))
	redirectURL := "/register?mode=" + in.Mode
	failForm := func(message string) {
		raw, _ := json.Marshal(in)
		_, _ = s.db.Exec(r.Context(), "UPDATE registration_flows SET error=$2,payload=$3 WHERE token_hash=$1 AND state='editing'", key, message, raw)
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
	if e := validateSignup(in, r.PostForm.Get("password"), r.PostForm.Get("confirm")); e != nil {
		failForm(e.Error())
		return
	}
	var target int64
	var e error
	if in.Mode == "join" {
		target, in.Organization, e = s.resolveCompany(r.Context(), in.Target)
		if e != nil {
			failForm(e.Error())
			return
		}
	}
	if in.Mode == "invite" {
		if f.Target == nil || len(f.Invitation) != 32 {
			failForm("请重新使用有效的公司邀请链接")
			return
		}
		target = *f.Target
		var email string
		e = s.db.QueryRow(r.Context(), "SELECT t.casdoor_org,i.email FROM invitations i JOIN tenants t ON t.id=i.tenant_id WHERE i.token_hash=$1 AND i.tenant_id=$2 AND i.accepted_at IS NULL AND i.revoked_at IS NULL AND i.expires_at>now()", f.Invitation, target).Scan(&in.Organization, &email)
		if e != nil || !strings.EqualFold(in.Email, email) {
			failForm("邀请已失效，或邮箱与受邀邮箱不一致")
			return
		}
	}
	in.Subject = randomIdentifier()
	if in.Mode == "create" {
		in.Organization = "seller_" + randomIdentifier()
		in.NewOrganization = true
	}
	raw, _ := json.Marshal(in)
	tag, e := s.db.Exec(r.Context(), "UPDATE registration_flows SET state='working',payload=$2,updated_at=now(),error='' WHERE token_hash=$1 AND state='editing' AND expires_at>now()", key, raw)
	if e != nil || tag.RowsAffected() != 1 {
		regError(w, 409, "注册正在处理，请勿重复提交")
		return
	}
	if !s.registrationRate(r) {
		_, _ = s.db.Exec(r.Context(), "UPDATE registration_flows SET state='editing',error='操作过于频繁，请稍后再试' WHERE token_hash=$1", key)
		http.Redirect(w, r, redirectURL, 303)
		return
	}
	rollback := func(message string) {
		cleanup := s.cleanupSignup(in)
		state := "editing"
		if cleanup != nil {
			state = "cleanup_required"
			message = "注册暂未完成，请联系管理员处理后重试"
		}
		_, _ = s.db.Exec(context.Background(), "UPDATE registration_flows SET state=$2,error=$3,updated_at=now() WHERE token_hash=$1", key, state, message)
		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
	if in.NewOrganization {
		e = s.identityAction(r.Context(), "/api/add-organization", map[string]any{"owner": "admin", "name": in.Organization, "displayName": in.Company, "defaultApplication": s.registration.Application, "passwordType": "bcrypt", "disableConsole": true, "languages": []string{"zh"}, "countryCodes": []string{"CN"}, "accountItems": []map[string]any{{"name": "Email", "visible": true, "viewRule": "Self", "modifyRule": "Self"}, {"name": "Password", "visible": true, "viewRule": "Self", "modifyRule": "Self"}}, "passwordOptions": []string{"At least 8 characters"}})
		if e != nil {
			rollback(e.Error())
			return
		}
	}
	e = s.identityAction(r.Context(), "/api/add-user", map[string]any{"owner": in.Organization, "name": in.Username, "id": in.Subject, "email": in.Email, "emailVerified": false, "displayName": in.Username, "password": r.PostForm.Get("password"), "passwordType": "plain", "type": "normal-user", "isAdmin": false, "signupApplication": s.registration.Application})
	if e != nil {
		rollback(e.Error())
		return
	}
	tx, e := s.db.Begin(r.Context())
	if e != nil {
		rollback("注册暂未完成，请重试")
		return
	}
	defer tx.Rollback(r.Context())
	var user int64
	var membership any
	if in.NewOrganization {
		e = tx.QueryRow(r.Context(), "INSERT INTO tenants(name,casdoor_org) VALUES($1,$2) RETURNING id", in.Company, in.Organization).Scan(&target)
	} else {
		var currentOrg string
		e = tx.QueryRow(r.Context(), "SELECT casdoor_org FROM tenants WHERE id=$1 FOR SHARE", target).Scan(&currentOrg)
		if e == nil && currentOrg != in.Organization {
			e = errors.New("company changed")
		}
	}
	if e == nil {
		e = tx.QueryRow(r.Context(), "INSERT INTO users(issuer,subject,email,email_verified,name,casdoor_org) VALUES($1,$2,$3,false,$4,$5) RETURNING id", s.registration.Issuer, in.Subject, in.Email, in.Username, in.Organization).Scan(&user)
	}
	if e == nil && in.Mode == "invite" {
		var invited string
		e = tx.QueryRow(r.Context(), "SELECT email FROM invitations WHERE token_hash=$1 AND tenant_id=$2 AND accepted_at IS NULL AND revoked_at IS NULL AND expires_at>now() FOR UPDATE", f.Invitation, target).Scan(&invited)
		if e == nil && !strings.EqualFold(invited, in.Email) {
			e = errors.New("invitation changed")
		}
		if e == nil {
			_, e = tx.Exec(r.Context(), "UPDATE invitations SET accepted_at=now() WHERE token_hash=$1", f.Invitation)
		}
	}
	if e == nil {
		if in.Mode == "join" {
			_, e = tx.Exec(r.Context(), "INSERT INTO company_join_requests(tenant_id,user_id) VALUES($1,$2)", target, user)
		} else {
			role := "member"
			if in.Mode == "create" {
				role = "owner"
			}
			_, e = tx.Exec(r.Context(), "INSERT INTO memberships(tenant_id,user_id,role) VALUES($1,$2,$3)", target, user, role)
			membership = target
		}
	}
	session := NewToken()
	if e == nil {
		_, e = tx.Exec(r.Context(), "INSERT INTO sessions(token_hash,user_id,tenant_id,expires_at) VALUES($1,$2,$3,now()+interval '12 hours')", HashToken(session), user, membership)
	}
	if e == nil {
		result, _ := json.Marshal(map[string]any{"user_id": user, "tenant_id": target, "mode": in.Mode})
		_, e = tx.Exec(r.Context(), "UPDATE registration_flows SET state='done',result=$2,error='',updated_at=now() WHERE token_hash=$1", key, result)
	}
	if e != nil {
		_ = tx.Rollback(r.Context())
		rollback("注册暂未完成，请核对公司或邀请后重试")
		return
	}
	if e = tx.Commit(r.Context()); e != nil {
		// Commit acknowledgements can be lost: never delete external accounts if the DB actually committed.
		current, readErr := s.readRegistration(r)
		if readErr != nil || current.State != "done" {
			if readErr != nil {
				_, _ = s.db.Exec(context.Background(), "UPDATE registration_flows SET state='cleanup_required',error='注册结果待核对，请联系管理员' WHERE token_hash=$1 AND state<>'done'", key)
				regError(w, 503, "注册结果待核对，请稍后再试")
				return
			}
			rollback("注册暂未完成，请重试")
			return
		}
	}
	s.cookie(w, SessionCookie, session, 43200)
	destination := "/dashboard"
	if in.Mode == "join" {
		destination = "/company-required"
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}

func (s *Service) companyInfo(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Authenticate(w, r)
	if !ok {
		return
	}
	input := map[string]any{}
	if r.Method == "POST" && "companyInfo" == "applyToCompany" {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if d.Decode(&input) != nil {
			regError(w, 400, "请求无效")
			return
		}
	}
	result, err := s.CompanyInfoUsecase(r.Context(), a, r.PathValue("id"), r.PathValue("action"), input)
	if err != nil {
		var e APIError
		if errors.As(err, &e) {
			regError(w, e.Status, e.Message)
		} else {
			regError(w, 503, "服务暂不可用")
		}
		return
	}
	regJSON(w, result.Status, result.Data)
}
func (s *Service) CompanyInfoUsecase(ctx context.Context, a Principal, resourceID, decisionAction string, input map[string]any) (*Result, error) {
	if a.TenantID == 0 {
		return nil, APIError{Status: 403, Message: "尚未加入公司", Plain: true}
	}
	var name string
	if s.db.QueryRow(ctx, "SELECT name FROM tenants WHERE id=$1", a.TenantID).Scan(&name) != nil {
		return nil, APIError{Status: 404, Message: "公司不存在", Plain: true}
	}
	return &Result{Status: 200, Data: map[string]any{"name": name, "code": fmt.Sprintf("C%d", a.TenantID)}}, nil
}
func (s *Service) registrationStatus(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Authenticate(w, r)
	if !ok {
		return
	}
	input := map[string]any{}
	if r.Method == "POST" && "registrationStatus" == "applyToCompany" {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if d.Decode(&input) != nil {
			regError(w, 400, "请求无效")
			return
		}
	}
	result, err := s.RegistrationStatusUsecase(r.Context(), a, r.PathValue("id"), r.PathValue("action"), input)
	if err != nil {
		var e APIError
		if errors.As(err, &e) {
			regError(w, e.Status, e.Message)
		} else {
			regError(w, 503, "服务暂不可用")
		}
		return
	}
	regJSON(w, result.Status, result.Data)
}
func (s *Service) RegistrationStatusUsecase(ctx context.Context, a Principal, resourceID, decisionAction string, input map[string]any) (*Result, error) {
	var raw []byte
	e := s.db.QueryRow(ctx, "SELECT jsonb_build_object('id',j.id,'companyName',t.name,'companyCode','C'||t.id::text,'state',j.state,'note',j.note,'createdAt',j.created_at) FROM company_join_requests j JOIN tenants t ON t.id=j.tenant_id WHERE j.user_id=$1", a.UserID).Scan(&raw)
	if errors.Is(e, pgx.ErrNoRows) {
		return &Result{Status: 200, Data: map[string]any{"request": nil}}, nil
	}
	if e != nil {
		return nil, APIError{Status: 503, Message: "暂时无法读取审批状态", Plain: true}
	}
	return &Result{Status: 200, Data: map[string]any{"request": json.RawMessage(raw)}}, nil
}
func (s *Service) joinRequests(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Authenticate(w, r)
	if !ok {
		return
	}
	input := map[string]any{}
	if r.Method == "POST" && "joinRequests" == "applyToCompany" {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if d.Decode(&input) != nil {
			regError(w, 400, "请求无效")
			return
		}
	}
	result, err := s.JoinRequestsUsecase(r.Context(), a, r.PathValue("id"), r.PathValue("action"), input)
	if err != nil {
		var e APIError
		if errors.As(err, &e) {
			regError(w, e.Status, e.Message)
		} else {
			regError(w, 503, "服务暂不可用")
		}
		return
	}
	regJSON(w, result.Status, result.Data)
}
func (s *Service) JoinRequestsUsecase(ctx context.Context, a Principal, resourceID, decisionAction string, input map[string]any) (*Result, error) {
	if a.Role != "owner" || a.TenantID == 0 {
		return nil, APIError{Status: 403, Message: "只有公司管理员可以审批", Plain: true}
	}
	rows, e := s.db.Query(ctx, "SELECT j.id,u.name,u.email,j.created_at FROM company_join_requests j JOIN users u ON u.id=j.user_id WHERE j.tenant_id=$1 AND j.state='pending' ORDER BY j.created_at,j.id LIMIT 100", a.TenantID)
	if e != nil {
		return nil, APIError{Status: 503, Message: "暂时无法读取申请", Plain: true}
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, email string
		var at time.Time
		if rows.Scan(&id, &name, &email, &at) != nil {
			return nil, APIError{Status: 503, Message: "读取申请失败", Plain: true}
		}
		items = append(items, map[string]any{"id": id, "name": name, "email": email, "createdAt": at})
	}
	if rows.Err() != nil {
		return nil, APIError{Status: 503, Message: "读取申请失败", Plain: true}
	}
	return &Result{Status: 200, Data: items}, nil
}
func (s *Service) applyToCompany(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Authenticate(w, r)
	if !ok {
		return
	}
	input := map[string]any{}
	if r.Method == "POST" && "applyToCompany" == "applyToCompany" {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if d.Decode(&input) != nil {
			regError(w, 400, "请求无效")
			return
		}
	}
	result, err := s.ApplyToCompanyUsecase(r.Context(), a, r.PathValue("id"), r.PathValue("action"), input)
	if err != nil {
		var e APIError
		if errors.As(err, &e) {
			regError(w, e.Status, e.Message)
		} else {
			regError(w, 503, "服务暂不可用")
		}
		return
	}
	regJSON(w, result.Status, result.Data)
}
func (s *Service) ApplyToCompanyUsecase(ctx context.Context, a Principal, resourceID, decisionAction string, input map[string]any) (*Result, error) {
	if a.TenantID != 0 {
		return nil, APIError{Status: 409, Message: "当前账号已加入公司", Plain: true}
	}
	var body struct{ Target string }
	if decodeRegistrationBody(input, &body) != nil {
		return nil, APIError{Status: 400, Message: "请求无效", Plain: true}
	}
	tenant, org, e := s.resolveCompany(ctx, strings.TrimSpace(body.Target))
	if e != nil {
		return nil, APIError{Status: 422, Message: e.Error(), Plain: true}
	}
	var userOrg string
	if s.db.QueryRow(ctx, "SELECT casdoor_org FROM users WHERE id=$1", a.UserID).Scan(&userOrg) != nil || userOrg != org {
		return nil, APIError{Status: 409, Message: "该账号属于其他组织，请联系管理员处理", Plain: true}
	}
	_, e = s.db.Exec(ctx, "INSERT INTO company_join_requests(tenant_id,user_id) VALUES($1,$2) ON CONFLICT(user_id) DO UPDATE SET state='pending',note='',decided_by=NULL,decided_at=NULL,created_at=now() WHERE company_join_requests.tenant_id=EXCLUDED.tenant_id AND company_join_requests.state<>'pending'", tenant, a.UserID)
	if e != nil {
		return nil, APIError{Status: 409, Message: "申请未提交，请刷新后重试", Plain: true}
	}
	return &Result{Status: 200, Data: map[string]bool{"submitted": true}}, nil
}
func (s *Service) decideJoinRequest(w http.ResponseWriter, r *http.Request) {
	a, ok := s.Authenticate(w, r)
	if !ok {
		return
	}
	input := map[string]any{}
	if r.Method == "POST" && "decideJoinRequest" == "applyToCompany" {
		d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		if d.Decode(&input) != nil {
			regError(w, 400, "请求无效")
			return
		}
	}
	result, err := s.DecideJoinRequestUsecase(r.Context(), a, r.PathValue("id"), r.PathValue("action"), input)
	if err != nil {
		var e APIError
		if errors.As(err, &e) {
			regError(w, e.Status, e.Message)
		} else {
			regError(w, 503, "服务暂不可用")
		}
		return
	}
	regJSON(w, result.Status, result.Data)
}
func (s *Service) DecideJoinRequestUsecase(ctx context.Context, a Principal, resourceID, decisionAction string, input map[string]any) (*Result, error) {
	if a.TenantID == 0 || a.Role != "owner" {
		return nil, APIError{Status: 403, Message: "只有公司管理员可以审批", Plain: true}
	}
	action := decisionAction
	if action != "approve" && action != "reject" {
		return nil, APIError{Status: 404, Message: "操作不存在", Plain: true}
	}

	tx, e := s.db.Begin(ctx)
	if e != nil {
		return nil, APIError{Status: 503, Message: "审批暂不可用", Plain: true}
	}
	defer tx.Rollback(ctx)
	var user int64
	var state string
	e = tx.QueryRow(ctx, "SELECT user_id,state FROM company_join_requests WHERE id=$1 AND tenant_id=$2 FOR UPDATE", resourceID, a.TenantID).Scan(&user, &state)
	if e != nil {
		return nil, APIError{Status: 404, Message: "申请不存在", Plain: true}
	}
	decision := "rejected"
	if action == "approve" {
		decision = "approved"
	}
	if state == decision {
		return &Result{Status: 200, Data: map[string]string{"state": state}}, nil
	}
	if state != "pending" {
		return nil, APIError{Status: 409, Message: "申请已被处理", Plain: true}
	}
	var currentOrg, targetOrg string
	e = tx.QueryRow(ctx, "SELECT casdoor_org FROM users WHERE id=$1 FOR UPDATE", user).Scan(&currentOrg)
	if e == nil {
		e = tx.QueryRow(ctx, "SELECT casdoor_org FROM tenants WHERE id=$1", a.TenantID).Scan(&targetOrg)
	}
	if e != nil || currentOrg != targetOrg {
		return nil, APIError{Status: 409, Message: "申请人组织已变化", Plain: true}
	}
	if action == "approve" {
		var bound int64
		e = tx.QueryRow(ctx, "SELECT tenant_id FROM memberships WHERE user_id=$1", user).Scan(&bound)
		if e != nil && !errors.Is(e, pgx.ErrNoRows) {
			return nil, APIError{Status: 503, Message: "审批暂不可用", Plain: true}
		}
		if bound != 0 && bound != a.TenantID {
			return nil, APIError{Status: 409, Message: "申请人已加入其他公司", Plain: true}
		}
		if bound == 0 {
			if _, e = tx.Exec(ctx, "INSERT INTO memberships(tenant_id,user_id,role) VALUES($1,$2,'member')", a.TenantID, user); e != nil {
				return nil, APIError{Status: 409, Message: "成员关系已变化，请刷新", Plain: true}
			}
		}
		_, e = tx.Exec(ctx, "UPDATE sessions SET tenant_id=$1 WHERE user_id=$2", a.TenantID, user)
		if e != nil {
			return nil, APIError{Status: 503, Message: "审批暂不可用", Plain: true}
		}
	}
	_, e = tx.Exec(ctx, "UPDATE company_join_requests SET state=$1,decided_by=$2,decided_at=now() WHERE id=$3 AND tenant_id=$4", decision, a.UserID, resourceID, a.TenantID)
	if e != nil || tx.Commit(ctx) != nil {
		return nil, APIError{Status: 503, Message: "审批未完成，请刷新确认", Plain: true}
	}
	return &Result{Status: 200, Data: map[string]string{"state": decision}}, nil
}

var registrationHTML = template.Must(template.New("registration").Parse(`<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>注册 · Seller Ops</title>
<style nonce="{{.Nonce}}">*{box-sizing:border-box}body{margin:0;background:#fafafa;color:#171717;font:16px/1.6 system-ui,-apple-system,"Segoe UI",sans-serif}main{width:min(460px,calc(100% - 32px));margin:6vh auto 32px;padding:28px;background:white;border:1px solid #e5e5e5;border-radius:16px}.brand{display:flex;align-items:center;gap:12px;font-size:22px;font-weight:700}.mark{display:grid;place-items:center;width:42px;height:42px;background:#171717;color:white;border-radius:10px;font-size:17px}h1{font-size:24px;margin:24px 0 12px}nav{display:flex;gap:8px;margin:16px 0 24px}nav a{flex:1;text-align:center;border:1px solid #ddd;border-radius:8px;padding:9px;text-decoration:none}nav a[aria-current=page]{background:#171717;color:white;border-color:#171717}label{display:block;margin-bottom:5px}input{width:100%;min-height:48px;padding:10px 12px;border:1px solid #b5b5b5;border-radius:8px;background:#fff;color:inherit;font:inherit}.field{margin:0 0 16px}.hint{color:#737373;font-size:14px;margin:6px 0 16px}button{width:100%;min-height:48px;background:#171717;color:white;border:0;border-radius:8px;font:inherit;font-weight:600;cursor:pointer}button:disabled{opacity:.6}:focus-visible{outline:3px solid #2563eb;outline-offset:3px}.error{padding:10px;border:1px solid #fecaca;background:#fff7f7;color:#b91c1c;border-radius:8px;margin-bottom:16px;font-size:14px}a{color:#525252;text-underline-offset:4px}footer{text-align:center;font-size:14px;margin-top:20px}.company{padding:12px;background:#f5f5f5;border-radius:8px;margin-bottom:18px}.passwords{display:grid;grid-template-columns:1fr 1fr;gap:12px}@media(max-width:420px){main{margin-top:20px;padding:22px}.passwords{grid-template-columns:1fr;gap:0}}</style></head><body><main><div class="brand"><span class="mark" aria-hidden="true">SO</span>Seller Ops</div>
{{if .Bootstrap}}<h1>加入公司</h1><p id="invite-status" role="status">正在验证邀请…</p><script nonce="{{.Nonce}}">(async()=>{try{const token=location.hash.slice(1);history.replaceState(history.state,'','/register?invited=1');if(!/^[A-Za-z0-9_-]{43}$/.test(token))throw Error('请重新打开公司邀请链接');const response=await fetch('/api/auth/registration/prepare',{method:'POST',headers:{'Content-Type':'application/json','X-Registration-CSRF':{{.CSRF}}},body:JSON.stringify({token})});const data=await response.json();if(!response.ok)throw Error(data.message||'邀请验证失败');location.replace('/register')}catch(error){document.getElementById('invite-status').textContent=error.message}})();</script>
{{else}}<h1>{{if eq .Mode "invite"}}受邀加入公司{{else}}注册账号{{end}}</h1>
{{if ne .Mode "invite"}}<nav aria-label="注册方式"><a href="/register?mode=create" {{if eq .Mode "create"}}aria-current="page"{{end}}>创建公司</a><a href="/register?mode=join" {{if eq .Mode "join"}}aria-current="page"{{end}}>申请加入公司</a></nav>{{else}}<div class="company">加入 <strong>{{.Company}}</strong></div>{{end}}
{{if .Error}}<div id="registration-error" class="error" role="alert">{{.Error}}</div>{{end}}
<form method="post" action="/api/auth/register-page"><input type="hidden" name="csrf" value="{{.CSRF}}"><input type="hidden" name="mode" value="{{.Mode}}">
{{if eq .Mode "create"}}<div class="field"><label for="company">公司名称</label><input id="company" name="company" autocomplete="organization" maxlength="80" value="{{.Company}}" required></div>{{end}}
{{if eq .Mode "join"}}<div class="field"><label for="target">公司名称或编号</label><input id="target" name="target" autocomplete="off" maxlength="128" value="{{.Target}}" required><p class="hint">公司重名时，请向管理员获取公司编号。</p></div>{{end}}
<div class="field"><label for="username">账号</label><input id="username" name="username" type="text" autocomplete="username" autocapitalize="none" autocorrect="off" spellcheck="false" minlength="2" maxlength="64" value="{{.Username}}" required><p class="hint">账号全局唯一，使用字母开头的字母、数字、下划线或连字符。</p></div>
<div class="field"><label for="email">邮箱</label><input id="email" name="email" type="email" autocomplete="email" maxlength="254" value="{{.Email}}" {{if eq .Mode "invite"}}readonly{{end}} required></div>
<div class="passwords"><div class="field"><label for="new-password">密码</label><input id="new-password" name="password" type="password" autocomplete="new-password" minlength="8" maxlength="128" required></div><div class="field"><label for="confirm-password">确认密码</label><input id="confirm-password" name="confirm" type="password" autocomplete="new-password" minlength="8" maxlength="128" required></div></div>
{{if eq .Mode "join"}}<p class="hint">提交后等待公司管理员审批，批准前无法访问公司业务数据。</p>{{end}}
<button type="submit">{{if eq .Mode "create"}}注册并创建公司{{else if eq .Mode "join"}}注册并提交申请{{else}}注册并加入公司{{end}}</button></form>
<script nonce="{{.Nonce}}">document.querySelector('form').addEventListener('submit',event=>{const button=event.currentTarget.querySelector('[type=submit]');button.dataset.label=button.textContent;button.disabled=true;button.textContent='正在提交…'});window.addEventListener('pageshow',()=>{const button=document.querySelector('[type=submit]');if(button&&button.dataset.label){button.disabled=false;button.textContent=button.dataset.label}});</script>{{end}}
<footer>已有账号？<a href="/login">返回登录</a></footer></main></body></html>`))

// Reconcile only resources recorded by our own interrupted registration flows.
// Successful registrations are never candidates for compensation.
func (s *Service) RegistrationMaintenance(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		rows, e := s.db.Query(ctx, "SELECT token_hash,payload FROM registration_flows WHERE state IN ('working','cleanup_required') AND updated_at<now()-interval '5 minutes' ORDER BY updated_at LIMIT 5")
		if e == nil {
			type entry struct{ Key, Payload []byte }
			entries := []entry{}
			for rows.Next() {
				var item entry
				if rows.Scan(&item.Key, &item.Payload) == nil {
					entries = append(entries, item)
				}
			}
			rows.Close()
			for _, item := range entries {
				tag, err := s.db.Exec(ctx, "UPDATE registration_flows SET state='cleanup_required',updated_at=now() WHERE token_hash=$1 AND state IN ('working','cleanup_required') AND updated_at<now()-interval '5 minutes'", item.Key)
				if err != nil || tag.RowsAffected() != 1 {
					continue
				}
				var input signupPayload
				if json.Unmarshal(item.Payload, &input) != nil || input.Subject == "" || input.Organization == "" {
					continue
				}
				if s.cleanupSignup(input) == nil {
					_, _ = s.db.Exec(ctx, "UPDATE registration_flows SET state='editing',error='上次注册未完成，已恢复，请重新提交' WHERE token_hash=$1 AND state='cleanup_required'", item.Key)
				}
			}
		}
		_, _ = s.db.Exec(ctx, "DELETE FROM registration_flows WHERE token_hash IN(SELECT token_hash FROM registration_flows WHERE state IN ('editing','done') AND expires_at<now()-interval '1 day' LIMIT 1000)")
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func decodeRegistrationBody(input map[string]any, dst any) error {
	raw, err := json.Marshal(input)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, dst)
}
