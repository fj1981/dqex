package store

// ---- DB 行模型（cydb tag 定义列，用于 AutoMigrate 自动建表/补列） ----
//
// 约定：
//   - 可查询字段（主键、索引、时间、状态等）用真实列，便于 SQL 过滤；
//   - 大对象字段（完整结构体）序列化为 JSON 存 TEXT 列（BodyJSON / ConnJSON）；
//   - 敏感字段（连接密码）由实现层做列级加密后写入 ConnJSON。
//
// 领域模型（值对象）见 types.go，行模型与值对象通过 JSON 序列化互相转换。

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
	ConnID    string `cydb:"column:conn_id;type:varchar;size:32;index"`
	CreatedAt int64  `cydb:"column:created_at;type:bigint;index"`
	BodyJSON  string `cydb:"column:body_json;type:text"` // 完整 SQLHistoryItem 序列化
}

func (sqlHistoryRow) TableName() string { return tableSQLHist }

// sqlFavoriteRow SQL 收藏行（独立表，不受执行历史环形上限影响）。
type sqlFavoriteRow struct {
	ID        string `cydb:"column:id;type:varchar;size:64;primary_key"`
	ConnID    string `cydb:"column:conn_id;type:varchar;size:32;index"`
	Title     string `cydb:"column:title;type:varchar;size:256"` // 默认取 SQL 去注释后首行前 40 字符
	CreatedAt int64  `cydb:"column:created_at;type:bigint;index"`
	BodyJSON  string `cydb:"column:body_json;type:text"` // 完整 SQLFavorite 序列化
}

func (sqlFavoriteRow) TableName() string { return tableSQLFav }

// sqlAuditRow SQL 审计日志行（只增不删）。
type sqlAuditRow struct {
	ID        string `cydb:"column:id;type:varchar;size:64;primary_key"`
	ConnID    string `cydb:"column:conn_id;type:varchar;size:32;index"`
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

// workspaceRow 查询工作区行（按连接一份；tabs_json 存可重跑上下文，不含结果集）。
type workspaceRow struct {
	ConnID    string `cydb:"column:conn_id;type:varchar;size:32;primary_key"`
	TabsJSON  string `cydb:"column:tabs_json;type:text"` // WorkspaceTab[] 序列化（不含 results/running 等瞬时状态）
	ActiveID  string `cydb:"column:active_id;type:varchar;size:64"`
	UpdatedAt int64  `cydb:"column:updated_at;type:bigint"`
}

func (workspaceRow) TableName() string { return tableWorkspace }

// aiSessionRow AI 会话行（按会话一份；messages_json 存整组对话消息，updated_at 用于过期清理）。
type aiSessionRow struct {
	ID           string `cydb:"column:id;type:varchar;size:64;primary_key"`
	ConnID       string `cydb:"column:conn_id;type:varchar;size:32;index"`
	TabID        string `cydb:"column:tab_id;type:varchar;size:64;index"`
	DB           string `cydb:"column:db;type:varchar;size:128"`
	MessagesJSON string `cydb:"column:messages_json;type:text"` // schema.Message[] 序列化（含 system/user/assistant/tool）
	UsageJSON    string `cydb:"column:usage_json;type:text"`    // llm.Usage 序列化
	CreatedAt    int64  `cydb:"column:created_at;type:bigint"`
	UpdatedAt    int64  `cydb:"column:updated_at;type:bigint;index"`
}

func (aiSessionRow) TableName() string { return tableAISession }

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
	// 未来新表：在此追加 &newRow{}，仅此一处。
}
