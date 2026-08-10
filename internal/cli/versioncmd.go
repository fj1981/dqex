package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Version 构建版本号（release 打包时经 -ldflags 注入，默认 dev）
var Version = "dev"

// BuildTime 构建时间（Makefile 构建时经 -ldflags 注入，默认空）
var BuildTime = ""

var versionCmd = &cobra.Command{
	Use:     "version",
	Aliases: []string{"v"},
	Short:   "查看版本号",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("dbx %s\n", Version)
		if BuildTime != "" {
			fmt.Printf("构建时间: %s\n", BuildTime)
		}
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
