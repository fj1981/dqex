package service

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gopkg.in/yaml.v3"
)

// ---- CLI 入口 ----

// IsCLISubcommand 判断参数是否为 CLI 子命令
func IsCLISubcommand(s string) bool {
	switch s {
	case "export", "import", "migrate", "task", "help", "-h", "--help":
		return true
	}
	return false
}

// RunCLI 执行 CLI 子命令
func RunCLI(args []string) {
	if len(args) == 0 {
		printUsage()
		return
	}
	var err error
	switch args[0] {
	case "export":
		err = cliExport(args[1:])
	case "import":
		err = cliImport(args[1:])
	case "migrate":
		err = cliMigrate(args[1:])
	case "task":
		err = cliTask(args[1:])
	default:
		printUsage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %s\n", cliErrMsg(err))
		os.Exit(1)
	}
}

// cliErrMsg 提取可读错误信息：*cygin.Error 输出中文消息 + 详情，其他错误直接输出
func cliErrMsg(err error) string {
	if e, ok := err.(*cygin.Error); ok {
		msg := e.Msg("zh")
		if len(e.Details) > 0 {
			return fmt.Sprintf("%s (%s)", msg, strings.Join(e.Details, "; "))
		}
		return msg
	}
	return err.Error()
}

func printUsage() {
	fmt.Println(`dbimpex - 数据库导入导出迁移工具

用法:
  dbimpex                      启动 Web 服务（默认端口 8181）
  dbimpex --port 9090          指定端口启动 Web 服务
  dbimpex export [flags]       导出
  dbimpex import [flags]       导入（.sql 或 .zip）
  dbimpex migrate [flags]      迁移（支持跨类型）
  dbimpex task <list|run|save|delete>   任务配置管理

通用 flags:
  --config <file.yaml>         配置文件（参数优先级: 命令行 > --task > --config > 默认值）
  --task <taskID>              使用已保存的任务配置

导出 flags:
  --source-type mysql|postgresql|oracle   --source-host --source-port
  --source-un --source-pw --source-db --source-subtype
  --output <dir|file.zip>      输出目录或 zip 文件路径（默认为二进制同级目录下的 .dbimpex-exports）
  --name <名称>                 任务名（用于生成文件名，默认 export）
  --databases a,b              指定库（默认连接配置的库）
  --tables a,b                 指定表（默认全部）
  --objects a,b                指定对象，格式 _views/名称（默认全部）
  --table-cond "表名:完整SELECT" 表级过滤条件（可重复，兼容旧格式 表名:WHERE片段）
  --schema-only                仅导出结构
  --data-only                  仅导出数据
  --compress=false             不打包 zip，保留目录结构

导入 flags:
  --target-type --target-host --target-port --target-un --target-pw --target-db --target-subtype
  --input <file.sql|file.zip>  导入文件
  --reset truncate|drop        重置模式（默认不重置）
  --backup=false               重置前不创建备份表（默认备份）

迁移 flags:
  --source-* / --target-*      源与目标连接
  --tables --objects --table-cond --schema-only --data-only --reset --backup --batch-size`)
}

// ---- 连接 flags ----

type connFlags struct {
	typ, host, un, pw, db, subtype string
	port                           int
}

func registerConnFlags(fs *flag.FlagSet, prefix string, cf *connFlags) {
	fs.StringVar(&cf.typ, prefix+"-type", "", prefix+" 数据库类型(mysql/postgresql/oracle)")
	fs.StringVar(&cf.host, prefix+"-host", "", prefix+" 主机")
	fs.IntVar(&cf.port, prefix+"-port", 0, prefix+" 端口")
	fs.StringVar(&cf.un, prefix+"-un", "", prefix+" 用户名")
	fs.StringVar(&cf.pw, prefix+"-pw", "", prefix+" 密码")
	fs.StringVar(&cf.db, prefix+"-db", "", prefix+" 数据库名")
	fs.StringVar(&cf.subtype, prefix+"-subtype", "", prefix+" 子类型/版本")
}

func (cf *connFlags) toConn() *DBConnInfo {
	if cf.typ == "" {
		return nil
	}
	return &DBConnInfo{DBConnection: def.DBConnection{
		Type:    cf.typ,
		SubType: cf.subtype,
		Host:    cf.host,
		Port:    cf.port,
		Un:      cf.un,
		Pw:      cf.pw,
		DBName:  cf.db,
	}}
}

// ---- 可重复 flag ----

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// ---- 配置文件 ----

type configFile struct {
	Export  *ExportOptions  `yaml:"export"`
	Import  *ImportOptions  `yaml:"import"`
	Migrate *MigrateOptions `yaml:"migrate"`
}

func loadConfigFile(path string) (*configFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("读取配置文件失败: %s", path))
	}
	cfg := &configFile{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, cygin.WrapError(err, cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("解析配置文件失败: %s", path))
	}
	return cfg, nil
}

// parseTableConds 解析 --table-cond "表名:完整SELECT" 参数（兼容旧格式 表名:WHERE 片段）
func parseTableConds(items []string) []TableCondition {
	ret := make([]TableCondition, 0, len(items))
	for _, item := range items {
		idx := strings.Index(item, ":")
		if idx <= 0 {
			continue
		}
		cond := TableCondition{TableName: strings.TrimSpace(item[:idx])}
		sql := strings.TrimSpace(item[idx+1:])
		if strings.HasPrefix(strings.ToLower(sql), "select ") {
			cond.Query = sql
		} else {
			cond.Where = sql // 旧格式：WHERE 片段
		}
		ret = append(ret, cond)
	}
	return ret
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	ret := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			ret = append(ret, p)
		}
	}
	return ret
}

// ---- CLI 进度输出 ----

func cliProgress() (ProgressFunc, *string) {
	lastMsg := ""
	var outputPath string
	return func(p ProgressInfo) {
		if p.OutputPath != "" {
			outputPath = p.OutputPath
		}
		if p.Message != "" && p.Message != lastMsg {
			lastMsg = p.Message
			fmt.Printf("  · %s\n", p.Message)
		}
		fmt.Printf("\r  进度 %.1f%% | %d/%d 项 | %d 行 | %s    ",
			p.Percent, p.DoneUnits, p.TotalUnits, p.DoneRows, p.CurrentTable)
		if p.State == "done" || p.State == "error" || p.State == "cancelled" {
			fmt.Println()
		}
	}, &outputPath
}

// ---- export ----

func cliExport(args []string) error {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	configPath := fs.String("config", "", "配置文件(yaml)")
	taskID := fs.String("task", "", "已保存任务配置 ID")
	var src connFlags
	registerConnFlags(fs, "source", &src)
	output := fs.String("output", "", "输出目录或 .zip 文件路径")
	name := fs.String("name", "", "任务名（用于生成文件名）")
	databases := fs.String("databases", "", "指定库，逗号分隔")
	tables := fs.String("tables", "", "指定表，逗号分隔")
	objects := fs.String("objects", "", "指定对象，逗号分隔，格式 _views/名称")
	var conds multiFlag
	fs.Var(&conds, "table-cond", "表过滤条件，格式 表名:完整SELECT（可重复，兼容旧格式 表名:WHERE片段）")
	schemaOnly := fs.Bool("schema-only", false, "仅导出结构")
	dataOnly := fs.Bool("data-only", false, "仅导出数据")
	compress := fs.Bool("compress", true, "打包为 zip")
	batchSize := fs.Int("batch-size", 500, "批量大小")
	_ = fs.Parse(args)

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	opts := ExportOptions{Compress: true}
	// 优先级: 配置文件 → 任务配置 → 命令行参数
	if *configPath != "" {
		cfg, err := loadConfigFile(*configPath)
		if err != nil {
			return err
		}
		if cfg.Export != nil {
			opts = *cfg.Export
		}
	}
	if *taskID != "" {
		svc, err := NewService("")
		if err != nil {
			return err
		}
		task, err := svc.GetTask(*taskID)
		if err != nil {
			return err
		}
		if task.ExportOpts == nil {
			return cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("任务配置 %s 不是导出任务", *taskID))
		}
		opts = *task.ExportOpts
	}
	if conn := src.toConn(); conn != nil {
		opts.Source = conn
	}
	if setFlags["name"] {
		opts.TaskName = *name
	}
	if setFlags["databases"] {
		opts.Databases = splitCSV(*databases)
	}
	if setFlags["tables"] {
		opts.Tables = splitCSV(*tables)
	}
	if setFlags["objects"] {
		opts.Objects = splitCSV(*objects)
	}
	if setFlags["table-cond"] {
		opts.Conditions = append(opts.Conditions, parseTableConds(conds)...)
	}
	if setFlags["schema-only"] {
		opts.SchemaOnly = *schemaOnly
	}
	if setFlags["data-only"] {
		opts.DataOnly = *dataOnly
	}
	if setFlags["compress"] {
		opts.Compress = *compress
	}
	if setFlags["batch-size"] {
		opts.BatchSize = *batchSize
	}

	// --output 处理：目录 或 .zip 文件路径
	var wantZip string
	if *output != "" {
		abs, _ := filepath.Abs(*output)
		if strings.HasSuffix(strings.ToLower(abs), ".zip") {
			wantZip = abs
			opts.OutputDir = filepath.Dir(abs)
		} else {
			opts.OutputDir = abs
		}
	}

	svc, err := NewService("")
	if err != nil {
		return err
	}
	cb, outputPathPtr := cliProgress()
	fmt.Println("开始导出...")
	outputPath, err := svc.RunExport(context.Background(), opts, cb)
	if err != nil {
		return err
	}
	// 指定了精确 zip 路径时重命名
	if wantZip != "" && outputPath != wantZip {
		if err := os.Rename(outputPath, wantZip); err == nil {
			outputPath = wantZip
		}
	}
	_ = outputPathPtr
	fmt.Printf("导出完成: %s\n", outputPath)
	return nil
}

// ---- import ----

func cliImport(args []string) error {
	fs := flag.NewFlagSet("import", flag.ExitOnError)
	configPath := fs.String("config", "", "配置文件(yaml)")
	taskID := fs.String("task", "", "已保存任务配置 ID")
	var target connFlags
	registerConnFlags(fs, "target", &target)
	input := fs.String("input", "", "导入文件(.sql 或 .zip)")
	reset := fs.String("reset", "", "重置模式: truncate|drop（默认不重置）")
	backup := fs.Bool("backup", true, "重置前创建备份表")
	batchSize := fs.Int("batch-size", 500, "批量大小")
	_ = fs.Parse(args)

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	opts := ImportOptions{Backup: true}
	if *configPath != "" {
		cfg, err := loadConfigFile(*configPath)
		if err != nil {
			return err
		}
		if cfg.Import != nil {
			opts = *cfg.Import
		}
	}
	if *taskID != "" {
		svc, err := NewService("")
		if err != nil {
			return err
		}
		task, err := svc.GetTask(*taskID)
		if err != nil {
			return err
		}
		if task.ImportOpts == nil {
			return cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("任务配置 %s 不是导入任务", *taskID))
		}
		opts = *task.ImportOpts
	}
	if conn := target.toConn(); conn != nil {
		opts.Target = conn
	}
	if setFlags["input"] {
		opts.InputPath = *input
	}
	if setFlags["reset"] {
		opts.ResetMode = ResetMode(*reset)
	}
	if setFlags["backup"] {
		opts.Backup = *backup
	}
	if setFlags["batch-size"] {
		opts.BatchSize = *batchSize
	}
	if opts.ResetMode != ResetNone && !opts.Backup {
		fmt.Println("警告: 重置数据且未开启备份，目标表现有数据将无法恢复！")
	}

	svc, err := NewService("")
	if err != nil {
		return err
	}
	cb, _ := cliProgress()
	fmt.Println("开始导入...")
	if err := svc.RunImport(context.Background(), opts, cb); err != nil {
		return err
	}
	fmt.Println("导入完成")
	return nil
}

// ---- migrate ----

func cliMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	configPath := fs.String("config", "", "配置文件(yaml)")
	taskID := fs.String("task", "", "已保存任务配置 ID")
	var src, target connFlags
	registerConnFlags(fs, "source", &src)
	registerConnFlags(fs, "target", &target)
	tables := fs.String("tables", "", "指定表，逗号分隔")
	objects := fs.String("objects", "", "指定对象，逗号分隔，格式 _views/名称（仅同类型迁移生效）")
	var conds multiFlag
	fs.Var(&conds, "table-cond", "表过滤条件，格式 表名:完整SELECT（可重复，兼容旧格式 表名:WHERE片段）")
	schemaOnly := fs.Bool("schema-only", false, "仅迁移结构")
	dataOnly := fs.Bool("data-only", false, "仅迁移数据")
	reset := fs.String("reset", "", "重置模式: truncate|drop（默认不重置）")
	backup := fs.Bool("backup", true, "重置前创建备份表")
	batchSize := fs.Int("batch-size", 500, "批量大小")
	_ = fs.Parse(args)

	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	opts := MigrateOptions{Backup: true}
	if *configPath != "" {
		cfg, err := loadConfigFile(*configPath)
		if err != nil {
			return err
		}
		if cfg.Migrate != nil {
			opts = *cfg.Migrate
		}
	}
	if *taskID != "" {
		svc, err := NewService("")
		if err != nil {
			return err
		}
		task, err := svc.GetTask(*taskID)
		if err != nil {
			return err
		}
		if task.MigrateOpts == nil {
			return cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("任务配置 %s 不是迁移任务", *taskID))
		}
		opts = *task.MigrateOpts
	}
	if conn := src.toConn(); conn != nil {
		opts.Source = conn
	}
	if conn := target.toConn(); conn != nil {
		opts.Target = conn
	}
	if setFlags["tables"] {
		opts.Tables = splitCSV(*tables)
	}
	if setFlags["objects"] {
		opts.Objects = splitCSV(*objects)
	}
	if setFlags["table-cond"] {
		opts.Conditions = append(opts.Conditions, parseTableConds(conds)...)
	}
	if setFlags["schema-only"] {
		opts.SchemaOnly = *schemaOnly
	}
	if setFlags["data-only"] {
		opts.DataOnly = *dataOnly
	}
	if setFlags["reset"] {
		opts.ResetMode = ResetMode(*reset)
	}
	if setFlags["backup"] {
		opts.Backup = *backup
	}
	if setFlags["batch-size"] {
		opts.BatchSize = *batchSize
	}
	if opts.ResetMode != ResetNone && !opts.Backup {
		fmt.Println("警告: 重置数据且未开启备份，目标表现有数据将无法恢复！")
	}

	svc, err := NewService("")
	if err != nil {
		return err
	}
	cb, _ := cliProgress()
	fmt.Println("开始迁移...")
	if err := svc.RunMigrate(context.Background(), opts, cb); err != nil {
		return err
	}
	fmt.Println("迁移完成")
	return nil
}

// ---- task ----

func cliTask(args []string) error {
	if len(args) == 0 {
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrDetailf("用法: dbimpex task <list|run|save|delete> [flags]"))
	}
	svc, err := NewService("")
	if err != nil {
		return err
	}
	switch args[0] {
	case "list":
		fs := flag.NewFlagSet("task list", flag.ExitOnError)
		taskType := fs.String("type", "", "按类型过滤: export|import|migrate")
		_ = fs.Parse(args[1:])
		tasks := svc.ListTasks(*taskType)
		if len(tasks) == 0 {
			fmt.Println("（无任务配置）")
			return nil
		}
		for _, t := range tasks {
			last := ""
			if t.IsLastUsed {
				last = " [最近使用]"
			}
			fmt.Printf("%s  %-8s %s%s\n", t.ID, t.Type, t.Name, last)
		}
		return nil
	case "run":
		fs := flag.NewFlagSet("task run", flag.ExitOnError)
		id := fs.String("id", "", "任务配置 ID")
		_ = fs.Parse(args[1:])
		if *id == "" {
			return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("缺少 --id"))
		}
		cb, _ := cliProgress()
		task, err := svc.GetTask(*id)
		if err != nil {
			return err
		}
		fmt.Printf("执行任务: %s (%s)\n", task.Name, task.Type)
		// CLI 直接同步执行
		switch task.Type {
		case "export":
			outputPath, err := svc.RunExport(context.Background(), *task.ExportOpts, cb)
			if err == nil {
				fmt.Printf("导出完成: %s\n", outputPath)
			}
			return err
		case "import":
			if err := svc.RunImport(context.Background(), *task.ImportOpts, cb); err != nil {
				return err
			}
			fmt.Println("导入完成")
			return nil
		case "migrate":
			if err := svc.RunMigrate(context.Background(), *task.MigrateOpts, cb); err != nil {
				return err
			}
			fmt.Println("迁移完成")
			return nil
		}
		return cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("未知任务类型: %s", task.Type))
	case "save":
		fs := flag.NewFlagSet("task save", flag.ExitOnError)
		name := fs.String("name", "", "任务名称")
		configPath := fs.String("config", "", "配置文件(yaml)")
		_ = fs.Parse(args[1:])
		if *name == "" || *configPath == "" {
			return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("缺少 --name 或 --config"))
		}
		cfg, err := loadConfigFile(*configPath)
		if err != nil {
			return err
		}
		task := TaskConfig{Name: *name}
		switch {
		case cfg.Export != nil:
			task.Type = "export"
			task.ExportOpts = cfg.Export
		case cfg.Import != nil:
			task.Type = "import"
			task.ImportOpts = cfg.Import
		case cfg.Migrate != nil:
			task.Type = "migrate"
			task.MigrateOpts = cfg.Migrate
		default:
			return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("配置文件中未找到 export/import/migrate 配置段"))
		}
		if err := svc.SaveTask(&task); err != nil {
			return err
		}
		fmt.Printf("任务已保存: %s (%s)\n", task.ID, task.Type)
		return nil
	case "delete":
		fs := flag.NewFlagSet("task delete", flag.ExitOnError)
		id := fs.String("id", "", "任务配置 ID")
		_ = fs.Parse(args[1:])
		if *id == "" {
			return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("缺少 --id"))
		}
		if err := svc.DeleteTask(*id); err != nil {
			return err
		}
		fmt.Println("任务已删除")
		return nil
	default:
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("未知子命令: task %s", args[0]))
	}
}
