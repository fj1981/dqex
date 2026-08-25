package cli

// 命令帮助文本注册表（cobra Short/Long/flag usage 双语）。
// cobra 的 Short/Long/Usage 是静态字段，命令定义时（init）无法感知 --lang，
// 因此在帮助/用法渲染前由 applyHelpLang 按当前语言重写命令树文本；
// 注册表缺条目时保持定义时中文（zh 基准），新增语言只加 map 条目。

import (
	"dqex/internal/cli/sqlcmd"
	"dqex/internal/llm"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type cliHelpCmd struct {
	short string
	long  string
	flags map[string]string // key: flag 长名（--xxx）
}

type cliHelpTexts struct {
	cmds map[string]cliHelpCmd // key: 命令路径（"root"、"conn add"、"export"）
}

// cliHelpMap 帮助文本语言注册表：新增语言只加条目，结构对齐 zh
var cliHelpMap = map[string]cliHelpTexts{
	"zh": zhHelpTexts(),
	"en": enHelpTexts(),
}

// cliHelpFor 取语言帮助文本，未知语言回退 zh
func cliHelpFor(lang string) cliHelpTexts {
	if h, ok := cliHelpMap[llm.NormLang(lang)]; ok {
		return h
	}
	return cliHelpMap["zh"]
}

// cmdKey 命令路径 key：root 用 "root"，一级子命令用 Use 名（"conn"），
// 更深层用父 key + 空格 + Use 名（"conn add"）；与注册表 key 对齐
func cmdKey(c *cobra.Command) string {
	if c == rootCmd {
		return "root"
	}
	if c.Parent() == nil || c.Parent() == rootCmd {
		return c.Name()
	}
	return cmdKey(c.Parent()) + " " + c.Name()
}

// applyHelpLang 按语言重写 rootCmd 命令树的 Short/Long/flag usage；
// sql 命令的帮助文本在 sqlcmd 包注册，单独处理
func applyHelpLang(lang string) {
	h := cliHelpFor(lang)
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if e, ok := h.cmds[cmdKey(c)]; ok {
			if e.short != "" {
				c.Short = e.short
			}
			if e.long != "" {
				c.Long = e.long
			}
			if len(e.flags) > 0 {
				c.Flags().VisitAll(func(f *pflag.Flag) {
					if u, ok := e.flags[f.Name]; ok {
						f.Usage = u
					}
				})
			}
		}
		if c.Name() == "sql" {
			sqlcmd.ApplyHelpLang(c, lang)
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk(rootCmd)
}

func mergeMaps(ms ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, m := range ms {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// ---- zh ----

func zhConnFlags(prefix string) map[string]string {
	return map[string]string{
		prefix + "-type":    prefix + " 数据库类型(mysql/postgresql/oracle)",
		prefix + "-host":    prefix + " 主机",
		prefix + "-port":    prefix + " 端口",
		prefix + "-un":      prefix + " 用户名",
		prefix + "-pw":      prefix + " 密码",
		prefix + "-db":      prefix + " 数据库名",
		prefix + "-subtype": prefix + " 数据库产品（兼容库，如 oceanbase/gaussdb/dameng，留空=原生）",
	}
}

func zhConnAliases(aliasPrefix, refPrefix string) map[string]string {
	m := map[string]string{}
	if aliasPrefix == "" {
		m["host"] = "主机（同 " + refPrefix + "host）"
		m["port"] = "端口（同 " + refPrefix + "port）"
		m["user"] = "用户名（同 " + refPrefix + "un）"
		m["password"] = "密码（同 " + refPrefix + "pw）"
	} else {
		m[aliasPrefix+"user"] = aliasPrefix + "用户名（同 " + refPrefix + "un）"
		m[aliasPrefix+"password"] = aliasPrefix + "密码（同 " + refPrefix + "pw）"
	}
	m[aliasPrefix+"database"] = aliasPrefix + "数据库名（同 " + refPrefix + "db）"
	return m
}

func zhHelpTexts() cliHelpTexts {
	return cliHelpTexts{cmds: map[string]cliHelpCmd{
		"root": {
			short: "数据库导入/导出/迁移/对比工具",
			long: `dqex - 数据库导入/导出/迁移/对比工具

不带子命令时启动 Web 服务（默认端口 8181）。
CLI 子命令与 Web 功能对齐：export / import / migrate / compare / dictionary / snapshot / conn / task / history`,
			flags: map[string]string{
				"host":        "Web 服务监听地址（默认仅本机回环；对外暴露用 0.0.0.0，外部来源强制令牌认证）",
				"port":        "Web 服务端口",
				"allow":       "访问来源白名单（IP/CIDR/域名，逗号分隔；对外暴露时必须配置，留空则外部来源一律拒绝、仅本机可访问；优先于配置 web.allow；本机回环始终放行）",
				"no-auth":     "完全禁用令牌认证",
				"no-browser":  "启动时不自动打开浏览器",
				"data-dir":    "数据根目录（默认取全局配置，否则 ~/.dqex）",
				"config-file": "全局配置文件（默认 环境变量 dqex_CONFIG 或 ~/.dqex/config.yaml）",
				"debug":       "输出 debug 及以上级别的日志（含 AI 调用；等效配置文件顶层 debug: true）",
				"lang":        "CLI 输出语言（zh/en；环境变量 DBX_LANG，默认 zh）",
			},
		},
		"conn": {
			short: "数据库连接管理（list/add/test/delete）",
		},
		"conn list": {
			short: "列出已保存连接",
		},
		"conn add": {
			short: "新增或更新连接配置",
			flags: mergeMaps(zhConnFlags(""), zhConnAliases("", ""), map[string]string{
				"name":       "连接名称（展示用）",
				"short-name": "短名/简写（命令行快速引用，仅允许字母数字连字符下划线）",
				"id":         "按 ID 更新已有连接",
				"env":        "环境标签：dev/test/staging/prod",
			}),
		},
		"conn test": {
			short: "测试连接可用性",
			flags: map[string]string{"conn": "已保存连接（ID 或名称）"},
		},
		"conn delete": {
			short: "删除连接配置",
			flags: map[string]string{"conn": "已保存连接（ID 或名称）"},
		},
		"config": {
			short: "查看全局配置（数据目录等）",
			long: `查看解析后的全局配置（四类数据目录）。

全局配置文件为 config.yaml，查找顺序：
  --config-file > 环境变量 dqex_CONFIG > ~/.dqex/config.yaml

目录优先级：--data-dir > 配置文件 dirs.data > 默认 ~/.dqex；
其余目录：配置文件显式值 > 由 data 目录派生。
使用 dqex config --gen 输出配置模板。`,
			flags: map[string]string{"gen": "输出全局配置模板到标准输出"},
		},
		"url": {
			short: "输出 Web 访问链接（带 token）",
			long: `输出当前数据目录下的 Web 访问链接（带 token），可直接在浏览器打开或用于 API 调试。
令牌每次启动自动重新生成（不读盘复用），有效期 24 小时；重启即刷新。
删除数据目录下的数据文件不影响启动（下次启动自动重建）。

示例：
  dqex url                                    # 完整访问链接
  dqex url --token-only                       # 仅输出 token
  curl -H "Authorization: Bearer $(dqex url --token-only)" http://127.0.0.1:8181/api/connections`,
			flags: map[string]string{"token-only": "仅输出 token（便于脚本/API 调试）"},
		},
		"version": {short: "查看版本号"},
		"stop":    {short: "终止其他 dqex 进程"},
		"task":    {short: "任务配置管理（list/show/run/save/delete）"},
		"task list": {
			short: "列出任务配置",
			flags: map[string]string{"type": "按类型过滤: export|import|migrate|compare"},
		},
		"task show":   {short: "查看任务配置详情（yaml）", flags: map[string]string{"id": "任务配置 ID"}},
		"task run":    {short: "执行已保存的任务配置（同步）", flags: map[string]string{"id": "任务配置 ID"}},
		"task save":   {short: "从配置文件保存任务配置", flags: map[string]string{"name": "任务名称", "config": "配置文件(yaml)，支持独立格式与旧嵌套格式", "type": "配置类型: export|import|migrate|compare（默认自动识别）"}},
		"task delete": {short: "删除任务配置", flags: map[string]string{"id": "任务配置 ID"}},
		"snapshot": {
			short: "管理数据库快照：创建、列表、查看、删除、对比",
			long: `管理数据库快照，支持与当前数据库状态对比。

独立闭环用法：
  dqex snapshot create -c <连接名> -d <数据库> -n <名称>     # 创建快照
  dqex snapshot list                                           # 列出所有快照
  dqex snapshot show -i <快照ID>                               # 查看快照详情
  dqex snapshot delete -i <快照ID>                             # 删除快照
  dqex snapshot compare -i <快照ID> -c <连接名> -d <数据库>    # 快照 vs 当前库`,
		},
		"snapshot create": {
			short: "创建数据库快照",
			long:  "连接数据库并创建快照（记录所有表结构 + 行数统计）",
			flags: map[string]string{
				"conn":         "已保存连接名（ID 或名称）",
				"database":     "数据库名，逗号分隔支持多库（留空=使用连接默认库）",
				"name":         "快照名称",
				"desc":         "备注说明",
				"samples":      "保存前 N 行数据采样",
				"sample-limit": "每表采样行数（<=0 用默认 10，仅在 --samples 开启时生效）",
			},
		},
		"snapshot list":   {short: "列出所有快照"},
		"snapshot show":   {short: "查看快照详情", flags: map[string]string{"id": "快照 ID"}},
		"snapshot delete": {short: "删除快照", flags: map[string]string{"id": "快照 ID"}},
		"snapshot compare": {
			short: "快照与当前数据库对比",
			long:  "将快照的表结构与当前数据库进行对比，输出差异报告",
			flags: map[string]string{
				"id":          "快照 ID",
				"target-conn": "目标连接名（ID 或名称）",
				"database":    "目标数据库名（覆盖目标连接默认库）",
				"db-map":      "快照库→目标库映射，逗号分隔的 源库=目标库 对（如 db1=db2,db3=db4）",
				"output":      "对比报告 JSON 额外保存路径",
			},
		},
		"history": {short: "执行历史管理（list/show/delete）"},
		"history list": {
			short: "列出执行历史",
			flags: map[string]string{"type": "按类型过滤: export|import|migrate|compare"},
		},
		"history show":   {short: "查看执行记录详情", flags: map[string]string{"id": "执行记录 ID"}},
		"history delete": {short: "删除执行记录（运行中的任务不允许删除）", flags: map[string]string{"id": "执行记录 ID"}},
		"compare": {
			short: "对比两个数据库的结构与数据差异",
			long: `对比两个数据库的结构与数据差异。

独立闭环用法：
  dqex compare --gen-config > compare.yaml   # 生成配置模板
  vi compare.yaml                               # 填写连接与选项
  dqex compare --config compare.yaml         # 执行对比

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--source-* / --target-*）。
连接 flag 提供 mysqldump 风格别名：--source-user/--source-password/--source-database（及 target- 同名系列）`,
			flags: mergeMaps(zhConnFlags("source"), zhConnFlags("target"), zhConnAliases("source-", "source-"), zhConnAliases("target-", "target-"), map[string]string{
				"config":         "配置文件(yaml)，dqex compare --gen-config 可生成模板",
				"gen-config":     "输出配置文件模板到标准输出",
				"task":           "已保存任务配置 ID（Web 保存的任务）",
				"source-conn":    "使用已保存源连接（ID 或名称）",
				"target-conn":    "使用已保存目标连接（ID 或名称）",
				"tables":         "指定对比的表，逗号分隔（默认库内全部表）",
				"alias":          "表别名配对，格式 源表=目标表（可重复）",
				"scope":          "对比范围: both|structure|data",
				"structure-only": "仅对比结构（等价 --scope structure）",
				"data-only":      "仅对比数据（等价 --scope data）",
				"threshold":      "数据逐行比较阈值（默认 1000，超过仅比行数）",
				"ignore-columns": "数据内容对比忽略的列，逗号分隔（如 created_at,updated_at）",
				"force-data":     "表结构不一致时仍强制对比数据（默认跳过数据对比）",
				"output":         "对比报告 JSON 额外保存路径（除历史记录自带的报告外）",
				"all":            "输出全部表（默认仅输出有差异的表）",
			}),
		},
		"compare show": {
			short: "回看历史对比记录的差异明细",
			long: `回看历史对比记录的差异明细。

dqex cmp show -i <记录ID>                # 全部表明细
dqex cmp show -i <记录ID> t_config       # 单表差异明细（位置参数或 --table/-t 均可，列级对照 + 差异行样例）`,
			flags: map[string]string{
				"id":    "对比记录 ID（compare 执行后输出，或 history list --type compare 查看）",
				"table": "仅显示指定表的差异明细",
			},
		},
		"export": {
			short: "导出数据库结构及数据为 SQL（可压缩为 zip / gzip）",
			long: `导出数据库结构及数据为 SQL（可压缩为 zip / gzip）。

独立闭环用法：
  dqex export --gen-config > export.yaml   # 生成配置模板
  vi export.yaml                              # 填写连接与选项
  dqex export --config export.yaml         # 执行导出

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--source-*）。
连接与常用 flag 提供 mysqldump 风格别名：
  --host/--port/--user/--password/--database、--no-data、--no-create-info
  短参：-h/-P/-u/-p（主机/端口/用户/密码）、-s（已保存连接）、-o（输出）、-T（表）
  位置参数 [库名 [表名...]] 等同 --databases/--tables`,
			flags: mergeMaps(zhConnFlags("source"), zhConnAliases("", "source-"), map[string]string{
				"config":             "配置文件(yaml)，dqex export --gen-config 可生成模板",
				"gen-config":         "输出配置文件模板到标准输出",
				"task":               "已保存任务配置 ID（Web 保存的任务）",
				"source-conn":        "使用已保存连接（ID 或名称），与 --source-* 同时给出时后者优先",
				"output":             "输出目录或 .zip 文件路径",
				"name":               "任务名（用于生成文件名）",
				"databases":          "指定库，逗号分隔",
				"tables":             "指定表，逗号分隔",
				"objects":            "指定对象，逗号分隔，格式 类别/名称（类别：_views/_functions/_procedures）",
				"table-cond":         "表过滤条件，格式 表名:完整SELECT（可重复，兼容旧格式 表名:WHERE片段）",
				"schema-only":        "仅导出结构",
				"data-only":          "仅导出数据",
				"no-data":            "不导出数据，仅导出结构（同 --schema-only，mysqldump 风格）",
				"no-create-info":     "不导出建表语句，仅导出数据（同 --data-only，mysqldump 风格）",
				"compress":           "打包为 zip",
				"gzip":               "SQL 文件 gzip 压缩为 .sql.gz（与 --compress 可同时开启；导入时自动解压）",
				"single-transaction": "一致性快照导出（等同 mysqldump --single-transaction，仅 MySQL/PostgreSQL 生效）",
				"batch-size":         "批量大小",
				"help":               "查看 export 命令帮助",
			}),
		},
		"import": {
			short: "导入 SQL 文件（.sql / .sql.gz / .zip）到数据库",
			long: `导入 SQL 文件（.sql / .sql.gz / .zip）到数据库。

独立闭环用法：
  dqex import --gen-config > import.yaml   # 生成配置模板
  vi import.yaml                              # 填写连接与选项
  dqex import --config import.yaml         # 执行导入

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--target-*）。
连接 flag 提供 mysqldump 风格别名：--host/--port/--user/--password/--database
短参：-h/-P/-u/-p（主机/端口/用户/密码）、-t（已保存连接）、-i（导入文件）`,
			flags: mergeMaps(zhConnFlags("target"), zhConnAliases("", "target-"), map[string]string{
				"config":      "配置文件(yaml)，dqex import --gen-config 可生成模板",
				"gen-config":  "输出配置文件模板到标准输出",
				"task":        "已保存任务配置 ID（Web 保存的任务）",
				"target-conn": "使用已保存连接（ID 或名称），与 --target-* 同时给出时后者优先",
				"input":       "导入文件(.sql / .sql.gz / .zip)",
				"reset":       "重置模式: truncate|drop（默认不重置）",
				"backup":      "重置前创建备份表",
				"batch-size":  "批量大小",
				"help":        "查看 import 命令帮助",
			}),
		},
		"migrate": {
			short: "数据库迁移（支持跨类型）",
			long: `数据库迁移（支持跨类型）。

独立闭环用法：
  dqex migrate --gen-config > migrate.yaml   # 生成配置模板
  vi migrate.yaml                               # 填写连接与选项
  dqex migrate --config migrate.yaml         # 执行迁移

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--source-* / --target-*）。
连接 flag 提供 mysqldump 风格别名：--source-user/--source-password/--source-database（及 target- 同名系列）`,
			flags: mergeMaps(zhConnFlags("source"), zhConnFlags("target"), zhConnAliases("source-", "source-"), zhConnAliases("target-", "target-"), map[string]string{
				"config":      "配置文件(yaml)，dqex migrate --gen-config 可生成模板",
				"gen-config":  "输出配置文件模板到标准输出",
				"task":        "已保存任务配置 ID（Web 保存的任务）",
				"source-conn": "使用已保存源连接（ID 或名称）",
				"target-conn": "使用已保存目标连接（ID 或名称）",
				"tables":      "指定表，逗号分隔",
				"objects":     "指定对象，逗号分隔，格式 类别/名称（类别：_views/_functions/_procedures；仅同类型迁移生效）",
				"table-cond":  "表过滤条件，格式 表名:完整SELECT（可重复，兼容旧格式 表名:WHERE片段）",
				"schema-only": "仅迁移结构",
				"data-only":   "仅迁移数据",
				"reset":       "重置模式: truncate|drop（默认不重置）",
				"backup":      "重置前创建备份表",
				"batch-size":  "批量大小",
			}),
		},
		"dictionary": {
			short: "生成数据库表/字段数据字典为 Excel（.xlsx，含注释）",
			long: `生成数据库表/字段数据字典为 Excel（.xlsx，含表/列注释），可压缩为 zip。
产物为单个 Excel 工作簿：总览页（全实例表清单，超链接跳转）+ 每库一个字段明细页。

独立闭环用法：
  dqex dictionary --gen-config > dictionary.yaml   # 生成配置模板
  vi dictionary.yaml                                # 填写连接与选项
  dqex dictionary --config dictionary.yaml         # 执行生成

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--source-*）。
  短参：-h/-P/-u/-p（主机/端口/用户/密码）、-s（已保存连接）、-o（输出）、-T（表）
  位置参数 [库名 [表名...]] 等同 --databases/--tables`,
			flags: mergeMaps(zhConnFlags("source"), zhConnAliases("", "source-"), map[string]string{
				"config":      "配置文件(yaml)，dqex dictionary --gen-config 可生成模板",
				"gen-config":  "输出配置文件模板到标准输出",
				"task":        "已保存任务配置 ID（Web 保存的任务）",
				"source-conn": "使用已保存连接（ID 或名称），与 --source-* 同时给出时后者优先",
				"output":      "输出目录或 .zip 文件路径",
				"name":        "任务名（用于生成文件名）",
				"databases":   "指定库，逗号分隔",
				"tables":      "指定表，逗号分隔",
				"compress":    "打包为 zip",
				"help":        "查看 dictionary 命令帮助",
			}),
		},
	}}
}

// ---- en ----

func enConnFlags(prefix string) map[string]string {
	return map[string]string{
		prefix + "-type":    prefix + " Database type (mysql/postgresql/oracle)",
		prefix + "-host":    prefix + " Host",
		prefix + "-port":    prefix + " Port",
		prefix + "-un":      prefix + " Username",
		prefix + "-pw":      prefix + " Password",
		prefix + "-db":      prefix + " Database name",
		prefix + "-subtype": prefix + " Database product (compat, e.g. oceanbase/gaussdb/dameng; empty = native)",
	}
}

func enConnAliases(aliasPrefix, refPrefix string) map[string]string {
	m := map[string]string{}
	if aliasPrefix == "" {
		m["host"] = "Host (same as " + refPrefix + "host)"
		m["port"] = "Port (same as " + refPrefix + "port)"
		m["user"] = "Username (same as " + refPrefix + "un)"
		m["password"] = "Password (same as " + refPrefix + "pw)"
	} else {
		m[aliasPrefix+"user"] = aliasPrefix + "Username (same as " + refPrefix + "un)"
		m[aliasPrefix+"password"] = aliasPrefix + "Password (same as " + refPrefix + "pw)"
	}
	m[aliasPrefix+"database"] = aliasPrefix + "Database name (same as " + refPrefix + "db)"
	return m
}

func enHelpTexts() cliHelpTexts {
	return cliHelpTexts{cmds: map[string]cliHelpCmd{
		"root": {
			short: "Database import/export/migrate/compare tool",
			long: `dqex - Database import/export/migrate/compare tool

Starts the Web service (default port 8181) when invoked without a subcommand.
CLI subcommands align with Web features: export / import / migrate / compare / dictionary / snapshot / conn / task / history`,
			flags: map[string]string{
				"host":        "Web listen address (loopback only by default; use 0.0.0.0 to expose, which requires token auth for external access)",
				"port":        "Web service port",
				"allow":       "Allowed origin whitelist (IP/CIDR/domain, comma-separated; required when exposing the service, empty = external access denied, loopback-only; takes priority over config web.allow; loopback always allowed)",
				"no-auth":     "Fully disable token auth",
				"no-browser":  "Do not auto-open the browser on startup",
				"data-dir":    "Data root directory (default from global config, else ~/.dqex)",
				"config-file": "Global config file (default env dqex_CONFIG or ~/.dqex/config.yaml)",
				"debug":       "Output debug-level or higher logs (incl. AI calls; equivalent to config top-level debug: true)",
				"lang":        "CLI output language (zh/en; env DBX_LANG, default zh)",
			},
		},
		"conn": {
			short: "Connection management (list/add/test/delete)",
		},
		"conn list": {
			short: "List saved connections",
		},
		"conn add": {
			short: "Add or update a connection",
			flags: mergeMaps(enConnFlags(""), enConnAliases("", ""), map[string]string{
				"name":       "Connection name (display)",
				"short-name": "Short name/alias (for quick CLI reference; letters/digits/hyphen/underscore only)",
				"id":         "Update an existing connection by ID",
				"env":        "Environment tag: dev/test/staging/prod",
			}),
		},
		"conn test": {
			short: "Test connection availability",
			flags: map[string]string{"conn": "Saved connection (ID or name)"},
		},
		"conn delete": {
			short: "Delete a connection",
			flags: map[string]string{"conn": "Saved connection (ID or name)"},
		},
		"config": {
			short: "Show global config (data dirs, etc.)",
			long: `Show the resolved global config (four data directories).

Global config file is config.yaml, resolved in order:
  --config-file > env dqex_CONFIG > ~/.dqex/config.yaml

Directory priority: --data-dir > config dirs.data > default ~/.dqex;
other dirs: explicit config values > derived from the data dir.
Use dqex config --gen to print the config template.`,
			flags: map[string]string{"gen": "Print the global config template to stdout"},
		},
		"url": {
			short: "Print the Web access URL (with token)",
			long: `Print the Web access URL (with token) for the current data dir; open it in a browser or use for API debugging.
The token is regenerated on every startup (not read from disk), valid for 24 hours; restarts refresh it.
Deleting the data files under the data dir does not affect startup (recreated on next start).

Examples:
  dqex url                                    # full access URL
  dqex url --token-only                       # print token only
  curl -H "Authorization: Bearer $(dqex url --token-only)" http://127.0.0.1:8181/api/connections`,
			flags: map[string]string{"token-only": "Print token only (for scripts/API debugging)"},
		},
		"version": {short: "Show version"},
		"stop":    {short: "Terminate other dqex processes"},
		"task":    {short: "Task config management (list/show/run/save/delete)"},
		"task list": {
			short: "List task configs",
			flags: map[string]string{"type": "Filter by type: export|import|migrate|compare"},
		},
		"task show":   {short: "Show task config details (yaml)", flags: map[string]string{"id": "Task config ID"}},
		"task run":    {short: "Run a saved task config (sync)", flags: map[string]string{"id": "Task config ID"}},
		"task save":   {short: "Save a task config from a config file", flags: map[string]string{"name": "Task name", "config": "Config file (yaml), supports standalone and legacy nested formats", "type": "Config type: export|import|migrate|compare (auto-detected by default)"}},
		"task delete": {short: "Delete a task config", flags: map[string]string{"id": "Task config ID"}},
		"snapshot": {
			short: "Manage database snapshots: create, list, show, delete, compare",
			long: `Manage database snapshots, with comparison against the current database state.

Standalone usage:
  dqex snapshot create -c <conn> -d <db> -n <name>     # create a snapshot
  dqex snapshot list                                   # list all snapshots
  dqex snapshot show -i <snapshotID>                   # show snapshot details
  dqex snapshot delete -i <snapshotID>                 # delete a snapshot
  dqex snapshot compare -i <snapshotID> -c <conn> -d <db>    # snapshot vs current db`,
		},
		"snapshot create": {
			short: "Create a database snapshot",
			long:  "Connect to the database and create a snapshot (records all table structures + row counts)",
			flags: map[string]string{
				"conn":         "Saved connection name (ID or name)",
				"database":     "Database name, comma-separated for multiple (empty = connection default db)",
				"name":         "Snapshot name",
				"desc":         "Description",
				"samples":      "Save sample data of first N rows",
				"sample-limit": "Sample rows per table (<=0 uses default 10, effective only with --samples)",
			},
		},
		"snapshot list":   {short: "List all snapshots"},
		"snapshot show":   {short: "Show snapshot details", flags: map[string]string{"id": "Snapshot ID"}},
		"snapshot delete": {short: "Delete a snapshot", flags: map[string]string{"id": "Snapshot ID"}},
		"snapshot compare": {
			short: "Compare snapshot with current database",
			long:  "Compare the snapshot's table structures with the current database and output a diff report",
			flags: map[string]string{
				"id":          "Snapshot ID",
				"target-conn": "Target connection name (ID or name)",
				"database":    "Target database name (overrides target connection default db)",
				"db-map":      "Snapshot db -> target db mapping, comma-separated src=target pairs (e.g. db1=db2,db3=db4)",
				"output":      "Extra path to save the compare report JSON",
			},
		},
		"history": {short: "Execution history management (list/show/delete)"},
		"history list": {
			short: "List execution history",
			flags: map[string]string{"type": "Filter by type: export|import|migrate|compare"},
		},
		"history show":   {short: "Show execution record details", flags: map[string]string{"id": "Execution record ID"}},
		"history delete": {short: "Delete execution records (running tasks cannot be deleted)", flags: map[string]string{"id": "Execution record ID"}},
		"compare": {
			short: "Compare structure and data differences between two databases",
			long: `Compare structure and data differences between two databases.

Standalone usage:
  dqex compare --gen-config > compare.yaml   # generate config template
  vi compare.yaml                               # fill in connections and options
  dqex compare --config compare.yaml         # run comparison

Command-line flags take priority over the config file; connections can also be fully specified via flags (--source-* / --target-*).
Conn flags provide mysqldump-style aliases: --source-user/--source-password/--source-database (and the target- series)`,
			flags: mergeMaps(enConnFlags("source"), enConnFlags("target"), enConnAliases("source-", "source-"), enConnAliases("target-", "target-"), map[string]string{
				"config":         "Config file (yaml); dqex compare --gen-config prints a template",
				"gen-config":     "Print the config template to stdout",
				"task":           "Saved task config ID (tasks saved from Web)",
				"source-conn":    "Use saved source connection (ID or name)",
				"target-conn":    "Use saved target connection (ID or name)",
				"tables":         "Tables to compare, comma-separated (default: all tables in the db)",
				"alias":          "Table alias pairing, format src=target (repeatable)",
				"scope":          "Comparison scope: both|structure|data",
				"structure-only": "Compare structure only (equivalent to --scope structure)",
				"data-only":      "Compare data only (equivalent to --scope data)",
				"threshold":      "Row-by-row comparison threshold (default 1000; above it only row counts are compared)",
				"ignore-columns": "Columns ignored in data comparison, comma-separated (e.g. created_at,updated_at)",
				"force-data":     "Force data comparison even when structures differ (default: skip data comparison)",
				"output":         "Extra path to save the compare report JSON (besides the report in history)",
				"all":            "Output all tables (default: only tables with differences)",
			}),
		},
		"compare show": {
			short: "Review diff details of a historical compare record",
			long: `Review diff details of a historical compare record.

dqex cmp show -i <recordID>                # all tables
dqex cmp show -i <recordID> t_config       # single table diff (positional arg or --table/-t; column-level diff + sample rows)`,
			flags: map[string]string{
				"id":    "Compare record ID (printed after compare, or via history list --type compare)",
				"table": "Show diff details for the given table only",
			},
		},
		"export": {
			short: "Export database structure and data as SQL (compressible as zip/gzip)",
			long: `Export database structure and data as SQL (compressible as zip/gzip).

Standalone usage:
  dqex export --gen-config > export.yaml   # generate config template
  vi export.yaml                              # fill in connections and options
  dqex export --config export.yaml         # run export

Command-line flags take priority over the config file; connections can also be fully specified via flags (--source-*).
Connection and common flags provide mysqldump-style aliases:
  --host/--port/--user/--password/--database, --no-data, --no-create-info
  Short flags: -h/-P/-u/-p (host/port/user/password), -s (saved connection), -o (output), -T (tables)
  Positional args [db [table...]] are equivalent to --databases/--tables`,
			flags: mergeMaps(enConnFlags("source"), enConnAliases("", "source-"), map[string]string{
				"config":             "Config file (yaml); dqex export --gen-config prints a template",
				"gen-config":         "Print the config template to stdout",
				"task":               "Saved task config ID (tasks saved from Web)",
				"source-conn":        "Use saved connection (ID or name); takes priority when --source-* also given",
				"output":             "Output directory or .zip file path",
				"name":               "Task name (used for generated file name)",
				"databases":          "Databases, comma-separated",
				"tables":             "Tables, comma-separated",
				"objects":            "Objects, comma-separated, format category/name (categories: _views/_functions/_procedures)",
				"table-cond":         "Table filter conditions, format table:full SELECT (repeatable; legacy format table:WHERE fragment also accepted)",
				"schema-only":        "Export structure only",
				"data-only":          "Export data only",
				"no-data":            "Do not export data, structure only (same as --schema-only, mysqldump style)",
				"no-create-info":     "Do not export CREATE statements, data only (same as --data-only, mysqldump style)",
				"compress":           "Package as zip",
				"gzip":               "Compress SQL files as .sql.gz (can combine with --compress; auto-decompressed on import)",
				"single-transaction": "Consistent snapshot export (same as mysqldump --single-transaction; MySQL/PostgreSQL only)",
				"batch-size":         "Batch size",
				"help":               "Show help for the export command",
			}),
		},
		"import": {
			short: "Import SQL files (.sql / .sql.gz / .zip) into a database",
			long: `Import SQL files (.sql / .sql.gz / .zip) into a database.

Standalone usage:
  dqex import --gen-config > import.yaml   # generate config template
  vi import.yaml                              # fill in connections and options
  dqex import --config import.yaml         # run import

Command-line flags take priority over the config file; connections can also be fully specified via flags (--target-*).
Conn flags provide mysqldump-style aliases: --host/--port/--user/--password/--database
Short flags: -h/-P/-u/-p (host/port/user/password), -t (saved connection), -i (input file)`,
			flags: mergeMaps(enConnFlags("target"), enConnAliases("", "target-"), map[string]string{
				"config":      "Config file (yaml); dqex import --gen-config prints a template",
				"gen-config":  "Print the config template to stdout",
				"task":        "Saved task config ID (tasks saved from Web)",
				"target-conn": "Use saved connection (ID or name); takes priority when --target-* also given",
				"input":       "Import file (.sql / .sql.gz / .zip)",
				"reset":       "Reset mode: truncate|drop (default: no reset)",
				"backup":      "Create backup tables before reset",
				"batch-size":  "Batch size",
				"help":        "Show help for the import command",
			}),
		},
		"migrate": {
			short: "Database migration (cross-type supported)",
			long: `Database migration (cross-type supported).

Standalone usage:
  dqex migrate --gen-config > migrate.yaml   # generate config template
  vi migrate.yaml                               # fill in connections and options
  dqex migrate --config migrate.yaml         # run migration

Command-line flags take priority over the config file; connections can also be fully specified via flags (--source-* / --target-*).
Conn flags provide mysqldump-style aliases: --source-user/--source-password/--source-database (and the target- series)`,
			flags: mergeMaps(enConnFlags("source"), enConnFlags("target"), enConnAliases("source-", "source-"), enConnAliases("target-", "target-"), map[string]string{
				"config":      "Config file (yaml); dqex migrate --gen-config prints a template",
				"gen-config":  "Print the config template to stdout",
				"task":        "Saved task config ID (tasks saved from Web)",
				"source-conn": "Use saved source connection (ID or name)",
				"target-conn": "Use saved target connection (ID or name)",
				"tables":      "Tables, comma-separated",
				"objects":     "Objects, comma-separated, format category/name (categories: _views/_functions/_procedures; same-type migration only)",
				"table-cond":  "Table filter conditions, format table:full SELECT (repeatable; legacy format table:WHERE fragment also accepted)",
				"schema-only": "Migrate structure only",
				"data-only":   "Migrate data only",
				"reset":       "Reset mode: truncate|drop (default: no reset)",
				"backup":      "Create backup tables before reset",
				"batch-size":  "Batch size",
			}),
		},
		"dictionary": {
			short: "Generate a database table/field data dictionary as Excel (.xlsx, with comments)",
			long: `Generate a database table/field data dictionary as Excel (.xlsx, with table/column comments), compressible as zip.
Output is a single Excel workbook: an overview sheet (all instance tables with hyperlinks) + one detail sheet per database.

Standalone usage:
  dqex dictionary --gen-config > dictionary.yaml   # generate config template
  vi dictionary.yaml                                # fill in connections and options
  dqex dictionary --config dictionary.yaml         # run generation

Command-line flags take priority over the config file; connections can also be fully specified via flags (--source-*).
  Short flags: -h/-P/-u/-p (host/port/user/password), -s (saved connection), -o (output), -T (tables)
  Positional args [db [table...]] are equivalent to --databases/--tables`,
			flags: mergeMaps(enConnFlags("source"), enConnAliases("", "source-"), map[string]string{
				"config":      "Config file (yaml); dqex dictionary --gen-config prints a template",
				"gen-config":  "Print the config template to stdout",
				"task":        "Saved task config ID (tasks saved from Web)",
				"source-conn": "Use saved connection (ID or name); takes priority when --source-* also given",
				"output":      "Output directory or .zip file path",
				"name":        "Task name (used for generated file name)",
				"databases":   "Databases, comma-separated",
				"tables":      "Tables, comma-separated",
				"compress":    "Package as zip",
				"help":        "Show help for the dictionary command",
			}),
		},
	}}
}
