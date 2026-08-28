// export 演示导出任务：结构 + 数据导出为 zip 产物，RunExport 返回 ArtifactRef。
//
//	export SRC_TYPE=mysql SRC_HOST=10.0.0.1 SRC_PORT=3306 SRC_USER=root SRC_PASSWORD=*** SRC_DBNAME=app
//	go run ./examples/export -out /tmp/dqex-exports
package main

import (
	"context"
	"flag"
	"fmt"

	dqex "github.com/fj1981/dqex"
	"github.com/fj1981/dqex/examples/common"
)

func main() {
	out := flag.String("out", "", "产物输出目录（StoreNone 库模式下必填，否则返回 ErrExpOutDir）")
	flag.Parse()

	src, err := common.Conn("SRC")
	if err != nil {
		common.Fail(err)
	}

	client, err := dqex.New(
		dqex.WithLang("zh"),
		dqex.WithInlineConns(dqex.ConnInfo{ID: "src", Name: "源库", Conn: src}),
	)
	if err != nil {
		common.Fail(err)
	}
	defer client.Close()

	ref, err := client.RunExport(context.Background(), dqex.ExportOptions{
		SourceConn: "src",
		OutputDir:  *out, // StoreNone 且未 WithDataDir 时必须显式指定（产物落位规则，3.2）
		TaskName:   "demo-export",
	}, func(p dqex.ProgressInfo) {
		fmt.Printf("[%d/%d] %s\n", p.DoneUnits, p.TotalUnits, p.Message)
	})
	if err != nil {
		common.Fail(err)
	}
	fmt.Printf("导出完成: storage=%s ref=%s size=%d\n", ref.Storage, ref.Ref, ref.Size)
}
