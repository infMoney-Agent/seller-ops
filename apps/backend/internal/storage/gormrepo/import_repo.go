package gormrepo

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"seller-ops/backend/internal/model"
)

// ImportRepo 定义导入任务与原始行快照的 GORM 仓储接口。
type ImportRepo interface {
	UpsertTask(ctx context.Context, task *model.ImportTask) error
	GetTaskByNo(ctx context.Context, workspaceID uint64, taskNo string) (*model.ImportTask, error)
	GetTaskByID(ctx context.Context, workspaceID uint64, id uint64) (*model.ImportTask, error)
	ListTasks(ctx context.Context, workspaceID uint64, page, pageSize int) ([]model.ImportTask, int64, error)
	UpdateTaskProgress(ctx context.Context, workspaceID uint64, id uint64, status string, total, success, failed, skipped int, errMsg string, finishedAt *time.Time) error
	BatchUpsertRecords(ctx context.Context, workspaceID uint64, records []model.ImportRecord) error
	ListRecords(ctx context.Context, workspaceID uint64, taskID uint64, page, pageSize int) ([]model.ImportRecord, int64, error)
}

type gormImportRepo struct {
	db *gorm.DB
}

func (r *gormImportRepo) UpsertTask(ctx context.Context, task *model.ImportTask) error {
	if task.SourceType == "" {
		task.SourceType = "excel"
	}
	if task.Status == "" {
		task.Status = "pending"
	}
	task.DeletedAt = gorm.DeletedAt{}
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_no"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"file_name", "source_type", "status", "total_rows",
			"success_rows", "failed_rows", "skipped_rows",
			"error_message", "operator_id", "finished_at", "deleted_at", "updated_at",
		}),
	}).Create(task).Error
	if err != nil {
		return err
	}
	if task.ID == 0 {
		existing, err := r.GetTaskByNo(ctx, task.WorkspaceID, task.TaskNo)
		if err != nil {
			return err
		}
		task.ID = existing.ID
	}
	return nil
}

func (r *gormImportRepo) GetTaskByNo(ctx context.Context, workspaceID uint64, taskNo string) (*model.ImportTask, error) {
	var task model.ImportTask
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "task_no"}, Value: taskNo}).
		First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *gormImportRepo) GetTaskByID(ctx context.Context, workspaceID uint64, id uint64) (*model.ImportTask, error) {
	var task model.ImportTask
	err := r.db.WithContext(ctx).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		First(&task).Error
	if err != nil {
		return nil, err
	}
	return &task, nil
}

func (r *gormImportRepo) ListTasks(ctx context.Context, workspaceID uint64, page, pageSize int) ([]model.ImportTask, int64, error) {
	var total int64
	base := r.db.WithContext(ctx).Model(&model.ImportTask{}).Scopes(WithWorkspace(workspaceID))
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.ImportTask
	err := base.
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "id"}, Desc: true}}}).
		Scopes(WithPagination(page, pageSize)).
		Find(&items).Error
	return items, total, err
}

func (r *gormImportRepo) UpdateTaskProgress(ctx context.Context, workspaceID uint64, id uint64, status string, total, success, failed, skipped int, errMsg string, finishedAt *time.Time) error {
	updates := map[string]any{
		"status":        status,
		"total_rows":    total,
		"success_rows":  success,
		"failed_rows":   failed,
		"skipped_rows":  skipped,
		"error_message": errMsg,
		"finished_at":   finishedAt,
	}
	return r.db.WithContext(ctx).
		Model(&model.ImportTask{}).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "id"}, Value: id}).
		Updates(updates).Error
}

func (r *gormImportRepo) BatchUpsertRecords(ctx context.Context, workspaceID uint64, records []model.ImportRecord) error {
	if len(records) == 0 {
		return nil
	}
	for i := range records {
		records[i].WorkspaceID = workspaceID
		records[i].DeletedAt = gorm.DeletedAt{}
		if records[i].Status == "" {
			records[i].Status = "success"
		}
		if len(records[i].RawPayload) == 0 {
			records[i].RawPayload = []byte("{}")
		}
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "task_id"}, {Name: "row_no"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"asin", "raw_payload", "status", "error_message", "deleted_at", "updated_at",
		}),
	}).CreateInBatches(records, DefaultBatchSize).Error
}

func (r *gormImportRepo) ListRecords(ctx context.Context, workspaceID uint64, taskID uint64, page, pageSize int) ([]model.ImportRecord, int64, error) {
	var total int64
	base := r.db.WithContext(ctx).
		Model(&model.ImportRecord{}).
		Scopes(WithWorkspace(workspaceID)).
		Where(clause.Eq{Column: clause.Column{Name: "task_id"}, Value: taskID})
	if err := base.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var items []model.ImportRecord
	err := base.
		Clauses(clause.OrderBy{Columns: []clause.OrderByColumn{{Column: clause.Column{Name: "row_no"}}}}).
		Scopes(WithPagination(page, pageSize)).
		Find(&items).Error
	return items, total, err
}
