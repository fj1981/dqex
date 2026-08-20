package cli

// 点导入：CLI 层大量复用 service 包的模型别名与入口（NewService/选项模型/错误码）
import (
	. "dbimpex/internal/service"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var configCmd = &cobra.Command{
	Use:     "config",
	Aliases: []string{"cfg"},
	Short:   "查看全局配置（数据目录等）",
	Long: `查看解析后的全局配置（四类数据目录）。

全局配置文件为 config.yaml，查找顺序：
  --config-file > 环境变量 DBIMPEX_CONFIG > ~/.dbimpex/config.yaml

目录优先级：--data-dir > 配置文件 dirs.data > 默认 ~/.dbimpex；
其余目录：配置文件显式值 > 由 data 目录派生。
使用 dbx config --gen 输出配置模板。`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if v, _ := cmd.Flags().GetBool("gen"); v {
			os.Stdout.WriteString(appConfigTemplate)
			return nil
		}
		cfgPath := FindConfigFile(webArgs.ConfigFile)
		cfg, err := LoadAppConfig(cfgPath)
		if err != nil {
			return err
		}
		dirs := ResolveDirs(webArgs.DataDir, cfg)
		txt := cliTextsFor(cliLang())
		if cfgPath == "" {
			fmt.Println(txt.cfgNotFound)
		} else {
			printf(txt.cfgPath+"\n", cfgPath)
		}
		printf(txt.cfgDirData+"\n", dirs.Data)
		printf(txt.cfgDirTmp+"\n", dirs.Tmp)
		printf(txt.cfgDirUploads+"\n", dirs.Uploads)
		printf(txt.cfgDirExports+"\n", dirs.Exports)
		if len(cfg.Web.Allow) > 0 {
			printf(txt.cfgAllow+"\n", strings.Join(cfg.Web.Allow, ", "))
		} else {
			fmt.Println(txt.cfgAllowNone)
		}
		return nil
	},
}

func init() {
	configCmd.Flags().Bool("gen", false, "输出全局配置模板到标准输出")
	rootCmd.AddCommand(configCmd)
}

const appConfigTemplate = `# dbx 全局配置
# 默认位置 ~/.dbimpex/config.yaml，可用 --config-file 或环境变量 DBIMPEX_CONFIG 指定
# 留空的项由 data 目录派生
dirs:
  data: ""        # ① 配置保存目录（connections/tasks/history），默认 ~/.dbimpex
  tmp: ""         # ② 任务处理临时目录，默认 <data>/tmp
  uploads: ""     # ③ Web 上传临时目录，默认 <data>/uploads
  exports: ""     # ④ 最终生成产物目录，默认 <data>/exports
web:
  allow: []       # ⑤ 允许访问的来源白名单（IP/CIDR/域名），留空不限制；本机回环始终放行
  # allow:        # 示例：对外暴露时收紧来源（--allow 命令行参数优先于此配置）
  #   - 192.168.1.0/24
  #   - 10.20.16.170
  #   - dbx.example.com
compat_collation: true  # 兼容排序规则：MySQL 8.0 特有排序规则替换为 5.7 兼容版本（默认开启）
`
