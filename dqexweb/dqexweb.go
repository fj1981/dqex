// Package dqexweb 提供 dqex Web UI + API 向宿主 gin engine 的挂载能力
// （docs/library-api-design.md 6.5.1 主形态 B：宿主 Go 程序同进程内嵌，同源 iframe）。
//
// 库 API（github.com/fj1981/dqex）与 Web UI 同进程共享同一个 Service 实例：
// 库调用与页面操作看到同一份状态，连接经 ConnProvider/WithInlineConns 由宿主持有。
//
// 基本用法：
//
//	r := gin.New()
//	r.Use(hostAuthMiddleware) // 宿主登录态（鉴权外置，作用于 /dqex 子树即可）
//	client, _ := dqex.New(dqex.WithConnProvider(...))
//	dqexweb.Mount(r, client, dqexweb.MountOptions{
//	    Prefix:         "/dqex",
//	    FrameAncestors: []string{"https://host.example"}, // 允许宿主页面 iframe 嵌入
//	})
//	r.Run(":8080")
//	// 同源 iframe 嵌入（无 token/CORS/SameSite 问题）：
//	// <iframe src="/dqex/#/query"></iframe>
//	// 精简嵌入视图：<iframe src="/dqex/?embed=1#/embed/query"></iframe>
package dqexweb

import (
	"github.com/fj1981/dqex"
	"github.com/fj1981/dqex/internal/web"
	"github.com/gin-gonic/gin"
)

// MountOptions 挂载选项（Prefix 默认 "/dqex"；FrameAncestors CSP 白名单；
// Fallback 宿主自有 SPA/404 回退）。
type MountOptions = web.MountOptions

// Mount 将 dqex 的 API 与前端 UI 挂载到宿主 gin engine：
//   - API 挂在 Prefix+"/api"（与独立部署同一套端点，含 SSE 进度/文件下载）
//   - 前端静态资源挂在 Prefix+"/"（SPA 回退经 NoRoute，不与宿主已有路由冲突）
//
// 鉴权外置：本函数不注册令牌认证与来源白名单，宿主在 engine 层统一接入登录态。
// StoreNone 库模式下依赖持久化的端点（历史/任务配置/收藏等）返回 ErrStoreUnavailable。
func Mount(r *gin.Engine, c *dqex.Client, opts MountOptions) {
	web.Mount(r, c.Service(), opts)
}
