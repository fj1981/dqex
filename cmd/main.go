package main

import (
	"flag"
	"fmt"
	"os"

	"dbimpex/internal/service"
	"dbimpex/internal/web"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

func main() {
	// CLI 子命令优先
	if len(os.Args) > 1 && service.IsCLISubcommand(os.Args[1]) {
		service.RunCLI(os.Args[1:])
		return
	}
	runWeb()
}

// runWeb 启动 Web 服务（默认模式）
func runWeb() {
	fs := flag.NewFlagSet("web", flag.ExitOnError)
	port := fs.Int("port", 8181, "Web 服务端口")
	dataDir := fs.String("data-dir", "", "数据存储目录（默认 ~/.dbimpex）")
	_ = fs.Parse(os.Args[1:])

	cylog.InitDefault()

	svc, err := service.NewService(*dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化服务失败: %v\n", err)
		os.Exit(1)
	}
	web.RunWeb(svc, *port)
}
