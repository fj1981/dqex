package service

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestSnapshotIndexMigration 存量 JSON 索引 → DB 一次性迁移：
// 首次访问自动导入、meta 置标记、legacy 文件只读保留、DB 删除不复活、
// Delete 入口同样触发迁移（存量快照可直接删，不误报不存在）、
// 无 persist（StoreNone 降级）时回退 JSON 索引路径。
func TestSnapshotIndexMigration(t *testing.T) {
	base := t.TempDir()
	p, err := NewPersistMgrWith(ResolveDirs(base, nil))
	if err != nil {
		t.Fatalf("NewPersistMgrWith: %v", err)
	}

	// 构造 legacy 本地索引（persist 非 nil 且 artifactStore 为 nil → 本地 snapshots/index.json）
	legacy := []SnapshotInfo{
		{ID: "snap-1", ConnID: "env:a", ConnLabel: "环境A", Name: "a-1", CreatedAt: 100, DBType: "mysql"},
		{ID: "snap-2", ConnID: "env:b", ConnLabel: "环境B", Name: "b-1", CreatedAt: 200, DBType: "mysql"},
	}
	raw, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(p.SnapshotDir(), "index.json")
	if err := os.WriteFile(legacyPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	// 1) 首次 List 触发迁移：条目入 DB、meta 置标记、legacy 保留
	svc := &Service{persist: p}
	got := svc.ListSnapshots("")
	if len(got) != 2 || got[0].ID != "snap-2" || got[1].ID != "snap-1" {
		t.Fatalf("迁移后列表应含 2 条且按时间倒序，实际 %+v", got)
	}
	if _, ok, err := p.GetMeta(metaKeySnapshotIndexMigrated); err != nil || !ok {
		t.Fatalf("迁移后应置 meta 标记: ok=%v err=%v", ok, err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy 索引应保留未删: %v", err)
	}

	// 2) conn 过滤（服务端语义）
	if got = svc.ListSnapshots("env:a"); len(got) != 1 || got[0].ID != "snap-1" {
		t.Fatalf("env:a 过滤应得 1 条 snap-1，实际 %+v", got)
	}

	// 3) DB 删除：列表减少，legacy 冻结不变（不会被同步清理，回滚旧版本仍可读全量）
	if err := svc.DeleteSnapshot("snap-1"); err != nil {
		t.Fatalf("DeleteSnapshot: %v", err)
	}
	if got = svc.ListSnapshots(""); len(got) != 1 || got[0].ID != "snap-2" {
		t.Fatalf("删除后应剩 snap-2，实际 %+v", got)
	}
	kept, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	var legacyAfter []SnapshotInfo
	if err := json.Unmarshal(kept, &legacyAfter); err != nil || len(legacyAfter) != 2 {
		t.Fatalf("legacy 索引应保持 2 条不变: n=%d err=%v", len(legacyAfter), err)
	}

	// 4) 新 Service 实例（模拟重启）：meta 已置位 → 不重迁移，已删条目不复活
	svcRestart := &Service{persist: p}
	if got = svcRestart.ListSnapshots(""); len(got) != 1 || got[0].ID != "snap-2" {
		t.Fatalf("重启后不应复活已删条目，实际 %+v", got)
	}

	// 5) Delete 入口独立触发迁移：跳过 List 直接删存量快照不误报不存在
	svcDel := &Service{persist: p, artifactStore: nil}
	if err := svcDel.DeleteSnapshot("snap-2"); err != nil {
		t.Fatalf("Delete 直接触发迁移后应可删除存量快照: %v", err)
	}
	if got = svcDel.ListSnapshots(""); len(got) != 0 {
		t.Fatalf("全部删除后列表应为空，实际 %+v", got)
	}

	// 6) StoreNone 降级（persist 为 nil）：回退 JSON 索引，conn 过滤在内存完成
	svcNone := &Service{}
	if got = svcNone.ListSnapshots(""); len(got) != 0 {
		t.Fatalf("无 persist 时应回退 JSON 索引（空目录=空列表），实际 %+v", got)
	}
	if got = svcNone.ListSnapshots("env:a"); got == nil {
		t.Fatal("StoreNone 降级列表应非 nil 切片")
	}
}
