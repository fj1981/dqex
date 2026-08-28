package service

// 业务层结构化错误：与 engine.MsgError 同机制（Key/Args/Cause + 按语言渲染），
// 额外携带 cygin 错误码（Code），供 Web 层重建 cygin.Error 时保留 HTTP/业务语义。
// Error() 以 zh 渲染兜底（历史记录/日志兼容）；Msg(lang) 按语言渲染供展示层使用。

import (
	"context"

	"errors"
	"fmt"

	"github.com/fj1981/dqex/internal/engine"
)

// SvcError 结构化业务错误
type SvcError struct {
	Code  int    // cygin 错误码（0 表示由包装方决定，如 cyginWrapAI）
	Key   string // 错误注册表模板 key
	Args  []any  // 模板参数
	Cause error  // 底层原因（原 ": %w" 包装），可为 nil
}

func (e *SvcError) Error() string { return renderSvcErr("zh", e) }

func (e *SvcError) Msg(lang string) string { return renderSvcErr(lang, e) }

func (e *SvcError) Unwrap() error { return e.Cause }

func renderSvcErr(lang string, e *SvcError) string {
	tpl := svcErrFor(lang)[e.Key]
	if tpl == "" {
		tpl = e.Key // 注册表缺失时回退 key 本身，避免空串误导
	}
	s := sprintf(tpl, e.Args...)
	if e.Cause != nil {
		s += ": " + svcCauseText(lang, e.Cause)
	}
	return s
}

// svcCauseText 渲染底层原因：cause 同为 SvcError/MsgError 时按同一语言递归渲染（嵌套错误链双语），
// 其余（驱动/系统错误）保持原样作为诊断细节。
func svcCauseText(lang string, err error) string {
	if me := engine.AsMsgErr(err); me != nil {
		return me.Msg(lang)
	}
	if se := AsSvcErr(err); se != nil {
		return se.Msg(lang)
	}
	return err.Error()
}

// ---- 请求语言上下文（ctx 注入） ----
// 同步方法无 lang 参数，由入口（web 中间件 / cli 调用点）把请求语言注入 ctx，
// 出错点在真实位置直接构造 cygin 错误时用 langFrom 取语言渲染 details（不封装、行号不失真）。

type ctxLangKey struct{}

// WithLang 将语言注入 ctx
func WithLang(ctx context.Context, lang string) context.Context {
	return context.WithValue(ctx, ctxLangKey{}, lang)
}

// langFrom 从 ctx 取请求语言，未注入时回退 zh（与 engine 兜底口径一致）
func langFrom(ctx context.Context) string {
	if l, ok := ctx.Value(ctxLangKey{}).(string); ok && l != "" {
		return normLang(l)
	}
	return "zh"
}

// newSvcErr 创建无底层原因的结构化业务错误
func newSvcErr(code int, key string, args ...any) error {
	return &SvcError{Code: code, Key: key, Args: args}
}

// newSvcErrf 创建带底层原因的结构化业务错误（等价原 fmt.Errorf("模板: %w", cause)）
func newSvcErrf(code int, cause error, key string, args ...any) error {
	return &SvcError{Code: code, Key: key, Args: args, Cause: cause}
}

// AsSvcErr 提取错误链中的 *SvcError（无则返回 nil）
func AsSvcErr(err error) *SvcError {
	var se *SvcError
	if errors.As(err, &se) {
		return se
	}
	return nil
}

// sprintf 包装：vet 不对自定义包装做格式串常量检查，供动态模板渲染使用
func sprintf(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}

// ---- 业务错误文本注册表 ----
// key 命名：svc + 模块缩写 + 语义（如 svcSnapNotFound / svcAISessionNotFound）。
// 新增语言只加条目；结构必须与 zh 完全对齐（缺失时 svcErrFor 回退 zh）。

// svcErrMap 语言 → (key → 模板)
var svcErrMap = map[string]map[string]string{
	"zh": {
		// 收藏（sql.go）
		svcFavConnID:     "connId 必填",
		svcFavSQLEmpty:   "SQL 不可为空",
		svcFavSQLTooLong: "SQL 过长（≤64KB）",
		svcFavIDEmpty:    "id 必填",
		svcFavTitleEmpty: "标题不可为空",
		// 快照（snapshot.go）
		svcSnapNoDBName:    "第 %d 个库未指定数据库名",
		svcSnapEmptyDB:     "库 %s 内没有表（或仅含视图），无法创建快照",
		svcSnapAllEmpty:    "所选 %d 个库均为空（无表或仅含视图），无法创建快照",
		svcSnapNotFound:    "快照不存在: %s",
		svcSnapCmpNotFound: "快照对比结果不存在: %s",
		// AI 链路（ai.go）
		svcAISchemaFail:      "获取表结构失败",
		svcAICancelled:       "任务已取消",
		svcAINoTargetDB:      "未找到可用数据库",
		svcAINoTables:        "目标库 %s 中没有可用的表，请检查连接配置的数据库名，或确认该库下存在表",
		svcAINotConfigured:   "AI 功能未配置：请先在设置中填写 BaseURL / API Key / Model",
		svcAIEmptyPrompt:     "请输入需求描述",
		svcAIEmptyResponse:   "模型返回空结果",
		svcAIToolListDBs:     "初始化 AI 查询工具失败",
		svcAIToolListTables:  "初始化 AI 查询工具失败",
		svcAIToolGetSchema:   "初始化 AI 查询工具失败",
		svcAISessionNotFound: "会话不存在: %s",
		svcAISessionExpired:  "会话不存在或已过期，请重新创建",
	},
	"en": {
		// 收藏（sql.go）
		svcFavConnID:     "connId is required",
		svcFavSQLEmpty:   "SQL must not be empty",
		svcFavSQLTooLong: "SQL is too long (≤64KB)",
		svcFavIDEmpty:    "id is required",
		svcFavTitleEmpty: "title must not be empty",
		// 快照（snapshot.go）
		svcSnapNoDBName:    "database %d: name is not specified",
		svcSnapEmptyDB:     "database %s has no tables (or only views); cannot create snapshot",
		svcSnapAllEmpty:    "all selected %d database(s) are empty (no tables or only views); cannot create snapshot",
		svcSnapNotFound:    "snapshot not found: %s",
		svcSnapCmpNotFound: "snapshot compare result not found: %s",
		// AI 链路（ai.go）
		svcAISchemaFail:      "failed to fetch table structure",
		svcAICancelled:       "task cancelled",
		svcAINoTargetDB:      "no available database found",
		svcAINoTables:        "no usable tables in target database %s; check the database name in the connection config, or confirm the database contains tables",
		svcAINotConfigured:   "AI feature is not configured: fill in BaseURL / API Key / Model in Settings first",
		svcAIEmptyPrompt:     "please describe what you need",
		svcAIEmptyResponse:   "the model returned an empty result",
		svcAIToolListDBs:     "failed to initialize AI query tools",
		svcAIToolListTables:  "failed to initialize AI query tools",
		svcAIToolGetSchema:   "failed to initialize AI query tools",
		svcAISessionNotFound: "session not found: %s",
		svcAISessionExpired:  "session not found or expired, please create a new one",
	},
}

func svcErrFor(lang string) map[string]string {
	if m, ok := svcErrMap[normLang(lang)]; ok {
		return m
	}
	return svcErrMap["zh"]
}

// normLang 语言归一：en 前缀 → en，其余回退 zh（与 engine/llm 口径一致）
func normLang(lang string) string {
	if len(lang) >= 2 && lang[:2] == "en" {
		return "en"
	}
	return "zh"
}

// ---- 同步方法 details 静态文案注册表 ----
// 同步方法无 lang 参数，由入口（web langCtx 中间件 / cli WithLang 调用点）注入 ctx，
// 出错点在真实位置直接构造 cygin 错误时用 svcTextsFor(langFrom(ctx)) 取双语模板。

type svcTexts struct {
	errConnNameEmpty       string // 连接名称校验
	errConnShortName       string
	errConnShortNameDup    string
	errConnTypeEmpty       string // 连接类型校验
	errConnTypeUnsupported string
	errCfgRead             string // 全局配置读写
	errCfgParse            string
	errCfgHome             string
	errCfgMkdir            string
	errCfgSerialize        string
	errCfgWrite            string
	errCfgHotReload        string
	errDirRead             string // 目录浏览
}

var svcTextsZh = svcTexts{
	errConnNameEmpty:       "连接名称不能为空或包含空格/控制字符",
	errConnShortName:       "短名: 仅允许字母、数字、连字符和下划线，长度 1-32",
	errConnShortNameDup:    "短名已存在: %s",
	errConnTypeEmpty:       "数据库类型不能为空",
	errConnTypeUnsupported: "不支持的数据库类型: %s",
	errCfgRead:             "读取全局配置失败: %s",
	errCfgParse:            "解析全局配置失败: %s",
	errCfgHome:             "无法定位用户主目录",
	errCfgMkdir:            "创建配置目录失败",
	errCfgSerialize:        "序列化全局配置失败",
	errCfgWrite:            "写入全局配置失败",
	errCfgHotReload:        "目录热更新失败",
	errDirRead:             "读取目录失败: %s",
}

var svcTextsEn = svcTexts{
	errConnNameEmpty:       "connection name cannot be empty or contain spaces/control characters",
	errConnShortName:       "short name: only letters, digits, hyphens and underscores, 1-32 chars",
	errConnShortNameDup:    "short name already exists: %s",
	errConnTypeEmpty:       "database type cannot be empty",
	errConnTypeUnsupported: "unsupported database type: %s",
	errCfgRead:             "failed to read global config: %s",
	errCfgParse:            "failed to parse global config: %s",
	errCfgHome:             "cannot locate user home directory",
	errCfgMkdir:            "failed to create config directory",
	errCfgSerialize:        "failed to serialize global config",
	errCfgWrite:            "failed to write global config",
	errCfgHotReload:        "failed to hot-reload directories",
	errDirRead:             "failed to read directory: %s",
}

func svcTextsFor(lang string) svcTexts {
	if normLang(lang) == "en" {
		return svcTextsEn
	}
	return svcTextsZh
}

// ---- 错误 key 常量（编译期防拼写错误） ----
const (
	// 收藏
	svcFavConnID     = "svcFavConnID"
	svcFavSQLEmpty   = "svcFavSQLEmpty"
	svcFavSQLTooLong = "svcFavSQLTooLong"
	svcFavIDEmpty    = "svcFavIDEmpty"
	svcFavTitleEmpty = "svcFavTitleEmpty"
	// 快照
	svcSnapNoDBName    = "svcSnapNoDBName"
	svcSnapEmptyDB     = "svcSnapEmptyDB"
	svcSnapAllEmpty    = "svcSnapAllEmpty"
	svcSnapNotFound    = "svcSnapNotFound"
	svcSnapCmpNotFound = "svcSnapCmpNotFound"
	// AI 链路
	svcAISchemaFail      = "svcAISchemaFail"
	svcAICancelled       = "svcAICancelled"
	svcAINoTargetDB      = "svcAINoTargetDB"
	svcAINoTables        = "svcAINoTables"
	svcAINotConfigured   = "svcAINotConfigured"
	svcAIEmptyPrompt     = "svcAIEmptyPrompt"
	svcAIEmptyResponse   = "svcAIEmptyResponse"
	svcAIToolListDBs     = "svcAIToolListDBs"
	svcAIToolListTables  = "svcAIToolListTables"
	svcAIToolGetSchema   = "svcAIToolGetSchema"
	svcAISessionNotFound = "svcAISessionNotFound"
	svcAISessionExpired  = "svcAISessionExpired"
)
