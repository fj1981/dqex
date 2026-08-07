package service

import (
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cygin"
)

// 业务错误码（cygin 系统码 1001~1012 之外，本项目从 2001 起）
const (
	ErrUnsupportedType  = 2002 // 不支持的数据库类型
	ErrConnNotFound     = 2001 // 连接配置不存在
	ErrConnNotSpecified = 2008 // 未指定数据库连接
	ErrConnFailed       = 2009 // 数据库连接/操作失败
	ErrTaskNotFound     = 2003 // 任务不存在
	ErrTaskInvalid      = 2010 // 任务配置无效（缺少选项/未知类型）
	ErrExecFailed       = 2004 // 执行操作失败（导出/导入/迁移/获取表等）
	ErrFileType         = 2005 // 不支持的文件类型
	ErrNoArtifact       = 2006 // 任务没有可下载产物
	ErrOpenDirFailed    = 2007 // 打开目录失败
	ErrCryptoFailed     = 2011 // 配置文件加解密失败
	ErrHistoryRunning   = 2012 // 任务运行中，无法删除历史记录
)

func init() {
	cygin.RegisterMessages(map[int]map[string]string{
		ErrConnNotFound:     {"zh": "连接配置不存在", "en": "Connection not found"},
		ErrConnNotSpecified: {"zh": "未指定数据库连接", "en": "Database connection not specified"},
		ErrUnsupportedType:  {"zh": "不支持的数据库类型", "en": "Unsupported database type"},
		ErrConnFailed:       {"zh": "数据库连接失败", "en": "Database connection failed"},
		ErrTaskNotFound:     {"zh": "任务不存在", "en": "Task not found"},
		ErrTaskInvalid:      {"zh": "任务配置无效", "en": "Invalid task configuration"},
		ErrExecFailed:       {"zh": "执行操作失败", "en": "Operation failed"},
		ErrFileType:         {"zh": "不支持的文件类型", "en": "Unsupported file type"},
		ErrNoArtifact:       {"zh": "任务没有可下载产物", "en": "No artifact for task"},
		ErrOpenDirFailed:    {"zh": "打开目录失败", "en": "Failed to open directory"},
		ErrCryptoFailed:     {"zh": "配置文件加解密失败", "en": "Failed to encrypt/decrypt configuration"},
		ErrHistoryRunning:   {"zh": "任务运行中，无法删除记录", "en": "Task is running, cannot delete record"},
	})
}

// 错误处理统一使用 cygin 方式：在出错位置直接调用（不做二次包装，
// 保证 WithErrPrint 打印的日志定位到真实出错文件行号）：
//
//	cygin.NewError(code, cygin.WithErrPrint(), cygin.WithErrDetailf(...))
//	cygin.WrapError(err, code, cygin.WithErrPrint(), cygin.WithErrDetails(err.Error()))
//
// 返回的 *cygin.Error 在 Web 模式由 cygin.Handle 统一以 {code, msg, success:false} 响应，
// CLI 模式直接输出。
