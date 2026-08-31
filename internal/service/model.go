// Package service 业务服务层：模型定义 + 持久化 + 任务调度 + CLI。
// 对外通过 Web 模式（Gin 托管前端 SPA）与 CLI 模式提供能力。
package service

import (
	"github.com/fj1981/dqex/internal/store"
)

// ---- 模型别名（统一定义在 store 包，避免循环依赖；service 通过别名复用） ----

type (
	// DBConnInfo 数据库连接信息（含子类型）
	DBConnInfo = store.DBConnInfo
	// TableCondition 表级过滤条件
	TableCondition = store.TableCondition
	// ExportOptions 导出选项
	ExportOptions = store.ExportOptions
	// ImportOptions 导入选项
	ImportOptions = store.ImportOptions
	// Contributor 业务对象贡献者（代理层扩展点）
	Contributor = store.Contributor
	// ContributorRequest 贡献者导出回调请求
	ContributorRequest = store.ContributorRequest
	// ContributorResult 贡献者导出回调结果
	ContributorResult = store.ContributorResult
	// ContributorImportRequest 贡献者导入回调请求
	ContributorImportRequest = store.ContributorImportRequest
	// DataPreparer 数据前置处理器（代理层，按目标库名注册）
	DataPreparer = store.DataPreparer
	// DataPrepareRequest 数据前置处理请求
	DataPrepareRequest = store.DataPrepareRequest
	// DataPackage 数据交换包（dqex 数据格式契约）
	DataPackage = store.DataPackage
	// DataEntry 数据包条目
	DataEntry = store.DataEntry
	// QueryHooks SQL 审计钩子（逐语句回调）
	QueryHooks = store.QueryHooks
	// ExportFormat 导出产物格式
	ExportFormat = store.ExportFormat
	// MigrateOptions 迁移选项
	MigrateOptions = store.MigrateOptions
	// CompareOptions 对比选项
	CompareOptions = store.CompareOptions
	// DictionaryOptions 数据字典导出选项
	DictionaryOptions = store.DictionaryOptions
	// TableAlias 表别名配对
	TableAlias = store.TableAlias
	// CompareResult 对比结果
	CompareResult = store.CompareResult
	// ResetMode 重置数据模式
	ResetMode = store.ResetMode
	// ProgressInfo 任务进度信息
	ProgressInfo = store.ProgressInfo
	// ProgressFunc 进度回调
	ProgressFunc = store.ProgressFunc
	// Snapshot 快照完整数据
	Snapshot = store.Snapshot
	// SnapshotInfo 快照摘要
	SnapshotInfo = store.SnapshotInfo
	// SnapshotTable 单表快照
	SnapshotTable = store.SnapshotTable
	// CreateSnapshotOptions 创建快照选项
	CreateSnapshotOptions = store.CreateSnapshotOptions
	// SnapshotCompareOptions 快照对比选项
	SnapshotCompareOptions = store.SnapshotCompareOptions
	// TaskConfig 任务配置
	TaskConfig = store.TaskConfig
	// ExecutionRecord 执行历史记录
	ExecutionRecord = store.ExecutionRecord
	// ConnRecord 连接配置存储记录
	ConnRecord = store.ConnRecord
	// ConnInfo 连接列表展示信息
	ConnInfo = store.ConnInfo
	// SQLHistoryItem 查询终端历史记录
	SQLHistoryItem = store.SQLHistoryItem
	// SQLFavorite 收藏的 SQL
	SQLFavorite = store.SQLFavorite
	// SQLAuditEntry 审计日志条目
	SQLAuditEntry = store.SQLAuditEntry
	// WebAccessInfo Web 访问凭证
	WebAccessInfo = store.WebAccessInfo
	// WorkspaceTab 查询工作区 tab
	WorkspaceTab = store.WorkspaceTab
	// WorkspaceState 查询工作区状态
	WorkspaceState = store.WorkspaceState
	// TabSettings 工作区标签页设置
	TabSettings = store.TabSettings
	// AISessionRecord AI 会话持久化记录
	AISessionRecord = store.AISessionRecord
)

// 重置模式常量
const (
	ResetNone     = store.ResetNone
	ResetTruncate = store.ResetTruncate
	ResetDrop     = store.ResetDrop
	// FormatSQL / FormatJSON 导出产物格式（ExportOptions.Format）
	FormatSQL  = store.FormatSQL
	FormatJSON = store.FormatJSON
)

// SupportedDBTypes 支持的数据库类型及子类型。
// SubType 不代表版本号，而是标识“使用该类型兼容模式的具体数据库产品”（彼此存在语法差异）：
// 首项为原生标准库（值等于类型名），其余为兼容库产品。
var SupportedDBTypes = store.SupportedDBTypes
