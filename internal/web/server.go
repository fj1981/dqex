// Package web Web 层：遵循 cygin 规范的服务启动 + 路由注册 + API handler。
package web

import (
	"context"
	"time"

	"dbimpex/internal/service"
	webui "dbimpex/web"

	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

func nowMillis() int64 { return time.Now().UnixMilli() }

// RunWeb 启动 Web 服务
func RunWeb(svc *service.Service, port int) {
	eb := cygin.NewEndpointBuilder("/api", "dbx API", []string{"dbx"})
	apiGroup := eb.Build(
		// 连接管理
		eb.GROUP("/connections", []cygin.APIHandler{
			eb.POST("", handleCreateConn(svc)),
			eb.GET("", handleListConns(svc)),
			eb.DELETE("/:id", handleDeleteConn(svc)),
			eb.POST("/test", handleTestConn(svc)),
			eb.GET("/:id/tables", handleGetTables(svc)),
			eb.GET("/:id/columns", handleGetTableColumns(svc)),
		}),
		// 导出
		eb.GROUP("/export", []cygin.APIHandler{
			eb.POST("", handleExport(svc)),
		}),
		// 导入
		eb.GROUP("/import", []cygin.APIHandler{
			eb.POST("", handleImport(svc)),
			eb.POST("/upload", handleImportUpload(svc)),
			eb.POST("/inspect", handleImportInspect(svc)),
		}),
		// 迁移
		eb.GROUP("/migrate", []cygin.APIHandler{
			eb.POST("", handleMigrate(svc)),
		}),
		// 对比
		eb.GROUP("/compare", []cygin.APIHandler{
			eb.POST("", handleCompare(svc)),
			eb.GET("/result", handleCompareResult(svc)),
		}),
		// 任务配置（避免与 :id 通配路由冲突，全部使用非通配路径）
		eb.GROUP("/tasks", []cygin.APIHandler{
			eb.GET("", handleListTasks(svc)),
			eb.POST("", handleSaveTask(svc)),
			eb.GET("/detail", handleGetTask(svc)),
			eb.PUT("/update", handleUpdateTask(svc)),
			eb.DELETE("", handleDeleteTask(svc)),
			eb.GET("/last/:type", handleGetLastTask(svc)),
			eb.POST("/run", handleRunTask(svc)),
		}),
		// 执行历史
		eb.GROUP("/history", []cygin.APIHandler{
			eb.GET("", handleListHistory(svc)),
			eb.GET("/:taskID", handleGetHistory(svc)),
		}),
		// 元数据
		eb.GROUP("/meta", []cygin.APIHandler{
			eb.GET("/dbtypes", handleDBTypes()),
		}),
	)

	opts := []cygin.ServerOption{
		cygin.WithPort(port),
		cygin.WithCORS("*"),
		cygin.WithHealthCheck(),
		cygin.AddApiGroup(apiGroup),
		// SSE 进度推送 / 文件下载 / 打开目录（需要直接控制响应，走原生 gin 路由）
		cygin.AddRouteGroup("/api", rawRoutes(svc)),
		// 前端资源：直接内嵌 web/dist（构建前需先 npm run build）
		cygin.WithEmbeddedFiles("/", webui.DistFS, "dist"),
	}

	server := cygin.NewServer(opts...)
	cylog.Infof("dbx Web 服务启动: http://localhost:%d", port)
	_ = server.Run(context.Background())
}
