package cli

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var stopCmd = &cobra.Command{
	Use:   "stop",
	Short: "终止其他 dqex 进程",
	Long: `查找并终止所有其他正在运行的 dqex 进程。
当前进程不会被终止。非交互环境下自动确认终止。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		procs := findDqexProcesses()
		// 过滤当前进程
		myPID := os.Getpid()
		var others []procInfo
		for _, p := range procs {
			if p.PID != myPID {
				others = append(others, p)
			}
		}
		if len(others) == 0 {
			fmt.Println("未找到其他 dqex 进程")
			return nil
		}
		// 展示将要终止的进程
		fmt.Println("找到以下 dqex 进程:")
		for _, p := range others {
			fmt.Printf("  PID %d  %s\n", p.PID, p.Name)
		}
		// 交互确认
		if term.IsTerminal(int(os.Stdin.Fd())) {
			fmt.Print("是否终止全部进程? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if !strings.EqualFold(strings.TrimSpace(answer), "y") {
				fmt.Println("已取消")
				return nil
			}
		} else {
			fmt.Println("非交互环境，自动确认终止...")
		}
		// 终止进程
		for _, p := range others {
			fmt.Printf("正在终止进程 %d ...\n", p.PID)
			if err := killProc(p.PID); err != nil {
				fmt.Printf("  终止失败: %v\n", err)
				continue
			}
			// 等待进程退出
			exited := false
			for i := 0; i < 50; i++ {
				if !processExists(p.PID) {
					exited = true
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if !exited {
				fmt.Printf("  超时，强制终止...\n")
				killProcForce(p.PID)
			}
		}
		fmt.Println("终止完成")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(stopCmd)
}

// procInfo 进程信息（stop 命令内部使用）
type procInfo struct {
	PID  int
	Name string
}

// findDqexProcesses 查找所有 dqex 进程（跨平台）
func findDqexProcesses() []procInfo {
	switch runtime.GOOS {
	case "windows":
		return findDqexProcessesWindows()
	default:
		return findDqexProcessesUnix()
	}
}
