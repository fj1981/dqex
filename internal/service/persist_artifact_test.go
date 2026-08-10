package service

import (
	"os"
	"path/filepath"
	"testing"
)

// TestRemoveArtifact 产物清理：受管目录内删除、外部路径不动、空路径/不存在文件不报错
func TestRemoveArtifact(t *testing.T) {
	base := t.TempDir()
	p, err := NewPersistMgrWith(ResolveDirs(base, nil))
	if err != nil {
		t.Fatalf("NewPersistMgrWith: %v", err)
	}

	// exports 内的文件与目录
	exportFile := filepath.Join(p.ExportDir(), "a.zip")
	exportDir := filepath.Join(p.ExportDir(), "export_task")
	// compares 内的文件
	compareFile := filepath.Join(p.CompareDir(), "compare-x.json")
	// 受管目录之外的文件（模拟用户 -o 自定义输出）
	outsideFile := filepath.Join(base, "outside", "keep.zip")
	if err := os.MkdirAll(filepath.Dir(outsideFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(exportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{exportFile, compareFile, outsideFile, filepath.Join(exportDir, "db.sql")} {
		if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	p.RemoveArtifact(exportFile)
	if _, err := os.Stat(exportFile); !os.IsNotExist(err) {
		t.Errorf("exports 内文件应被删除: %s", exportFile)
	}
	p.RemoveArtifact(exportDir)
	if _, err := os.Stat(exportDir); !os.IsNotExist(err) {
		t.Errorf("exports 内目录应被删除: %s", exportDir)
	}
	p.RemoveArtifact(compareFile)
	if _, err := os.Stat(compareFile); !os.IsNotExist(err) {
		t.Errorf("compares 内文件应被删除: %s", compareFile)
	}
	p.RemoveArtifact(outsideFile)
	if _, err := os.Stat(outsideFile); err != nil {
		t.Errorf("受管目录之外的文件不应被删除: %s", outsideFile)
	}
	// 空路径与不存在的路径：不 panic、不报错
	p.RemoveArtifact("")
	p.RemoveArtifact(filepath.Join(p.ExportDir(), "not-exist.zip"))
}
