package main

import (
	"context"
	"net/http"
	"seller-ops/server/internal/auth"
)

func (s *server) session(w http.ResponseWriter, r *http.Request) {
	a, ok := s.auth.Authenticate(w, r)
	if !ok {
		return
	}
	result, err := s.sessionUsecase(r.Context(), a)
	s.respondBusiness(w, result, err)
}
func (s *server) sessionUsecase(ctx context.Context, a auth.Principal) (*auth.Result, error) {
	var name, email string
	var verified bool
	if err := s.db.QueryRow(ctx, "SELECT name,email,email_verified FROM users WHERE id=$1", a.UserID).Scan(&name, &email, &verified); err != nil {
		return nil, err
	}
	// workspaces is kept for clients still rendering a workspace selector.
	rows, err := s.db.Query(ctx, `SELECT t.id,t.name,m.role FROM memberships m JOIN tenants t ON t.id=m.tenant_id WHERE m.user_id=$1 ORDER BY t.id`, a.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	workspaces := []map[string]any{}
	for rows.Next() {
		var id int64
		var workspaceName, workspaceRole string
		if err := rows.Scan(&id, &workspaceName, &workspaceRole); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, map[string]any{"id": id, "name": workspaceName, "role": workspaceRole})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &auth.Result{Status: 200, Data: map[string]any{"id": a.UserID, "name": name, "email": email, "emailVerified": verified, "workspaceId": a.TenantID, "role": a.Role, "workspaces": workspaces}}, nil
}
