// webmount 演示宿主 gin engine 同进程挂载 dqex（形态 B，docs/library-api-design.md 6.5.1）：
// 库 API 与 Web UI 同进程共享同一 Service，宿主页面同源 iframe 嵌入。
//
//	go run ./examples/webmount -addr :8080
//	# 浏览器打开 http://localhost:8080/dqex/
//	# 嵌入视图：http://localhost:8080/dqex/?embed=1#/embed/query
package main

import (
	"flag"
	"log"

	"github.com/gin-gonic/gin"

	dqex "github.com/fj1981/dqex"
	"github.com/fj1981/dqex/dqexweb"
	"github.com/fj1981/dqex/examples/common"
)

func main() {
	addr := flag.String("addr", ":8080", "监听地址")
	flag.Parse()

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

	r := gin.Default()
	// 宿主登录态中间件挂这里（鉴权外置）：r.Use(hostAuth)
	dqexweb.Mount(r, client, dqexweb.MountOptions{
		Prefix: "/dqex",
		// 允许跨域 iframe 的宿主 origin（同源部署可留空；生产建议显式配置）
		// FrameAncestors: []string{"https://host.example"},
	})
	log.Println("dqex 已挂载: http://localhost" + *addr + "/dqex/ （嵌入视图 /dqex/?embed=1#/embed/query）")
	if err := r.Run(*addr); err != nil {
		log.Fatal(err)
	}
}
