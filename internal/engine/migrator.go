package engine

import (
	"context"
	"fmt"
	"strings"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/dialect"
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
		return nil, fmt.Errorf("未提供源或目标数据库连接")
	}
	t := newTracker(cb)

	sourceCli, err := Connect(*opts.Source)
	if err != nil {
		return nil, fmt.Errorf("源库连接失败: %w", err)
	}
	defer sourceCli.Close()
	// 迁移前确保目标库存在（不存在则自动创建），否则后续连接会直接失败
	if err := EnsureDBExists(*opts.Target, opts.Target.DBName); err != nil {
		return nil, fmt.Errorf("确保目标库 %s 存在失败: %w", opts.Target.DBName, err)
	}
	targetCli, err := Connect(*opts.Target)
	if err != nil {
		return nil, fmt.Errorf("目标库连接失败: %w", err)
	}
	defer targetCli.Close()

	// 1. 确定迁移表清单
	crossType := !strings.EqualFold(sourceCli.DBType(), targetCli.DBType())
	all, err := sourceCli.GetTables(opts.Source.DBName, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("获取源库表列表失败: %w", err)
	}
	// 视图走对象迁移通道 _views，不当作表迁移（视图无数据且无法按表建表）
	all = excludeViews(sourceCli, opts.Source.DBName, opts.Source.Schema, all)
	tables := filterTables(all, opts.Tables, opts.Source.DBName)
	if len(tables) == 0 {
		if crossType {
			return nil, fmt.Errorf("没有可迁移的表（跨类型迁移不支持仅迁移对象）")
		}
		if opts.Objects != nil && len(opts.Objects) == 0 {
			return nil, fmt.Errorf("没有选择任何表或对象")
		}
	}

	t.p.TotalUnits = len(tables)
	modeDesc := "同类型迁移"
	if crossType {
		modeDesc = fmt.Sprintf("跨类型迁移(%s → %s)", sourceCli.DBType(), targetCli.DBType())
	}
	t.log("开始%s: %d 张表, 重置模式=%s", modeDesc, len(tables), resetDesc(opts.ResetMode))

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	var totalRows int64
	backedUp := []string{}
	for _, table := range tables {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("任务已取消")
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
				ddl, err := buildCreateTableDDL(sourceCli, targetCli, table, crossType)
				if err != nil {
					return nil, fmt.Errorf("生成表 %s 建表语句失败: %w", table, err)
				}
				if _, err := targetCli.DirectExecute(ddl); err != nil {
					return nil, fmt.Errorf("创建目标表 %s 失败: %w", table, err)
				}
				t.log("已创建目标表 %s", table)
			}
		}

		// 4. 数据迁移
		if opts.SchemaOnly {
			t.p.DoneUnits++
			continue
		}
		rows, err := migrateTableData(ctx, sourceCli, targetCli, table, opts, batchSize, t)
		if err != nil {
			return nil, fmt.Errorf("迁移表 %s 数据失败: %w", table, err)
		}
		totalRows += rows
		t.p.DoneUnits++
		t.log("表 %s 迁移完成 (%d 行)", table, rows)
	}

	// 5. 对象迁移（仅同类型且非 DataOnly：视图/函数/存储过程；触发器已随建表语句迁移）
	if !crossType && !opts.DataOnly {
		migrateDBObjects(ctx, sourceCli, targetCli, opts.Source.DBName, opts.Source.Schema, opts.Objects, t)
	}

	// 6. 成功后清理备份表
	for _, table := range backedUp {
		if err := dropBackupTable(targetCli, table); err != nil {
			t.log("清理备份表失败（可忽略）: %v", err)
		}
	}

	t.finish()
	t.log("迁移完成: %d 张表, %d 行", len(tables), totalRows)
	return &MigrateResult{TotalTables: len(tables), TotalRows: totalRows}, nil
}

// buildCreateTableDDL 生成目标库建表语句：
// 同类型 → 源库方言原生 DDL（含触发器等）；跨类型 → 源库标准化 TableInfo + 目标 MigrationDialect 转换（仅表结构）
func buildCreateTableDDL(sourceCli, targetCli *cydb.DBCli, table string, crossType bool) (string, error) {
	if !crossType {
		content, err := sourceCli.GetDDLSql(dialect.FuncNameGetCreateTableSql, table)
		if err != nil {
			return "", err
		}
		if content == nil || strings.TrimSpace(content.Content) == "" {
			return "", fmt.Errorf("未获取到建表语句")
		}
		return strings.TrimRight(strings.TrimSpace(content.Content), ";"), nil
	}
	tableInfo, err := sourceCli.GetTableInfo(table)
	if err != nil {
		return "", err
	}
	md, ok := dialect.GetMigrationDialect(targetCli.DBType(), targetCli.DBSubType())
	if !ok {
		return "", fmt.Errorf("目标库类型 %s 不支持结构迁移", targetCli.DBType())
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
				if objectInWhitelist(allowed, db, dirName+"/"+name) {
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
				t.log("任务已取消，跳过剩余对象迁移")
				return
			}
			t.p.CurrentTable = db + "." + dirName + "/" + name
			t.emit(true)
			ddl, err := objectDDL(sourceCli, kind, name)
			if err != nil {
				t.log("获取%s %s.%s 失败（已跳过）: %v", dirName, db, name, err)
				t.p.DoneUnits++
				continue
			}
			// 与导入链路一致：执行规范化终止后的完整 DDL（存储过程/PL-SQL 块体内含分号，按单条语句直接执行）
			if _, err := targetCli.DirectExecute(terminateSQL(ddl)); err != nil {
				t.log("目标库执行%s %s.%s 失败（已跳过）: %v", dirName, db, name, err)
				t.p.DoneUnits++
				continue
			}
			t.p.DoneUnits++
			t.log("%s.%s/%s 迁移完成", db, dirName, name)
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
			return fmt.Errorf("批量写入失败: %w", err)
		}
		batch = batch[:0]
		return nil
	}

	err := sourceCli.ForEachQuery(table, selectSQL, func(rd cydb.RowData) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("任务已取消")
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
