package engine

import (
	"archive/zip"
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

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/dialect"
)

// ImportResult 导入结果
type ImportResult struct {
	TotalDatabases int
	TotalStmts     int64 // 执行的 SQL 语句数
}

// ImportFileInfo 导入文件预览信息
type ImportFileInfo struct {
	Type      string                 `json:"type"` // sql / zip
	Size      int64                  `json:"size"`
	Databases []string               `json:"databases"`
	Descs     map[string]*ExportDesc `json:"descs,omitempty"` // 库名 → 导出描述（如有）
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
		return nil, fmt.Errorf("解压 gzip 失败: %w", err)
	}
	return &gzReadCloser{gz: gz, f: f}, nil
}

// InspectImportFile 预览导入文件信息（不解压执行）
// 如果存在同名 .desc 描述文件，会读取其中的元信息（表列表、对象、条件等）
func InspectImportFile(path string) (*ImportFileInfo, error) {
	st, err := os.Stat(path)
	if err != nil {
		// 不向外暴露服务器文件路径
		return nil, fmt.Errorf("导入文件不存在或无法读取")
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
	} else if ext == ".zip" {
		info.Type = "zip"
		r, err := zip.OpenReader(path)
		if err != nil {
			return nil, fmt.Errorf("打开 zip 失败: %w", err)
		}
		defer r.Close()
		dbSet := map[string]bool{}
		descs := map[string]*ExportDesc{}
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
		sort.Strings(info.Databases)
	} else {
		return nil, fmt.Errorf("不支持的文件格式: %s（仅支持 .sql / .sql.gz / .zip）", filepath.Ext(path))
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

// RunImport 执行导入：支持 .sql 单文件与 .zip 包（每个 库名.sql 即一个库的完整导出）
func RunImport(ctx context.Context, opts ImportOptions, cb ProgressFunc) (*ImportResult, error) {
	if opts.Target == nil {
		return nil, fmt.Errorf("未提供目标数据库连接")
	}
	if opts.InputPath == "" {
		return nil, fmt.Errorf("未指定导入文件")
	}
	t := newTracker(cb)

	ext := strings.ToLower(filepath.Ext(opts.InputPath))
	var files []importFile
	var tempDir string

	if dbName, ok := sqlBaseName(filepath.Base(opts.InputPath)); ok {
		if opts.Target.DBName == "" {
			return nil, fmt.Errorf("连接配置未指定目标库，无法导入单文件")
		}
		files = []importFile{{db: opts.Target.DBName, name: dbName, path: opts.InputPath}}
	} else if ext == ".zip" {
		var err error
		tempDir, err = os.MkdirTemp(opts.TempDir, "dbimpex_import_*")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tempDir)
		if err := unzip(opts.InputPath, tempDir); err != nil {
			return nil, fmt.Errorf("解压 zip 失败: %w", err)
		}
		files, err = planZipImport(tempDir, opts.Target)
		if err != nil {
			return nil, err
		}
	} else {
		return nil, fmt.Errorf("不支持的文件格式: %s（仅支持 .sql / .sql.gz / .zip）", filepath.Ext(opts.InputPath))
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("导入文件中没有可导入的 SQL")
	}

	// 进度单元：按方言预切分统计 SQL 块总数（块内含建表/数据/对象语句，粒度远细于库级）；
	// 预切分失败时回退为库级单元
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
	if blockUnits && totalBlocks > 0 {
		t.p.TotalUnits = totalBlocks
	} else {
		blockUnits = false
		t.p.TotalUnits = len(files)
	}
	t.log("开始导入: %d 个库", len(files))

	var totalStmts int64
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("任务已取消")
		}
		// 导入前确保目标库存在（不存在则自动创建）
		if err := EnsureDBExists(*opts.Target, f.db); err != nil {
			return nil, fmt.Errorf("确保目标库 %s 存在失败: %w", f.db, err)
		}
		cli, err := ConnectDB(*opts.Target, f.db)
		if err != nil {
			return nil, err
		}

		t.p.CurrentTable = f.db
		t.emit(true)

		stmts, err := importSQLFile(ctx, cli, f.path, t, blockUnits, opts.Target.CompatCollation)
		if err != nil {
			cli.Close()
			return nil, fmt.Errorf("导入库 %s 失败: %w", f.db, err)
		}
		totalStmts += stmts
		if !blockUnits {
			// 库级单元模式：整库导入完成才计一个单元
			t.p.DoneUnits++
		}
		t.log("库 %s 导入完成 (%d 条语句)", f.db, stmts)
		cli.Close()
	}

	t.finish()
	t.log("导入完成: %d 个库, %d 条语句", len(files), totalStmts)
	return &ImportResult{TotalDatabases: len(files), TotalStmts: totalStmts}, nil
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
		return 0, fmt.Errorf("不支持的方言: %s", conn.Type)
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

// importSQLFile 解析并执行单个 SQL 文件，返回语句数；blockUnits 为 true 时每执行一块推进一个进度单元
func importSQLFile(ctx context.Context, cli *cydb.DBCli, path string, t *tracker, blockUnits bool, compatCollation bool) (int64, error) {
	f, err := openSQLFile(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	var count int64
	err = cli.ReadSQLFile(f, func(stmt *dialect.SQLBlock) error {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("任务已取消")
		}
		content := strings.TrimSpace(stmt.Content)
		if content == "" {
			return nil
		}
		// collation 兼容：将 MySQL 8.0 特有排序规则替换为 5.7 兼容版本
		if compatCollation && strings.EqualFold(cli.DBType(), "mysql") {
			content = compatCollationSQL(content)
		}
		if _, err := cli.DirectExecute(content); err != nil {
			return fmt.Errorf("执行 SQL 失败(第 %d 块): %w", stmt.Index, err)
		}
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
	"utf8mb4_0900_ai_ci":  "utf8mb4_unicode_ci",
	"utf8mb4_0900_as_ci":  "utf8mb4_unicode_ci",
	"utf8mb4_0900_as_cs":  "utf8mb4_unicode_ci",
	"utf8mb4_0900_bin":    "utf8mb4_bin",
}
