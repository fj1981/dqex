package main

import (
	"fmt"
	"os"

	"dbimpex/internal/cli"
	"dbimpex/internal/service"
	"dbimpex/internal/web"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

func main() {
	// CLI 子命令优先：Execute 返回 nil 表示已执行 CLI 子命令（或 help），直接退出
	args := cli.Execute()
	if args == nil {
		return
	}
	runWeb(args)
}

// runWeb 启动 Web 服务（默认模式）
func runWeb(args *cli.WebArgs) {
	cylog.InitDefault()

	svc, err := service.NewServiceWith(args.DataDir, args.ConfigFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化服务失败: %v\n", err)
		os.Exit(1)
	}
	web.RunWeb(svc, args.Port)
}
