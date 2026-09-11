// render.go 行数据 → 方言正确 SQL 文本渲染 + SQL 文本归一：库模式宿主（如 tl-env）
// 导出写盘薄壳复用。复用 cydb dialect 的 GetReplaceSql（与导入/回滚链路同源转义），
// 替代宿主手写方言补丁（PG 保留字表、MySQL 转义转换、bool→int 等均由方言层权威处理）。
package dqex

import (
	"fmt"
	"strings"

	"github.com/fj1981/dqex/internal/engine"
	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/def"
	"github.com/fj1981/infrakit/pkg/cydb/schema"
)

// RenderRowsSQL 把行数据（map 切片，键为列名）渲染为方言正确的可执行 SQL 文本，
// 每行一条语句（MySQL 为 REPLACE INTO，PG/Oracle 为方言 upsert），统一结尾分号。
// 列类型/标识符引用/值转义均取自 cli.GetTableInfo 元数据（与 cydb 导入链路同源）；
// 行中缺失的列自动省略（与旧宿主导出行为一致）。cli 为 nil 或无行时返回空串。
func RenderRowsSQL(cli *cydb.DBCli, table string, rows []map[string]any) (string, error) {
	if cli == nil {
		return "", fmt.Errorf("dqex: RenderRowsSQL: nil DBCli")
	}
	if len(rows) == 0 {
		return "", nil
	}
	info, err := cli.GetTableInfo(table)
	if err != nil {
		return "", fmt.Errorf("dqex: RenderRowsSQL: get table info %q: %w", table, err)
	}
	cols := info.GetColumns()
	var stmts []string
	for ri, row := range rows {
		fds := make([]def.FieldData, 0, len(row))
		// 按 TableInfo 列序输出，保证同表各行语句结构一致
		for i, c := range cols {
			v, ok := row[c.GetName()]
			if !ok {
				continue
			}
			fds = append(fds, schema.NewFieldData(v, i, info))
		}
		if len(fds) == 0 {
			continue
		}
		sql, err := schema.NewTableRowData(info, fds).GetReplaceSql()
		if err != nil {
			return "", fmt.Errorf("dqex: RenderRowsSQL: render row %d of %q: %w", ri+1, table, err)
		}
		stmts = append(stmts, sql)
	}
	return strings.Join(stmts, "\n"), nil
}

// StripSQLDBPrefix 清除 SQL 中的源库名前缀（`db`. / db.），使 MySQL 风格 SQL 可在
// PG/openGauss 等目标库上执行。前缀仅当命中 knownDBs（宿主按业务配置传入的库名
// 集合）时清除，避免误伤表别名前缀（t.col）；isMySQL 为 false 时同时移除全部反引号。
func StripSQLDBPrefix(isMySQL bool, sql string, knownDBs ...string) string {
	return engine.StripSQLDBPrefix(isMySQL, sql, knownDBs...)
}
