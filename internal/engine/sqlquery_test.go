package engine

import (
	"testing"

	"github.com/fj1981/infrakit/pkg/cydb"
)

func TestClassifySQL(t *testing.T) {
	cases := []struct {
		sql  string
		want bool // 是否写操作
	}{
		{"SELECT * FROM t", false},
		{"select a, b from t where id = 1", false},
		{"SHOW TABLES", false},
		{"DESC t", false},
		{"EXPLAIN SELECT * FROM t", false},
		{"USE db1", false},
		{"WITH cte AS (SELECT 1) SELECT * FROM cte", false},
		{"SET @x = 1", false},
		{"BEGIN", false},
		{"INSERT INTO t VALUES (1)", true},
		{"UPDATE t SET a = 1 WHERE id = 2", true},
		{"DELETE FROM t WHERE id = 1", true},
		{"DROP TABLE t", true},
		{"CREATE TABLE t (id INT)", true},
	}
	for _, c := range cases {
		if got := ClassifySQL(c.sql); got != c.want {
			t.Errorf("ClassifySQL(%q) = %v, want %v", c.sql, got, c.want)
		}
	}
}

func TestEnsureLimit(t *testing.T) {
	cli := cydb.NewDBCli(nil, "mysql", "k", "db", "u", "p", "")
	cases := []struct {
		name   string
		sql    string
		limit  int
		offset int
		want   string
	}{
		{"无 LIMIT 追加默认", "SELECT * FROM t", 100, 0, "SELECT * FROM `t` LIMIT 100"},
		{"无 LIMIT 带 offset", "SELECT * FROM t", 100, 50, "SELECT * FROM `t` LIMIT 100 OFFSET 50"},
		{"已含 LIMIT 原样", "SELECT * FROM t LIMIT 10", 100, 0, "SELECT * FROM t LIMIT 10"},
		{"已含 LIMIT 逗号偏移 原样", "select * from t limit 5, 10", 100, 0, "select * from t limit 5, 10"},
		{"尾部带分号 解析后剥离", "SELECT * FROM t;", 100, 0, "SELECT * FROM `t` LIMIT 100"},
		{"尾部换行+分号 解析后剥离", "SELECT * FROM t\n;\n", 100, 50, "SELECT * FROM `t` LIMIT 100 OFFSET 50"},
		{"SHOW 不加 LIMIT", "SHOW TABLES", 100, 0, "SHOW TABLES"},
		{"DESC 不加 LIMIT", "DESC t", 100, 0, "DESC t"},
		{"字符串内 limit 不误判", "SELECT 'limit' FROM t", 100, 0, "SELECT 'limit' FROM `t` LIMIT 100"},
		{"行注释内 limit 不误判", "SELECT * FROM t -- limit 99\nWHERE a=1", 100, 0, "SELECT * FROM `t` WHERE `a` = 1 LIMIT 100"},
		{"列名含 limit 不误判", "SELECT my_limit FROM t", 100, 0, "SELECT `my_limit` FROM `t` LIMIT 100"},
		{"ORDER BY 后正确追加", "SELECT a FROM t ORDER BY id DESC", 100, 0, "SELECT `a` FROM `t` ORDER BY `id` DESC LIMIT 100"},
	}
	for _, c := range cases {
		got, err := cli.EnsureLimit(c.sql, c.limit, c.offset)
		if err != nil {
			t.Fatalf("%s: EnsureLimit(%q) error = %v", c.name, c.sql, err)
		}
		if got != c.want {
			t.Errorf("%s: EnsureLimit(%q) = %q, want %q", c.name, c.sql, got, c.want)
		}
	}
}

func TestCheckDangerous(t *testing.T) {
	if _, forbidden := CheckDangerous("SELECT * FROM t"); len(forbidden) != 0 {
		t.Error("plain select should not be forbidden")
	}
	w, forbidden := CheckDangerous("SELECT LOAD_FILE('/etc/passwd')")
	if len(forbidden) == 0 {
		t.Error("LOAD_FILE should be forbidden")
	}
	_ = w
	// SLEEP 应产生警告但不禁止
	w, forbidden = CheckDangerous("SELECT SLEEP(5)")
	if len(forbidden) != 0 {
		t.Error("SLEEP should not be forbidden")
	}
	if len(w) == 0 {
		t.Error("SLEEP should produce warning")
	}
}

func TestNormalizeLimit(t *testing.T) {
	if normalizeLimit(0) != MaxQueryLimit {
		t.Error("default limit should be MaxQueryLimit")
	}
	if normalizeLimit(-1) != MaxQueryLimit {
		t.Error("negative limit should default")
	}
	if normalizeLimit(5000) != MaxQueryLimit {
		t.Error("over-limit should clamp")
	}
	if normalizeLimit(10) != 10 {
		t.Error("small limit should pass through")
	}
}

func TestSplitSQLStatements(t *testing.T) {
	cases := []struct {
		name string
		sql  string
		want []string
	}{
		{"单条", "SELECT * FROM t", []string{"SELECT * FROM t"}},
		{"多条分号", "SELECT 1; SELECT 2; SELECT 3", []string{"SELECT 1", "SELECT 2", "SELECT 3"}},
		{"尾部空语句过滤", "SELECT 1; ; SELECT 2;", []string{"SELECT 1", "SELECT 2"}},
		{"字符串内分号不分割", "SELECT 'a;b' AS x; SELECT 2", []string{"SELECT 'a;b' AS x", "SELECT 2"}},
		{"双引号内分号不分割", "SELECT \"a;b\"; SELECT 2", []string{"SELECT \"a;b\"", "SELECT 2"}},
		{"反引号内分号不分割", "SELECT `a;b` FROM t; SELECT 2", []string{"SELECT `a;b` FROM t", "SELECT 2"}},
		{"行注释内分号不分割", "SELECT 1 -- 注释; 还是注释\n; SELECT 2", []string{"SELECT 1 -- 注释; 还是注释", "SELECT 2"}},
		{"块注释内分号不分割", "SELECT 1 /* a;b */; SELECT 2", []string{"SELECT 1 /* a;\nb */", "SELECT 2"}},
		{"读写混合", "SELECT * FROM t; UPDATE t SET a=1; SELECT 2", []string{"SELECT * FROM t", "UPDATE t SET a=1", "SELECT 2"}},
		{"换行分隔", "SELECT 1\n; SELECT 2", []string{"SELECT 1", "SELECT 2"}},
	}
	for _, c := range cases {
		got := cydb.SplitSQLStatements("mysql", c.sql)
		if len(got) != len(c.want) {
			t.Errorf("%s: 语句数 = %d, want %d (%v)", c.name, len(got), len(c.want), got)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("%s: 第 %d 条 = %q, want %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}
