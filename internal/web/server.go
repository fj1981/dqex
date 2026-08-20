// Package web Web 层：遵循 cygin 规范的服务启动 + 路由注册 + API handler。
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
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

// webAccessFileName vite dev 代理读取的令牌桥接文件名（位于数据根目录）。
// 仅服务于本地前端开发（Node 进程无法直接读 SQLite），非权威数据源。
const webAccessFileName = "web-access.json"

// writeWebAccessFile 将访问凭证写入数据目录下的 web-access.json（0600），
// 供本地 vite dev 代理读取令牌注入 /api 请求。失败仅告警，不影响启动。
func writeWebAccessFile(baseDir, addr, token string, issuedAt int64) {
	if baseDir == "" {
		return
	}
	info := map[string]any{"addr": addr, "token": token, "issuedAt": issuedAt}
	data, err := json.Marshal(info)
	if err != nil {
		cylog.Warnf("序列化 Web 访问凭证文件失败: %v", err)
		return
	}
	path := filepath.Join(baseDir, webAccessFileName)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		cylog.Warnf("写入 Web 访问凭证文件失败（不影响启动）: %v", err)
	}
}

// cyginMsg 按请求语言（?lang= 优先，其次 Accept-Language）取已注册错误码的消息；
// 注册表见 internal/service/errors.go，未注册码回退英文。
func cyginMsg(c *gin.Context, code int) string {
	if ce, ok := cygin.NewError(code).(*cygin.Error); ok {
		return ce.Msg(cygin.FromCtx(c))
	}
	return "unknown error"
}

// tokenAuth /api 路由的令牌认证中间件：
//   - 令牌超过 expireAt 过期：拒绝并提示重启服务刷新（令牌不自动续期）
//   - 接受 Authorization: Bearer <token>、X-Auth-Token 头或 ?token= 查询参数
//     （SSE EventSource 与文件下载无法自定义请求头，必须支持查询参数）
//   - OPTIONS 预检与 /api 之外的路由（页面/健康检查）放行
//   - 常量时间比较防时序探测；失败按真实 TCP 对端 IP 限速，防暴力破解
func tokenAuth(token string, expireAt time.Time, limiter *authLimiter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions || !strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.Next()
			return
		}
		ip := remoteIP(c).String()
		if !limiter.allow(ip) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"code": http.StatusTooManyRequests, "msg": cyginMsg(c, service.ErrRateLimited)})
			return
		}
		if time.Now().After(expireAt) {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": cyginMsg(c, service.ErrTokenExpired)})
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
			limiter.pass(ip)
			c.Next()
			return
		}
		limiter.fail(ip)
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": http.StatusUnauthorized, "msg": cyginMsg(c, service.ErrAuthFailed)})
	}
}

// remoteIP 取 TCP 对端 IP（不信任 X-Forwarded-For 等可伪造头，避免白名单/限速被绕过）
func remoteIP(c *gin.Context) net.IP {
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		return net.ParseIP(c.Request.RemoteAddr)
	}
	return net.ParseIP(host)
}

// accessControl 访问来源白名单中间件：仅放行白名单内的来源 IP（回环始终放行）
func accessControl(f *accessFilter) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := remoteIP(c)
		if f.allow(ip) {
			c.Next()
			return
		}
		cylog.Warnf("拒绝白名单外访问: %s %s（来源 %s）", c.Request.Method, c.Request.URL.Path, ip)
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": http.StatusForbidden, "msg": cyginMsg(c, service.ErrAccessDenied)})
			return
		}
		c.AbortWithStatus(http.StatusForbidden)
	}
}

// securityHeaders 安全响应头：
//   - Referrer-Policy: no-referrer 防令牌经 Referer 泄漏（令牌可出现在 ?token= 中）
//   - X-Content-Type-Options: nosniff 防 MIME 嗅探
//   - /api 响应 no-store，防浏览器/代理缓存留存敏感数据（SSE 由 handler 自行覆盖）
func securityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Content-Type-Options", "nosniff")
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		c.Next()
	}
}

// isLoopback 监听地址是否仅本机回环（空按默认回环处理）
func isLoopback(host string) bool {
	switch host {
	case "", "localhost", "127.0.0.1", "::1":
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
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

// RunWeb 启动 Web 服务。allow 为访问来源白名单（IP/CIDR/域名），空 = 不限制。
//
// 安全默认：
//   - host 默认 127.0.0.1 仅本机访问，需对外暴露时显式传 0.0.0.0 等
//   - 默认启用随机令牌认证（noAuth=true 关闭，且仅限监听回环地址）
//   - 前端与 API 同源部署，不开启 CORS 通配
func RunWeb(svc *service.Service, host string, port int, allow []string, noAuth, noBrowser bool) {
	if noAuth && !isLoopback(host) {
		cylog.Errorf("安全限制: --no-auth 仅允许监听本机回环地址；对外暴露（--host %s）必须启用令牌认证，请去掉 --no-auth 后重启", host)
		return
	}
	// 访问来源白名单过滤器：启动时由 --allow / 配置初始化，配置保存后可热更新（handleSaveConfig）
	filter := newAccessFilter(allow)
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
			eb.POST("/inspect", handleImportInspect()),
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
		// 数据字典
		eb.GROUP("/dictionary", []cygin.APIHandler{
			eb.POST("", handleDictionary(svc)),
		}),
		// 快照
		eb.GROUP("/snapshots", []cygin.APIHandler{
			eb.POST("", handleCreateSnapshot(svc)),
			eb.GET("", handleListSnapshots(svc)),
			eb.GET("/:id", handleGetSnapshot(svc)),
			eb.DELETE("/:id", handleDeleteSnapshot(svc)),
			eb.POST("/compare", handleSnapshotCompare(svc)),
			eb.GET("/compare/result", handleSnapshotCompareResult(svc)),
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
			eb.GET("/version", handleVersion()),
		}),
		// 全局配置
		eb.GROUP("/config", []cygin.APIHandler{
			eb.GET("", handleGetConfig(svc)),
			eb.PUT("", handleSaveConfig(svc, filter)),
			eb.GET("/browse-dirs", handleBrowseDirs(svc)),
		}),
		// AI 辅助 SQL
		eb.GROUP("/ai", []cygin.APIHandler{
			eb.GET("/status", handleAIStatus(svc)),
			eb.GET("/providers", handleAIProviders(svc)),
			eb.POST("/providers/save", handleAISaveProviders(svc)),
			eb.GET("/usage", handleAIProcessUsage(svc)),
			eb.POST("/sessions", handleAICreateSession(svc)),
			eb.GET("/sessions", handleAIListSessions(svc)),
			eb.DELETE("/sessions/by-tab", handleAIDeleteSessionByTab(svc)),
			eb.DELETE("/sessions/:id", handleAIDeleteSession(svc)),
			eb.POST("/sessions/:id/reset", handleAIResetSession(svc)),
			eb.GET("/sessions/:id/usage", handleAISessionUsage(svc)),
			eb.GET("/sessions/:id/history", handleAISessionHistory(svc)),
			eb.POST("/chat", handleAIChat(svc)),
		}),
		// SQL 查询终端
		eb.GROUP("/sql", []cygin.APIHandler{
			eb.POST("/run", handleSQLRun(svc)),
			eb.POST("/table", handleSQLTablePage(svc)),
			eb.POST("/cell", handleUpdateCell(svc)),
			eb.POST("/cell-value", handleCellValue(svc)),
			eb.POST("/delete-rows", handleDeleteRows(svc)),
			eb.POST("/insert-row", handleInsertRow(svc)),
			eb.POST("/generate", handleSQLGen(svc)),
			eb.GET("/history", handleSQLHistory(svc)),
			eb.DELETE("/history", handleClearSQLHistory(svc)),
			eb.GET("/favorites", handleListFavorites(svc)),
			eb.POST("/favorites", handleAddFavorite(svc)),
			eb.DELETE("/favorites", handleDeleteFavorite(svc)),
			eb.PATCH("/favorites", handleRenameFavorite(svc)),
			eb.GET("/audit", handleSQLAudit(svc)),
			eb.POST("/ping", handleSQLPing(svc)),
			eb.GET("/ddl", handleSQLDDL(svc)),
			eb.GET("/workspace", handleGetWorkspace(svc)),
			eb.PUT("/workspace", handleSaveWorkspace(svc)),
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
	// 始终挂载访问控制中间件：空规则放行，配置保存后热更新（无需重启）
	// langCtx 将请求语言注入 ctx，service 同步方法在真实出错点按语言渲染 details
	middlewares := []gin.HandlerFunc{langCtx(), securityHeaders(), accessControl(filter)}
	if len(filter.rules) > 0 {
		cylog.Infof("访问来源白名单已启用: %d 条规则（本机回环始终放行，保存配置后热更新）", len(filter.rules))
	}
	token := ""
	issuedAt := time.Now()
	if !noAuth {
		// 每次启动总是重新生成令牌（不读盘复用），重启即刷新，避免长期使用同一令牌
		var terr error
		token, terr = genToken()
		if terr != nil {
			cylog.Errorf("生成访问令牌失败，拒绝启动: %v", terr)
			return
		}
		middlewares = append(middlewares, tokenAuth(token, issuedAt.Add(tokenTTL), newAuthLimiter()))
	}
	opts = append(opts, cygin.WithGlobalMiddlewares(middlewares...))

	server := cygin.NewServer(opts...)
	// 绑定地址（WithPort 仅设置端口且默认全网卡，此处显式覆盖为 host:port）
	server.Config.Address = net.JoinHostPort(host, strconv.Itoa(port))
	// 访问凭证落盘：dbx url 可随时取回当前访问链接（含签发时间供过期判断）
	if err := svc.Persist().SaveWebAccess(service.WebAccessInfo{Addr: server.Config.Address, Token: token, IssuedAt: issuedAt.UnixMilli()}); err != nil {
		cylog.Warnf("保存 Web 访问凭证失败（不影响启动）: %v", err)
	}
	// 令牌桥接文件：本地开发（vite dev 代理）读取 web-access.json 注入 /api 请求头，
	// 数据源仍以 SQLite 为准（dbx url 走 SQLite），此文件仅为 vite 的令牌快照
	writeWebAccessFile(svc.Persist().BaseDir(), server.Config.Address, token, issuedAt.UnixMilli())
	// 浏览器访问地址：通配地址（0.0.0.0/::）无法直接访问，回退为本机回环
	browserHost := host
	if browserHost == "" || browserHost == "0.0.0.0" || browserHost == "::" {
		browserHost = "127.0.0.1"
	}
	openURL := "http://" + net.JoinHostPort(browserHost, strconv.Itoa(port)) + "/"
	if !isLoopback(host) {
		cylog.Warnf("服务已对外暴露（监听 %s）：请确保仅面向可信网络，必要时用 --allow / 配置 web.allow 收紧来源", server.Config.Address)
	}
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
