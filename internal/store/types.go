// Package store 存储领域：领域模型（值对象）+ 存储接口 + SQLite 实现 + 自动迁移。
//
// 本包是唯一接触数据库（cydb DBCli）的领域，service 等上层只依赖 Store 接口与
// 本包的领域模型（通过类型别名引用）。未来切换 MySQL 等数据库只需修改本包实现。
//
// 分层约定：
//   - types.go   领域模型（值对象，json tag），供业务层与 JSON 序列化使用；
//   - models.go  DB 行模型（cydb tag），用于 AutoMigrate 建表与列映射；
//   - store.go   Store 接口；
//   - sqlite.go  SQLite 实现（cydb DBCli + ss 构建器 + named bind）；
//   - migrate.go 统一迁移入口（遍历 allModels）；
//   - crypto.go  列级敏感字段加密（连接密码）。
package store

import (
	"encoding/json"

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

// SQLHistoryItem 查询终端历史记录（用户手写 SQL，可回填重跑）
type SQLHistoryItem struct {
	ID        string `json:"id"`
	ConnID    string `json:"connId"`
	DB        string `json:"db,omitempty"`   // 执行时目标库名（空=连接默认库）
	Mode      string `json:"mode,omitempty"` // 执行模式 transform/raw
	SQL       string `json:"sql"`
	IsWrite   bool   `json:"isWrite"`
	RowCount  int    `json:"rowCount"`
	Elapsed   int64  `json:"elapsedMs"`
	Status    string `json:"status"` // ok / error
	ErrorMsg  string `json:"error,omitempty"`
	CreatedAt int64  `json:"createdAt"` // Unix 毫秒
}

// SQLAuditEntry 审计日志条目（安全兜底，全量、只追加、不可删）。
// 与 SQLHistoryItem 解耦：审计记所有真实执行（含对象树自动查询、单元格编辑），
// 字段完整可追溯，含目标库、执行模式、来源、以及单元格编辑的结构化参数。
type SQLAuditEntry struct {
	ID        string `json:"id"`
	ConnID    string `json:"connId"`
	DB        string `json:"db,omitempty"`   // 执行时目标库名
	Mode      string `json:"mode,omitempty"` // 执行模式 transform/raw
	Source    string `json:"source"`         // manual=用户手写 / tree=对象树自动查询 / cell=单元格内联编辑
	SQL       string `json:"sql"`            // 实际执行的 SQL（cell 类型下为摘要）
	IsWrite   bool   `json:"isWrite"`
	RowCount  int    `json:"rowCount"`
	Elapsed   int64  `json:"elapsedMs"`
	Status    string `json:"status"` // ok / error
	ErrorMsg  string `json:"error,omitempty"`
	CreatedAt int64  `json:"createdAt"` // Unix 毫秒

	// 单元格内联编辑的结构化参数（source=cell 时才有值）
	Table     string   `json:"table,omitempty"`
	Column    string   `json:"column,omitempty"`
	NewValue  any      `json:"newValue,omitempty"`
	PKColumns []string `json:"pkColumns,omitempty"`
	PKValues  []any    `json:"pkValues,omitempty"`
}

// WebAccessInfo Web 访问凭证：持久化后重启可复用（未过期时），dbx url 随时可取
type WebAccessInfo struct {
	Addr     string `json:"addr"`               // 监听地址 host:port
	Token    string `json:"token"`              // 访问令牌（空=启动时禁用了认证）
	IssuedAt int64  `json:"issuedAt,omitempty"` // 令牌签发时间（Unix 毫秒），用于过期判断
}

// ---- 查询工作区（SQL 终端 Tab 布局，按连接持久化，可重跑上下文，不含结果集） ----

// WorkspaceTab 工作区 tab。仅持久化「可重跑」的上下文字段（结果集靠重新执行恢复）。
// QueryKind/ObjectKind 用 kind 判别；具体字段见 QueryTab / ObjectTab。
type WorkspaceTab struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"` // "query" | "object"
	Seq   int    `json:"seq,omitempty"`   // 查询序号（query tab 专用）
	Title string `json:"title,omitempty"` // 展示标题（query tab 重命名后）
	DB    string `json:"db,omitempty"`    // 目标库名（空 = 连接默认库）
	SQL   string `json:"sql,omitempty"`   // 查询 SQL（query tab 专用）
	Mode  string `json:"mode,omitempty"`  // 执行模式 transform/raw（query tab 专用）
	// object tab 专用
	Name    string `json:"name,omitempty"`
	ObjType string `json:"objType,omitempty"` // table / view / function / procedure
	SubTab  string `json:"subTab,omitempty"`  // data / struct / ddl
	// 表浏览视图布局（过滤/排序/列显隐/页大小），原样 JSON 存取，后端不解析语义
	ViewLayout json.RawMessage `json:"viewLayout,omitempty"`
}

// WorkspaceState 某连接的工作区状态。
type WorkspaceState struct {
	Tabs    []WorkspaceTab `json:"tabs"`
	ActiveID string        `json:"activeId"`
}
