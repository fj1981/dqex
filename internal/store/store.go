package store

import "path/filepath"

// OpenSQLite 打开（或创建）SQLite 存储并执行自动迁移，返回 Store 接口。
// dbPath 为 SQLite 数据库文件路径。
func OpenSQLite(dbPath string) (Store, error) {
	return NewSQLiteStore(dbPath)
}

// DBFileName SQLite 数据库文件名（位于数据根目录）。
const DBFileName = "dqex.db"

// DefaultDBPath 返回数据根目录下的默认 SQLite 库路径。
func DefaultDBPath(baseDir string) string {
	return filepath.Join(baseDir, DBFileName)
}

// Store 存储接口：连接配置 + 任务配置 + 执行历史 + SQL 历史 + SQL 审计 + Web 凭证。
//
// 方法签名与原 PersistMgr 公开方法保持一致，service 层委托调用零改动。
// SQLite 是第一个实现；未来切换 MySQL 等数据库只需新增实现并在工厂处选择，
// 上层（service/web/cli）无感知。
type Store interface {
	// Close 关闭底层数据库连接。
	Close() error

	// Migrate 执行自动迁移（建表/补列），基于 allModels 注册表。
	Migrate() error

	// ---- 连接配置 ----

	// SaveConn 保存连接配置：rec.ID 非空则按主键更新，否则生成新 ID。
	SaveConn(rec ConnRecord) (ConnRecord, error)
	// LoadConns 加载全部连接配置（按 ID 索引）。
	LoadConns() map[string]ConnRecord
	// GetConn 按主键 ID 查找；兼容按名称或短名查找。
	GetConn(key string) (ConnRecord, bool)
	// DeleteConn 删除连接（按主键 ID，兼容名称或短名）。
	DeleteConn(key string) error

	// ---- 任务配置 ----

	// SaveTask 保存任务配置（按 ID 更新或新增）。
	SaveTask(task TaskConfig) error
	// LoadTasks 加载全部任务配置。
	LoadTasks() []TaskConfig
	// GetTask 获取指定任务配置。
	GetTask(id string) (TaskConfig, bool)
	// DeleteTask 删除任务配置。
	DeleteTask(taskID string) error
	// MarkLastUsed 标记指定类型为最近使用（同类型其他任务取消标记）。
	MarkLastUsed(taskID, taskType string) error
	// GetLastUsed 获取指定类型最近使用的任务配置。
	GetLastUsed(taskType string) *TaskConfig

	// ---- 执行历史 ----

	// SaveHistory 保存执行历史（按 ID 更新或新增，超出上限裁剪最旧记录）。
	SaveHistory(record ExecutionRecord) error
	// LoadHistory 加载执行历史（taskType 为空=全部，taskConfigID 为空=不过滤）。
	LoadHistory(taskType, taskConfigID string) []ExecutionRecord
	// GetHistory 获取指定执行记录。
	GetHistory(id string) (ExecutionRecord, error)
	// DeleteHistory 删除指定执行记录。
	DeleteHistory(id string) error

	// ---- Web 访问凭证 ----

	// SaveWebAccess 保存 Web 访问凭证。
	SaveWebAccess(info WebAccessInfo) error
	// LoadWebAccess 读取 Web 访问凭证；无有效内容时 ok=false。
	LoadWebAccess() (WebAccessInfo, bool)

	// ---- SQL 执行历史 ----

	// AddSQLHistory 追加一条 SQL 执行历史（每连接环形保留最近 N 条）。
	AddSQLHistory(item SQLHistoryItem) error
	// ListSQLHistory 返回某连接的历史（新→旧）。
	ListSQLHistory(connID string) ([]SQLHistoryItem, error)
	// ClearSQLHistory 清空某连接的历史。
	ClearSQLHistory(connID string) error

	// ---- SQL 收藏（全局共享，conn_id/db 仅作来源标记） ----

	// AddFavorite 新增一条收藏。
	AddFavorite(f *SQLFavorite) error
	// ListFavorites 返回全部收藏（全局共享，不按连接隔离；新→旧）。
	ListFavorites() ([]*SQLFavorite, error)
	// DeleteFavorite 删除收藏（按全局唯一 id 定位；无 conn_id 隔离，跨连接可见）。
	DeleteFavorite(id string) error
	// RenameFavorite 重命名收藏（按全局唯一 id 定位）。
	RenameFavorite(id, title string) error

	// ---- SQL 审计（只增不删） ----

	// AppendSQLAudit 追加一条 SQL 审计日志（只追加，不提供删除）。
	AppendSQLAudit(entry SQLAuditEntry) error
	// ListSQLAudit 读取审计日志（倒序，分页）。connID 为空返回全部连接。
	ListSQLAudit(connID string, limit, offset int) ([]SQLAuditEntry, error)

	// ---- 查询工作区（SQL 终端 Tab 布局，按连接持久化） ----

	// SaveWorkspace 保存某连接的工作区（整体覆盖）。
	SaveWorkspace(connID string, state WorkspaceState) error
	// LoadWorkspace 读取某连接的工作区；无记录时 ok=false。
	LoadWorkspace(connID string) (WorkspaceState, bool)
	// DeleteWorkspace 删除某连接的工作区。
	DeleteWorkspace(connID string) error

	// ---- AI 会话（对话历史，按连接持久化） ----

	// SaveAISession 保存/更新一个 AI 会话（整组消息覆盖写）。
	SaveAISession(rec AISessionRecord) error
	// LoadAISession 读取指定会话；无记录时 ok=false。
	LoadAISession(sessionID string) (AISessionRecord, bool)
	// ListAISessions 列出某连接（可选指定 tab）的会话（新→旧，仅元信息不含消息，供前端恢复选择）。
	ListAISessions(connID, tabID string) ([]AISessionRecord, error)
	// DeleteAISession 删除指定会话。
	DeleteAISession(sessionID string) error
	// DeleteAISessionByTab 删除某连接下指定 tab 的会话（tab 关闭时调用）。
	DeleteAISessionByTab(connID, tabID string) error
	// DeleteAISessionsByConn 删除某连接的全部会话。
	DeleteAISessionsByConn(connID string) error
	// PurgeExcessAISessions 清理超额会话：当某连接会话数 > maxPerConn 时，
	// 删除其中「超过 keepDays 天未活动」的会话（从最旧开始），返回删除条数。
	PurgeExcessAISessions(maxPerConn int, keepDays int) (int64, error)
}

// maxSQLHistoryPerConn 每个连接保留的 SQL 历史条数。
const maxSQLHistoryPerConn = 200

// maxHistoryRecords 执行历史保留上限。
const maxHistoryRecords = 200
