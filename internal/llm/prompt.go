package llm

import (
	"fmt"
	"regexp"
	"strings"
)

// 默认 system prompt 模板。用户可在 Settings 中覆盖（同样支持占位符）。
// {dialect} 由 service 从连接元数据注入；{schema} 为表结构上下文。
const defaultSystemPrompt = `你是一个资深 {dialect} DBA 与 SQL 专家。你的任务是根据用户需求生成、解释或修复 SQL。
约束：
1. 只输出 SQL，不输出多余解释（除非用户明确要求解释）。
2. 禁止生成 DROP TABLE、TRUNCATE、无 WHERE 的 DELETE/UPDATE 等危险语句；用户提出此类需求必须拒绝并说明原因。
3. 仅使用下方提供的表结构（schema）中的表与列，不得臆造不存在的对象。
4. 表名/字段名使用正确的方言标识符引用，字符串字面量用单引号。
5. 不确定时明确说明，不编造。
6. 输出格式：涉及 SQL 时，必须把完整 SQL 放进 markdown 代码块中输出，代码块以三个反引号加 sql 起始、以三个反引号结束；代码块内只放 SQL 本身，不得混入任何解释文字；一条回复只输出一个代码块。

{schema}`

// TableInfo 表结构信息（service 层从 engine 元数据转换而来，本包不依赖 engine）。
type TableInfo struct {
	Schema  string       // 库名（可为空）
	Table   string       // 表名
	Comment string       // 表注释（可为空）
	Columns []ColumnInfo // 列信息（渲染时可按需过滤敏感列，见 BuildSchemaTextFull）
}

// ColumnInfo 列信息。
type ColumnInfo struct {
	Name     string
	Type     string
	Nullable bool
	Comment  string
}

// 摘要注入控制常量：防止超长注释/超宽表挤占 token 预算。
const (
	// maxSchemaCommentRunes 单条表/列注释注入的最大字符数（超长截断，按 rune 计）。
	maxSchemaCommentRunes = 60
	// maxSchemaColumnsPerTable 单表注入的最大列数（超宽表省略其余列）。
	maxSchemaColumnsPerTable = 80
)

// sensitiveColumns 敏感列关键词：命中即从 schema 中排除（PII 脱敏）。
var sensitiveColumns = []string{
	"password", "passwd", "pwd", "token", "secret", "private_key",
	"api_key", "apikey", "access_key", "authorization", "credit_card",
	"creditcard", "card_no", "cardno", "id_card", "idcard", "ssn",
	"phone", "mobile", "email", "cookie", "session_key",
}

func isSensitiveColumn(name string) bool {
	l := strings.ToLower(name)
	for _, k := range sensitiveColumns {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

// RenderSystemPrompt 渲染最终 system prompt：
// - 使用用户自定义模板（custom），为空则用内置默认模板；
// - 替换 {dialect}；
// - {schema} 占位符仅控制注入位置；模板缺失 {schema} 时在末尾追加 schema 段，保证上下文不丢。
func RenderSystemPrompt(custom, dialect, schema string) string {
	tpl := custom
	if strings.TrimSpace(tpl) == "" {
		tpl = defaultSystemPrompt
	}
	tpl = strings.ReplaceAll(tpl, "{dialect}", dialect)
	if strings.Contains(tpl, "{schema}") {
		tpl = strings.ReplaceAll(tpl, "{schema}", schema)
	} else if strings.TrimSpace(schema) != "" {
		tpl += "\n\n可用表结构（schema）：\n" + schema
	}
	return tpl
}

// BuildSchemaText 将表结构渲染为注入文本：
// - 表/列名统一用反引号包裹（内嵌反引号双写转义），防间接 Prompt 注入；
// - 排除敏感列（PII 脱敏）；
// - maxTables 控制最多注入的表数量（<=0 表示不限），超出部分省略；
// - maxChars 控制注入文本的字符上限（<=0 表示不裁剪），超出部分裁剪。
// 两个上限由调用方从配置（ai.max_schema_tables / ai.max_schema_chars）传入。
func BuildSchemaText(tables []TableInfo, maxTables, maxChars int) string {
	return buildSchemaText(tables, maxTables, maxChars, true)
}

// BuildSchemaTextFull 与 BuildSchemaText 相同，但不做敏感列（PII）过滤，返回完整表结构。
// 供 get_schema 等「模型主动按需查询真实表结构」的工具使用：
// 过滤会破坏结构完整性，导致模型误判字段不存在（如 email/mobile/password_* 等）。
func BuildSchemaTextFull(tables []TableInfo, maxTables, maxChars int) string {
	return buildSchemaText(tables, maxTables, maxChars, false)
}

func buildSchemaText(tables []TableInfo, maxTables, maxChars int, filterSensitive bool) string {
	if len(tables) == 0 {
		return ""
	}
	if maxTables <= 0 {
		maxTables = len(tables)
	}
	var b strings.Builder
	budget := maxChars
	for i, t := range tables {
		if i >= maxTables {
			b.WriteString("\n# 其余表结构已省略...\n")
			break
		}
		if maxChars > 0 && budget <= 0 {
			b.WriteString("\n# 表结构已裁剪（内容过长）...\n")
			break
		}
		block := formatTableOpt(t, filterSensitive)
		if maxChars > 0 && len(block) > budget {
			block = truncateBlock(block, budget)
		}
		if maxChars > 0 {
			budget -= len(block)
		}
		b.WriteString(block)
	}
	return b.String()
}

// truncateBlock 在字节预算内安全截断文本：
// 按 rune 逐字符累积字节，保证不切坏 UTF-8（不产生乱码/半字），并在末尾标注裁剪。
func truncateBlock(block string, budget int) string {
	if len(block) <= budget {
		return block
	}
	var b strings.Builder
	for _, r := range block {
		if b.Len()+len(string(r)) > budget {
			break
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "\n# （已裁剪）\n"
	}
	return b.String() + "\n# （已裁剪）\n"
}

// truncRunes 按 rune 截断字符串，避免按字节硬切产生乱码；超出加省略号。
func truncRunes(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

func formatTable(t TableInfo) string {
	return formatTableOpt(t, true)
}

// formatTableOpt 渲染单表结构；filterSensitive=true 时排除敏感列（PII 脱敏），
// false 时输出全部列（get_schema 工具需要完整结构供模型生成 SQL）。
func formatTableOpt(t TableInfo, filterSensitive bool) string {
	var b strings.Builder
	name := quoteIdent(t.Table)
	if t.Schema != "" {
		name = quoteIdent(t.Schema) + "." + name
	}
	if t.Comment != "" {
		fmt.Fprintf(&b, "# 表 %s（%s）\n", name, truncRunes(t.Comment, maxSchemaCommentRunes))
	} else {
		fmt.Fprintf(&b, "# 表 %s\n", name)
	}
	written := 0
	for _, c := range t.Columns {
		if filterSensitive && isSensitiveColumn(c.Name) {
			continue
		}
		if written >= maxSchemaColumnsPerTable {
			fmt.Fprintf(&b, "# （列数过多，仅展示前 %d 列）\n", written)
			break
		}
		col := quoteIdent(c.Name) + " " + c.Type
		if c.Nullable {
			col += " NULL"
		}
		if c.Comment != "" {
			col += " -- " + truncRunes(c.Comment, maxSchemaCommentRunes)
		}
		b.WriteString(col)
		b.WriteString("\n")
		written++
	}
	return b.String()
}

// quoteIdent 用反引号包裹标识符，内嵌反引号双写转义（MySQL 风格，通用可读）。
func quoteIdent(s string) string {
	return "`" + strings.ReplaceAll(s, "`", "``") + "`"
}

// ActionPrompt 生成任务级用户消息（任务指令固定，不交给用户改 system prompt）。
func ActionPrompt(action, detail string) string {
	detail = strings.TrimSpace(detail)
	switch strings.ToLower(action) {
	case "explain":
		return "请解释以下 SQL 的用途、逻辑与潜在风险，用中文简要分条说明，不要改写 SQL：\n" + detail
	case "fix":
		// 修复场景下模型经常「按 SQL 里的字段名臆造修复」，不调 get_schema 验证真实表结构，
		// 导致修复后的 SQL 列名/表名与数据库实际不符。强制要求先调工具验证再修复。
		return "以下 SQL 执行报错。\n" +
			"修复流程：\n" +
			"1. 先从原 SQL 中识别涉及的库、表、字段；\n" +
			"2. 对每张涉及的表，必须先用 get_schema(库名, 表名) 获取真实字段后再修改，禁止基于 SQL 中的列名臆造字段或关联；\n" +
			"3. 如果当前会话上下文中已经有这些表的字段信息，可直接复用，否则必须先调 get_schema。\n" +
			"请把修复后的完整 SQL 放在 ```sql ... ``` 代码块中输出（代码块内只放 SQL 本身），代码块外再简要说明报错原因与修复要点：\n" + detail
	case "optimize":
		// 优化场景同样需要先验证表结构，否则优化可能引用不存在的字段或丢失业务逻辑。
		return "请对以下 SQL 进行性能与可读性优化。\n" +
			"优化流程：\n" +
			"1. 先从原 SQL 中识别涉及的库、表、字段；\n" +
			"2. 对每张涉及的表，必须先用 get_schema(库名, 表名) 获取真实字段后再优化（包括用到的索引信息可参考注释），禁止臆造字段或调整关联；\n" +
			"3. 如果当前会话上下文中已经有这些表的字段信息，可直接复用，否则必须先调 get_schema。\n" +
			"请把优化后的完整 SQL 放在 ```sql ... ``` 代码块中输出（代码块内只放 SQL 本身），再简要说明优化点；如无法优化请说明原因：\n" + detail
	default: // generate（对话：生成 SQL 或回答元数据咨询）
		return "请根据以下需求处理：如果需要生成 SQL，必须把 SQL 放在 ```sql ... ``` 代码块中输出（代码块内只放 SQL 本身），代码块外不要输出任何其他内容；如果是关于表结构、字段、关联关系、索引等信息的咨询，直接回答即可，不要生成 SQL。需求：\n" + detail
	}
}

// sqlFenceRe 匹配标准 markdown 代码围栏（支持常见 SQL 方言标记），
// 可嵌在解释文字中间（提示词已要求模型用此格式标准化输出）。
var sqlFenceRe = regexp.MustCompile("(?s)```(?:sql|sqlite|mysql|mariadb|postgresql|pgsql)?[ \t]*\n([\\s\\S]*?)```")

// ExtractSQL 从模型输出中提取 SQL：
// - 优先提取标准 ```sql ... ``` 代码围栏块（可嵌在解释文字中间）；
// - 无围栏时兼容旧格式：剥离首尾 ```，整段作为 SQL；
// - 剥离行注释（--）与块注释（/* */）（先剥离再交由 checkDangerous，防注释绕过）；
// - 收尾清理。
func ExtractSQL(s string) string {
	s = strings.TrimSpace(s)
	if m := sqlFenceRe.FindStringSubmatch(s); m != nil {
		s = m[1]
	} else if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		} else {
			s = ""
		}
		s = strings.TrimSuffix(s, "```")
	}
	s = stripSQLComments(s)
	return strings.TrimSpace(s)
}

// stripSQLComments 剥离 SQL 注释。安全过滤场景宁多勿少：不在引号内识别（简化实现），
// 仅用于"剥离注释后再过危险检测"，不用于解析。
func stripSQLComments(s string) string {
	// 块注释
	for {
		start := strings.Index(s, "/*")
		if start < 0 {
			break
		}
		end := strings.Index(s[start+2:], "*/")
		if end < 0 {
			s = s[:start]
			break
		}
		s = s[:start] + " " + s[start+2+end+2:]
	}
	// 行注释
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		if before, _, ok := strings.Cut(ln, "--"); ok {
			lines[i] = before
		}
	}
	return strings.Join(lines, "\n")
}
