package gormrepo

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
)

// WorkspaceRepo 定义工作空间与成员的 GORM 仓储接口。
type WorkspaceRepo interface {
	UpsertWorkspace(ctx context.Context, ws *model.Workspace) error
	GetByCode(ctx context.Context, code string) (*model.Workspace, error)
	GetByID(ctx context.Context, id uint64) (*model.Workspace, error)
	ListWorkspaces(ctx context.Context) ([]model.Workspace, error)
	UpsertMember(ctx context.Context, member *model.WorkspaceMember) error
	GetMember(ctx context.Context, workspaceID uint64, userID string) (*model.WorkspaceMember, error)
	ListMembers(ctx context.Context, workspaceID uint64) ([]model.WorkspaceMember, error)
}

type gormWorkspaceRepo struct {
	db *gorm.DB
}

func (r *gormWorkspaceRepo) UpsertWorkspace(ctx context.Context, ws *model.Workspace) error {
	if ws.Status == "" {
		ws.Status = "active"
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "code"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "status", "created_by", "updated_at"}),
	}).Create(ws).Error
	if err != nil {
		return err
	}
	if ws.ID == 0 {
		existing, err := r.GetByCode(ctx, ws.Code)
		if err != nil {
			return err
		}
		ws.ID = existing.ID
	}
	return nil
}

func (r *gormWorkspaceRepo) GetByCode(ctx context.Context, code string) (*model.Workspace, error) {
	var ws model.Workspace
	err := r.db.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "code"}, Value: code}).
		First(&ws).Error
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

func (r *gormWorkspaceRepo) GetByID(ctx context.Context, id uint64) (*model.Workspace, error) {
	var ws model.Workspace
	err := r.db.WithContext(ctx).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		First(&ws).Error
	if err != nil {
		return nil, err
	}
	return &ws, nil
}

func (r *gormWorkspaceRepo) ListWorkspaces(ctx context.Context) ([]model.Workspace, error) {
	var items []model.Workspace
	err := r.db.WithContext(ctx).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}).
		Find(&items).Error
	return items, err
}

func (r *gormWorkspaceRepo) UpsertMember(ctx context.Context, member *model.WorkspaceMember) error {
	if member.Role == "" {
		member.Role = "operator"
	}
	if member.Status == "" {
		member.Status = "active"
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "workspace_id"}, {Name: "user_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"role", "status", "updated_at"}),
	}).Create(member).Error
	if err != nil {
		return err
	}
	if member.ID == 0 {
		existing, err := r.GetMember(ctx, member.WorkspaceID, member.UserID)
		if err != nil {
			return err
		}
		member.ID = existing.ID
	}
	return nil
}

func (r *gormWorkspaceRepo) GetMember(ctx context.Context, workspaceID uint64, userID string) (*model.WorkspaceMember, error) {
	var member model.WorkspaceMember
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "user_id"}, Value: userID}).
		First(&member).Error
	if err != nil {
		return nil, err
	}
	return &member, nil
}

func (r *gormWorkspaceRepo) ListMembers(ctx context.Context, workspaceID uint64) ([]model.WorkspaceMember, error) {
	var members []model.WorkspaceMember
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}}}}).
		Find(&members).Error
	return members, err
}
