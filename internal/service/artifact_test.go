package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fj1981/dqex/internal/engine"
	"github.com/fj1981/infrakit/pkg/cystore"

	// 注册 local provider（模拟 OSS；options 根包会循环依赖，测试直接引 provider 包）
	_ "github.com/fj1981/infrakit/pkg/cystore/local"
)

// newArtifactTestSvc 构造独立模式（本地目录）测试实例
func newArtifactTestSvc(t *testing.T) (*Service, string) {
	t.Helper()
	base := t.TempDir()
	p, err := NewPersistMgrWith(ResolveDirs(base, nil))
	if err != nil {
		t.Fatalf("NewPersistMgrWith: %v", err)
	}
	return &Service{persist: p}, base
}

// newArtifactTestSvcWithStore 构造对象存储模式测试实例（local provider 模拟 OSS）
func newArtifactTestSvcWithStore(t *testing.T) (*Service, string, string) {
	t.Helper()
	svc, base := newArtifactTestSvc(t)
	ossDir := t.TempDir()
	st, err := cystore.NewStore(&cystore.Config{Provider: cystore.ProviderLocal, BasePath: ossDir})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	svc.artifactStore = st
	svc.artifactBucket = "test-bucket" // local provider 必须指定 bucket（模拟宿主注入）
	return svc, base, ossDir
}

func TestIsArtifactLogicalPath(t *testing.T) {
	cases := map[string]bool{
		"exports/a.zip":                true,
		"compares/compare-x.json":      true,
		"snapshots/snap-1.json":        false, // 快照内容走快照专用读写，不参与历史产物分流
		"compare-x.json":               false,
		"/abs/path/exports/a.zip":      false,
		"deploy/dl/dqex/exports/a.zip": false,
		"":                             false,
	}
	for in, want := range cases {
		if got := isArtifactLogicalPath(in); got != want {
			t.Errorf("isArtifactLogicalPath(%q)=%v, want %v", in, got, want)
		}
	}
}

func TestArtifactPutGetRemoveLocal(t *testing.T) {
	svc, base := newArtifactTestSvc(t)
	logical, err := svc.artifactPut(artifactPrefixCompares, "compare-1.json", []byte(`{"a":1}`))
	if err != nil {
		t.Fatalf("artifactPut: %v", err)
	}
	if logical != "compares/compare-1.json" {
		t.Fatalf("logical=%q", logical)
	}
	local := filepath.Join(base, "compares", "compare-1.json")
	if _, err := os.Stat(local); err != nil {
		t.Fatalf("本地受管目录应有产物: %v", err)
	}
	data, err := svc.artifactGet(logical)
	if err != nil || string(data) != `{"a":1}` {
		t.Fatalf("artifactGet: %v %s", err, data)
	}
	if err := svc.artifactRemove(logical); err != nil {
		t.Fatalf("artifactRemove: %v", err)
	}
	if _, err := os.Stat(local); !os.IsNotExist(err) {
		t.Fatalf("删除后文件不应存在: %v", err)
	}
}

func TestArtifactPutGetRemoveWithStore(t *testing.T) {
	svc, base, ossDir := newArtifactTestSvcWithStore(t)
	logical, err := svc.artifactPut(artifactPrefixCompares, "compare-2.json", []byte(`{"b":2}`))
	if err != nil {
		t.Fatalf("artifactPut: %v", err)
	}
	// 一步到位：对象存在（local provider 落盘结构 BasePath/<bucket>/<key>），本地受管目录不落终态文件
	if _, err := os.Stat(filepath.Join(ossDir, "test-bucket", "compares", "compare-2.json")); err != nil {
		t.Fatalf("对象应直接落存储: %v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "compares", "compare-2.json")); !os.IsNotExist(err) {
		t.Fatalf("对象存储模式本地不应有终态文件: %v", err)
	}
	data, err := svc.artifactGet(logical)
	if err != nil || string(data) != `{"b":2}` {
		t.Fatalf("artifactGet: %v %s", err, data)
	}
	if err := svc.artifactRemove(logical); err != nil {
		t.Fatalf("artifactRemove: %v", err)
	}
	if _, err := svc.artifactGet(logical); err == nil {
		t.Fatal("删除后再读应报错")
	}
}

func TestArtifactLocalPutRelocatesFile(t *testing.T) {
	// 对象存储模式：源文件唯一一次流式上传后删除
	svc, _, ossDir := newArtifactTestSvcWithStore(t)
	src := filepath.Join(t.TempDir(), "pack", "demo_1.zip")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, []byte("zip-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	logical, size, err := svc.artifactLocalPut(artifactPrefixExports, "demo_1.zip", src)
	if err != nil {
		t.Fatalf("artifactLocalPut: %v", err)
	}
	if logical != "exports/demo_1.zip" || size != int64(len("zip-bytes")) {
		t.Fatalf("logical=%q size=%d", logical, size)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("上传后源文件应清理: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ossDir, "test-bucket", "exports", "demo_1.zip")); err != nil {
		t.Fatalf("对象应存在: %v", err)
	}

	// 独立模式：rename 进受管目录
	svc2, base := newArtifactTestSvc(t)
	src2 := filepath.Join(base, "tmp", "artifact-export", "demo_2.zip")
	if err := os.MkdirAll(filepath.Dir(src2), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src2, []byte("z2"), 0o644); err != nil {
		t.Fatal(err)
	}
	logical2, size2, err := svc2.artifactLocalPut(artifactPrefixExports, "demo_2.zip", src2)
	if err != nil || logical2 != "exports/demo_2.zip" || size2 != 2 {
		t.Fatalf("local put: logical=%q size=%d err=%v", logical2, size2, err)
	}
	if _, err := os.Stat(filepath.Join(base, "exports", "demo_2.zip")); err != nil {
		t.Fatalf("受管目录应有产物: %v", err)
	}
}

func TestRelocateExportArtifactKeepsDir(t *testing.T) {
	svc, _, _ := newArtifactTestSvcWithStore(t)
	dir := filepath.Join(t.TempDir(), "result")
	if err := os.MkdirAll(filepath.Join(dir, "db1"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 目录产物（Compress=false 桌面场景）：原样保留本地，不上传
	logical, size, err := svc.relocateExportArtifact(dir)
	if err != nil || logical != dir || size != 0 {
		t.Fatalf("目录产物应原样返回: logical=%q size=%d err=%v", logical, size, err)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("目录产物应保留: %v", err)
	}
}

func TestCompareReportRoundtrip(t *testing.T) {
	result := &engine.CompareResult{Source: "src", Target: "dst", Summary: engine.CompareSummary{Total: 3, Matched: 2, DataDiff: 1}}
	for _, tc := range []struct {
		name string
		svc  *Service
	}{
		{"local", func() *Service { s, _ := newArtifactTestSvc(t); return s }()},
		{"store", func() *Service { s, _, _ := newArtifactTestSvcWithStore(t); return s }()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logical, err := tc.svc.saveCompareResultArtifact("compare-t1.json", result)
			if err != nil || !isArtifactLogicalPath(logical) {
				t.Fatalf("save: logical=%q err=%v", logical, err)
			}
			got, err := tc.svc.loadCompareResultArtifact(logical)
			if err != nil {
				t.Fatalf("load: %v", err)
			}
			if !strings.Contains(string(got), `"source": "src"`) {
				t.Fatalf("报告内容不符: %s", got)
			}
			// 历史记录只存文件名（无前缀）时也能读
			if _, err := tc.svc.loadCompareResultArtifact("compare-t1.json"); err != nil {
				t.Fatalf("load by name: %v", err)
			}
		})
	}
}

func TestDeleteHistoryRemovesArtifactByMode(t *testing.T) {
	svc, base, ossDir := newArtifactTestSvcWithStore(t)
	// 逻辑路径记录：走存取器删对象
	if _, err := svc.artifactPut(artifactPrefixExports, "del-me.zip", []byte("x")); err != nil {
		t.Fatal(err)
	}
	rec := ExecutionRecord{ID: "task-del-1", TaskType: "export", Status: "done", OutputPath: "exports/del-me.zip"}
	if err := svc.persist.SaveHistory(rec); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteHistory("task-del-1"); err != nil {
		t.Fatalf("DeleteHistory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(ossDir, "test-bucket", "exports", "del-me.zip")); !os.IsNotExist(err) {
		t.Fatalf("对象应被清理: %v", err)
	}
	// 本地目录产物记录（真实路径）：走 RemoveArtifact 清理
	dirArt := filepath.Join(base, "exports", "dir-task")
	if err := os.MkdirAll(dirArt, 0o755); err != nil {
		t.Fatal(err)
	}
	rec2 := ExecutionRecord{ID: "task-del-2", TaskType: "export", Status: "done", OutputPath: dirArt}
	if err := svc.persist.SaveHistory(rec2); err != nil {
		t.Fatal(err)
	}
	if err := svc.DeleteHistory("task-del-2"); err != nil {
		t.Fatalf("DeleteHistory dir: %v", err)
	}
	if _, err := os.Stat(dirArt); !os.IsNotExist(err) {
		t.Fatalf("目录产物应被清理: %v", err)
	}
}
