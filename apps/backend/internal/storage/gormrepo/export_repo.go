package gormrepo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
)

// ExportRepo 定义筛选结果导出任务的 GORM 仓储接口。
type ExportRepo interface {
	UpsertTask(ctx context.Context, task *model.ExportTask) error
	GetByTaskNo(ctx context.Context, workspaceID uint64, taskNo string) (*model.ExportTask, error)
	ListTasks(ctx context.Context, workspaceID uint64, page, pageSize int) ([]model.ExportTask, int64, error)
	UpdateTaskStatus(ctx context.Context, workspaceID uint64, id uint64, status string, exportedRows int, filePath, errMsg string, finishedAt *time.Time) error
}

type gormExportRepo struct {
	db *gorm.DB
}

func (r *gormExportRepo) UpsertTask(ctx context.Context, task *model.ExportTask) error {
	if task.Status == "" {
		task.Status = "pending"
	}
	if len(task.FilterParams) == 0 {
		task.FilterParams = []byte("{}")
	}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_no"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"filter_params", "status", "exported_rows", "file_path",
			"error_message", "operator_id", "finished_at", "updated_at",
		}),
	}).Create(task).Error
	if err != nil {
		return err
	}
	if task.ID == 0 {
		existing, err := r.GetByTaskNo(ctx, task.WorkspaceID, task.TaskNo)
		if err != nil {
			return err
		}
		task.ID = existing.ID
	}
	return nil
}

func (r *gormExportRepo) GetByTaskNo(ctx context.Context, workspaceID uint64, taskNo string) (*model.ExportTask, error) {
	var task model.ExportTask
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "task_no"}, Value: taskNo}).
		First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *gormExportRepo) ListTasks(ctx context.Context, workspaceID uint64, page, pageSize int) ([]model.ExportTask, int64, error) {
	var total int64
	base := r.db.WithContext(ctx).Model(&model.ExportTask{}).Scopes(WithWorkspace(workspaceID))
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.ExportTask
	err := base.
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}, Desc: true}}}).
		Scopes(WithPagination(page, pageSize)).
		Find(&items).Error
	return items, total, err
}

func (r *gormExportRepo) UpdateTaskStatus(ctx context.Context, workspaceID uint64, id uint64, status string, exportedRows int, filePath, errMsg string, finishedAt *time.Time) error {
	return r.db.WithContext(ctx).
		Model(&model.ExportTask{}).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Updates(map[string]any{
			"status":        status,
			"exported_rows": exportedRows,
			"file_path":     filePath,
			"error_message": errMsg,
			"finished_at":   finishedAt,
		}).Error
}
