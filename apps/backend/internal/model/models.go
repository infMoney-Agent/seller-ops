package model

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// BaseModel 公共基础字段
type BaseModel struct {
	ID        uint64         `gorm:"primaryKey;autoIncrement"`
	CreatedAt time.Time      `gorm:"not null;autoCreateTime"`
	UpdatedAt time.Time      `gorm:"not null;autoUpdateTime"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

// ---------------------------------------------------------------------------
// 1. 认证与工作空间域
// ---------------------------------------------------------------------------

// Workspace 工作空间主表
type Workspace struct {
	BaseModel
	Code      string `gorm:"type:varchar(64);not null;uniqueIndex:uk_workspace_code"`
	Name      string `gorm:"type:varchar(128);not null"`
	Status    string `gorm:"type:varchar(32);not null;default:'active';index"` // active / disabled
	CreatedBy string `gorm:"type:varchar(64);not null"`
}

func (Workspace) TableName() string { return "workspaces" }

// WorkspaceMember 工作空间成员归属表
type WorkspaceMember struct {
	BaseModel
	WorkspaceID uint64 `gorm:"not null;uniqueIndex:uk_ws_user;index:idx_ws_member_ws"`
	UserID      string `gorm:"type:varchar(64);not null;uniqueIndex:uk_ws_user;index:idx_ws_member_user"`
	Role        string `gorm:"type:varchar(32);not null;default:'operator'"` // owner / admin / operator
	Status      string `gorm:"type:varchar(32);not null;default:'active'"`
}

func (WorkspaceMember) TableName() string { return "workspace_members" }

// ---------------------------------------------------------------------------
// 2. 导入与导出任务域
// ---------------------------------------------------------------------------

// ImportTask 批量导入任务主表
type ImportTask struct {
	BaseModel
	WorkspaceID  uint64 `gorm:"not null;index:idx_import_task_ws_status"`
	TaskNo       string `gorm:"type:varchar(64);not null;uniqueIndex:uk_import_task_no"`
	FileName     string `gorm:"type:varchar(255);not null"`
	SourceType   string `gorm:"type:varchar(32);not null;default:'excel'"`                                   // excel / csv / local_migration
	Status       string `gorm:"type:varchar(32);not null;default:'pending';index:idx_import_task_ws_status"` // pending / running / completed / failed
	TotalRows    int    `gorm:"not null;default:0"`
	SuccessRows  int    `gorm:"not null;default:0"`
	FailedRows   int    `gorm:"not null;default:0"`
	SkippedRows  int    `gorm:"not null;default:0"`
	ErrorMessage string `gorm:"type:text"`
	OperatorID   string `gorm:"type:varchar(64);not null"`
	FinishedAt   *time.Time
}

func (ImportTask) TableName() string { return "import_tasks" }

// ImportRecord 导入原始行快照与异常记录表
type ImportRecord struct {
	BaseModel
	WorkspaceID  uint64         `gorm:"not null;index:idx_import_rec_ws_task"`
	TaskID       uint64         `gorm:"not null;uniqueIndex:uk_task_row;index:idx_import_rec_ws_task"`
	RowNo        int            `gorm:"not null;uniqueIndex:uk_task_row"`
	ASIN         string         `gorm:"type:varchar(32);index"`
	RawPayload   datatypes.JSON `gorm:"type:json;not null"`
	Status       string         `gorm:"type:varchar(32);not null;default:'success';index"` // success / failed / skipped
	ErrorMessage string         `gorm:"type:text"`
}

func (ImportRecord) TableName() string { return "import_records" }

// ExportTask 导出任务表
type ExportTask struct {
	BaseModel
	WorkspaceID  uint64         `gorm:"not null;index:idx_export_task_ws"`
	TaskNo       string         `gorm:"type:varchar(64);not null;uniqueIndex:uk_export_task_no"`
	FilterParams datatypes.JSON `gorm:"type:json"`
	Status       string         `gorm:"type:varchar(32);not null;default:'pending';index"` // pending / running / completed / failed
	ExportedRows int            `gorm:"not null;default:0"`
	FilePath     string         `gorm:"type:varchar(512)"`
	ErrorMessage string         `gorm:"type:text"`
	OperatorID   string         `gorm:"type:varchar(64);not null"`
	FinishedAt   *time.Time
}

func (ExportTask) TableName() string { return "export_tasks" }

// ---------------------------------------------------------------------------
// 3. 产品初筛与月度指标域
// ---------------------------------------------------------------------------

// ScreeningItem 初筛商品 / 父子体主表
type ScreeningItem struct {
	ProfitSourceData datatypes.JSON `gorm:"type:json"`
	// SourceData preserves the complete source API projection, including fields not yet normalized.
	SourceData       datatypes.JSON `gorm:"type:json"`
	SourceFamilyRank int            `gorm:"not null;default:0"`
	BaseModel
	WorkspaceID uint64 `gorm:"not null;uniqueIndex:uk_ws_asin;index:idx_screen_ws_parent;index:idx_screen_ws_status"`
	ASIN        string `gorm:"type:varchar(32);not null;uniqueIndex:uk_ws_asin"`
	ParentASIN  string `gorm:"type:varchar(32);not null;default:'';index:idx_screen_ws_parent"`
	IsParent    bool   `gorm:"not null;default:false;index"`
	ChildCount  int    `gorm:"not null;default:0"`

	// 基础商品信息
	TitleZh       string `gorm:"type:varchar(512);not null;default:''"`
	TitleEn       string `gorm:"type:varchar(512);not null;default:''"`
	Brand         string `gorm:"type:varchar(128);not null;default:'';index"`
	BrandCategory string `gorm:"type:varchar(64);not null;default:'';index"` // 商标分类
	CategoryPath  string `gorm:"type:varchar(512);not null;default:''"`
	CategoryZh    string `gorm:"type:varchar(255);not null;default:''"`
	ImageURL      string `gorm:"type:varchar(512);not null;default:''"`
	ListingPeriod string `gorm:"type:varchar(64);not null;default:''"` // 上架周期描述

	// 最新快照指标（多月份历史序列见 ScreeningItemMonthlyMetric）
	LatestPrice float64 `gorm:"type:decimal(12,2);not null;default:0"`
	Currency    string  `gorm:"type:varchar(16);not null;default:'USD'"`
	LatestSales int     `gorm:"not null;default:0"`
	ChildSales  *int    `gorm:""` // 可为空，无数据时前端展示 "—"
	Rating      float64 `gorm:"type:decimal(4,2);not null;default:0"`
	ReviewCount int     `gorm:"not null;default:0"`

	// 尺寸与重量规格
	ItemDimensions    string  `gorm:"type:varchar(128);not null;default:''"`
	PackageDimensions string  `gorm:"type:varchar(128);not null;default:''"`
	NetWeightGrams    float64 `gorm:"type:decimal(10,2);not null;default:0"`
	GrossWeightGrams  float64 `gorm:"type:decimal(10,2);not null;default:0"`

	// 机器初筛结论（基于规则库）
	MachineResult      string         `gorm:"type:varchar(32);not null;default:'pending';index:idx_screen_ws_status"` // pending / passed / rejected
	MachineRuleVersion string         `gorm:"type:varchar(64);not null;default:''"`
	MachineReasons     datatypes.JSON `gorm:"type:json"` // 命中的规则编码与原因列表

	// 人工复核结论（机器通过后由运营人员判断）
	ManualStatus string `gorm:"type:varchar(32);not null;default:'pending';index:idx_screen_ws_status"` // pending / approved / rejected
	ManualNote   string `gorm:"type:varchar(512);not null;default:''"`
	ReviewedBy   string `gorm:"type:varchar(64);not null;default:''"`
	ReviewedAt   *time.Time

	// 利润测算同步快照（未测算时 ProfitCalcStatus 为 pending，列表显示“待测算”）
	ProfitCalcStatus string   `gorm:"type:varchar(32);not null;default:'pending';index"` // pending / calculated
	LatestCalcID     *uint64  `gorm:"index"`
	ProfitMargin     *float64 `gorm:"type:decimal(8,4)"`  // 如 0.2530 表示 25.30%
	EstimatedProfit  *float64 `gorm:"type:decimal(12,2)"` // 单件预计利润

	LastImportTaskID *uint64 `gorm:"index"`
}

func (ScreeningItem) TableName() string { return "screening_items" }

// ScreeningItemMonthlyMetric 商品多月份售价与销量明细表（按月拆行）
type ScreeningItemMonthlyMetric struct {
	BaseModel
	WorkspaceID  uint64  `gorm:"not null;uniqueIndex:uk_ws_asin_month;index:idx_metric_ws_month"`
	ItemID       uint64  `gorm:"not null;index:idx_metric_item"`
	ASIN         string  `gorm:"type:varchar(32);not null;uniqueIndex:uk_ws_asin_month"`
	ParentASIN   string  `gorm:"type:varchar(32);not null;default:'';index"`
	StatMonth    string  `gorm:"type:char(7);not null;uniqueIndex:uk_ws_asin_month;index:idx_metric_ws_month"` // 统一格式 YYYY-MM，如 2026-10
	SellingPrice float64 `gorm:"type:decimal(12,2);not null;default:0"`                                        // 当月售价
	Currency     string  `gorm:"type:varchar(16);not null;default:'USD'"`
	MonthlySales int     `gorm:"not null;default:0"` // 当月销量
	ChildSales   *int    `gorm:""`                   // 当月子体销量（可选）
	BSRRank      *int    `gorm:""`                   // 当月大类排名（可选）
	SourceTaskID *uint64 `gorm:"index"`
}

func (ScreeningItemMonthlyMetric) TableName() string { return "screening_item_monthly_metrics" }

// ScreeningReviewLog 人工复核操作流水表
type ScreeningReviewLog struct {
	BaseModel
	WorkspaceID uint64 `gorm:"not null;index:idx_review_log_ws_item"`
	ItemID      uint64 `gorm:"not null;index:idx_review_log_ws_item"`
	ASIN        string `gorm:"type:varchar(32);not null;index"`
	BatchID     string `gorm:"type:varchar(64);not null;default:'';index"` // 批量复核时关联同一批次号
	ActionType  string `gorm:"type:varchar(32);not null"`                  // single / batch
	PrevStatus  string `gorm:"type:varchar(32);not null"`
	NextStatus  string `gorm:"type:varchar(32);not null"`
	Note        string `gorm:"type:varchar(512);not null;default:''"`
	OperatorID  string `gorm:"type:varchar(64);not null;index"`
}

func (ScreeningReviewLog) TableName() string { return "screening_review_logs" }

// ---------------------------------------------------------------------------
// 4. 利润测算域
// ---------------------------------------------------------------------------

// ProfitCalculation 利润测算记录表
type ProfitCalculation struct {
	SourceData datatypes.JSON `gorm:"type:json"`
	BaseModel
	WorkspaceID     uint64  `gorm:"not null;index:idx_profit_ws_item"`
	ItemID          uint64  `gorm:"not null;index:idx_profit_ws_item"`
	ASIN            string  `gorm:"type:varchar(32);not null;index"`
	Version         int     `gorm:"not null;default:1"`
	IsActive        bool    `gorm:"not null;default:true;index"`
	RefStatMonth    string  `gorm:"type:char(7);not null;default:''"` // 参考售价所属月份（可选）
	SellingPrice    float64 `gorm:"type:decimal(12,2);not null"`
	PurchaseCost    float64 `gorm:"type:decimal(12,2);not null;default:0"` // 采购成本
	FirstLegFreight float64 `gorm:"type:decimal(12,2);not null;default:0"` // 头程物流费
	ReferralFee     float64 `gorm:"type:decimal(12,2);not null;default:0"` // 平台佣金
	FulfillmentFee  float64 `gorm:"type:decimal(12,2);not null;default:0"` // FBA/尾程配送费
	AdsAndOtherCost float64 `gorm:"type:decimal(12,2);not null;default:0"` // 广告及其他成本
	ProfitMargin    float64 `gorm:"type:decimal(8,4);not null"`            // 测算利润率
	EstimatedProfit float64 `gorm:"type:decimal(12,2);not null"`           // 单件预计利润
	Currency        string  `gorm:"type:varchar(16);not null;default:'USD'"`
	CalculatedBy    string  `gorm:"type:varchar(64);not null"`
}

func (ProfitCalculation) TableName() string { return "profit_calculations" }

// ---------------------------------------------------------------------------
// 5. 规则配置与后台任务域（支持规则/模板管理、批量复核撤销与 MySQL 后台任务循环）
// ---------------------------------------------------------------------------

// ScreeningConfig 规则策略与利润模板配置表
type ScreeningConfig struct {
	BaseModel
	WorkspaceID uint64         `gorm:"not null;uniqueIndex:uk_cfg_ws_kind_ver;index:idx_cfg_ws_kind_active"`
	Kind        string         `gorm:"type:varchar(32);not null;uniqueIndex:uk_cfg_ws_kind_ver;index:idx_cfg_ws_kind_active"` // screening / profit_template
	Version     int            `gorm:"not null;uniqueIndex:uk_cfg_ws_kind_ver"`
	Name        string         `gorm:"type:varchar(128);not null"`
	Config      datatypes.JSON `gorm:"type:json;not null"`
	ConfigHash  string         `gorm:"type:char(64);not null"`
	Active      bool           `gorm:"not null;default:false;index:idx_cfg_ws_kind_active"`
	CreatedBy   string         `gorm:"type:varchar(64);not null"`
}

func (ScreeningConfig) TableName() string { return "screening_configs" }

// ScreeningJob 统一后台任务与批量复核批次表
type ScreeningJob struct {
	BaseModel
	WorkspaceID    uint64         `gorm:"not null;index:idx_sjob_ws_kind_state"`
	Kind           string         `gorm:"type:varchar(32);not null;index:idx_sjob_ws_kind_state"` // review / screen / profit_batch / export / import / parse
	State          string         `gorm:"type:varchar(32);not null;default:'queued';index:idx_sjob_ws_kind_state"`
	IdempotencyKey string         `gorm:"type:varchar(200);not null;default:'';index"`
	Payload        datatypes.JSON `gorm:"type:json;not null"`
	Result         datatypes.JSON `gorm:"type:json"`
	UndoResult     datatypes.JSON `gorm:"type:json"`
	ErrorMessage   string         `gorm:"type:text"`
	Progress       int            `gorm:"not null;default:0"`
	Attempts       int            `gorm:"not null;default:0"`
	ExpiresAt      *time.Time     `gorm:"index"`
	CreatedBy      string         `gorm:"type:varchar(64);not null"`
}

func (ScreeningJob) TableName() string { return "screening_jobs" }

// AllModels 返回全部核心业务表的 GORM 模型定义，供受控迁移与完整性校验使用。
func AllModels() []any {
	return []any{
		&Workspace{},
		&WorkspaceMember{},
		&ImportTask{},
		&ImportRecord{},
		&ExportTask{},
		&ScreeningItem{},
		&ScreeningItemMonthlyMetric{},
		&ScreeningReviewLog{},
		&ProfitCalculation{},
		&ScreeningConfig{},
		&ScreeningJob{},
	}
}
