package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"net/http"
)

type APIError struct {
	Status       int
	Message      string
	ClearSession bool
	Plain        bool
}

func (e APIError) Error() string { return e.Message }

type Result struct {
	Status int
	Data   any
}

func DecodeBody(body map[string]any, dst any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return APIError{Status: 400, Message: "invalid request"}
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(dst); err != nil {
		return APIError{Status: 400, Message: "invalid request"}
	}
	return nil
}
func (s *Service) SessionToken(r *http.Request) string { return s.readCookie(r, SessionCookie) }
func (s *Service) AuthenticateToken(ctx context.Context, token string) (Principal, error) {
	var a Principal
	if token == "" {
		return a, APIError{Status: 401, Message: "sign in required"}
	}
	err := s.db.QueryRow(ctx, `SELECT s.user_id,coalesce(m.tenant_id,0),coalesce(m.role,'') FROM sessions s LEFT JOIN memberships m ON m.user_id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now()`, HashToken(token)).Scan(&a.UserID, &a.TenantID, &a.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, APIError{Status: 401, Message: "session expired"}
	}
	if err != nil {
		return a, err
	}
	if s.sharedVerifier != nil && a.TenantID != 0 {
		var matches bool
		err = s.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users u JOIN tenants t ON t.id=$2 WHERE u.id=$1 AND u.casdoor_org<>'' AND u.casdoor_org=t.casdoor_org)`, a.UserID, a.TenantID).Scan(&matches)
		if err != nil {
			return a, err
		}
		if !matches {
			_, _ = s.db.Exec(ctx, "DELETE FROM sessions WHERE token_hash=$1", HashToken(token))
			return a, APIError{Status: 401, Message: "company identity changed; please sign in again", ClearSession: true}
		}
	}
	return a, nil
}
func (s *Service) RegistrationEnabled() bool { return s.registration != nil }

func (s *Service) ClearSessionCookie(w http.ResponseWriter) { s.cookie(w, SessionCookie, "", -1) }
