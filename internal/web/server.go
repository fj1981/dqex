// Package web Web 层：遵循 cygin 规范的服务启动 + 路由注册 + API handler。
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	"dbimpex/internal/service"
	webui "dbimpex/web"

	"github.com/gin-gonic/gin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cylog"
)

func nowMillis() int64 { return time.Now().UnixMilli() }

// genToken 生成随机访问令牌（32 字符 URL 安全 base64）
func genToken() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// tokenTTL 访问令牌有效期：到期后 API 拒绝访问并提示重启服务刷新令牌
const tokenTTL = 24 * time.Hour

// tokenAuth /api 路由的令牌认证中间件：
//   - 令牌超过 expireAt 过期：拒绝并提示重启服务刷新（令牌不自动续期）
//   - 接受 Authorization: Bearer <token>、X-Auth-Token 头或 ?token= 查询参数
//     （SSE EventSource 与文件下载无法自定义请求头，必须支持查询参数）
//   - OPTIONS 预检与 /api 之外的路由（页面/健康检查）放行
//   - 常量时间比较防时序探测
func tokenAuth(token string, expireAt time.Time) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}
		if time.Now().After(expireAt) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": "访问令牌已过期（有效期 24 小时），请重启 dbx 服务刷新令牌后重新访问"})
			return
		}
		got := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if got == "" {
			got = c.GetHeader("X-Auth-Token")
		}
		if got == "" {
			got = c.Query("token")
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) == 1 {
			c.Next()
			return
		}
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": "认证失败：缺少访问令牌或令牌无效"})
	}
}

// openBrowser 打开系统默认浏览器（失败静默忽略，如远程/无头环境）
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// RunWeb 启动 Web 服务。
//
// 安全默认：
//   - host 默认 127.0.0.1 仅本机访问，需对外暴露时显式传 0.0.0.0 等
//   - 默认启用随机令牌认证（noAuth=true 关闭，仅限可信环境）
//   - 前端与 API 同源部署，不开启 CORS 通配
func RunWeb(svc *service.Service, host string, port int, noAuth, noBrowser bool) {
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
		cygin.WithHealthCheck(),
		cygin.AddApiGroup(apiGroup),
		// SSE 进度推送 / 文件下载 / 打开目录（需要直接控制响应，走原生 gin 路由）
		cygin.AddRouteGroup("/api", rawRoutes(svc)),
		// 前端资源：直接内嵌 web/dist（构建前需先 npm run build）
		cygin.WithEmbeddedFiles("/", webui.DistFS, "dist"),
	}
	token := ""
	issuedAt := time.Now()
	if !noAuth {
		// 未过期的已持久化令牌可复用；已过期（或首次启动）则重新生成——重启即刷新
		if info, ok := svc.Persist().LoadWebAccess(); ok && info.Token != "" &&
			info.IssuedAt > 0 && time.Since(time.UnixMilli(info.IssuedAt)) < tokenTTL {
			token = info.Token
			issuedAt = time.UnixMilli(info.IssuedAt)
		} else {
			var terr error
			token, terr = genToken()
			if terr != nil {
				cylog.Errorf("生成访问令牌失败，拒绝启动: %v", terr)
				return
			}
		}
		opts = append(opts, cygin.WithGlobalMiddlewares(tokenAuth(token, issuedAt.Add(tokenTTL))))
	}

	server := cygin.NewServer(opts...)
	// 绑定地址（WithPort 仅设置端口且默认全网卡，此处显式覆盖为 host:port）
	server.Config.Address = net.JoinHostPort(host, strconv.Itoa(port))
	// 访问凭证落盘：dbx url 可随时取回当前访问链接（含签发时间供过期判断）
	if err := svc.Persist().SaveWebAccess(service.WebAccessInfo{Addr: server.Config.Address, Token: token, IssuedAt: issuedAt.UnixMilli()}); err != nil {
		cylog.Warnf("保存 Web 访问凭证失败（不影响启动）: %v", err)
	}
	// 浏览器访问地址：通配地址（0.0.0.0/::）无法直接访问，回退为本机回环
	browserHost := host
	if browserHost == "" || browserHost == "0.0.0.0" || browserHost == "::" {
		browserHost = "127.0.0.1"
	}
	openURL := "http://" + net.JoinHostPort(browserHost, strconv.Itoa(port)) + "/"
	if token != "" {
		openURL += "?token=" + token
		cylog.Infof("dbx Web 服务启动: %s", openURL)
		cylog.Infof("令牌有效期至 %s（过期后请重启服务刷新）", issuedAt.Add(tokenTTL).Format("2006-01-02 15:04:05"))
		cylog.Infof("API 认证: 请求头 Authorization: Bearer <token> / X-Auth-Token，或查询参数 ?token=")
	} else {
		cylog.Warnf("dbx Web 服务启动: %s（已禁用认证 --no-auth，请勿暴露到不可信网络）", openURL)
	}
	if !noBrowser {
		// 延迟至监听就绪后自动打开浏览器（携带 token，前端会存入 sessionStorage 供后续请求使用）
		go func() {
			time.Sleep(500 * time.Millisecond)
			openBrowser(openURL)
		}()
	}
	_ = server.Run(context.Background())
}
