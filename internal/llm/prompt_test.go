package llm

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// 构造一条带注释的测试列
func mkCol(name, typ string, nullable bool, comment string) ColumnInfo {
	return ColumnInfo{Name: name, Type: typ, Nullable: nullable, Comment: comment}
}

func TestFormatTable(t *testing.T) {
	ti := TableInfo{
		Schema:  "rpa_cs",
		Table:   "sys_user",
		Comment: "用户表",
		Columns: []ColumnInfo{
			mkCol("id", "bigint", false, "主键"),
			mkCol("username", "varchar(64)", false, "用户名"),
			mkCol("org_id", "bigint", true, ""),
		},
	}
	out := formatTable(ti)
	if !strings.Contains(out, "# 表 `rpa_cs`.`sys_user`（用户表）") {
		t.Fatalf("表头格式错误:\n%s", out)
	}
	if !strings.Contains(out, "`id` bigint -- 主键") {
		t.Fatalf("列格式缺少注释:\n%s", out)
	}
	if !strings.Contains(out, "`org_id` bigint NULL") {
		t.Fatalf("可空列应带 NULL 标记:\n%s", out)
	}
	if strings.Contains(out, "-- ") && strings.Count(out, "-- ") != 2 {
		t.Fatalf("无注释列不应带 -- 标记:\n%s", out)
	}
}

func TestFormatTableNoSchema(t *testing.T) {
	ti := TableInfo{
		Table:   "t1",
		Columns: []ColumnInfo{mkCol("a", "int", false, "")},
	}
	out := formatTable(ti)
	if !strings.Contains(out, "# 表 `t1`") || strings.Contains(out, "`t1`（") {
		t.Fatalf("无库名/无注释时表头格式错误:\n%s", out)
	}
}

func TestSensitiveColumnFilter(t *testing.T) {
	ti := TableInfo{
		Schema: "s",
		Table:  "account",
		Columns: []ColumnInfo{
			mkCol("id", "int", false, ""),
			mkCol("username", "varchar(32)", false, ""),
			mkCol("password", "varchar(128)", false, "登录密码"),
			mkCol("api_token", "varchar(64)", true, "令牌"),
		},
	}
	out := formatTable(ti)
	if strings.Contains(out, "password") || strings.Contains(out, "api_token") {
		t.Fatalf("敏感列未脱敏:\n%s", out)
	}
	if !strings.Contains(out, "`username`") {
		t.Fatalf("正常列不应被误过滤:\n%s", out)
	}
}

func TestCommentTruncate(t *testing.T) {
	long := strings.Repeat("很长的列注释描述", 20) // 140 字
	ti := TableInfo{
		Table:   "t",
		Comment: long,
		Columns: []ColumnInfo{mkCol("a", "int", false, long)},
	}
	out := formatTable(ti)
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		t.Fatalf("输出行数不足:\n%s", out)
	}
	if !strings.HasSuffix(lines[0], "…）") {
		t.Fatalf("表注释未截断加省略号: %q", lines[0])
	}
	if !strings.HasSuffix(strings.TrimSpace(lines[1]), "…") {
		t.Fatalf("列注释未截断加省略号: %q", lines[1])
	}
	// 中文按 rune 截断，不得产生非法 UTF-8
	if !utf8.ValidString(out) {
		t.Fatal("截断后产生非法 UTF-8")
	}
}

func TestColumnLimit(t *testing.T) {
	// 用非敏感列名，确保不被脱敏过滤
	cols := make([]ColumnInfo, 0, maxSchemaColumnsPerTable+10)
	for range maxSchemaColumnsPerTable + 10 {
		cols = append(cols, mkCol("col_", "int", false, ""))
	}
	ti := TableInfo{Table: "wide", Columns: cols}
	out := formatTable(ti)
	if !strings.Contains(out, "仅展示前") {
		t.Fatalf("超宽表应输出省略标注:\n%s", out)
	}
	n := strings.Count(out, "\n") - 2 // 去掉表头行与标注行
	if n != maxSchemaColumnsPerTable {
		t.Fatalf("列数应为 %d，实际 %d", maxSchemaColumnsPerTable, n)
	}
}

func TestBuildSchemaTextFullKeepsSensitive(t *testing.T) {
	ti := TableInfo{
		Schema:  "rpa_cs",
		Table:   "robotics_user",
		Comment: "用户表",
		Columns: []ColumnInfo{
			mkCol("id", "bigint", false, "主键id"),
			mkCol("login_name", "varchar(50)", false, "登录名"),
			mkCol("password_salt", "varchar(64)", true, "盐"),
			mkCol("password_hash", "varchar(128)", true, ""),
			mkCol("email", "varchar(100)", true, "邮箱"),
			mkCol("mobile", "varchar(20)", true, "手机"),
			mkCol("password_reset", "int", true, ""),
		},
	}
	// 完整版（get_schema 工具用）：敏感列必须保留
	out := BuildSchemaTextFull([]TableInfo{ti}, 1, 0)
	for _, name := range []string{"password_salt", "password_hash", "email", "mobile", "password_reset"} {
		if !strings.Contains(out, "`"+name+"`") {
			t.Fatalf("完整版应保留敏感列 %s:\n%s", name, out)
		}
	}
	// 默认版：仍应过滤敏感列（兼容旧静态注入语义）
	out2 := BuildSchemaText([]TableInfo{ti}, 1, 0)
	for _, name := range []string{"password_salt", "password_hash", "email", "mobile", "password_reset"} {
		if strings.Contains(out2, "`"+name+"`") {
			t.Fatalf("默认版应过滤敏感列 %s:\n%s", name, out2)
		}
	}
}

func TestBuildSchemaTextEmpty(t *testing.T) {
	if s := BuildSchemaText(nil, 0, 0); s != "" {
		t.Fatalf("空表列表应返回空串，实际 %q", s)
	}
}

func TestBuildSchemaTextMaxTables(t *testing.T) {
	tables := []TableInfo{
		{Table: "a", Columns: []ColumnInfo{mkCol("x", "int", false, "")}},
		{Table: "b", Columns: []ColumnInfo{mkCol("x", "int", false, "")}},
		{Table: "c", Columns: []ColumnInfo{mkCol("x", "int", false, "")}},
	}
	out := BuildSchemaText(tables, 2, 0)
	if !strings.Contains(out, "其余表结构已省略") {
		t.Fatalf("超出 maxTables 应输出省略标记:\n%s", out)
	}
	if strings.Contains(out, "`c`") {
		t.Fatalf("第 3 张表不应被注入:\n%s", out)
	}
}

func TestBuildSchemaTextMaxChars(t *testing.T) {
	tables := []TableInfo{
		{Table: "中文表", Columns: []ColumnInfo{mkCol("列", "varchar(255)", false, "中文注释")}},
	}
	out := BuildSchemaText(tables, 0, 40)
	if !strings.Contains(out, "（已裁剪）") {
		t.Fatalf("超出 maxChars 应输出裁剪标记:\n%s", out)
	}
	if !utf8.ValidString(out) {
		t.Fatalf("裁剪后产生非法 UTF-8:\n%q", out)
	}
}

func TestQuoteIdent(t *testing.T) {
	if got := quoteIdent("a`b"); got != "`a``b`" {
		t.Fatalf("内嵌反引号转义错误: %s", got)
	}
}

func TestRenderSystemPrompt(t *testing.T) {
	schema := "# 表 `t`\n`a` int\n"
	// 自定义模板含 {schema}
	custom := "你是 DBA，方言 {dialect}。\n{schema}\n结尾"
	got := RenderSystemPrompt(custom, "mysql", schema)
	if !strings.Contains(got, "方言 mysql") || !strings.Contains(got, "# 表 `t`") {
		t.Fatalf("自定义模板替换失败:\n%s", got)
	}
	// 自定义模板不含 {schema}：追加在末尾
	got2 := RenderSystemPrompt("你是 DBA。", "mysql", schema)
	if !strings.Contains(got2, "可用表结构（schema）：\n"+schema) {
		t.Fatalf("缺失 {schema} 时未追加:\n%s", got2)
	}
	// 空自定义模板：用默认模板
	got3 := RenderSystemPrompt("", "postgresql", schema)
	if !strings.Contains(got3, "资深 postgresql DBA") {
		t.Fatalf("默认模板未生效:\n%s", got3)
	}
}
