package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/dialect"
)

// MigrateResult 迁移结果
type MigrateResult struct {
	TotalTables int
	TotalRows   int64
}

// RunMigrate 执行迁移：源库 → 目标库。
// 两种模式的数据均以行数据为媒介（源库流式 SELECT → 批量 BatchReplace），不经过 SQL 文本。
// 同类型：表结构用源库原生 DDL（触发器随建表语句一并迁移），表迁移完成后追加迁移视图/函数/存储过程；
// 跨类型：仅迁表（结构由源库标准化 TableInfo + 目标方言 MigrationDialect 自动转换），
// 触发器/视图/函数/存储过程为源库方言对象，不迁移
func RunMigrate(ctx context.Context, opts MigrateOptions, cb ProgressFunc) (*MigrateResult, error) {
	if opts.Source == nil || opts.Target == nil {
		return nil, NewMsgErr(errMigNoConn)
	}
	t := newTracker(cb, opts.Lang)

	sourceCli, err := Connect(*opts.Source)
	if err != nil {
		return nil, NewMsgErrf(errMigSrcConn, err)
	}
	defer sourceCli.Close()
	// 迁移前确保目标库存在（不存在则自动创建），否则后续连接会直接失败
	if err := EnsureDBExists(*opts.Target, opts.Target.DBName); err != nil {
		return nil, NewMsgErrf(errMigEnsureDB, err, opts.Target.DBName)
	}
	targetCli, err := Connect(*opts.Target)
	if err != nil {
		return nil, NewMsgErrf(errMigTgtConn, err)
	}
	defer targetCli.Close()

	// 1. 确定迁移表清单
	crossType := !strings.EqualFold(sourceCli.DBType(), targetCli.DBType())
	// 视图走对象迁移通道 _views，不当作表迁移（视图无数据且无法按表建表）
	all, err := listSchemaTables(sourceCli, opts.Source.DBName, &opts.Source.Schema)
	if err != nil {
		return nil, NewMsgErrf(errMigListTables, err)
	}
	tables := filterTables(all, opts.Tables, opts.Source.DBName)
	if len(tables) == 0 {
		if crossType {
			return nil, NewMsgErr(errMigNoTablesObj)
		}
		if opts.Objects != nil && len(opts.Objects) == 0 {
			return nil, NewMsgErr(errMigNoSel)
		}
	}

	t.p.TotalUnits = len(tables)
	modeDesc := engineTextsFor(t.lang).modeSame
	if crossType {
		modeDesc = fmt.Sprintf(engineTextsFor(t.lang).modeCross, sourceCli.DBType(), targetCli.DBType())
	} else {
		modeDesc = engineTextsFor(t.lang).modeSame
	}
	t.log(engineTextsFor(t.lang).migStart, modeDesc, len(tables), resetDesc(opts.ResetMode, t.lang))

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	var totalRows int64
	backedUp := []string{}
	for _, table := range tables {
		if err := ctx.Err(); err != nil {
			return nil, NewMsgErr(errCancelled)
		}
		t.p.CurrentTable = table
		t.emit(true)

		// 2. 重置处理（备份 + truncate/drop）
		ok, err := resetTable(targetCli, table, opts.ResetMode, opts.Backup, t)
		if err != nil {
			return nil, err
		}
		if ok {
			backedUp = append(backedUp, table)
		}

		// 3. 结构迁移（目标表不存在时创建）
		if !opts.DataOnly {
			exist, err := targetCli.IsTableExist(table)
			if err != nil {
				return nil, err
			}
			if !exist {
				ddl, err := buildCreateTableDDL(sourceCli, targetCli, table, crossType, opts.CompatCollation)
				if err != nil {
					return nil, NewMsgErrf(errMigDDL, err, table)
				}
				if _, err := targetCli.DirectExecute(ddl); err != nil {
					return nil, NewMsgErrf(errMigCreateTable, err, table)
				}
				t.log(engineTextsFor(t.lang).migCreate, table)
			}
		}

		// 4. 数据迁移
		if opts.SchemaOnly {
			t.p.DoneUnits++
			continue
		}
		rows, err := migrateTableData(ctx, sourceCli, targetCli, table, opts, batchSize, t)
		if err != nil {
			return nil, NewMsgErrf(errMigData, err, table)
		}
		totalRows += rows
		t.p.DoneUnits++
		t.log(engineTextsFor(t.lang).migTableDone, table, rows)
	}

	// 5. 对象迁移（仅同类型且非 DataOnly：视图/函数/存储过程；触发器已随建表语句迁移）
	if !crossType && !opts.DataOnly {
		migrateDBObjects(ctx, sourceCli, targetCli, opts.Source.DBName, opts.Source.Schema, opts.Objects, t)
	}

	// 6. 成功后清理备份表
	for _, table := range backedUp {
		if err := dropBackupTable(targetCli, table); err != nil {
			t.log(engineTextsFor(t.lang).migCleanFail, err)
		}
	}

	t.finish()
	t.log(engineTextsFor(t.lang).migDone, len(tables), totalRows)
	return &MigrateResult{TotalTables: len(tables), TotalRows: totalRows}, nil
}

// buildCreateTableDDL 生成目标库建表语句：
// 同类型 → 源库方言原生 DDL（含触发器等，如果开启兼容排序规则则替换 8.0→5.7 兼容版本）；
// 跨类型 → 源库标准化 TableInfo + 目标 MigrationDialect 转换（仅表结构）
func buildCreateTableDDL(sourceCli, targetCli *cydb.DBCli, table string, crossType bool, compatCollation bool) (string, error) {
	if !crossType {
		content, err := sourceCli.GetDDLSql(dialect.FuncNameGetCreateTableSql, table)
		if err != nil {
			return "", err
		}
		if content == nil || strings.TrimSpace(content.Content) == "" {
			return "", NewMsgErr(errMigNoDDL)
		}
		ddl := strings.TrimRight(strings.TrimSpace(content.Content), ";")
		// MySQL 同类型迁移：将 8.0 特有排序规则替换为 5.7 兼容版本
		if compatCollation && strings.EqualFold(sourceCli.DBType(), "mysql") {
			ddl = compatCollationSQL(ddl)
		}
		return ddl, nil
	}
	tableInfo, err := sourceCli.GetTableInfo(table)
	if err != nil {
		return "", err
	}
	md, ok := dialect.GetMigrationDialect(targetCli.DBType(), targetCli.DBSubType())
	if !ok {
		return "", NewMsgErr(errMigTypeUnsupported, targetCli.DBType())
	}
	return md.GenerateCreateTableSQL(tableInfo), nil
}

// migrateDBObjects 将源库的视图/函数/存储过程迁移到目标库（仅同类型迁移调用，直接执行源库方言 DDL）。
// objects 为对象白名单（格式 子目录/对象名）：nil=全部迁移，空数组=不迁移。
// 按 视图→函数→存储过程 顺序执行；单个对象失败仅记录日志不阻断（对象属辅助能力，不影响已完成的表迁移）
func migrateDBObjects(ctx context.Context, sourceCli, targetCli *cydb.DBCli, db, schema string, objects []string, t *tracker) {
	if objects != nil && len(objects) == 0 {
		return // 显式指定了空列表：不迁移任何对象
	}
	allowed := make(map[string]bool, len(objects))
	for _, o := range objects {
		allowed[strings.TrimSpace(o)] = true
	}
	objs := listDBObjects(sourceCli, db, schema)
	for _, kind := range objectExportOrder {
		names := objs[kind]
		dirName := objectKindDirs[kind]
		if objects != nil {
			filtered := make([]string, 0, len(names))
			for _, name := range names {
				if objectInWhitelist(allowed, db, objectWhitelistID(dirName, name)) {
					filtered = append(filtered, name)
				}
			}
			names = filtered
		}
		if len(names) == 0 {
			continue
		}
		t.p.TotalUnits += len(names)
		t.emit(true)
		for _, name := range names {
			if err := ctx.Err(); err != nil {
				t.log("%s", engineTextsFor(t.lang).migCancel)
				return
			}
			t.p.CurrentTable = db + "." + dirName + "/" + name
			t.emit(true)
			ddl, err := objectDDL(sourceCli, kind, name)
			if err != nil {
				t.log(engineTextsFor(t.lang).migObjFail, dirName, db, name, err)
				t.p.DoneUnits++
				continue
			}
			// 与导入链路一致：执行规范化终止后的完整 DDL（存储过程/PL-SQL 块体内含分号，按单条语句直接执行）
			if _, err := targetCli.DirectExecute(terminateSQL(ddl)); err != nil {
				t.log(engineTextsFor(t.lang).migObjExecFail, dirName, db, name, err)
				t.p.DoneUnits++
				continue
			}
			t.p.DoneUnits++
			t.log(engineTextsFor(t.lang).migObjDone, db, dirName, name)
		}
	}
}

// migrateTableData 流式迁移单表数据：ForEachQuery → 批量 BatchReplace（目标已有同主键行时覆盖，避免主键冲突）
func migrateTableData(ctx context.Context, sourceCli, targetCli *cydb.DBCli, table string, opts MigrateOptions, batchSize int, t *tracker) (int64, error) {
	srcType, srcSub := sourceCli.DBType(), sourceCli.DBSubType()

	cond := findCondition(opts.Conditions, opts.Source.DBName, table)
	// 取数 SQL：条件统一为完整 SELECT（旧版 Where/Columns 归一化拼装），无条件时全表
	selectSQL := conditionQuery(srcType, srcSub, table, cond)
	if selectSQL == "" {
		selectSQL = fmt.Sprintf("SELECT * FROM %s", EscapeTable(srcType, srcSub, table))
	}

	var rows int64
	batch := make([]map[string]any, 0, batchSize)

	// 写入模式：REPLACE 覆盖写（MySQL/SQLite 原生支持；PG/Oracle 按主键转 upsert）。
	// PG/Oracle 目标无主键的表无法冲突覆盖，降级为普通 INSERT
	useReplace := true
	switch targetCli.DBType() {
	case "postgresql", "oracle":
		if ti, err := targetCli.GetTableInfo(table); err != nil || len(ti.GetPrimaryKeys()) == 0 {
			useReplace = false
		}
	}

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		var err error
		if useReplace {
			_, err = targetCli.BatchReplaceContext(ctx, table, batch)
		} else {
			_, err = targetCli.BatchInsertContext(ctx, table, batch)
		}
		if err != nil {
			return NewMsgErrf(errMigBatchWrite, err)
		}
		batch = batch[:0]
		return nil
	}

	err := sourceCli.ForEachQuery(table, selectSQL, func(rd cydb.RowData) error {
		if err := ctx.Err(); err != nil {
			return NewMsgErr(errCancelled)
		}
		obj, err := rd.AsObject()
		if err != nil {
			return err
		}
		batch = append(batch, obj)
		rows++
		t.p.DoneRows++
		if rows%100 == 0 {
			t.emit(false)
		}
		if len(batch) >= batchSize {
			return flush()
		}
		return nil
	})
	if err != nil {
		return rows, err
	}
	if err := flush(); err != nil {
		return rows, err
	}
	return rows, nil
}
