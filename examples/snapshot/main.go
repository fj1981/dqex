// snapshot 演示创建快照（StoreNone 下仅内存返回）→ 落盘 JSON → 离线读回 → 与目标库对比。
//
//	export SRC_TYPE=mysql SRC_HOST=10.0.0.1 SRC_PORT=3306 SRC_USER=root SRC_PASSWORD=*** SRC_DBNAME=app
//	export TGT_TYPE=mysql TGT_HOST=10.0.0.2 TGT_PORT=3306 TGT_USER=root TGT_PASSWORD=*** TGT_DBNAME=app
//	go run ./examples/snapshot -file /tmp/dqex-snapshot.json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	dqex "github.com/fj1981/dqex"
	"github.com/fj1981/dqex/examples/common"
)

func main() {
	file := flag.String("file", "", "快照落盘路径（演示 StoreNone 下调用方自行持久化 *Snapshot）")
	flag.Parse()

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

	ctx := context.Background()

	// 1. 创建快照（参数对象化；StoreNone 下仅内存返回，不落盘）
	snap, err := client.CreateSnapshot(ctx, "src", []string{src.DBName},
		dqex.SnapshotParams{Name: "demo", Description: "安装前基线"}, nil)
	if err != nil {
		common.Fail(err)
	}
	fmt.Printf("快照完成: %s 表 %d 张, 共 %d 行\n", snap.ID, snap.TableCount, snap.TotalRows)

	// 2. 调用方自行持久化（StoreNone 语义），LoadSnapshot 可离线读回
	if *file != "" {
		data, err := json.MarshalIndent(snap, "", "  ")
		if err != nil {
			common.Fail(err)
		}
		if err := os.WriteFile(*file, data, 0o644); err != nil {
			common.Fail(err)
		}
		loaded, err := client.LoadSnapshot(*file)
		if err != nil {
			common.Fail(err)
		}
		snap = loaded
		fmt.Println("快照已落盘并读回:", *file)
	}

	// 3. 与目标库对比（target 用完整连接信息：对比目标常为临时环境，未必已注册）
	result, err := client.CompareSnapshot(ctx, snap, &tgt, dqex.SnapshotCompareOptions{}, nil)
	if err != nil {
		common.Fail(err)
	}
	sm := result.Summary
	fmt.Printf("快照对比: 共%d项, 一致%d, 结构差异%d, 数据差异%d\n",
		sm.Total, sm.Matched, sm.StructureDiff, sm.DataDiff)
}
