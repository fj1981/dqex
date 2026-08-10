package engine

import (
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/dialect"
)

// ExportResult 导出结果
type ExportResult struct {
	OutputPath  string // 最终产物路径（zip 或目录）
	OutputDir   string // 导出明细目录
	TotalTables int
	TotalRows   int64
}

// RunExport 执行导出：多库多表 → {outputDir}/{taskName}_{时间}/库名.sql → 可选 zip 打包
//
// 每个数据库导出为单个 SQL 文件，文件内按以下顺序组织：
//  1. 建表语句（CREATE TABLE，含触发器——底层库方言的 GetCreateTableSql 已一并返回）
//  2. 视图（CREATE VIEW）
//  3. 函数（CREATE FUNCTION）
//  4. 存储过程（CREATE PROCEDURE）
//
// 表之间按外键依赖拓扑排序，确保导入时被引用表先创建。全部内容在单个文件内，
// 可直接体现依赖顺序；前置/收尾语句（如 SET FOREIGN_KEY_CHECKS）包裹整个文件。
func RunExport(ctx context.Context, opts ExportOptions, cb ProgressFunc) (*ExportResult, error) {
	if opts.Source == nil {
		return nil, fmt.Errorf("未提供源数据库连接")
	}
	t := newTracker(cb)

	outputDir := opts.OutputDir
	if outputDir == "" {
		// 默认：数据根目录下 exports/（service 层调用时已注入同一默认值）
		home, _ := os.UserHomeDir()
		outputDir = filepath.Join(home, ".dbimpex", "exports")
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建输出目录失败: %w", err)
	}

	taskName := sanitizeName(opts.TaskName)
	if taskName == "" {
		taskName = "export"
	}
	ts := time.Now().Format("20060102_150405")
	baseDir := filepath.Join(outputDir, fmt.Sprintf("%s_%s", taskName, ts))
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("创建导出目录失败: %w", err)
	}

	// 1. 确定要导出的库列表
	databases := opts.Databases
	if len(databases) == 0 {
		if opts.Source.DBName == "" {
			return nil, fmt.Errorf("未指定要导出的数据库（连接配置或 databases 参数至少提供一个）")
		}
		databases = []string{opts.Source.DBName}
	}

	// 2. 预扫描：收集各库的表清单
	type dbTables struct {
		db     string
		tables []string
	}
	var plan []dbTables
	for _, db := range databases {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("任务已取消")
		}
		cli, err := ConnectDB(*opts.Source, db)
		if err != nil {
			return nil, err
		}
		all, err := cli.GetTables(db, nil, nil)
		if err == nil {
			all = excludeViews(cli, db, opts.Source.Schema, all) // 视图走对象通道，不当作表导出
		}
		cli.Close()
		if err != nil {
			return nil, fmt.Errorf("获取库 %s 的表列表失败: %w", db, err)
		}
		tables := filterTables(all, opts.Tables, db)
		if len(tables) == 0 {
			t.log("库 %s 无选中的表，仅处理对象", db)
		}
		plan = append(plan, dbTables{db: db, tables: tables})
	}
	if len(plan) == 0 {
		return nil, fmt.Errorf("没有可导出的数据库")
	}

	totalTables := 0
	for _, p := range plan {
		totalTables += len(p.tables)
	}
	t.p.TotalUnits = totalTables
	t.log("开始导出: %d 个库, %d 张表 → %s", len(plan), totalTables, baseDir)

	// 3. 逐库导出（每库一个 sql 文件）
	var totalRows int64
	for _, p := range plan {
		cli, err := ConnectDB(*opts.Source, p.db)
		if err != nil {
			return nil, err
		}
		// 按外键依赖拓扑排序（被引用表先导出，导入时可顺序建表）；不支持或失败时保持原顺序
		p.tables = sortTablesByFK(cli, p.tables, t)
		// 约束控制语句（方言提供：MySQL 为 USE 库 + SET FOREIGN_KEY_CHECKS 开关），写入整个库文件保证可独立导入；
		// 必须传入库名：方言实现依赖 name 参数定位库，缺失会导致前置语句生成失败而被静默丢弃
		beginSQL := dialectDDL(cli, dialect.FuncNameGetBeginSql, p.db)
		endSQL := dialectDDL(cli, dialect.FuncNameGetEndSql, p.db)
		if beginSQL == "" {
			// 前置语句缺失时明示告警（如 MySQL 关闭外键检查失败，导入带外键依赖的库会失败）
			t.log("警告: 库 %s 未获取到前置约束语句，导入带外键依赖的库可能失败", p.db)
		}

		dbFile := filepath.Join(baseDir, sanitizeName(p.db)+sqlFileExt(opts.Gzip))
		// 一致性快照：启用后全部读取在同一事务内进行，跨表处于同一时间点
		exportCli, endSnapshot := beginSnapshot(cli, opts.SingleTransaction, t)
		rows, err := exportDatabase(ctx, exportCli, p.db, p.tables, dbFile, opts, t, beginSQL, endSQL)
		endSnapshot(err == nil)
		if err != nil {
			cli.Close()
			return nil, fmt.Errorf("导出库 %s 失败: %w", p.db, err)
		}
		totalRows += rows
		cli.Close()
	}

	// 4. 打包 zip（可选）
	result := &ExportResult{OutputDir: baseDir, TotalTables: totalTables, TotalRows: totalRows}
	if opts.Compress {
		zipPath := filepath.Join(outputDir, fmt.Sprintf("%s_%s.zip", taskName, ts))
		t.log("打包 zip: %s", zipPath)
		if err := zipDir(baseDir, zipPath); err != nil {
			return nil, fmt.Errorf("打包 zip 失败: %w", err)
		}
		// 打包成功后清理明细目录
		_ = os.RemoveAll(baseDir)
		result.OutputPath = zipPath
	} else {
		result.OutputPath = baseDir
	}

	t.p.OutputPath = result.OutputPath
	t.finish()
	t.log("导出完成: %d 表, %d 行 → %s", totalTables, totalRows, result.OutputPath)
	return result, nil
}

// sqlFileExt 导出 SQL 文件后缀（开启 gzip 时为 .sql.gz，导入侧透明解压）
func sqlFileExt(gzipEnabled bool) string {
	if gzipEnabled {
		return ".sql.gz"
	}
	return ".sql"
}

// beginSnapshot 启用且库类型支持时开启一致性快照事务（等同 mysqldump --single-transaction），
// 返回用于读取的 cli 与结束函数（按成功与否 commit/rollback）；不支持或开启失败时退化为原 cli 普通读取
func beginSnapshot(cli *cydb.DBCli, enabled bool, t *tracker) (*cydb.DBCli, func(ok bool)) {
	noop := func(bool) {}
	if !enabled {
		return cli, noop
	}
	dbType := strings.ToLower(cli.DBType())
	switch dbType {
	case "mysql", "mariadb":
		// 快照需 REPEATABLE READ 隔离级别才对整事务有效；会话级设置，对其后开启的事务生效
		if _, err := cli.DirectExecute("SET SESSION TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
			t.log("警告: 设置会话隔离级别失败: %v（退化为普通导出）", err)
			return cli, noop
		}
	case "postgres":
	default:
		t.log("库类型 %s 不支持一致性快照导出，按普通方式导出", cli.DBType())
		return cli, noop
	}
	tx, err := cli.BeginTx()
	if err != nil {
		t.log("警告: 开启快照事务失败: %v（退化为普通导出）", err)
		return cli, noop
	}
	if dbType == "postgres" {
		// Postgres 事务默认 READ COMMITTED，需在首次读取前提升为 REPEATABLE READ
		if _, err := tx.DirectExecute("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ"); err != nil {
			_ = tx.Rollback()
			t.log("警告: 设置事务隔离级别失败: %v（退化为普通导出）", err)
			return cli, noop
		}
	}
	t.log("一致性快照已启用：全部表在同一事务内读取")
	return tx, func(ok bool) {
		if ok {
			_ = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	}
}

// exportDatabase 将整个数据库（表 + 视图/函数/存储过程）导出为单个 SQL 文件。
// 文件内顺序：建表 DDL（含触发器）→ 数据（INSERT）→ 视图 → 函数 → 存储过程。
// SchemaOnly=true 时跳过数据段；DataOnly=true 时跳过建表段；二者都为 false 时两段都有。
// 同时生成同名 .desc 描述文件（JSON 格式），导入时直接读取获取元信息。
func exportDatabase(ctx context.Context, cli *cydb.DBCli, db string, tables []string, filePath string, opts ExportOptions, t *tracker, beginSQL, endSQL string) (int64, error) {
	f, err := os.Create(filePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	// gzip 开启时 SQL 内容经 gzip 流写出（文件名以 .sql.gz 结尾）
	var out io.Writer = f
	var gz *gzip.Writer
	if opts.Gzip {
		gz = gzip.NewWriter(f)
		out = gz
	}
	w := bufio.NewWriterSize(out, 256*1024)

	now := time.Now().Format("2006-01-02 15:04:05")
	fmt.Fprintf(w, "-- dbx export\n-- Database: %s\n-- Time: %s\n\n", db, now)

	// 前置语句：忽略约束检查等（如 MySQL 的 SET FOREIGN_KEY_CHECKS = 0）
	writeSqlBlock(w, beginSQL)

	var totalRows int64
	desc := ExportDesc{
		Database:   db,
		ExportTime: now,
		DBType:     cli.DBType(),
		Mode:       exportMode(opts),
	}

	// ============ 建表 DDL（含触发器）============
	if len(tables) > 0 && !opts.DataOnly {
		fmt.Fprintf(w, "-- ============ Tables ============\n\n")
		for _, table := range tables {
			if err := ctx.Err(); err != nil {
				return totalRows, fmt.Errorf("任务已取消")
			}
			t.p.CurrentTable = db + "." + table
			t.emit(true)

			fmt.Fprintf(w, "-- Table: %s\n", table)
			if err := writeTableDDL(cli, table, w); err != nil {
				return totalRows, fmt.Errorf("导出表 %s.%s 建表语句失败: %w", db, table, err)
			}
			// 表进度每表只计一次：SchemaOnly 无数据段在此计数，否则留给数据段
			if opts.SchemaOnly {
				t.p.DoneUnits++
			}
			t.log("%s.%s 结构导出完成", db, table)
			fmt.Fprintln(w)
		}
	}

	// ============ 数据（INSERT）============
	if len(tables) > 0 && !opts.SchemaOnly {
		fmt.Fprintf(w, "-- ============ Data ============\n\n")
		for _, table := range tables {
			if err := ctx.Err(); err != nil {
				return totalRows, fmt.Errorf("任务已取消")
			}
			t.p.CurrentTable = db + "." + table
			t.emit(true)

			cond := findCondition(opts.Conditions, db, table)
			// 检查表级数据导出模式
			dataMode := TableDataModeAll
			if cond != nil {
				dataMode = cond.DataMode
			}
			// skip 模式：跳过该表的数据导出
			if dataMode == TableDataModeSkip {
				t.log("%s.%s 跳过数据导出（dataMode=skip）", db, table)
				t.p.DoneUnits++
				desc.Tables = append(desc.Tables, ExportDescTable{Name: table, Rows: 0})
				continue
			}

			// 数据段注释：表名 + 条件 SQL（仅 condition 模式；多行 SQL 折叠为单行注释）
			if dataMode == TableDataModeCondition && cond != nil {
				if q := conditionQuery(cli.DBType(), cli.DBSubType(), table, cond); q != "" {
					fmt.Fprintf(w, "-- Data: %s\n-- Query: %s\n", table, strings.Join(strings.Fields(q), " "))
				} else {
					fmt.Fprintf(w, "-- Data: %s\n", table)
				}
			} else {
				fmt.Fprintf(w, "-- Data: %s\n", table)
			}
			rows, err := writeTableData(ctx, cli, db, table, w, opts, t)
			if err != nil {
				return totalRows, fmt.Errorf("导出表 %s.%s 数据失败: %w", db, table, err)
			}
			totalRows += rows
			t.p.DoneUnits++
			t.log("%s.%s 数据导出完成 (%d 行)", db, table, rows)
			fmt.Fprintln(w)

			// 记录表信息到 desc（条件统一归一化为完整 SELECT）
			td := ExportDescTable{Name: table, Rows: rows}
			if dataMode == TableDataModeCondition && cond != nil {
				td.Query = conditionQuery(cli.DBType(), cli.DBSubType(), table, cond)
			}
			desc.Tables = append(desc.Tables, td)
		}
	}

	// ============ 视图/函数/存储过程 ============
	if !opts.DataOnly {
		exportedObjs, err := exportObjectsToWriter(ctx, cli, db, opts.Source.Schema, w, opts.Objects, t)
		if err != nil {
			return totalRows, fmt.Errorf("导出库 %s 的数据库对象失败: %w", db, err)
		}
		if len(exportedObjs) > 0 {
			desc.Objects = exportedObjs
		}
	}

	// 收尾语句：恢复约束检查等（如 MySQL 的 SET FOREIGN_KEY_CHECKS = 1）
	writeSqlBlock(w, endSQL)

	if err := w.Flush(); err != nil {
		return totalRows, err
	}
	if gz != nil {
		if err := gz.Close(); err != nil {
			return totalRows, err
		}
	}

	// 写入 .desc 描述文件（.sql.gz 先剥 .gz 再去 .sql，保持 库.desc 同名约定）
	base := strings.TrimSuffix(filePath, ".gz")
	descPath := strings.TrimSuffix(base, filepath.Ext(base)) + ".desc"
	if err := writeDescFile(descPath, desc); err != nil {
		t.log("写入描述文件失败（已跳过）: %v", err)
	}

	return totalRows, nil
}

// exportMode 根据选项返回导出模式描述
func exportMode(opts ExportOptions) string {
	if opts.SchemaOnly {
		return "schemaOnly"
	}
	if opts.DataOnly {
		return "dataOnly"
	}
	return "schema+data"
}

// writeDescFile 将导出描述写入 JSON 文件
func writeDescFile(path string, desc ExportDesc) error {
	data, err := json.MarshalIndent(desc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// sortTablesByFK 复用底层库的表拓扑排序（GetSortedSql/SortTables，按外键依赖排序），
// 仅取排序后的表名顺序；方言不支持、存在循环依赖等失败场景回退原顺序
func sortTablesByFK(cli *cydb.DBCli, tables []string, t *tracker) []string {
	sorted, err := cli.GetSortedSql(dialect.FuncNameSortTables, cli.Database(), tables)
	if err != nil || len(sorted) == 0 {
		if err != nil {
			t.log("表依赖排序不可用（将按原顺序导出）: %v", err)
		}
		return tables
	}
	seen := make(map[string]bool, len(sorted))
	ret := make([]string, 0, len(tables))
	for _, sc := range sorted {
		if !seen[sc.Name] {
			seen[sc.Name] = true
			ret = append(ret, sc.Name)
		}
	}
	// 兜底：排序结果未覆盖的表（理论上不会发生）追加到末尾
	for _, tb := range tables {
		if !seen[tb] {
			ret = append(ret, tb)
		}
	}
	return ret
}

// dialectDDL 获取方言级 DDL 语句字符串（如导入前置/收尾语句），不支持或为空时返回 ""；
// name 透传给方言实现（部分方言的 BeginSql 需要库名定位）
func dialectDDL(cli *cydb.DBCli, funcName dialect.DDLSqlFuncName, name ...string) string {
	content, err := cli.GetDDLSql(funcName, name...)
	if err != nil || content == nil {
		return ""
	}
	return strings.TrimSpace(content.Content)
}

// terminateSQL 规范化语句结尾：各方言返回的结尾分号不一致（MySQL/PG 自带 ;，
// Oracle PL/SQL 块以 / 终止），先去除已有分号再统一处理，避免出现双分号或 /; 连写
func terminateSQL(sql string) string {
	sql = strings.TrimRight(strings.TrimSpace(sql), ";")
	if strings.HasSuffix(sql, "/") {
		// PL/SQL 块终止符 / 需独占一行，其后不再补分号
		return sql + "\n"
	}
	return sql + ";"
}

// writeSqlBlock 写入一段可能含多条语句的 SQL 文本，保证结尾规范
func writeSqlBlock(w *bufio.Writer, sql string) {
	if strings.TrimSpace(sql) == "" {
		return
	}
	fmt.Fprintf(w, "%s\n\n", terminateSQL(sql))
}

// writeTableDDL 将单表的 CREATE TABLE DDL（含触发器——底层库方言已一并返回）写入 bufio.Writer
func writeTableDDL(cli *cydb.DBCli, table string, w *bufio.Writer) error {
	content, err := cli.GetDDLSql(dialect.FuncNameGetCreateTableSql, table)
	if err != nil {
		return fmt.Errorf("生成建表语句失败: %w", err)
	}
	if content != nil && strings.TrimSpace(content.Content) != "" {
		fmt.Fprintf(w, "%s;\n\n", strings.TrimRight(strings.TrimSpace(content.Content), ";"))
	}
	return nil
}

// writeTableData 将单表的 INSERT 数据写入 bufio.Writer，返回导出行数
func writeTableData(ctx context.Context, cli *cydb.DBCli, db, table string, w *bufio.Writer, opts ExportOptions, t *tracker) (int64, error) {
	dbType, subType := cli.DBType(), cli.DBSubType()

	cond := findCondition(opts.Conditions, db, table)
	// 取数 SQL：条件统一为完整 SELECT（旧版 Where/Columns 归一化拼装），无条件时全表
	selectSQL := conditionQuery(dbType, subType, table, cond)
	if selectSQL == "" {
		selectSQL = fmt.Sprintf("SELECT * FROM %s", EscapeTable(dbType, subType, table))
	}

	var rows int64
	err := cli.ForEachQuery(table, selectSQL, func(rd cydb.RowData) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("任务已取消")
		}
		sql, err := rd.GetReplaceSql()
		if err != nil {
			return fmt.Errorf("生成 INSERT 语句失败: %w", err)
		}
		// 各方言返回的语句结尾分号不一致（MySQL/PG 自带 ;），统一规范化后写入
		if _, err := fmt.Fprintf(w, "%s\n", terminateSQL(sql)); err != nil {
			return err
		}
		rows++
		t.p.DoneRows++
		if rows%100 == 0 {
			t.emit(false)
		}
		return nil
	})
	if err != nil {
		return rows, err
	}
	return rows, nil
}

// exportObjectsToWriter 将视图/函数/存储过程的创建语句写入 bufio.Writer。
// objects 为对象白名单（格式 子目录/对象名）：nil=全部导出，空数组=不导出。
// 触发器不单独导出：底层库三方言的建表 DDL 已包含该表触发器。
// 单个对象导出失败仅记录日志不阻断（对象属于辅助能力，不应影响已完成的表数据导出）
// 返回已导出的对象列表（按类型分组，key 为 _views/_functions/_procedures）
func exportObjectsToWriter(ctx context.Context, cli *cydb.DBCli, db, schema string, w *bufio.Writer, objects []string, t *tracker) (map[string][]string, error) {
	exported := make(map[string][]string)
	if objects != nil && len(objects) == 0 {
		return exported, nil // 显式指定了空列表：不导出任何对象
	}
	allowed := make(map[string]bool, len(objects))
	for _, o := range objects {
		allowed[strings.TrimSpace(o)] = true
	}
	objs := listDBObjects(cli, db, schema)

	kindTitles := map[objectKind]string{
		objectView:      "Views",
		objectFunction:  "Functions",
		objectProcedure: "Stored Procedures",
	}
	kindSingular := map[objectKind]string{
		objectView:      "View",
		objectFunction:  "Function",
		objectProcedure: "Procedure",
	}

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

		fmt.Fprintf(w, "-- ============ %s ============\n\n", kindTitles[kind])

		for _, name := range names {
			if err := ctx.Err(); err != nil {
				return exported, fmt.Errorf("任务已取消")
			}
			t.p.CurrentTable = db + "." + dirName + "/" + name
			t.emit(true)

			ddl, err := objectDDL(cli, kind, name)
			if err != nil {
				t.log("导出%s %s.%s 失败（已跳过）: %v", dirName, db, name, err)
				t.p.DoneUnits++
				continue
			}
			fmt.Fprintf(w, "-- %s: %s\n", kindSingular[kind], name)
			fmt.Fprintf(w, "%s\n\n", terminateSQL(ddl))
			t.p.DoneUnits++
			t.log("%s.%s/%s 导出完成", db, dirName, name)
			exported[dirName] = append(exported[dirName], name)
		}
		fmt.Fprintln(w)
	}
	return exported, nil
}

// filterTables 按指定表名过滤：nil=全部，空数组=不过滤出任何表。
// 条目支持限定形式 "库.表"（仅在对应库生效）与裸表名（匹配任意库，便于 CLI 手输）
func filterTables(all []string, wanted []string, db string) []string {
	if wanted == nil {
		return all
	}
	bare := make(map[string]bool, len(wanted))
	qualified := make(map[string]bool, len(wanted))
	for _, w := range wanted {
		w = strings.TrimSpace(w)
		if d, t, ok := splitQualifiedName(w); ok {
			if strings.EqualFold(d, db) {
				qualified[t] = true
			}
		} else {
			bare[w] = true
		}
	}
	ret := make([]string, 0, len(all))
	for _, tb := range all {
		if bare[tb] || qualified[tb] {
			ret = append(ret, tb)
		}
	}
	return ret
}
