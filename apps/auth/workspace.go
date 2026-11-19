package main

import (
	"net/http"

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
	// workspaces is kept for clients still rendering a workspace selector.
	rows, err := s.db.Query(r.Context(), `SELECT t.id,t.name,m.role FROM memberships m JOIN tenants t ON t.id=m.tenant_id WHERE m.user_id=$1 ORDER BY t.id`, a.UserID)
	if err != nil {
		httpresponse.Internal(w, err)
		return
	}
	defer rows.Close()
	workspaces := []map[string]any{}
	for rows.Next() {
		var id int64
		var workspaceName, workspaceRole string
		if err := rows.Scan(&id, &workspaceName, &workspaceRole); err != nil {
			httpresponse.Internal(w, err)
			return
		}
		workspaces = append(workspaces, map[string]any{"id": id, "name": workspaceName, "role": workspaceRole})
	}
	if err := rows.Err(); err != nil {
		httpresponse.Internal(w, err)
		return
	}
	httpresponse.JSON(w, 200, map[string]any{"id": a.UserID, "name": name, "email": email, "workspaceId": a.TenantID, "role": a.Role, "workspaces": workspaces})
}
