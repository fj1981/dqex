// Package sqlcmd 提供 dbx sql 子命令：交互式 SQL 终端 + JSON 输出。
// 完全自包含，不引用父包 internal/cli 以避免循环依赖。
package sqlcmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"dbimpex/internal/engine"
	"dbimpex/internal/service"

	"github.com/spf13/cobra"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/ss"
)

// ---- 颜色函数（自包含） ----

var colorOn = func() bool {
	// 简单检测：stdout 是否终端 && 无 NO_COLOR
	fi, _ := os.Stdout.Stat()
	return (fi.Mode()&os.ModeCharDevice) != 0 && os.Getenv("NO_COLOR") == ""
}()

func colorize(code, s string) string {
	if !colorOn {
		return s
	}
	return "\033[" + code + "m" + s + "\033[0m"
}

var (
	green  = func(s string) string { return colorize("32", s) }
	red    = func(s string) string { return colorize("31", s) }
	yellow = func(s string) string { return colorize("33", s) }
	bold   = func(s string) string { return colorize("1", s) }
	dim    = func(s string) string { return colorize("2", s) }
)

// ---- 子命令入口 ----

// Command 返回 dbx sql 子命令，供 root.go 注册。
func Command() *cobra.Command {
	opts := &options{}
	cmd := &cobra.Command{
		Use:   "sql",
		Short: "SQL 交互终端 / 执行 SQL 查询",
		Long: `交互式数据库 SQL 终端，支持表格渲染与 JSON 输出。

使用方式：
  dbx sql -c <连接>              交互式 REPL 终端
  dbx sql -c <连接> -e "SQL"     单次执行（表格输出）
  dbx sql -c <连接> --json "SQL" JSON 输出（智能体友好）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run(cmd, opts, args)
		},
	}

	cmd.Flags().StringVarP(&opts.connKey, "conn", "c", "", "已保存连接（支持 ID、名称或短名）")
	registerConnFlags(cmd, "", &opts.cf)
	registerConnAliases(cmd, "", "", &opts.cf)

	cmd.Flags().StringVarP(&opts.execute, "execute", "e", "", "执行 SQL 后退出")
	cmd.Flags().StringVarP(&opts.file, "file", "f", "", "从文件读取 SQL 执行")
	cmd.Flags().BoolVar(&opts.asJSON, "json", false, "JSON 格式输出")
	cmd.Flags().DurationVar(&opts.timeout, "timeout", 30*time.Second, "查询超时")
	cmd.Flags().IntVar(&opts.maxRows, "max-rows", 1000, "最大返回行数")
	cmd.Flags().BoolVar(&opts.allowWrite, "allow-write", false, "--json/-f 模式下允许执行写操作")
	cmd.Flags().StringVar(&opts.sslCA, "ssl-ca", "", "TLS CA 证书路径")
	cmd.Flags().BoolVar(&opts.noColor, "no-color", false, "禁用颜色输出")

	reserveHelpFlag(cmd) // -h 让给 --host，帮助用 --help

	return cmd
}

type options struct {
	connKey    string
	cf         connFlags
	execute    string
	file       string
	asJSON     bool
	timeout    time.Duration
	maxRows    int
	allowWrite bool
	sslCA      string
	noColor    bool
}

// ---- 连接 flag（自包含，和 cli.connFlags 保持结构一致） ----

type connFlags struct {
	typ, host, un, pw, db, subtype string
	port                           int
}

func registerConnFlags(cmd *cobra.Command, prefix string, cf *connFlags) {
	f := cmd.Flags()
	f.StringVar(&cf.typ, prefix+"-type", "", prefix+" 数据库类型(mysql/postgresql/oracle)")
	f.StringVar(&cf.host, prefix+"-host", "", prefix+" 主机")
	f.IntVar(&cf.port, prefix+"-port", 0, prefix+" 端口")
	f.StringVar(&cf.un, prefix+"-un", "", prefix+" 用户名")
	f.StringVar(&cf.pw, prefix+"-pw", "", prefix+" 密码")
	f.StringVar(&cf.db, prefix+"-db", "", prefix+" 数据库名")
	f.StringVar(&cf.subtype, prefix+"-subtype", "", prefix+" 数据库产品")
}

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

func (cf *connFlags) toConn() *engine.DBConnInfo {
	if cf.typ == "" {
		return nil
	}
	return &engine.DBConnInfo{DBConnection: def.DBConnection{
		Type:    cf.typ,
		SubType: cf.subtype,
		Host:    cf.host,
		Port:    cf.port,
		Un:      cf.un,
		Pw:      cf.pw,
		DBName:  cf.db,
	}}
}

// ---- 主流程 ----

func run(_ *cobra.Command, opts *options, args []string) error {
	if opts.noColor {
		colorOn = false
	}

	connInfo, err := resolveConn(opts)
	if err != nil {
		return err
	}

	if opts.execute != "" || opts.file != "" || opts.asJSON {
		sql := opts.execute
		if sql == "" && len(args) > 0 {
			sql = args[0]
		}
		if opts.file != "" {
			data, err := os.ReadFile(opts.file)
			if err != nil {
				return fmt.Errorf("读取文件失败: %w", err)
			}
			sql = string(data)
		}
		if sql == "" {
			return errors.New(cliTextsFor(cliLang).noSQLProvided)
		}
		return executeOnce(connInfo, sql, opts)
	}

	return runInteractive(connInfo)
}

func resolveConn(opts *options) (*engine.DBConnInfo, error) {
	svc, err := service.NewServiceWith("", "")
	if err != nil {
		return nil, fmt.Errorf("初始化服务失败: %w", err)
	}
	if opts.connKey != "" {
		rec, ok := svc.Persist().GetConn(opts.connKey)
		if !ok {
			return nil, fmt.Errorf("未找到连接: %s", opts.connKey)
		}
		conn := rec.Conn
		if opts.cf.db != "" {
			conn.DBName = opts.cf.db
		}
		return &conn, nil
	}
	if info := opts.cf.toConn(); info != nil {
		return info, nil
	}
	return nil, errors.New(cliTextsFor(cliLang).needConn)
}

func executeOnce(info *engine.DBConnInfo, sql string, opts *options) error {
	ctx, cancel := context.WithTimeout(context.Background(), opts.timeout)
	defer cancel()

	isWrite := classifySQL(sql)

	if isWrite {
		if opts.asJSON && !opts.allowWrite {
			return errors.New(cliTextsFor(cliLang).jsonNoWrite)
		}
		if opts.file != "" && !opts.allowWrite {
			return errors.New(cliTextsFor(cliLang).fileNoWrite)
		}
	}

	cliDB, err := engine.Connect(*info)
	if err != nil {
		return err
	}
	defer cliDB.Close()

	start := time.Now()

	if !isWrite {
		processedSQL := sql
		if !hasLimitClause(sql) && !opts.asJSON && shouldAddLimit(sql) {
			processedSQL = addLimit(sql, opts.maxRows)
		}
		rows, columns, err := cliDB.DirectQueryFastContext(ctx, processedSQL)
		if err != nil {
			return fmt.Errorf("查询失败: %w", err)
		}
		result := &queryResult{
			Columns:  columns,
			Rows:     rows,
			RowCount: len(rows),
			Elapsed:  time.Since(start),
			SQL:      processedSQL,
		}
		return outputResult(result, opts)
	}

	affected, err := cliDB.DirectExecuteContext(ctx, sql)
	if err != nil {
		return fmt.Errorf("执行失败: %w", err)
	}
	result := &queryResult{
		AffectedRows: affected,
		Elapsed:      time.Since(start),
		SQL:          sql,
		IsWrite:      true,
	}
	return outputResult(result, opts)
}

func outputResult(r *queryResult, opts *options) error {
	if opts.asJSON {
		return outputJSON(r)
	}
	outputTable(r)
	return nil
}

// reserveHelpFlag 预定义 --help（不带 -h 短形式），避免 cobra 自动注册 -h；
// 把 -h 让给 --host，帮助仍可用 --help。
func reserveHelpFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("help", false, "查看 "+cmd.Name()+" 命令帮助")
}

func outputJSON(r *queryResult) error {
	// 大数据量时使用 NDJSON 流式输出
	if r.RowCount > 10000 {
		return outputNDJSON(r)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if r.Error != "" {
		return enc.Encode(map[string]any{
			"error":   r.Error,
			"elapsed": r.Elapsed.String(),
			"sql":     r.SQL,
		})
	}
	out := map[string]any{
		"columns":  r.Columns,
		"rows":     r.Rows,
		"rowCount": r.RowCount,
		"elapsed":  r.Elapsed.String(),
		"sql":      r.SQL,
	}
	if r.AffectedRows > 0 || r.RowCount == 0 {
		out["affectedRows"] = r.AffectedRows
	}
	return enc.Encode(out)
}

type queryResult struct {
	Columns      []string
	Rows         [][]any
	RowCount     int
	AffectedRows int64
	Elapsed      time.Duration
	SQL          string
	Error        string
	IsWrite      bool // 写操作标记：空结果时区分「Query OK」（写）与「Empty set」（读）
}

// ---- SQL 分类 ----

func classifySQL(sql string) bool {
	trimmed := strings.TrimSpace(sql)
	if stmt, err := cydb.ParseMySQL(trimmed); err == nil && stmt != nil {
		switch stmt.GetType() {
		case def.QueryTypeSelect:
			return false
		case def.QueryTypeInsert, def.QueryTypeUpdate, def.QueryTypeDelete:
			return true
		default:
			return !isKnownReadOnly(trimmed)
		}
	}
	return !isKnownReadOnly(trimmed)
}

func isKnownReadOnly(sql string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	for _, prefix := range []string{
		"SELECT", "SHOW", "DESC", "DESCRIBE", "EXPLAIN", "USE", "WITH", "SET",
		"KILL", "CHECK", "HELP",
		"BEGIN", "COMMIT", "ROLLBACK", "START",
	} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func hasLimitClause(sql string) bool {
	if stmt, err := cydb.ParseMySQL(sql); err == nil && stmt != nil {
		return stmt.GetLimitClause() != nil
	}
	return strings.Contains(strings.ToUpper(sql), "LIMIT")
}

// shouldAddLimit 判断是否为应追加 LIMIT 的 SELECT 类语句。
// SHOW/DESC/EXPLAIN/SET/USE 等不支持 LIMIT 的语句应排除。
func shouldAddLimit(sql string) bool {
	upper := strings.ToUpper(strings.TrimSpace(sql))
	noLimitPrefixes := []string{"SHOW", "DESC", "DESCRIBE", "EXPLAIN", "SET", "USE", "WITH"}
	for _, prefix := range noLimitPrefixes {
		if strings.HasPrefix(upper, prefix) {
			return false
		}
	}
	return true
}

func addLimit(sql string, n int) string {
	return sprintf("%s LIMIT %d", sql, n)
}

// ---- 危险函数检测 ----

var dangerousFuncs = []string{"SLEEP(", "BENCHMARK("}
var forbiddenFuncs = []string{"LOAD_FILE(", "INTO OUTFILE", "INTO DUMPFILE"}

func checkDangerous(sql string) (warnings []string, forbidden []string) {
	txt := cliTextsFor(cliLang)
	if stmt, err := cydb.ParseMySQL(sql); err == nil && stmt != nil {
		return checkDangerousAST(stmt)
	}
	sqlUpper := strings.ToUpper(sql)
	for _, f := range dangerousFuncs {
		if strings.Contains(sqlUpper, f) {
			warnings = append(warnings, sprintf(txt.dangerFunc, f))
		}
	}
	for _, f := range forbiddenFuncs {
		if strings.Contains(sqlUpper, f) {
			forbidden = append(forbidden, sprintf(txt.forbidFunc, f))
		}
	}
	return
}

func checkDangerousAST(stmt cydb.SQLBuilder) (warnings []string, forbidden []string) {
	txt := cliTextsFor(cliLang)
	s := (*ss.SQLStmt)(stmt)
	if s.SelectClause != nil {
		for _, sel := range s.SelectClause.Items {
			if sel.Expr != nil {
				visitFuncName(sel.Expr, func(name string) {
					for _, f := range dangerousFuncs {
						if strings.EqualFold(name, strings.TrimSuffix(f, "(")) {
							warnings = append(warnings, sprintf(txt.dangerFunc, f))
						}
					}
					for _, f := range forbiddenFuncs {
						if strings.EqualFold(name, strings.TrimSuffix(f, "(")) {
							forbidden = append(forbidden, sprintf(txt.forbidFunc, f))
						}
					}
				})
			}
		}
	}
	return
}

func visitFuncName(expr def.Expression, fn func(string)) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ss.FunctionExpr:
		fn(e.Name)
	}
}

// ---- NDJSON 流式输出 ----

// outputNDJSON 流式输出 NDJSON 格式，逐行写入避免大数据集 OOM。
func outputNDJSON(r *queryResult) error {
	enc := json.NewEncoder(os.Stdout)
	// header
	enc.Encode(map[string]any{
		"type":    "header",
		"columns": r.Columns,
		"sql":     r.SQL,
	})
	// rows
	for _, row := range r.Rows {
		enc.Encode(map[string]any{
			"type": "row",
			"data": row,
		})
	}
	// summary
	enc.Encode(map[string]any{
		"type":     "summary",
		"rowCount": r.RowCount,
		"elapsed":  r.Elapsed.String(),
	})
	return nil
}
