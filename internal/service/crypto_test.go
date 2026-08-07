package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCryptoRoundTrip 验证加解密回环与密文格式
func TestCryptoRoundTrip(t *testing.T) {
	plain := []byte(`{"本地":{"Type":"mysql","Pw":"s3cret"}}`)
	enc, err := encryptData(plain)
	if err != nil {
		t.Fatal("encrypt:", err)
	}
	if !strings.HasPrefix(enc, encPrefix) {
		t.Fatal("缺少密文前缀")
	}
	if strings.Contains(enc, "s3cret") {
		t.Fatal("密文中包含明文密码")
	}
	dec, err := decryptData(enc)
	if err != nil {
		t.Fatal("decrypt:", err)
	}
	if string(dec) != string(plain) {
		t.Fatal("解密结果不一致")
	}
}

// TestPersistEncryptOnSaveAndLegacyPlain 验证旧明文兼容迁移与落盘加密
func TestPersistEncryptOnSaveAndLegacyPlain(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "exports"), 0o755); err != nil {
		t.Fatal(err)
	}
	p := &PersistMgr{baseDir: dir}

	// 1. 旧版明文文件可直接读取
	legacy := `{"old":{"Type":"mysql","Host":"127.0.0.1","Port":3306,"Pw":"pw1"}}`
	if err := os.WriteFile(filepath.Join(dir, "connections.json"), []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	conns := p.LoadConns()
	var found bool
	for _, rec := range conns {
		if rec.Name == "old" && rec.Conn.Pw == "pw1" {
			found = true
		}
	}
	if !found {
		t.Fatal("旧明文读取失败")
	}

	// 2. 保存后文件变为密文（含自动迁移旧数据），且可再次读取
	nc := DBConnInfo{}
	nc.Type = "mysql"
	nc.Host = "h"
	nc.Port = 1
	nc.Pw = "pw2"
	if _, err := p.SaveConn(ConnRecord{Name: "new", Conn: nc}); err != nil {
		t.Fatal("save:", err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "connections.json"))
	if !strings.HasPrefix(string(raw), encPrefix) {
		t.Fatal("保存后文件未加密")
	}
	if strings.Contains(string(raw), "pw1") || strings.Contains(string(raw), "pw2") {
		t.Fatal("文件中存在明文密码")
	}
	conns = p.LoadConns()
	var hitOld, hitNew bool
	for _, rec := range conns {
		if rec.Name == "old" && rec.Conn.Pw == "pw1" {
			hitOld = true
		}
		if rec.Name == "new" && rec.Conn.Pw == "pw2" {
			hitNew = true
		}
	}
	if !hitOld || !hitNew {
		t.Fatal("加密后读取失败")
	}

	// 3. 文件权限为 0600
	info, _ := os.Stat(filepath.Join(dir, "connections.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatal("权限不是 0600:", info.Mode().Perm())
	}
}
