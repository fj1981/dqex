package engine

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/dialect"
)

// MigrateResult 迁移结果
type MigrateResult struct {
	TotalTables int
	TotalRows   int64
}

// RunMigrate 执行迁移：源库 → 目标库。
// 结构化选择（Selections）时逐库迁移：每库独立连接源侧，目标库为连接配置库（未配置时与源库同名，不存在自动创建）；
// 旧格式（Tables/Objects）保持单库（源连接配置库）。Oracle 的“库”语义为 schema。
// 两种模式的数据均以行数据为媒介（源库流式 SELECT → 批量 BatchReplace），不经过 SQL 文本。
// 同类型：表结构用源库原生 DDL（触发器随建表语句一并迁移），表迁移完成后追加迁移视图/函数/存储过程；
// 跨类型：仅迁表（结构由源库标准化 TableInfo + 目标方言 MigrationDialect 自动转换），
// 触发器/视图/函数/存储过程为源库方言对象，不迁移
func RunMigrate(ctx context.Context, opts MigrateOptions, cb ProgressFunc) (*MigrateResult, error) {
	if opts.Source == nil || opts.Target == nil {
		return nil, NewMsgErr(errMigNoConn)
	}
	t := newTracker(cb, opts.Lang)

	batchSize := opts.BatchSize
	if batchSize <= 0 {
		batchSize = DefaultBatchSize
	}

	// 迁移任务列表：结构化选择（库→表/对象）时逐库；旧格式回退单库（源连接配置库）
	type dbJob struct {
		srcDB, tgtDB string
		srcSchema    string // oracle 的“库”=schema
		tables       []string
		objects      []string
	}
	jobs := make([]dbJob, 0, 1)
	if len(opts.Selections) > 0 {
		for _, sel := range opts.Selections {
			tgtDB := opts.Target.DBName
			if tgtDB == "" {
				tgtDB = sel.DB // 目标未配置库时与源库同名
			}
			jobs = append(jobs, dbJob{srcDB: sel.DB, tgtDB: tgtDB, tables: sel.Tables, objects: sel.Objects})
		}
	} else {
		jobs = append(jobs, dbJob{srcDB: opts.Source.DBName, tgtDB: opts.Target.DBName, tables: opts.Tables, objects: opts.Objects})
	}

	var totalRows int64
	totalTables := 0
	for _, job := range jobs {
		// 连接源库（oracle 的库=schema，写入 Schema 而非 DBName）
		srcConn := *opts.Source
		if strings.EqualFold(opts.Source.Type, "oracle") {
			srcConn.DBName = ""
			srcConn.Schema = job.srcDB
			job.srcSchema = job.srcDB
		} else {
			srcConn.DBName = job.srcDB
		}
		sourceCli, err := Connect(srcConn)
		if err != nil {
			return nil, NewMsgErrf(errMigSrcConn, err)
		}
		// 迁移前确保目标库存在（不存在则自动创建），否则后续连接会直接失败
		tgtConn := *opts.Target
		if strings.EqualFold(opts.Target.Type, "oracle") {
			tgtConn.DBName = ""
			tgtConn.Schema = job.tgtDB
		} else {
			tgtConn.DBName = job.tgtDB
		}
		if err := EnsureDBExists(tgtConn, job.tgtDB); err != nil {
			sourceCli.Close()
			return nil, NewMsgErrf(errMigEnsureDB, err, job.tgtDB)
		}
		targetCli, err := Connect(tgtConn)
		if err != nil {
			sourceCli.Close()
			return nil, NewMsgErrf(errMigTgtConn, err)
		}
		// 数据写入前挂起目标库约束检查（自引用/跨表外键的行序无法保证，见 suspendTargetChecks 注释）
		suspendTargetChecks(targetCli, t)

		// 1. 确定迁移表清单
		crossType := !strings.EqualFold(sourceCli.DBType(), targetCli.DBType())
		// 视图走对象迁移通道 _views，不当作表迁移（视图无数据且无法按表建表）
		all, err := listSchemaTables(sourceCli, job.srcDB, &job.srcSchema)
		if err != nil {
			sourceCli.Close()
			targetCli.Close()
			return nil, NewMsgErrf(errMigListTables, err)
		}
		tables := filterTables(all, job.tables, job.srcDB)
		if len(tables) == 0 {
			sourceCli.Close()
			targetCli.Close()
			if crossType {
				return nil, NewMsgErr(errMigNoTablesObj)
			}
			if job.objects != nil && len(job.objects) == 0 {
				return nil, NewMsgErr(errMigNoSel)
			}
		}
		// 按外键依赖拓扑排序：被引用表先创建/迁移，避免依赖表找不到父表
		tables = sortTablesByFK(sourceCli, tables, t)

		t.p.TotalUnits += len(tables)
		// 同类型且非 DataOnly 才导出对象，跨类型不导出对象；对象数一次性计入，避免后续动态增加 TotalUnits 导致进度回退
		if !crossType && !opts.DataOnly {
			t.p.TotalUnits += countExportObjects(sourceCli, job.srcDB, job.srcSchema, job.objects)
		}
		modeDesc := engineTextsFor(t.lang).modeSame
		if crossType {
			modeDesc = fmt.Sprintf(engineTextsFor(t.lang).modeCross, sourceCli.DBType(), targetCli.DBType())
		} else {
			modeDesc = engineTextsFor(t.lang).modeSame
		}
		t.log(engineTextsFor(t.lang).migStart, modeDesc, len(tables), resetDesc(opts.ResetMode, t.lang))

		backedUp := []string{}
		for _, table := range tables {
			if err := ctx.Err(); err != nil {
				sourceCli.Close()
				targetCli.Close()
				return nil, NewMsgErr(errCancelled)
			}
			t.p.CurrentTable = table
			t.emit(true)

			// 2. 重置处理（备份 + truncate/drop）
			ok, err := resetTable(targetCli, table, opts.ResetMode, opts.Backup, t)
			if err != nil {
				sourceCli.Close()
				targetCli.Close()
				return nil, err
			}
			if ok {
				backedUp = append(backedUp, table)
			}

			// 3. 结构迁移（目标表不存在时创建）
			if !opts.DataOnly {
				exist, err := targetCli.IsTableExist(table)
				if err != nil {
					sourceCli.Close()
					targetCli.Close()
					return nil, err
				}
				if !exist {
					ddl, err := buildCreateTableDDL(sourceCli, targetCli, table, crossType, opts.CompatCollation)
					if err != nil {
						sourceCli.Close()
						targetCli.Close()
						return nil, NewMsgErrf(errMigDDL, err, table)
					}
					start := time.Now()
					if _, err := targetCli.DirectExecute(ddl); err != nil {
						fireQueryHook(ctx, opts.TargetConn, ddl, start, -1)
						sourceCli.Close()
						targetCli.Close()
						return nil, NewMsgErrf(errMigCreateTable, err, table)
					}
					fireQueryHook(ctx, opts.TargetConn, ddl, start, 0)
					t.log(engineTextsFor(t.lang).migCreate, table)
				}
			}

			// 4. 数据迁移
			if opts.SchemaOnly {
				t.p.DoneUnits++
				continue
			}
			rows, err := migrateTableData(ctx, sourceCli, targetCli, table, opts, batchSize, t, job.srcDB)
			if err != nil {
				sourceCli.Close()
				targetCli.Close()
				return nil, NewMsgErrf(errMigData, err, table)
			}
			totalRows += rows
			t.p.DoneUnits++
			t.log(engineTextsFor(t.lang).migTableDone, table, rows)
		}

		// 5. 对象迁移（仅同类型且非 DataOnly：视图/函数/存储过程；触发器已随建表语句迁移）
		if !crossType && !opts.DataOnly {
			migrateDBObjects(ctx, sourceCli, targetCli, job.srcDB, job.srcSchema, job.objects, t, opts.TargetConn)
		}

		// 6. 成功后清理备份表
		for _, table := range backedUp {
			if err := dropBackupTable(targetCli, table); err != nil {
				t.log(engineTextsFor(t.lang).migCleanFail, err)
			}
		}

		sourceCli.Close()
		targetCli.Close()
		totalTables += len(tables)
	}

	t.finish()
	t.log(engineTextsFor(t.lang).migDone, totalTables, totalRows)
	return &MigrateResult{TotalTables: totalTables, TotalRows: totalRows}, nil
}

// suspendTargetChecks 数据写入阶段挂起目标库的外键/触发器校验（业界标准做法，mysqldump 导出文件
// 同样以 SET FOREIGN_KEY_CHECKS 开关包裹）：自引用外键（如 act_ru_execution 的 PROC_INST_ID_ →
// 本表 ID_）无法靠表级拓扑排序保证行序，父行可能落在后续批次，必须挂起校验后整体写入。
// 能力下沉 infrakit cydb（PinSingleConnection + SuspendConstraintChecks）；开关为会话级，
// 连接为本次迁移独占，任务结束随 Close 释放自动失效，无需显式恢复。
// PG 的 session_replication_role 需要超级用户权限，失败仅告警不阻断；Oracle 暂不支持（保持原校验行为）
func suspendTargetChecks(targetCli *cydb.DBCli, t *tracker) {
	targetCli.PinSingleConnection()
	if sql, err := targetCli.SuspendConstraintChecks(); err != nil {
		t.log(engineTextsFor(t.lang).migCheckWarn, sql, err)
	}
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
func migrateDBObjects(ctx context.Context, sourceCli, targetCli *cydb.DBCli, db, schema string, objects []string, t *tracker, connKey string) {
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
		// 对象数已在任务开始时计入 TotalUnits，此处不再重复增加
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
			execSQL := terminateSQL(ddl)
			start := time.Now()
			if _, err := targetCli.DirectExecute(execSQL); err != nil {
				fireQueryHook(ctx, connKey, execSQL, start, -1)
				t.log(engineTextsFor(t.lang).migObjExecFail, dirName, db, name, err)
				t.p.DoneUnits++
				continue
			}
			fireQueryHook(ctx, connKey, execSQL, start, 0)
			t.p.DoneUnits++
			t.log(engineTextsFor(t.lang).migObjDone, db, dirName, name)
		}
	}
}

// migrateTableData 流式迁移单表数据：ForEachQuery → 批量 BatchReplace（目标已有同主键行时覆盖，避免主键冲突）
func migrateTableData(ctx context.Context, sourceCli, targetCli *cydb.DBCli, table string, opts MigrateOptions, batchSize int, t *tracker, srcDB string) (int64, error) {
	srcType, srcSub := sourceCli.DBType(), sourceCli.DBSubType()

	cond := findCondition(opts.Conditions, srcDB, table)
	// 取数 SQL：条件统一为完整 SELECT（旧版 Where/Columns 归一化拼装），无条件时全表
	selectSQL := conditionQuery(srcType, srcSub, table, cond)
	if selectSQL == "" {
		selectSQL = fmt.Sprintf("SELECT * FROM %s", EscapeTable(srcType, srcSub, table))
	}

	var rows int64
	batch := make([]map[string]any, 0, batchSize)

	// 写入模式：REPLACE 覆盖写（MySQL/SQLite 原生支持；PG/Oracle 按主键转 upsert）。
	// 需要显式冲突列的方言（方言自描述）目标无主键时无法冲突覆盖，降级为普通 INSERT
	useReplace := true
	if targetCli.GetWriteCapability().ReplaceNeedsConflictColumns {
		if ti, err := targetCli.GetTableInfo(table); err != nil || len(ti.GetPrimaryKeys()) == 0 {
			useReplace = false
		}
	}

	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		// 审计钩子：批量写入未暴露最终 SQL 文本，回调语句为批写模板描述（归属 connKey 供审计聚合）
		mode := "INSERT"
		if useReplace {
			mode = "REPLACE"
		}
		stmt := fmt.Sprintf("%s INTO %s (batch %d rows)", mode, table, len(batch))
		start := time.Now()
		var err error
		if useReplace {
			_, err = targetCli.BatchReplaceContext(ctx, table, batch)
		} else {
			_, err = targetCli.BatchInsertContext(ctx, table, batch)
		}
		if err != nil {
			fireQueryHook(ctx, opts.TargetConn, stmt, start, -1)
			return NewMsgErrf(errMigBatchWrite, err)
		}
		fireQueryHook(ctx, opts.TargetConn, stmt, start, int64(len(batch)))
		batch = batch[:0]
		return nil
	}

	// DirectForEachQuery 跳过 preProcess：GoSQLX 无法解析 PG/Kingbase 双引号限定名（"schema"."table"），
	// selectSQL 为可执行完整 SQL，直接交由数据库解析执行
	err := sourceCli.DirectForEachQuery(table, selectSQL, func(rd cydb.RowData) error {
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
