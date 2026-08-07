package engine

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
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
	switch ext {
	case ".sql":
		info.Type = "sql"
		dbName := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		info.Databases = []string{dbName}
		// 尝试读取同名 .desc 文件
		descPath := strings.TrimSuffix(path, filepath.Ext(path)) + ".desc"
		if desc, err := readDescFile(descPath); err == nil {
			info.Descs = map[string]*ExportDesc{dbName: desc}
		}
	case ".zip":
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
				// 根目录文件
				if strings.HasSuffix(strings.ToLower(name), ".sql") {
					db := strings.TrimSuffix(parts[0], filepath.Ext(parts[0]))
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
	default:
		return nil, fmt.Errorf("不支持的文件格式: %s（仅支持 .sql / .zip）", ext)
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

	switch ext {
	case ".sql":
		if opts.Target.DBName == "" {
			return nil, fmt.Errorf("连接配置未指定目标库，无法导入单文件")
		}
		dbName := strings.TrimSuffix(filepath.Base(opts.InputPath), ".sql")
		files = []importFile{{db: opts.Target.DBName, name: dbName, path: opts.InputPath}}
	case ".zip":
		var err error
		tempDir, err = os.MkdirTemp("", "dbimpex_import_*")
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
	default:
		return nil, fmt.Errorf("不支持的文件格式: %s（仅支持 .sql / .zip）", ext)
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

		stmts, err := importSQLFile(ctx, cli, f.path, t, blockUnits)
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
		if !strings.EqualFold(filepath.Ext(path), ".sql") {
			return nil
		}
		rel, _ := filepath.Rel(tempDir, path)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if len(parts) != 1 {
			// 忽略子目录中的文件
			return nil
		}
		dbName := strings.TrimSuffix(parts[0], filepath.Ext(parts[0]))
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
	f, err := os.Open(path)
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
func importSQLFile(ctx context.Context, cli *cydb.DBCli, path string, t *tracker, blockUnits bool) (int64, error) {
	f, err := os.Open(path)
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
