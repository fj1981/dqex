// Package service 业务服务层：模型定义 + 持久化 + 任务调度 + CLI。
// 对外通过 Web 模式（Gin 托管前端 SPA）与 CLI 模式提供能力。
package service

import (
	"dbimpex/internal/engine"
)

// ---- 引擎模型别名（模型统一定义在 engine 包，避免循环依赖） ----

type (
	// DBConnInfo 数据库连接信息（含子类型）
	DBConnInfo = engine.DBConnInfo
	// TableCondition 表级过滤条件
	TableCondition = engine.TableCondition
	// ExportOptions 导出选项
	ExportOptions = engine.ExportOptions
	// ImportOptions 导入选项
	ImportOptions = engine.ImportOptions
	// MigrateOptions 迁移选项
	MigrateOptions = engine.MigrateOptions
	// CompareOptions 对比选项
	CompareOptions = engine.CompareOptions
	// DictionaryOptions 数据字典导出选项
	DictionaryOptions = engine.DictionaryOptions
	// TableAlias 表别名配对
	TableAlias = engine.TableAlias
	// CompareResult 对比结果
	CompareResult = engine.CompareResult
	// ResetMode 重置数据模式
	ResetMode = engine.ResetMode
	// ProgressInfo 任务进度信息
	ProgressInfo = engine.Progress
	// ProgressFunc 进度回调
	ProgressFunc = engine.ProgressFunc
	// Snapshot 快照完整数据
	Snapshot = engine.Snapshot
	// SnapshotInfo 快照摘要
	SnapshotInfo = engine.SnapshotInfo
	// SnapshotTable 单表快照
	SnapshotTable = engine.SnapshotTable
	// CreateSnapshotOptions 创建快照选项
	CreateSnapshotOptions = engine.CreateSnapshotOptions
	// SnapshotCompareOptions 快照对比选项
	SnapshotCompareOptions = engine.SnapshotCompareOptions
)

// 重置模式常量
const (
	ResetNone     = engine.ResetNone
	ResetTruncate = engine.ResetTruncate
	ResetDrop     = engine.ResetDrop
)

// SupportedDBTypes 支持的数据库类型及子类型。
// SubType 不代表版本号，而是标识“使用该类型兼容模式的具体数据库产品”（彼此存在语法差异）：
// 首项为原生标准库（值等于类型名），其余为兼容库产品。
var SupportedDBTypes = map[string][]string{
	"mysql":      {"mysql", "oceanbase", "mariadb"},
	"postgresql": {"postgresql", "gaussdb", "kingbase"},
	"oracle":     {"oracle", "dameng"},
}

// TaskConfig 任务配置（可保存/加载，自动记忆上次配置）
type TaskConfig struct {
	ID             string             `json:"id" yaml:"id"`
	Name           string             `json:"name" yaml:"name"`
	Type           string             `json:"type" yaml:"type"` // export/import/migrate/compare/dictionary
	ExportOpts     *ExportOptions     `json:"exportOpts,omitempty" yaml:"exportOpts,omitempty"`
	ImportOpts     *ImportOptions     `json:"importOpts,omitempty" yaml:"importOpts,omitempty"`
	MigrateOpts    *MigrateOptions    `json:"migrateOpts,omitempty" yaml:"migrateOpts,omitempty"`
	CompareOpts    *CompareOptions    `json:"compareOpts,omitempty" yaml:"compareOpts,omitempty"`
	DictionaryOpts *DictionaryOptions `json:"dictionaryOpts,omitempty" yaml:"dictionaryOpts,omitempty"`
	CreatedAt      int64              `json:"createdAt" yaml:"createdAt"`
	UpdatedAt      int64              `json:"updatedAt" yaml:"updatedAt"`
	IsLastUsed     bool               `json:"isLastUsed" yaml:"isLastUsed"`
}

// ExecutionRecord 执行历史记录
type ExecutionRecord struct {
	ID           string   `json:"id"`                     // 同 taskID
	TaskType     string   `json:"taskType"`               // export/import/migrate/compare/dictionary
	TaskConfigID string   `json:"taskConfigId,omitempty"` // 关联的任务配置 ID（可选）
	Status       string   `json:"status"`                 // running/done/error/cancelled
	StartedAt    int64    `json:"startedAt"`
	FinishedAt   int64    `json:"finishedAt"`
	Duration     int64    `json:"duration"`   // 毫秒
	TotalUnits   int      `json:"totalUnits"` // 工作单元数（表 + 对象）
	DoneUnits    int      `json:"doneUnits"`  // 终态时已完成的单元数（失败/取消时用于还原真实进度）
	TotalRows    int64    `json:"totalRows"`
	OutputPath   string   `json:"outputPath,omitempty"`
	FileSize     int64    `json:"fileSize,omitempty"`
	ErrorMsg     string   `json:"errorMsg,omitempty"`
	Logs         []string `json:"logs,omitempty"`   // 执行日志快照（截断保留最近若干条，供终态回放展示）
	Target       string   `json:"target,omitempty"` // 操作目标描述（连接名 + 库表对象），如 "生产库 · db1(93表)"
	Summary      string   `json:"summary"`          // 如 "3表, 40000行, 15.3MB"
}

// ConnRecord 连接配置存储记录：ID（xid）为主键，Name 仅为展示名（可改可重名）；
// ShortName 为命令行简写（唯一，不可重名），用于快速引用连接
type ConnRecord struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	ShortName string     `json:"shortName,omitempty"` // 命令行简写（如 prod/my-dev），可选；不填时用 Name 匹配
	Env       string     `json:"env,omitempty"`        // dev/test/staging/prod，留空视为 prod
	Conn      DBConnInfo `json:"conn"`
}

// ConnInfo 连接列表展示信息
type ConnInfo struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	ShortName string     `json:"shortName,omitempty"`
	Env       string     `json:"env,omitempty"`
	Conn      DBConnInfo `json:"conn"`
	SubTypes  []string   `json:"subTypes"` // 该类型可用的子类型（兼容数据库产品）
}
