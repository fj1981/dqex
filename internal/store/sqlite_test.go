package store

import (
	"path/filepath"
	"testing"

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
		if err := s.AddSQLHistory(item); err != nil {
			t.Fatalf("AddSQLHistory 失败: %v", err)
		}
	}
	items, err := s.ListSQLHistory("c1")
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
	if err := s.ClearSQLHistory("c1"); err != nil {
		t.Fatalf("ClearSQLHistory 失败: %v", err)
	}
	items, _ = s.ListSQLHistory("c1")
	if len(items) != 0 {
		t.Fatal("清空后历史应为空")
	}
}

// TestSQLAuditAppendAndList 验证审计只增、倒序分页。
func TestSQLAuditAppendAndList(t *testing.T) {
	s := newTestStore(t)

	for i := 0; i < 10; i++ {
		entry := SQLAuditEntry{ConnID: "c1", SQL: "select 1", CreatedAt: int64(i), Source: "manual"}
		if err := s.AppendSQLAudit(entry); err != nil {
			t.Fatalf("AppendSQLAudit 失败: %v", err)
		}
	}

	// 倒序（新→旧）
	entries, err := s.ListSQLAudit("c1", 5, 0)
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
	entries2, err := s.ListSQLAudit("c1", 5, 5)
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

	if _, ok := s.LoadWorkspace("c1"); ok {
		t.Fatal("初始不应有工作区")
	}

	state := WorkspaceState{
		Tabs: []WorkspaceTab{
			{ID: "q1", Kind: "query", Seq: 1, DB: "app", SQL: "select 1", Mode: "transform"},
			{ID: "o1", Kind: "object", DB: "app", Name: "users", ObjType: "table", SubTab: "data"},
		},
		ActiveID: "q1",
	}
	if err := s.SaveWorkspace("c1", state); err != nil {
		t.Fatalf("SaveWorkspace 失败: %v", err)
	}

	got, ok := s.LoadWorkspace("c1")
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
	if err := s.SaveWorkspace("c1", WorkspaceState{Tabs: []WorkspaceTab{}, ActiveID: ""}); err != nil {
		t.Fatalf("覆盖 SaveWorkspace 失败: %v", err)
	}
	got2, _ := s.LoadWorkspace("c1")
	if len(got2.Tabs) != 0 {
		t.Fatalf("覆盖后 tabs 应为空，实际 %d", len(got2.Tabs))
	}

	// 删除
	if err := s.DeleteWorkspace("c1"); err != nil {
		t.Fatalf("DeleteWorkspace 失败: %v", err)
	}
	if _, ok := s.LoadWorkspace("c1"); ok {
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
