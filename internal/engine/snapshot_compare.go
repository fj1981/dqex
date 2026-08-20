package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
)

// snapshotDatabases 返回快照的所有库（兼容 v1 单库快照：Databases 为空但 DBName/Tables 存在时包装为单库）
func snapshotDatabases(snap *Snapshot) []SnapshotDatabase {
	if len(snap.Databases) > 0 {
		return snap.Databases
	}
	if snap.DBName != "" || len(snap.Tables) > 0 {
		return []SnapshotDatabase{{
			DBName:     snap.DBName,
			TableCount: snap.TableCount,
			TotalRows:  snap.TotalRows,
			Tables:     snap.Tables,
		}}
	}
	return nil
}

// RunSnapshotCompare 快照 vs 在线库对比（多库）：逐库对比，结构对比直接比较快照列定义 vs 在线库列定义，
// 数据对比走在线库全量查询 + 快照侧行数辅助判断（快照不存全量数据，不做逐行对比）。
// 库对按 opts.DBMapping（快照库名→目标库名）配对，未指定时同名配对。结果按库分组到 CompareResult.Databases。
// 注意：每个目标库必须单独建立绑定该库名的 pooled 连接，
// 否则 GetTableInfo/DirectQuery 会查错库导致结构/数据张冠李戴（非首个快照库的所有列/行数都会失败）。
func RunSnapshotCompare(ctx context.Context, snapshot *Snapshot, targetConn *DBConnInfo, opts SnapshotCompareOptions, cb ProgressFunc) (*CompareResult, error) {
	dbs := snapshotDatabases(snapshot)
	if len(dbs) == 0 {
		return nil, NewMsgErr(errScmpNoData)
	}

	// 解析快照库→目标库映射（默认同名）
	targetScope := targetScopeDB(opts.Target)
	mapping := make(map[string]string, len(dbs))
	for _, sd := range dbs {
		tgt := sd.DBName
		if m, ok := opts.DBMapping[sd.DBName]; ok && m != "" {
			tgt = m
		} else if tgt == "" {
			tgt = targetScope // 快照库名缺失时回退到目标连接库
		}
		mapping[sd.DBName] = tgt
	}

	// 清空表结构元数据缓存
	cydb.FlushTableInfoCache()
	t := newTracker(cb, opts.Lang)

	// 首位库的 pooled 连接用于生成类型归一化器（不参与实际查询；类型归一器只依赖目标连接类型与 DBType）。
	// 每个库对的实际查询由 loop 里 ConnectPooled 出的绑定库名连接承担。
	probeCli, err := ConnectPooled(*targetConn, targetScope)
	if err != nil {
		return nil, NewMsgErrf(errScmpConn, err)
	}
	normTgt := typeNormalizer(probeCli)

	result := &CompareResult{
		Source:    fmt.Sprintf("%s (快照 %s)", snapshot.Name, time.Unix(snapshot.CreatedAt, 0).Format(time.RFC3339)),
		Target:    fmt.Sprintf("%s (当前 %s)", targetScope, probeCli.DBType()),
		Databases: make([]CompareDatabaseResult, 0, len(dbs)),
		Tables:    []CompareTableResult{},
	}

	for _, sd := range dbs {
		dbName := mapping[sd.DBName]
		if dbName == "" {
			dbName = targetScope
		}
		// 多库对比：每个库对必须用绑定了对应库名的 pooled 连接，
		// 否则 GetTableInfo 会查错库导致结构/数据张冠李戴（修复：非首个快照库的所有概要信息都会丢失）
		tgtConn := *targetConn
		tgtConn.DBName, tgtConn.Schema = scopeDBValue(targetConn, dbName)
		tgtCli, err := ConnectPooled(tgtConn, dbName)
		if err != nil {
			return nil, NewMsgErrf(errScmpConnDB, err, dbName)
		}
		dr, err := runSnapshotCompareDatabase(ctx, snapshot, sd, dbName, normTgt, tgtCli, opts, t)
		if err != nil {
			return nil, err
		}
		result.Databases = append(result.Databases, *dr)
	}

	for _, dr := range result.Databases {
		result.Tables = append(result.Tables, dr.Tables...)
		result.Summary = mergeSummary(result.Summary, dr.Summary)
	}
	t.finish()
	t.log(engineTextsFor(t.lang).scmpDone,
		len(result.Databases), result.Summary.Total, result.Summary.Matched, result.Summary.SourceOnly,
		result.Summary.TargetOnly, result.Summary.StructureDiff, result.Summary.DataDiff)
	return result, nil
}

// runSnapshotCompareDatabase 快照单库 vs 在线目标库对比
// targetCli 应为绑定到 dbName 的 pooled 连接；调用方负责确保目标连接库名与快照库映射一致。
func runSnapshotCompareDatabase(ctx context.Context, snapshot *Snapshot, sd SnapshotDatabase, dbName string,
	normTgt func(string) string, targetCli *cydb.DBCli, opts SnapshotCompareOptions, t *tracker) (*CompareDatabaseResult, error) {
	var schemaPtr *string
	if strings.EqualFold(targetCli.DBType(), "oracle") && dbName != "" {
		schemaPtr = &dbName
	}
	tgtAll, err := targetCli.GetTables(dbName, schemaPtr, nil)
	if err != nil {
		return nil, NewMsgErrf(errScmpListTables, err, dbName)
	}
	tgtAll = excludeViews(targetCli, dbName, "", tgtAll)

	snapTables := sd.Tables
	if opts.Tables != nil {
		wanted := make(map[string]bool, len(opts.Tables))
		for _, tbl := range opts.Tables {
			// 支持 "库.表" 限定名：库不匹配则该表不参与本库对比
			if db, tb, ok := splitQualifiedName(tbl); ok {
				if !strings.EqualFold(db, sd.DBName) {
					continue
				}
				wanted[strings.ToLower(tb)] = true
			} else {
				wanted[strings.ToLower(strings.TrimSpace(tbl))] = true
			}
		}
		filtered := make([]SnapshotTable, 0, len(snapTables))
		for _, st := range snapTables {
			if wanted[strings.ToLower(st.Name)] {
				filtered = append(filtered, st)
			}
		}
		snapTables = filtered
	}

	snapMap := make(map[string]*SnapshotTable, len(snapTables))
	for i := range snapTables {
		snapMap[strings.ToLower(snapTables[i].Name)] = &snapTables[i]
	}
	tgtMap := make(map[string]bool, len(tgtAll))
	for _, tb := range tgtAll {
		tgtMap[strings.ToLower(tb)] = true
	}

	type pair struct {
		name       string
		sourceName string
		targetName string
		status     string
	}
	pairs := make([]pair, 0, len(snapTables)+len(tgtAll))
	for _, st := range snapTables {
		key := strings.ToLower(st.Name)
		if tgtMap[key] {
			pairs = append(pairs, pair{name: st.Name, sourceName: st.Name, targetName: st.Name, status: compareStatusBoth})
		} else {
			pairs = append(pairs, pair{name: st.Name, sourceName: st.Name, status: compareStatusSourceOnly})
		}
	}
	usedSnap := make(map[string]bool, len(snapTables))
	for _, st := range snapTables {
		usedSnap[strings.ToLower(st.Name)] = true
	}
	for _, tb := range tgtAll {
		if !usedSnap[strings.ToLower(tb)] {
			pairs = append(pairs, pair{name: tb, targetName: tb, status: compareStatusTargetOnly})
		}
	}

	t.p.TotalUnits += len(pairs)
	t.log(engineTextsFor(t.lang).scmpStart, sd.DBName, dbName, len(pairs))

	dr := &CompareDatabaseResult{SourceDB: sd.DBName, TargetDB: dbName, Tables: []CompareTableResult{}}
	for _, p := range pairs {
		if err := ctx.Err(); err != nil {
			return nil, NewMsgErr(errCancelled)
		}
		t.p.CurrentTable = p.name
		t.emit(true)

		tr := CompareTableResult{
			Name:       p.name,
			SourceName: p.sourceName,
			TargetName: p.targetName,
			SourceDB:   sd.DBName,
			TargetDB:   dbName,
			Status:     p.status,
		}

		if p.status == compareStatusBoth {
			snapTable := snapMap[strings.ToLower(p.sourceName)]
			if snapTable == nil {
				continue
			}

			tgtCols, err := tableColumns(targetCli, p.targetName)
			if err != nil {
				t.log(engineTextsFor(t.lang).scmpStructFail, p.name, err)
			} else {
				srcCols := snapshotColumnsToColumnItems(snapTable.Columns)
				cols := diffColumns(srcCols, tgtCols, nil, normTgt)
				tr.Columns = cols

				structDiff := !cols.Matched
				if structDiff {
					tr.Data = &DataDiff{Mode: "skipped", SkippedReason: engineTextsFor(t.lang).skipStructRows}
				}
			}

			if tr.Data == nil {
				tgtRows, err := countTableRows(targetCli, p.targetName)
				if err != nil {
					t.log(engineTextsFor(t.lang).scmpRowFail, p.name, err)
				} else {
					dd := &DataDiff{
						Mode:       "count",
						SourceRows: snapTable.RowCount,
						TargetRows: tgtRows,
						Equal:      snapTable.RowCount == tgtRows,
					}
					if !dd.Equal {
						dd.SkippedReason = fmt.Sprintf(engineTextsFor(t.lang).skipRowChanged, snapTable.RowCount, tgtRows)
					}
					tr.Data = dd
				}
			}

			if len(snapTable.RowSamples) > 0 && tr.Data != nil && !tr.Data.Equal {
				tr.Data.ExtraSamples = snapTable.RowSamples
				if len(tr.Data.SampleColumns) == 0 {
					sampleCols := make([]string, 0, len(snapTable.Columns))
					for _, c := range snapTable.Columns {
						sampleCols = append(sampleCols, strings.ToLower(c.Name))
					}
					tr.Data.SampleColumns = sampleCols
				}
			}
		}

		dr.Tables = append(dr.Tables, tr)
		t.p.DoneUnits++
		t.log("%s: %s", p.name, tableResultDesc(&tr, t.lang))
	}
	dr.Summary = buildCompareSummary(dr.Tables)
	return dr, nil
}

// targetScopeDB 返回目标连接的作用域库名（oracle 为 schema），用于展示
func targetScopeDB(conn *DBConnInfo) string {
	if conn == nil {
		return ""
	}
	if strings.EqualFold(conn.Type, "oracle") {
		if conn.Schema != "" {
			return conn.Schema
		}
	}
	return conn.DBName
}

// snapshotColumnsToColumnItems 将快照列定义转换为 ColumnItem 列表（供 diffColumns 使用）
// 直接复用快照中已固化的 NormalizedType，无需再次按方言归一的
func snapshotColumnsToColumnItems(cols []SnapshotColumn) []ColumnItem {
	ret := make([]ColumnItem, 0, len(cols))
	for _, c := range cols {
		ret = append(ret, ColumnItem{
			Name:           c.Name,
			DataType:       c.DataType,
			NormalizedType: c.NormalizedType,
			Nullable:       c.Nullable,
			PrimaryKey:     c.PrimaryKey,
		})
	}
	return ret
}

// RunSnapshotCompareWithConn 快照对比的便捷入口：以连接配置构建多库 pooled 连接后执行对比
// （不再统一 Connect 成单一 cli；原因见 RunSnapshotCompare）
func RunSnapshotCompareWithConn(ctx context.Context, snapshot *Snapshot, targetConn *DBConnInfo, opts SnapshotCompareOptions, cb ProgressFunc) (*CompareResult, error) {
	// 清空表结构元数据缓存
	cydb.FlushTableInfoCache()
	return RunSnapshotCompare(ctx, snapshot, targetConn, opts, cb)
}
