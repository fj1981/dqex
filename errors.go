package dqex

import (
	"context"

	"github.com/fj1981/dqex/internal/service"
	"github.com/fj1981/infrakit/pkg/cygin"
)

// 错误体系是契约（3.3）：库使用者按错误码分支处理，而不是字符串匹配。
// 错误码常量 re-export 自 internal/service（cygin 系统码 1001~1012 之外，业务码 2001 起）。

// SvcError 结构化业务错误（Code/Key/Args/Cause + 按语言渲染）。
// Error() 以 zh 渲染兜底；Msg(lang) 按语言渲染。
type SvcError = service.SvcError

// AsSvcErr 提取错误链中的 *SvcError（无则返回 false，target 不变）。
//
//	var se *dqex.SvcError
//	if dqex.AsSvcErr(err, &se) { fmt.Println(se.Code) }
func AsSvcErr(err error, target **SvcError) bool {
	if se := service.AsSvcErr(err); se != nil {
		*target = se
		return true
	}
	return false
}

// 错误码常量（与 internal/service 一一对应）
const (
	// ErrParamsInvalid 参数无效（cygin 系统码）
	ErrParamsInvalid = cygin.ErrParamsInvalid
	// ErrInternalServer 内部错误（cygin 系统码）
	ErrInternalServer = cygin.ErrInternalServer

	// ErrConnNotFound 连接配置不存在（key 不在任何解析来源中）
	ErrConnNotFound = service.ErrConnNotFound
	// ErrConnNotSpecified 未指定数据库连接
	ErrConnNotSpecified = service.ErrConnNotSpecified
	// ErrUnsupportedType 不支持的数据库类型
	ErrUnsupportedType = service.ErrUnsupportedType
	// ErrConnFailed 数据库连接/操作失败
	ErrConnFailed = service.ErrConnFailed
	// ErrTaskNotFound 任务/记录不存在
	ErrTaskNotFound = service.ErrTaskNotFound
	// ErrTaskInvalid 任务配置无效
	ErrTaskInvalid = service.ErrTaskInvalid
	// ErrExecFailed 执行操作失败（导出/导入/迁移/获取元数据等）
	ErrExecFailed = service.ErrExecFailed
	// ErrFileType 不支持的文件类型
	ErrFileType = service.ErrFileType
	// ErrNoArtifact 任务没有可下载产物
	ErrNoArtifact = service.ErrNoArtifact
	// ErrCryptoFailed 配置文件加解密失败
	ErrCryptoFailed = service.ErrCryptoFailed

	// ErrExpOutDir 未显式指定产物输出目录（StoreNone 库模式且无 DataDir 时，RunExport/RunDictionary 必须显式指定 OutputDir）
	ErrExpOutDir = service.ErrExpOutDir
	// ErrClientClosed 客户端已关闭（Close 后调用能力方法）
	ErrClientClosed = service.ErrClientClosed
	// ErrNotImplemented 触发式能力尚未实现（WithStoreConn/WithCacheRedis/WithArtifactStore 等，随 4.4 触发落地）
	ErrNotImplemented = service.ErrNotImplemented
	// ErrStoreUnavailable 当前模式无持久化存储，该能力不可用
	ErrStoreUnavailable = service.ErrStoreUnavailable
)

// WithLangCtx 将请求语言注入 ctx（错误消息语言，zh/en）。
// 通常无需手动调用：New(WithLang(...)) 后 Client 所有方法自动注入。
func WithLangCtx(ctx context.Context, lang string) context.Context {
	return service.WithLang(ctx, lang)
}
