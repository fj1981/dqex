// migrate 演示安装工具场景的最小形态：进程内完成一次结构 + 数据迁移。
//
//	export SRC_TYPE=mysql SRC_HOST=10.0.0.1 SRC_PORT=3306 SRC_USER=root SRC_PASSWORD=*** SRC_DBNAME=app
//	export TGT_TYPE=mysql TGT_HOST=10.0.0.2 TGT_PORT=3306 TGT_USER=root TGT_PASSWORD=*** TGT_DBNAME=app_new
//	go run ./examples/migrate
package main

import (
	"context"
	"fmt"

	dqex "github.com/fj1981/dqex"
	"github.com/fj1981/dqex/examples/common"
)

func main() {
	src, err := common.Conn("SRC")
	if err != nil {
		common.Fail(err)
	}
	tgt, err := common.Conn("TGT")
	if err != nil {
		common.Fail(err)
	}

	client, err := dqex.New(
		dqex.WithLang("zh"),
		dqex.WithInlineConns(
			dqex.ConnInfo{ID: "src", Name: "源库", Conn: src},
			dqex.ConnInfo{ID: "tgt", Name: "目标库", Conn: tgt},
		),
	)
	if err != nil {
		common.Fail(err)
	}
	defer client.Close()

	// 同步 API：ctx 控制取消，ProgressFunc 回调进度（回调内阻塞会暂停任务，重活自行转 goroutine）
	err = client.RunMigrate(context.Background(), dqex.MigrateOptions{
		SourceConn: "src",
		TargetConn: "tgt",
		// Tables:    []string{"users", "orders"}, // nil = 全部表
	}, func(p dqex.ProgressInfo) {
		fmt.Printf("[%d/%d] %s\n", p.DoneUnits, p.TotalUnits, p.Message)
	})
	if err != nil {
		common.Fail(err)
	}
	fmt.Println("迁移完成")
}
