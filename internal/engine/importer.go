package engine

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/fj1981/infrakit/pkg/cydb"
	"github.com/fj1981/infrakit/pkg/cydb/dialect"
)

// ImportResult 导入结果
type ImportResult struct {
	TotalDatabases int
	TotalStmts     int64    // 执行的 SQL 语句数
	RollbackPath   string   // 精确回滚 SQL 产物路径（仅 .json 数据导入且 Rollback=true 时非空；多库 zip 按库分文件，此为首个产物路径，其余经任务日志输出）
	SkippedTables  []string // 跳过的表（无主键，无法精确导入/回滚）
	Unrollback     []string // 执行了但无法生成精确回滚的语句（宿主侧告警）
}

// ImportFileInfo 导入文件预览信息
type ImportFileInfo struct {
	Type      string                 `json:"type"` // sql / json / zip
	Size      int64                  `json:"size"`
	Databases []string               `json:"databases"`
	Descs     map[string]*ExportDesc `json:"descs,omitempty"`   // 库名 → 导出描述（如有）
	Entries   int                    `json:"entries,omitempty"` // 条目数（json 数据包格式）
}

// importFile 待导入的单个 SQL 文件（每个文件即一个数据库）
type importFile struct {
	db   string // 目标库名
	name string // 文件名（去 .sql 后缀，用于进度显示）
	path string
}

// sqlBaseName 从 SQL 文件名提取库名（支持 .sql 与 .sql.gz，大小写不敏感），ok=false 表示非 SQL 文件
func sqlBaseName(name string) (string, bool) {
	lower := strings.ToLower(name)
	switch {
	case strings.HasSuffix(lower, ".sql.gz"):
		return name[:len(name)-len(".sql.gz")], true
	case strings.HasSuffix(lower, ".sql"):
		return name[:len(name)-len(".sql")], true
	}
	return "", false
}

// gzReadCloser gzip 流与底层文件的组合读取器，Close 时两者均关闭
type gzReadCloser struct {
	gz *gzip.Reader
	f  *os.File
}

func (r *gzReadCloser) Read(p []byte) (int, error) { return r.gz.Read(p) }
func (r *gzReadCloser) Close() error {
	err1 := r.gz.Close()
	err2 := r.f.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

// openSQLFile 打开 SQL 文件，.gz 结尾时透明 gzip 解压
func openSQLFile(path string) (io.ReadCloser, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if !strings.HasSuffix(strings.ToLower(path), ".gz") {
		return f, nil
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		f.Close()
		return nil, NewMsgErrf(errImpGzip, err)
	}
	return &gzReadCloser{gz: gz, f: f}, nil
}

// InspectImportFile 预览导入文件信息（不解压执行）
// 如果存在同名 .desc 描述文件，会读取其中的元信息（表列表、对象、条件等）
func InspectImportFile(path string) (*ImportFileInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		// 不向外暴露服务器文件路径
		return nil, NewMsgErr(errImpNoFile)
	}
	ext := strings.ToLower(filepath.Ext(path))
	info := &ImportFileInfo{Size: st.Size()}
	if dbName, ok := sqlBaseName(filepath.Base(path)); ok {
		info.Type = "sql"
		info.Databases = []string{dbName}
		// 尝试读取同名 .desc 文件（库.desc；.sql.gz 先剥 .gz）
		base := path
		if strings.HasSuffix(strings.ToLower(base), ".gz") {
			base = base[:len(base)-3]
		}
		descPath := strings.TrimSuffix(base, filepath.Ext(base)) + ".desc"
		if desc, err := readDescFile(descPath); err == nil {
			info.Descs = map[string]*ExportDesc{dbName: desc}
		}
	} else if ext == ".json" {
		// DataPackage 数据包预览：解析库名与条目数（解析失败视为非数据包格式）
		pkg, err := LoadDataPackageFile(path)
		if err != nil {
			return nil, NewMsgErrf(errImpFormat, err)
		}
		info.Type = "json"
		info.Databases = []string{pkg.DB}
		info.Entries = len(pkg.Entries)
	} else if ext == ".zip" {
		info.Type = "zip"
		r, err := zip.OpenReader(path)
		if err != nil {
			return nil, NewMsgErrf(errImpZip, err)
		}
		defer r.Close()
		dbSet := map[string]bool{}
		descs := map[string]*ExportDesc{}
		pkgEntries := 0
		for _, f := range r.File {
			if f.FileInfo().IsDir() {
				continue
			}
			name := filepath.ToSlash(f.Name)
			parts := strings.Split(name, "/")
			if len(parts) == 1 {
				// 根目录文件（.sql / .sql.gz 每个即一个库的完整导出）
				if db, ok := sqlBaseName(parts[0]); ok {
					if db != "" {
						dbSet[db] = true
					}
				} else if strings.HasSuffix(strings.ToLower(name), ".json") {
					// 根目录 .json 数据包（与 RunImport 的 planZipJSONPackages 对齐）
					if pkg, err := loadDataPackageFromZip(f); err == nil {
						if pkg.DB != "" {
							dbSet[pkg.DB] = true
						}
						pkgEntries += len(pkg.Entries)
					}
				} else if strings.HasSuffix(strings.ToLower(name), ".desc") {
					// 读取 zip 内的 .desc 文件
					db := strings.TrimSuffix(parts[0], ".desc")
					if db != "" {
						if desc, err := readDescFromZip(f); err == nil {
							descs[db] = desc
						}
					}
				}
			}
		}
		for db := range dbSet {
			info.Databases = append(info.Databases, db)
		}
		if len(descs) > 0 {
			info.Descs = descs
		}
		if pkgEntries > 0 {
			info.Entries = pkgEntries
		}
		sort.Strings(info.Databases)
	} else {
		return nil, NewMsgErr(errImpFormat, filepath.Ext(path))
	}
	return info, nil
}

// readDescFile 读取 .desc 描述文件
func readDescFile(path string) (*ExportDesc, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var desc ExportDesc
	if err := json.Unmarshal(data, &desc); err != nil {
		return nil, err
	}
	return &desc, nil
}

// readDescFromZip 从 zip 文件条目读取 .desc 内容
func readDescFromZip(f *zip.File) (*ExportDesc, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var desc ExportDesc
	if err := json.NewDecoder(rc).Decode(&desc); err != nil {
		return nil, err
	}
	return &desc, nil
}

// loadDataPackageFromZip 从 zip 文件条目解析 DataPackage 数据包
// （顶层无 "datas" 字段视为非数据包格式，与 LoadDataPackageFile 判定一致）
func loadDataPackageFromZip(f *zip.File) (*DataPackage, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	probe := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	if _, ok := probe["datas"]; !ok {
		return nil, NewMsgErr(errImpFormat, "not a DataPackage json")
	}
	return LoadDataPackage(data)
}

// RunImport 执行导入：支持 .sql 单文件、.json 数据包（DataPackage，支持精确回滚）
// 与 .zip 包（每个 库名.sql / 库名.json 即一个库的导出；含 <Type>/ 目录时回调贡献者）
func RunImport(ctx context.Context, opts ImportOptions, cb ProgressFunc) (*ImportResult, error) {
	if opts.Target == nil {
		return nil, NewMsgErr(errImpNoTgt)
	}
	if opts.InputPath == "" {
		return nil, NewMsgErr(errImpNoInput)
	}
	t := newTracker(cb, opts.Lang)

	ext := strings.ToLower(filepath.Ext(opts.InputPath))
	var files []importFile
	var pkgs []*DataPackage // .json 数据包（zip 内或单文件输入）
	var tempDir string

	if dbName, ok := sqlBaseName(filepath.Base(opts.InputPath)); ok {
		if opts.Target.DBName == "" {
			return nil, NewMsgErr(errImpNoTgtDB)
		}
		files = []importFile{{db: opts.Target.DBName, name: dbName, path: opts.InputPath}}
	} else if ext == ".json" {
		pkg, err := LoadDataPackageFile(opts.InputPath)
		if err != nil {
			return nil, NewMsgErrf(errImpFormat, err)
		}
		nctx, npkg, perr := applyDataPreparer(ctx, opts, pkg)
		if perr != nil {
			return nil, perr
		}
		ctx, pkg = nctx, npkg
		pkgs = append(pkgs, pkg)
	} else if ext == ".zip" {
		var err error
		tempDir, err = os.MkdirTemp(opts.TempDir, "dqex_import_*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tempDir)
		if err := unzip(opts.InputPath, tempDir); err != nil {
			return nil, NewMsgErrf(errImpZip, err)
		}
		files, err = planZipImport(tempDir, opts.Target)
		if err != nil {
			return nil, err
		}
		ctx, pkgs, err = planZipJSONPackages(ctx, tempDir, opts, t)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, NewMsgErr(errImpFormat, filepath.Ext(opts.InputPath))
	}

	if len(files) == 0 && len(pkgs) == 0 {
		return nil, NewMsgErr(errImpNoSQL)
	}

	// 进度单元：SQL 文件按方言预切分统计块数（块内含建表/数据/对象语句，粒度远细于库级）；
	// 数据包按条目数计；预切分失败时回退为库级单元
	blockUnits := true
	totalBlocks := 0
	for _, f := range files {
		n, err := countSQLBlocks(*opts.Target, f.path)
		if err != nil {
			blockUnits = false
			break
		}
		totalBlocks += n
	}
	if blockUnits {
		totalBlocks += jsonPackageUnits(pkgs)
	}
	if blockUnits && totalBlocks > 0 {
		t.p.TotalUnits = totalBlocks
	} else {
		blockUnits = false
		t.p.TotalUnits = len(files) + len(pkgs)
	}
	t.log(engineTextsFor(t.lang).impStart, len(files)+len(pkgs))

	var totalStmts int64
	var skippedTables []string
	var unrollback []string
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, NewMsgErr(errCancelled)
		}
		// 导入前确保目标库存在（不存在则自动创建）
		if err := EnsureDBExists(*opts.Target, f.db); err != nil {
			return nil, NewMsgErrf(errImpEnsureDB, err, f.db)
		}
		cli, err := ConnectDB(*opts.Target, f.db)
		if err != nil {
			return nil, err
		}
		// 导入文件内含会话级约束开关（如 SET FOREIGN_KEY_CHECKS=0/1 包裹整个文件），
		// 钉单连接保证开关与后续语句落在同一连接（连接池下 Exec 可能取到不同连接导致开关随机失效）
		cli.PinSingleConnection()

		t.p.CurrentTable = f.db
		t.emit(true)

		stmts, err := importSQLFile(ctx, cli, f.path, t, blockUnits, opts.Target.CompatCollation, opts.TargetConn)
		if err != nil {
			cli.Close()
			return nil, NewMsgErrf(errImpDB, err, f.db)
		}
		totalStmts += stmts
		if !blockUnits {
			// 库级单元模式：整库导入完成才计一个单元
			t.p.DoneUnits++
		}
		t.log(engineTextsFor(t.lang).impDBDone, f.db, stmts)
		cli.Close()
	}

	// 数据包导入：单事务应用 + 行级回滚收集（回滚 SQL 按库分组；多库导入分文件写产物）
	type rollGroup struct {
		db   string
		sqls []string
	}
	var rollGroups []rollGroup
	addRollback := func(db string, sqls []string) {
		for i := range rollGroups {
			if rollGroups[i].db == db {
				rollGroups[i].sqls = append(rollGroups[i].sqls, sqls...)
				return
			}
		}
		rollGroups = append(rollGroups, rollGroup{db: db, sqls: append([]string(nil), sqls...)})
	}
	// writeRollbacks 尽力写出回滚产物：单库为 <名称>.rollback.sql（契约命名）；多库按库
	// 分文件 <名称>.<db>.rollback.sql（回滚语句无库上下文，回放须连接对应库）。
	// 单个文件写出失败打告警（不中断其余产物）；返回写出的路径清单
	writeRollbacks := func() []string {
		var paths []string
		write := func(dbTag string, sqls []string) {
			p, werr := writeRollbackArtifact(opts.InputPath, dbTag, sqls)
			if werr != nil {
				t.log("%s", NewMsgErrf(errImpRollback, werr).Error())
				return
			}
			paths = append(paths, p)
		}
		if len(rollGroups) == 1 {
			write("", rollGroups[0].sqls)
			return paths
		}
		for _, g := range rollGroups {
			write(g.db, g.sqls)
		}
		return paths
	}
	for _, pkg := range pkgs {
		if err := ctx.Err(); err != nil {
			return nil, NewMsgErr(errCancelled)
		}
		db := pkg.DB
		if opts.Target.DBName != "" {
			db = opts.Target.DBName
		}
		if db == "" {
			return nil, NewMsgErr(errImpNoTgtDB)
		}
		if err := EnsureDBExists(*opts.Target, db); err != nil {
			return nil, NewMsgErrf(errImpEnsureDB, err, db)
		}
		cli, err := ConnectDB(*opts.Target, db)
		if err != nil {
			return nil, err
		}
		dbType := cli.DBType()
		t.p.CurrentTable = db
		t.emit(true)
		res, err := ApplyDataPackage(ctx, cli, pkg, opts.TargetConn)
		cli.Close()
		if res != nil {
			// 失败时仍保留已执行部分的回滚/告警（ApplyDataPackage 失败返回非 nil res）
			addRollback(db, res.RollbackSQL)
			skippedTables = append(skippedTables, res.SkippedTables...)
			unrollback = append(unrollback, res.Unrollback...)
			totalStmts += res.Stmts
		}
		if err != nil {
			// 失败兜底：MySQL 系与 Oracle 的 DDL 隐式提交，事务回滚不撤销已建表；尽力写出
			// 已执行部分的回滚产物供人工补偿。PG 系事务原子回滚（含 DDL），无残留无需产物。
			if strings.EqualFold(dbType, "mysql") || strings.EqualFold(dbType, "oracle") {
				for _, p := range writeRollbacks() {
					t.log(engineTextsFor(t.lang).impRollbackPartial, p)
				}
			}
			return nil, NewMsgErrf(errImpDB, err, db)
		}
		t.p.DoneUnits += len(pkg.Entries)
		t.p.DoneRows = totalStmts
		t.log(engineTextsFor(t.lang).impPkgDone, db, len(pkg.Entries), len(res.SkippedTables))
		if len(res.Unrollback) > 0 {
			t.log(engineTextsFor(t.lang).impPkgUnrollback, db, len(res.Unrollback))
		}
		t.emit(false)
	}

	// 业务对象贡献者回读（仅 zip 布局：包内 <Type>/ 目录存在时回调宿主 Import）
	if err := importContributors(ctx, opts, tempDir, t); err != nil {
		return nil, err
	}

	// 精确回滚产物：仅 .json 数据包导入可生成（.sql 盲执行无行级语义）
	result := &ImportResult{TotalDatabases: len(files) + len(pkgs), TotalStmts: totalStmts, SkippedTables: skippedTables, Unrollback: unrollback}
	if opts.Rollback && len(rollGroups) > 0 {
		paths := writeRollbacks()
		if len(paths) == 0 {
			// 回滚产物是导入的安全网，一份都写不出时硬失败，不允许静默丢失回滚能力
			return nil, NewMsgErr(errImpRollback)
		}
		result.RollbackPath = paths[0]
		for _, p := range paths[1:] {
			t.log(engineTextsFor(t.lang).impRollbackMulti, p)
		}
	}

	t.finish()
	t.log(engineTextsFor(t.lang).impDone, len(files)+len(pkgs), totalStmts)
	return result, nil
}

// applyDataPreparer 回调宿主数据前置处理器（按包库名注册；未注册键则原样返回）。
// 返回回调派生的新 ctx（含宿主注入的值/取消能力，回调返回 nil ctx 时回退原 ctx），
// 供后续数据包应用等流程延续使用
func applyDataPreparer(ctx context.Context, opts ImportOptions, pkg *DataPackage) (context.Context, *DataPackage, error) {
	preparer, ok := opts.DataPreparers[pkg.DB]
	if !ok || preparer == nil {
		return ctx, pkg, nil
	}
	nctx, npkg, err := preparer(ctx, DataPrepareRequest{Key: opts.TargetConn, DB: pkg.DB, Package: pkg})
	if err != nil {
		return ctx, pkg, err
	}
	if npkg != nil {
		pkg = npkg
	}
	if nctx == nil {
		nctx = ctx
	}
	return nctx, pkg, nctx.Err()
}

// planZipJSONPackages 收集 zip 包内根目录的 <库名>.json 数据包（应用前置处理器后返回，
// 多包时前置处理器派生的 ctx 链式传播）。缺 "datas" 字段的 .json 视为非数据包格式静默
// 跳过；解析失败（损坏/IO 错误）的数据包打告警后跳过（避免导入"成功"但数据缺失无感知）。
func planZipJSONPackages(ctx context.Context, tempDir string, opts ImportOptions, t *tracker) (context.Context, []*DataPackage, error) {
	var pkgs []*DataPackage
	err := filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(tempDir, path)
		if len(strings.Split(filepath.ToSlash(rel), "/")) != 1 || !strings.HasSuffix(strings.ToLower(path), ".json") {
			return nil
		}
		pkg, err := LoadDataPackageFile(path)
		if err != nil {
			if me := AsMsgErr(err); me != nil && me.Key == errImpFormat {
				return nil // 非 DataPackage 格式的 .json（如普通清单），静默跳过
			}
			t.log(engineTextsFor(t.lang).impPkgSkip, rel, err)
			return nil
		}
		nctx, npkg, perr := applyDataPreparer(ctx, opts, pkg)
		if perr != nil {
			return perr
		}
		ctx, pkg = nctx, npkg
		pkgs = append(pkgs, pkg)
		return nil
	})
	return ctx, pkgs, err
}

// jsonPackageUnits 统计数据包进度单元数（条目数）
func jsonPackageUnits(pkgs []*DataPackage) int {
	n := 0
	for _, p := range pkgs {
		n += len(p.Entries)
	}
	return n
}

// LoadDataPackageFile 从 .json 文件加载数据包（顶层无 "datas"/"db" 字段时视为非数据包格式）
func LoadDataPackageFile(path string) (*DataPackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	probe := map[string]json.RawMessage{}
	if err := json.Unmarshal(data, &probe); err != nil {
		return nil, err
	}
	if _, ok := probe["datas"]; !ok {
		return nil, NewMsgErr(errImpFormat, "not a DataPackage json")
	}
	return LoadDataPackage(data)
}

// writeRollbackArtifact 将回滚 SQL 写入输入文件同目录：dbTag 为空时命名 <名称>.rollback.sql
// （单库契约命名），非空时命名 <名称>.<db>.rollback.sql（多库导入按库分文件，回放须连接
// 对应库）。产物含旧行全量明文数据，权限收紧 0600。
func writeRollbackArtifact(inputPath, dbTag string, rollbackSQLs []string) (string, error) {
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	name := base + ".rollback.sql"
	if dbTag != "" {
		name = base + "." + sanitizeName(dbTag) + ".rollback.sql"
	}
	rollPath := filepath.Join(filepath.Dir(inputPath), name)
	f, err := os.OpenFile(rollPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", err
	}
	w := bufio.NewWriterSize(f, 128*1024)
	dbLine := ""
	if dbTag != "" {
		dbLine = "\n-- Database: " + dbTag
	}
	fmt.Fprintf(w, "-- dqex rollback\n-- Source: %s%s\n-- Time: %s\n-- 回滚语义：基于导入时点旧值快照，仅适用于紧随导入的撤销（见 docs/library.md）\n\n",
		filepath.Base(inputPath), dbLine, time.Now().Format("2006-01-02 15:04:05"))
	for _, sql := range rollbackSQLs {
		fmt.Fprintln(w, sql)
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return "", err
	}
	return rollPath, f.Close()
}

// planZipImport 规划 zip 导入任务列表：每个根目录 库名.sql 文件即一个库
//   - 目标连接已指定库 → 全部导入该库
//   - 目标连接未指定库 → 按文件名分发到对应库
//   - Oracle 无多库概念 → 全部导入连接库
func planZipImport(tempDir string, target *DBConnInfo) ([]importFile, error) {
	var files []importFile
	singleDB := target.DBName != "" || strings.EqualFold(target.Type, "oracle")

	err := filepath.Walk(tempDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(tempDir, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 1 {
			// 忽略子目录中的文件
			return nil
		}
		// .sql / .sql.gz 均为一个库的完整导出
		dbName, ok := sqlBaseName(parts[0])
		if !ok {
			return nil
		}
		f := importFile{name: dbName, path: path}
		if singleDB {
			f.db = target.DBName
		} else {
			f.db = dbName
		}
		files = append(files, f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].name < files[j].name
	})
	return files, nil
}

// importObjectNameRe 从 SQL 块开头提取目标对象标识符（表/视图/函数/存储过程），供进度展示
var importObjectNameRe = regexp.MustCompile(`(?i)^\s*(?:CREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?|REPLACE\s+INTO|INSERT\s+INTO|CREATE\s+(?:OR\s+REPLACE\s+)?(?:VIEW|FUNCTION|PROCEDURE))\s+([^\s(;]+)`)

// objectNameQuoteStripper 剥离标识符包裹符，兼容 `db`.`表` 限定名与各方言引号
var objectNameQuoteStripper = strings.NewReplacer("`", "", "\"", "", "'", "")

// blockObjectName 提取 SQL 块的目标对象名，无法识别时返回 ""
func blockObjectName(content string) string {
	m := importObjectNameRe.FindStringSubmatch(content)
	if m == nil {
		return ""
	}
	return objectNameQuoteStripper.Replace(m[1])
}

// countSQLBlocks 按目标方言预切分 SQL 文件统计非空块数（纯文本解析，不建数据库连接）
func countSQLBlocks(conn DBConnInfo, path string) (int, error) {
	sqlFunc, ok := dialect.GetSqlDialect(conn.Type, conn.SubType)
	if !ok {
		return 0, NewMsgErr(errImpDialect, conn.Type)
	}
	f, err := openSQLFile(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	n := 0
	err = sqlFunc.ReadSQLFile(f, func(stmt *dialect.SQLBlock) error {
		if strings.TrimSpace(stmt.Content) != "" {
			n++
		}
		return nil
	})
	return n, err
}

// importSQLFile 解析并执行单个 SQL 文件，返回语句数；blockUnits 为 true 时每执行一块推进一个进度单元；
// connKey 用于审计钩子归属
func importSQLFile(ctx context.Context, cli *cydb.DBCli, path string, t *tracker, blockUnits bool, compatCollation bool, connKey string) (int64, error) {
	f, err := openSQLFile(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var count int64
	err = cli.ReadSQLFile(f, func(stmt *dialect.SQLBlock) error {
		if err := ctx.Err(); err != nil {
			return NewMsgErr(errCancelled)
		}
		content := strings.TrimSpace(stmt.Content)
		if content == "" {
			return nil
		}
		// collation 兼容：将 MySQL 8.0 特有排序规则替换为 5.7 兼容版本
		if compatCollation && strings.EqualFold(cli.DBType(), "mysql") {
			content = compatCollationSQL(content)
		}
		start := time.Now()
		if _, err := cli.DirectExecute(content); err != nil {
			fireQueryHook(ctx, connKey, content, start, -1)
			return NewMsgErrf(errImpExec, err, stmt.Index)
		}
		fireQueryHook(ctx, connKey, content, start, 0)
		count++
		t.p.DoneRows = count
		if blockUnits {
			t.p.DoneUnits++
		}
		// 进度展示真实的对象名（表/视图/函数/存储过程），而非仅库名
		if name := blockObjectName(content); name != "" {
			t.p.CurrentTable = name
		}
		if blockUnits || count%20 == 0 {
			t.emit(false)
		}
		return nil
	})
	return count, err
}

// compatCollationSQL 将 SQL 语句中的 MySQL 8.0 特有排序规则替换为 5.7 兼容版本。
// 用于导入 SQL 文件场景（静态文本，不经过 DDL 方言处理）。
func compatCollationSQL(sql string) string {
	// 使用正则替换 CREATE TABLE 语句中的 COLLATE=utf8mb4_0900_* 模式
	// 处理表级: DEFAULT COLLATE = utf8mb4_0900_ai_ci
	// 处理列级: COLLATE utf8mb4_0900_ai_ci
	re := regexp.MustCompile(`(?i)utf8mb4_0900_[a-z_]+`)
	return re.ReplaceAllStringFunc(sql, func(match string) string {
		if repl, ok := compatCollationReplace[strings.ToLower(match)]; ok {
			return repl
		}
		return "utf8mb4_unicode_ci"
	})
}

// compatCollationReplace MySQL 8.0 特有排序规则 → MySQL 5.7 兼容替代（文本替换用）
var compatCollationReplace = map[string]string{
	"utf8mb4_0900_ai_ci": "utf8mb4_unicode_ci",
	"utf8mb4_0900_as_ci": "utf8mb4_unicode_ci",
	"utf8mb4_0900_as_cs": "utf8mb4_unicode_ci",
	"utf8mb4_0900_bin":   "utf8mb4_bin",
}
