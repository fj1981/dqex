// Package engine 提供数据库执行核心。本文件为 Web 查询终端提供
// SQL 分类 / 危险检测 / LIMIT 护栏 / 查询与执行薄封装。
// 逻辑与 internal/cli/sqlcmd 保持一致，但不依赖 CLI 交互状态。
package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/ss"
)

// MaxQueryLimit 单次查询返回行数上限（安全护栏，防止拖垮后端）
const MaxQueryLimit = 1000

// SQLQueryResult 查询/执行结果（Web 查询终端使用）
type SQLQueryResult struct {
	Columns      []string `json:"columns"`
	Rows         [][]any  `json:"rows"`
	RowCount     int      `json:"rowCount"`
	AffectedRows int64    `json:"affectedRows"`
	Elapsed      int64    `json:"elapsedMs"` // 毫秒
	SQL          string   `json:"sql"`
	IsWrite      bool     `json:"isWrite"`
	Warnings     []string `json:"warnings"`
	Error        string   `json:"error,omitempty"` // 执行失败原因（非空时视为失败结果，正常返回给前端展示）
}

// ClassifySQL 判断 SQL 是否为写操作（INSERT/UPDATE/DELETE 等）。
// 与 sqlcmd.classifySQL 保持同一内核。
func ClassifySQL(sql string) bool {
	trimmed := strings.TrimSpace(sql)
	if stmt, err := cydb.ParseMySQL(trimmed); err == nil && stmt != nil {
		switch stmt.GetType() {
		case def.QueryTypeSelect:
			return false
		case def.QueryTypeInsert, def.QueryTypeUpdate, def.QueryTypeDelete:
			return true
		default:
			return !IsKnownReadOnly(trimmed)
		}
	}
	return !IsKnownReadOnly(trimmed)
}

// IsKnownReadOnly 判断语句是否属于已知只读前缀（SELECT/SHOW/USE 等）
func IsKnownReadOnly(sql string) bool {
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

// 危险/禁止函数清单（与 sqlcmd 保持一致）
var (
	dangerousFuncs = []string{"SLEEP(", "BENCHMARK("}
	forbiddenFuncs = []string{"LOAD_FILE(", "INTO OUTFILE", "INTO DUMPFILE"}
)

// CheckDangerous 检测危险/禁止函数，返回 (警告, 禁止)
func CheckDangerous(sql string) (warnings []string, forbidden []string) {
	if stmt, err := cydb.ParseMySQL(sql); err == nil && stmt != nil {
		return checkDangerousAST(stmt)
	}
	sqlUpper := strings.ToUpper(sql)
	for _, f := range dangerousFuncs {
		if strings.Contains(sqlUpper, f) {
			warnings = append(warnings, fmt.Sprintf("检测到危险函数: %s", f))
		}
	}
	for _, f := range forbiddenFuncs {
		if strings.Contains(sqlUpper, f) {
			forbidden = append(forbidden, fmt.Sprintf("检测到禁止函数: %s", f))
		}
	}
	return
}

func checkDangerousAST(stmt cydb.SQLBuilder) (warnings []string, forbidden []string) {
	s := (*ss.SQLStmt)(stmt)
	if s.SelectClause != nil {
		for _, sel := range s.SelectClause.Items {
			if sel.Expr != nil {
				visitFuncName(sel.Expr, func(name string) {
					for _, f := range dangerousFuncs {
						if strings.EqualFold(name, strings.TrimSuffix(f, "(")) {
							warnings = append(warnings, fmt.Sprintf("检测到危险函数: %s", f))
						}
					}
					for _, f := range forbiddenFuncs {
						if strings.EqualFold(name, strings.TrimSuffix(f, "(")) {
							forbidden = append(forbidden, fmt.Sprintf("检测到禁止函数: %s", f))
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

// cleanDBError 从 cydb 的调试格式错误中提取核心数据库错误信息。
// cydb 错误形如 "[query] error=> <真实错误> ||sql=> <sql> || arguments=> <args>"，
// 直接透传给前端会暴露内部 SQL/参数，且充满噪声；这里只保留 error=> 之后的真实原因。
// 无法识别该格式时原样返回，避免丢失信息。
func cleanDBError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	// 提取 "error=> " 到 " ||" 之间的核心错误
	if idx := strings.Index(msg, "error=> "); idx >= 0 {
		core := msg[idx+len("error=> "):]
		if end := strings.Index(core, " ||"); end >= 0 {
			core = core[:end]
		}
		core = strings.TrimSpace(core)
		if core != "" {
			return errors.New(core)
		}
	}
	return err
}

// normalizeLimit 归一化 limit：<=0 用默认上限，超过上限截断
func normalizeLimit(limit int) int {
	if limit <= 0 {
		return MaxQueryLimit
	}
	if limit > MaxQueryLimit {
		return MaxQueryLimit
	}
	return limit
}

// 敏感列名匹配规则（小写匹配子串，覆盖 password/passwd/token/secret 等）
var sensitiveColumnPatterns = []string{"password", "passwd", "pwd", "secret", "token", "apikey", "api_key", "credential", "credential_key", "access_key", "private_key"}

// IsSensitiveColumn 判断列名是否属于敏感字段（用于结果集脱敏）
func IsSensitiveColumn(column string) bool {
	lower := strings.ToLower(column)
	for _, p := range sensitiveColumnPatterns {
		if strings.Contains(lower, p) {
			return true
		}
	}
	return false
}

// MaskResult 对查询结果集做脱敏：敏感列的值统一打码（NULL 保留）。
// 已脱敏的结果集中敏感值以 ****** 呈现，防止前端展示明文密钥/口令。
func MaskResult(result *SQLQueryResult) {
	if result == nil {
		return
	}
	for i, col := range result.Columns {
		if !IsSensitiveColumn(col) {
			continue
		}
		for r := range result.Rows {
			if result.Rows[r][i] != nil {
				result.Rows[r][i] = "******"
			}
		}
	}
}

// RunSQLQuery 执行查询类 SQL（SELECT/SHOW 等），自动追加 LIMIT 上限保护。
// 返回结果及耗时；写操作直接报错（写操作请走 RunSQLExec）。
//
// mode 控制执行模式：
//   - "transform"（默认）：解析重构 + 自动补 LIMIT 上限（无 LIMIT 才补，已含则原样）；
//   - "raw"：原始 SQL 直传，不做转换/限制（用于解析重构异常时的兜底，由用户自行负责行数）。
//
// 无论何种模式，危险函数与写操作始终拦截（安全底线不因 raw 而关闭）。
func RunSQLQuery(ctx context.Context, cli *cydb.DBCli, sql string, limit, offset int, mode string) (*SQLQueryResult, error) {
	warnings, forbidden := CheckDangerous(sql)
	if len(forbidden) > 0 {
		return nil, fmt.Errorf("检测到禁止函数，已拦截: %s", strings.Join(forbidden, "; "))
	}
	isWrite := ClassifySQL(sql)
	if isWrite {
		return nil, fmt.Errorf("检测到写操作，请使用写操作接口执行: %s", strings.TrimSpace(sql))
	}
	limit = normalizeLimit(limit)
	start := time.Now()

	// 执行模式分支：raw 直传；transform 由底层库 EnsureLimit 解析重构 + 补 LIMIT
	var (
		execSQL     string
		rows        [][]any
		columns     []string
		err         error
	)
	if mode == "raw" {
		execSQL = sql
		rows, columns, err = cli.DirectQueryFastContext(ctx, sql)
	} else {
		// 由底层库 EnsureLimit 负责「无 LIMIT 才补、已含 LIMIT 原样执行」：
		// 解析 AST → 判断是否已有限制 → 无则按方言重构追加上限（MySQL/PG 用 LIMIT，Oracle 用 ROWNUM）。
		execSQL, err = cli.EnsureLimit(sql, limit, offset)
		if err != nil {
			return nil, fmt.Errorf("查询 SQL 处理失败: %w", err)
		}
		rows, columns, err = cli.QueryWithLimitContext(ctx, sql, limit, offset)
	}
	if err != nil {
		return nil, fmt.Errorf("查询失败: %w", cleanDBError(err))
	}
	if mode != "raw" && len(rows) > limit {
		rows = rows[:limit]
	}
	// 空结果兜底为切片而非 nil，避免 JSON 序列化为 null 导致前端解构报错
	if rows == nil {
		rows = [][]any{}
	}
	if columns == nil {
		columns = []string{}
	}
	return &SQLQueryResult{
		Columns:  columns,
		Rows:     rows,
		RowCount: len(rows),
		Elapsed:  time.Since(start).Milliseconds(),
		SQL:      execSQL,
		IsWrite:  false,
		Warnings: warnings,
	}, nil
}

// RunSQLExec 执行写操作 SQL（INSERT/UPDATE/DELETE/DDL）。
// 调用方（Web handler）必须已完成危险语句拦截与写操作确认。
func RunSQLExec(ctx context.Context, cli *cydb.DBCli, sql string) (*SQLQueryResult, error) {
	warnings, forbidden := CheckDangerous(sql)
	if len(forbidden) > 0 {
		return nil, fmt.Errorf("检测到禁止函数，已拦截: %s", strings.Join(forbidden, "; "))
	}
	start := time.Now()
	affected, err := cli.DirectExecuteContext(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("执行失败: %w", cleanDBError(err))
	}
	return &SQLQueryResult{
		AffectedRows: affected,
		RowCount:     0,
		Elapsed:      time.Since(start).Milliseconds(),
		SQL:          sql,
		IsWrite:      true,
		Warnings:     warnings,
	}, nil
}

// UpdateCellParams 单元格更新参数：表名、目标列、主键列及值（用于 WHERE 等值定位）。
type UpdateCellParams struct {
	Table      string   // 表名（不含库前缀）
	SetColumn  string   // 目标列名
	SetValue   any      // 目标列新值（nil 表示 SET NULL）
	PKColumns  []string // 主键列名（可复合主键）
	PKValues   []any    // 主键值，与 PKColumns 顺序一致
}

// RunParamUpdate 执行单元格 UPDATE（named bind + 标识符引用，彻底防注入）：
//   - 表名/列名用 ss.QuoteIdentifier 按连接方言引用（防标识符注入）；
//   - 值通过 sqlx 命名参数（:set_val / :pk_N）绑定，而非字符串拼接（防值注入）。
//
// 生成形如：UPDATE `t` SET `c` = :set_val WHERE `id` = :pk_0
func RunParamUpdate(ctx context.Context, cli *cydb.DBCli, p UpdateCellParams) (int64, error) {
	if p.Table == "" || p.SetColumn == "" || len(p.PKColumns) == 0 {
		return 0, fmt.Errorf("表名/目标列/主键不能为空")
	}
	flavor := ss.FlavorForDatabase(cli.DBType())
	qt := ss.QuoteIdentifier(flavor, p.Table)
	qc := ss.QuoteIdentifier(flavor, p.SetColumn)

	var b strings.Builder
	b.WriteString("UPDATE ")
	b.WriteString(qt)
	b.WriteString(" SET ")
	b.WriteString(qc)
	b.WriteString(" = :set_val")

	params := cydb.PARAMS{"set_val": p.SetValue}
	if len(p.PKColumns) > 0 {
		b.WriteString(" WHERE ")
		for i, pk := range p.PKColumns {
			if i > 0 {
				b.WriteString(" AND ")
			}
			name := fmt.Sprintf("pk_%d", i)
			b.WriteString(ss.QuoteIdentifier(flavor, pk))
			b.WriteString(" = :")
			b.WriteString(name)
			params[name] = p.PKValues[i]
		}
	}

	affected, err := cli.DirectNamedExecuteContext(ctx, b.String(), params)
	if err != nil {
		return 0, fmt.Errorf("执行失败: %w", cleanDBError(err))
	}
	return affected, nil
}

// RunSQLScript 批量执行多语句 SQL（Navicat 式）：按分号分割，逐条判断读写，
// 读语句走查询（含 LIMIT 护栏与方言重构），写语句走执行，返回结果集数组（顺序与语句一致）。
//
// 语句分割复用底层库 cydb.SplitSQLStatements（正确处理字符串/引号/三种注释内的分号）。
// 写操作的安全确认由调用方（Web handler / 前端）在进入本函数前完成；
// 本函数仍会对每条语句做危险函数拦截（安全底线）。
func RunSQLScript(ctx context.Context, cli *cydb.DBCli, sql string, limit, offset int, mode string) ([]*SQLQueryResult, error) {
	// 语句分割方言：transform 模式按 MySQL 语法；raw 模式按连接定义的方言
	splitDialect := cli.DBType()
	if mode == "transform" {
		splitDialect = "mysql"
	}
	stmts := cydb.SplitSQLStatements(splitDialect, sql)
	if len(stmts) == 0 {
		return nil, fmt.Errorf("未检测到可执行的 SQL 语句")
	}
	results := make([]*SQLQueryResult, 0, len(stmts))
	for _, stmt := range stmts {
		if ClassifySQL(stmt) {
			// 写操作：逐条执行（危险函数仍拦截）
			r, err := RunSQLExec(ctx, cli, stmt)
			if err != nil {
				return nil, fmt.Errorf("第 %d 条语句执行失败: %w", len(results)+1, err)
			}
			results = append(results, r)
		} else {
			r, err := RunSQLQuery(ctx, cli, stmt, limit, offset, mode)
			if err != nil {
				return nil, fmt.Errorf("第 %d 条语句执行失败: %w", len(results)+1, err)
			}
			results = append(results, r)
		}
	}
	return results, nil
}

// Ping 检测数据库连接可用性（SELECT 1），返回耗时（毫秒）。
// 用于前端连接健康检测：连接断开/数据库不可达时返回错误。
func Ping(ctx context.Context, cli *cydb.DBCli) (int64, error) {
	start := time.Now()
	if _, _, err := cli.DirectQueryFastContext(ctx, "SELECT 1"); err != nil {
		return 0, fmt.Errorf("连接不可用: %w", cleanDBError(err))
	}
	return time.Since(start).Milliseconds(), nil
}
