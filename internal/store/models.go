package store

// ---- DB 行模型（cydb tag 定义列，用于 AutoMigrate 自动建表/补列） ----
//
// 约定：
//   - 可查询字段（主键、索引、时间、状态等）用真实列，便于 SQL 过滤；
//   - 大对象字段（完整结构体）序列化为 JSON 存 TEXT 列（BodyJSON / ConnJSON）；
//   - 敏感字段（连接密码）由实现层做列级加密后写入 ConnJSON。
//
// 领域模型（值对象）见 types.go，行模型与值对象通过 JSON 序列化互相转换。
//
// ---- 五张隔离表（sql_history/sql_audit/ai_session/workspace/sql_favorites）列语义 ----
//
// 命名决策（迁移安全优先）：ScopeKey 的物理列名保留为 conn_id 不改名。原因：workspace
// 表该列是主键，若按语义改名为 scope_key，需要对存量表 ALTER TABLE ADD COLUMN scope_key
// PRIMARY KEY——SQLite/MySQL/PostgreSQL 均不支持向已有表补加主键列（cydb AutoMigrate 的
// 补列 SQL 也不携带主键约束），跨方言风险高；保留 conn_id 列名 + 新增裸连接列 conn_key
// 属于纯 ADD COLUMN，跨方言安全。因此：
//   - ScopeKey（物理列 conn_id，varchar 190）：作用域存储键（即目标结构中的 scope_key）
//     ——local 域=裸 key 原样；其他用户=user\x1fconn_id（service 层 scopeConnID 结果），
//     同用户×同连接唯一定位，workspace 表继续以它为主键；
//   - ConnKey（物理列 conn_key，varchar 190，索引）：原始连接 key（裸，宿主连接标识）
//     ——按连接维度跨用户查询/级联删除。
// 存量行为（scopeConnID 方案）经 backfillUsersDefault 幂等归一到该结构，见 sqlite.go。

// connRow 连接配置行。
type connRow struct {
	ID        string `cydb:"column:id;type:varchar;size:32;primary_key"`
	Name      string `cydb:"column:name;type:varchar;size:128"`
	ShortName string `cydb:"column:short_name;type:varchar;size:64;index"`
	Env       string `cydb:"column:env;type:varchar;size:16"`
	ConnJSON  string `cydb:"column:conn_json;type:text"` // DBConnInfo 序列化（密码已列级加密）
}

func (connRow) TableName() string { return tableConn }

// taskRow 任务配置行。
type taskRow struct {
	ID         string `cydb:"column:id;type:varchar;size:32;primary_key"`
	Name       string `cydb:"column:name;type:varchar;size:128"`
	Type       string `cydb:"column:type;type:varchar;size:16;index"`
	IsLastUsed bool   `cydb:"column:is_last_used;type:bool"`
	CreatedAt  int64  `cydb:"column:created_at;type:bigint"`
	UpdatedAt  int64  `cydb:"column:updated_at;type:bigint"`
	BodyJSON   string `cydb:"column:body_json;type:text"` // 完整 TaskConfig 序列化
}

func (taskRow) TableName() string { return tableTask }

// historyRow 执行历史行。
type historyRow struct {
	ID           string `cydb:"column:id;type:varchar;size:32;primary_key"`
	TaskType     string `cydb:"column:task_type;type:varchar;size:16;index"`
	TaskConfigID string `cydb:"column:task_config_id;type:varchar;size:32;index"`
	Status       string `cydb:"column:status;type:varchar;size:16"`
	StartedAt    int64  `cydb:"column:started_at;type:bigint;index"`
	FinishedAt   int64  `cydb:"column:finished_at;type:bigint"`
	BodyJSON     string `cydb:"column:body_json;type:text"` // 完整 ExecutionRecord 序列化
}

func (historyRow) TableName() string { return tableHistory }

// sqlHistoryRow SQL 执行历史行。
type sqlHistoryRow struct {
	ID        string `cydb:"column:id;type:varchar;size:64;primary_key"`
	User      string `cydb:"column:user;type:varchar;size:64;index"`      // 所属用户域（local=独立部署）
	ScopeKey  string `cydb:"column:conn_id;type:varchar;size:190;index"`  // 作用域存储键（物理列名 conn_id，见文件头命名决策）
	ConnKey   string `cydb:"column:conn_key;type:varchar;size:190;index"` // 原始连接 key（裸，跨用户按连接查询/级联删除）
	CreatedAt int64  `cydb:"column:created_at;type:bigint;index"`
	BodyJSON  string `cydb:"column:body_json;type:text"` // 完整 SQLHistoryItem 序列化
}

func (sqlHistoryRow) TableName() string { return tableSQLHist }

// sqlFavoriteRow SQL 收藏行（独立表，不受执行历史环形上限影响）。
type sqlFavoriteRow struct {
	ID        string `cydb:"column:id;type:varchar;size:64;primary_key"`
	User      string `cydb:"column:user;type:varchar;size:64;index"`      // 所属用户域（local=独立部署）
	ScopeKey  string `cydb:"column:conn_id;type:varchar;size:190;index"`  // 作用域存储键（物理列名 conn_id，见文件头命名决策）
	ConnKey   string `cydb:"column:conn_key;type:varchar;size:190;index"` // 原始连接 key（裸，跨用户按连接查询/级联删除）
	Title     string `cydb:"column:title;type:varchar;size:256"`          // 默认取 SQL 去注释后首行前 40 字符
	CreatedAt int64  `cydb:"column:created_at;type:bigint;index"`
	BodyJSON  string `cydb:"column:body_json;type:text"` // 完整 SQLFavorite 序列化
}

func (sqlFavoriteRow) TableName() string { return tableSQLFav }

// sqlAuditRow SQL 审计日志行（只增不删）。
type sqlAuditRow struct {
	ID        string `cydb:"column:id;type:varchar;size:64;primary_key"`
	User      string `cydb:"column:user;type:varchar;size:64;index"`      // 所属用户域（local=独立部署）
	ScopeKey  string `cydb:"column:conn_id;type:varchar;size:190;index"`  // 作用域存储键（物理列名 conn_id，见文件头命名决策）
	ConnKey   string `cydb:"column:conn_key;type:varchar;size:190;index"` // 原始连接 key（裸，跨用户按连接查询/级联删除）
	CreatedAt int64  `cydb:"column:created_at;type:bigint;index"`
	BodyJSON  string `cydb:"column:body_json;type:text"` // 完整 SQLAuditEntry 序列化
}

func (sqlAuditRow) TableName() string { return tableSQLAudit }

// webAccessRow Web 访问凭证行（单行，主键固定）。
type webAccessRow struct {
	Addr     string `cydb:"column:addr;type:varchar;size:64;primary_key"`
	Token    string `cydb:"column:token;type:varchar;size:256"`
	IssuedAt int64  `cydb:"column:issued_at;type:bigint"`
}

func (webAccessRow) TableName() string { return tableWebAcc }

// workspaceRow 查询工作区行（按用户域×连接一份；tabs_json 存可重跑上下文，不含结果集）。
type workspaceRow struct {
	ScopeKey  string `cydb:"column:conn_id;type:varchar;size:190;primary_key"` // 作用域存储键（物理列名 conn_id，主键，见文件头命名决策）
	User      string `cydb:"column:user;type:varchar;size:64;index"`           // 所属用户域（local=独立部署）
	ConnKey   string `cydb:"column:conn_key;type:varchar;size:190;index"`      // 原始连接 key（裸，跨用户按连接查询/级联删除）
	TabsJSON  string `cydb:"column:tabs_json;type:text"`                       // WorkspaceTab[] 序列化（不含 results/running 等瞬时状态）
	ActiveID  string `cydb:"column:active_id;type:varchar;size:64"`
	UpdatedAt int64  `cydb:"column:updated_at;type:bigint"`
}

func (workspaceRow) TableName() string { return tableWorkspace }

// aiSessionRow AI 会话行（按会话一份；messages_json 存整组对话消息，updated_at 用于过期清理）。
type aiSessionRow struct {
	ID           string `cydb:"column:id;type:varchar;size:64;primary_key"`
	User         string `cydb:"column:user;type:varchar;size:64;index"`      // 所属用户域（local=独立部署）
	ScopeKey     string `cydb:"column:conn_id;type:varchar;size:190;index"`  // 作用域存储键（物理列名 conn_id，见文件头命名决策）
	ConnKey      string `cydb:"column:conn_key;type:varchar;size:190;index"` // 原始连接 key（裸，跨用户按连接查询/级联删除）
	TabID        string `cydb:"column:tab_id;type:varchar;size:64;index"`
	DB           string `cydb:"column:db;type:varchar;size:128"`
	Dialect      string `cydb:"column:dialect;type:varchar;size:32"` // 方言标签（恢复时沿用）
	Lang         string `cydb:"column:lang;type:varchar;size:16"`    // 会话语言（恢复时沿用，历史会话不回溯）
	MessagesJSON string `cydb:"column:messages_json;type:text"`      // schema.Message[] 序列化（含 system/user/assistant/tool）
	UsageJSON    string `cydb:"column:usage_json;type:text"`         // llm.Usage 序列化
	CreatedAt    int64  `cydb:"column:created_at;type:bigint"`
	UpdatedAt    int64  `cydb:"column:updated_at;type:bigint;index"`
}

func (aiSessionRow) TableName() string { return tableAISession }

// snapshotRow 快照索引行：conn_id/created_at 为可查询真实列，body_json 存完整
// SnapshotInfo（含 db_names/description/table_count 等，JSON 自描述便于演进）。
type snapshotRow struct {
	ID        string `cydb:"column:id;type:varchar;size:64;primary_key"`
	ConnID    string `cydb:"column:conn_id;type:varchar;size:190;index"` // 创建时连接 ID（虚拟连接 env:<id>，按环境过滤主列）
	ConnLabel string `cydb:"column:conn_label;type:varchar;size:190"`    // 创建时连接/环境显示名
	CreatedAt int64  `cydb:"column:created_at;type:bigint;index"`        // Unix 秒
	BodyJSON  string `cydb:"column:body_json;type:text"`                 // 完整 SnapshotInfo 序列化
}

func (snapshotRow) TableName() string { return tableSnapshot }

// metaRow 通用 KV 行（迁移标记等进程间共享的元信息）。
// 列名避开各数据库保留字（key 为 MySQL 保留字、value 为关键字，故用 meta_key/meta_value）。
type metaRow struct {
	MetaKey   string `cydb:"column:meta_key;type:varchar;size:128;primary_key"`
	MetaValue string `cydb:"column:meta_value;type:text"`
}

func (metaRow) TableName() string { return tableMeta }

// allModels 统一迁移注册表：新增表时只需在此切片追加一个行模型结构体，
// Migrate 遍历执行 AutoMigrate，其余代码零改动。
var allModels = []any{
	&connRow{},
	&taskRow{},
	&historyRow{},
	&sqlHistoryRow{},
	&sqlAuditRow{},
	&webAccessRow{},
	&workspaceRow{},
	&aiSessionRow{},
	&sqlFavoriteRow{},
	&snapshotRow{},
	&metaRow{},
	// 未来新表：在此追加 &newRow{}，仅此一处。
}
