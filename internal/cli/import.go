package cli

// 点导入：CLI 层大量复用 service 包的模型别名与入口（NewService/选项模型/错误码）
import (
	"context"
	. "dqex/internal/service"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/fj1981/infrakit/pkg/cygin"
)

var importTarget connFlags

var importCmd = &cobra.Command{
	Use:     "import",
	Aliases: []string{"imp"},
	Short:   "导入 SQL 文件（.sql / .sql.gz / .zip）到数据库",
	Long: `导入 SQL 文件（.sql / .sql.gz / .zip）到数据库。

独立闭环用法：
  dqex import --gen-config > import.yaml   # 生成配置模板
  vi import.yaml                              # 填写连接与选项
  dqex import --config import.yaml         # 执行导入

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--target-*）。
连接 flag 提供 mysqldump 风格别名：--host/--port/--user/--password/--database
短参：-h/-P/-u/-p（主机/端口/用户/密码）、-t（已保存连接）、-i（导入文件）`,
	RunE: cliImport,
}

func init() {
	reserveHelpFlag(importCmd) // -h 让给 --host（mysqldump 风格），帮助用 --help
	f := importCmd.Flags()
	f.String("config", "", "配置文件(yaml)，dqex import --gen-config 可生成模板")
	f.Bool("gen-config", false, "输出配置文件模板到标准输出")
	f.String("task", "", "已保存任务配置 ID（Web 保存的任务）")
	registerConnFlags(importCmd, "target", &importTarget)
	f.StringP("target-conn", "t", "", "使用已保存连接（ID 或名称），与 --target-* 同时给出时后者优先")
	_ = importCmd.RegisterFlagCompletionFunc("task", completeTaskIDs("import"))
	_ = importCmd.RegisterFlagCompletionFunc("target-conn", completeConnNames)
	f.StringP("input", "i", "", "导入文件(.sql / .sql.gz / .zip)")
	f.String("reset", "", "重置模式: truncate|drop（默认不重置）")
	_ = importCmd.RegisterFlagCompletionFunc("reset", fixedCompletion("truncate", "drop"))
	f.Bool("backup", true, "重置前创建备份表")
	f.Int("batch-size", 500, "批量大小")
	registerConnAliases(importCmd, "", "target-", &importTarget)
	rootCmd.AddCommand(importCmd)
}

func cliImport(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	if v, _ := f.GetBool("gen-config"); v {
		printConfigTemplate("import")
		return nil
	}

	opts := ImportOptions{Backup: true, Lang: cliLang()}
	if v, _ := f.GetString("config"); v != "" {
		cfg, lerr := loadImportConfig(v)
		if lerr != nil {
			return lerr
		}
		copts, oerr := importOptsFromConfig(cfg)
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
		if task.ImportOpts == nil {
			return cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errTaskNotImp, v))
		}
		opts = *task.ImportOpts
	}

	// 命令行参数覆盖（仅 Changed 的 flag 生效）
	if v, _ := f.GetString("target-conn"); v != "" {
		opts.TargetConn = v
	}
	if conn := importTarget.toConn(); conn != nil {
		opts.Target = conn
	}
	// 库名覆盖：引用已保存连接时用 --database/--target-database 指定目标库（单文件导入必需）
	if importTarget.db != "" && opts.TargetConn != "" && opts.Target == nil {
		svcOv, oerr := newCliService()
		if oerr != nil {
			return oerr
		}
		conn, oerr := overrideConnDB(svcOv, opts.TargetConn, importTarget.db)
		if oerr != nil {
			return oerr
		}
		opts.Target, opts.TargetConn = conn, ""
	}
	if f.Changed("input") {
		opts.InputPath, _ = f.GetString("input")
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
	if opts.Target == nil && opts.TargetConn == "" {
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoTgtConn))
	}
	if opts.InputPath == "" {
		return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetails(cliTextsFor(cliLang()).errNoImportFile))
	}
	if opts.ResetMode != ResetNone && !opts.Backup {
		fmt.Println(yellow(cliTextsFor(cliLang()).warnReset))
	}

	svc, err := newCliService()
	if err != nil {
		return err
	}
	cb, _ := cliProgress()
	fmt.Println(cliTextsFor(cliLang()).startImport)
	if err := svc.RunImport(context.Background(), opts, cb); err != nil {
		return err
	}
	fmt.Println(green(cliTextsFor(cliLang()).doneImport))
	return nil
}
