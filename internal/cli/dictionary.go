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

var dictSrc connFlags

var dictionaryCmd = &cobra.Command{
	Use:     "dictionary [database [table...]]",
	Aliases: []string{"dict"},
	Short:   "生成数据库表/字段数据字典为 Excel（.xlsx，含注释）",
	Long: `生成数据库表/字段数据字典为 Excel（.xlsx，含表/列注释），可压缩为 zip。
产物为单个交付级工作簿：总览 sheet（全实例表清单，超链接跳转）+ 每库一个字段明细 sheet。

独立闭环用法：
  dqex dictionary --gen-config > dictionary.yaml   # 生成配置模板
  vi dictionary.yaml                                # 填写连接与选项
  dqex dictionary --config dictionary.yaml         # 执行生成

命令行参数优先于配置文件；也可完全通过 flags 指定连接（--source-*）。
  短参：-h/-P/-u/-p（主机/端口/用户/密码）、-s（已保存连接）、-o（输出）、-T（表）
  位置参数 [库名 [表名...]] 等同 --databases/--tables`,
	RunE: cliDictionary,
}

func init() {
	reserveHelpFlag(dictionaryCmd) // -h 让给 --host（mysqldump 风格），帮助用 --help
	f := dictionaryCmd.Flags()
	f.String("config", "", "配置文件(yaml)，dqex dictionary --gen-config 可生成模板")
	f.Bool("gen-config", false, "输出配置文件模板到标准输出")
	f.String("task", "", "已保存任务配置 ID（Web 保存的任务）")
	registerConnFlags(dictionaryCmd, "source", &dictSrc)
	f.StringP("source-conn", "s", "", "使用已保存连接（ID 或名称），与 --source-* 同时给出时后者优先")
	_ = dictionaryCmd.RegisterFlagCompletionFunc("task", completeTaskIDs("dictionary"))
	_ = dictionaryCmd.RegisterFlagCompletionFunc("source-conn", completeConnNames)
	f.StringP("output", "o", "", "输出目录或 .zip 文件路径")
	f.String("name", "", "任务名（用于生成文件名）")
	f.String("databases", "", "指定库，逗号分隔")
	f.StringP("tables", "T", "", "指定表，逗号分隔")
	f.Bool("compress", true, "打包为 zip")
	registerConnAliases(dictionaryCmd, "", "source-", &dictSrc)
	rootCmd.AddCommand(dictionaryCmd)
}

func cliDictionary(cmd *cobra.Command, args []string) error {
	f := cmd.Flags()
	if v, _ := f.GetBool("gen-config"); v {
		printConfigTemplate("dictionary")
		return nil
	}

	opts := DictionaryOptions{Compress: true, Lang: cliLang()}
	var wantZip string
	if v, _ := f.GetString("config"); v != "" {
		cfg, lerr := loadDictionaryConfig(v)
		if lerr != nil {
			return lerr
		}
		dopts, oerr := dictionaryOptsFromConfig(cfg)
		if oerr != nil {
			return oerr
		}
		opts = dopts
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
		if task.DictionaryOpts == nil {
			return cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf(cliTextsFor(cliLang()).errTaskNotDict, v))
		}
		opts = *task.DictionaryOpts
	}

	// 命令行参数覆盖（仅 Changed 的 flag 生效）
	if v, _ := f.GetString("source-conn"); v != "" {
		opts.SourceConn = v
	}
	if conn := dictSrc.toConn(); conn != nil {
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
	// mysqldump 风格位置参数：dictionary [库名 [表名...]]（不覆盖已显式给出的 flag）
	if len(args) > 0 {
		if !f.Changed("databases") {
			opts.Databases = []string{args[0]}
		}
		if len(args) > 1 && !f.Changed("tables") {
			opts.Tables = args[1:]
		}
	}
	if f.Changed("compress") {
		opts.Compress, _ = f.GetBool("compress")
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
	fmt.Println(cliTextsFor(cliLang()).startDict)
	outputPath, err := svc.RunDictionary(context.Background(), opts, cb)
	if err != nil {
		return err
	}
	// 指定了精确 zip 路径时重命名
	if wantZip != "" && outputPath != wantZip {
		if err := os.Rename(outputPath, wantZip); err == nil {
			outputPath = wantZip
		}
	}
	printf(cliTextsFor(cliLang()).doneDict+"\n", outputPath)
	return nil
}
