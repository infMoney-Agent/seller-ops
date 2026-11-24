package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"seller-ops/server/internal/auth"
	"seller-ops/server/internal/httpresponse"
)

func (s *server) registerBusiness(mux *http.ServeMux) {
	for _, pattern := range []string{"GET /api/products", "POST /api/products", "GET /api/products/{id}", "PUT /api/products/{id}", "PATCH /api/products/{id}", "DELETE /api/products/{id}"} {
		mux.HandleFunc(pattern, s.products)
	}
	mux.HandleFunc("GET /api/users", s.members)
	mux.HandleFunc("PATCH /api/users/{id}", s.changeMember)
	mux.HandleFunc("DELETE /api/users/{id}", s.changeMember)
	mux.HandleFunc("GET /api/invitations", s.invitations)
	mux.HandleFunc("POST /api/invitations", s.invitations)
	mux.HandleFunc("DELETE /api/invitations/{id}", s.invitations)
	mux.HandleFunc("POST /api/invitations/accept", s.accept)
	mux.HandleFunc("POST /api/invitations/inspect", s.inspectInvitation)
}

func (s *server) workspace(w http.ResponseWriter, r *http.Request, owner bool) (auth.Principal, bool) {
	a, ok := s.auth.Authenticate(w, r)
	if !ok {
		return a, false
	}
	if a.TenantID == 0 || (owner && a.Role != "owner") {
		httpresponse.Fail(w, 403, "workspace access denied")
		return a, false
	}
	return a, true
}

type productInput struct {
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Price       float64 `json:"price"`
	Description string  `json:"description"`
}

type product struct {
	productInput
	ID      int64     `json:"id"`
	Created time.Time `json:"created_at"`
	Updated time.Time `json:"updated_at"`
}

const productFields = "id,name,category,price,description,created_at,updated_at"

func scanProduct(row pgx.Row) (product, error) {
	var p product
	err := row.Scan(&p.ID, &p.Name, &p.Category, &p.Price, &p.Description, &p.Created, &p.Updated)
	return p, err
}

func pageParam(raw string, fallback, max int) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return fallback
	}
	if n > max {
		return max
	}
	return n
}

func (s *server) products(w http.ResponseWriter, r *http.Request) {
	a, ok := s.workspace(w, r, r.Method != http.MethodGet)
	if !ok {
		return
	}
	rawID := r.PathValue("id")
	if r.Method == http.MethodGet && rawID == "" {
		s.listProducts(w, r, a)
		return
	}
	id, err := strconv.ParseInt(rawID, 10, 64)
	if rawID != "" && (err != nil || id < 1) {
		httpresponse.Fail(w, 404, "product not found")
		return
	}
	ctx := r.Context()
	if r.Method == http.MethodDelete {
		tag, err := s.db.Exec(ctx, "DELETE FROM products WHERE tenant_id=$1 AND id=$2", a.TenantID, id)
		if err != nil {
			httpresponse.Internal(w, err)
			return
		}
		if tag.RowsAffected() == 0 {
			httpresponse.Fail(w, 404, "product not found")
			return
		}
		httpresponse.JSON(w, 200, map[string]any{"success": true, "message": "Product deleted"})
		return
	}
	var row pgx.Row
	status := 200
	if r.Method == http.MethodGet {
		row = s.db.QueryRow(ctx, "SELECT "+productFields+" FROM products WHERE tenant_id=$1 AND id=$2", a.TenantID, id)
	} else {
		var p productInput
		if !decode(w, r, &p) {
			return
		}
		if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Category) == "" || p.Price < 0 || p.Price >= 1e12 {
			httpresponse.Fail(w, 400, "invalid product")
			return
		}
		if r.Method == http.MethodPost {
			status = 201
			row = s.db.QueryRow(ctx, "INSERT INTO products(tenant_id,name,category,price,description) VALUES($1,$2,$3,$4,$5) RETURNING "+productFields,
				a.TenantID, p.Name, p.Category, p.Price, p.Description)
		} else {
			row = s.db.QueryRow(ctx, "UPDATE products SET name=$3,category=$4,price=$5,description=$6,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING "+productFields,
				a.TenantID, id, p.Name, p.Category, p.Price, p.Description)
		}
	}
	p, err := scanProduct(row)
	if errors.Is(err, pgx.ErrNoRows) {
		httpresponse.Fail(w, 404, "product not found")
		return
	}
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	httpresponse.JSON(w, status, map[string]any{"success": true, "time": time.Now().UTC(), "message": "OK", "product": p})
}

func (s *server) listProducts(w http.ResponseWriter, r *http.Request, a auth.Principal) {
	q := r.URL.Query()
	limit := pageParam(q.Get("limit"), 10, 100)
	offset := (pageParam(q.Get("page"), 1, 1000000) - 1) * limit
	categories := strings.FieldsFunc(q.Get("categories"), func(r rune) bool { return r == ',' || r == '.' })
	if categories == nil {
		categories = []string{}
	}
	search := q.Get("search")
	ctx := r.Context()
	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	const filter = ` FROM products WHERE tenant_id=$1
 AND (cardinality($2::text[])=0 OR category=ANY($2))
 AND ($3='' OR strpos(lower(name),lower($3))>0 OR strpos(lower(description),lower($3))>0)`
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*)"+filter, a.TenantID, categories, search).Scan(&count); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	order := "id DESC"
	var sorts []struct {
		ID   string `json:"id"`
		Desc bool   `json:"desc"`
	}
	if json.Unmarshal([]byte(q.Get("sort")), &sorts) == nil {
		columns := map[string]string{"id": "id", "name": "name", "category": "category", "price": "price", "created_at": "created_at", "updated_at": "updated_at"}
		fields := []string{}
		for _, sort := range sorts {
			column := columns[sort.ID]
			if column == "" {
				continue
			}
			if sort.Desc {
				column += " DESC"
			}
			fields = append(fields, column)
		}
		if len(fields) > 0 {
			order = strings.Join(fields, ",") + ",id DESC"
		}
	}
	rows, err := tx.Query(ctx, "SELECT "+productFields+filter+" ORDER BY "+order+" LIMIT $4 OFFSET $5", a.TenantID, categories, search, limit, offset)
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	defer rows.Close()
	list := []product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			httpresponse.Internal(w, err)
			return
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	httpresponse.JSON(w, 200, map[string]any{"success": true, "time": time.Now().UTC(), "message": "OK", "total_products": count, "offset": offset, "limit": limit, "products": list})
}

func (s *server) members(w http.ResponseWriter, r *http.Request) {
	a, ok := s.workspace(w, r, false)
	if !ok {
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT u.id,u.name,u.email,m.role FROM memberships m
 JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 ORDER BY u.id`, a.TenantID)
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, email, role string
		if err := rows.Scan(&id, &name, &email, &role); err != nil {
			httpresponse.Internal(w, err)
			return
		}
		list = append(list, map[string]any{"id": id, "name": name, "email": email, "role": role})
	}
	if err := rows.Err(); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	httpresponse.JSON(w, 200, list)
}

func (s *server) changeMember(w http.ResponseWriter, r *http.Request) {
	a, ok := s.workspace(w, r, true)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpresponse.Fail(w, 404, "member not found")
		return
	}
	var input struct {
		Role string `json:"role"`
	}
	if r.Method != http.MethodDelete {
		if !decode(w, r, &input) {
			return
		}
		if input.Role != "owner" && input.Role != "member" {
			httpresponse.Fail(w, 400, "invalid role")
			return
		}
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", a.TenantID); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	var isOwner bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=$1 AND user_id=$2 AND role='owner')", a.TenantID, a.UserID).Scan(&isOwner)
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	if !isOwner {
		httpresponse.Fail(w, 403, "workspace access denied")
		return
	}
	var role string
	err = tx.QueryRow(ctx, "SELECT role FROM memberships WHERE tenant_id=$1 AND user_id=$2", a.TenantID, id).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		httpresponse.Fail(w, 404, "member not found")
		return
	}
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	if role == "owner" && input.Role != "owner" {
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM memberships WHERE tenant_id=$1 AND role='owner'", a.TenantID).Scan(&count); err != nil {
			httpresponse.Internal(w, err)
			return
		}
		if count <= 1 {
			httpresponse.Fail(w, 409, "workspace requires an owner")
			return
		}
	}
	if r.Method == http.MethodDelete {
		_, err = tx.Exec(ctx, "DELETE FROM memberships WHERE tenant_id=$1 AND user_id=$2", a.TenantID, id)
	} else {
		_, err = tx.Exec(ctx, "UPDATE memberships SET role=$3 WHERE tenant_id=$1 AND user_id=$2", a.TenantID, id, input.Role)
	}
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) invitations(w http.ResponseWriter, r *http.Request) {
	a, ok := s.workspace(w, r, true)
	if !ok {
		return
	}
	ctx := r.Context()
	switch r.Method {
	case http.MethodGet:
		rows, err := s.db.Query(ctx, `SELECT encode(token_hash,'hex'),email,expires_at FROM invitations
  WHERE tenant_id=$1 AND expires_at>now() AND accepted_at IS NULL AND revoked_at IS NULL ORDER BY expires_at`, a.TenantID)
		if err != nil {
			httpresponse.Internal(w, err)
			return
		}
		defer rows.Close()
		list := []map[string]any{}
		for rows.Next() {
			var id, email string
			var expires time.Time
			if err := rows.Scan(&id, &email, &expires); err != nil {
				httpresponse.Internal(w, err)
				return
			}
			list = append(list, map[string]any{"id": id, "email": email, "expiresAt": expires})
		}
		if err := rows.Err(); err != nil {
			httpresponse.Internal(w, err)
			return
		}
		httpresponse.JSON(w, 200, list)
	case http.MethodPost:
		var input struct {
			Email string `json:"email"`
		}
		if !decode(w, r, &input) {
			return
		}
		email := strings.ToLower(strings.TrimSpace(input.Email))
		address, err := mail.ParseAddress(email)
		if err != nil || address.Address != email {
			httpresponse.Fail(w, 400, "invalid email")
			return
		}
		token := auth.NewToken()
		_, err = s.db.Exec(ctx, "INSERT INTO invitations(token_hash,tenant_id,email,expires_at) VALUES($1,$2,$3,now()+interval '7 days')", auth.HashToken(token), a.TenantID, email)
		if err != nil {
			httpresponse.Internal(w, err)
			return
		}
		httpresponse.JSON(w, 201, map[string]string{"url": fmt.Sprintf("%s/invite#%s", s.origins[0], token)})
	case http.MethodDelete:
		tag, err := s.db.Exec(ctx, `UPDATE invitations SET revoked_at=now() WHERE tenant_id=$1 AND encode(token_hash,'hex')=$2
  AND accepted_at IS NULL AND revoked_at IS NULL`, a.TenantID, r.PathValue("id"))
		if err != nil {
			httpresponse.Internal(w, err)
			return
		}
		if tag.RowsAffected() == 0 {
			httpresponse.Fail(w, 404, "invitation not found")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *server) inspectInvitation(w http.ResponseWriter, r *http.Request) {
	var input struct{ Token string }
	if !decode(w, r, &input) {
		return
	}
	if len(input.Token) != 43 {
		httpresponse.Fail(w, 404, "invitation unavailable")
		return
	}
	var name, email, organization string
	var expires time.Time
	err := s.db.QueryRow(r.Context(), "SELECT t.name,i.email,i.expires_at,coalesce(t.casdoor_org,'') FROM invitations i JOIN tenants t ON t.id=i.tenant_id WHERE i.token_hash=$1 AND i.expires_at>now() AND i.accepted_at IS NULL AND i.revoked_at IS NULL", auth.HashToken(input.Token)).Scan(&name, &email, &expires, &organization)
	if errors.Is(err, pgx.ErrNoRows) {
		httpresponse.Fail(w, 404, "invitation unavailable")
		return
	}
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	parts := strings.SplitN(email, "@", 2)
	hint := "***"
	if len(parts) == 2 {
		hint = string([]rune(parts[0])[:1]) + "***@" + parts[1]
	}
	httpresponse.JSON(w, 200, map[string]any{"companyName": name, "emailHint": hint, "expiresAt": expires, "organization": organization})
}

func (s *server) accept(w http.ResponseWriter, r *http.Request) {
	a, ok := s.auth.Authenticate(w, r)
	if !ok {
		return
	}
	var input struct{ Token string }
	if !decode(w, r, &input) {
		return
	}
	if len(input.Token) != 43 {
		httpresponse.Fail(w, 404, "invitation unavailable")
		return
	}
	ctx := r.Context()
	tx, err := s.db.Begin(ctx)
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	defer tx.Rollback(ctx)
	var email string
	var verified bool
	if err = tx.QueryRow(ctx, "SELECT email,email_verified FROM users WHERE id=$1 FOR UPDATE", a.UserID).Scan(&email, &verified); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT true FROM sessions WHERE token_hash=$1 AND user_id=$2 AND expires_at>now() FOR UPDATE", s.auth.SessionHash(r), a.UserID).Scan(&active); errors.Is(err, pgx.ErrNoRows) {
		httpresponse.Fail(w, 401, "session expired")
		return
	}
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	var company int64
	var invitedEmail string
	var accepted *time.Time
	err = tx.QueryRow(ctx, "SELECT tenant_id,email,accepted_at FROM invitations WHERE token_hash=$1 AND expires_at>now() AND revoked_at IS NULL FOR UPDATE", auth.HashToken(input.Token)).Scan(&company, &invitedEmail, &accepted)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !strings.EqualFold(email, invitedEmail)) {
		httpresponse.Fail(w, 404, "invitation unavailable")
		return
	}
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	if !verified && !s.allowUnverifiedInvites {
		httpresponse.Fail(w, 403, "email verification required")
		return
	}
	var organizationMatches bool
	if err = tx.QueryRow(ctx, `SELECT t.casdoor_org IS NULL OR t.casdoor_org=u.casdoor_org FROM tenants t CROSS JOIN users u WHERE t.id=$1 AND u.id=$2`, company, a.UserID).Scan(&organizationMatches); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	if !organizationMatches {
		httpresponse.Fail(w, 403, "please sign in to the invited company organization")
		return
	}
	var bound int64
	err = tx.QueryRow(ctx, "SELECT tenant_id FROM memberships WHERE user_id=$1", a.UserID).Scan(&bound)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		httpresponse.Internal(w, err)
		return
	}
	if bound != 0 && bound != company {
		httpresponse.Fail(w, 409, "account belongs to another company")
		return
	}
	if accepted != nil {
		httpresponse.Fail(w, 404, "invitation unavailable")
		return
	}
	if bound == 0 {
		if _, err = tx.Exec(ctx, "INSERT INTO memberships(tenant_id,user_id,role) VALUES($1,$2,'member')", company, a.UserID); err != nil {
			httpresponse.Internal(w, err)
			return
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE invitations SET accepted_at=now() WHERE token_hash=$1", auth.HashToken(input.Token)); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	if _, err = tx.Exec(ctx, "UPDATE sessions SET tenant_id=$1 WHERE user_id=$2 AND expires_at>now()", company, a.UserID); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	if err = tx.Commit(ctx); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	httpresponse.JSON(w, 200, map[string]int64{"workspaceId": company})
}
