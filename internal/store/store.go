package store

import (
	"path/filepath"

	"github.com/fj1981/infrakit/pkg/cydb/def"
)

// OpenSQLite 打开（或创建）SQLite 存储并执行自动迁移，返回 Store 接口。
// dbPath 为 SQLite 数据库文件路径。
func OpenSQLite(dbPath string) (Store, error) {
	return newSQLStore(&def.DBConnection{Type: "sqlite", Path: sqliteDSN(dbPath)})
}

// OpenSQL 按连接配置打开 SQL 存储并执行自动迁移（库模式 WithStoreConn 用）。
// 经 cydb 跨方言能力支持 sqlite/mysql/postgresql/oracle；宿主可复用自己的
// MySQL 实例（独立 database，表自动迁移创建），元数据不落本地文件。
func OpenSQL(conn *def.DBConnection) (Store, error) {
	return newSQLStore(conn)
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
	// user 为所属用户域；item.ConnID 为裸连接 key（三列同写：user/conn_key/conn_id 作用域键）。
	AddSQLHistory(user string, item SQLHistoryItem) error
	// ListSQLHistory 返回某用户域下某连接的历史（新→旧）。
	// connID 为裸连接 key（user 列 + conn_key 双条件过滤）；为空时仅按 user 列过滤
	// （该用户域全部连接）。
	ListSQLHistory(user, connID string) ([]SQLHistoryItem, error)
	// ClearSQLHistory 清空某用户域下某连接的历史（connID 为空=该用户域全部连接）。
	ClearSQLHistory(user, connID string) error

	// ---- SQL 收藏（按用户域隔离；conn_id/db 仅作来源标记） ----

	// AddFavorite 新增一条收藏（user 为所属用户域；f.ConnID 为裸连接 key，三列同写）。
	AddFavorite(user string, f *SQLFavorite) error
	// ListFavorites 返回该用户域的全部收藏（新→旧）。
	ListFavorites(user string) ([]*SQLFavorite, error)
	// DeleteFavorite 删除收藏（按 id 定位，仅限本用户域）。
	DeleteFavorite(user, id string) error
	// RenameFavorite 重命名收藏（按 id 定位，仅限本用户域）。
	RenameFavorite(user, id, title string) error

	// ---- SQL 审计（只增不删） ----

	// AppendSQLAudit 追加一条 SQL 审计日志（只追加，不提供删除）。
	// user 为所属用户域；entry.ConnID 为裸连接 key（三列同写）。
	AppendSQLAudit(user string, entry SQLAuditEntry) error
	// ListSQLAudit 读取审计日志（倒序，分页）。connID 为裸连接 key（user 列 + conn_key
	// 双条件过滤）；为空时仅按 user 列过滤（该用户域全部连接，跨用户隔离由 user 列保证）。
	ListSQLAudit(user, connID string, limit, offset int) ([]SQLAuditEntry, error)

	// ---- 查询工作区（SQL 终端 Tab 布局，按连接持久化） ----

	// SaveWorkspace 保存某用户域下某连接的工作区（整体覆盖）。
	// connID 为裸连接 key（conn_id 物理列存作用域键作主键，conn_key 存裸 key）；
	// user 为所属用户域。
	SaveWorkspace(user, connID string, state WorkspaceState) error
	// LoadWorkspace 读取某用户域下某连接的工作区（user 列 + conn_key 双条件）；无记录时 ok=false。
	LoadWorkspace(user, connID string) (WorkspaceState, bool)
	// DeleteWorkspace 删除某用户域下某连接的工作区（user 列 + conn_key 双条件）。
	DeleteWorkspace(user, connID string) error
	// DeleteWorkspacesByConnAllUsers 跨用户域删除某连接的全部工作区（连接删除级联清理，
	// connID 为裸连接 key，按 conn_key 列等值删除所有用户域的行）。
	DeleteWorkspacesByConnAllUsers(connID string) error

	// ---- AI 会话（对话历史，按连接持久化） ----

	// SaveAISession 保存/更新一个 AI 会话（整组消息覆盖写）。
	// user 为所属用户域；rec.ConnID 为裸连接 key（三列同写：user/conn_key/conn_id 作用域键）。
	SaveAISession(user string, rec AISessionRecord) error
	// LoadAISession 读取指定会话（user 为所属用户域，按 user 列归属校验，不属于该用户域
	// 视为不存在）；无记录时 ok=false。rec.ConnID 还原为裸连接 key。
	LoadAISession(user, sessionID string) (AISessionRecord, bool)
	// AISessionExists 判断会话 ID 是否已落盘（跨用户域存在性判定，不含归属校验），
	// 供透明重建复用原 ID 前区分「彻底不存在」与「存在但归属其他用户域」。
	AISessionExists(sessionID string) bool
	// ListAISessions 列出某用户域下某连接（可选指定 tab）的会话（新→旧，仅元信息不含
	// 消息，供前端恢复选择）。connID 为裸连接 key，按 user 列 + conn_key 双条件过滤。
	ListAISessions(user, connID, tabID string) ([]AISessionRecord, error)
	// DeleteAISession 删除指定会话（user 为所属用户域，按 user 列 + 主键双条件删除）。
	DeleteAISession(user, sessionID string) error
	// DeleteAISessionByTab 删除某用户域下某连接指定 tab 的会话（tab 关闭时调用）。
	DeleteAISessionByTab(user, connID, tabID string) error
	// DeleteAISessionsByConn 删除某用户域下某连接的全部会话。
	DeleteAISessionsByConn(user, connID string) error
	// DeleteAISessionsByConnAllUsers 跨用户域删除某连接的全部会话（连接删除级联清理，
	// connID 为裸连接 key，按 conn_key 列等值删除所有用户域的会话）。
	DeleteAISessionsByConnAllUsers(connID string) error
	// PurgeExcessAISessions 清理超额会话：当某连接会话数 > maxPerConn 时，
	// 删除其中「超过 keepDays 天未活动」的会话（从最旧开始），返回删除条数。
	PurgeExcessAISessions(maxPerConn int, keepDays int) (int64, error)

	// ---- 快照索引（仅索引元数据；快照内容仍为 OSS 对象/本地文件） ----

	// UpsertSnapshot 写入/更新快照索引行（按 info.ID 主键幂等；存量 JSON 索引迁移与
	// 新建快照共用）。conn_id/created_at 同步写入真实列供 SQL 过滤。
	UpsertSnapshot(info SnapshotInfo) error
	// ListSnapshots 列出快照索引（created_at 倒序）。connID 非空时按 conn_id 列等值过滤
	// （连接 ID 即创建时连接，库模式下虚拟连接为 env:<id>，用于按环境隔离）。
	ListSnapshots(connID string) ([]SnapshotInfo, error)
	// DeleteSnapshot 删除快照索引行（按主键 ID）。
	DeleteSnapshot(id string) error

	// ---- 通用 KV 元信息（迁移标记等进程间共享状态） ----

	// GetMeta 读取 KV 元信息；无记录时 ok=false。
	GetMeta(key string) (string, bool, error)
	// SetMeta 写入/更新 KV 元信息（按 key 幂等）。
	SetMeta(key, value string) error
}

// maxSQLHistoryPerConn 每个连接保留的 SQL 历史条数。
const maxSQLHistoryPerConn = 200

// maxHistoryRecords 执行历史保留上限。
const maxHistoryRecords = 200
