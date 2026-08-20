package cli

// 点导入：CLI 层大量复用 service 包的模型别名与入口（NewService/选项模型/错误码）
import (
	"context"
	. "dbimpex/internal/service"
	"fmt"
	"os"
	"sort"
	"strings"

	"dbimpex/internal/cli/sqlcmd"
	"dbimpex/internal/engine"
	"dbimpex/internal/llm"

	"github.com/spf13/cobra"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
	"golang.org/x/term"
)

// ---- CLI 入口（cobra） ----

// WebArgs Web 启动参数（未执行 CLI 子命令时由 Execute 返回）
type WebArgs struct {
	Host       string
	Port       int
	Allow      string
	NoAuth     bool
	NoBrowser  bool
	DataDir    string
	ConfigFile string
	Debug      bool // 全局 debug 日志开关（等效 config 顶层 debug: true）
}

var (
	webArgs     = &WebArgs{Host: "127.0.0.1", Port: 8181}
	cliExecuted bool   // 执行过任一 CLI 子命令（含 help）时为 true
	langFlag    string // --lang 标志（CLI 输出语言，优先级最高）
)

// cliLang 解析 CLI 输出语言：--lang 标志 > 环境变量 DBX_LANG > 默认 zh。
// 语言代码走 llm.NormLang 归一与回退，与 AI/字典注册表一致（可扩展）。
func cliLang() string {
	lang := strings.TrimSpace(langFlag)
	if lang == "" {
		lang = os.Getenv("DBX_LANG")
	}
	return llm.NormLang(lang)
}

func init() {
	rootCmd.AddCommand(sqlcmd.Command())
}

var rootCmd = &cobra.Command{
	Use:   "dbx",
	Short: "数据库导入/导出/迁移/对比工具",
	Long: `dbx - 数据库导入/导出/迁移/对比工具

不带子命令时启动 Web 服务（默认端口 8181）。
CLI 子命令与 Web 功能对齐：export / import / migrate / compare / conn / task / history`,
	SilenceErrors: true,
	SilenceUsage:  true,
	// 空 Run：裸跑（可带 --port/--data-dir）时不打印 help，由 Execute 返回后启动 Web
	Run: func(cmd *cobra.Command, args []string) {},
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		// root 命令自身执行（启动 Web）不计入 CLI 子命令
		if cmd.Parent() != nil {
			cliExecuted = true
		}
		// 把解析后的语言注入 sqlcmd 包（元命令/帮助/状态文本按语言输出）；
		// 放在此处而非 Execute：--lang flag 需先经 cobra 解析完成
		sqlcmd.SetLang(cliLang())
	},
}

// Execute CLI 入口：返回非 nil 表示需启动 Web 服务；CLI 子命令执行完直接退出
func Execute() *WebArgs {
	rootCmd.PersistentFlags().StringVar(&webArgs.Host, "host", "127.0.0.1", "Web 服务监听地址（默认仅本机回环；对外暴露用 0.0.0.0，此时强制启用令牌认证）")
	rootCmd.PersistentFlags().IntVar(&webArgs.Port, "port", 8181, "Web 服务端口")
	rootCmd.PersistentFlags().StringVar(&webArgs.Allow, "allow", "", "访问来源白名单（IP/CIDR/域名，逗号分隔；留空不限制，优先于配置 web.allow；本机回环始终放行）")
	rootCmd.PersistentFlags().BoolVar(&webArgs.NoAuth, "no-auth", false, "禁用令牌认证（仅限监听本机回环，不推荐）")
	rootCmd.PersistentFlags().BoolVar(&webArgs.NoBrowser, "no-browser", false, "启动时不自动打开浏览器")
	rootCmd.PersistentFlags().StringVar(&webArgs.DataDir, "data-dir", "", "数据根目录（默认取全局配置，否则 ~/.dbimpex）")
	rootCmd.PersistentFlags().StringVar(&webArgs.ConfigFile, "config-file", "", "全局配置文件（默认 环境变量 DBIMPEX_CONFIG 或 ~/.dbimpex/config.yaml）")
	rootCmd.PersistentFlags().BoolVar(&webArgs.Debug, "debug", false, "输出 debug 及以上级别的日志（含 AI 链路；等效 config 顶层 debug: true）")
	rootCmd.PersistentFlags().StringVar(&langFlag, "lang", "", "CLI 输出语言（zh/en；环境变量 DBX_LANG，默认 zh）")
	// help 展示（--help / 裸跑分组命令）也视为 CLI 执行，避免随后误启 Web
	defaultHelp := rootCmd.HelpFunc()
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		cliExecuted = true
		applyHelpLang(cliLang()) // 帮助/flag usage 按当前语言渲染（--help 时 PersistentPreRun 不执行）
		defaultHelp(cmd, args)
	})
	// 命令错误打印 usage 的场景同样按语言重写（如 RunE 返回错误时 cobra 展示用法）
	defaultUsage := rootCmd.UsageFunc()
	rootCmd.SetUsageFunc(func(cmd *cobra.Command) error {
		applyHelpLang(cliLang())
		return defaultUsage(cmd)
	})
	if err := rootCmd.Execute(); err != nil {
		fprintf(os.Stderr, cliTextsFor(cliLang()).errPrefix+"\n", cliErrMsg(err))
		os.Exit(1)
	}
	if cliExecuted {
		return nil
	}
	return webArgs
}

// cliErrMsg 提取可读错误信息：engine.MsgError / service.SvcError 按当前语言渲染（双语核心业务错误）；
// *cygin.Error 输出当前语言消息；details 已按当前语言渲染（参数校验/存储/任务链路均双语），zh/en 均拼接展示。
func cliErrMsg(err error) string {
	if me := engine.AsMsgErr(err); me != nil {
		return me.Msg(cliLang())
	}
	if se := AsSvcErr(err); se != nil {
		return se.Msg(cliLang())
	}
	if e, ok := err.(*cygin.Error); ok {
		msg := e.Msg(cliLang())
		if len(e.Details) > 0 {
			return sprintf("%s (%s)", msg, strings.Join(e.Details, "; "))
		}
		return msg
	}
	return err.Error()
}

// newCliService 解析全局配置（--data-dir/--config-file/DBIMPEX_CONFIG/~/.dbimpex/config.yaml）后构建 Service
func newCliService() (*Service, error) {
	svc, err := NewServiceWith(WithLang(context.Background(), cliLang()), webArgs.DataDir, webArgs.ConfigFile)
	if err != nil {
		return nil, err
	}
	// 全局 debug 日志：--debug flag 或 config 顶层 debug=true 时把日志级别切到 debug（CLI 与 Web 统一入口）
	if webArgs.Debug || svc.Config().Debug {
		cylog.InitDefault(cylog.WithLevelStr("debug"))
		cylog.Debugf("[cli] 全局 debug 日志已开启")
	}
	return svc, nil
}

// ---- Shell 动态补全（配合 dbx completion 生成的补全脚本） ----

// completeConnNames 补全已保存连接（名称+短名，Tab 描述附 ID 和地址）
func completeConnNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	svc, err := newCliService()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	conns := svc.Persist().LoadConns()
	names := make([]string, 0, len(conns)*2)
	for _, rec := range conns {
		desc := sprintf("ID: %s  %s:%d", rec.ID, rec.Conn.Host, rec.Conn.Port)
		names = append(names, rec.Name+"\t"+desc)
		if rec.ShortName != "" {
			names = append(names, rec.ShortName+"\t"+desc+" ("+rec.Name+")")
		}
	}
	sort.Strings(names)
	return names, cobra.ShellCompDirectiveNoFileComp
}

// completeTaskIDs 补全已保存任务配置 ID（可限定任务类型，空=不限）
func completeTaskIDs(taskType string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		svc, err := newCliService()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ret := make([]string, 0)
		for _, t := range svc.Persist().LoadTasks() {
			if taskType != "" && t.Type != taskType {
				continue
			}
			ret = append(ret, t.ID+"\t"+t.Name+" ("+t.Type+")")
		}
		sort.Strings(ret)
		return ret, cobra.ShellCompDirectiveNoFileComp
	}
}

// completeHistoryIDs 补全执行记录 ID（可限定任务类型，空=不限）
func completeHistoryIDs(taskType string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		svc, err := newCliService()
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ret := make([]string, 0)
		for _, r := range svc.Persist().LoadHistory("", "") {
			if taskType != "" && r.TaskType != taskType {
				continue
			}
			ret = append(ret, r.ID+"\t"+r.TaskType+" · "+r.Status)
		}
		sort.Strings(ret)
		return ret, cobra.ShellCompDirectiveNoFileComp
	}
}

// fixedCompletion 固定候选值补全（如 --type/--scope/--reset 等枚举 flag）
func fixedCompletion(values ...string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		return values, cobra.ShellCompDirectiveNoFileComp
	}
}

// ---- 连接 flags（export/import/migrate/compare 共用） ----

type connFlags struct {
	typ, host, un, pw, db, subtype string
	port                           int
}

func registerConnFlags(cmd *cobra.Command, prefix string, cf *connFlags) {
	f := cmd.Flags()
	// 前缀非空时插入分隔符（source-type），空前缀直接用 type 等名（--type 而非 ---type）
	sep := ""
	if prefix != "" {
		sep = "-"
	}
	f.StringVar(&cf.typ, prefix+sep+"type", "", prefix+" 数据库类型(mysql/postgresql/oracle)")
	f.StringVar(&cf.host, prefix+sep+"host", "", prefix+" 主机")
	f.IntVar(&cf.port, prefix+sep+"port", 0, prefix+" 端口")
	f.StringVar(&cf.un, prefix+sep+"un", "", prefix+" 用户名")
	f.StringVar(&cf.pw, prefix+sep+"pw", "", prefix+" 密码")
	f.StringVar(&cf.db, prefix+sep+"db", "", prefix+" 数据库名")
	f.StringVar(&cf.subtype, prefix+sep+"subtype", "", prefix+" 数据库产品（兼容库，如 oceanbase/gaussdb/dameng，留空=原生）")
}

// registerConnAliases mysqldump 风格连接 flag 别名（绑定同一变量，便于 DBA 肌肉记忆）；
// aliasPrefix 空为无前缀（export/import，补全 host/port 并提供 mysqldump 短参 -h/-P/-u/-p），
// 否则如 source-/target-（仅补 user/password/database，因 source-host 等已存在）；
// refPrefix 为 help 中指向的原 flag 前缀（source-/target-）
func registerConnAliases(cmd *cobra.Command, aliasPrefix, refPrefix string, cf *connFlags) {
	f := cmd.Flags()
	if aliasPrefix == "" {
		f.StringVarP(&cf.host, "host", "h", "", "主机（同 "+refPrefix+"host）")
		f.IntVarP(&cf.port, "port", "P", 0, "端口（同 "+refPrefix+"port）")
		f.StringVarP(&cf.un, "user", "u", "", "用户名（同 "+refPrefix+"un）")
		f.StringVarP(&cf.pw, "password", "p", "", "密码（同 "+refPrefix+"pw）")
	} else {
		f.StringVar(&cf.un, aliasPrefix+"user", "", aliasPrefix+"用户名（同 "+refPrefix+"un）")
		f.StringVar(&cf.pw, aliasPrefix+"password", "", aliasPrefix+"密码（同 "+refPrefix+"pw）")
	}
	f.StringVar(&cf.db, aliasPrefix+"database", "", aliasPrefix+"数据库名（同 "+refPrefix+"db）")
}

// reserveHelpFlag 预定义 --help（不带 -h 短形式），避免 cobra 自动注册 -h；
// 供 export/import 把 -h 让给 mysqldump 风格的 --host（帮助仍可用 --help）
func reserveHelpFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("help", false, "查看 "+cmd.Name()+" 命令帮助")
}

// anyChanged 任一别名 flag 被显式给出（用于 mysqldump 风格别名判定）
func anyChanged(f interface {
	Changed(string) bool
}, names ...string) bool {
	for _, n := range names {
		if f.Changed(n) {
			return true
		}
	}
	return false
}

func (cf *connFlags) toConn() *DBConnInfo {
	if cf.typ == "" {
		return nil
	}
	return &DBConnInfo{DBConnection: def.DBConnection{
		Type:    cf.typ,
		SubType: cf.subtype,
		Host:    cf.host,
		Port:    cf.port,
		Un:      cf.un,
		Pw:      cf.pw,
		DBName:  cf.db,
	}}
}

// overrideConnDB 已保存连接引用 + 库名覆盖：解析为内联连接并写入库名（引擎要求连接带库）
func overrideConnDB(svc *Service, key, dbName string) (*DBConnInfo, error) {
	rec, ok := svc.Persist().GetConn(key)
	if !ok {
		return nil, cygin.NewError(ErrConnNotFound, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errConnNotFound, key))
	}
	conn := rec.Conn
	if dbName != "" {
		conn.DBName = dbName
	}
	return &conn, nil
}

// parseTableConds 解析 --table-cond "表名:完整SELECT" 参数（兼容旧格式 表名:WHERE 片段）
func parseTableConds(items []string) []TableCondition {
	ret := make([]TableCondition, 0, len(items))
	for _, item := range items {
		idx := strings.Index(item, ":")
		if idx <= 0 {
			continue
		}
		cond := TableCondition{TableName: strings.TrimSpace(item[:idx])}
		sql := strings.TrimSpace(item[idx+1:])
		if strings.HasPrefix(strings.ToLower(sql), "select ") {
			cond.Query = sql
		} else {
			cond.Where = sql // 旧格式：WHERE 片段
		}
		ret = append(ret, cond)
	}
	return ret
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	ret := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			ret = append(ret, p)
		}
	}
	return ret
}

// parseDBMap 解析 "src=tgt,src2=tgt2" 形式的库映射为 map
func parseDBMap(s string) map[string]string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	ret := make(map[string]string)
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		kv := strings.SplitN(pair, "=", 2)
		src := strings.TrimSpace(kv[0])
		if src == "" {
			continue
		}
		tgt := ""
		if len(kv) == 2 {
			tgt = strings.TrimSpace(kv[1])
		}
		ret[src] = tgt
	}
	if len(ret) == 0 {
		return nil
	}
	return ret
}

// ---- CLI 终端输出 ----

// stdoutTTY 标准输出是否终端（进度条刷新与颜色都依赖它，管道/重定向时自动降级）
var stdoutTTY = term.IsTerminal(int(os.Stdout.Fd()))

// colorOn ANSI 颜色开关（非终端或 NO_COLOR 时关闭，便于管道/脚本消费）
var colorOn = stdoutTTY && os.Getenv("NO_COLOR") == ""

func colorize(code, s string) string {
	if !colorOn {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

func green(s string) string  { return colorize("32", s) }
func red(s string) string    { return colorize("31", s) }
func yellow(s string) string { return colorize("33", s) }
func bold(s string) string   { return colorize("1", s) }
func dim(s string) string    { return colorize("2", s) }

// cliProgress 终端进度条：TTY 单行原地刷新，非 TTY 退化为里程碑行输出
func cliProgress() (ProgressFunc, *string) {
	tty := stdoutTTY
	lastMsg := ""
	lastPct := -1.0
	var outputPath string
	return func(p ProgressInfo) {
		if p.OutputPath != "" {
			outputPath = p.OutputPath
		}
		if p.Message != "" && p.Message != lastMsg {
			lastMsg = p.Message
			if tty {
				printf("\r\033[K%s %s\n", dim("·"), p.Message)
			} else {
				printf("  · %s\n", p.Message)
			}
		}
		terminal := p.State == "done" || p.State == "error" || p.State == "cancelled"
		if !tty {
			// 非 TTY：每 20% 一条，避免重定向日志刷屏
			if terminal || p.Percent >= lastPct+20 {
				lastPct = p.Percent
				printf(cliTextsFor(cliLang()).progressPct+"\n", p.Percent, p.DoneUnits, p.TotalUnits)
			}
			return
		}
		printf("\r\033[K%s", renderBar(p))
		if terminal {
			fmt.Println()
		}
	}, &outputPath
}

// renderBar 单行进度条：[██████░░░░] 62.3% 50/81 项 · 1.2万行 · 当前表
func renderBar(p ProgressInfo) string {
	const width = 28
	pct := p.Percent
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * width)
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	line := sprintf("[%s] %5.1f%%  %s", bar, pct, sprintf(cliTextsFor(cliLang()).progressUnits, p.DoneUnits, p.TotalUnits))
	if p.DoneRows > 0 {
		line += sprintf(cliTextsFor(cliLang()).progressRows, humanRows(p.DoneRows))
	}
	if p.CurrentTable != "" {
		table := p.CurrentTable
		if len(table) > 24 {
			table = table[:24] + "…"
		}
		line += " · " + table
	}
	return line
}

// humanRows 行数人性化（1.2万 / 3456；en 下 1.2K）
func humanRows(n int64) string {
	if n >= 10000 {
		return sprintf(cliTextsFor(cliLang()).rowsThousand, float64(n)/10000)
	}
	return sprintf("%d", n)
}
