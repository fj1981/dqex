package store

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/rs/xid"

	"github.com/fj1981/dqex/internal/engine"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/def"
	"github.com/fj1981/infrakit/pkg/cylog"

	// 注册 SQLite 方言（GetSqlDialect 依赖 init 注册）与 sqlx 驱动
	_ "github.com/fj1981/infrakit/pkg/cydb/dialect/sqlite"
)

// 表名常量（行模型见 models.go）。
const (
	tableConn      = "conn_record"
	tableTask      = "task_record"
	tableHistory   = "history_record"
	tableSQLHist   = "sql_history"
	tableSQLAudit  = "sql_audit"
	tableWebAcc    = "web_access"
	tableWorkspace = "workspace"
	tableAISession = "ai_session"
	tableSQLFav    = "sql_favorites"
	tableSnapshot  = "snapshot_index"
	tableMeta      = "meta"
)

// 用户域存储键规则（与 service 层 user_scope.go 的 scopeConnID 算法一致）：
//   - local 域（独立部署）：作用域键 = 裸连接 key 原样，user 列 = local；
//   - 其他用户：作用域键 = user + userScopeSep + connID（user 经 NormalizeUser 不含
//     分隔符），user 列存原始用户域。
//
// 五张隔离表（sql_history/sql_audit/ai_session/workspace/sql_favorites）统一三列同写：
//   - user：所属用户域（NormalizeUser 后）；
//   - conn_key：原始连接 key（裸，宿主连接标识），按连接维度跨用户查询/级联删除；
//   - conn_id（即 scope_key 语义，物理列名保留见 models.go）：作用域存储键，同用户×同
//     连接唯一定位；workspace 表以它为主键。
//
// 读路径三分法：精确用户域定位 → user + conn_key 双条件；仅按用户域（connID 空）→
// EQ(user)；跨用户按连接（级联删除/AllUsers）→ EQ(conn_key) 等值。
const (
	scopeLocalUser = "local"
	userScopeSep   = "\x1f"
)

// scopeConnKey 计算作用域存储键（store 写路径统一入口，算法与 service 层 scopeConnID
// 一致，保证存量数据兼容）：local 域（或空 user/空 connID）= 裸 key 原样；其他用户 =
// user\x1fconnID。user 须为 persist 层 NormalizeUser 后的值（不含 \x1f）。
func scopeConnKey(user, connID string) string {
	if user == "" || user == scopeLocalUser || connID == "" {
		return connID
	}
	return user + userScopeSep + connID
}

// SQLiteStore 基于 cydb 的 SQL 存储实现（名字沿用自首个 SQLite 后端，现支持
// cydb 全部方言：sqlite/mysql/postgresql/oracle，由 OpenSQLite/OpenSQL 选择）。
// 通过 cydb.DBCli 打开连接，复用其跨方言能力（切 MySQL 等仅改连接参数）。
// 所有写/查询均通过 cydb 高级 CRUD 方法（内部 ss 构建器 + named bind），不手写 SQL。
type SQLiteStore struct {
	cli *cydb.DBCli
}

// sqliteDSN 生成 SQLite 连接 DSN：通过 modernc.org/sqlite 的 _pragma 参数为
// 连接池中的每个新连接自动执行对应 PRAGMA。
//   - busy_timeout(5000)：写锁竞争时最多等待 5s 而不是立即报 database is locked。
//     busy_timeout 是连接级设置，必须走 DSN 才能在每次打开连接时生效。
//   - journal_mode(WAL)：写事务不再阻塞其他连接的读（读-写并发友好）。
//   - synchronous(NORMAL)：WAL 模式下兼顾安全与吞吐的推荐级别。
func sqliteDSN(dbPath string) string {
	if strings.Contains(dbPath, "?") {
		return dbPath // 调用方已提供完整 DSN，尊重原值
	}
	return dbPath + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
}

// NewSQLiteStore 打开（或创建）指定路径的 SQLite 库并执行自动迁移。
func NewSQLiteStore(dbPath string) (*SQLiteStore, error) {
	s, err := newSQLStore(&def.DBConnection{Type: "sqlite", Path: sqliteDSN(dbPath)})
	if err != nil {
		return nil, err
	}
	return s.(*SQLiteStore), nil
}

// newSQLStore 按 cydb 连接配置打开存储并执行自动迁移（跨方言统一入口）。
func newSQLStore(conn *def.DBConnection) (Store, error) {
	cli, err := cydb.TryConnect(conn)
	if err != nil {
		return nil, engine.NewMsgErrf(engine.ErrStoreOpen, err)
	}
	s := &SQLiteStore{cli: cli}
	if err := s.Migrate(); err != nil {
		_ = cli.Close()
		return nil, engine.NewMsgErrf(engine.ErrStoreMigrate, err)
	}
	return s, nil
}

// Close 关闭底层数据库连接。
func (s *SQLiteStore) Close() error { return s.cli.Close() }

// Migrate 执行自动迁移（建表/补列），随后执行存量数据幂等回填。
func (s *SQLiteStore) Migrate() error {
	if err := migrateModels(s.cli); err != nil {
		return err
	}
	return s.backfillUsersDefault()
}

// ---- 序列化辅助 ----

func marshal(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func unmarshal(s string, v any) error { return json.Unmarshal([]byte(s), v) }

// newID 生成新的行主键（时间戳 + xid 计数器，避免碰撞）。
func newID(ts int64) string { return fmt.Sprintf("%d-%s", ts, xid.New().String()) }

// ---- 连接配置 ----

// connToRow 领域模型 → 行模型（密码列级加密后序列化）。
func connToRow(rec ConnRecord) (connRow, error) {
	conn := rec.Conn
	// 仅密码字段列级加密，其余明文（可查询）
	if conn.Pw != "" {
		enc, err := encryptString(conn.Pw)
		if err != nil {
			return connRow{}, err
		}
		conn.Pw = enc
	}
	body, err := marshal(conn)
	if err != nil {
		return connRow{}, err
	}
	return connRow{ID: rec.ID, Name: rec.Name, ShortName: rec.ShortName, Env: rec.Env, ConnJSON: body}, nil
}

// rowToConn 行模型 → 领域模型（密码解密）。
func rowToConn(r connRow) (ConnRecord, error) {
	var conn DBConnInfo
	if err := unmarshal(r.ConnJSON, &conn); err != nil {
		return ConnRecord{}, err
	}
	if conn.Pw != "" {
		dec, err := decryptString(conn.Pw)
		if err != nil {
			return ConnRecord{}, err
		}
		conn.Pw = dec
	}
	return ConnRecord{ID: r.ID, Name: r.Name, ShortName: r.ShortName, Env: r.Env, Conn: conn}, nil
}

// SaveConn 保存连接配置：rec.ID 非空则按主键更新，否则生成新 xid。
func (s *SQLiteStore) SaveConn(rec ConnRecord) (ConnRecord, error) {
	if rec.ID == "" {
		rec.ID = xid.New().String()
	}
	row, err := connToRow(rec)
	if err != nil {
		return rec, err
	}
	_, err = s.cli.Replace(tableConn, map[string]any{
		"id":         row.ID,
		"name":       row.Name,
		"short_name": row.ShortName,
		"env":        row.Env,
		"conn_json":  row.ConnJSON,
	})
	if err != nil {
		return rec, err
	}
	return rec, nil
}

// LoadConns 加载全部连接配置（按 ID 索引）。
// 注意：返回空 map 可能意味着"确实无连接"，也可能意味着"读取失败"（如 SQLite 锁竞争）。
// 读取失败不再静默——记录日志，避免上层把"读库失败"误判为"连接配置不存在"。
func (s *SQLiteStore) LoadConns() map[string]ConnRecord {
	rows, err := s.cli.List(tableConn, nil)
	if err != nil {
		cylog.Errorf("加载连接配置失败（注意：这是存储读取错误，不等于连接不存在）: %v", err)
		return map[string]ConnRecord{}
	}
	recs := make(map[string]ConnRecord, len(rows))
	for _, m := range rows {
		r := connRow{
			ID:        str(m["id"]),
			Name:      str(m["name"]),
			ShortName: str(m["short_name"]),
			Env:       str(m["env"]),
			ConnJSON:  str(m["conn_json"]),
		}
		if rec, err := rowToConn(r); err == nil {
			recs[rec.ID] = rec
		} else {
			// 解密失败（如库文件被拷贝到其他机器）：保留连接但密码置空，供用户重填
			cylog.Warnf("连接 %q 密码解密失败，请重新录入密码: %v", r.Name, err)
			recs[r.ID] = ConnRecord{ID: r.ID, Name: r.Name, ShortName: r.ShortName, Env: r.Env}
		}
	}
	return recs
}

// GetConn 按主键 ID 查找；兼容按名称或短名查找。
func (s *SQLiteStore) GetConn(key string) (ConnRecord, bool) {
	conns := s.LoadConns()
	if rec, ok := conns[key]; ok {
		return rec, true
	}
	for _, rec := range conns {
		if rec.Name == key {
			return rec, true
		}
	}
	for _, rec := range conns {
		if rec.ShortName == key {
			return rec, true
		}
	}
	return ConnRecord{}, false
}

// DeleteConn 删除连接（按主键 ID，兼容名称或短名）。
func (s *SQLiteStore) DeleteConn(key string) error {
	conns := s.LoadConns()
	if _, ok := conns[key]; ok {
		_, err := s.cli.Delete(tableConn, map[string]any{"id": key}, cydb.WithWhere(cydb.EQ("id")))
		return err
	}
	for id, rec := range conns {
		if rec.Name == key || rec.ShortName == key {
			_, err := s.cli.Delete(tableConn, map[string]any{"id": id}, cydb.WithWhere(cydb.EQ("id")))
			return err
		}
	}
	return nil
}

// ---- 任务配置 ----

// taskToRow 领域模型 → 行模型。
func taskToRow(t TaskConfig) (taskRow, error) {
	body, err := marshal(t)
	if err != nil {
		return taskRow{}, err
	}
	return taskRow{ID: t.ID, Name: t.Name, Type: t.Type, IsLastUsed: t.IsLastUsed, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, BodyJSON: body}, nil
}

func rowToTask(r taskRow) (TaskConfig, error) {
	var t TaskConfig
	if err := unmarshal(r.BodyJSON, &t); err != nil {
		return TaskConfig{}, err
	}
	return t, nil
}

// SaveTask 保存任务配置（按 ID 更新或新增）。
func (s *SQLiteStore) SaveTask(task TaskConfig) error {
	row, err := taskToRow(task)
	if err != nil {
		return err
	}
	_, err = s.cli.Replace(tableTask, map[string]any{
		"id":           row.ID,
		"name":         row.Name,
		"type":         row.Type,
		"is_last_used": row.IsLastUsed,
		"created_at":   row.CreatedAt,
		"updated_at":   row.UpdatedAt,
		"body_json":    row.BodyJSON,
	})
	return err
}

// LoadTasks 加载全部任务配置（按 updated_at 降序）。
func (s *SQLiteStore) LoadTasks() []TaskConfig {
	rows, err := s.cli.List(tableTask, nil, cydb.WithOrderByDesc("updated_at"))
	if err != nil {
		return []TaskConfig{}
	}
	tasks := make([]TaskConfig, 0, len(rows))
	for _, m := range rows {
		r := taskRow{
			ID:         str(m["id"]),
			Name:       str(m["name"]),
			Type:       str(m["type"]),
			IsLastUsed: boolean(m["is_last_used"]),
			CreatedAt:  integer(m["created_at"]),
			UpdatedAt:  integer(m["updated_at"]),
			BodyJSON:   str(m["body_json"]),
		}
		if t, err := rowToTask(r); err == nil {
			tasks = append(tasks, t)
		}
	}
	return tasks
}

// GetTask 获取指定任务配置。
func (s *SQLiteStore) GetTask(id string) (TaskConfig, bool) {
	m, err := s.cli.First(tableTask, map[string]any{"id": id}, cydb.WithWhere(cydb.EQ("id")))
	if err != nil || m == nil {
		return TaskConfig{}, false
	}
	r := taskRow{
		ID:         str(m["id"]),
		Name:       str(m["name"]),
		Type:       str(m["type"]),
		IsLastUsed: boolean(m["is_last_used"]),
		CreatedAt:  integer(m["created_at"]),
		UpdatedAt:  integer(m["updated_at"]),
		BodyJSON:   str(m["body_json"]),
	}
	t, err := rowToTask(r)
	if err != nil {
		return TaskConfig{}, false
	}
	return t, true
}

// DeleteTask 删除任务配置。
func (s *SQLiteStore) DeleteTask(taskID string) error {
	_, err := s.cli.Delete(tableTask, map[string]any{"id": taskID}, cydb.WithWhere(cydb.EQ("id")))
	return err
}

// MarkLastUsed 标记指定类型为最近使用（同类型其他任务取消标记）。
func (s *SQLiteStore) MarkLastUsed(taskID, taskType string) error {
	tasks := s.LoadTasks()
	for i := range tasks {
		if tasks[i].Type == taskType {
			tasks[i].IsLastUsed = tasks[i].ID == taskID
			if err := s.SaveTask(tasks[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// GetLastUsed 获取指定类型最近使用的任务配置。
func (s *SQLiteStore) GetLastUsed(taskType string) *TaskConfig {
	tasks := s.LoadTasks()
	var latest *TaskConfig
	for i := range tasks {
		if tasks[i].Type != taskType {
			continue
		}
		if tasks[i].IsLastUsed {
			return &tasks[i]
		}
		if latest == nil || tasks[i].UpdatedAt > latest.UpdatedAt {
			latest = &tasks[i]
		}
	}
	return latest
}

// ---- 执行历史 ----

// historyToRow 领域模型 → 行模型。
func historyToRow(r ExecutionRecord) (historyRow, error) {
	body, err := marshal(r)
	if err != nil {
		return historyRow{}, err
	}
	return historyRow{ID: r.ID, TaskType: r.TaskType, TaskConfigID: r.TaskConfigID, Status: r.Status, StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, BodyJSON: body}, nil
}

func rowToHistory(r historyRow) (ExecutionRecord, error) {
	var rec ExecutionRecord
	if err := unmarshal(r.BodyJSON, &rec); err != nil {
		return ExecutionRecord{}, err
	}
	return rec, nil
}

// SaveHistory 保存执行历史（按 ID 更新或新增，超出上限裁剪最旧记录）。
func (s *SQLiteStore) SaveHistory(record ExecutionRecord) error {
	row, err := historyToRow(record)
	if err != nil {
		return err
	}
	_, err = s.cli.Replace(tableHistory, map[string]any{
		"id":             row.ID,
		"task_type":      row.TaskType,
		"task_config_id": row.TaskConfigID,
		"status":         row.Status,
		"started_at":     row.StartedAt,
		"finished_at":    row.FinishedAt,
		"body_json":      row.BodyJSON,
	})
	if err != nil {
		return err
	}
	return s.trimHistory()
}

// trimHistory 裁剪执行历史，仅保留最近 maxHistoryRecords 条。
// 优化：定位"第 maxHistoryRecords 条"的 started_at 边界，用时间条件一次性删除更旧记录，
// 避免逐条删除（超限的最旧记录删除后，产物清理由 service 层 RemoveArtifact 负责）。
func (s *SQLiteStore) trimHistory() error {
	// 查第 maxHistoryRecords 条（保留边界）的 started_at
	rows, err := s.cli.List(tableHistory, nil,
		cydb.WithOrderByDesc("started_at"),
		cydb.WithLimit(1),
		cydb.WithOffset(maxHistoryRecords-1),
	)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil // 未超限
	}
	boundary := integer(rows[0]["started_at"])
	// 删除 started_at 严格小于边界（即第 N 条之后更旧）的记录
	_, err = s.cli.Delete(tableHistory,
		map[string]any{"started_at": boundary},
		cydb.WithWhere(cydb.LT("started_at")),
	)
	return err
}

// LoadHistory 加载执行历史（taskType 为空=全部，taskConfigID 为空=不过滤）。
func (s *SQLiteStore) LoadHistory(taskType, taskConfigID string) []ExecutionRecord {
	// 按 started_at 降序全量加载后内存过滤（数据量小，无需 SQL 复杂过滤）
	rows, err := s.cli.List(tableHistory, nil, cydb.WithOrderByDesc("started_at"))
	if err != nil {
		return []ExecutionRecord{}
	}
	ret := make([]ExecutionRecord, 0, len(rows))
	for _, m := range rows {
		r := historyRow{
			ID:           str(m["id"]),
			TaskType:     str(m["task_type"]),
			TaskConfigID: str(m["task_config_id"]),
			Status:       str(m["status"]),
			StartedAt:    integer(m["started_at"]),
			FinishedAt:   integer(m["finished_at"]),
			BodyJSON:     str(m["body_json"]),
		}
		if taskType != "" && r.TaskType != taskType {
			continue
		}
		if taskConfigID != "" && r.TaskConfigID != taskConfigID {
			continue
		}
		if rec, err := rowToHistory(r); err == nil {
			ret = append(ret, rec)
		}
	}
	return ret
}

// GetHistory 获取指定执行记录。
func (s *SQLiteStore) GetHistory(id string) (ExecutionRecord, error) {
	m, err := s.cli.First(tableHistory, map[string]any{"id": id}, cydb.WithWhere(cydb.EQ("id")))
	if err != nil || m == nil {
		return ExecutionRecord{}, engine.NewMsgErr(engine.ErrStoreRecordNotFound, id)
	}
	r := historyRow{
		ID:           str(m["id"]),
		TaskType:     str(m["task_type"]),
		TaskConfigID: str(m["task_config_id"]),
		Status:       str(m["status"]),
		StartedAt:    integer(m["started_at"]),
		FinishedAt:   integer(m["finished_at"]),
		BodyJSON:     str(m["body_json"]),
	}
	return rowToHistory(r)
}

// DeleteHistory 删除指定执行记录。
func (s *SQLiteStore) DeleteHistory(id string) error {
	_, err := s.cli.Delete(tableHistory, map[string]any{"id": id}, cydb.WithWhere(cydb.EQ("id")))
	return err
}

// ---- Web 访问凭证 ----

// SaveWebAccess 保存 Web 访问凭证（单行，主键固定）。
func (s *SQLiteStore) SaveWebAccess(info WebAccessInfo) error {
	_, err := s.cli.Replace(tableWebAcc, map[string]any{
		"addr":      info.Addr,
		"token":     info.Token,
		"issued_at": info.IssuedAt,
	})
	return err
}

// LoadWebAccess 读取 Web 访问凭证；无有效内容时 ok=false。
func (s *SQLiteStore) LoadWebAccess() (WebAccessInfo, bool) {
	rows, err := s.cli.List(tableWebAcc, nil)
	if err != nil || len(rows) == 0 {
		return WebAccessInfo{}, false
	}
	m := rows[0]
	info := WebAccessInfo{Addr: str(m["addr"]), Token: str(m["token"]), IssuedAt: integer(m["issued_at"])}
	if info.Addr == "" && info.Token == "" {
		return WebAccessInfo{}, false
	}
	return info, true
}

// ---- 查询工作区 ----

// userConnWhere 构建按用户域过滤的查询条件（params 为对应的命名绑定参数）：
//   - connID（裸连接 key）非空：user + conn_key 双条件并存——user 列保证按用户隔离，
//     conn_key 条件精确定位该连接（作用域唯一性由写路径 conn_id=scopeConnKey 保证）；
//   - connID 为空：仅按 user 列过滤（返回该用户域全部连接的数据）。
func userConnWhere(user, connID string) (map[string]any, []cydb.Where) {
	params := map[string]any{"user": user}
	conds := []cydb.Where{cydb.EQ("user")}
	if connID != "" {
		params["conn_key"] = connID
		conds = append(conds, cydb.EQ("conn_key"))
	}
	return params, conds
}

// SaveWorkspace 保存某用户域下某连接的工作区（整体覆盖）。
// connID 为裸连接 key；三列同写：conn_id 物理列=作用域键（scopeConnKey，主键）、
// conn_key=裸 key（跨用户级联删除定位）、user=所属用户域。
func (s *SQLiteStore) SaveWorkspace(user, connID string, state WorkspaceState) error {
	tabsJSON, err := marshal(state.Tabs)
	if err != nil {
		return err
	}
	// Replace 按主键（conn_id）upsert：不存在则插入，存在则整行覆盖——
	// 本方法语义即"整体覆盖"，所有列都在 data 中显式给出，不会残留旧列值。
	_, err = s.cli.Replace(tableWorkspace, map[string]any{
		"conn_id":    scopeConnKey(user, connID),
		"user":       user,
		"conn_key":   connID,
		"tabs_json":  tabsJSON,
		"active_id":  state.ActiveID,
		"updated_at": time.Now().UnixMilli(),
	})
	return err
}

// LoadWorkspace 读取某用户域下某连接的工作区；无记录时 ok=false。
// connID 为裸连接 key，按 user 列 + conn_key 双条件过滤。
func (s *SQLiteStore) LoadWorkspace(user, connID string) (WorkspaceState, bool) {
	m, err := s.cli.First(tableWorkspace,
		map[string]any{"user": user, "conn_key": connID},
		cydb.WithWhereAnd(cydb.EQ("user"), cydb.EQ("conn_key")),
	)
	if err != nil || m == nil {
		return WorkspaceState{}, false
	}
	var tabs []WorkspaceTab
	if raw := str(m["tabs_json"]); raw != "" {
		if err := unmarshal(raw, &tabs); err != nil {
			return WorkspaceState{}, false
		}
	}
	return WorkspaceState{Tabs: tabs, ActiveID: str(m["active_id"])}, true
}

// DeleteWorkspace 删除某用户域下某连接的工作区（按 user 列 + conn_key 双条件）。
func (s *SQLiteStore) DeleteWorkspace(user, connID string) error {
	_, err := s.cli.Delete(tableWorkspace,
		map[string]any{"user": user, "conn_key": connID},
		cydb.WithWhereAnd(cydb.EQ("user"), cydb.EQ("conn_key")),
	)
	return err
}

// DeleteWorkspacesByConnAllUsers 跨用户域删除某连接的全部工作区（连接删除级联清理）。
// connID 为裸连接 key，按 conn_key 列等值一次删除所有用户域的行。
func (s *SQLiteStore) DeleteWorkspacesByConnAllUsers(connID string) error {
	_, err := s.cli.Delete(tableWorkspace,
		map[string]any{"conn_key": connID},
		cydb.WithWhere(cydb.EQ("conn_key")),
	)
	return err
}

// ---- SQL 执行历史 ----

// AddSQLHistory 追加一条 SQL 执行历史（每连接环形保留最近 N 条）。
// item.ConnID 为裸连接 key；三列同写：user=所属用户域、conn_key=裸 key、
// conn_id 物理列=作用域键（scopeConnKey）。
func (s *SQLiteStore) AddSQLHistory(user string, item SQLHistoryItem) error {
	if item.ID == "" {
		item.ID = newID(item.CreatedAt)
	}
	body, err := marshal(item)
	if err != nil {
		return err
	}
	_, err = s.cli.Replace(tableSQLHist, map[string]any{
		"id":         item.ID,
		"user":       user,
		"conn_id":    scopeConnKey(user, item.ConnID),
		"conn_key":   item.ConnID,
		"created_at": item.CreatedAt,
		"body_json":  body,
	})
	if err != nil {
		return err
	}
	return s.trimSQLHistory(user, item.ConnID)
}

// trimSQLHistory 裁剪某用户域下某连接的 SQL 历史，仅保留最近 maxSQLHistoryPerConn 条。
// 优化：定位"第 maxSQLHistoryPerConn 条"的 created_at 边界，用时间条件一次性删除更旧记录。
func (s *SQLiteStore) trimSQLHistory(user, connID string) error {
	// 查该用户域该连接第 maxSQLHistoryPerConn 条（保留边界）的 created_at
	rows, err := s.cli.List(tableSQLHist,
		map[string]any{"user": user, "conn_key": connID},
		cydb.WithWhereAnd(cydb.EQ("user"), cydb.EQ("conn_key")),
		cydb.WithOrderByDesc("created_at"),
		cydb.WithLimit(1),
		cydb.WithOffset(maxSQLHistoryPerConn-1),
	)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil // 未超限
	}
	boundary := integer(rows[0]["created_at"])
	// 删除该连接 created_at 严格小于边界（即第 N 条之后更旧）的记录
	_, err = s.cli.Delete(tableSQLHist,
		map[string]any{"user": user, "conn_key": connID, "created_at": boundary},
		cydb.WithWhereAnd(cydb.EQ("user"), cydb.EQ("conn_key"), cydb.LT("created_at")),
	)
	return err
}

// ListSQLHistory 返回某用户域下某连接的历史（新→旧）。
// user 为所属用户域；connID 为裸连接 key，为空时返回该用户域全部连接的历史
// （仅按 user 列过滤，跨用户隔离由 user 列保证）。
func (s *SQLiteStore) ListSQLHistory(user, connID string) ([]SQLHistoryItem, error) {
	params, conds := userConnWhere(user, connID)
	rows, err := s.cli.List(tableSQLHist, params,
		cydb.WithWhereAnd(conds...),
		cydb.WithOrderByDesc("created_at"),
		cydb.WithLimit(maxSQLHistoryPerConn),
	)
	if err != nil {
		return nil, err
	}
	ret := make([]SQLHistoryItem, 0, len(rows))
	for _, m := range rows {
		var item SQLHistoryItem
		if err := unmarshal(str(m["body_json"]), &item); err == nil {
			// 裸连接 key 以 conn_key 列为准（存量 body_json 内可能为 scoped 旧值）
			item.ConnID = str(m["conn_key"])
			ret = append(ret, item)
		}
	}
	return ret, nil
}

// ClearSQLHistory 清空某用户域下某连接的历史；connID 为空时清空该用户域全部连接的历史。
func (s *SQLiteStore) ClearSQLHistory(user, connID string) error {
	params, conds := userConnWhere(user, connID)
	rows, err := s.cli.List(tableSQLHist, params, cydb.WithWhereAnd(conds...))
	if err != nil {
		return err
	}
	for _, m := range rows {
		if id := str(m["id"]); id != "" {
			_, _ = s.cli.Delete(tableSQLHist, map[string]any{"id": id}, cydb.WithWhere(cydb.EQ("id")))
		}
	}
	return nil
}

// ---- SQL 收藏（按用户域隔离；conn_id/db 仅作来源标记，用于跨连接回填提示） ----

// AddFavorite 新增一条收藏（user 为所属用户域；f.ConnID 为裸连接 key，三列同写）。
func (s *SQLiteStore) AddFavorite(user string, f *SQLFavorite) error {
	if f.ID == "" {
		f.ID = newID(f.CreatedAt)
	}
	body, err := marshal(f)
	if err != nil {
		return err
	}
	_, err = s.cli.Replace(tableSQLFav, map[string]any{
		"id":         f.ID,
		"user":       user,
		"conn_id":    scopeConnKey(user, f.ConnID),
		"conn_key":   f.ConnID,
		"title":      f.Title,
		"created_at": f.CreatedAt,
		"body_json":  body,
	})
	return err
}

// ListFavorites 返回该用户域的全部收藏（新→旧）。
// user 列经迁移回填后恒非空，仅按 user 列过滤（跨用户隔离由 user 列保证）。
func (s *SQLiteStore) ListFavorites(user string) ([]*SQLFavorite, error) {
	rows, err := s.cli.List(tableSQLFav,
		map[string]any{"user": user},
		cydb.WithWhere(cydb.EQ("user")),
		cydb.WithOrderByDesc("created_at"),
	)
	if err != nil {
		return nil, err
	}
	ret := make([]*SQLFavorite, 0, len(rows))
	for _, m := range rows {
		var f SQLFavorite
		if err := unmarshal(str(m["body_json"]), &f); err == nil {
			// 裸连接 key 以 conn_key 列为准（存量行 conn_id/body_json 可能为 scoped 旧值）
			f.ConnID = str(m["conn_key"])
			ret = append(ret, &f)
		}
	}
	return ret, nil
}

// DeleteFavorite 删除收藏（按 id 定位，仅限本用户域）。
func (s *SQLiteStore) DeleteFavorite(user, id string) error {
	_, err := s.cli.Delete(tableSQLFav,
		map[string]any{"id": id, "user": user},
		cydb.WithWhereAnd(cydb.EQ("id"), cydb.EQ("user")),
	)
	return err
}

// RenameFavorite 重命名收藏（按 id 定位，仅限本用户域）。
func (s *SQLiteStore) RenameFavorite(user, id, title string) error {
	rows, err := s.cli.List(tableSQLFav,
		map[string]any{"id": id, "user": user},
		cydb.WithWhereAnd(cydb.EQ("id"), cydb.EQ("user")),
	)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return fmt.Errorf("favorite not found")
	}
	body := str(rows[0]["body_json"])
	var f SQLFavorite
	if err := unmarshal(body, &f); err != nil {
		return err
	}
	f.Title = title
	body, err = marshal(f)
	if err != nil {
		return err
	}
	// 仅 UPDATE title 与 body_json 两列，不回写连接相关列（conn_id/conn_key）：
	// body_json 内可能为 scoped 旧值或空，整行 Replace 会把存量行的连接列写坏。
	// id 为主键列经 FilterColumns 自动从 SET 中剔除，仅作 WHERE 定位参数；
	// user 列 SET 为同值回写（归属不变），主要供 WHERE 定位参数使用。
	_, err = s.cli.Update(tableSQLFav, map[string]any{
		"id":        id,
		"user":      user,
		"title":     title,
		"body_json": body,
	}, cydb.WithWhereAnd(cydb.EQ("id"), cydb.EQ("user")))
	return err
}

// backfillUsersDefault 幂等回填五张隔离表的存量数据（Go 逐行修复，方言安全）。
// 存量行仅 conn_id 列有值（scoped 存储键或裸 key），conn_key 为本次改造新增列（空），
// 以 conn_key 为空判定未回填，按同一规则归一：
//   - conn_id 含分隔符 \x1f（scoped 形态）：user=前缀（用户域）、conn_key=后缀（裸 key），
//     conn_id 原值保留（即作用域键语义）；
//   - conn_id 不含分隔符（裸 key 形态，含历史 local 行与 favorites 裸 key 行）：
//     conn_key=conn_id，user 为空时归属 local 域。
//
// conn_id（作用域键）恒保持原值不动（workspace 表主键，避免改键），回填仅补 user/conn_key；
// 回填后 conn_key 恒非空，重复执行幂等。
func (s *SQLiteStore) backfillUsersDefault() error {
	for _, table := range []string{tableSQLHist, tableSQLAudit, tableAISession, tableWorkspace, tableSQLFav} {
		rows, err := s.cli.List(table, nil)
		if err != nil {
			return err
		}
		for _, m := range rows {
			if str(m["conn_key"]) != "" {
				continue // 已回填（幂等）
			}
			stored := str(m["conn_id"])
			if stored == "" {
				continue // 无连接键可归一
			}
			user, raw := str(m["user"]), stored
			if idx := strings.Index(stored, userScopeSep); idx >= 0 {
				user = stored[:idx] // scoped 形态：前缀即用户域
				raw = stored[idx+len(userScopeSep):]
			} else if user == "" {
				user = scopeLocalUser // 裸 key 形态：归属 local 域
			}
			// workspace 表主键为 conn_id（作用域键），其余表主键为 id；主键列经
			// FilterColumns 自动从 SET 中剔除，仅作 WHERE 定位参数
			pkCol, pkVal := "id", str(m["id"])
			if table == tableWorkspace {
				pkCol, pkVal = "conn_id", stored
			}
			data := map[string]any{"user": user, "conn_key": raw, pkCol: pkVal}
			if _, err := s.cli.Update(table, data, cydb.WithWhere(cydb.EQ(pkCol))); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---- SQL 审计（只增不删） ----

// AppendSQLAudit 追加一条 SQL 审计日志（只追加，不提供删除）。
// entry.ConnID 为裸连接 key；三列同写：user=所属用户域、conn_key=裸 key、
// conn_id 物理列=作用域键（scopeConnKey）。
func (s *SQLiteStore) AppendSQLAudit(user string, entry SQLAuditEntry) error {
	if entry.ID == "" {
		entry.ID = newID(entry.CreatedAt)
	}
	body, err := marshal(entry)
	if err != nil {
		return err
	}
	_, err = s.cli.Replace(tableSQLAudit, map[string]any{
		"id":         entry.ID,
		"user":       user,
		"conn_id":    scopeConnKey(user, entry.ConnID),
		"conn_key":   entry.ConnID,
		"created_at": entry.CreatedAt,
		"body_json":  body,
	})
	return err
}

// normAuditLimit 规范审计查询分页参数：limit<=0 默认 100，上限 500；offset 非负。
func normAuditLimit(limit, offset int) (int, int) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

// auditEntryList 行集合 → 审计条目列表（裸连接 key 以 conn_key 列为准）。
func auditEntryList(rows []map[string]any) []SQLAuditEntry {
	ret := make([]SQLAuditEntry, 0, len(rows))
	for _, m := range rows {
		var entry SQLAuditEntry
		if err := unmarshal(str(m["body_json"]), &entry); err == nil {
			// 裸连接 key 以 conn_key 列为准（存量 body_json 内可能为 scoped 旧值）
			entry.ConnID = str(m["conn_key"])
			ret = append(ret, entry)
		}
	}
	return ret
}

// ListSQLAudit 读取审计日志（倒序，分页）。user 为所属用户域；connID 为裸连接 key
// （非空时按 user 列 + conn_key 双条件过滤），为空时仅按 user 列过滤（返回该用户域
// 全部连接，跨用户隔离由 user 列保证）。
func (s *SQLiteStore) ListSQLAudit(user, connID string, limit, offset int) ([]SQLAuditEntry, error) {
	limit, offset = normAuditLimit(limit, offset)
	params, conds := userConnWhere(user, connID)
	rows, err := s.cli.List(tableSQLAudit, params,
		cydb.WithWhereAnd(conds...),
		cydb.WithOrderByDesc("created_at"),
		cydb.WithLimit(limit),
		cydb.WithOffset(offset),
	)
	if err != nil {
		return nil, err
	}
	return auditEntryList(rows), nil
}

// ---- AI 会话（对话历史，按连接持久化） ----

// aiSessionToRow 领域模型 → 行模型（消息与 usage 分别 JSON 序列化）。
// rec.ConnID 为裸连接 key；三列同写：user=所属用户域、conn_key=裸 key、
// conn_id 物理列=作用域键（scopeConnKey）。
func aiSessionToRow(user string, rec AISessionRecord) (aiSessionRow, error) {
	msgs, err := marshal(rec.Messages)
	if err != nil {
		return aiSessionRow{}, err
	}
	usage, err := marshal(rec.Usage)
	if err != nil {
		return aiSessionRow{}, err
	}
	return aiSessionRow{
		ID:           rec.ID,
		User:         user,
		ScopeKey:     scopeConnKey(user, rec.ConnID),
		ConnKey:      rec.ConnID,
		TabID:        rec.TabID,
		DB:           rec.DB,
		Dialect:      rec.Dialect,
		Lang:         rec.Lang,
		MessagesJSON: msgs,
		UsageJSON:    usage,
		CreatedAt:    rec.CreatedAt,
		UpdatedAt:    rec.UpdatedAt,
	}, nil
}

// rowToAISession 行模型 → 领域模型（rec.ConnID 还原为裸连接 key）。
func rowToAISession(r aiSessionRow) AISessionRecord {
	rec := AISessionRecord{
		ID:        r.ID,
		ConnID:    r.ConnKey,
		TabID:     r.TabID,
		DB:        r.DB,
		Dialect:   r.Dialect,
		Lang:      r.Lang,
		CreatedAt: r.CreatedAt,
		UpdatedAt: r.UpdatedAt,
	}
	if r.MessagesJSON != "" {
		var msgs []any
		if err := unmarshal(r.MessagesJSON, &msgs); err == nil {
			rec.Messages = msgs
		}
	}
	if r.UsageJSON != "" {
		var usage any
		if err := unmarshal(r.UsageJSON, &usage); err == nil {
			rec.Usage = usage
		}
	}
	return rec
}

// SaveAISession 保存/更新一个 AI 会话（整组消息覆盖写）。
// rec.ConnID 为裸连接 key；user 为所属用户域（三列同写）。
func (s *SQLiteStore) SaveAISession(user string, rec AISessionRecord) error {
	row, err := aiSessionToRow(user, rec)
	if err != nil {
		return err
	}
	_, err = s.cli.Replace(tableAISession, map[string]any{
		"id":            row.ID,
		"user":          row.User,
		"conn_id":       row.ScopeKey,
		"conn_key":      row.ConnKey,
		"tab_id":        row.TabID,
		"db":            row.DB,
		"dialect":       row.Dialect,
		"lang":          row.Lang,
		"messages_json": row.MessagesJSON,
		"usage_json":    row.UsageJSON,
		"created_at":    row.CreatedAt,
		"updated_at":    row.UpdatedAt,
	})
	return err
}

// LoadAISession 读取指定会话（user 为所属用户域，按 user 列做归属校验，
// 不属于该用户域视为不存在）；无记录时 ok=false。rec.ConnID 还原为裸连接 key。
func (s *SQLiteStore) LoadAISession(user, sessionID string) (AISessionRecord, bool) {
	m, err := s.cli.First(tableAISession, map[string]any{"id": sessionID}, cydb.WithWhere(cydb.EQ("id")))
	if err != nil || m == nil {
		return AISessionRecord{}, false
	}
	r := aiSessionRow{
		ID:           str(m["id"]),
		User:         str(m["user"]),
		ScopeKey:     str(m["conn_id"]),
		ConnKey:      str(m["conn_key"]),
		TabID:        str(m["tab_id"]),
		DB:           str(m["db"]),
		Dialect:      str(m["dialect"]),
		Lang:         str(m["lang"]),
		MessagesJSON: str(m["messages_json"]),
		UsageJSON:    str(m["usage_json"]),
		CreatedAt:    integer(m["created_at"]),
		UpdatedAt:    integer(m["updated_at"]),
	}
	if r.User != user {
		return AISessionRecord{}, false // 归属校验：跨用户域不可见
	}
	return rowToAISession(r), true
}

// AISessionExists 判断会话 ID 是否已落盘（跨用户域存在性判定，不含归属校验）。
// 供透明重建复用原 ID 前区分「彻底不存在」与「存在但归属其他用户域」。
func (s *SQLiteStore) AISessionExists(sessionID string) bool {
	m, err := s.cli.First(tableAISession, map[string]any{"id": sessionID}, cydb.WithWhere(cydb.EQ("id")))
	return err == nil && m != nil
}

// ListAISessions 列出某用户域下某连接（可选指定 tab）的会话（新→旧，仅元信息不含消息）。
// connID 为裸连接 key；tabID 为空 = 返回该连接全部会话，非空 = 仅返回该 tab
// 的会话（按 tab 隔离）。查询条件恒含 user 列 + conn_key（跨用户隔离由 user 列保证）。
func (s *SQLiteStore) ListAISessions(user, connID, tabID string) ([]AISessionRecord, error) {
	if tabID != "" {
		rows, err := s.cli.List(tableAISession,
			map[string]any{"user": user, "conn_key": connID, "tab_id": tabID},
			cydb.WithWhereAnd(cydb.EQ("user"), cydb.EQ("conn_key"), cydb.EQ("tab_id")),
			cydb.WithOrderByDesc("updated_at"),
		)
		if err != nil {
			return nil, err
		}
		return sessionMetaList(rows), nil
	}
	rows, err := s.cli.List(tableAISession,
		map[string]any{"user": user, "conn_key": connID},
		cydb.WithWhereAnd(cydb.EQ("user"), cydb.EQ("conn_key")),
		cydb.WithOrderByDesc("updated_at"),
	)
	if err != nil {
		return nil, err
	}
	return sessionMetaList(rows), nil
}

// sessionMetaList 行集合 → 会话元信息列表（不含消息体；裸连接 key 以 conn_key 列为准）。
func sessionMetaList(rows []map[string]any) []AISessionRecord {
	ret := make([]AISessionRecord, 0, len(rows))
	for _, m := range rows {
		ret = append(ret, AISessionRecord{
			ID:        str(m["id"]),
			ConnID:    str(m["conn_key"]),
			TabID:     str(m["tab_id"]),
			DB:        str(m["db"]),
			CreatedAt: integer(m["created_at"]),
			UpdatedAt: integer(m["updated_at"]),
		})
	}
	return ret
}

// DeleteAISession 删除指定会话（user 为所属用户域，按 user 列 + 主键双条件删除，
// 跨用户域删不到他人会话）。
func (s *SQLiteStore) DeleteAISession(user, sessionID string) error {
	_, err := s.cli.Delete(tableAISession,
		map[string]any{"id": sessionID, "user": user},
		cydb.WithWhereAnd(cydb.EQ("id"), cydb.EQ("user")),
	)
	return err
}

// DeleteAISessionByTab 删除某用户域下某连接指定 tab 的会话（tab 关闭时调用）。
// connID 为裸连接 key，按 user 列 + conn_key 双条件删除。
func (s *SQLiteStore) DeleteAISessionByTab(user, connID, tabID string) error {
	_, err := s.cli.Delete(tableAISession,
		map[string]any{"user": user, "conn_key": connID, "tab_id": tabID},
		cydb.WithWhereAnd(cydb.EQ("user"), cydb.EQ("conn_key"), cydb.EQ("tab_id")),
	)
	return err
}

// DeleteAISessionsByConn 删除某用户域下某连接的全部会话（按 user 列 + conn_key 双条件）。
func (s *SQLiteStore) DeleteAISessionsByConn(user, connID string) error {
	_, err := s.cli.Delete(tableAISession,
		map[string]any{"user": user, "conn_key": connID},
		cydb.WithWhereAnd(cydb.EQ("user"), cydb.EQ("conn_key")),
	)
	return err
}

// DeleteAISessionsByConnAllUsers 跨用户域删除某连接的全部会话（连接删除级联清理）。
// connID 为裸连接 key，按 conn_key 列等值一次删除所有用户域的会话。
func (s *SQLiteStore) DeleteAISessionsByConnAllUsers(connID string) error {
	_, err := s.cli.Delete(tableAISession,
		map[string]any{"conn_key": connID},
		cydb.WithWhere(cydb.EQ("conn_key")),
	)
	return err
}

// PurgeExcessAISessions 清理超额会话：
//   - 当某连接的会话数 > maxPerConn 时，删除其中「超过 keepDays 天未活动」的会话；
//   - 从最旧（updated_at 最小）的会话开始删，直到会话数 ≤ maxPerConn 或没有可删的超期会话；
//   - 会话数 ≤ maxPerConn 或未超期（tab 未关闭）时永久保留，不做时间过期清理。
func (s *SQLiteStore) PurgeExcessAISessions(maxPerConn int, keepDays int) (int64, error) {
	if maxPerConn <= 0 || keepDays <= 0 {
		return 0, nil
	}
	cutoff := time.Now().AddDate(0, 0, -keepDays).UnixMilli()

	// 全量加载会话（数据量小），按「用户域×连接」分组统计（conn_key 为裸 key，
	// 需与 user 组合定位，保持每用户每连接的配额语义）
	rows, err := s.cli.List(tableAISession, nil)
	if err != nil {
		return 0, err
	}
	type rec struct {
		id        string
		connKey   string // 用户域×连接组合键（user + sep + conn_key）
		updatedAt int64
	}
	recs := make([]rec, 0, len(rows))
	for _, m := range rows {
		recs = append(recs, rec{
			id:        str(m["id"]),
			connKey:   str(m["user"]) + userScopeSep + str(m["conn_key"]),
			updatedAt: integer(m["updated_at"]),
		})
	}

	// 按连接分组计数
	counts := map[string]int{}
	for _, r := range recs {
		counts[r.connKey]++
	}

	var n int64
	// 找出超额连接，收集其「超期」会话，按最旧优先删除
	for connKey, cnt := range counts {
		if cnt <= maxPerConn {
			continue
		}
		excess := cnt - maxPerConn
		// 该连接下超期的会话（updated_at < cutoff），按最旧排序
		var stale []rec
		for _, r := range recs {
			if r.connKey == connKey && r.updatedAt < cutoff {
				stale = append(stale, r)
			}
		}
		// 按 updated_at 升序（最旧在前）
		sort.Slice(stale, func(i, j int) bool { return stale[i].updatedAt < stale[j].updatedAt })
		// 最多删 excess 条（超期会话可能不足 excess，此时保留部分超额，等后续超期再清）
		toDelete := excess
		if len(stale) < toDelete {
			toDelete = len(stale)
		}
		for i := 0; i < toDelete; i++ {
			if _, derr := s.cli.Delete(tableAISession, map[string]any{"id": stale[i].id}, cydb.WithWhere(cydb.EQ("id"))); derr == nil {
				n++
			}
		}
	}
	return n, nil
}

// ---- 快照索引（仅索引元数据；快照内容仍为 OSS 对象/本地文件） ----

// snapshotToRow 领域模型 → 行模型（body_json 存完整 SnapshotInfo）。
func snapshotToRow(info SnapshotInfo) (snapshotRow, error) {
	body, err := marshal(info)
	if err != nil {
		return snapshotRow{}, err
	}
	return snapshotRow{ID: info.ID, ConnID: info.ConnID, ConnLabel: info.ConnLabel, CreatedAt: info.CreatedAt, BodyJSON: body}, nil
}

func rowToSnapshot(r snapshotRow) (SnapshotInfo, error) {
	var info SnapshotInfo
	if err := unmarshal(r.BodyJSON, &info); err != nil {
		return SnapshotInfo{}, err
	}
	// 真实列兜底：body_json 缺失/损坏时至少保留可查询字段（正常路径 JSON 内已含同值）
	if info.ID == "" {
		info.ID = r.ID
	}
	if info.ConnID == "" {
		info.ConnID = r.ConnID
	}
	if info.ConnLabel == "" {
		info.ConnLabel = r.ConnLabel
	}
	if info.CreatedAt == 0 {
		info.CreatedAt = r.CreatedAt
	}
	return info, nil
}

// UpsertSnapshot 写入/更新快照索引行（按 info.ID 主键幂等）。
func (s *SQLiteStore) UpsertSnapshot(info SnapshotInfo) error {
	if info.ID == "" {
		return fmt.Errorf("快照索引行缺少 ID")
	}
	row, err := snapshotToRow(info)
	if err != nil {
		return err
	}
	_, err = s.cli.Replace(tableSnapshot, map[string]any{
		"id":         row.ID,
		"conn_id":    row.ConnID,
		"conn_label": row.ConnLabel,
		"created_at": row.CreatedAt,
		"body_json":  row.BodyJSON,
	})
	return err
}

// ListSnapshots 列出快照索引（created_at 倒序）；connID 非空时按 conn_id 等值过滤。
func (s *SQLiteStore) ListSnapshots(connID string) ([]SnapshotInfo, error) {
	opts := []def.QueryOption{cydb.WithOrderByDesc("created_at")}
	if connID != "" {
		opts = append(opts, cydb.WithWhere(cydb.EQ("conn_id")))
	}
	args := map[string]any{}
	if connID != "" {
		args["conn_id"] = connID
	}
	rows, err := s.cli.List(tableSnapshot, args, opts...)
	if err != nil {
		return nil, err
	}
	infos := make([]SnapshotInfo, 0, len(rows))
	for _, m := range rows {
		r := snapshotRow{
			ID:        str(m["id"]),
			ConnID:    str(m["conn_id"]),
			ConnLabel: str(m["conn_label"]),
			CreatedAt: integer(m["created_at"]),
			BodyJSON:  str(m["body_json"]),
		}
		if info, err := rowToSnapshot(r); err == nil {
			infos = append(infos, info)
		} else {
			cylog.Warnf("快照索引行 %s 解析失败，已跳过: %v", r.ID, err)
		}
	}
	return infos, nil
}

// DeleteSnapshot 删除快照索引行（按主键 ID）。
func (s *SQLiteStore) DeleteSnapshot(id string) error {
	_, err := s.cli.Delete(tableSnapshot, map[string]any{"id": id}, cydb.WithWhere(cydb.EQ("id")))
	return err
}

// ---- 通用 KV 元信息 ----

// GetMeta 读取 KV 元信息；无记录时 ok=false。
func (s *SQLiteStore) GetMeta(key string) (string, bool, error) {
	m, err := s.cli.First(tableMeta, map[string]any{"meta_key": key}, cydb.WithWhere(cydb.EQ("meta_key")))
	if err != nil {
		return "", false, err
	}
	if m == nil {
		return "", false, nil
	}
	return str(m["meta_value"]), true, nil
}

// SetMeta 写入/更新 KV 元信息（按 key 幂等）。
func (s *SQLiteStore) SetMeta(key, value string) error {
	_, err := s.cli.Replace(tableMeta, map[string]any{"meta_key": key, "meta_value": value})
	return err
}

// ---- 类型转换辅助 ----

func str(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", t)
	}
}

func boolean(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case int64:
		return t != 0
	case int:
		return t != 0
	default:
		return false
	}
}

func integer(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	default:
		return 0
	}
}
