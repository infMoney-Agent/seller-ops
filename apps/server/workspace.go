package main

import (
	"net/http"

	"seller-ops/server/internal/auth"
	"seller-ops/server/internal/httpresponse"
)

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	a, ok := s.auth.Authenticate(w, r)
	if !ok {
		return
	}
	var name, email string
	if err := s.db.QueryRow(r.Context(), "SELECT name,email FROM users WHERE id=$1", a.UserID).Scan(&name, &email); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	rows, err := s.db.Query(r.Context(), `SELECT t.id,t.name,m.role FROM memberships m JOIN tenants t ON t.id=m.tenant_id WHERE m.user_id=$1 ORDER BY t.id`, a.UserID)
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	defer rows.Close()
	workspaces := []map[string]any{}
	for rows.Next() {
		var id int64
		var name, role string
		if err := rows.Scan(&id, &name, &role); err != nil {
			httpresponse.Internal(w, err)
			return
		}
		workspaces = append(workspaces, map[string]any{"id": id, "name": name, "role": role})
	}
	if err := rows.Err(); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	httpresponse.JSON(w, 200, map[string]any{"id": a.UserID, "name": name, "email": email, "workspaceId": a.TenantID, "role": a.Role, "workspaces": workspaces})
}

func (s *server) switchWorkspace(w http.ResponseWriter, r *http.Request) {
	a, ok := s.auth.Authenticate(w, r)
	if !ok {
		return
	}
	var input struct {
		WorkspaceID int64 `json:"workspaceId"`
	}
	if !decode(w, r, &input) {
		return
	}
	tag, err := s.db.Exec(r.Context(), `UPDATE sessions SET tenant_id=$1 WHERE token_hash=$2 AND expires_at>now()
 AND EXISTS(SELECT 1 FROM memberships WHERE tenant_id=$1 AND user_id=$3)`,
		input.WorkspaceID, auth.SessionHash(r), a.UserID)
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	if tag.RowsAffected() == 0 {
		httpresponse.Fail(w, 404, "workspace not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
