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

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/def"
	"github.com/fj1981/infrakit/pkg/cydb/ss"
	"github.com/xuri/excelize/v2"
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
	Error        string   `json:"error,omitempty"`   // 执行失败原因（非空时视为失败结果，正常返回给前端展示）
	Skipped      bool     `json:"skipped,omitempty"` // 未执行：因前面语句失败而跳过的语句（仅占位，无结果）
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
		return nil, NewMsgErr(errQryForbidden, strings.Join(forbidden, "; "))
	}
	isWrite := ClassifySQL(sql)
	if isWrite {
		return nil, NewMsgErr(errQryWriteOp, strings.TrimSpace(sql))
	}
	limit = normalizeLimit(limit)
	start := time.Now()

	// 执行模式分支：raw 直传；transform 由底层库 EnsureLimit 解析重构 + 补 LIMIT
	var (
		execSQL string
		rows    [][]any
		columns []string
		err     error
	)
	if mode == "raw" {
		execSQL = sql
		rows, columns, err = cli.DirectQueryFastContext(ctx, sql)
	} else {
		// 由底层库 EnsureLimit 负责「无 LIMIT 才补、已含 LIMIT 原样执行」：
		// 解析 AST → 判断是否已有限制 → 无则按方言重构追加上限（MySQL/PG 用 LIMIT，Oracle 用 ROWNUM）。
		execSQL, err = cli.EnsureLimit(sql, limit, offset)
		if err != nil {
			return nil, NewMsgErrf(errQryProcess, err)
		}
		rows, columns, err = cli.QueryWithLimitContext(ctx, sql, limit, offset)
	}
	if err != nil {
		return nil, NewMsgErrf(errQryFail, cleanDBError(err))
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

// FilterOp 列过滤操作符（前端 FilterOp 与后端严格对齐，后端枚举白名单校验防注入）。
type FilterOp string

const (
	FilterEq          FilterOp = "eq"          // 等于
	FilterNeq         FilterOp = "neq"         // 不等于
	FilterContains    FilterOp = "contains"    // 包含（LIKE %v%）
	FilterNotContains FilterOp = "notContains" // 不包含（NOT LIKE %v%）
	FilterStartsWith  FilterOp = "startsWith"  // 开头是（LIKE v%）
	FilterEndsWith    FilterOp = "endsWith"    // 结尾是（LIKE %v）
	FilterGt          FilterOp = "gt"          // 大于
	FilterGte         FilterOp = "gte"         // 大于等于
	FilterLt          FilterOp = "lt"          // 小于
	FilterLte         FilterOp = "lte"         // 小于等于
	FilterIsNull      FilterOp = "isNull"      // 为空
	FilterIsNotNull   FilterOp = "isNotNull"   // 非空
)

// ColumnFilter 单列过滤条件（值仅在 isNull/isNotNull 时为空）。
type ColumnFilter struct {
	Column string   `json:"column"`
	Op     FilterOp `json:"op"`
	Value  any      `json:"value,omitempty"`
}

// SortSpec 单列排序规格（多列排序：按切片顺序叠加 ORDER BY，优先级从高到低）。
type SortSpec struct {
	Column string `json:"column"`
	Order  string `json:"order"` // "asc" | "desc"
}

// validFilterOps 操作符白名单：后端拒绝未知操作符（前端可被篡改）。
var validFilterOps = map[FilterOp]bool{
	FilterEq: true, FilterNeq: true,
	FilterContains: true, FilterNotContains: true,
	FilterStartsWith: true, FilterEndsWith: true,
	FilterGt: true, FilterGte: true, FilterLt: true, FilterLte: true,
	FilterIsNull: true, FilterIsNotNull: true,
}

// buildFilterWheres 将过滤条件转换为 cydb 条件构建器（值自动参数绑定 + LIKE 自动转义）。
// column 必须是已通过列名白名单校验的列名；op 必须是白名单内的操作符。
// isBigField 用于限制大字段列：仅允许 isNull/isNotNull，其余操作符直接报错。
// 复用 cydb.EQ/NEQ/GT/GTE/LT/LTE/LIKEC/LIKEL/LIKER/NOT_LIKE*/ISNULL/ISNOTNULL，
// 不手写 SQL 拼接、不手写 LIKE 转义（cydb 内部已 EscapeLikePattern）。
func buildFilterWheres(filters []ColumnFilter, isBigField func(column string) bool) ([]ss.Condition, error) {
	if len(filters) == 0 {
		return nil, nil
	}
	conds := make([]ss.Condition, 0, len(filters))
	for _, f := range filters {
		if f.Column == "" {
			return nil, NewMsgErr(errFilterColEmpty)
		}
		if !validFilterOps[f.Op] {
			return nil, NewMsgErr(errFilterOp, f.Op)
		}
		// 大字段列限制：二进制/超长文本不支持文本过滤，仅允许空值判断
		if isBigField != nil && isBigField(f.Column) && f.Op != FilterIsNull && f.Op != FilterIsNotNull {
			return nil, NewMsgErr(errQryBigFieldFilter, f.Column)
		}
		var cond ss.Condition
		switch f.Op {
		case FilterEq:
			cond = cydb.EQ(f.Column, f.Value)
		case FilterNeq:
			cond = cydb.NEQ(f.Column, f.Value)
		case FilterContains:
			cond = cydb.LIKEC(f.Column, fmt.Sprint(f.Value))
		case FilterNotContains:
			cond = cydb.NOT_LIKEC(f.Column, fmt.Sprint(f.Value))
		case FilterStartsWith:
			cond = cydb.LIKEL(f.Column, fmt.Sprint(f.Value))
		case FilterEndsWith:
			cond = cydb.LIKER(f.Column, fmt.Sprint(f.Value))
		case FilterGt:
			cond = cydb.GT(f.Column, f.Value)
		case FilterGte:
			cond = cydb.GTE(f.Column, f.Value)
		case FilterLt:
			cond = cydb.LT(f.Column, f.Value)
		case FilterLte:
			cond = cydb.LTE(f.Column, f.Value)
		case FilterIsNull:
			cond = cydb.ISNULL(f.Column)
		case FilterIsNotNull:
			cond = cydb.ISNOTNULL(f.Column)
		}
		conds = append(conds, cond)
	}
	return conds, nil
}

// TablePageResult 表数据分页查询结果（对象树数据浏览专用），
// 一次返回当前页数据与全表总行数，避免前端再发一次 COUNT(*) 造成审计冗余。
type TablePageResult struct {
	Columns        []string `json:"columns"`
	Rows           [][]any  `json:"rows"`
	Total          int64    `json:"total"`
	Page           int      `json:"page"`
	PageSize       int      `json:"pageSize"`
	ExcludeColumns []string `json:"excludeColumns,omitempty"` // 被省略的大字段列名（值为 NULL，前端渲染占位）
}

// QueryTablePage 分页查询单表数据并返回全表总行数（对象树数据浏览专用）。
// table 为已校验的表名（调用方负责白名单/反引号转义）；sortSpecs 非空时按顺序叠加 ORDER BY（多列排序）。
// excludeColumns 为需要省略的大字段列名（二进制/超长文本），这些列在列表查询时用 NULL 占位，
// 前端据此渲染「点击加载」占位，避免一次传输大量无意义数据；点击单元格时再按需单独取值。
// 总数与数据通过 cydb 的 QueryPagedResult 一次获取（内部自动做 COUNT，忽略 ORDER BY/LIMIT/OFFSET），
// 与数据查询共用同一连接，杜绝前端二次 COUNT 的审计冗余与并发漂移。
//
// filters 为列过滤条件（AND 叠加），复用 cydb 条件构建器（值参数化绑定 + LIKE 自动转义），
// 不手写 SQL 拼接；列名通过 GetTableInfo 白名单校验（失败时降级为仅结构化转义，不阻塞查询）。
func QueryTablePage(ctx context.Context, cli *cydb.DBCli, table string, page, pageSize int, sortSpecs []SortSpec, excludeColumns []string, filters []ColumnFilter) (*TablePageResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 100
	}

	// 大字段列集合（前端已识别并传入，用于过滤操作符限制 + 结果置 NULL）
	excludeSet := make(map[string]bool, len(excludeColumns))
	for _, c := range excludeColumns {
		excludeSet[strings.ToLower(c)] = true
	}
	// isBigField 供 buildFilterWheres 限制大字段列过滤（仅允许 isNull/isNotNull）
	isBigField := func(column string) bool {
		return excludeSet[strings.ToLower(column)]
	}

	// 列名白名单：从表元数据拿列名集合，校验过滤列/排序列真实存在（防列名注入）。
	// 元数据不可用（如视图/无权限）时降级为仅结构化转义，不阻塞查询。
	// infoCols 按表结构顺序保存列名，用于「表无数据」时兜底补列头（cydb 底层 0 行时不返回列名）。
	validCols := map[string]bool{}
	var infoCols []string
	if info, err := cli.GetTableInfo(table); err == nil && info != nil {
		cols := info.GetColumns()
		infoCols = make([]string, 0, len(cols))
		for _, c := range cols {
			name := c.GetName()
			validCols[strings.ToLower(name)] = true
			infoCols = append(infoCols, name)
		}
	}
	// 过滤列名白名单校验
	for _, f := range filters {
		if len(validCols) > 0 && !validCols[strings.ToLower(f.Column)] {
			return nil, NewMsgErr(errQryFilterColNotExist, f.Column, table)
		}
	}
	// 排序列名白名单校验
	for _, sp := range sortSpecs {
		if sp.Column != "" && len(validCols) > 0 && !validCols[strings.ToLower(sp.Column)] {
			return nil, NewMsgErr(errQrySortColNotExist, sp.Column, table)
		}
	}

	// 过滤条件 → cydb 条件构建器（值参数化绑定 + LIKE 自动转义）
	conds, err := buildFilterWheres(filters, isBigField)
	if err != nil {
		return nil, err
	}

	// 用 cydb 的 ss.Q 链式查询构建分页查询：表名/列名均为结构化标识符（渲染时按方言转义），
	// 分页/排序/计数全部交给 cydb 处理，跨方言（MySQL/PG/Oracle）无需手写 LIMIT/OFFSET/ROWNUM。
	// 多列排序：按 sortSpecs 顺序依次 OrderByAsc/OrderByDesc（cydb 内部 append 叠加，不覆盖）。
	var q def.SQLStmt = ss.Q()
	if len(conds) > 0 {
		q = q.Where(cydb.AND(conds...))
	}
	q = q.From(table).SelectIfEmpty(ss.Star())
	for _, sp := range sortSpecs {
		if sp.Column == "" {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(sp.Order), "desc") {
			q = q.OrderByDesc(sp.Column)
		} else {
			q = q.OrderByAsc(sp.Column)
		}
	}
	q = q.Page(page, pageSize)

	res := q.QueryPagedResult(cli, ss.WithExecContext(ctx))
	if res.HasError() {
		return nil, NewMsgErrf(errQryFail, cleanDBError(res.Err()))
	}
	rows, columns, err := res.RawData()
	if err != nil {
		return nil, NewMsgErrf(errQryParseResult, err)
	}
	if rows == nil {
		rows = [][]any{}
	}
	if columns == nil {
		columns = []string{}
	}
	// 兜底：表无数据时 cydb 底层 scanSQLRowsEfficient 只在有首行时才返回列名，
	// 导致 columns 为空、前端无表头。此处用表元数据列名补齐，保证「空表也有表头」。
	if len(columns) == 0 && len(infoCols) > 0 {
		columns = infoCols
	}

	// 大字段省略：将指定列在结果中置为 NULL（不取真实数据），并记录实际省略的列（仅当该列真实存在于结果列中）。
	// 置 NULL 而非删除列，保持 columns 顺序与前端「列名 → 索引」映射一致。
	excluded := make([]string, 0, len(excludeColumns))
	for ci, col := range columns {
		if excludeSet[strings.ToLower(col)] {
			excluded = append(excluded, col)
			for ri := range rows {
				if ci < len(rows[ri]) {
					rows[ri][ci] = nil
				}
			}
		}
	}

	// 总数：QueryPagedResult 已内部 COUNT（去 ORDER BY/LIMIT/OFFSET），直接取。
	total := res.GetTotalCount()

	return &TablePageResult{
		Columns:        columns,
		Rows:           rows,
		Total:          total,
		Page:           page,
		PageSize:       pageSize,
		ExcludeColumns: excluded,
	}, nil
}

// ExportTableExcel 将表数据（应用过滤/排序）导出为 Excel（.xlsx）字节流。
// 复用 QueryTablePage 拿数据（pageSize=maxRows，一次取回上限内的全部行），
// 再用 excelize 内存生成单 sheet 数据表（列头加粗 + 冻结首行），不落盘。
// maxRows 为导出行数上限（防内存溢出，超限截断并返回已截断标志）。
func ExportTableExcel(ctx context.Context, cli *cydb.DBCli, table string, sortSpecs []SortSpec, filters []ColumnFilter, maxRows int) ([]byte, int64, bool, error) {
	if maxRows <= 0 {
		maxRows = 100000
	}
	// 导出为离线完整数据交付：不传 excludeColumns，大字段列取真实内容（列表展示才置 NULL 省流）
	res, err := QueryTablePage(ctx, cli, table, 1, maxRows, sortSpecs, nil, filters)
	if err != nil {
		return nil, 0, false, err
	}

	truncated := res.Total > int64(maxRows)

	f := excelize.NewFile()
	defer f.Close()
	sheet := "Sheet1"
	// 列头
	for ci, col := range res.Columns {
		cell, _ := excelize.CoordinatesToCellName(ci+1, 1)
		if err := f.SetCellValue(sheet, cell, col); err != nil {
			return nil, 0, false, NewMsgErrf(errQryWriteHeader, err)
		}
	}
	// 数据行
	for ri, row := range res.Rows {
		for ci, v := range row {
			cell, _ := excelize.CoordinatesToCellName(ci+1, ri+2)
			if err := f.SetCellValue(sheet, cell, v); err != nil {
				return nil, 0, false, NewMsgErrf(errQryWriteData, err)
			}
		}
	}
	// 表头加粗
	if len(res.Columns) > 0 {
		lastCol, _ := excelize.ColumnNumberToName(len(res.Columns))
		if err := f.SetPanes(sheet, &excelize.Panes{Freeze: true, YSplit: 1, TopLeftCell: "A2", ActivePane: "bottomLeft"}); err != nil {
			return nil, 0, false, err
		}
		_ = lastCol
	}

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, 0, false, NewMsgErrf(errQryGenExcel, err)
	}
	return buf.Bytes(), res.Total, truncated, nil
}

// RunSQLExec 执行写操作 SQL（INSERT/UPDATE/DELETE/DDL）。
// 调用方（Web handler）必须已完成危险语句拦截与写操作确认。
func RunSQLExec(ctx context.Context, cli *cydb.DBCli, sql string) (*SQLQueryResult, error) {
	warnings, forbidden := CheckDangerous(sql)
	if len(forbidden) > 0 {
		return nil, NewMsgErr(errQryForbidden, strings.Join(forbidden, "; "))
	}
	start := time.Now()
	affected, err := cli.DirectExecuteContext(ctx, sql)
	if err != nil {
		return nil, NewMsgErrf(errQryExecFail, cleanDBError(err))
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
	Table     string   // 表名（不含库前缀）
	SetColumn string   // 目标列名
	SetValue  any      // 目标列新值（nil 表示 SET NULL）
	PKColumns []string // 主键列名（可复合主键）
	PKValues  []any    // 主键值，与 PKColumns 顺序一致
}

// RunParamUpdate 执行单元格 UPDATE（复用 cydb 语句构建器 + 命名参数绑定，彻底防注入）：
//   - 表名/列名由 cydb 的 ss.Update/AssignParam 按连接方言引用（防标识符注入）；
//   - 值通过命名参数（:set_val / :pk_N）绑定，而非字符串拼接（防值注入）。
//
// 生成形如：UPDATE `t` SET `c` = :set_val WHERE `id` = :pk_0
func RunParamUpdate(ctx context.Context, cli *cydb.DBCli, p UpdateCellParams) (int64, error) {
	if p.Table == "" || p.SetColumn == "" || len(p.PKColumns) == 0 {
		return 0, NewMsgErr(errQryNoIdent)
	}

	params := cydb.PARAMS{"set_val": p.SetValue}
	pkConds := make([]cydb.Where, 0, len(p.PKColumns))
	for i, pk := range p.PKColumns {
		name := fmt.Sprintf("pk_%d", i)
		pkConds = append(pkConds, cydb.EQ(pk, ss.Param(name)))
		params[name] = p.PKValues[i]
	}

	var q def.SQLStmt = ss.Q().Update(p.Table).Set(ss.AssignParam(p.SetColumn, "set_val"))
	q = q.Where(cydb.AND(pkConds...))
	sql, _, err := q.BuildSQL(ss.BuildOptions{Flavor: ss.FlavorForDatabase(cli.DBType())})
	if err != nil {
		return 0, NewMsgErrf(errQryBuildSQL, err)
	}

	affected, err := cli.DirectNamedExecuteContext(ctx, sql, params)
	if err != nil {
		return 0, NewMsgErrf(errQryExecFail, cleanDBError(err))
	}
	return affected, nil
}

// DeleteRowParams 删除整行参数：表名、主键列及值（用于 WHERE 等值定位）。
type DeleteRowParams struct {
	Table     string   // 表名（不含库前缀）
	PKColumns []string // 主键列名（可复合主键）
	PKValues  []any    // 主键值，与 PKColumns 顺序一致
}

// RunParamDelete 执行整行 DELETE（复用 cydb 语句构建器 + 命名参数绑定，彻底防注入）：
//   - 表名/主键列由 cydb 的 ss.Delete/cydb.EQ 按连接方言引用（防标识符注入）；
//   - 主键值通过命名参数（:pk_N）绑定，而非字符串拼接（防值注入）。
//
// 生成形如：DELETE FROM `t` WHERE `id` = :pk_0 AND `c` = :pk_1
func RunParamDelete(ctx context.Context, cli *cydb.DBCli, p DeleteRowParams) (int64, error) {
	if p.Table == "" || len(p.PKColumns) == 0 {
		return 0, NewMsgErr(errQryNoPKIdent)
	}
	if len(p.PKColumns) != len(p.PKValues) {
		return 0, NewMsgErr(errQryPKValueMismatch)
	}

	params := cydb.PARAMS{}
	pkConds := make([]cydb.Where, 0, len(p.PKColumns))
	for i, pk := range p.PKColumns {
		name := fmt.Sprintf("pk_%d", i)
		pkConds = append(pkConds, cydb.EQ(pk, ss.Param(name)))
		params[name] = p.PKValues[i]
	}

	var q def.SQLStmt = ss.Q().Delete(p.Table)
	q = q.Where(cydb.AND(pkConds...))
	sql, _, err := q.BuildSQL(ss.BuildOptions{Flavor: ss.FlavorForDatabase(cli.DBType())})
	if err != nil {
		return 0, NewMsgErrf(errQryBuildSQL, err)
	}

	affected, err := cli.DirectNamedExecuteContext(ctx, sql, params)
	if err != nil {
		return 0, NewMsgErrf(errQryExecFail, cleanDBError(err))
	}
	return affected, nil
}

// InsertRowParams 新增行参数：表名、目标列名与值（列值映射）。
// 自增主键列通常不在 columns 中（由数据库生成），仅传用户显式填写的列。
type InsertRowParams struct {
	Table   string   // 表名（不含库前缀）
	Columns []string // 要写入的列名（与 Values 顺序一致）
	Values  []any    // 对应列的值（nil 表示 NULL）
}

// RunParamInsert 执行 INSERT（复用 cydb 语句构建器 + 命名参数绑定，彻底防注入）：
//   - 表名/列名由 cydb 的 ss.Insert/Columns 按连接方言引用（防标识符注入）；
//   - 值通过命名参数（:v_N）绑定，而非字符串拼接（防值注入）。
//
// 生成形如：INSERT INTO `t` (`a`,`b`) VALUES (:v_0,:v_1)
func RunParamInsert(ctx context.Context, cli *cydb.DBCli, p InsertRowParams) (int64, error) {
	if p.Table == "" || len(p.Columns) == 0 {
		return 0, NewMsgErr(errQryNoColIdent)
	}
	if len(p.Columns) != len(p.Values) {
		return 0, NewMsgErr(errQryColValueMismatch)
	}

	params := cydb.PARAMS{}
	valueExprs := make([]any, len(p.Columns))
	for i := range p.Columns {
		name := fmt.Sprintf("v_%d", i)
		valueExprs[i] = ss.Param(name)
		params[name] = p.Values[i]
	}

	var q def.SQLStmt = ss.Q().Insert(p.Table).Columns(stringsToAny(p.Columns)...).Values(valueExprs...)
	sql, _, err := q.BuildSQL(ss.BuildOptions{Flavor: ss.FlavorForDatabase(cli.DBType())})
	if err != nil {
		return 0, NewMsgErrf(errQryBuildSQL, err)
	}

	affected, err := cli.DirectNamedExecuteContext(ctx, sql, params)
	if err != nil {
		return 0, NewMsgErrf(errQryExecFail, cleanDBError(err))
	}
	return affected, nil
}

// stringsToAny 将 []string 转为 []any（供 cydb 构建器的 variadic 参数使用）。
func stringsToAny(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// GetCellValue 按主键 + 列名定位单行单列，返回该单元格完整值（大字段懒加载）。
// 复用 cydb 语句构建器：表名/列名/主键列按连接方言引用（防标识符注入）；
// 主键值用命名参数绑定（防值注入）。查询不到行时返回 nil。
func GetCellValue(ctx context.Context, cli *cydb.DBCli, table, column string, pkColumns []string, pkValues []any) (any, error) {
	if table == "" || column == "" || len(pkColumns) == 0 {
		return nil, NewMsgErr(errQryNoIdentAll)
	}

	params := cydb.PARAMS{}
	pkConds := make([]cydb.Where, 0, len(pkColumns))
	for i, pk := range pkColumns {
		name := fmt.Sprintf("pk_%d", i)
		pkConds = append(pkConds, cydb.EQ(pk, ss.Param(name)))
		params[name] = pkValues[i]
	}

	var q def.SQLStmt = ss.Q().Select(column).From(table)
	q = q.Where(cydb.AND(pkConds...))
	sql, _, err := q.BuildSQL(ss.BuildOptions{Flavor: ss.FlavorForDatabase(cli.DBType())})
	if err != nil {
		return nil, NewMsgErrf(errQryBuildSQL, err)
	}

	// 单行查询（主键唯一），返回 [值, 列名]；无行时返回 nil
	row, _, err := cli.DirectNamedQueryRowFastContext(ctx, sql, params)
	if err != nil {
		return nil, NewMsgErrf(errQryFail, cleanDBError(err))
	}
	if len(row) == 0 {
		return nil, nil
	}
	return row[0], nil
}

// RunSQLScript 批量执行多语句 SQL（Navicat 式）：按分号分割，逐条判断读写，
// 读语句走查询（含 LIMIT 护栏与方言重构），写语句走执行，返回结果集数组（顺序与语句一致）。
//
// 失败策略：语句级失败时停止后续执行（避免依赖后续语句错乱），但保留已成功的结果集，
// 并在失败语句的对应位置插入错误占位结果（Error 非空），后续语句逐个插入「未执行」占位
// （Skipped=true）——前端在多结果集 tab 上定位显示，而非整体失败丢弃全部结果。
// 错误占位携带失败语句原文（SQL 字段），不依赖「第 N 条」序号。
//
// 语句分割复用底层库 cydb.SplitSQLStatements（正确处理字符串/引号/三种注释内的分号）。
// 写操作的安全确认由调用方（Web handler / 前端）在进入本函数前完成；
// 本函数仍会对每条语句做危险函数拦截（安全底线）。
// RunSQLScript 批量执行多语句脚本：按分号分割，逐条判断读写并执行，返回结果集数组。
// connKey 用于审计钩子归属（QueryHooks 经 ctx 注册时逐语句回调 OnQuery）。
func RunSQLScript(ctx context.Context, cli *cydb.DBCli, sql string, limit, offset int, mode string, connKey string) ([]*SQLQueryResult, error) {
	// 语句分割方言：transform 模式按 MySQL 语法；raw 模式按连接定义的方言
	splitDialect := cli.DBType()
	if mode == "transform" {
		splitDialect = "mysql"
	}
	stmts := cydb.SplitSQLStatements(splitDialect, sql)
	if len(stmts) == 0 {
		return nil, NewMsgErr(errQryNoSQL)
	}
	results := make([]*SQLQueryResult, 0, len(stmts))
	for i, stmt := range stmts {
		if ClassifySQL(stmt) {
			// 写操作：逐条执行（危险函数仍拦截）；审计钩子耗时统一由局部 start 计算，
			// 保证成功/失败两条路径 costMs 口径一致
			start := time.Now()
			r, err := RunSQLExec(ctx, cli, stmt)
			if err != nil {
				fireQueryHook(ctx, connKey, stmt, start, -1)
				results = append(results, &SQLQueryResult{SQL: stmt, IsWrite: true, Error: err.Error()})
				appendSkipped(results, stmts[i+1:])
				return results, nil
			}
			fireQueryHook(ctx, connKey, stmt, start, r.AffectedRows)
			results = append(results, r)
		} else {
			start := time.Now()
			r, err := RunSQLQuery(ctx, cli, stmt, limit, offset, mode)
			if err != nil {
				fireQueryHook(ctx, connKey, stmt, start, -1)
				results = append(results, &SQLQueryResult{SQL: stmt, Error: err.Error()})
				appendSkipped(results, stmts[i+1:])
				return results, nil
			}
			fireQueryHook(ctx, connKey, stmt, start, int64(r.RowCount))
			results = append(results, r)
		}
	}
	return results, nil
}

// appendSkipped 将未执行的剩余语句逐个追加为「未执行」占位结果（Skipped=true）。
// 后续语句可能依赖失败语句（如先建表后插数），继续执行只会产生连锁报错噪音，
// 故统一跳过并在结果集数组中保留对应位置，前端可逐条标识未执行。
func appendSkipped(results []*SQLQueryResult, rest []string) []*SQLQueryResult {
	for _, s := range rest {
		results = append(results, &SQLQueryResult{SQL: s, IsWrite: ClassifySQL(s), Skipped: true})
	}
	return results
}

// Ping 检测数据库连接可用性（SELECT 1），返回耗时（毫秒）。
// 用于前端连接健康检测：连接断开/数据库不可达时返回错误。
func Ping(ctx context.Context, cli *cydb.DBCli) (int64, error) {
	start := time.Now()
	if _, _, err := cli.DirectQueryFastContext(ctx, "SELECT 1"); err != nil {
		return 0, NewMsgErrf(errQryConnUnavailable, cleanDBError(err))
	}
	return time.Since(start).Milliseconds(), nil
}
