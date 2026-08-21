package cli

// 点导入：CLI 层大量复用 service 包的模型别名与入口（NewService/选项模型/错误码）
import (
	"context"
	. "dqex/internal/service"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/fj1981/infrakit/pkg/cygin"
)

var exportSrc connFlags

var exportCmd = &cobra.Command{
	Use:     "export [database [table...]]",
	Aliases: []string{"exp"},
	Short:   "导出数据库结构及数据为 SQL（可压缩为 zip / gzip）",
	Long: `导出数据库结构及数据为 SQL（可压缩为 zip / gzip）。

独立闭环用法：
  dqex export --gen-config > export.yaml   # 生成配置模板
  vi export.yaml                              # 填写连接与选项
  dqex export --config export.yaml         # 执行导出

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--source-*）。
连接与常用 flag 提供 mysqldump 风格别名：
  --host/--port/--user/--password/--database、--no-data、--no-create-info
  短参：-h/-P/-u/-p（主机/端口/用户/密码）、-s（已保存连接）、-o（输出）、-T（表）
  位置参数 [库名 [表名...]] 等同 --databases/--tables`,
	RunE: cliExport,
}

func init() {
	reserveHelpFlag(exportCmd) // -h 让给 --host（mysqldump 风格），帮助用 --help
	f := exportCmd.Flags()
	f.String("config", "", "配置文件(yaml)，dqex export --gen-config 可生成模板")
	f.Bool("gen-config", false, "输出配置文件模板到标准输出")
	f.String("task", "", "已保存任务配置 ID（Web 保存的任务）")
	registerConnFlags(exportCmd, "source", &exportSrc)
	f.StringP("source-conn", "s", "", "使用已保存连接（ID 或名称），与 --source-* 同时给出时后者优先")
	_ = exportCmd.RegisterFlagCompletionFunc("task", completeTaskIDs("export"))
	_ = exportCmd.RegisterFlagCompletionFunc("source-conn", completeConnNames)
	f.StringP("output", "o", "", "输出目录或 .zip 文件路径")
	f.String("name", "", "任务名（用于生成文件名）")
	f.String("databases", "", "指定库，逗号分隔")
	f.StringP("tables", "T", "", "指定表，逗号分隔")
	f.String("objects", "", "指定对象，逗号分隔，格式 _views/名称")
	f.StringArray("table-cond", nil, "表过滤条件，格式 表名:完整SELECT（可重复，兼容旧格式 表名:WHERE片段）")
	f.Bool("schema-only", false, "仅导出结构")
	f.Bool("data-only", false, "仅导出数据")
	f.Bool("no-data", false, "不导出数据，仅导出结构（同 --schema-only，mysqldump 风格）")
	f.Bool("no-create-info", false, "不导出建表语句，仅导出数据（同 --data-only，mysqldump 风格）")
	f.Bool("compress", true, "打包为 zip")
	f.Bool("gzip", false, "SQL 文件 gzip 压缩为 .sql.gz（与 --compress 可同时开启；导入侧透明解压）")
	f.Bool("single-transaction", true, "一致性快照导出（等同 mysqldump --single-transaction，仅 MySQL/PostgreSQL 生效）")
	f.Int("batch-size", 500, "批量大小")
	registerConnAliases(exportCmd, "", "source-", &exportSrc)
	rootCmd.AddCommand(exportCmd)
}

func cliExport(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	if v, _ := f.GetBool("gen-config"); v {
		printConfigTemplate("export")
		return nil
	}

	opts := ExportOptions{Compress: true, SingleTransaction: true, Lang: cliLang()}
	var wantZip string
	if v, _ := f.GetString("config"); v != "" {
		cfg, lerr := loadExportConfig(v)
		if lerr != nil {
			return lerr
		}
		copts, oerr := exportOptsFromConfig(cfg)
		if oerr != nil {
			return oerr
		}
		opts = copts
		_, wantZip = splitOutput(cfg.Output)
	}
	if v, _ := f.GetString("task"); v != "" {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		task, err := svc.GetTask(v)
		if err != nil {
			return err
		}
		if task.ExportOpts == nil {
			return cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errTaskNotExp, v))
		}
		opts = *task.ExportOpts
	}

	// 命令行参数覆盖（仅 Changed 的 flag 生效）
	if v, _ := f.GetString("source-conn"); v != "" {
		opts.SourceConn = v
	}
	if conn := exportSrc.toConn(); conn != nil {
		opts.Source = conn
	}
	if f.Changed("name") {
		opts.TaskName, _ = f.GetString("name")
	}
	if f.Changed("databases") {
		v, _ := f.GetString("databases")
		opts.Databases = splitCSV(v)
	}
	if f.Changed("tables") {
		v, _ := f.GetString("tables")
		opts.Tables = splitCSV(v)
	}
	// mysqldump 风格位置参数：export [库名 [表名...]]（不覆盖已显式给出的 flag）
	if len(args) > 0 {
		if !f.Changed("databases") {
			opts.Databases = []string{args[0]}
		}
		if len(args) > 1 && !f.Changed("tables") {
			opts.Tables = args[1:]
		}
	}
	if f.Changed("objects") {
		v, _ := f.GetString("objects")
		opts.Objects = splitCSV(v)
	}
	if f.Changed("table-cond") {
		v, _ := f.GetStringArray("table-cond")
		opts.Conditions = append(opts.Conditions, parseTableConds(v)...)
	}
	if anyChanged(f, "schema-only", "no-data") {
		opts.SchemaOnly, _ = f.GetBool("schema-only")
		if !opts.SchemaOnly {
			opts.SchemaOnly, _ = f.GetBool("no-data")
		}
	}
	if anyChanged(f, "data-only", "no-create-info") {
		opts.DataOnly, _ = f.GetBool("data-only")
		if !opts.DataOnly {
			opts.DataOnly, _ = f.GetBool("no-create-info")
		}
	}
	if f.Changed("compress") {
		opts.Compress, _ = f.GetBool("compress")
	}
	if f.Changed("gzip") {
		opts.Gzip, _ = f.GetBool("gzip")
	}
	if f.Changed("single-transaction") {
		opts.SingleTransaction, _ = f.GetBool("single-transaction")
	}
	if f.Changed("batch-size") {
		opts.BatchSize, _ = f.GetInt("batch-size")
	}
	if f.Changed("output") {
		v, _ := f.GetString("output")
		opts.OutputDir, wantZip = splitOutput(v)
	}
	if opts.Source == nil && opts.SourceConn == "" {
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoSrcConn))
	}

	svc, err := newCliService()
	if err != nil {
		return err
	}
	cb, _ := cliProgress()
	fmt.Println(cliTextsFor(cliLang()).startExport)
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
	printf(cliTextsFor(cliLang()).doneExport+"\n", outputPath)
	return nil
}
