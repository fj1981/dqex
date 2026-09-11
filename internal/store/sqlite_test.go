package store

import (
	"path/filepath"
	"testing"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/def"
)

// newTestStore 创建临时 SQLite 存储（每个测试独立临时目录）。
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dir := t.TempDir()
	s, err := NewSQLiteStore(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("打开测试存储失败: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestConnRoundTrip 验证连接配置的保存/加载/密码加解密往返。
func TestConnRoundTrip(t *testing.T) {
	s := newTestStore(t)

	rec := ConnRecord{
		Name:      "prod",
		ShortName: "p",
		Env:       "prod",
		Conn: DBConnInfo{DBConnection: def.DBConnection{
			Type: "mysql", Host: "127.0.0.1", Port: 3306, Un: "root", Pw: "s3cret!", DBName: "app",
		}},
	}
	saved, err := s.SaveConn(rec)
	if err != nil {
		t.Fatalf("SaveConn 失败: %v", err)
	}
	if saved.ID == "" {
		t.Fatal("SaveConn 未生成 ID")
	}

	// 按 ID 查找
	got, ok := s.GetConn(saved.ID)
	if !ok {
		t.Fatal("GetConn 按 ID 未找到")
	}
	if got.Conn.Pw != "s3cret!" {
		t.Fatalf("密码加解密往返失败，期望 s3cret!，实际 %q", got.Conn.Pw)
	}
	if got.Name != "prod" || got.ShortName != "p" {
		t.Fatalf("连接字段不匹配: %+v", got)
	}

	// 按短名查找
	if _, ok := s.GetConn("p"); !ok {
		t.Fatal("GetConn 按短名未找到")
	}

	// 更新
	got.Name = "prod2"
	if _, err := s.SaveConn(got); err != nil {
		t.Fatalf("更新 SaveConn 失败: %v", err)
	}
	conns := s.LoadConns()
	if len(conns) != 1 {
		t.Fatalf("更新后连接数应为 1，实际 %d", len(conns))
	}
	if conns[saved.ID].Name != "prod2" {
		t.Fatalf("更新未生效: %+v", conns[saved.ID])
	}

	// 删除
	if err := s.DeleteConn(saved.ID); err != nil {
		t.Fatalf("DeleteConn 失败: %v", err)
	}
	if len(s.LoadConns()) != 0 {
		t.Fatal("删除后连接数应为 0")
	}
}

// TestTaskRoundTrip 验证任务配置保存/加载。
func TestTaskRoundTrip(t *testing.T) {
	s := newTestStore(t)

	task := TaskConfig{ID: "t1", Name: "导出", Type: "export", CreatedAt: 1, UpdatedAt: 2}
	if err := s.SaveTask(task); err != nil {
		t.Fatalf("SaveTask 失败: %v", err)
	}

	got, ok := s.GetTask("t1")
	if !ok {
		t.Fatal("GetTask 未找到")
	}
	if got.Name != "导出" || got.Type != "export" {
		t.Fatalf("任务字段不匹配: %+v", got)
	}

	if err := s.DeleteTask("t1"); err != nil {
		t.Fatalf("DeleteTask 失败: %v", err)
	}
	if _, ok := s.GetTask("t1"); ok {
		t.Fatal("删除后仍能找到任务")
	}
}

// TestSQLHistoryTrim 验证 SQL 历史环形裁剪（每连接最多 200 条）。
func TestSQLHistoryTrim(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < maxSQLHistoryPerConn+10; i++ {
		item := SQLHistoryItem{ConnID: "c1", SQL: "select 1", CreatedAt: int64(i)}
		if err := s.AddSQLHistory("local", item); err != nil {
			t.Fatalf("AddSQLHistory 失败: %v", err)
		}
	}
	items, err := s.ListSQLHistory("local", "c1")
	if err != nil {
		t.Fatalf("ListSQLHistory 失败: %v", err)
	}
	if len(items) != maxSQLHistoryPerConn {
		t.Fatalf("历史条数应为 %d，实际 %d", maxSQLHistoryPerConn, len(items))
	}
	// 应保留最新的（CreatedAt 最大的）
	if items[0].CreatedAt != int64(maxSQLHistoryPerConn+10-1) {
		t.Fatalf("最新一条 CreatedAt 应为 %d，实际 %d", maxSQLHistoryPerConn+9, items[0].CreatedAt)
	}

	// 清空
	if err := s.ClearSQLHistory("local", "c1"); err != nil {
		t.Fatalf("ClearSQLHistory 失败: %v", err)
	}
	items, _ = s.ListSQLHistory("local", "c1")
	if len(items) != 0 {
		t.Fatal("清空后历史应为空")
	}
}

// TestSQLAuditAppendAndList 验证审计只增、倒序分页。
func TestSQLAuditAppendAndList(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 10; i++ {
		entry := SQLAuditEntry{ConnID: "c1", SQL: "select 1", CreatedAt: int64(i), Source: "manual"}
		if err := s.AppendSQLAudit("local", entry); err != nil {
			t.Fatalf("AppendSQLAudit 失败: %v", err)
		}
	}

	// 倒序（新→旧）
	entries, err := s.ListSQLAudit("local", "c1", 5, 0)
	if err != nil {
		t.Fatalf("ListSQLAudit 失败: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("分页 limit=5 应返回 5 条，实际 %d", len(entries))
	}
	if entries[0].CreatedAt != 9 {
		t.Fatalf("倒序首条应为最新 CreatedAt=9，实际 %d", entries[0].CreatedAt)
	}

	// 第二页
	entries2, err := s.ListSQLAudit("local", "c1", 5, 5)
	if err != nil {
		t.Fatalf("ListSQLAudit 第二页失败: %v", err)
	}
	if len(entries2) != 5 {
		t.Fatalf("第二页应返回 5 条，实际 %d", len(entries2))
	}
	if entries2[0].CreatedAt != 4 {
		t.Fatalf("第二页首条应为 CreatedAt=4，实际 %d", entries2[0].CreatedAt)
	}
}

// TestWorkspaceRoundTrip 验证查询工作区保存/加载/删除。
func TestWorkspaceRoundTrip(t *testing.T) {
	s := newTestStore(t)

	if _, ok := s.LoadWorkspace("local", "c1"); ok {
		t.Fatal("初始不应有工作区")
	}

	state := WorkspaceState{
		Tabs: []WorkspaceTab{
			{ID: "q1", Kind: "query", Seq: 1, DB: "app", SQL: "select 1", Mode: "transform"},
			{ID: "o1", Kind: "object", DB: "app", Name: "users", ObjType: "table", SubTab: "data"},
		},
		ActiveID: "q1",
	}
	if err := s.SaveWorkspace("local", "c1", state); err != nil {
		t.Fatalf("SaveWorkspace 失败: %v", err)
	}

	got, ok := s.LoadWorkspace("local", "c1")
	if !ok {
		t.Fatal("LoadWorkspace 未找到")
	}
	if len(got.Tabs) != 2 {
		t.Fatalf("tabs 数量应为 2，实际 %d", len(got.Tabs))
	}
	if got.ActiveID != "q1" {
		t.Fatalf("activeId 应为 q1，实际 %q", got.ActiveID)
	}
	// 验证 query tab 字段完整
	q := got.Tabs[0]
	if q.SQL != "select 1" || q.Mode != "transform" || q.Seq != 1 {
		t.Fatalf("query tab 字段不匹配: %+v", q)
	}
	// 验证 object tab 字段完整
	o := got.Tabs[1]
	if o.Name != "users" || o.ObjType != "table" || o.SubTab != "data" {
		t.Fatalf("object tab 字段不匹配: %+v", o)
	}

	// 覆盖更新
	if err := s.SaveWorkspace("local", "c1", WorkspaceState{Tabs: []WorkspaceTab{}, ActiveID: ""}); err != nil {
		t.Fatalf("覆盖 SaveWorkspace 失败: %v", err)
	}
	got2, _ := s.LoadWorkspace("local", "c1")
	if len(got2.Tabs) != 0 {
		t.Fatalf("覆盖后 tabs 应为空，实际 %d", len(got2.Tabs))
	}

	// 删除
	if err := s.DeleteWorkspace("local", "c1"); err != nil {
		t.Fatalf("DeleteWorkspace 失败: %v", err)
	}
	if _, ok := s.LoadWorkspace("local", "c1"); ok {
		t.Fatal("删除后仍能找到工作区")
	}
}

// TestWebAccessRoundTrip 验证 Web 凭证保存/加载。
func TestWebAccessRoundTrip(t *testing.T) {
	s := newTestStore(t)

	if _, ok := s.LoadWebAccess(); ok {
		t.Fatal("初始不应有 Web 凭证")
	}
	info := WebAccessInfo{Addr: "127.0.0.1:8080", Token: "tok", IssuedAt: 123}
	if err := s.SaveWebAccess(info); err != nil {
		t.Fatalf("SaveWebAccess 失败: %v", err)
	}
	got, ok := s.LoadWebAccess()
	if !ok {
		t.Fatal("LoadWebAccess 未找到")
	}
	if got.Addr != "127.0.0.1:8080" || got.Token != "tok" {
		t.Fatalf("Web 凭证不匹配: %+v", got)
	}
}

// TestUserColumnIsolation 验证五张隔离表 user/conn_key/conn_id(作用域键) 三列的
// 写入与读路径三分法：
//  1. 写路径三列同写：user=用户域、conn_key=裸连接 key、conn_id=作用域键（local=裸 key，
//     其他用户=user\x1fconn）；
//  2. 精确用户域定位按 user 列 + conn_key 双条件过滤，跨用户域互不可见（同裸连接 key）；
//  3. connID 为空的分支仅按 user 列过滤（该用户域全部连接）；
//  4. AI 会话归属校验：LoadAISession/DeleteAISession 按 user 列隔离；
//  5. 存量回填三种形态（scoped/user、裸/local、裸+已有 user、workspace 主键形态）幂等；
//  6. 跨用户级联删除按裸 conn_key 等值一次删除所有用户域的行。
func TestUserColumnIsolation(t *testing.T) {
	s := newTestStore(t)

	must := func(err error, what string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s 失败: %v", what, err)
		}
	}
	// local 与 alice 使用同一裸连接 key c1（跨用户同连接场景）
	must(s.AddSQLHistory("local", SQLHistoryItem{ID: "h-local", ConnID: "c1", SQL: "select 1", CreatedAt: 1}), "AddSQLHistory local")
	must(s.AddSQLHistory("alice", SQLHistoryItem{ID: "h-alice", ConnID: "c1", SQL: "select 2", CreatedAt: 2}), "AddSQLHistory alice")
	must(s.AppendSQLAudit("local", SQLAuditEntry{ID: "a-local", ConnID: "c1", SQL: "select 1", CreatedAt: 1}), "AppendSQLAudit local")
	must(s.AppendSQLAudit("alice", SQLAuditEntry{ID: "a-alice", ConnID: "c1", SQL: "select 2", CreatedAt: 2}), "AppendSQLAudit alice")
	must(s.SaveWorkspace("local", "c1", WorkspaceState{ActiveID: "q1"}), "SaveWorkspace local")
	must(s.SaveWorkspace("alice", "c1", WorkspaceState{ActiveID: "q2"}), "SaveWorkspace alice")
	must(s.SaveAISession("local", AISessionRecord{ID: "s-local", ConnID: "c1", UpdatedAt: 1}), "SaveAISession local")
	must(s.SaveAISession("alice", AISessionRecord{ID: "s-alice", ConnID: "c1", UpdatedAt: 2}), "SaveAISession alice")
	must(s.AddFavorite("local", &SQLFavorite{ID: "f-local", ConnID: "c1", SQL: "select 1", CreatedAt: 1}), "AddFavorite local")
	must(s.AddFavorite("alice", &SQLFavorite{ID: "f-alice", ConnID: "c1", SQL: "select 2", CreatedAt: 2}), "AddFavorite alice")

	// ---- 1) 三列同写：直查原始行确认 user/conn_key/conn_id(作用域键) 列值 ----
	rawRow := func(table, pkCol, pkVal string) map[string]any {
		t.Helper()
		m, err := s.cli.First(table, map[string]any{pkCol: pkVal}, cydb.WithWhere(cydb.EQ(pkCol)))
		if err != nil || m == nil {
			t.Fatalf("查询 %s(%s=%s) 失败: %v", table, pkCol, pkVal, err)
		}
		return m
	}
	for _, tc := range []struct{ table, pkCol, id, wantScope string }{
		{tableSQLHist, "id", "h-local", "c1"},
		{tableSQLHist, "id", "h-alice", "alice\x1fc1"},
		{tableSQLAudit, "id", "a-local", "c1"},
		{tableSQLAudit, "id", "a-alice", "alice\x1fc1"},
		{tableWorkspace, "conn_id", "c1", "c1"},
		{tableWorkspace, "conn_id", "alice\x1fc1", "alice\x1fc1"},
		{tableAISession, "id", "s-local", "c1"},
		{tableAISession, "id", "s-alice", "alice\x1fc1"},
		{tableSQLFav, "id", "f-local", "c1"},
		{tableSQLFav, "id", "f-alice", "alice\x1fc1"},
	} {
		m := rawRow(tc.table, tc.pkCol, tc.id)
		wantUser := "local"
		if tc.wantScope != "c1" {
			wantUser = "alice"
		}
		if got := str(m["user"]); got != wantUser {
			t.Fatalf("%s(%s=%s) 的 user 列应为 %q，实际 %q", tc.table, tc.pkCol, tc.id, wantUser, got)
		}
		if got := str(m["conn_key"]); got != "c1" {
			t.Fatalf("%s(%s=%s) 的 conn_key 列应为裸 key %q，实际 %q", tc.table, tc.pkCol, tc.id, "c1", got)
		}
		if got := str(m["conn_id"]); got != tc.wantScope {
			t.Fatalf("%s(%s=%s) 的 conn_id(作用域键) 列应为 %q，实际 %q", tc.table, tc.pkCol, tc.id, tc.wantScope, got)
		}
	}

	// ---- 2) 精确用户域定位（user 列 + conn_key 双条件）：跨用户域互不可见 ----
	items, err := s.ListSQLHistory("alice", "c1")
	if err != nil || len(items) != 1 || items[0].ID != "h-alice" {
		t.Fatalf("alice 应仅查到自己的历史，实际 %d 条: %v", len(items), err)
	}
	if items[0].ConnID != "c1" {
		t.Fatalf("历史条目 ConnID 应还原为裸 key，实际 %q", items[0].ConnID)
	}
	if items, _ = s.ListSQLHistory("local", "c1"); len(items) != 1 || items[0].ID != "h-local" {
		t.Fatalf("local 应仅查到自己的历史，实际 %d 条", len(items))
	}
	entries, err := s.ListSQLAudit("alice", "", 100, 0)
	if err != nil || len(entries) != 1 || entries[0].ID != "a-alice" {
		t.Fatalf("空 connID 审计应仅按 user 列过滤返回 alice 域数据，实际 %d 条: %v", len(entries), err)
	}
	if entries[0].ConnID != "c1" {
		t.Fatalf("审计条目 ConnID 应还原为裸 key，实际 %q", entries[0].ConnID)
	}
	favs, err := s.ListFavorites("alice")
	if err != nil || len(favs) != 1 || favs[0].ID != "f-alice" {
		t.Fatalf("alice 应仅查到自己的收藏，实际 %d 条: %v", len(favs), err)
	}

	// ---- 3) 空 connID：仅按 user 列过滤（该用户域全部连接） ----
	if items, _ = s.ListSQLHistory("alice", ""); len(items) != 1 || items[0].ID != "h-alice" {
		t.Fatalf("空 connID 历史应仅按 user 列过滤，alice 域实际 %d 条", len(items))
	}

	// ---- 4) 工作区/AI 会话按用户域隔离 + AI 会话归属校验 ----
	if ws, ok := s.LoadWorkspace("alice", "c1"); !ok || ws.ActiveID != "q2" {
		t.Fatalf("alice 应能读到自己的工作区: %+v ok=%v", ws, ok)
	}
	if ws, ok := s.LoadWorkspace("local", "c1"); !ok || ws.ActiveID != "q1" {
		t.Fatalf("local 应能读到自己的工作区: %+v ok=%v", ws, ok)
	}
	sessions, err := s.ListAISessions("alice", "c1", "")
	if err != nil || len(sessions) != 1 || sessions[0].ID != "s-alice" {
		t.Fatalf("alice 应仅列出自己的会话，实际 %d 条: %v", len(sessions), err)
	}
	if sessions[0].ConnID != "c1" {
		t.Fatalf("会话元信息 ConnID 应还原为裸 key，实际 %q", sessions[0].ConnID)
	}
	if sessions, _ = s.ListAISessions("local", "c1", ""); len(sessions) != 1 || sessions[0].ID != "s-local" {
		t.Fatalf("local 应仅列出自己的会话（不含 alice 的），实际 %d 条", len(sessions))
	}
	if _, ok := s.LoadAISession("local", "s-alice"); ok {
		t.Fatal("local 不应读取 alice 的 AI 会话（user 列归属校验）")
	}
	if rec, ok := s.LoadAISession("alice", "s-alice"); !ok || rec.ConnID != "c1" {
		t.Fatalf("alice 应能读取自己的会话且 ConnID 还原为裸 key: %+v ok=%v", rec, ok)
	}
	must(s.DeleteAISession("local", "s-alice"), "DeleteAISession 跨用户域（不应命中）")
	if _, ok := s.LoadAISession("alice", "s-alice"); !ok {
		t.Fatal("跨用户域删除不应命中 alice 的会话")
	}

	// ---- 5) 存量回填三种形态幂等（scoped/user、裸/local、裸+已有 user、workspace 主键） ----
	legacy := []struct {
		table             string
		pkCol             string
		row               map[string]any
		wantUser, wantRaw string
	}{
		// scoped 形态（scopeConnID 方案时代，user 列可能为空）：user=前缀、conn_key=后缀
		{tableSQLHist, "id", map[string]any{"id": "h-scoped", "conn_id": "bob\x1fc9", "created_at": 3, "body_json": "{}"}, "bob", "c9"},
		// 裸 key 形态（无 user 列时代）：user=local、conn_key=裸 key
		{tableSQLHist, "id", map[string]any{"id": "h-raw", "conn_id": "c9", "created_at": 4, "body_json": "{}"}, "local", "c9"},
		// favorites 历史上即存裸 key：已有 user 时保持不变，仅补 conn_key
		{tableSQLFav, "id", map[string]any{"id": "f-raw", "user": "carol", "conn_id": "c9", "title": "t", "created_at": 5, "body_json": "{}"}, "carol", "c9"},
		// workspace 表主键为 conn_id（作用域键）：回填不改主键，仅补 user/conn_key
		{tableWorkspace, "conn_id", map[string]any{"conn_id": "carl\x1fc9", "user": "", "tabs_json": "[]", "updated_at": 6}, "carl", "c9"},
	}
	for _, lg := range legacy {
		if _, err := s.cli.Replace(lg.table, lg.row); err != nil {
			t.Fatalf("写入模拟存量行失败: %v", err)
		}
	}
	must(s.backfillUsersDefault(), "backfillUsersDefault")
	must(s.backfillUsersDefault(), "backfillUsersDefault 幂等重跑")
	for _, lg := range legacy {
		m := rawRow(lg.table, lg.pkCol, str(lg.row[lg.pkCol]))
		wantScope := str(lg.row["conn_id"])
		if got := str(m["user"]); got != lg.wantUser {
			t.Fatalf("回填后 %s(%s=%v) 的 user 应为 %q，实际 %q", lg.table, lg.pkCol, lg.row[lg.pkCol], lg.wantUser, got)
		}
		if got := str(m["conn_key"]); got != lg.wantRaw {
			t.Fatalf("回填后 %s(%s=%v) 的 conn_key 应为 %q，实际 %q", lg.table, lg.pkCol, lg.row[lg.pkCol], lg.wantRaw, got)
		}
		if got := str(m["conn_id"]); got != wantScope {
			t.Fatalf("回填后 %s(%s=%v) 的 conn_id 应保持原值 %q，实际 %q", lg.table, lg.pkCol, lg.row[lg.pkCol], wantScope, got)
		}
	}

	// ---- 6) 跨用户级联删除：按裸 conn_key 等值一次删除所有用户域的行 ----
	must(s.DeleteWorkspacesByConnAllUsers("c1"), "DeleteWorkspacesByConnAllUsers")
	if _, ok := s.LoadWorkspace("local", "c1"); ok {
		t.Fatal("级联删除后 local 工作区应被删除")
	}
	if _, ok := s.LoadWorkspace("alice", "c1"); ok {
		t.Fatal("级联删除后 alice 工作区应被删除")
	}
	if _, ok := s.LoadWorkspace("carl", "c9"); !ok {
		t.Fatal("级联删除 c1 不应影响其他连接（c9）的工作区")
	}
	must(s.DeleteAISessionsByConnAllUsers("c1"), "DeleteAISessionsByConnAllUsers")
	if sessions, _ = s.ListAISessions("local", "c1", ""); len(sessions) != 0 {
		t.Fatal("级联删除后 local 会话应被删除")
	}
	if sessions, _ = s.ListAISessions("alice", "c1", ""); len(sessions) != 0 {
		t.Fatal("级联删除后 alice 会话应被删除")
	}
}

// TestSnapshotIndexAndMeta 验证快照索引行的 upsert/过滤/删除与 KV 元信息读写。
func TestSnapshotIndexAndMeta(t *testing.T) {
	s := newTestStore(t)

	mk := func(id, connID, label string, createdAt int64, name string) SnapshotInfo {
		return SnapshotInfo{ID: id, ConnID: connID, ConnLabel: label, Name: name, CreatedAt: createdAt, DBType: "mysql", TableCount: 3}
	}
	a1 := mk("snap-a1", "env:a", "环境A", 100, "a-1")
	a2 := mk("snap-a2", "env:a", "环境A", 200, "a-2")
	b1 := mk("snap-b1", "env:b", "环境B", 150, "b-1")

	// upsert 幂等写入
	for _, info := range []SnapshotInfo{a1, a2, b1} {
		if err := s.UpsertSnapshot(info); err != nil {
			t.Fatalf("UpsertSnapshot(%s): %v", info.ID, err)
		}
	}

	// 全量列表：created_at 倒序
	all, err := s.ListSnapshots("")
	if err != nil {
		t.Fatalf("ListSnapshots: %v", err)
	}
	if len(all) != 3 || all[0].ID != "snap-a2" || all[1].ID != "snap-b1" || all[2].ID != "snap-a1" {
		t.Fatalf("全量列表应为 created_at 倒序 [a2 b1 a1]，实际 %+v", all)
	}

	// conn_id 过滤（库模式虚拟连接 env:<id> 按环境隔离）
	onlyA, err := s.ListSnapshots("env:a")
	if err != nil {
		t.Fatalf("ListSnapshots(env:a): %v", err)
	}
	if len(onlyA) != 2 || onlyA[0].ID != "snap-a2" || onlyA[1].ID != "snap-a1" {
		t.Fatalf("env:a 过滤应得 [a2 a1]，实际 %+v", onlyA)
	}

	// 同 ID upsert = 更新（不产生重复行）
	a1Upd := a1
	a1Upd.Name = "a-1-renamed"
	a1Upd.TableCount = 9
	if err := s.UpsertSnapshot(a1Upd); err != nil {
		t.Fatalf("UpsertSnapshot 更新: %v", err)
	}
	all, _ = s.ListSnapshots("")
	if len(all) != 3 {
		t.Fatalf("同 ID upsert 后应仍为 3 行，实际 %d", len(all))
	}
	for _, info := range all {
		if info.ID == "snap-a1" && (info.Name != "a-1-renamed" || info.TableCount != 9) {
			t.Fatalf("同 ID upsert 应更新字段，实际 %+v", info)
		}
	}

	// 删除
	if err := s.DeleteSnapshot("snap-b1"); err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	if all, _ = s.ListSnapshots(""); len(all) != 2 {
		t.Fatalf("删除后应剩 2 行，实际 %d", len(all))
	}

	// KV 元信息
	if err := s.SetMeta("k1", "v1"); err != nil {
		t.Fatalf("SetMeta: %v", err)
	}
	if v, ok, err := s.GetMeta("k1"); err != nil || !ok || v != "v1" {
		t.Fatalf("GetMeta 往返失败: v=%q ok=%v err=%v", v, ok, err)
	}
	if err := s.SetMeta("k1", "v2"); err != nil {
		t.Fatalf("SetMeta 覆盖: %v", err)
	}
	if v, ok, _ := s.GetMeta("k1"); !ok || v != "v2" {
		t.Fatalf("SetMeta 覆盖失败: v=%q ok=%v", v, ok)
	}
	if _, ok, err := s.GetMeta("missing"); err != nil || ok {
		t.Fatalf("不存在的 key 应 ok=false 无错误: ok=%v err=%v", ok, err)
	}
}
