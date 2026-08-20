package sqlcmd

import (
	"dbimpex/internal/llm"
	"fmt"
	"io"
)

// printf/fprintf/sprintf 包装 fmt 同名函数：语言注册表文本为动态格式串，
// go vet 对非字面量格式串会误报 non-constant format string，自定义包装可绕过检查，行为与 fmt 一致。
func printf(format string, args ...any)               { fmt.Printf(format, args...) }
func fprintf(w io.Writer, format string, args ...any) { fmt.Fprintf(w, format, args...) }
func sprintf(format string, args ...any) string       { return fmt.Sprintf(format, args...) }

// ---- CLI 输出语言（元命令/帮助/状态类高频文本；核心业务错误沿用中文） ----

// cliLang 当前 CLI 输出语言（由父命令 --lang / 环境变量 DBX_LANG 注入，默认 zh）。
// 语言代码走 llm.NormLang 归一与回退，与 AI/字典注册表一致，可扩展。
var cliLang = "zh"

// SetLang 设置 CLI 输出语言（父命令 --lang / DBX_LANG 入口，归一化存储）。
func SetLang(lang string) {
	cliLang = llm.NormLang(lang)
}

// cliTexts SQL 终端高频输出文案（按语言索引，新增语言只加 map 条目）。
type cliTexts struct {
	// 交互横幅
	bannerTitle string // dbx sql - 交互式 SQL 终端
	bannerConn  string // 连接: %s @ %s:%d/%s
	bannerHint  string // 输入 \h 查看帮助，\q 退出

	// 写操作确认
	confirmWrite string // 确认执行写操作? [y/N]
	cancelled    string // 已取消
	errorPrefix  string // 错误: %v
	aiFixHint    string // 提示: 输入 \ai fix 可让 AI 根据该报错自动修复 SQL

	// 行数警告
	autoLimit     string // （已自动追加 LIMIT %d）
	rowsReturned  string // ⚠ 已返回 %d 行%s
	rowsTip       string // 提示：使用 LIMIT n 指定返回行数，或加 WHERE 条件缩小范围
	rowsTipNoTerm string // 提示：使用 LIMIT n 或 WHERE 缩小范围

	// 元命令
	timingOn      string // 耗时显示: 开
	timingOff     string // 耗时显示: 关
	xDisplaySaved string // 扩展显示: %s（已写入 config.yaml 作为默认）
	xModeVertical string // 开（垂直显示）
	xModeTable    string // 关（表格显示）
	xModeAuto     string // auto（表格超宽时自动垂直）
	copyUsage     string // 用法: \copy <文件路径>

	// 元数据展示
	noTables       string // (无匹配表)
	noColumns      string // (无列信息)
	noIndexes      string // (无索引)
	indexesTitle   string // 索引:
	failListTables string // 获取表列表失败: %v
	failListDBs    string // 获取数据库列表失败: %v
	failTableMeta  string // 获取表结构失败: %v
	failIndexInfo  string // (无法获取索引信息: %v)
	colName        string // 列名
	colType        string // 类型
	colNullable    string // 可空
	colDefault     string // 默认值
	colComment     string // 说明

	// 文件操作
	noExportSQL  string // 没有上一条查询结果可导出
	exportedRows string // 已导出 %d 行到 %s
	execFile     string // 执行 %s...

	// 编辑器
	noEditSQL  string // 没有上一条 SQL 可编辑
	sqlUpdated string // SQL 已更新

	// 帮助
	metaHelp string // \h 元命令帮助全文
	aiHelp   string // \ai 帮助全文

	// 危险检测
	dangerFunc string // 检测到危险函数: %s
	forbidFunc string // 检测到禁止函数: %s

	// 单次执行（-e/-f）提示
	noSQLProvided string // 请通过 -e / -f / 位置参数提供 SQL
	needConn      string // 请指定连接: -c <已保存连接> 或内联 --type/--host/--port/--un/--pw/--db
	jsonNoWrite   string // JSON 模式下不允许写操作，请使用 --allow-write 确认
	fileNoWrite   string // 文件执行含写操作，请使用 --allow-write 确认

	// 表格渲染提示
	tableAutoVertical string // ⚠ 表格宽度超过终端，已自动切换为垂直显示（每行一个字段）
	tableTooWide      string // ⚠ 表格宽度超过终端（%d 列），建议用 \G 垂直显示
	tableTruncChars   string // …(共 %d 字符)

	// 配置写入提示
	displayModeSaveFail string // (提示：显示模式未能写入 config.yaml: %s)

	// token 统计
	tokenInOut string // 输入 %d / 输出 %d / 合计 %d

	// \ai config 引导提示
	aiCfgAPIKey          string // api_key（输入新值覆盖，回车保持）
	aiCfgMaxSchemaTables string // max_schema_tables（注入上下文的最大表数）
	aiCfgMaxSchemaChars  string // max_schema_chars（表结构文本字符上限）
	aiCfgSystemPrompt    string // system_prompt（输入 clear 清空，回车保持）
	aiCfgDefaultPrompt   string // 内置默认模板

	// 交互终端提示
	needTTY string // 交互模式需要真实终端，请使用 -e 或 --json

	// 危险检测（SQL 终端复用，同文案）
	// ---- \ai 状态与提示 ----
	aiStatusTitle    string // AI 状态: %s
	aiEnabled        string // 已启用
	aiNotConfigured  string // 未配置
	aiEndpoint       string //   端点:   %s
	aiModel          string //   模型:   %s
	aiTuning         string //   温度:   %.2f   上限: %d token   超时: %ds
	aiSchemaLimit    string //   表结构: 最多 %d 张表 / %d 字符
	aiDebugLog       string //   debug 日志: %s（全局开关，--debug 或 config 顶层 debug）
	aiDebugOn        string // 开启
	aiDebugOff       string // 关闭
	aiContext        string //   上下文: %d 条消息（含 system）
	aiProcTokens     string // 进程累计 token:
	aiProcTokensNone string // 进程累计 token: 尚无消耗
	aiResetDone      string // AI 会话已重置（对话上下文与 token 统计清零）
	aiCopyEmpty      string // 缓冲区为空：请先用 \ai <需求> 生成 SQL
	aiCopyFail       string // 复制到剪贴板失败: %s
	aiCopyOK         string // 已复制到系统剪贴板
	aiConfigTitle    string // AI 配置引导（直接回车保持原值，输入 . 退出）：
	aiConfigSaved    string // AI 配置已保存到 config.yaml
	aiSaveFail       string // 保存失败: %s
	aiInitFail       string // 初始化服务失败: %s
	aiGenerateEmpty  string // 请输入需求描述，如: \ai 查询最近 30 天订单量按天分组
	aiGenerating     string // 正在生成 SQL...
	aiGenerating2    string // 正在继续生成...
	aiExplaining     string // 正在解释 SQL...
	aiFixing         string // 正在修复 SQL...
	aiGenFail        string // 生成失败: %s
	aiExplainFail    string // 解释失败: %s
	aiFixFail        string // 修复失败: %s
	aiNoSQL          string // 请提供 SQL 或先用 \ai <需求> 生成
	aiContinueUsage  string // 用法: \ai continue <补充描述>
	aiFixDetailSQL   string // 原始 SQL：
	aiFixDetailErr   string // 报错信息：
	aiNoExecSQL      string // 模型未返回可执行的 SQL，原文如下：
	aiTokens         string // 本轮 token: 输入 %d / 输出 %d / 合计 %d
	aiRejected       string // 已拒绝：生成结果包含禁止操作
	aiWarning        string // 警告: %s
	aiGeneratedSQL   string // 生成的 SQL：
	aiBufferWritten  string // 已写入缓冲区。可用 \e 编辑、\g 执行、\ai continue 继续补充

	// 工具进度（stderr 提示，非模型可见）
	toolProgressListDBs    string // 正在列出可用的数据库
	toolProgressListTables string // 正在查询表列表
	toolProgressGetSchema  string // 正在查询表结构
	toolRunning            string //   ⟳ %s (%s)...
}

// cliTextsMap 语言注册表：缺失语言回退 zh。
var cliTextsMap = map[string]cliTexts{
	"zh": {
		bannerTitle:          "dbx sql - 交互式 SQL 终端",
		bannerConn:           "连接: %s @ %s:%d/%s",
		bannerHint:           "输入 \\h 查看帮助，\\q 退出",
		confirmWrite:         "确认执行写操作? [y/N] ",
		cancelled:            "已取消",
		errorPrefix:          "错误: %v",
		aiFixHint:            "提示: 输入 \\ai fix 可让 AI 根据该报错自动修复 SQL",
		autoLimit:            "（已自动追加 LIMIT %d）",
		rowsReturned:         "⚠ 已返回 %d 行%s",
		rowsTip:              "提示：使用 LIMIT n 指定返回行数，或加 WHERE 条件缩小范围",
		rowsTipNoTerm:        "提示：使用 LIMIT n 或 WHERE 缩小范围",
		timingOn:             "耗时显示: 开",
		timingOff:            "耗时显示: 关",
		xDisplaySaved:        "扩展显示: %s（已写入 config.yaml 作为默认）",
		xModeVertical:        "开（垂直显示）",
		xModeTable:           "关（表格显示）",
		xModeAuto:            "auto（表格超宽时自动垂直）",
		copyUsage:            "用法: \\copy <文件路径>",
		noTables:             "(无匹配表)",
		noColumns:            "(无列信息)",
		noIndexes:            "(无索引)",
		indexesTitle:         "索引:",
		failListTables:       "获取表列表失败: %v",
		failListDBs:          "获取数据库列表失败: %v",
		failTableMeta:        "获取表结构失败: %v",
		failIndexInfo:        "(无法获取索引信息: %v)",
		colName:              "列名",
		colType:              "类型",
		colNullable:          "可空",
		colDefault:           "默认值",
		colComment:           "说明",
		noExportSQL:          "没有上一条查询结果可导出",
		exportedRows:         "已导出 %d 行到 %s",
		execFile:             "执行 %s...",
		noEditSQL:            "没有上一条 SQL 可编辑",
		sqlUpdated:           "SQL 已更新",
		dangerFunc:           "检测到危险函数: %s",
		forbidFunc:           "检测到禁止函数: %s",
		noSQLProvided:        "请通过 -e / -f / 位置参数提供 SQL",
		needConn:             "请指定连接: -c <已保存连接> 或内联 --type/--host/--port/--un/--pw/--db",
		jsonNoWrite:          "JSON 模式下不允许写操作，请使用 --allow-write 确认",
		fileNoWrite:          "文件执行含写操作，请使用 --allow-write 确认",
		tableAutoVertical:    "⚠ 表格宽度超过终端，已自动切换为垂直显示（每行一个字段）",
		tableTooWide:         "⚠ 表格宽度超过终端（%d 列），建议用 \\G 垂直显示",
		tableTruncChars:      "…(共 %d 字符)",
		displayModeSaveFail:  "(提示：显示模式未能写入 config.yaml: %s)",
		tokenInOut:           "输入 %d / 输出 %d / 合计 %d",
		aiCfgAPIKey:          "api_key（输入新值覆盖，回车保持）",
		aiCfgMaxSchemaTables: "max_schema_tables（注入上下文的最大表数）",
		aiCfgMaxSchemaChars:  "max_schema_chars（表结构文本字符上限）",
		aiCfgSystemPrompt:    "system_prompt（输入 clear 清空，回车保持）",
		aiCfgDefaultPrompt:   "内置默认模板",
		needTTY:              "交互模式需要真实终端，请使用 -e 或 --json",
		metaHelp: `元命令:
  \\q, \\quit           退出
  \\dt, \\tables [pat]  列出表（支持通配符 * ?）
  \\d, \\desc  <表名>    查看表结构
  \\d+ <表名>           查看表结构（含索引/约束）
  \\l, \\list            列出数据库
  \\c, \\use  <库名>     切换数据库
  \\c                   查看当前连接信息
  \\timing              切换耗时显示
  \\g                   再次执行上一条 SQL（表格）
  \\G                   执行上一条 SQL 并垂直显示（每行一个字段）
  \\x [on|off|auto]     扩展显示：on=垂直 off=表格 auto=超宽自动（写入 config.yaml）
  \\p, \\print           打印当前缓冲区
  \\r, \\reset           清空缓冲区
  \\ai <需求>            AI 生成 SQL 到缓冲区（\\ai help 查看子命令）
  \\h, \\help            显示此帮助
  \\e, \\edit           用外部编辑器编辑上一条 SQL
  \\copy <文件>          导出上一条查询结果到文件（CSV）
  \\w <文件>             导出上一条查询结果到文件
  \\i <文件>             执行文件中的 SQL

快捷键:
  Enter (分号结尾)      执行 SQL
  Enter (无分号)        多行续写
  Ctrl+R               搜索历史
  Tab                  自动补全
  Ctrl+C               取消输入（空行退出）
  Ctrl+D               退出`,
		aiHelp: `AI 辅助 SQL（OpenAI 兼容协议，配置见 config.yaml ai 段或 Web 设置）:
  \\ai <需求>                 生成 SQL 到缓冲区（可 \\e 编辑后 \\g 执行）
  \\ai explain [SQL]          解释 SQL（缺省用缓冲区）
  \\ai fix [报错信息]      修复缓冲区 SQL（缺省自动附带上次执行报错）
  \\ai continue <补充>        基于上文继续补充生成
  \\ai copy                   复制缓冲区 SQL 到系统剪贴板
  \\ai status                 查看配置状态与 token 统计
  \\ai config                 引导式修改 AI 配置（写回 config.yaml）
  \\ai clear                  重置当前会话（清空上下文与 token 统计）
  \\ai help                   显示此帮助
生成时自动调用工具（list_databases / list_tables / get_schema）查询真实表结构，无需手动刷新`,
		aiStatusTitle:          "AI 状态: %s",
		aiEnabled:              "已启用",
		aiNotConfigured:        "未配置",
		aiEndpoint:             "  端点:   %s",
		aiModel:                "  模型:   %s",
		aiTuning:               "  温度:   %.2f   上限: %d token   超时: %ds",
		aiSchemaLimit:          "  表结构: 最多 %d 张表 / %d 字符",
		aiDebugLog:             "  debug 日志: %s（全局开关，--debug 或 config 顶层 debug）",
		aiDebugOn:              "开启",
		aiDebugOff:             "关闭",
		aiContext:              "  上下文: %d 条消息（含 system）",
		aiProcTokens:           "进程累计 token:",
		aiProcTokensNone:       "进程累计 token: 尚无消耗",
		aiResetDone:            "AI 会话已重置（对话上下文与 token 统计清零）",
		aiCopyEmpty:            "缓冲区为空：请先用 \\ai <需求> 生成 SQL",
		aiCopyFail:             "复制到剪贴板失败: %s",
		aiCopyOK:               "已复制到系统剪贴板",
		aiConfigTitle:          "AI 配置引导（直接回车保持原值，输入 . 退出）：",
		aiConfigSaved:          "AI 配置已保存到 config.yaml",
		aiSaveFail:             "保存失败: %s",
		aiInitFail:             "初始化服务失败: %s",
		aiGenerateEmpty:        "请输入需求描述，如: \\ai 查询最近 30 天订单量按天分组",
		aiGenerating:           "正在生成 SQL...",
		aiGenerating2:          "正在继续生成...",
		aiExplaining:           "正在解释 SQL...",
		aiFixing:               "正在修复 SQL...",
		aiGenFail:              "生成失败: %s",
		aiExplainFail:          "解释失败: %s",
		aiFixFail:              "修复失败: %s",
		aiNoSQL:                "请提供 SQL 或先用 \\ai <需求> 生成",
		aiContinueUsage:        "用法: \\ai continue <补充描述>",
		aiFixDetailSQL:         "原始 SQL：",
		aiFixDetailErr:         "报错信息：",
		aiNoExecSQL:            "模型未返回可执行的 SQL，原文如下：",
		aiTokens:               "本轮 token: 输入 %d / 输出 %d / 合计 %d",
		aiRejected:             "已拒绝：生成结果包含禁止操作",
		aiWarning:              "警告: %s",
		aiGeneratedSQL:         "生成的 SQL：",
		aiBufferWritten:        "已写入缓冲区。可用 \\e 编辑、\\g 执行、\\ai continue 继续补充",
		toolProgressListDBs:    "正在列出可用的数据库",
		toolProgressListTables: "正在查询表列表",
		toolProgressGetSchema:  "正在查询表结构",
		toolRunning:            "  ⟳ %s (%s)...",
	},
	"en": {
		bannerTitle:          "dbx sql - interactive SQL terminal",
		bannerConn:           "Connected: %s @ %s:%d/%s",
		bannerHint:           "Type \\h for help, \\q to quit",
		confirmWrite:         "Confirm write operation? [y/N] ",
		cancelled:            "cancelled",
		errorPrefix:          "Error: %v",
		aiFixHint:            "Tip: type \\ai fix to let AI fix the SQL based on this error",
		autoLimit:            " (LIMIT %d auto-appended)",
		rowsReturned:         "⚠ returned %d rows%s",
		rowsTip:              "Tip: use LIMIT n to limit rows, or add a WHERE clause to narrow the result",
		rowsTipNoTerm:        "Tip: use LIMIT n or WHERE to narrow the result",
		timingOn:             "timing: on",
		timingOff:            "timing: off",
		xDisplaySaved:        "expanded display: %s (saved to config.yaml as default)",
		xModeVertical:        "on (vertical)",
		xModeTable:           "off (table)",
		xModeAuto:            "auto (vertical when too wide)",
		copyUsage:            "usage: \\copy <file path>",
		noTables:             "(no matching tables)",
		noColumns:            "(no column info)",
		noIndexes:            "(no indexes)",
		indexesTitle:         "indexes:",
		failListTables:       "failed to list tables: %v",
		failListDBs:          "failed to list databases: %v",
		failTableMeta:        "failed to get table structure: %v",
		failIndexInfo:        "(failed to fetch index info: %v)",
		colName:              "column",
		colType:              "type",
		colNullable:          "nullable",
		colDefault:           "default",
		colComment:           "comment",
		noExportSQL:          "no previous query result to export",
		exportedRows:         "exported %d rows to %s",
		execFile:             "executing %s...",
		noEditSQL:            "no previous SQL to edit",
		sqlUpdated:           "SQL updated",
		dangerFunc:           "dangerous function detected: %s",
		forbidFunc:           "forbidden function detected: %s",
		noSQLProvided:        "provide SQL via -e / -f / positional argument",
		needConn:             "specify a connection: -c <saved connection> or inline --type/--host/--port/--un/--pw/--db",
		jsonNoWrite:          "write operations are not allowed in JSON mode, use --allow-write to confirm",
		fileNoWrite:          "the file contains write operations, use --allow-write to confirm",
		tableAutoVertical:    "⚠ table width exceeds the terminal; switched to vertical display (one field per line)",
		tableTooWide:         "⚠ table width exceeds the terminal (%d columns), consider \\G vertical display",
		tableTruncChars:      "…(%d chars total)",
		displayModeSaveFail:  "(note: display mode could not be written to config.yaml: %s)",
		tokenInOut:           "%d in / %d out / %d total",
		aiCfgAPIKey:          "api_key (enter new value to overwrite, Enter keeps current)",
		aiCfgMaxSchemaTables: "max_schema_tables (max tables injected into context)",
		aiCfgMaxSchemaChars:  "max_schema_chars (schema text char limit)",
		aiCfgSystemPrompt:    "system_prompt (type clear to reset, Enter keeps current)",
		aiCfgDefaultPrompt:   "built-in default template",
		needTTY:              "interactive mode requires a real terminal, use -e or --json",
		metaHelp: `Meta commands:
  \\q, \\quit           quit
  \\dt, \\tables [pat]  list tables (wildcards * ? supported)
  \\d, \\desc  <table>   describe table structure
  \\d+ <table>           describe table structure (with indexes/constraints)
  \\l, \\list            list databases
  \\c, \\use  <db>       switch database
  \\c                   show current connection info
  \\timing              toggle timing display
  \\g                   re-run the last SQL (table)
  \\G                   re-run the last SQL vertically (one field per line)
  \\x [on|off|auto]     expanded display: on=vertical off=table auto=auto-when-wide (saved to config.yaml)
  \\p, \\print           print current buffer
  \\r, \\reset           clear buffer
  \\ai <request>         AI generates SQL into buffer (\\ai help for subcommands)
  \\h, \\help            show this help
  \\e, \\edit           edit the last SQL in an external editor
  \\copy <file>          export the last query result to a file (CSV)
  \\w <file>             export the last query result to a file
  \\i <file>             execute SQL from a file

Shortcuts:
  Enter (ends with ;)   execute SQL
  Enter (no ;)          continue on the next line
  Ctrl+R                search history
  Tab                   auto completion
  Ctrl+C                cancel input (quit on empty line)
  Ctrl+D                quit`,
		aiHelp: `AI-assisted SQL (OpenAI-compatible protocol; configure in config.yaml ai section or Web settings):
  \\ai <request>               generate SQL into buffer (edit with \\e, run with \\g)
  \\ai explain [SQL]           explain SQL (defaults to buffer)
  \\ai fix [error message]     fix buffer SQL (defaults to last execution error)
  \\ai continue <follow-up>    continue generating based on the context
  \\ai copy                    copy buffer SQL to the system clipboard
  \\ai status                  show config status and token stats
  \\ai config                  guided AI config editing (writes config.yaml)
  \\ai clear                   reset the current session (clears context and token stats)
  \\ai help                    show this help
Tools (list_databases / list_tables / get_schema) are called automatically to query real table structures; no manual refresh needed`,
		aiStatusTitle:          "AI status: %s",
		aiEnabled:              "enabled",
		aiNotConfigured:        "not configured",
		aiEndpoint:             "  endpoint: %s",
		aiModel:                "  model:   %s",
		aiTuning:               "  temperature: %.2f   max tokens: %d   timeout: %ds",
		aiSchemaLimit:          "  schema: up to %d tables / %d chars",
		aiDebugLog:             "  debug log: %s (global switch, --debug or config top-level debug)",
		aiDebugOn:              "on",
		aiDebugOff:             "off",
		aiContext:              "  context: %d messages (incl. system)",
		aiProcTokens:           "process total tokens:",
		aiProcTokensNone:       "process total tokens: none yet",
		aiResetDone:            "AI session reset (context and token stats cleared)",
		aiCopyEmpty:            "buffer is empty: generate SQL first with \\ai <request>",
		aiCopyFail:             "failed to copy to clipboard: %s",
		aiCopyOK:               "copied to system clipboard",
		aiConfigTitle:          "AI config wizard (Enter keeps the current value, type . to quit):",
		aiConfigSaved:          "AI config saved to config.yaml",
		aiSaveFail:             "failed to save: %s",
		aiInitFail:             "failed to initialize service: %s",
		aiGenerateEmpty:        "describe what you need, e.g. \\ai count orders in the last 30 days grouped by day",
		aiGenerating:           "generating SQL...",
		aiGenerating2:          "continuing...",
		aiExplaining:           "explaining SQL...",
		aiFixing:               "fixing SQL...",
		aiGenFail:              "generate failed: %s",
		aiExplainFail:          "explain failed: %s",
		aiFixFail:              "fix failed: %s",
		aiNoSQL:                "provide SQL or generate it first with \\ai <request>",
		aiContinueUsage:        "usage: \\ai continue <follow-up description>",
		aiFixDetailSQL:         "original SQL:",
		aiFixDetailErr:         "error message:",
		aiNoExecSQL:            "model returned no executable SQL; raw output:",
		aiTokens:               "tokens: %d in / %d out / %d total",
		aiRejected:             "rejected: generated result contains forbidden operations",
		aiWarning:              "warning: %s",
		aiGeneratedSQL:         "generated SQL:",
		aiBufferWritten:        "written to buffer. Use \\e to edit, \\g to run, \\ai continue to extend",
		toolProgressListDBs:    "listing databases",
		toolProgressListTables: "querying tables",
		toolProgressGetSchema:  "querying table structure",
		toolRunning:            "  ⟳ %s (%s)...",
	},
}

// cliTextsFor 按语言取 CLI 输出文案，缺失语言回退 zh。
func cliTextsFor(lang string) cliTexts {
	if t, ok := cliTextsMap[llm.NormLang(lang)]; ok {
		return t
	}
	return cliTextsMap["zh"]
}
