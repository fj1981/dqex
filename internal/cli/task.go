package cli

// 点导入：CLI 层大量复用 service 包的模型别名与入口（NewService/选项模型/错误码）
import (
	"context"
	. "dbimpex/internal/service"
	"fmt"

	"github.com/spf13/cobra"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gopkg.in/yaml.v3"
)

var taskCmd = &cobra.Command{
	Use:     "task",
	Aliases: []string{"tk"},
	Short:   "任务配置管理（list/show/run/save/delete）",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	taskListCmd.Flags().String("type", "", "按类型过滤: export|import|migrate|compare")
	_ = taskListCmd.RegisterFlagCompletionFunc("type", fixedCompletion("export", "import", "migrate", "compare"))
	taskShowCmd.Flags().String("id", "", "任务配置 ID")
	taskRunCmd.Flags().String("id", "", "任务配置 ID")
	taskSaveCmd.Flags().String("name", "", "任务名称")
	taskSaveCmd.Flags().String("config", "", "配置文件(yaml)，支持独立格式与旧嵌套格式")
	taskSaveCmd.Flags().String("type", "", "配置类型: export|import|migrate|compare（默认自动识别）")
	_ = taskSaveCmd.RegisterFlagCompletionFunc("type", fixedCompletion("export", "import", "migrate", "compare"))
	taskDeleteCmd.Flags().String("id", "", "任务配置 ID")
	_ = taskShowCmd.MarkFlagRequired("id")
	_ = taskRunCmd.MarkFlagRequired("id")
	_ = taskDeleteCmd.MarkFlagRequired("id")
	_ = taskShowCmd.RegisterFlagCompletionFunc("id", completeTaskIDs(""))
	_ = taskRunCmd.RegisterFlagCompletionFunc("id", completeTaskIDs(""))
	_ = taskDeleteCmd.RegisterFlagCompletionFunc("id", completeTaskIDs(""))
	taskCmd.AddCommand(taskListCmd, taskShowCmd, taskRunCmd, taskSaveCmd, taskDeleteCmd)
	rootCmd.AddCommand(taskCmd)
}

var taskListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "列出任务配置",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		taskType, _ := cmd.Flags().GetString("type")
		tasks := svc.ListTasks(taskType)
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
	},
}

var taskShowCmd = &cobra.Command{
	Use:   "show",
	Short: "查看任务配置详情（yaml）",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		task, err := svc.GetTask(id)
		if err != nil {
			return err
		}
		data, err := yaml.Marshal(task)
		if err != nil {
			return err
		}
		fmt.Print(string(data))
		return nil
	},
}

var taskRunCmd = &cobra.Command{
	Use:   "run",
	Short: "执行已保存的任务配置（同步）",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		task, err := svc.GetTask(id)
		if err != nil {
			return err
		}
		fmt.Printf("执行任务: %s (%s)\n", task.Name, task.Type)
		cb, _ := cliProgress()
		ctx := context.Background()
		// CLI 直接同步执行
		switch task.Type {
		case "export":
			outputPath, err := svc.RunExport(ctx, *task.ExportOpts, cb)
			if err == nil {
				fmt.Printf("导出完成: %s\n", outputPath)
			}
			return err
		case "import":
			if err := svc.RunImport(ctx, *task.ImportOpts, cb); err != nil {
				return err
			}
			fmt.Println("导入完成")
			return nil
		case "migrate":
			if err := svc.RunMigrate(ctx, *task.MigrateOpts, cb); err != nil {
				return err
			}
			fmt.Println("迁移完成")
			return nil
		case "compare":
			recordID, result, err := svc.RunCompareRecorded(ctx, *task.CompareOpts, cb, id)
			if err != nil {
				return err
			}
			printCompareReport(result, false)
			printCompareShowHint(recordID)
			return nil
		}
		return cygin.NewError(ErrTaskInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("未知任务类型: %s", task.Type))
	},
}

var taskSaveCmd = &cobra.Command{
	Use:   "save",
	Short: "从配置文件保存任务配置",
	RunE: func(cmd *cobra.Command, args []string) error {
		f := cmd.Flags()
		name, _ := f.GetString("name")
		configPath, _ := f.GetString("config")
		if name == "" || configPath == "" {
			return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("缺少 --name 或 --config"))
		}
		data, err := readConfigFile(configPath)
		if err != nil {
			return err
		}
		hint, _ := f.GetString("type")
		kind, err := detectConfigKind(data, hint)
		if err != nil {
			return err
		}
		task := TaskConfig{Name: name, Type: kind}
		switch kind {
		case "export":
			cfg, err := loadExportConfig(configPath)
			if err != nil {
				return err
			}
			opts, err := exportOptsFromConfig(cfg)
			if err != nil {
				return err
			}
			task.ExportOpts = &opts
		case "import":
			cfg, err := loadImportConfig(configPath)
			if err != nil {
				return err
			}
			opts, err := importOptsFromConfig(cfg)
			if err != nil {
				return err
			}
			task.ImportOpts = &opts
		case "migrate":
			cfg, err := loadMigrateConfig(configPath)
			if err != nil {
				return err
			}
			opts, err := migrateOptsFromConfig(cfg)
			if err != nil {
				return err
			}
			task.MigrateOpts = &opts
		case "compare":
			cfg, err := loadCompareConfig(configPath)
			if err != nil {
				return err
			}
			opts, _, err := compareOptsFromConfig(cfg, true)
			if err != nil {
				return err
			}
			task.CompareOpts = &opts
		}
		svc, err := newCliService()
		if err != nil {
			return err
		}
		if err := svc.SaveTask(&task); err != nil {
			return err
		}
		fmt.Printf("任务已保存: %s (%s)\n", task.ID, task.Type)
		return nil
	},
}

var taskDeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "删除任务配置",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		if err := svc.DeleteTask(id); err != nil {
			return err
		}
		fmt.Println("任务已删除")
		return nil
	},
}
