package llm

import (
	"errors"
	"fmt"
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
	out := BuildSchemaTextFull("zh", []TableInfo{ti}, 1, 0)
	for _, name := range []string{"password_salt", "password_hash", "email", "mobile", "password_reset"} {
		if !strings.Contains(out, "`"+name+"`") {
			t.Fatalf("完整版应保留敏感列 %s:\n%s", name, out)
		}
	}
	// 默认版：仍应过滤敏感列（兼容旧静态注入语义）
	out2 := BuildSchemaText("zh", []TableInfo{ti}, 1, 0)
	for _, name := range []string{"password_salt", "password_hash", "email", "mobile", "password_reset"} {
		if strings.Contains(out2, "`"+name+"`") {
			t.Fatalf("默认版应过滤敏感列 %s:\n%s", name, out2)
		}
	}
}

func TestBuildSchemaTextEmpty(t *testing.T) {
	if s := BuildSchemaText("zh", nil, 0, 0); s != "" {
		t.Fatalf("空表列表应返回空串，实际 %q", s)
	}
}

func TestBuildSchemaTextMaxTables(t *testing.T) {
	tables := []TableInfo{
		{Table: "a", Columns: []ColumnInfo{mkCol("x", "int", false, "")}},
		{Table: "b", Columns: []ColumnInfo{mkCol("x", "int", false, "")}},
		{Table: "c", Columns: []ColumnInfo{mkCol("x", "int", false, "")}},
	}
	out := BuildSchemaText("zh", tables, 2, 0)
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
	out := BuildSchemaText("zh", tables, 0, 40)
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
	got := RenderSystemPrompt("zh", custom, "mysql", schema)
	if !strings.Contains(got, "方言 mysql") || !strings.Contains(got, "# 表 `t`") {
		t.Fatalf("自定义模板替换失败:\n%s", got)
	}
	// 自定义模板不含 {schema}：追加在末尾
	got2 := RenderSystemPrompt("zh", "你是 DBA。", "mysql", schema)
	if !strings.Contains(got2, "可用表结构（schema）：\n"+schema) {
		t.Fatalf("缺失 {schema} 时未追加:\n%s", got2)
	}
	// 空自定义模板：用默认模板
	got3 := RenderSystemPrompt("zh", "", "postgresql", schema)
	if !strings.Contains(got3, "资深 postgresql DBA") {
		t.Fatalf("默认模板未生效:\n%s", got3)
	}
}

func TestRenderSystemPromptEn(t *testing.T) {
	schema := "# Table `t`\n"
	got := RenderSystemPrompt("en", "", "mysql", schema)
	if !strings.Contains(got, "senior mysql DBA") {
		t.Fatalf("en 默认模板未生效:\n%s", got)
	}
	// 语言归一：en-US → en，未知语言回退 zh
	if g := RenderSystemPrompt("en-US", "", "mysql", schema); !strings.Contains(g, "senior mysql DBA") {
		t.Fatalf("en-US 应归一为 en:\n%s", g)
	}
	if g := RenderSystemPrompt("ja", "", "mysql", schema); !strings.Contains(g, "资深 mysql DBA") {
		t.Fatalf("未知语言应回退 zh:\n%s", g)
	}
}

func TestActionPromptLang(t *testing.T) {
	zh := ActionPrompt("zh", "explain", "SELECT 1")
	en := ActionPrompt("en", "explain", "SELECT 1")
	if !strings.Contains(zh, "解释以下 SQL") {
		t.Fatalf("zh 动作指令未生效:\n%s", zh)
	}
	if !strings.Contains(en, "Explain the purpose") {
		t.Fatalf("en 动作指令未生效:\n%s", en)
	}
	if !strings.HasSuffix(en, "SELECT 1") {
		t.Fatalf("需求文本应追加在指令末尾:\n%s", en)
	}
}

func TestKnownTables(t *testing.T) {
	names := []string{"a", "b", "c"}
	got := KnownTables("zh", "mydb", names)
	if !strings.Contains(got, "已知元数据") || !strings.Contains(got, "- mydb: a, b, c") {
		t.Fatalf("名录渲染错误:\n%s", got)
	}
	en := KnownTables("en", "mydb", names)
	if !strings.Contains(en, "Known metadata") {
		t.Fatalf("en 名录未生效:\n%s", en)
	}
	// 超上限省略并提示
	many := make([]string, knownTablesMaxList+5)
	for i := range many {
		many[i] = "t"
	}
	got2 := KnownTables("zh", "mydb", many)
	if !strings.Contains(got2, "共 35 张表") {
		t.Fatalf("名录超限未提示总数:\n%s", got2)
	}
	if strings.Count(got2, "t,") != knownTablesMaxList-1 {
		t.Fatalf("名录应仅列出前 %d 张表:\n%s", knownTablesMaxList, got2)
	}
}

func TestAgentRules(t *testing.T) {
	zh := AgentRules("zh", "mydb")
	if !strings.Contains(zh, "必须先查表结构") || !strings.Contains(zh, "数据库 \"mydb\" 内") {
		t.Fatalf("zh 规则段错误:\n%s", zh)
	}
	en := AgentRules("en", "mydb")
	if !strings.Contains(en, "query real table structures first") || !strings.Contains(en, `database "mydb"`) {
		t.Fatalf("en 规则段错误:\n%s", en)
	}
}

func TestToolTextsFor(t *testing.T) {
	zh := ToolTextsFor("zh")
	if !strings.Contains(zh.ListDBsDesc, "列出当前连接可访问的所有数据库") {
		t.Fatalf("zh list_databases 描述错误: %s", zh.ListDBsDesc)
	}
	if got := fmt.Sprintf(zh.DBNotFound, "db1", "db1, db2"); got != "数据库 \"db1\" 不存在。可用数据库：db1, db2" {
		t.Fatalf("zh 库不存在提示错误: %s", got)
	}
	if got := fmt.Sprintf(zh.TableNotFound, "t", "db1", "a, b"); !strings.Contains(got, "表 \"t\" 在库 \"db1\" 中不存在") {
		t.Fatalf("zh 表不存在提示错误: %s", got)
	}

	en := ToolTextsFor("en-US") // 语言归一
	if !strings.Contains(en.ListTablesDesc, "List all table names") {
		t.Fatalf("en list_tables 描述错误: %s", en.ListTablesDesc)
	}
	if got := fmt.Sprintf(en.DBNotFoundSchema, "db1", "db1, db2"); !strings.Contains(got, "does not exist") || !strings.Contains(got, "retry get_schema") {
		t.Fatalf("en 库不存在提示错误: %s", got)
	}
	if got := fmt.Sprintf(en.TableNotFound, "t", "db1", "a, b"); !strings.Contains(got, "Table \"t\" does not exist in database \"db1\"") {
		t.Fatalf("en 表不存在提示错误: %s", got)
	}

	ja := ToolTextsFor("ja") // 未知语言回退 zh
	if ja.ListDBsDesc != zh.ListDBsDesc {
		t.Fatalf("未知语言应回退 zh 工具文本")
	}
	// 错误文本模板可被 fmt.Errorf 消费（%w 占位符）
	if err := fmt.Errorf(zh.ErrListDBs, errors.New("boom")); !strings.Contains(err.Error(), "列出数据库失败: boom") {
		t.Fatalf("zh 工具错误文本错误: %v", err)
	}
}
