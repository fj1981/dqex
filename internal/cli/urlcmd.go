package cli

import (
	"fmt"
	"net"
	"time"

	"github.com/spf13/cobra"
)

// localLANIP 探测本机局域网 IPv4 地址（非回环），用于通配监听（0.0.0.0/::）时
// 生成可被局域网其他设备直接访问的链接；探测失败返回 nil。
func localLANIP() net.IP {
	// 优先取默认路由出口地址：UDP 无连接，Dial 仅本地选路、不实际发包，
	// 不依赖外网可达（纯局域网也能正常返回本机出口 IP）。
	// 8.8.8.8 仅作为触发默认路由的占位目标，不会真正发送数据。
	if conn, err := net.Dial("udp", "8.8.8.8:80"); err == nil {
		addr, ok := conn.LocalAddr().(*net.UDPAddr)
		conn.Close()
		if ok && addr.IP != nil && !addr.IP.IsLoopback() && addr.IP.To4() != nil {
			return addr.IP
		}
	}
	// 兜底：遍历网卡取第一个非回环 IPv4
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok {
			ip := ipnet.IP
			if ip != nil && !ip.IsLoopback() && ip.To4() != nil {
				return ip
			}
		}
	}
	return nil
}

// resolveAddr 解析监听地址，通配地址（0.0.0.0/::）替换为本机局域网 IP 或回退 127.0.0.1
func resolveAddr(addr string) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr // 解析失败，原样返回
	}
	if host == "0.0.0.0" || host == "::" || host == "" {
		if ip := localLANIP(); ip != nil {
			host = ip.String()
		} else {
			host = "127.0.0.1"
		}
	}
	return net.JoinHostPort(host, port)
}

var urlCmd = &cobra.Command{
	Use:   "url",
	Short: "输出 Web 访问链接（带 token）",
	Long: `输出当前数据目录下的 Web 访问链接（带 token），可直接在浏览器打开或用于 API 调试。
令牌每次启动自动重新生成（不读盘复用），有效期 24 小时；重启即刷新。
删除数据目录下的存储库 dqex.db 不影响启动（下次启动重新生成并写入）。

示例：
  dqex url                                    # 完整访问链接
  dqex url --token-only                       # 仅输出 token
  curl -H "Authorization: Bearer $(dqex url --token-only)" http://127.0.0.1:8181/api/connections`,
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		info, ok := svc.Persist().LoadWebAccess()
		if !ok {
			return textErr(nil, cliTextsFor(cliLang()).errNoWebCred)
		}
		tokenOnly, _ := cmd.Flags().GetBool("token-only")
		expireAt := time.UnixMilli(info.IssuedAt).Add(24 * time.Hour)
		if tokenOnly {
			if info.Token == "" {
				return textErr(nil, cliTextsFor(cliLang()).errNoToken)
			}
			fmt.Println(info.Token)
			return nil
		}
		url := "http://" + resolveAddr(info.Addr) + "/"
		txt := cliTextsFor(cliLang())
		if info.Token != "" {
			url += "?token=" + info.Token
			if info.IssuedAt > 0 && time.Now().After(expireAt) {
				fmt.Println(txt.urlTokenExpired)
			} else if info.IssuedAt > 0 {
				printf(txt.urlExpireAt+"\n", expireAt.Format("2006-01-02 15:04:05"))
			}
		} else {
			fmt.Println(txt.urlNoAuth)
		}
		fmt.Println(url)
		return nil
	},
}

func init() {
	urlCmd.Flags().Bool("token-only", false, "仅输出 token（便于脚本/API 调试）")
	rootCmd.AddCommand(urlCmd)
}
