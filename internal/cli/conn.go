package cli

// 点导入：CLI 层大量复用 service 包的模型别名与入口（NewService/选项模型/错误码）
import (
	. "dbimpex/internal/service"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

var connAddFlags connFlags

var connCmd = &cobra.Command{
	Use:     "conn",
	Aliases: []string{"cn"},
	Short:   "数据库连接管理（list/add/test/delete）",
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

func init() {
	// add：连接参数复用公共 flags（type/host/port/un/pw/db/subtype）
	registerConnFlags(connAddCmd, "", &connAddFlags)
	connAddCmd.Flags().StringP("name", "n", "", "连接名称")
	connAddCmd.Flags().String("id", "", "按 ID 更新已有连接")
	_ = connAddCmd.MarkFlagRequired("name")
	_ = connAddCmd.RegisterFlagCompletionFunc("type", fixedCompletion("mysql", "postgresql", "oracle"))

	connTestCmd.Flags().StringP("conn", "c", "", "已保存连接（ID 或名称）")
	connDeleteCmd.Flags().StringP("conn", "c", "", "已保存连接（ID 或名称）")
	_ = connTestCmd.MarkFlagRequired("conn")
	_ = connDeleteCmd.MarkFlagRequired("conn")
	_ = connTestCmd.RegisterFlagCompletionFunc("conn", completeConnNames)
	_ = connDeleteCmd.RegisterFlagCompletionFunc("conn", completeConnNames)

	connCmd.AddCommand(connListCmd, connAddCmd, connTestCmd, connDeleteCmd)
	rootCmd.AddCommand(connCmd)
}

var connListCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls"},
	Short:   "列出已保存连接",
	RunE: func(cmd *cobra.Command, args []string) error {
		svc, err := newCliService()
		if err != nil {
			return err
		}
		conns := svc.ListConnections()
		if len(conns) == 0 {
			fmt.Println("（无已保存连接）")
			return nil
		}
		fmt.Printf("%-26s %-16s %-10s %s\n", "ID", "名称", "类型", "地址")
		for _, c := range conns {
			addr := c.Conn.Host
			if c.Conn.Port > 0 {
				addr = fmt.Sprintf("%s:%d", c.Conn.Host, c.Conn.Port)
			}
			if c.Conn.DBName != "" {
				addr += "/" + c.Conn.DBName
			}
			typ := c.Conn.Type
			if c.Conn.SubType != "" && !strings.EqualFold(c.Conn.SubType, c.Conn.Type) {
				typ += " " + c.Conn.SubType
			}
			fmt.Printf("%-26s %-16s %-10s %s\n", c.ID, c.Name, typ, addr)
		}
		return nil
	},
}

var connAddCmd = &cobra.Command{
	Use:   "add",
	Short: "新增或更新连接配置",
	RunE: func(cmd *cobra.Command, args []string) error {
		conn := connAddFlags.toConn()
		if conn == nil {
			return cygin.NewError(cygin.ErrParamsInvalid, cygin.WithErrPrint(), cygin.WithErrDetailf("缺少 --type（mysql/postgresql/oracle）"))
		}
		name, _ := cmd.Flags().GetString("name")
		id, _ := cmd.Flags().GetString("id")
		svc, err := newCliService()
		if err != nil {
			return err
		}
		saved, err := svc.AddConnection(ConnRecord{ID: id, Name: name, Conn: *conn})
		if err != nil {
			return err
		}
		fmt.Printf("连接已保存: %s (%s)\n", saved.ID, saved.Name)
		return nil
	},
}

var connTestCmd = &cobra.Command{
	Use:   "test",
	Short: "测试连接可用性",
	RunE: func(cmd *cobra.Command, args []string) error {
		key, _ := cmd.Flags().GetString("conn")
		svc, err := newCliService()
		if err != nil {
			return err
		}
		rec, ok := svc.Persist().GetConn(key)
		if !ok {
			return cygin.NewError(ErrConnNotFound, cygin.WithErrPrint(), cygin.WithErrDetailf("未找到连接: %s", key))
		}
		fmt.Printf("测试连接 %s ... ", rec.Name)
		if err := svc.TestConnection(rec.Conn); err != nil {
			fmt.Println("失败")
			return err
		}
		fmt.Println("成功")
		return nil
	},
}

var connDeleteCmd = &cobra.Command{
	Use:     "delete",
	Aliases: []string{"del"},
	Short:   "删除连接配置",
	RunE: func(cmd *cobra.Command, args []string) error {
		key, _ := cmd.Flags().GetString("conn")
		svc, err := newCliService()
		if err != nil {
			return err
		}
		if err := svc.DeleteConnection(key); err != nil {
			return err
		}
		fmt.Println("连接已删除")
		return nil
	},
}
