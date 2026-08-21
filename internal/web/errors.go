package web

// Web 层错误渲染出口：handler 返回的错误统一经 renderErr 按请求语言渲染，
// 保证 engine.MsgError / service.SvcError 在 en 请求下不再泄漏中文（前端优先展示 details）。

import (
	"errors"

	"dqex/internal/engine"
	"dqex/internal/service"

	"github.com/gin-gonic/gin"
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

// renderErr 按请求语言渲染错误：
//   - engine.MsgError → 纯文本错误（Msg(lang) 渲染后由 cygin 中间件拆入 details）；
//   - service.SvcError → 重建 cygin.Error（保留业务错误码，detail 按语言渲染）；
//   - 其他类型（含 *cygin.Error）原样透传，不改变既有行为。
func renderErr(c *gin.Context, err error) error {
	lang := cygin.FromCtx(c)
	if me := engine.AsMsgErr(err); me != nil {
		return errors.New(me.Msg(lang))
	}
	if se := service.AsSvcErr(err); se != nil {
		code := se.Code
		if code == 0 {
			code = cygin.ErrInternalServer
		}
		return cygin.NewError(code, cygin.WithErrPrint(), cygin.WithErrDetailf("%s", se.Msg(lang)))
	}
	return err
}

// ---- web 层错误文案注册表（少量原生 gin 路由的细节文案，按请求语言渲染） ----

type webTexts struct {
	errNoArtifactDownload string
	errNoArtifactPath     string
	errConnNotFound       string
	errFileTypeOnly       string
	errFileTooLarge       string
	errSaveUploadFail     string
	errMissingTaskID      string
}

var webTextsZh = webTexts{
	errNoArtifactDownload: "任务没有可下载的产物: %s",
	errNoArtifactPath:     "任务没有产物路径: %s",
	errConnNotFound:       "未找到连接: %s",
	errFileTypeOnly:       "仅支持 .sql / .sql.gz / .zip 文件: %s",
	errFileTooLarge:       "文件超过 2GB 上传大小限制: %s",
	errSaveUploadFail:     "保存上传文件失败",
	errMissingTaskID:      "缺少任务 ID",
}

var webTextsEn = webTexts{
	errNoArtifactDownload: "Task has no downloadable artifact: %s",
	errNoArtifactPath:     "Task has no artifact path: %s",
	errConnNotFound:       "connection not found: %s",
	errFileTypeOnly:       "only .sql / .sql.gz / .zip files are supported: %s",
	errFileTooLarge:       "file exceeds the 2GB upload size limit: %s",
	errSaveUploadFail:     "failed to save uploaded file",
	errMissingTaskID:      "missing task ID",
}

// webTextsFor 按语言取文案，未知语言回退 zh
func webTextsFor(lang string) webTexts {
	if len(lang) >= 2 && lang[:2] == "en" {
		return webTextsEn
	}
	return webTextsZh
}

// renderErrMsg 按请求语言将错误渲染为纯文本（SSE 等非 cygin 响应场景），
// 避免 MsgError/SvcError 的 zh 兜底文案在 en 请求下泄漏。
func renderErrMsg(c *gin.Context, err error) string {
	lang := cygin.FromCtx(c)
	if me := engine.AsMsgErr(err); me != nil {
		return me.Msg(lang)
	}
	if se := service.AsSvcErr(err); se != nil {
		return se.Msg(lang)
	}
	return err.Error()
}

// langCtx 请求语言中间件：将 cygin.FromCtx(c) 注入 request ctx，
// service 同步方法经 langFrom(ctx) 在真实出错点按语言渲染 details（不封装、行号不失真）。
func langCtx() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request = c.Request.WithContext(service.WithLang(c.Request.Context(), cygin.FromCtx(c)))
		c.Next()
	}
}
