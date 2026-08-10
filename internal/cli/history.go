package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var historyCmd = &cobra.Command{
	Use:     "history",
	Aliases: []string{"his"},
	Short:   "执行历史管理（list/show/delete）",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	historyListCmd.Flags().String("type", "", "按类型过滤: export|import|migrate|compare")
	_ = historyListCmd.RegisterFlagCompletionFunc("type", fixedCompletion("export", "import", "migrate", "compare"))
	historyGetCmd.Flags().String("id", "", "执行记录 ID")
	historyDeleteCmd.Flags().String("id", "", "执行记录 ID")
	_ = historyGetCmd.MarkFlagRequired("id")
	_ = historyDeleteCmd.MarkFlagRequired("id")
	_ = historyGetCmd.RegisterFlagCompletionFunc("id", completeHistoryIDs(""))
	_ = historyDeleteCmd.RegisterFlagCompletionFunc("id", completeHistoryIDs(""))
	historyCmd.AddCommand(historyListCmd, historyGetCmd, historyDeleteCmd)
	rootCmd.AddCommand(historyCmd)
}

// cliStatus 状态中文展示
func cliStatus(status string) string {
	switch status {
	case "done":
		return "成功"
	case "error":
		return "失败"
	case "cancelled":
		return "已取消"
	case "running":
		return "运行中"
	}
	return status
}

var historyListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "列出执行历史",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		taskType, _ := cmd.Flags().GetString("type")
		records := svc.ListHistory(taskType, "")
		if len(records) == 0 {
			fmt.Println("（无执行历史）")
			return nil
		}
		fmt.Printf("%-26s %-8s %-8s %-19s %s\n", "ID", "类型", "状态", "开始时间", "摘要")
		for _, r := range records {
			summary := r.Summary
			if r.Status == "error" && r.ErrorMsg != "" {
				summary = r.ErrorMsg
			}
			if r.Target != "" && summary != "" {
				summary = r.Target + " · " + summary
			} else if r.Target != "" {
				summary = r.Target
			}
			fmt.Printf("%-26s %-8s %-8s %-19s %s\n",
				r.ID, r.TaskType, cliStatus(r.Status),
				time.UnixMilli(r.StartedAt).Format("2006-01-02 15:04:05"), summary)
		}
		return nil
	},
}

var historyGetCmd = &cobra.Command{
	Use:   "show",
	Short: "查看执行记录详情",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		r, err := svc.GetHistory(id)
		if err != nil {
			return err
		}
		fmt.Printf("ID:       %s\n", r.ID)
		fmt.Printf("类型:     %s\n", r.TaskType)
		fmt.Printf("状态:     %s\n", cliStatus(r.Status))
		fmt.Printf("开始:     %s\n", time.UnixMilli(r.StartedAt).Format("2006-01-02 15:04:05"))
		if r.FinishedAt > 0 {
			fmt.Printf("结束:     %s\n", time.UnixMilli(r.FinishedAt).Format("2006-01-02 15:04:05"))
		}
		if r.Duration > 0 {
			fmt.Printf("耗时:     %s\n", time.Duration(r.Duration)*time.Millisecond)
		}
		if r.Target != "" {
			fmt.Printf("目标:     %s\n", r.Target)
		}
		fmt.Printf("进度:     %d/%d 项", r.DoneUnits, r.TotalUnits)
		if r.TotalRows > 0 {
			fmt.Printf("，%d 行", r.TotalRows)
		}
		fmt.Println()
		if r.Summary != "" {
			fmt.Printf("摘要:     %s\n", r.Summary)
		}
		if r.OutputPath != "" {
			fmt.Printf("输出文件: %s\n", r.OutputPath)
		}
		if r.ErrorMsg != "" {
			fmt.Printf("错误:     %s\n", r.ErrorMsg)
		}
		if len(r.Logs) > 0 {
			fmt.Println("日志:")
			for _, line := range r.Logs {
				fmt.Printf("  %s\n", line)
			}
		}
		return nil
	},
}

var historyDeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "删除执行记录（运行中的任务不允许删除）",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		id, _ := cmd.Flags().GetString("id")
		if err := svc.DeleteHistory(id); err != nil {
			return err
		}
		fmt.Println("记录已删除")
		return nil
	},
}
