// SQL 快速生成：按表浏览的行/单元格/过滤条件生成方言正确的可执行 SQL 文本。
// 复用 cydb 语句构建器 + InlineLiterals（字面量由方言层转义后内联），
// 生成文本与执行链路（RunParamInsert/RunParamDelete 等）同源转义，即生成即能执行。
package engine

import (
	"context"
	"fmt"
	"strings"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/ss"
)

// GenSQLKind 快速生成 SQL 的类型（白名单校验，防止前端被篡改后传入未知类型）。
type GenSQLKind string

const (
	GenSQLInsert         GenSQLKind = "insert"         // 行/多行 INSERT（跳过自增列）
	GenSQLUpdate         GenSQLKind = "update"         // 单行 UPDATE（SET 非主键列，WHERE 主键）
	GenSQLDelete         GenSQLKind = "delete"         // 单行 DELETE（WHERE 主键）
	GenSQLSelectByPK     GenSQLKind = "selectByPk"     // 单行 SELECT（WHERE 主键）
	GenSQLSelectByFilter GenSQLKind = "selectByFilter" // 按当前过滤条件 + 排序 SELECT
	GenSQLWhereCell      GenSQLKind = "whereCell"      // 单元格等于值条件 SELECT
)

// validGenSQLKinds 生成类型白名单
var validGenSQLKinds = map[GenSQLKind]bool{
	GenSQLInsert: true, GenSQLUpdate: true, GenSQLDelete: true,
	GenSQLSelectByPK: true, GenSQLSelectByFilter: true, GenSQLWhereCell: true,
}

// maxGenSQLRows 批量生成最大行数（防超长请求撑爆内存）
const maxGenSQLRows = 1000

// GenSQLParams 快速生成 SQL 请求参数。
// 行数据/主键值/单元格值均来自表浏览当前页展示数据；生成仅产出文本、不执行。
type GenSQLParams struct {
	Table       string         `json:"table"`       // 表名（不含库前缀）
	Kind        GenSQLKind     `json:"kind"`        // 生成类型
	Columns     []string       `json:"columns"`     // 列清单（与 Rows 列序一致；whereCell 时仅 1 个 = 条件列名）
	Rows        [][]any        `json:"rows"`        // 行数据（insert 支持多行；其余取首行；whereCell 时 [[值]]）
	PKColumns   []string       `json:"pkColumns"`   // 主键列名（update/delete/selectByPk）
	SkipColumns []string       `json:"skipColumns"` // 跳过的列（insert 的自增列）
	Filters     []ColumnFilter `json:"filters"`     // 过滤条件（selectByFilter）
	SortSpecs   []SortSpec     `json:"sortSpecs"`   // 排序（selectByFilter）
}

// GenerateSQL 按参数生成方言正确的 SQL 文本（多条语句以 ;\n 拼接，统一结尾分号）。
func GenerateSQL(_ context.Context, cli *cydb.DBCli, p GenSQLParams) (string, error) {
	if cli == nil {
		return "", NewMsgErr(errGenNoConn)
	}
	// 列名白名单：元数据可用时校验列真实存在（防篡改列名），失败降级为结构化转义
	// （cydb 渲染时按方言引用标识符，与 QueryTablePage 的降级策略一致）。
	validCols := map[string]bool{}
	if info, err := cli.GetTableInfo(p.Table); err == nil && info != nil {
		for _, c := range info.GetColumns() {
			validCols[strings.ToLower(c.GetName())] = true
		}
	}
	return genSQLText(cli.DBType(), p, validCols)
}

// genSQLText 生成 SQL 文本（与 cli 解耦，便于单测）：dbType 决定方言 flavor；
// validCols 为列名白名单（空 = 跳过白名单校验，仅结构化转义）。
func genSQLText(dbType string, p GenSQLParams, validCols map[string]bool) (string, error) {
	if p.Table == "" {
		return "", NewMsgErr(errGenNoTable)
	}
	if !validGenSQLKinds[p.Kind] {
		return "", NewMsgErr(errGenKind, p.Kind)
	}
	if len(p.Rows) > maxGenSQLRows {
		return "", NewMsgErr(errGenRowsLimit, maxGenSQLRows, len(p.Rows))
	}

	checkCol := func(col string) error {
		if len(validCols) > 0 && !validCols[strings.ToLower(col)] {
			return NewMsgErr(errGenColNotExist, col, p.Table)
		}
		return nil
	}

	flavor := ss.FlavorForDatabase(dbType)
	// build 以 InlineLiterals 模式构建语句：字面量由方言层转义后内联为可执行文本
	build := func(q def.SQLStmt) (string, error) {
		sql, args, err := q.BuildSQL(ss.BuildOptions{Flavor: flavor, InlineLiterals: true})
		if err != nil {
			return "", err
		}
		if len(args) != 0 {
			return "", NewMsgErr(errGenNoInline)
		}
		return sql, nil
	}

	// 主键值提取：按 PKColumns 顺序返回某行的主键值（校验行/列一致性，供 EQ/IN/TupleIN 共用）
	pkVals := func(row []any) ([]any, error) {
		if len(p.PKColumns) == 0 {
			return nil, NewMsgErr(errGenNoPK, p.Table)
		}
		if len(row) != len(p.Columns) {
			return nil, NewMsgErr(errGenRowMismatch)
		}
		colIdx := make(map[string]int, len(p.Columns))
		for i, c := range p.Columns {
			colIdx[strings.ToLower(c)] = i
		}
		vals := make([]any, 0, len(p.PKColumns))
		for _, pk := range p.PKColumns {
			if err := checkCol(pk); err != nil {
				return nil, err
			}
			idx, ok := colIdx[strings.ToLower(pk)]
			if !ok {
				return nil, NewMsgErr(errGenPKNotInCols, pk)
			}
			vals = append(vals, row[idx])
		}
		return vals, nil
	}

	// 主键条件：复合主键 AND 叠加，值经 ss.Lit 内联（方言层转义）
	pkConds := func(row []any) ([]ss.Condition, error) {
		vals, err := pkVals(row)
		if err != nil {
			return nil, err
		}
		conds := make([]ss.Condition, 0, len(p.PKColumns))
		for i, pk := range p.PKColumns {
			conds = append(conds, cydb.EQ(pk, ss.Lit(vals[i])))
		}
		return conds, nil
	}

	// 列清单 → cydb 列表达式（逐一白名单校验）
	selCols := func() ([]any, error) {
		out := make([]any, 0, len(p.Columns))
		for _, c := range p.Columns {
			if err := checkCol(c); err != nil {
				return nil, err
			}
			out = append(out, ss.Col(c))
		}
		return out, nil
	}

	var stmts []string
	switch p.Kind {
	case GenSQLInsert:
		// 跳过列（自增等由数据库生成的列），保持剩余列与原始行索引的映射
		skip := make(map[string]bool, len(p.SkipColumns))
		for _, c := range p.SkipColumns {
			skip[strings.ToLower(c)] = true
		}
		cols := make([]string, 0, len(p.Columns))
		colIdx := make([]int, 0, len(p.Columns))
		for i, c := range p.Columns {
			if skip[strings.ToLower(c)] {
				continue
			}
			if err := checkCol(c); err != nil {
				return "", err
			}
			cols = append(cols, c)
			colIdx = append(colIdx, i)
		}
		if len(cols) == 0 {
			return "", NewMsgErr(errGenNoInsertCols)
		}
		for ri, row := range p.Rows {
			if len(row) != len(p.Columns) {
				return "", NewMsgErr(errGenRowMismatchN, ri+1)
			}
			vals := make([]any, 0, len(colIdx))
			for _, i := range colIdx {
				vals = append(vals, ss.Lit(row[i]))
			}
			var q def.SQLStmt = ss.Q().Insert(p.Table).Columns(stringsToAny(cols)...).Values(vals...)
			sql, err := build(q)
			if err != nil {
				return "", NewMsgErrf(errGenInsert, err)
			}
			stmts = append(stmts, sql)
		}

	case GenSQLUpdate:
		if len(p.Rows) == 0 {
			return "", NewMsgErr(errGenNoRowData)
		}
		row := p.Rows[0]
		conds, err := pkConds(row)
		if err != nil {
			return "", err
		}
		// SET 非主键列（主键列用于 WHERE 定位）
		pkSet := make(map[string]bool, len(p.PKColumns))
		for _, pk := range p.PKColumns {
			pkSet[strings.ToLower(pk)] = true
		}
		assigns := make([]any, 0, len(p.Columns))
		for i, c := range p.Columns {
			if pkSet[strings.ToLower(c)] {
				continue
			}
			if err := checkCol(c); err != nil {
				return "", err
			}
			assigns = append(assigns, ss.Assign(ss.Col(c), ss.Lit(row[i])))
		}
		if len(assigns) == 0 {
			return "", NewMsgErr(errGenPKOnly, p.Table)
		}
		var q def.SQLStmt = ss.Q().Update(p.Table).Set(assigns...).Where(cydb.AND(conds...))
		sql, err := build(q)
		if err != nil {
			return "", NewMsgErrf(errGenUpdate, err)
		}
		stmts = append(stmts, sql)

	case GenSQLDelete:
		if len(p.Rows) == 0 {
			return "", NewMsgErr(errGenNoRowData)
		}
		if len(p.PKColumns) == 0 {
			return "", NewMsgErr(errGenNoPK, p.Table)
		}
		var cond ss.Condition
		switch {
		case len(p.Rows) == 1:
			// 单行：主键列 EQ AND 叠加（与 UPDATE/SELECT 定位一致）
			conds, err := pkConds(p.Rows[0])
			if err != nil {
				return "", err
			}
			cond = cydb.AND(conds...)
		case len(p.PKColumns) == 1:
			// 多行单列主键：pk IN (v1, v2, ...)，值经 ss.Lit 内联转义
			vals := make([]any, 0, len(p.Rows))
			for _, row := range p.Rows {
				pkV, err := pkVals(row)
				if err != nil {
					return "", err
				}
				vals = append(vals, ss.Lit(pkV[0]))
			}
			cond = cydb.IN(p.PKColumns[0], vals...)
		default:
			// 多行复合主键：(pk1, pk2) IN ((...), (...))，值经 TupleVals→Lit 内联转义
			valsList := make([][]any, 0, len(p.Rows))
			for _, row := range p.Rows {
				pkV, err := pkVals(row)
				if err != nil {
					return "", err
				}
				valsList = append(valsList, pkV)
			}
			cond = cydb.TupleIN(p.PKColumns, valsList)
		}
		var q def.SQLStmt = ss.Q().Delete(p.Table).Where(cond)
		sql, err := build(q)
		if err != nil {
			return "", NewMsgErrf(errGenDelete, err)
		}
		stmts = append(stmts, sql)

	case GenSQLSelectByPK:
		if len(p.Rows) == 0 {
			return "", NewMsgErr(errGenNoRowData)
		}
		conds, err := pkConds(p.Rows[0])
		if err != nil {
			return "", err
		}
		cols, err := selCols()
		if err != nil {
			return "", err
		}
		var q def.SQLStmt = ss.Q().Select(cols...).From(p.Table).Where(cydb.AND(conds...))
		sql, err := build(q)
		if err != nil {
			return "", NewMsgErrf(errGenSelect, err)
		}
		stmts = append(stmts, sql)

	case GenSQLWhereCell:
		// Columns[0] = 条件列名，Rows[0][0] = 单元格值（nil = NULL → IS NULL）
		if len(p.Columns) != 1 || len(p.Rows) == 0 || len(p.Rows[0]) != 1 {
			return "", NewMsgErr(errGenCellCond)
		}
		col := p.Columns[0]
		if err := checkCol(col); err != nil {
			return "", err
		}
		var cond ss.Condition
		if p.Rows[0][0] == nil {
			cond = cydb.ISNULL(col)
		} else {
			cond = cydb.EQ(col, ss.Lit(p.Rows[0][0]))
		}
		var q def.SQLStmt = ss.Q().Select(ss.Star()).From(p.Table).Where(cond)
		sql, err := build(q)
		if err != nil {
			return "", NewMsgErrf(errGenWhere, err)
		}
		stmts = append(stmts, sql)

	case GenSQLSelectByFilter:
		cols, err := selCols()
		if err != nil {
			return "", err
		}
		// 过滤/排序列名白名单
		for _, f := range p.Filters {
			if err := checkCol(f.Column); err != nil {
				return "", err
			}
		}
		for _, sp := range p.SortSpecs {
			if err := checkCol(sp.Column); err != nil {
				return "", err
			}
		}
		// 过滤条件：操作符白名单复用 validFilterOps，值经 ss.Lit 内联
		conds, err := buildFilterWheresLiteral(p.Filters)
		if err != nil {
			return "", err
		}
		var q def.SQLStmt = ss.Q().Select(cols...).From(p.Table)
		if len(conds) > 0 {
			q = q.Where(cydb.AND(conds...))
		}
		for _, sp := range p.SortSpecs {
			if sp.Column == "" {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(sp.Order), "desc") {
				q = q.OrderByDesc(sp.Column)
			} else {
				q = q.OrderByAsc(sp.Column)
			}
		}
		sql, err := build(q)
		if err != nil {
			return "", NewMsgErrf(errGenSelect, err)
		}
		stmts = append(stmts, sql)
	}

	if len(stmts) == 0 {
		return "", NewMsgErr(errGenNoStmt)
	}
	// 统一结尾分号（与导出 SQL 一致：各方言返回风格不一）
	parts := make([]string, len(stmts))
	for i, s := range stmts {
		parts[i] = terminateSQL(s)
	}
	return strings.Join(parts, "\n"), nil
}

// buildFilterWheresLiteral 与 buildFilterWheres 同构的过滤条件构建，区别：值经 ss.Lit 内联
// （InlineLiterals 模式下由方言层转义），生成可独立执行的 SELECT 文本；操作符白名单复用。
// LIKE 系列值仍以字符串传入，% 包裹与通配符转义由 cydb.LIKEC/LIKEL/LIKER 内部处理。
func buildFilterWheresLiteral(filters []ColumnFilter) ([]ss.Condition, error) {
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
		var cond ss.Condition
		switch f.Op {
		case FilterEq:
			cond = cydb.EQ(f.Column, ss.Lit(f.Value))
		case FilterNeq:
			cond = cydb.NEQ(f.Column, ss.Lit(f.Value))
		case FilterContains:
			cond = cydb.LIKEC(f.Column, fmt.Sprint(f.Value))
		case FilterNotContains:
			cond = cydb.NOT_LIKEC(f.Column, fmt.Sprint(f.Value))
		case FilterStartsWith:
			cond = cydb.LIKEL(f.Column, fmt.Sprint(f.Value))
		case FilterEndsWith:
			cond = cydb.LIKER(f.Column, fmt.Sprint(f.Value))
		case FilterGt:
			cond = cydb.GT(f.Column, ss.Lit(f.Value))
		case FilterGte:
			cond = cydb.GTE(f.Column, ss.Lit(f.Value))
		case FilterLt:
			cond = cydb.LT(f.Column, ss.Lit(f.Value))
		case FilterLte:
			cond = cydb.LTE(f.Column, ss.Lit(f.Value))
		case FilterIsNull:
			cond = cydb.ISNULL(f.Column)
		case FilterIsNotNull:
			cond = cydb.ISNOTNULL(f.Column)
		}
		conds = append(conds, cond)
	}
	return conds, nil
}
