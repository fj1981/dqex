package main

import (
	"fmt"
	"os"
	"strings"

	"dbimpex/internal/cli"
	"dbimpex/internal/engine"
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
	defer engine.CloseAllCliPool() // 释放进程级连接池（对比/获取表信息复用池化 cli）
	cylog.InitDefault()

	svc, err := service.NewServiceWith(args.DataDir, args.ConfigFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化服务失败: %v\n", err)
		os.Exit(1)
	}
	defer svc.Close() // 释放 SQLite 连接
	// 访问来源白名单：--allow flag 优先，未给出时取配置文件 web.allow
	allow := []string{}
	for _, item := range strings.Split(args.Allow, ",") {
		if item = strings.TrimSpace(item); item != "" {
			allow = append(allow, item)
		}
	}
	if len(allow) == 0 {
		allow = svc.Config().Web.Allow
	}
	web.RunWeb(svc, args.Host, args.Port, allow, args.NoAuth, args.NoBrowser)
}
