package llm

import (
	"fmt"
	"regexp"
	"strings"
)

// promptTexts 提示词文案（按语言索引，新增语言只加 map 条目）
type promptTexts struct {
	systemPrompt string // 内置默认 system prompt（{dialect}/{schema} 占位符）

	// schema 渲染片段
	schemaOmitted    string // 其余表结构已省略
	schemaTrimmed    string // 表结构已裁剪（内容过长）
	blockTrimmed     string // （已裁剪）
	tableHeader      string // # 表 %s（%s）
	tableHeaderPlain string // # 表 %s
	colLimitNote     string // # （列数过多，仅展示前 %d 列）

	// agent 模式约束（agentRules 静态段 + agentScopeRule 动态段，%q=目标库）
	agentRules     string
	agentScopeRule string

	// 已知表名录段（knownTablesHeader %s=目标库；knownTablesMore %d=总表数）
	knownTablesHeader string
	knownTablesMore   string

	// 动作指令（detail 追加在末尾）
	actionExplain  string
	actionFix      string
	actionOptimize string
	actionGenerate string

	// agent 只读工具描述与输出文本（模型可见，需按会话语言）
	toolListDBsDesc         string // list_databases 工具描述
	toolListTablesDesc      string // list_tables 工具描述
	toolGetSchemaDesc       string // get_schema 工具描述
	toolDBNotFound          string // 库不存在提示（list_tables，%q=库名 %s=可用库列表）
	toolDBNotFoundSchema    string // 库不存在提示（get_schema，同上）
	toolTableNotFound       string // 表不存在提示（%q=表名 %q=库名 %s=可用表列表）
	toolErrListDBs          string // 列出数据库失败错误（%w=原始错误）
	toolErrListTables       string // 列出表失败错误（%w=原始错误）
	toolErrListDBsForSchema string // get_schema 前置获取库列表失败错误（%w=原始错误）
}

// promptTextsMap 语言注册表：缺失语言回退 zh
var promptTextsMap = map[string]promptTexts{
	"zh": {
		systemPrompt: `你是一个资深 {dialect} DBA 与 SQL 专家。你的任务是根据用户需求生成、解释或修复 SQL。
约束：
1. 只输出 SQL，不输出多余解释（除非用户明确要求解释）。
2. 禁止生成 DROP TABLE、TRUNCATE、无 WHERE 的 DELETE/UPDATE 等危险语句；用户提出此类需求必须拒绝并说明原因。
3. 仅使用下方提供的表结构（schema）中的表与列，不得臆造不存在的对象。
4. 表名/字段名使用正确的方言标识符引用，字符串字面量用单引号。
5. 不确定时明确说明，不编造。
6. 输出格式：涉及 SQL 时，必须把完整 SQL 放进 markdown 代码块中输出，代码块以三个反引号加 sql 起始、以三个反引号结束；代码块内只放 SQL 本身，不得混入任何解释文字；一条回复只输出一个代码块。

{schema}`,
		schemaOmitted:    "\n# 其余表结构已省略...\n",
		schemaTrimmed:    "\n# 表结构已裁剪（内容过长）...\n",
		blockTrimmed:     "\n# （已裁剪）\n",
		tableHeader:      "# 表 %s（%s）\n",
		tableHeaderPlain: "# 表 %s\n",
		colLimitNote:     "# （列数过多，仅展示前 %d 列）\n",
		agentRules: "【最高优先级：必须先查表结构，禁止凭空生成 SQL】\n" +
			"生成 SQL 前，必须先完整梳理需求涉及的所有表与关联关系，并对每一张涉及的表调用 get_schema(库名, 表名) 获取真实字段后再编写。只要还没有拿到某张表的真实字段信息，就必须先调用 get_schema 去查询确认，禁止在缺少表结构信息的情况下直接臆造表名、字段名或关联关系生成 SQL。\n" +
			"如果上下文里还没有任何表结构信息，你的第一轮回复必须只调用 get_schema（或先 list_tables 定位表名）获取结构，禁止第一轮就直接输出 SQL。\n" +
			"涉及多表关联时，先分析清楚各表之间靠哪个字段关联（外键/关联列），再用 get_schema 确认关联字段确实存在。\n" +
			"【回答元数据问题（如“某表有哪些字段”）时必须聚焦】只调用 get_schema 查询用户明确指定的那一张表，只回答那一张表的字段，禁止把其它表或整个库的表结构一起列出。\n" +
			"仅当当前库的表确实无法满足需求（表名或字段对不上、关联表明显不在当前库）时，才调用 list_databases 查看其他库；禁止无依据地随意探索其他库。\n" +
			"需要工具时直接发起工具调用，不要输出解释文本；可一次并行调用多个 get_schema 批量获取多张表结构。\n" +
			"禁止输出思考过程、推理过程或任何 <think>/<thinking>/<reasoning> 标签包裹的内容，直接给出结果。\n" +
			"生成的 SQL 放在 ```sql 代码块中。危险语句（DROP/TRUNCATE/无 WHERE 的 DELETE 等）一律拒绝。\n",
		agentScopeRule:    "\n你的工作范围默认限定在数据库 %q 内，优先只使用该库的表。\n",
		knownTablesHeader: "\n\n已知元数据（表结构需用 get_schema 查询）：\n- %s: ",
		knownTablesMore:   "\n…共 %d 张表（其余请用 list_tables/get_schema 查询）",
		actionExplain:     "请解释以下 SQL 的用途、逻辑与潜在风险，用中文简要分条说明，不要改写 SQL：\n",
		actionFix: "以下 SQL 执行报错。\n" +
			"修复流程：\n" +
			"1. 先从原 SQL 中识别涉及的库、表、字段；\n" +
			"2. 对每张涉及的表，必须先用 get_schema(库名, 表名) 获取真实字段后再修改，禁止基于 SQL 中的列名臆造字段或关联；\n" +
			"3. 如果当前会话上下文中已经有这些表的字段信息，可直接复用，否则必须先调 get_schema。\n" +
			"请把修复后的完整 SQL 放在 ```sql ... ``` 代码块中输出（代码块内只放 SQL 本身），代码块外再简要说明报错原因与修复要点：\n",
		actionOptimize: "请对以下 SQL 进行性能与可读性优化。\n" +
			"优化流程：\n" +
			"1. 先从原 SQL 中识别涉及的库、表、字段；\n" +
			"2. 对每张涉及的表，必须先用 get_schema(库名, 表名) 获取真实字段后再优化（包括用到的索引信息可参考注释），禁止臆造字段或调整关联；\n" +
			"3. 如果当前会话上下文中已经有这些表的字段信息，可直接复用，否则必须先调 get_schema。\n" +
			"请把优化后的完整 SQL 放在 ```sql ... ``` 代码块中输出（代码块内只放 SQL 本身），再简要说明优化点；如无法优化请说明原因：\n",
		actionGenerate:          "请根据以下需求处理：如果需要生成 SQL，必须把 SQL 放在 ```sql ... ``` 代码块中输出（代码块内只放 SQL 本身），代码块外不要输出任何其他内容；如果是关于表结构、字段、关联关系、索引等信息的咨询，直接回答即可，不要生成 SQL。需求：\n",
		toolListDBsDesc:         "列出当前连接可访问的所有数据库（Oracle 为 schema 列表）。仅当确认需要跨库查询时才调用，默认应优先使用当前库。",
		toolListTablesDesc:      "列出指定数据库中的全部表名。",
		toolGetSchemaDesc:       "获取指定表的结构摘要（表注释 + 字段名/类型/可空/注释）。",
		toolDBNotFound:          "数据库 %q 不存在。可用数据库：%s",
		toolDBNotFoundSchema:    "数据库 %q 不存在。可用数据库：%s。请用正确的库名重试 get_schema。",
		toolTableNotFound:       "表 %q 在库 %q 中不存在。该库可用表（前 50 个）：%s。请用正确的表名重试。",
		toolErrListDBs:          "列出数据库失败: %w",
		toolErrListTables:       "列出表失败: %w",
		toolErrListDBsForSchema: "获取库列表失败: %w",
	},
	"en": {
		systemPrompt: `You are a senior {dialect} DBA and SQL expert. Your task is to generate, explain, or fix SQL based on user requests.
Constraints:
1. Output only SQL, with no extra explanation (unless the user explicitly asks for an explanation).
2. Never generate dangerous statements such as DROP TABLE, TRUNCATE, or DELETE/UPDATE without a WHERE clause; if the user requests such, refuse and explain why.
3. Use only the tables and columns in the schema below; do not invent objects that do not exist.
4. Quote table/column names with the correct dialect identifier; use single quotes for string literals.
5. When unsure, say so explicitly; never fabricate.
6. Output format: when SQL is involved, always put the complete SQL in a markdown code block starting with three backticks plus sql and ending with three backticks; the code block must contain only SQL, no explanation text; output only one code block per reply.

{schema}`,
		schemaOmitted:    "\n# Remaining table structures omitted...\n",
		schemaTrimmed:    "\n# Schema trimmed (content too long)...\n",
		blockTrimmed:     "\n# (trimmed)\n",
		tableHeader:      "# Table %s (%s)\n",
		tableHeaderPlain: "# Table %s\n",
		colLimitNote:     "# (Too many columns; only the first %d shown)\n",
		agentRules: "[TOP PRIORITY: query real table structures first; never fabricate SQL]\n" +
			"Before writing SQL, first analyze all tables and relationships involved in the request, and call get_schema(database, table) for every involved table to fetch the real columns. Until you have the real column info for a table, you MUST call get_schema to confirm it; never invent table names, column names, or relationships without schema info.\n" +
			"If no table structure info exists in the context yet, your first reply must only call get_schema (or list_tables first to locate table names); never output SQL in the first turn.\n" +
			"For multi-table joins, first figure out which columns join the tables (foreign keys/join columns), then confirm with get_schema that those columns exist.\n" +
			"[Focus when answering metadata questions (e.g. “which columns does a table have”)] Only call get_schema for the single table the user explicitly asked about and answer only that table's columns; never dump other tables or the whole database.\n" +
			"Only when tables in the current database cannot satisfy the request (mismatched names/columns, related tables clearly in another database) may you call list_databases to explore; never explore other databases without reason.\n" +
			"Call tools directly when needed without explanatory text; you may call multiple get_schema tools in parallel to fetch several table structures at once.\n" +
			"Never output your thinking process, reasoning, or any <think>/<thinking>/<reasoning> tags; give the result directly.\n" +
			"Put generated SQL inside a ```sql code block. Always refuse dangerous statements (DROP/TRUNCATE/DELETE without WHERE, etc.).\n",
		agentScopeRule:    "\nYour working scope defaults to database %q; prefer using only tables in that database.\n",
		knownTablesHeader: "\n\nKnown metadata (query table structures with get_schema when needed):\n- %s: ",
		knownTablesMore:   "\n…%d tables in total (use list_tables/get_schema for the rest)",
		actionExplain:     "Explain the purpose, logic, and potential risks of the following SQL in brief English bullet points; do not rewrite the SQL:\n",
		actionFix: "The following SQL failed to execute.\n" +
			"Fix process:\n" +
			"1. Identify the databases, tables, and columns involved in the original SQL;\n" +
			"2. For each involved table, call get_schema(database, table) to fetch the real columns before modifying; never assume fields or joins based on column names in the SQL;\n" +
			"3. If field info for these tables is already in the session context, reuse it; otherwise call get_schema first.\n" +
			"Output the fixed complete SQL inside a ```sql ... ``` code block (only SQL inside), then briefly explain the cause of the error and the fix:\n",
		actionOptimize: "Please optimize the following SQL for performance and readability.\n" +
			"Optimization process:\n" +
			"1. Identify the databases, tables, and columns involved in the original SQL;\n" +
			"2. For each involved table, call get_schema(database, table) to fetch the real columns before optimizing (index information may be referenced in comments); never assume fields or adjust joins;\n" +
			"3. If field info for these tables is already in the session context, reuse it; otherwise call get_schema first.\n" +
			"Output the optimized complete SQL inside a ```sql ... ``` code block (only SQL inside), then briefly explain the optimizations; if it cannot be optimized, explain why:\n",
		actionGenerate:          "Process the following request: if SQL needs to be generated, put it inside a ```sql ... ``` code block (only SQL inside) and output nothing else outside the block; if it is a question about table structure, columns, relationships, indexes, or other metadata, answer directly without generating SQL. Request:\n",
		toolListDBsDesc:         "List all databases accessible on the current connection (schemas for Oracle). Only call this when you are sure a cross-database query is needed; by default prefer the current database.",
		toolListTablesDesc:      "List all table names in the specified database.",
		toolGetSchemaDesc:       "Get a structural summary of the specified table (table comment + column names/types/nullability/comments).",
		toolDBNotFound:          "Database %q does not exist. Available databases: %s",
		toolDBNotFoundSchema:    "Database %q does not exist. Available databases: %s. Please retry get_schema with the correct database name.",
		toolTableNotFound:       "Table %q does not exist in database %q. Available tables in this database (first 50): %s. Please retry with the correct table name.",
		toolErrListDBs:          "failed to list databases: %w",
		toolErrListTables:       "failed to list tables: %w",
		toolErrListDBsForSchema: "failed to get database list: %w",
	},
}

// NormLang 语言代码归一：zh 前缀→zh、en 前缀→en、未知→zh（与业务注册表一致，可扩展）。
func NormLang(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	switch {
	case strings.HasPrefix(l, "zh"):
		return "zh"
	case strings.HasPrefix(l, "en"):
		return "en"
	default:
		return "zh"
	}
}

// textsFor 按语言取提示词文案，缺失语言回退 zh
func textsFor(lang string) promptTexts {
	if t, ok := promptTextsMap[NormLang(lang)]; ok {
		return t
	}
	return promptTextsMap["zh"]
}

// 默认 system prompt 模板。用户可在 Settings 中覆盖（同样支持占位符）。
// {dialect} 由 service 从连接元数据注入；{schema} 为表结构上下文。

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
// - 使用用户自定义模板（custom），为空则用内置默认模板（按 lang 选择）；
// - 替换 {dialect}；
// - {schema} 占位符仅控制注入位置；模板缺失 {schema} 时在末尾追加 schema 段，保证上下文不丢。
func RenderSystemPrompt(lang, custom, dialect, schema string) string {
	tpl := custom
	if strings.TrimSpace(tpl) == "" {
		tpl = textsFor(lang).systemPrompt
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
func BuildSchemaText(lang string, tables []TableInfo, maxTables, maxChars int) string {
	return buildSchemaText(lang, tables, maxTables, maxChars, true)
}

// BuildSchemaTextFull 与 BuildSchemaText 相同，但不做敏感列（PII）过滤，返回完整表结构。
// 供 get_schema 等「模型主动按需查询真实表结构」的工具使用：
// 过滤会破坏结构完整性，导致模型误判字段不存在（如 email/mobile/password_* 等）。
func BuildSchemaTextFull(lang string, tables []TableInfo, maxTables, maxChars int) string {
	return buildSchemaText(lang, tables, maxTables, maxChars, false)
}

func buildSchemaText(lang string, tables []TableInfo, maxTables, maxChars int, filterSensitive bool) string {
	if len(tables) == 0 {
		return ""
	}
	txt := textsFor(lang)
	if maxTables <= 0 {
		maxTables = len(tables)
	}
	var b strings.Builder
	budget := maxChars
	for i, t := range tables {
		if i >= maxTables {
			b.WriteString(txt.schemaOmitted)
			break
		}
		if maxChars > 0 && budget <= 0 {
			b.WriteString(txt.schemaTrimmed)
			break
		}
		block := formatTableOpt(t, filterSensitive, txt)
		if maxChars > 0 && len(block) > budget {
			block = truncateBlock(block, budget, txt)
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
func truncateBlock(block string, budget int, txt promptTexts) string {
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
		return txt.blockTrimmed
	}
	return b.String() + txt.blockTrimmed
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
	return formatTableOpt(t, true, textsFor("zh"))
}

// formatTableOpt 渲染单表结构；filterSensitive=true 时排除敏感列（PII 脱敏），
// false 时输出全部列（get_schema 工具需要完整结构供模型生成 SQL）。
func formatTableOpt(t TableInfo, filterSensitive bool, txt promptTexts) string {
	var b strings.Builder
	name := quoteIdent(t.Table)
	if t.Schema != "" {
		name = quoteIdent(t.Schema) + "." + name
	}
	if t.Comment != "" {
		fmt.Fprintf(&b, txt.tableHeader, name, truncRunes(t.Comment, maxSchemaCommentRunes))
	} else {
		fmt.Fprintf(&b, txt.tableHeaderPlain, name)
	}
	written := 0
	for _, c := range t.Columns {
		if filterSensitive && isSensitiveColumn(c.Name) {
			continue
		}
		if written >= maxSchemaColumnsPerTable {
			fmt.Fprintf(&b, txt.colLimitNote, written)
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
func ActionPrompt(lang, action, detail string) string {
	txt := textsFor(lang)
	detail = strings.TrimSpace(detail)
	switch strings.ToLower(action) {
	case "explain":
		return txt.actionExplain + detail
	case "fix":
		// 修复场景下模型经常「按 SQL 里的字段名臆造修复」，不调 get_schema 验证真实表结构，
		// 导致修复后的 SQL 列名/表名与数据库实际不符。强制要求先调工具验证再修复。
		return txt.actionFix + detail
	case "optimize":
		// 优化场景同样需要先验证表结构，否则优化可能引用不存在的字段或丢失业务逻辑。
		return txt.actionOptimize + detail
	default: // generate（对话：生成 SQL 或回答元数据咨询）
		return txt.actionGenerate + detail
	}
}

// knownTablesMaxList 名录中最多列出的表名数量（其余提示用工具查询）。
const knownTablesMaxList = 30

// AgentRules 返回 agent 模式的工具使用规则段（含目标库工作范围，按语言）。
// 该段始终追加在 system prompt 末尾，保证 agent 工具调用规则不被自定义 prompt 覆盖。
func AgentRules(lang, target string) string {
	txt := textsFor(lang)
	return txt.agentRules + fmt.Sprintf(txt.agentScopeRule, target)
}

// KnownTables 渲染「已知表名录」段（目标库 + 表名列表，按语言）。
// 表名超上限时省略并提示用工具查询剩余。
func KnownTables(lang, target string, names []string) string {
	txt := textsFor(lang)
	var b strings.Builder
	fmt.Fprintf(&b, txt.knownTablesHeader, target)
	list := names
	if len(list) > knownTablesMaxList {
		list = list[:knownTablesMaxList]
	}
	b.WriteString(strings.Join(list, ", "))
	if len(names) > knownTablesMaxList {
		fmt.Fprintf(&b, txt.knownTablesMore, len(names))
	}
	return b.String()
}

// ToolTexts agent 只读工具（list_databases/list_tables/get_schema）的描述与输出文本，
// 模型可见，需与会话语言一致（缺失语言回退 zh）。
type ToolTexts struct {
	ListDBsDesc         string // list_databases 工具描述
	ListTablesDesc      string // list_tables 工具描述
	GetSchemaDesc       string // get_schema 工具描述
	DBNotFound          string // 库不存在提示（list_tables，%q=库名 %s=可用库列表）
	DBNotFoundSchema    string // 库不存在提示（get_schema，同上）
	TableNotFound       string // 表不存在提示（%q=表名 %q=库名 %s=可用表列表）
	ErrListDBs          string // 列出数据库失败错误（%w=原始错误）
	ErrListTables       string // 列出表失败错误（%w=原始错误）
	ErrListDBsForSchema string // get_schema 前置获取库列表失败错误（%w=原始错误）
}

// ToolTextsFor 按语言取 agent 工具文本，缺失语言回退 zh。
func ToolTextsFor(lang string) ToolTexts {
	txt := textsFor(lang)
	return ToolTexts{
		ListDBsDesc:         txt.toolListDBsDesc,
		ListTablesDesc:      txt.toolListTablesDesc,
		GetSchemaDesc:       txt.toolGetSchemaDesc,
		DBNotFound:          txt.toolDBNotFound,
		DBNotFoundSchema:    txt.toolDBNotFoundSchema,
		TableNotFound:       txt.toolTableNotFound,
		ErrListDBs:          txt.toolErrListDBs,
		ErrListTables:       txt.toolErrListTables,
		ErrListDBsForSchema: txt.toolErrListDBsForSchema,
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
