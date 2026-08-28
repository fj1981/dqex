package cli

// 点导入：CLI 层大量复用 service 包的模型别名与入口（NewService/选项模型/错误码）
import (
	"context"
	"fmt"

	. "github.com/fj1981/dqex/internal/service"

	"github.com/fj1981/infrakit/pkg/cygin"
	"github.com/spf13/cobra"
)

var migrateSrc, migrateTarget connFlags

var migrateCmd = &cobra.Command{
	Use:     "migrate",
	Aliases: []string{"mig"},
	Short:   "数据库迁移（支持跨类型）",
	Long: `数据库迁移（支持跨类型）。

独立闭环用法：
  dqex migrate --gen-config > migrate.yaml   # 生成配置模板
  vi migrate.yaml                               # 填写连接与选项
  dqex migrate --config migrate.yaml         # 执行迁移

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--source-* / --target-*）。
连接 flag 提供 mysqldump 风格别名：--source-user/--source-password/--source-database（及 target- 同名系列）`,
	RunE: cliMigrate,
}

func init() {
	f := migrateCmd.Flags()
	f.String("config", "", "配置文件(yaml)，dqex migrate --gen-config 可生成模板")
	f.Bool("gen-config", false, "输出配置文件模板到标准输出")
	f.String("task", "", "已保存任务配置 ID（Web 保存的任务）")
	registerConnFlags(migrateCmd, "source", &migrateSrc)
	registerConnFlags(migrateCmd, "target", &migrateTarget)
	f.StringP("source-conn", "s", "", "使用已保存源连接（ID 或名称）")
	f.StringP("target-conn", "t", "", "使用已保存目标连接（ID 或名称）")
	_ = migrateCmd.RegisterFlagCompletionFunc("task", completeTaskIDs("migrate"))
	_ = migrateCmd.RegisterFlagCompletionFunc("source-conn", completeConnNames)
	_ = migrateCmd.RegisterFlagCompletionFunc("target-conn", completeConnNames)
	f.StringP("tables", "T", "", "指定表，逗号分隔")
	f.String("objects", "", "指定对象，逗号分隔，格式 _views/名称（仅同类型迁移生效）")
	f.StringArray("table-cond", nil, "表过滤条件，格式 表名:完整SELECT（可重复，兼容旧格式 表名:WHERE片段）")
	f.Bool("schema-only", false, "仅迁移结构")
	f.Bool("data-only", false, "仅迁移数据")
	f.String("reset", "", "重置模式: truncate|drop（默认不重置）")
	_ = migrateCmd.RegisterFlagCompletionFunc("reset", fixedCompletion("truncate", "drop"))
	f.Bool("backup", true, "重置前创建备份表")
	f.Int("batch-size", 500, "批量大小")
	registerConnAliases(migrateCmd, "source-", "source-", &migrateSrc)
	registerConnAliases(migrateCmd, "target-", "target-", &migrateTarget)
	rootCmd.AddCommand(migrateCmd)
}

func cliMigrate(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	if v, _ := f.GetBool("gen-config"); v {
		printConfigTemplate("migrate")
		return nil
	}

	opts := MigrateOptions{Backup: true, Lang: cliLang()}
	var migCfg *migrateConfig
	if v, _ := f.GetString("config"); v != "" {
		cfg, lerr := loadMigrateConfig(v)
		if lerr != nil {
			return lerr
		}
		migCfg = cfg
		copts, oerr := migrateOptsFromConfig(cfg)
		if oerr != nil {
			return oerr
		}
		opts = copts
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
		if task.MigrateOpts == nil {
			return cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errTaskNotMig, v))
		}
		opts = *task.MigrateOpts
	}

	// 命令行参数覆盖（仅 Changed 的 flag 生效）
	if v, _ := f.GetString("source-conn"); v != "" {
		opts.SourceConn = v
	}
	if v, _ := f.GetString("target-conn"); v != "" {
		opts.TargetConn = v
	}
	if conn := migrateSrc.toConn(); conn != nil {
		opts.Source = conn
	}
	if conn := migrateTarget.toConn(); conn != nil {
		opts.Target = conn
	}
	if f.Changed("tables") {
		v, _ := f.GetString("tables")
		opts.Tables = splitCSV(v)
	}
	if f.Changed("objects") {
		v, _ := f.GetString("objects")
		opts.Objects = splitCSV(v)
	}
	if f.Changed("table-cond") {
		v, _ := f.GetStringArray("table-cond")
		opts.Conditions = append(opts.Conditions, parseTableConds(v)...)
	}
	if f.Changed("schema-only") {
		opts.SchemaOnly, _ = f.GetBool("schema-only")
	}
	if f.Changed("data-only") {
		opts.DataOnly, _ = f.GetBool("data-only")
	}
	if f.Changed("reset") {
		v, _ := f.GetString("reset")
		reset, err := validResetMode(v)
		if err != nil {
			return err
		}
		opts.ResetMode = reset
	}
	if f.Changed("backup") {
		opts.Backup, _ = f.GetBool("backup")
	}
	if f.Changed("batch-size") {
		opts.BatchSize, _ = f.GetInt("batch-size")
	}
	if opts.Source == nil && opts.SourceConn == "" {
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoSrcConn))
	}
	if opts.Target == nil && opts.TargetConn == "" {
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoTgtConn))
	}
	if opts.ResetMode != ResetNone && !opts.Backup {
		fmt.Println(yellow(cliTextsFor(cliLang()).warnReset))
	}

	svc, err := newCliService()
	if err != nil {
		return err
	}
	// 库名覆盖：引用连接解析为内联并写入指定库（引擎要求连接带库）；
	// 命令行 --source-db/--source-database 优先于配置文件 source_database
	srcDB, tgtDB := migrateSrc.db, migrateTarget.db
	if migCfg != nil {
		if srcDB == "" {
			srcDB = migCfg.SourceDB
		}
		if tgtDB == "" {
			tgtDB = migCfg.TargetDB
		}
	}
	if srcDB != "" && opts.SourceConn != "" && opts.Source == nil {
		conn, oerr := overrideConnDB(svc, opts.SourceConn, srcDB)
		if oerr != nil {
			return oerr
		}
		opts.Source, opts.SourceConn = conn, ""
	}
	if tgtDB != "" && opts.TargetConn != "" && opts.Target == nil {
		conn, oerr := overrideConnDB(svc, opts.TargetConn, tgtDB)
		if oerr != nil {
			return oerr
		}
		opts.Target, opts.TargetConn = conn, ""
	}
	cb, _ := cliProgress()
	fmt.Println(cliTextsFor(cliLang()).startMigrate)
	if err := svc.RunMigrate(context.Background(), opts, cb); err != nil {
		return err
	}
	fmt.Println(green(cliTextsFor(cliLang()).doneMigrate))
	return nil
}
