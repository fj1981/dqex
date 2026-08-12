package cli

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"
)

var urlCmd = &cobra.Command{
	Use:   "url",
	Short: "输出 Web 访问链接（带 token）",
	Long: `输出当前数据目录下的 Web 访问链接（带 token），可直接在浏览器打开或用于 API 调试。
令牌每次启动自动重新生成（不读盘复用），有效期 24 小时；重启即刷新。
删除数据目录下 web-access.json 不影响启动（下次启动重新生成并写入）。

示例：
  dbx url                                    # 完整访问链接
  dbx url --token-only                       # 仅输出 token
  curl -H "Authorization: Bearer $(dbx url --token-only)" http://127.0.0.1:8181/api/connections`,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		info, ok := svc.Persist().LoadWebAccess()
		if !ok {
			return fmt.Errorf("未找到 Web 访问凭证，请先启动 Web 服务（直接运行 dbx）")
		}
		tokenOnly, _ := cmd.Flags().GetBool("token-only")
		expireAt := time.UnixMilli(info.IssuedAt).Add(24 * time.Hour)
		if tokenOnly {
			if info.Token == "" {
				return fmt.Errorf("当前凭证无 token（上次以 --no-auth 启动）")
			}
			fmt.Println(info.Token)
			return nil
		}
		url := "http://" + info.Addr + "/"
		if info.Token != "" {
			url += "?token=" + info.Token
			if info.IssuedAt > 0 && time.Now().After(expireAt) {
				fmt.Println("⚠️  令牌已过期（签发给 24 小时有效期），请重启 Web 服务刷新后重新执行 dbx url")
			} else if info.IssuedAt > 0 {
				fmt.Printf("令牌有效期至 %s\n", expireAt.Format("2006-01-02 15:04:05"))
			}
		} else {
			fmt.Println("提示: 上次启动禁用了认证（--no-auth），链接不带 token")
		}
		fmt.Println(url)
		return nil
	},
}

func init() {
	urlCmd.Flags().Bool("token-only", false, "仅输出 token（便于脚本/API 调试）")
	rootCmd.AddCommand(urlCmd)
}
