package gormrepo

import (
	"context"

	"gorm.io/gorm"
)

const DefaultBatchSize = 500

// Store 汇总全部 9 张业务表的 GORM 仓储实现，并统一管理跨表事务边界。
type Store struct {
	db *gorm.DB
}

// NewStore 创建基于 GORM DB 的仓储聚合入口。
func NewStore(db *gorm.DB) *Store {
	return &Store{db: db}
}

// DB 返回底层 GORM 实例（仅用于连接健康检查或测试断言）。
func (s *Store) DB() *gorm.DB {
	return s.db
}

// Transaction 在单一数据库事务内执行跨表业务操作。
func (s *Store) Transaction(ctx context.Context, fn func(txStore *Store) error) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&Store{db: tx})
	})
}

func (s *Store) Workspaces() WorkspaceRepo         { return &gormWorkspaceRepo{db: s.db} }
func (s *Store) Imports() ImportRepo               { return &gormImportRepo{db: s.db} }
func (s *Store) Screening() ScreeningRepo          { return &gormScreeningRepo{store: s, db: s.db} }
func (s *Store) MonthlyMetrics() MonthlyMetricRepo { return &gormMonthlyMetricRepo{db: s.db} }
func (s *Store) ProfitCalcs() ProfitCalcRepo       { return &gormProfitCalcRepo{store: s, db: s.db} }
func (s *Store) Exports() ExportRepo               { return &gormExportRepo{db: s.db} }
