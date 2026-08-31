package engine

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/dialect"
)

// exportDatabaseJSON 将整库导出为 DataPackage 数据包（<db>.json）。
// 仅覆盖表结构（type=0 建表）与行数据（type=1 按 PK）；视图/函数/存储过程无行级
// 语义，不适用数据包格式（需要对象导出请使用 FormatSQL）。
// 适用业务配置类中小表；无主键表仅导出结构并告警（跳过数据，导入侧同样跳过）。
// 条件导出（Conditions/DataMode）与 SQL 格式语义一致。
func exportDatabaseJSON(ctx context.Context, cli *cydb.DBCli, db string, tables []string, filePath string, opts ExportOptions, t *tracker) (int64, error) {
	pkg := &DataPackage{DB: db, MapIndex: map[string]int{}}
	var totalRows int64

	// ============ 建表 DDL（type=0）============
	if len(tables) > 0 && !opts.DataOnly {
		for _, table := range tables {
			if err := ctx.Err(); err != nil {
				return totalRows, NewMsgErr(errCancelled)
			}
			t.p.CurrentTable = db + "." + table
			t.emit(true)
			content, err := cli.GetDDLSql(dialect.FuncNameGetCreateTableSql, table)
			if err != nil {
				return totalRows, NewMsgErrf(errExpGenDDL, err)
			}
			if content != nil && strings.TrimSpace(content.Content) != "" {
				pkg.Add(table, DataEntry{Type: DataEntryCreateTable, Table: table, SQL: strings.TrimRight(strings.TrimSpace(content.Content), ";")})
			}
			t.p.DoneUnits++
			t.log(engineTextsFor(t.lang).expStructDone, db, table)
		}
	}

	// ============ 行数据（type=1，按 PK 组织）============
	if len(tables) > 0 && !opts.SchemaOnly {
		for _, table := range tables {
			if err := ctx.Err(); err != nil {
				return totalRows, NewMsgErr(errCancelled)
			}
			t.p.CurrentTable = db + "." + table
			t.emit(true)

			// 表级数据模式与 SQL 格式一致：skip 跳过、condition 条件取数
			cond := findCondition(opts.Conditions, db, table)
			dataMode := TableDataModeAll
			if cond != nil {
				dataMode = cond.DataMode
			}
			if dataMode == TableDataModeSkip {
				t.log(engineTextsFor(t.lang).expSkipData, db, table)
				t.p.DoneUnits++
				continue
			}

			pk, err := cli.GetPrimaryKeys(table)
			if err != nil {
				return totalRows, NewMsgErrf(errExpData, err, db, table)
			}
			if len(pk) == 0 {
				// 无主键表（B6 策略）：仅结构可复现，行数据无法精确导入/回滚，跳过并告警
				t.log(engineTextsFor(t.lang).expNoPKSkip, db, table)
				t.p.DoneUnits++
				continue
			}

			dbType, subType := cli.DBType(), cli.DBSubType()
			selectSQL := conditionQuery(dbType, subType, table, cond)
			if selectSQL == "" {
				selectSQL = "SELECT * FROM " + EscapeTable(dbType, subType, table)
			}
			rows, err := collectTableRows(ctx, cli, table, selectSQL, pk, pkg)
			if err != nil {
				return totalRows, NewMsgErrf(errExpData, err, db, table)
			}
			totalRows += rows
			t.p.DoneRows += rows
			t.p.DoneUnits++
			t.log(engineTextsFor(t.lang).expDataDone, db, table, rows)
			t.emit(false)
		}
	}

	// 序列化写出（数据包含明文业务数据，权限收紧 0600）
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		return totalRows, err
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return totalRows, err
	}
	return totalRows, os.WriteFile(filePath, data, 0o600)
}

// collectTableRows 读取表数据并入包（合并到同表同类型条目；整表驻留内存——数据包
// 格式契约限定中小表）。pk 必须写入条目（PK 字段）：导入侧按条目 PK 定位旧行与生成
// 回滚，缺失时数据条目会被整体跳过。
func collectTableRows(ctx context.Context, cli *cydb.DBCli, table, selectSQL string, pk []string, pkg *DataPackage) (int64, error) {
	var rows int64
	err := cli.DirectForEachQuery(table, selectSQL, func(rd cydb.RowData) error {
		if err := ctx.Err(); err != nil {
			return NewMsgErr(errCancelled)
		}
		obj, err := rd.AsObject()
		if err != nil {
			return err
		}
		pkg.Add(table, DataEntry{Type: DataEntryUpsertData, PK: pk, Data: []map[string]interface{}{obj}})
		rows++
		return nil
	})
	return rows, err
}
