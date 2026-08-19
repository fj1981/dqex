package engine

import (
	"strings"
	"testing"
)

// genSQL 便捷封装：validCols 传 nil 跳过列名白名单校验（仅结构化转义），聚焦语句生成正确性
func genSQL(t *testing.T, dbType string, p GenSQLParams) string {
	t.Helper()
	sql, err := genSQLText(dbType, p, nil)
	if err != nil {
		t.Fatalf("genSQLText error = %v", err)
	}
	return sql
}

// TestGenSQLInsert 验证 INSERT 生成：标识符引用、字符串转义、NULL、跳过自增列、批量多行。
func TestGenSQLInsert(t *testing.T) {
	base := GenSQLParams{Table: "users", Kind: GenSQLInsert, Columns: []string{"id", "name", "note"}}

	t.Run("MySQL 字符串转义 + NULL", func(t *testing.T) {
		p := base
		p.Rows = [][]any{{1, "O'Brien", nil}}
		want := "INSERT INTO `users` (`id`, `name`, `note`) VALUES (1, 'O''Brien', NULL);"
		if got := genSQL(t, "mysql", p); got != want {
			t.Errorf("MySQL insert 不符.\nwant: %s\ngot : %s", want, got)
		}
	})

	t.Run("PostgreSQL 标识符引用与单引号转义", func(t *testing.T) {
		p := base
		p.Rows = [][]any{{1, "Alice's note", "x"}}
		want := "INSERT INTO \"users\" (\"id\", \"name\", \"note\") VALUES (1, 'Alice''s note', 'x');"
		if got := genSQL(t, "postgresql", p); got != want {
			t.Errorf("PostgreSQL insert 不符.\nwant: %s\ngot : %s", want, got)
		}
	})

	t.Run("跳过自增列", func(t *testing.T) {
		p := base
		p.SkipColumns = []string{"id"}
		p.Rows = [][]any{{0, "Bob", "x"}}
		want := "INSERT INTO `users` (`name`, `note`) VALUES ('Bob', 'x');"
		if got := genSQL(t, "mysql", p); got != want {
			t.Errorf("跳过自增列不符.\nwant: %s\ngot : %s", want, got)
		}
	})

	t.Run("批量多行", func(t *testing.T) {
		p := base
		p.Rows = [][]any{{1, "A", "x"}, {2, "B", nil}}
		want := "INSERT INTO `users` (`id`, `name`, `note`) VALUES (1, 'A', 'x');\n" +
			"INSERT INTO `users` (`id`, `name`, `note`) VALUES (2, 'B', NULL);"
		if got := genSQL(t, "mysql", p); got != want {
			t.Errorf("批量多行不符.\nwant: %s\ngot : %s", want, got)
		}
	})

	t.Run("全部列为跳过列报错", func(t *testing.T) {
		p := base
		p.SkipColumns = []string{"id", "name", "note"}
		p.Rows = [][]any{{1, "A", "x"}}
		if _, err := genSQLText("mysql", p, nil); err == nil || !strings.Contains(err.Error(), "无可插入列") {
			t.Errorf("应报「无可插入列」，got %v", err)
		}
	})
}

// TestGenSQLUpdate 验证 UPDATE 生成：SET 非主键列、复合主键 WHERE、仅主键列报错。
func TestGenSQLUpdate(t *testing.T) {
	p := GenSQLParams{
		Table: "orders", Kind: GenSQLUpdate,
		Columns: []string{"id", "sku", "status", "qty"}, PKColumns: []string{"id", "sku"},
		Rows: [][]any{{7, "S-1", "done", 3}},
	}
	want := "UPDATE `orders` SET `status` = 'done', `qty` = 3 WHERE `id` = 7 AND `sku` = 'S-1';"
	if got := genSQL(t, "mysql", p); got != want {
		t.Errorf("update 不符.\nwant: %s\ngot : %s", want, got)
	}

	t.Run("表仅主键列报错", func(t *testing.T) {
		p2 := p
		p2.Columns = []string{"id", "sku"}
		p2.Rows = [][]any{{7, "S-1"}}
		if _, err := genSQLText("mysql", p2, nil); err == nil || !strings.Contains(err.Error(), "无可更新列") {
			t.Errorf("应报「无可更新列」，got %v", err)
		}
	})
}

// TestGenSQLDelete 验证 DELETE 生成（复合主键）。
func TestGenSQLDelete(t *testing.T) {
	p := GenSQLParams{
		Table: "orders", Kind: GenSQLDelete,
		Columns: []string{"id", "sku", "status"}, PKColumns: []string{"id", "sku"},
		Rows: [][]any{{7, "S-1", "done"}},
	}
	want := "DELETE FROM `orders` WHERE `id` = 7 AND `sku` = 'S-1';"
	if got := genSQL(t, "mysql", p); got != want {
		t.Errorf("delete 不符.\nwant: %s\ngot : %s", want, got)
	}

	t.Run("多行复合主键→元组IN", func(t *testing.T) {
		p2 := p
		p2.Rows = [][]any{{7, "S-1", "done"}, {8, "S-2", "pending"}}
		want := "DELETE FROM `orders` WHERE (`id`, `sku`) IN ((7, 'S-1'), (8, 'S-2'));"
		if got := genSQL(t, "mysql", p2); got != want {
			t.Errorf("delete 多行复合主键不符.\nwant: %s\ngot : %s", want, got)
		}
	})

	t.Run("多行单列主键→IN", func(t *testing.T) {
		p2 := GenSQLParams{
			Table: "users", Kind: GenSQLDelete,
			Columns: []string{"id", "name"}, PKColumns: []string{"id"},
			Rows: [][]any{{1, "a"}, {2, "b"}, {3, nil}},
		}
		want := "DELETE FROM `users` WHERE `id` IN (1, 2, 3);"
		if got := genSQL(t, "mysql", p2); got != want {
			t.Errorf("delete 多行单列主键不符.\nwant: %s\ngot : %s", want, got)
		}
	})

	t.Run("冒号前缀值安全内联（cydb 转义为 ::）", func(t *testing.T) {
		p2 := GenSQLParams{
			Table: "users", Kind: GenSQLDelete,
			Columns: []string{"id", "name"}, PKColumns: []string{"id"},
			Rows: [][]any{{":abc", "a"}, {":def", "b"}},
		}
		// 值未被误判为命名参数，而是按 cydb 统一转义（: → ::，与 EQ 路径一致）
		want := "DELETE FROM `users` WHERE `id` IN ('::abc', '::def');"
		if got := genSQL(t, "mysql", p2); got != want {
			t.Errorf("冒号前缀值不符.\nwant: %s\ngot : %s", want, got)
		}
	})
}

// TestGenSQLSelectByPK 验证按主键 SELECT（PostgreSQL 双引号标识符）。
func TestGenSQLSelectByPK(t *testing.T) {
	p := GenSQLParams{
		Table: "orders", Kind: GenSQLSelectByPK,
		Columns: []string{"id", "sku", "status"}, PKColumns: []string{"id"},
		Rows: [][]any{{7, "S-1", "done"}},
	}
	got := genSQL(t, "postgresql", p)
	for _, frag := range []string{"SELECT", `"id"`, `"sku"`, `"status"`, "FROM \"orders\"", "WHERE \"id\" = 7", ";"} {
		if !strings.Contains(got, frag) {
			t.Errorf("selectByPk 缺少片段 %q.\ngot: %s", frag, got)
		}
	}
}

// TestGenSQLWhereCell 验证单元格条件：等于值（含转义）/ NULL → IS NULL。
func TestGenSQLWhereCell(t *testing.T) {
	t.Run("等于值", func(t *testing.T) {
		p := GenSQLParams{Table: "users", Kind: GenSQLWhereCell, Columns: []string{"name"}, Rows: [][]any{{"O'Reilly"}}}
		want := "SELECT * FROM `users` WHERE `name` = 'O''Reilly';"
		if got := genSQL(t, "mysql", p); got != want {
			t.Errorf("whereCell 等于值不符.\nwant: %s\ngot : %s", want, got)
		}
	})

	t.Run("NULL 值", func(t *testing.T) {
		p := GenSQLParams{Table: "users", Kind: GenSQLWhereCell, Columns: []string{"deleted_at"}, Rows: [][]any{{nil}}}
		want := "SELECT * FROM `users` WHERE `deleted_at` IS NULL;"
		if got := genSQL(t, "mysql", p); got != want {
			t.Errorf("whereCell NULL 不符.\nwant: %s\ngot : %s", want, got)
		}
	})
}

// TestGenSQLSelectByFilter 验证过滤条件 SELECT：eq/gt/isNull 组合 + 排序 + LIKE 通配符转义。
func TestGenSQLSelectByFilter(t *testing.T) {
	p := GenSQLParams{
		Table: "users", Kind: GenSQLSelectByFilter,
		Columns:   []string{"id", "name", "age"},
		Filters:   []ColumnFilter{{Column: "age", Op: FilterGte, Value: 18}, {Column: "deleted", Op: FilterIsNull}},
		SortSpecs: []SortSpec{{Column: "age", Order: "desc"}},
	}
	got := genSQL(t, "mysql", p)
	for _, frag := range []string{"SELECT", "`id`", "`name`", "`age`", "FROM `users`", "`age` >= 18", "`deleted` IS NULL", "ORDER BY `age` DESC", ";"} {
		if !strings.Contains(got, frag) {
			t.Errorf("selectByFilter 缺少片段 %q.\ngot: %s", frag, got)
		}
	}

	t.Run("LIKE 通配符转义", func(t *testing.T) {
		p2 := p
		p2.Filters = []ColumnFilter{{Column: "name", Op: FilterContains, Value: "100%"}}
		got := genSQL(t, "mysql", p2)
		// 转义后的通配符必须出现在文本中（% 包裹 + \% 转义），且原值不裸出现
		if !strings.Contains(got, "LIKE") || !strings.Contains(got, `%100\%`) {
			t.Errorf("LIKE 通配符未正确转义.\ngot: %s", got)
		}
		if strings.Contains(got, "100%") {
			t.Errorf("LIKE 原值裸出现在 SQL 中（未转义）.\ngot: %s", got)
		}
	})
}

// TestGenSQLValidation 验证参数校验：非法 kind、无主键、列值长度不一致、列名白名单。
func TestGenSQLValidation(t *testing.T) {
	t.Run("非法 kind", func(t *testing.T) {
		p := GenSQLParams{Table: "t", Kind: "drop", Rows: [][]any{{1}}}
		if _, err := genSQLText("mysql", p, nil); err == nil || !strings.Contains(err.Error(), "未知的生成类型") {
			t.Errorf("非法 kind 应报错，got %v", err)
		}
	})

	t.Run("无主键", func(t *testing.T) {
		p := GenSQLParams{Table: "t", Kind: GenSQLDelete, Columns: []string{"a"}, Rows: [][]any{{1}}}
		if _, err := genSQLText("mysql", p, nil); err == nil || !strings.Contains(err.Error(), "无主键") {
			t.Errorf("无主键应报错，got %v", err)
		}
	})

	t.Run("列值长度不一致", func(t *testing.T) {
		p := GenSQLParams{Table: "t", Kind: GenSQLInsert, Columns: []string{"a", "b"}, Rows: [][]any{{1}}}
		if _, err := genSQLText("mysql", p, nil); err == nil || !strings.Contains(err.Error(), "数量不一致") {
			t.Errorf("列值长度不一致应报错，got %v", err)
		}
	})

	t.Run("列名白名单拒绝", func(t *testing.T) {
		p := GenSQLParams{Table: "t", Kind: GenSQLWhereCell, Columns: []string{"evil"}, Rows: [][]any{{"x"}}}
		valid := map[string]bool{"id": true}
		if _, err := genSQLText("mysql", p, valid); err == nil || !strings.Contains(err.Error(), "不存在于表") {
			t.Errorf("白名单外列名应报错，got %v", err)
		}
	})

	t.Run("行数超限", func(t *testing.T) {
		rows := make([][]any, maxGenSQLRows+1)
		p := GenSQLParams{Table: "t", Kind: GenSQLInsert, Columns: []string{"a"}, Rows: rows}
		if _, err := genSQLText("mysql", p, nil); err == nil || !strings.Contains(err.Error(), "最多生成") {
			t.Errorf("行数超限应报错，got %v", err)
		}
	})
}
