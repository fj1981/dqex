package service

import (
	"github.com/fj1981/infrakit/pkg/cygin"
)

// 业务错误码（cygin 系统码 1001~1012 之外，本项目从 2001 起）
const (
	ErrUnsupportedType   = 2002 // 不支持的数据库类型
	ErrConnNotFound      = 2001 // 连接配置不存在
	ErrConnNotSpecified  = 2008 // 未指定数据库连接
	ErrConnFailed        = 2009 // 数据库连接/操作失败
	ErrTaskNotFound      = 2003 // 任务不存在
	ErrTaskInvalid       = 2010 // 任务配置无效（缺少选项/未知类型）
	ErrExecFailed        = 2004 // 执行操作失败（导出/导入/迁移/获取表等）
	ErrFileType          = 2005 // 不支持的文件类型
	ErrNoArtifact        = 2006 // 任务没有可下载产物
	ErrOpenDirFailed     = 2007 // 打开目录失败
	ErrCryptoFailed      = 2011 // 配置文件加解密失败
	ErrHistoryRunning    = 2012 // 任务运行中，无法删除历史记录
	ErrTokenExpired      = 2013 // Web 访问令牌过期
	ErrAuthFailed        = 2014 // 认证失败（缺少令牌或令牌无效）
	ErrRateLimited       = 2015 // 认证失败过于频繁被临时锁定
	ErrAccessDenied      = 2016 // 访问来源不在白名单
	ErrStreamUnsupported = 2017 // 环境不支持流式输出
	ErrServiceException  = 2018 // 服务异常
	ErrAITimeout         = 2019 // AI 生成超时
	ErrAINotConfigured   = 2020 // AI 功能未配置
	ErrAISessionNotFound = 2021 // AI 会话不存在或已过期
	ErrAIEmptyPrompt     = 2022 // AI 需求描述为空
	ErrAINoTargetDB      = 2023 // 未找到可用数据库
	ErrAINoTables        = 2024 // 目标库没有可用的表
	ErrAISchemaFailed    = 2025 // 获取表结构失败
	ErrAIEmptyResponse   = 2026 // 模型未返回有效结果（空结果或仅思考过程）
	ErrExpOutDir         = 2027 // 库模式（StoreNone 且无 DataDir）下未显式指定产物输出目录
	ErrClientClosed      = 2028 // 客户端已关闭（Close 后调用能力方法）
	ErrNotImplemented    = 2029 // 触发式能力尚未实现（WithStoreConn/WithCacheRedis/WithArtifactStore 等）
	ErrStoreUnavailable  = 2030 // 当前模式无持久化存储，该能力不可用（SQL 历史/任务配置/收藏等）
)

func init() {
	cygin.RegisterMessages(map[int]map[string]string{
		ErrConnNotFound:      {"zh": "连接配置不存在", "en": "Connection not found"},
		ErrConnNotSpecified:  {"zh": "未指定数据库连接", "en": "Database connection not specified"},
		ErrUnsupportedType:   {"zh": "不支持的数据库类型", "en": "Unsupported database type"},
		ErrConnFailed:        {"zh": "数据库连接失败", "en": "Database connection failed"},
		ErrTaskNotFound:      {"zh": "任务不存在", "en": "Task not found"},
		ErrTaskInvalid:       {"zh": "任务配置无效", "en": "Invalid task configuration"},
		ErrExecFailed:        {"zh": "执行操作失败", "en": "Operation failed"},
		ErrFileType:          {"zh": "不支持的文件类型", "en": "Unsupported file type"},
		ErrNoArtifact:        {"zh": "任务没有可下载产物", "en": "No artifact for task"},
		ErrOpenDirFailed:     {"zh": "打开目录失败", "en": "Failed to open directory"},
		ErrCryptoFailed:      {"zh": "配置文件加解密失败", "en": "Failed to encrypt/decrypt configuration"},
		ErrHistoryRunning:    {"zh": "任务运行中，无法删除记录", "en": "Task is running, cannot delete record"},
		ErrTokenExpired:      {"zh": "访问令牌已过期（有效期 24 小时），请重启 dqex 服务刷新令牌后重新访问", "en": "Access token expired (valid for 24h); restart dqex to refresh the token"},
		ErrAuthFailed:        {"zh": "认证失败：缺少访问令牌或令牌无效", "en": "Authentication failed: access token missing or invalid"},
		ErrRateLimited:       {"zh": "认证失败过于频繁，来源已临时锁定，请稍后重试", "en": "Too many failed attempts; source temporarily locked, retry later"},
		ErrAccessDenied:      {"zh": "访问被拒绝：当前来源不在允许访问的 IP/域名白名单内", "en": "Access denied: source not in the allowed IP/domain whitelist"},
		ErrStreamUnsupported: {"zh": "当前环境不支持流式输出", "en": "Streaming output is not supported in this environment"},
		ErrServiceException:  {"zh": "服务异常，请重试", "en": "Service error, please retry"},
		ErrAITimeout:         {"zh": "生成超时，请检查模型服务是否可用，或调大 ai.timeout_sec 配置", "en": "Generation timed out; check the model service or increase ai.timeout_sec"},
		ErrAINotConfigured:   {"zh": "AI 功能未配置：请先在设置中填写 BaseURL / API Key / Model", "en": "AI is not configured: fill in BaseURL / API Key / Model in settings first"},
		ErrAISessionNotFound: {"zh": "会话不存在或已过期，请重新创建", "en": "Session not found or expired, please create a new one"},
		ErrAIEmptyPrompt:     {"zh": "请输入需求描述", "en": "Please enter a request description"},
		ErrAINoTargetDB:      {"zh": "未找到可用数据库", "en": "No available database found"},
		ErrAINoTables:        {"zh": "目标库中没有可用的表，请检查连接配置的数据库名，或确认该库下存在表", "en": "No usable tables in the target database; check the configured database name or confirm the database has tables"},
		ErrAISchemaFailed:    {"zh": "获取表结构失败", "en": "Failed to fetch table structure"},
		ErrAIEmptyResponse:   {"zh": "模型未返回有效结果，请重试", "en": "Model returned no valid result, please retry"},
		ErrExpOutDir:         {"zh": "未指定产物输出目录：当前为无持久化的库模式，请在任务选项中显式指定 OutputDir", "en": "Output dir is required: current mode has no persistence, specify OutputDir in task options explicitly"},
		ErrClientClosed:      {"zh": "客户端已关闭", "en": "Client is closed"},
		ErrNotImplemented:    {"zh": "该能力尚未实现（触发式，将随具体场景落地）", "en": "Feature not implemented yet (trigger-based, lands with a concrete scenario)"},
		ErrStoreUnavailable:  {"zh": "当前为无持久化的库模式，该能力不可用", "en": "Feature unavailable: current mode has no persistent store"},
	})
}

// 错误处理统一使用 cygin 方式：在出错位置直接调用（不做二次包装，
// 保证 WithErrPrint 打印的日志定位到真实出错文件行号）：
//
//	cygin.NewError(code, cygin.WithErrPrint(), cygin.WithErrDetailf(...))
//	cygin.WrapError(err, code, cygin.WithErrPrint())
//
// WrapError 会自动把 err 的消息拼入 details，无需再传 WithErrDetails(err.Error())。
// 返回的 *cygin.Error 在 Web 模式由 cygin.Handle 统一以 {code, msg, success:false} 响应，
// CLI 模式直接输出。
