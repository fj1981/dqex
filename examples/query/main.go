// query 演示元数据浏览 + SQL 执行 + 表分页查询。
//
//	export SRC_TYPE=mysql SRC_HOST=10.0.0.1 SRC_PORT=3306 SRC_USER=root SRC_PASSWORD=*** SRC_DBNAME=app
//	go run ./examples/query
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

	client, err := dqex.New(
		dqex.WithLang("zh"),
		dqex.WithInlineConns(dqex.ConnInfo{ID: "src", Name: "演示库", Conn: src}),
	)
	if err != nil {
		common.Fail(err)
	}
	defer client.Close()

	ctx := context.Background()
	db := src.DBName

	// 元数据分级接口：库列表 → 对象清单 → 表列信息
	dbs, err := client.Databases(ctx, "src", false)
	if err != nil {
		common.Fail(err)
	}
	fmt.Println("databases:", dbs)

	objs, err := client.Objects(ctx, "src", db, "", false)
	if err != nil {
		common.Fail(err)
	}
	fmt.Printf("objects in %s: %d tables\n", db, len(objs.Tables))

	// 执行 SQL 脚本（参数对象化，3.2）
	results, err := client.RunSQLScript(ctx, "src", db, "SELECT 1 AS one", dqex.ScriptParams{Limit: 100})
	if err != nil {
		common.Fail(err)
	}
	for _, r := range results {
		fmt.Println("columns:", r.Columns)
		for _, row := range r.Rows {
			fmt.Println("row:", row)
		}
	}
}
