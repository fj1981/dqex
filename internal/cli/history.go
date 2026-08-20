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

// cliStatus 状态词双语展示（zh/en 查 cliTexts 注册表）
func cliStatus(status string) string {
	txt := cliTextsFor(cliLang())
	switch status {
	case "done":
		return txt.statusDone
	case "error":
		return txt.statusError
	case "cancelled":
		return txt.statusCancelled
	case "running":
		return txt.statusRunning
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
			fmt.Println(cliTextsFor(cliLang()).histNone)
			return nil
		}
		txt := cliTextsFor(cliLang())
		printf("%-26s %-8s %-8s %-19s %s\n", txt.hdrID, txt.hdrType, txt.hdrStatus, txt.hdrStartedAt, txt.hdrSummary)
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
			printf("%-26s %-8s %-8s %-19s %s\n",
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
		txt := cliTextsFor(cliLang())
		printf(txt.histID+"\n", r.ID)
		printf(txt.histType+"\n", r.TaskType)
		printf(txt.histStatus+"\n", cliStatus(r.Status))
		printf(txt.histStarted+"\n", time.UnixMilli(r.StartedAt).Format("2006-01-02 15:04:05"))
		if r.FinishedAt > 0 {
			printf(txt.histFinished+"\n", time.UnixMilli(r.FinishedAt).Format("2006-01-02 15:04:05"))
		}
		if r.Duration > 0 {
			printf(txt.histDuration+"\n", time.Duration(r.Duration)*time.Millisecond)
		}
		if r.Target != "" {
			printf(txt.histTarget+"\n", r.Target)
		}
		printf(txt.histProgress, r.DoneUnits, r.TotalUnits)
		if r.TotalRows > 0 {
			printf(txt.histRows, r.TotalRows)
		}
		fmt.Println()
		if r.Summary != "" {
			printf(txt.histSummary+"\n", r.Summary)
		}
		if r.OutputPath != "" {
			printf(txt.histOutput+"\n", r.OutputPath)
		}
		if r.ErrorMsg != "" {
			printf(txt.histError+"\n", r.ErrorMsg)
		}
		if len(r.Logs) > 0 {
			fmt.Println(txt.histLogs)
			for _, line := range r.Logs {
				printf("  %s\n", line)
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
		fmt.Println(cliTextsFor(cliLang()).histDeleted)
		return nil
	},
}
