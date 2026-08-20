package sqlcmd

// sql 命令帮助文本注册表（Short/Long/flag usage 双语）。
// 本包自包含（不引用父包 internal/cli），由父包 applyHelpLang 在帮助渲染前调用
// ApplyHelpLang 重写文本；注册表缺条目时保持定义时中文（zh 基准）。

import (
	"dbimpex/internal/llm"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type sqlHelpTexts struct {
	short string
	long  string
	flags map[string]string // key: flag 长名（--xxx）
}

// sqlHelpMap 帮助文本语言注册表：新增语言只加条目，结构对齐 zh
var sqlHelpMap = map[string]sqlHelpTexts{
	"zh": {
		short: "SQL 交互终端 / 执行 SQL 查询",
		long: `交互式数据库 SQL 终端，支持表格渲染与 JSON 输出。

使用方式：
  dbx sql -c <连接>              交互式 REPL 终端
  dbx sql -c <连接> -e "SQL"     单次执行（表格输出）
  dbx sql -c <连接> --json "SQL" JSON 输出（智能体友好）`,
		flags: map[string]string{
			"conn":        "已保存连接（支持 ID、名称或短名）",
			"execute":     "执行 SQL 后退出",
			"file":        "从文件读取 SQL 执行",
			"json":        "JSON 格式输出",
			"timeout":     "查询超时",
			"max-rows":    "最大返回行数",
			"allow-write": "--json/-f 模式下允许执行写操作",
			"ssl-ca":      "TLS CA 证书路径",
			"no-color":    "禁用颜色输出",
			"type":        " 数据库类型(mysql/postgresql/oracle)",
			"host":        " 主机",
			"port":        " 端口",
			"un":          " 用户名",
			"pw":          " 密码",
			"db":          " 数据库名",
			"subtype":     " 数据库产品（兼容库，如 oceanbase/gaussdb/dameng，留空=原生）",
			"user":        "用户名（同 un）",
			"password":    "密码（同 pw）",
			"database":    "数据库名（同 db）",
			"help":        "查看 sql 命令帮助",
		},
	},
	"en": {
		short: "SQL interactive terminal / run SQL queries",
		long: `Interactive database SQL terminal with table rendering and JSON output.

Usage:
  dbx sql -c <conn>              interactive REPL terminal
  dbx sql -c <conn> -e "SQL"     single run (table output)
  dbx sql -c <conn> --json "SQL" JSON output (agent friendly)`,
		flags: map[string]string{
			"conn":        "Saved connection (ID, name or short name)",
			"execute":     "Run SQL and exit",
			"file":        "Read SQL from file and run",
			"json":        "JSON format output",
			"timeout":     "Query timeout",
			"max-rows":    "Max rows returned",
			"allow-write": "Allow write operations in --json/-f mode",
			"ssl-ca":      "TLS CA certificate path",
			"no-color":    "Disable colored output",
			"type":        " Database type (mysql/postgresql/oracle)",
			"host":        " Host",
			"port":        " Port",
			"un":          " Username",
			"pw":          " Password",
			"db":          " Database name",
			"subtype":     " Database product (compat, e.g. oceanbase/gaussdb/dameng; empty = native)",
			"user":        "Username (same as un)",
			"password":    "Password (same as pw)",
			"database":    "Database name (same as db)",
			"help":        "Show help for the sql command",
		},
	},
}

// sqlHelpFor 取语言帮助文本，未知语言回退 zh
func sqlHelpFor(lang string) sqlHelpTexts {
	if h, ok := sqlHelpMap[llm.NormLang(lang)]; ok {
		return h
	}
	return sqlHelpMap["zh"]
}

// ApplyHelpLang 按语言重写 sql 命令的 Short/Long/flag usage（供父包 applyHelpLang 调用）
func ApplyHelpLang(cmd *cobra.Command, lang string) {
	h := sqlHelpFor(lang)
	cmd.Short = h.short
	cmd.Long = h.long
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		if u, ok := h.flags[f.Name]; ok {
			f.Usage = u
		}
	})
}
