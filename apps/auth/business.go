package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"seller-ops/server/internal/auth"
)

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

func parseProductID(rawID string) (int64, error) {
	id, err := strconv.ParseInt(rawID, 10, 64)
	if err != nil || id < 1 {
		return 0, auth.APIError{Status: 404, Message: "product not found"}
	}
	return id, nil
}

func parseProductInput(body map[string]any) (productInput, error) {
	var p productInput
	if err := auth.DecodeBody(body, &p); err != nil {
		return p, err
	}
	if strings.TrimSpace(p.Name) == "" || strings.TrimSpace(p.Category) == "" || p.Price < 0 || p.Price >= 1e12 {
		return p, auth.APIError{Status: 400, Message: "invalid product"}
	}
	return p, nil
}

func productResult(status int, row pgx.Row) (*auth.Result, error) {
	p, err := scanProduct(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.APIError{Status: 404, Message: "product not found"}
	}
	if err != nil {
		return nil, err
	}
	return &auth.Result{Status: status, Data: map[string]any{"success": true, "time": time.Now().UTC(), "message": "OK", "product": p}}, nil
}

func (s *server) listProductsUsecase(ctx context.Context, a auth.Principal, params url.Values) (*auth.Result, error) {
	limit := pageParam(params.Get("limit"), 10, 100)
	offset := (pageParam(params.Get("page"), 1, 1000000) - 1) * limit
	categories := strings.FieldsFunc(params.Get("categories"), func(r rune) bool { return r == ',' || r == '.' })
	if categories == nil {
		categories = []string{}
	}
	search := params.Get("search")

	tx, err := s.db.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	const filter = ` FROM products WHERE tenant_id=$1
 AND (cardinality($2::text[])=0 OR category=ANY($2))
 AND ($3='' OR strpos(lower(name),lower($3))>0 OR strpos(lower(description),lower($3))>0)`
	var count int
	if err := tx.QueryRow(ctx, "SELECT count(*)"+filter, a.TenantID, categories, search).Scan(&count); err != nil {
		return nil, err
	}
	order := "id DESC"
	var sorts []struct {
		ID   string `json:"id"`
		Desc bool   `json:"desc"`
	}
	if json.Unmarshal([]byte(params.Get("sort")), &sorts) == nil {
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
		return nil, err
	}
	defer rows.Close()
	list := []product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &auth.Result{Status: 200, Data: map[string]any{"success": true, "time": time.Now().UTC(), "message": "OK", "total_products": count, "offset": offset, "limit": limit, "products": list}}, nil
}

func (s *server) getProductUsecase(ctx context.Context, a auth.Principal, resourceID string) (*auth.Result, error) {
	id, err := parseProductID(resourceID)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(ctx, "SELECT "+productFields+" FROM products WHERE tenant_id=$1 AND id=$2", a.TenantID, id)
	return productResult(200, row)
}

func (s *server) createProductUsecase(ctx context.Context, a auth.Principal, body map[string]any) (*auth.Result, error) {
	p, err := parseProductInput(body)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(ctx, "INSERT INTO products(tenant_id,name,category,price,description) VALUES($1,$2,$3,$4,$5) RETURNING "+productFields,
		a.TenantID, p.Name, p.Category, p.Price, p.Description)
	return productResult(201, row)
}

func (s *server) updateProductUsecase(ctx context.Context, a auth.Principal, resourceID string, body map[string]any) (*auth.Result, error) {
	id, err := parseProductID(resourceID)
	if err != nil {
		return nil, err
	}
	p, err := parseProductInput(body)
	if err != nil {
		return nil, err
	}
	row := s.db.QueryRow(ctx, "UPDATE products SET name=$3,category=$4,price=$5,description=$6,updated_at=now() WHERE tenant_id=$1 AND id=$2 RETURNING "+productFields,
		a.TenantID, id, p.Name, p.Category, p.Price, p.Description)
	return productResult(200, row)
}

func (s *server) deleteProductUsecase(ctx context.Context, a auth.Principal, resourceID string) (*auth.Result, error) {
	id, err := parseProductID(resourceID)
	if err != nil {
		return nil, err
	}
	tag, err := s.db.Exec(ctx, "DELETE FROM products WHERE tenant_id=$1 AND id=$2", a.TenantID, id)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, auth.APIError{Status: 404, Message: "product not found"}
	}
	return &auth.Result{Status: 200, Data: map[string]any{"success": true, "message": "Product deleted"}}, nil
}

func (s *server) listMembersUsecase(ctx context.Context, a auth.Principal) (*auth.Result, error) {
	rows, err := s.db.Query(ctx, `SELECT u.id,u.name,u.email,m.role FROM memberships m
 JOIN users u ON u.id=m.user_id WHERE m.tenant_id=$1 ORDER BY u.id`, a.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, email, role string
		if err := rows.Scan(&id, &name, &email, &role); err != nil {
			return nil, err
		}
		list = append(list, map[string]any{"id": id, "name": name, "email": email, "role": role})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &auth.Result{Status: 200, Data: list}, nil
}

func (s *server) updateMemberUsecase(ctx context.Context, a auth.Principal, resourceID string, body map[string]any) (*auth.Result, error) {
	var input struct {
		Role string `json:"role"`
	}
	if err := auth.DecodeBody(body, &input); err != nil {
		return nil, err
	}
	if input.Role != "owner" && input.Role != "member" {
		return nil, auth.APIError{Status: 400, Message: "invalid role"}
	}
	return s.mutateMember(ctx, a, resourceID, input.Role, false)
}

func (s *server) removeMemberUsecase(ctx context.Context, a auth.Principal, resourceID string) (*auth.Result, error) {
	return s.mutateMember(ctx, a, resourceID, "", true)
}

func (s *server) mutateMember(ctx context.Context, a auth.Principal, resourceID, newRole string, remove bool) (*auth.Result, error) {
	id, err := strconv.ParseInt(resourceID, 10, 64)
	if err != nil {
		return nil, auth.APIError{Status: 404, Message: "member not found"}
	}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "SELECT id FROM tenants WHERE id=$1 FOR UPDATE", a.TenantID); err != nil {
		return nil, err
	}
	var isOwner bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM memberships WHERE tenant_id=$1 AND user_id=$2 AND role='owner')", a.TenantID, a.UserID).Scan(&isOwner)
	if err != nil {
		return nil, err
	}
	if !isOwner {
		return nil, auth.APIError{Status: 403, Message: "workspace access denied"}
	}
	var role string
	err = tx.QueryRow(ctx, "SELECT role FROM memberships WHERE tenant_id=$1 AND user_id=$2", a.TenantID, id).Scan(&role)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.APIError{Status: 404, Message: "member not found"}
	}
	if err != nil {
		return nil, err
	}
	if role == "owner" && (remove || newRole != "owner") {
		var count int
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM memberships WHERE tenant_id=$1 AND role='owner'", a.TenantID).Scan(&count); err != nil {
			return nil, err
		}
		if count <= 1 {
			return nil, auth.APIError{Status: 409, Message: "workspace requires an owner"}
		}
	}
	if remove {
		_, err = tx.Exec(ctx, "DELETE FROM memberships WHERE tenant_id=$1 AND user_id=$2", a.TenantID, id)
	} else {
		_, err = tx.Exec(ctx, "UPDATE memberships SET role=$3 WHERE tenant_id=$1 AND user_id=$2", a.TenantID, id, newRole)
	}
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &auth.Result{Status: http.StatusNoContent}, nil
}

func (s *server) listInvitationsUsecase(ctx context.Context, a auth.Principal) (*auth.Result, error) {
	rows, err := s.db.Query(ctx, `SELECT encode(token_hash,'hex'),email,expires_at FROM invitations
  WHERE tenant_id=$1 AND expires_at>now() AND accepted_at IS NULL AND revoked_at IS NULL ORDER BY expires_at`, a.TenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []map[string]any{}
	for rows.Next() {
		var id, email string
		var expires time.Time
		if err := rows.Scan(&id, &email, &expires); err != nil {
			return nil, err
		}
		list = append(list, map[string]any{"id": id, "email": email, "expiresAt": expires})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &auth.Result{Status: 200, Data: list}, nil
}

func (s *server) createInvitationUsecase(ctx context.Context, a auth.Principal, body map[string]any) (*auth.Result, error) {
	var input struct {
		Email string `json:"email"`
	}
	if err := auth.DecodeBody(body, &input); err != nil {
		return nil, err
	}
	email := strings.ToLower(strings.TrimSpace(input.Email))
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return nil, auth.APIError{Status: 400, Message: "invalid email"}
	}
	token := auth.NewToken()
	_, err = s.db.Exec(ctx, "INSERT INTO invitations(token_hash,tenant_id,email,expires_at) VALUES($1,$2,$3,now()+interval '7 days')", auth.HashToken(token), a.TenantID, email)
	if err != nil {
		return nil, err
	}
	return &auth.Result{Status: 201, Data: map[string]string{"url": fmt.Sprintf("%s/invite#%s", s.origins[0], token)}}, nil
}

func (s *server) revokeInvitationUsecase(ctx context.Context, a auth.Principal, resourceID string) (*auth.Result, error) {
	tag, err := s.db.Exec(ctx, `UPDATE invitations SET revoked_at=now() WHERE tenant_id=$1 AND encode(token_hash,'hex')=$2
  AND accepted_at IS NULL AND revoked_at IS NULL`, a.TenantID, resourceID)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, auth.APIError{Status: 404, Message: "invitation not found"}
	}
	return &auth.Result{Status: http.StatusNoContent}, nil
}

func (s *server) inspectInvitationUsecase(ctx context.Context, body map[string]any) (*auth.Result, error) {
	var input struct{ Token string }
	if err := auth.DecodeBody(body, &input); err != nil {
		return nil, err
	}
	if len(input.Token) != 43 {
		return nil, auth.APIError{Status: 404, Message: "invitation unavailable"}
	}
	var name, email, organization string
	var expires time.Time
	err := s.db.QueryRow(ctx, "SELECT t.name,i.email,i.expires_at,coalesce(t.casdoor_org,'') FROM invitations i JOIN tenants t ON t.id=i.tenant_id WHERE i.token_hash=$1 AND i.expires_at>now() AND i.accepted_at IS NULL AND i.revoked_at IS NULL", auth.HashToken(input.Token)).Scan(&name, &email, &expires, &organization)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.APIError{Status: 404, Message: "invitation unavailable"}
	}
	if err != nil {
		return nil, err
	}
	parts := strings.SplitN(email, "@", 2)
	hint := "***"
	if len(parts) == 2 {
		hint = string([]rune(parts[0])[:1]) + "***@" + parts[1]
	}
	return &auth.Result{Status: 200, Data: map[string]any{"companyName": name, "emailHint": hint, "expiresAt": expires, "organization": organization}}, nil
}

func (s *server) acceptInvitationUsecase(ctx context.Context, a auth.Principal, body map[string]any, sessionHash []byte) (*auth.Result, error) {
	var input struct{ Token string }
	if err := auth.DecodeBody(body, &input); err != nil {
		return nil, err
	}
	if len(input.Token) != 43 {
		return nil, auth.APIError{Status: 404, Message: "invitation unavailable"}
	}

	tx, err := s.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	var email string
	var verified bool
	if err = tx.QueryRow(ctx, "SELECT email,email_verified FROM users WHERE id=$1 FOR UPDATE", a.UserID).Scan(&email, &verified); err != nil {
		return nil, err
	}
	var active bool
	if err = tx.QueryRow(ctx, "SELECT true FROM sessions WHERE token_hash=$1 AND user_id=$2 AND expires_at>now() FOR UPDATE", sessionHash, a.UserID).Scan(&active); errors.Is(err, pgx.ErrNoRows) {
		return nil, auth.APIError{Status: 401, Message: "session expired"}
	}
	if err != nil {
		return nil, err
	}
	var company int64
	var invitedEmail string
	var accepted *time.Time
	err = tx.QueryRow(ctx, "SELECT tenant_id,email,accepted_at FROM invitations WHERE token_hash=$1 AND expires_at>now() AND revoked_at IS NULL FOR UPDATE", auth.HashToken(input.Token)).Scan(&company, &invitedEmail, &accepted)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && !strings.EqualFold(email, invitedEmail)) {
		return nil, auth.APIError{Status: 404, Message: "invitation unavailable"}
	}
	if err != nil {
		return nil, err
	}
	if !verified && !s.allowUnverifiedInvites {
		return nil, auth.APIError{Status: 403, Message: "email verification required"}
	}
	var organizationMatches bool
	if err = tx.QueryRow(ctx, `SELECT t.casdoor_org IS NULL OR t.casdoor_org=u.casdoor_org FROM tenants t CROSS JOIN users u WHERE t.id=$1 AND u.id=$2`, company, a.UserID).Scan(&organizationMatches); err != nil {
		return nil, err
	}
	if !organizationMatches {
		return nil, auth.APIError{Status: 403, Message: "please sign in to the invited company organization"}
	}
	var bound int64
	err = tx.QueryRow(ctx, "SELECT tenant_id FROM memberships WHERE user_id=$1", a.UserID).Scan(&bound)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if bound != 0 && bound != company {
		return nil, auth.APIError{Status: 409, Message: "account belongs to another company"}
	}
	if accepted != nil {
		return nil, auth.APIError{Status: 404, Message: "invitation unavailable"}
	}
	if bound == 0 {
		if _, err = tx.Exec(ctx, "INSERT INTO memberships(tenant_id,user_id,role) VALUES($1,$2,'member')", company, a.UserID); err != nil {
			return nil, err
		}
	}
	if _, err = tx.Exec(ctx, "UPDATE invitations SET accepted_at=now() WHERE token_hash=$1", auth.HashToken(input.Token)); err != nil {
		return nil, err
	}
	if _, err = tx.Exec(ctx, "UPDATE sessions SET tenant_id=$1 WHERE user_id=$2 AND expires_at>now()", company, a.UserID); err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &auth.Result{Status: 200, Data: map[string]int64{"workspaceId": company}}, nil
}
