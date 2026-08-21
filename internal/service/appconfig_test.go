package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestResolveDirs 验证四类目录解析优先级：--data-dir > 配置文件 dirs.data > 默认；子目录显式值 > 派生
func TestResolveDirs(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("无法获取 home 目录")
	}

	// 无任何输入：全部默认派生
	d := ResolveDirs("", nil)
	if d.Data != filepath.Join(home, ".dqex") {
		t.Fatalf("默认 data 目录应为 ~/.dqex，实际 %s", d.Data)
	}
	if d.Exports != filepath.Join(d.Data, ExportDirName) || d.Tmp != filepath.Join(d.Data, TempDirName) || d.Uploads != filepath.Join(d.Data, UploadDirName) {
		t.Fatalf("默认子目录应由 data 派生，实际 %+v", d)
	}

	// 配置文件指定 data 与 exports：exports 显式生效，tmp 随 data 派生
	cfg := &AppConfig{Dirs: DirConfig{Data: "/cfg/data", Exports: "/disk/exports"}}
	d = ResolveDirs("", cfg)
	if d.Data != "/cfg/data" || d.Exports != "/disk/exports" {
		t.Fatalf("配置值未生效: %+v", d)
	}
	if d.Tmp != filepath.Join("/cfg/data", TempDirName) {
		t.Fatalf("tmp 应由配置 data 派生，实际 %s", d.Tmp)
	}

	// --data-dir 优先级高于配置文件 dirs.data，但子目录显式值仍生效
	d = ResolveDirs("/flag/data", cfg)
	if d.Data != "/flag/data" {
		t.Fatalf("--data-dir 应优先，实际 %s", d.Data)
	}
	if d.Exports != "/disk/exports" || d.Tmp != filepath.Join("/flag/data", TempDirName) {
		t.Fatalf("flag 覆盖 data 后子目录解析错误: %+v", d)
	}
}

// TestLoadAppConfig 验证空路径返回默认配置（兼容排序规则默认开启）、显式路径缺失报错
func TestLoadAppConfig(t *testing.T) {
	cfg, err := LoadAppConfig(context.Background(), "")
	if err != nil || cfg == nil {
		t.Fatalf("空路径应返回默认配置: %v", err)
	}
	if !cfg.CompatCollation {
		t.Fatal("首次初始化（无配置文件）时 CompatCollation 应默认为 true")
	}
	if _, err := LoadAppConfig(context.Background(), "/no/such/config.yaml"); err == nil {
		t.Fatal("显式路径缺失应报错")
	}
}
