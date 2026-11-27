package main

import (
	"context"
	"google.golang.org/protobuf/proto"
	authv1 "seller-ops/contracts/sellerops/auth/v1"
	"seller-ops/server/internal/auth"
)

func (s *server) sessionUsecase(ctx context.Context, a auth.Principal) (*authv1.SessionProfile, error) {
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
	workspaces := []*authv1.Workspace{}
	for rows.Next() {
		var id int64
		var workspaceName, workspaceRole string
		if err := rows.Scan(&id, &workspaceName, &workspaceRole); err != nil {
			return nil, err
		}
		workspaces = append(workspaces, &authv1.Workspace{
			Id:   proto.Int64(id),
			Name: proto.String(workspaceName),
			Role: proto.String(workspaceRole),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &authv1.SessionProfile{
		Id:            proto.Int64(a.UserID),
		Name:          proto.String(name),
		Email:         proto.String(email),
		EmailVerified: proto.Bool(verified),
		WorkspaceId:   proto.Int64(a.TenantID),
		Role:          proto.String(a.Role),
		Workspaces:    workspaces,
	}, nil
}
