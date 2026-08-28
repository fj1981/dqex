// compare 演示库对结构 + 数据对比，结果按库分组返回。
//
//	export SRC_TYPE=mysql SRC_HOST=10.0.0.1 SRC_PORT=3306 SRC_USER=root SRC_PASSWORD=*** SRC_DBNAME=app
//	export TGT_TYPE=mysql TGT_HOST=10.0.0.2 TGT_PORT=3306 TGT_USER=root TGT_PASSWORD=*** TGT_DBNAME=app
//	go run ./examples/compare
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

	result, err := client.RunCompare(context.Background(), dqex.CompareOptions{
		SourceConn: "src",
		TargetConn: "tgt",
		Databases: []dqex.CompareDBPair{
			{SourceDB: src.DBName, TargetDB: tgt.DBName},
		},
	}, func(p dqex.ProgressInfo) {
		fmt.Printf("[%d/%d] %s\n", p.DoneUnits, p.TotalUnits, p.Message)
	})
	if err != nil {
		common.Fail(err)
	}
	sm := result.Summary
	fmt.Printf("对比完成: 共%d项, 一致%d, 结构差异%d, 数据差异%d\n",
		sm.Total, sm.Matched, sm.StructureDiff, sm.DataDiff)
	for _, db := range result.Databases {
		for _, t := range db.Tables {
			if t.Status != "both" {
				fmt.Printf("  %s.%s: %s\n", db.SourceDB, t.Name, t.Status)
			}
		}
	}
}
