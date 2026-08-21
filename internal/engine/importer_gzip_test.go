package engine

import (
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func TestSqlBaseName(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"cy_licgen.sql", "cy_licgen", true},
		{"cy_licgen.sql.gz", "cy_licgen", true},
		{"CY_LICGEN.SQL.GZ", "CY_LICGEN", true},
		{"mydb.SQL", "mydb", true},
		{"mydb.gz", "", false}, // 非 .sql.gz 的裸 .gz 不识别
		{"mydb.zip", "", false},
		{"mydb.desc", "", false},
	}
	for _, c := range cases {
		got, ok := sqlBaseName(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("sqlBaseName(%q) = (%q,%v), want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// [FIX] .sql.gz 透明解压回归：导入侧必须能读取 gzip 压缩的 SQL，
// 否则 --gzip 导出产物无法回导
func TestOpenSQLFileGzipRoundTrip(t *testing.T) {
	dir := t.TempDir()
	content := "-- dqex export\nCREATE TABLE t (id INT);\n"

	// 明文 .sql
	plain := filepath.Join(dir, "a.sql")
	if err := os.WriteFile(plain, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	// gzip .sql.gz
	gzPath := filepath.Join(dir, "b.sql.gz")
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(content)); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	if err := os.WriteFile(gzPath, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, p := range []string{plain, gzPath} {
		r, err := openSQLFile(p)
		if err != nil {
			t.Fatalf("openSQLFile(%s): %v", p, err)
		}
		var got bytes.Buffer
		if _, err := got.ReadFrom(r); err != nil {
			t.Fatalf("读取 %s 失败: %v", p, err)
		}
		r.Close()
		if got.String() != content {
			t.Errorf("%s 内容不一致: %q", p, got.String())
		}
	}

	// 损坏的 gzip 应报错
	bad := filepath.Join(dir, "bad.sql.gz")
	if err := os.WriteFile(bad, []byte("not gzip"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := openSQLFile(bad); err == nil {
		t.Error("损坏的 gzip 文件应返回错误")
	}
}
