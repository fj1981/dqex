package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/xid"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
)

const defaultSampleLimit = 10

// ErrEmptyDatabase 表示目标库内没有任何（非视图）表，可被子层跳过而非致命失败。
var ErrEmptyDatabase = fmt.Errorf("empty database")

// CreateSnapshot 连接数据库并创建快照：读取所有表结构 + 行数 + 可选采样。
// 作用域为单个库（Oracle 为 schema），通过 conn 的 DBName/Schema 确定。
func CreateSnapshot(ctx context.Context, conn *DBConnInfo, name, description string, opts CreateSnapshotOptions, cb ProgressFunc) (*Snapshot, error) {
	cli, err := Connect(*conn)
	if err != nil {
		return nil, NewMsgErrf(errSnapConn, err)
	}
	defer cli.Close()

	dbName := conn.DBName
	if strings.EqualFold(conn.Type, "oracle") && conn.Schema != "" {
		dbName = conn.Schema
	}
	if dbName == "" {
		return nil, NewMsgErr(errSnapNoDB)
	}

	// 获取库内表列表（剔除视图）
	var schemaPtr *string
	if strings.EqualFold(cli.DBType(), "oracle") {
		schemaPtr = &dbName
	}
	allTables, err := cli.GetTables(dbName, schemaPtr, nil)
	if err != nil {
		return nil, NewMsgErrf(errSnapListTables, err)
	}
	tables := excludeViews(cli, dbName, conn.Schema, allTables)
	if len(tables) == 0 {
		return nil, NewMsgErrf(errSnapEmpty, ErrEmptyDatabase, dbName)
	}

	sampleLimit := opts.SampleLimit
	if sampleLimit <= 0 {
		sampleLimit = defaultSampleLimit
	}

	now := time.Now()
	snap := &Snapshot{
		ID:          xid.New().String(),
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		ConnID:      "", // 由 service 层填充
		ConnLabel:   "",
		DBName:      dbName,
		DBType:      conn.Type,
		CreatedAt:   now.Unix(),
		Tables:      make([]SnapshotTable, 0, len(tables)),
	}

	t := newTracker(cb, opts.Lang)
	t.p.TotalUnits = len(tables)
	t.log(engineTextsFor(t.lang).snapStart, dbName, len(tables))

	for _, tbl := range tables {
		if err := ctx.Err(); err != nil {
			return nil, NewMsgErr(errCancelled)
		}
		t.p.CurrentTable = tbl
		t.emit(true)

		st, err := buildSnapshotTable(ctx, cli, tbl, opts.IncludeSamples, sampleLimit)
		if err != nil {
			t.log(engineTextsFor(t.lang).snapFail, tbl, err)
			t.p.DoneUnits++
			continue
		}
		snap.Tables = append(snap.Tables, *st)
		snap.TotalRows += st.RowCount
		t.p.DoneUnits++
		t.log(engineTextsFor(t.lang).snapRow, tbl, st.RowCount, len(st.Columns))
	}

	snap.TableCount = len(snap.Tables)
	t.finish()
	t.log(engineTextsFor(t.lang).snapDone, snap.TableCount, snap.TotalRows)
	return snap, nil
}

// buildSnapshotTable 为单表构建快照（列定义 + 主键 + 行数 + 可选采样）
func buildSnapshotTable(ctx context.Context, cli *cydb.DBCli, tableName string, includeSamples bool, sampleLimit int) (*SnapshotTable, error) {
	info, err := cli.GetTableInfo(tableName)
	if err != nil {
		return nil, NewMsgErrf(errSnapTableInfo, err)
	}

	cols := info.GetColumns()
	norm := typeNormalizer(cli)
	snapCols := make([]SnapshotColumn, 0, len(cols))
	for i, col := range cols {
		dt := col.GetOrginalDataType()
		snapCols = append(snapCols, SnapshotColumn{
			Name:           col.GetName(),
			DataType:       dt,
			NormalizedType: norm(dt), // 按快照库方言归一后固化，对比时直接比对，无需运行时推断
			Nullable:       !col.IsNotNull(),
			PrimaryKey:     col.IsPrimaryKey(),
			Position:       i + 1,
		})
	}

	pks, _ := cli.GetPrimaryKeys(tableName)

	st := &SnapshotTable{
		Name:       tableName,
		Columns:    snapCols,
		PrimaryKey: pks,
	}

	// 统计行数
	rowCount, err := countTableRows(cli, tableName)
	if err != nil {
		return st, nil // 行数统计失败不阻断
	}
	st.RowCount = rowCount

	// 可选采样
	if includeSamples && rowCount > 0 {
		samples, err := loadSampleRows(ctx, cli, tableName, sampleLimit)
		if err == nil {
			st.RowSamples = samples
		}
	}

	return st, nil
}

// loadSampleRows 加载表的前 N 行数据作为采样
func loadSampleRows(ctx context.Context, cli *cydb.DBCli, table string, limit int) ([]map[string]any, error) {
	selectSQL := fmt.Sprintf("SELECT * FROM %s", EscapeTable(cli.DBType(), cli.DBSubType(), table))
	var samples []map[string]any
	count := 0
	err := cli.ForEachQuery(table, selectSQL, func(rd cydb.RowData) error {
		if err := ctx.Err(); err != nil {
			return NewMsgErr(errCancelled)
		}
		if count >= limit {
			return fmt.Errorf("__stop__") // 达到上限后终止遍历
		}
		obj, err := rd.AsObject()
		if err != nil {
			return err
		}
		samples = append(samples, obj)
		count++
		return nil
	})
	if err != nil && err.Error() != "__stop__" {
		return nil, err
	}
	return samples, nil
}

// LoadSnapshot 从 JSON 文件加载快照完整数据
func LoadSnapshot(path string) (*Snapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, NewMsgErrf(errSnapRead, err)
	}
	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, NewMsgErrf(errSnapParse, err)
	}
	return &snap, nil
}
