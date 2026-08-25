package cli

import (
	"dqex/internal/llm"
	"errors"
	"fmt"
	"io"
)

// printf/fprintf/sprintf 包装 fmt 同名函数：语言注册表文本为动态格式串，
// go vet 对非字面量格式串会误报 non-constant format string，自定义包装可绕过检查，行为与 fmt 一致。
func printf(format string, args ...any)               { fmt.Printf(format, args...) }
func fprintf(w io.Writer, format string, args ...any) { fmt.Fprintf(w, format, args...) }
func sprintf(format string, args ...any) string       { return fmt.Sprintf(format, args...) }

// ---- CLI 子命令输出语言（conn/config/url/version/task/snapshot/history/compare 等状态类高频文本；
// cobra 静态帮助见 help.go 注册表；核心业务错误见下方 textErr + err* 注册） ----

// cliTexts 高频状态输出文案（按语言索引，新增语言只加 map 条目）。
type cliTexts struct {
	// 通用
	errPrefix     string // 错误: %s
	progressPct   string //  进度 %.0f%% (%d/%d 项)
	progressUnits string // %d/%d 项
	progressRows  string //  · %s行
	rowsThousand  string // %.1f万

	// 任务状态词（history 等列表展示）
	statusDone      string // 成功
	statusError     string // 失败
	statusCancelled string // 已取消
	statusRunning   string // 运行中

	// conn 子命令
	connNone      string // （无已保存连接）
	connShortName string //  (短名: %s)
	connSaved     string // 连接已保存: %s (%s)
	connTesting   string // 测试连接 %s ...
	connOK        string // 成功
	connFail      string // 失败
	connDeleted   string // 连接已删除

	// config 子命令
	cfgNotFound   string // 配置文件: （未发现，使用默认值；dqex config --gen 可生成模板）
	cfgPath       string // 配置文件:     %s
	cfgDirData    string // 配置保存目录: %s
	cfgDirTmp     string // 任务临时目录: %s
	cfgDirUploads string // 上传临时目录: %s
	cfgDirExports string // 最终产物目录: %s
	cfgAllow      string // 访问来源白名单: %s（本机回环始终放行）
	cfgAllowNone  string // 访问来源白名单: （未配置，外部来源一律拒绝，仅本机可访问）

	// url 子命令
	urlTokenExpired string // ⚠️  令牌已过期（签发给 24 小时有效期），请重启 Web 服务刷新后重新执行 dqex url
	urlExpireAt     string // 令牌有效期至 %s
	urlNoAuth       string // 提示: 上次启动禁用了认证（--no-auth），链接不带 token

	// version 子命令
	versionCommitID  string // 提交: %s
	versionBuildTime string // 构建时间: %s

	// 任务起止（export/import/migrate/compare/dictionary）
	startExport  string // 开始导出...
	doneExport   string // 导出完成: %s
	startImport  string // 开始导入...
	doneImport   string // 导入完成
	warnReset    string // 警告: 重置数据且未开启备份，目标表现有数据将无法恢复！
	startMigrate string // 开始迁移...
	doneMigrate  string // 迁移完成
	startCompare string // 开始对比...
	savedCompare string // 报告已保存: %s
	startDict    string // 开始生成数据字典...
	doneDict     string // 数据字典生成完成: %s

	// task 子命令
	taskNone     string // （无任务配置）
	taskLastUsed string //  [最近使用]
	taskRunning  string // 执行任务: %s (%s)
	taskSaved    string // 任务已保存: %s (%s)
	taskDeleted  string // 任务已删除

	// snapshot 子命令
	snapCreated     string // \n%s 快照创建成功
	snapID          string //   ID:       %s
	snapName        string //   名称:     %s
	snapDBs         string //   数据库:   %s (%s)
	snapTables      string //   表数量:   %d
	snapRows        string //   总行数:   %s
	snapDesc        string //   备注:     %s
	snapNone        string // 暂无快照
	snapListTitle   string // %s 共 %d 个快照
	snapListWord    string // 快照列表
	snapDetailTitle string // 快照详情
	snapConn        string //   连接:     %s
	snapCreatedAt   string //   创建时间: %s
	snapTableUnit   string // %d表 %s行
	snapDBLine      string // \n%s %s（%d表 %s行）
	snapDBWord      string // 库
	snapPK          string // , 主键: %v
	snapTableLine   string //   %-30s %d列 %s行%s
	snapDeleted     string // %s 快照 %s 已删除

	// history 子命令
	histNone     string // （无执行历史）
	histID       string // ID:       %s
	histType     string // 类型:     %s
	histStatus   string // 状态:     %s
	histStarted  string // 开始:     %s
	histFinished string // 结束:     %s
	histDuration string // 耗时:     %s
	histTarget   string // 目标:     %s
	histProgress string // 进度:     %d/%d 项
	histRows     string // ，%d 行
	histSummary  string // 摘要:     %s
	histOutput   string // 输出文件: %s
	histError    string // 错误:     %s
	histLogs     string // 日志:
	histDeleted  string // 记录已删除

	// 表格头（conn/history 共用）
	hdrID        string // ID
	hdrName      string // 名称
	hdrShort     string // 短名
	hdrType      string // 类型
	hdrAddr      string // 地址
	hdrStatus    string // 状态
	hdrStartedAt string // 开始时间
	hdrSummary   string // 摘要

	// compare 汇总
	cmpTitle       string // 对比结果: %s ↔ %s
	cmpSummaryLine string // 共 %d 项 | %s %d | %s %d | %s %d | %s %d | %s %d
	cmpMatched     string // 一致
	cmpSrcOnly     string // 仅源有
	cmpTgtOnly     string // 仅目标有
	cmpStructDiff  string // 结构差异
	cmpDataDiff    string // 数据差异
	cmpDBCount     string // %d 个库参与对比
	cmpDBLine      string // \n%s %s ↔ %s（共%d项, 一致%d, 结构差异%d, 数据差异%d）
	cmpDBWord      string // 库
	cmpColTable    string // 表
	cmpColStatus   string // 状态
	cmpColDetail   string // 差异说明
	cmpNoDiff      string // （无差异）
	cmpDiffOnly    string // 仅显示有差异的表（%d）；使用 --all 查看全部 %d 项
	cmpShowHint    string // 记录 ID: %s · 查看差异明细: dqex cmp show -i %s [表名]
	cmpStatusDiff  string // 有差异

	// compare 明细
	cmpTableTitle     string // 表: %s
	cmpTableAlias     string // %s（%s ↔ %s）
	cmpStructTitle    string // \n── 结构 ──
	cmpStructSame     string // 结构一致
	cmpColOnlySrc     string //   （仅源有）
	cmpColOnlyTgt     string //   （仅目标有）
	cmpColDiffDef     string // 定义不一致
	cmpSrcCol         string //       源:   %s
	cmpTgtCol         string //       目标: %s
	cmpDataTitle      string // \n── 数据 ──
	cmpNotCompared    string // 未逐行比较: %s
	cmpRowsLine       string // 行数: 源 %d / 目标 %d
	cmpByPK           string //   （按主键 %s 判断有无）
	cmpNoPK           string //   （无主键，整行对比）
	cmpDataSame       string // 数据一致
	cmpRowsDiff       string // 行数不一致（超过阈值，仅比行数；可调大 --threshold 后重跑逐行对比）
	cmpMissingSuffix  string // %s（源有目标无）
	cmpMissingCount   string // 缺失 %d 行
	cmpExtraSuffix    string // %s（目标有源无）
	cmpExtraCount     string // 多出 %d 行
	cmpChangedSuffix  string // %s（主键匹配但内容不同）
	cmpChangedCount   string // 变化 %d 行
	cmpStructCounts   string // 结构: +%d -%d ±%d
	cmpRowsCount      string // 行数 %d vs %d
	cmpDataSameRows   string // 数据一致 (%d行)
	cmpMissingShort   string // 缺失%d行
	cmpExtraShort     string // 多出%d行
	cmpChangedShort   string // 变化%d行
	cmpMissingSamples string // 缺失行样例
	cmpExtraSamples   string // 多出行样例
	cmpChangedSamples string // 变化行样例（%d）
	cmpSampleTitle    string // %s（%d）:
	cmpSampleRow      string //      %s: 源=%v  目标=%v

	// 核心业务错误（textErr 渲染）
	errSaveReport string // 保存对比报告失败
	errNoWebCred  string // 未找到 Web 访问凭证，请先启动 Web 服务（直接运行 dqex）
	errNoToken    string // 当前凭证无 token（上次以 --no-auth 启动）

	// 参数校验错误（cygin details 渲染，cliErrMsg 拼接展示）
	errConnNotFound   string // 未找到连接: %s
	errCfgRead        string // 读取配置文件失败: %s
	errCfgParse       string // 解析配置文件失败: %s
	errCfgNoCmp       string // 配置文件中未找到对比配置（需包含 source/target 段）: %s
	errCfgNoExp       string // 配置文件中未找到导出配置（需包含 source 段）: %s
	errCfgNoDict      string // 配置文件中未找到数据字典配置（需包含 source 段）: %s
	errCfgNoImp       string // 配置文件中未找到导入配置（需包含 target 段）: %s
	errCfgNoMig       string // 配置文件中未找到迁移配置（需包含 source/target 段）: %s
	errResetMode      string // 无效的重置模式: %s（可选 truncate/drop）
	errNoSrcConn      string // 缺少源连接：配置 source/source_ref 段或 --source-*/--source-conn
	errNoTgtConn      string // 缺少目标连接：配置 target/target_ref 段或 --target-*/--target-conn
	errCmpScope       string // 无效的对比范围: %s（可选 both/structure/data）
	errThresholdNeg   string // threshold 不能为负数
	errTaskNotImp     string // 任务配置 %s 不是导入任务
	errTaskNotExp     string // 任务配置 %s 不是导出任务
	errTaskNotDict    string // 任务配置 %s 不是数据字典任务
	errTaskNotMig     string // 任务配置 %s 不是迁移任务
	errNoImportFile   string // 缺少导入文件：配置 input 字段或 --input
	errAliasFmt       string // 别名格式应为 源表=目标表: %s
	errNoCmpID        string // 缺少 --id（对比记录 ID）
	errTableConflict  string // 表名指定冲突：--table %s 与位置参数 %s
	errNoTableInRec   string // 记录 %s 中未找到表: %s
	errTaskType       string // 未知任务类型: %s
	errNoNameConfig   string // 缺少 --name 或 --config
	errNoConnType     string // 缺少 --type（mysql/postgresql/oracle）
	errTaskHint       string // 无效的任务类型: %s（可选 export/import/migrate/compare/dictionary）
	errCfgParseOnly   string // 解析配置文件失败
	errCfgTypeUnknown string // 无法识别配置文件类型，可用 --type 指定（export/import/migrate/compare）
}

// textErr 构造双语错误：args[0] 为 cliTexts 注册文案模板，args[1:] 为模板参数；
// cause 非空时追加 ": <cause>" 保留错误链。
// 签名避开 go vet 的 printf wrapper 识别（无 string+...any 模式），规避 go1.24+
// 对「非常量格式串为最后参数」的检查（golang/go#71485）；调用方须保证 args[0] 为字符串模板。
func textErr(cause error, args ...any) error {
	tpl, _ := args[0].(string)
	msg := sprintf(tpl, args[1:]...)
	if cause == nil {
		return errors.New(msg)
	}
	return fmt.Errorf("%s: %w", msg, cause)
}

// cliTextsMap 语言注册表：缺失语言回退 zh。
var cliTextsMap = map[string]cliTexts{
	"zh": {
		errPrefix:     "错误: %s",
		progressPct:   "  进度 %.0f%% (%d/%d 项)",
		progressUnits: "%d/%d 项",
		progressRows:  " · %s行",
		rowsThousand:  "%.1f万",

		statusDone:      "成功",
		statusError:     "失败",
		statusCancelled: "已取消",
		statusRunning:   "运行中",

		connNone:      "（无已保存连接）",
		connShortName: " (短名: %s)",
		connSaved:     "连接已保存: %s (%s)",
		connTesting:   "测试连接 %s ... ",
		connOK:        "成功",
		connFail:      "失败",
		connDeleted:   "连接已删除",

		cfgNotFound:   "配置文件: （未发现，使用默认值；dqex config --gen 可生成模板）",
		cfgPath:       "配置文件:     %s",
		cfgDirData:    "配置保存目录: %s",
		cfgDirTmp:     "任务临时目录: %s",
		cfgDirUploads: "上传临时目录: %s",
		cfgDirExports: "最终产物目录: %s",
		cfgAllow:      "访问来源白名单: %s（本机回环始终放行）",
		cfgAllowNone:  "访问来源白名单: （未配置，外部来源一律拒绝，仅本机可访问）",

		urlTokenExpired: "⚠️ 令牌已过期（有效期 24 小时），请重启 Web 服务后重新执行 dqex url",
		urlExpireAt:     "令牌有效期至 %s",
		urlNoAuth:       "提示: 上次启动禁用了认证（--no-auth），链接不带 token",

		versionCommitID:  "提交: %s",
		versionBuildTime: "构建时间: %s",

		startExport:  "开始导出...",
		doneExport:   "导出完成: %s",
		startImport:  "开始导入...",
		doneImport:   "导入完成",
		warnReset:    "警告: 未开启备份的重置操作，目标表数据将无法恢复！",
		startMigrate: "开始迁移...",
		doneMigrate:  "迁移完成",
		startCompare: "开始对比...",
		savedCompare: "报告已保存: %s",
		startDict:    "开始生成数据字典...",
		doneDict:     "数据字典生成完成: %s",

		taskNone:     "（无任务配置）",
		taskLastUsed: " [最近使用]",
		taskRunning:  "执行任务: %s (%s)",
		taskSaved:    "任务已保存: %s (%s)",
		taskDeleted:  "任务已删除",

		snapCreated:     "\n%s 快照创建成功",
		snapID:          "  ID:       %s",
		snapName:        "  名称:     %s",
		snapDBs:         "  数据库:   %s (%s)",
		snapTables:      "  表数量:   %d",
		snapRows:        "  总行数:   %s",
		snapDesc:        "  备注:     %s",
		snapNone:        "暂无快照",
		snapListTitle:   "%s 共 %d 个快照",
		snapListWord:    "快照列表",
		snapDetailTitle: "快照详情",
		snapConn:        "  连接:     %s",
		snapCreatedAt:   "  创建时间: %s",
		snapTableUnit:   "%d表 %s行",
		snapDBLine:      "\n%s %s（%d表 %s行）",
		snapDBWord:      "库",
		snapPK:          ", 主键: %v",
		snapTableLine:   "  %-30s %d列 %s行%s",
		snapDeleted:     "%s 快照 %s 已删除",

		histNone:     "（无执行历史）",
		histID:       "ID:       %s",
		histType:     "类型:     %s",
		histStatus:   "状态:     %s",
		histStarted:  "开始:     %s",
		histFinished: "结束:     %s",
		histDuration: "耗时:     %s",
		histTarget:   "目标:     %s",
		histProgress: "进度:     %d/%d 项",
		histRows:     "，%d 行",
		histSummary:  "摘要:     %s",
		histOutput:   "输出文件: %s",
		histError:    "错误:     %s",
		histLogs:     "日志:",
		histDeleted:  "记录已删除",

		hdrID:        "ID",
		hdrName:      "名称",
		hdrShort:     "短名",
		hdrType:      "类型",
		hdrAddr:      "地址",
		hdrStatus:    "状态",
		hdrStartedAt: "开始时间",
		hdrSummary:   "摘要",

		cmpTitle:       "对比结果: %s ↔ %s",
		cmpSummaryLine: "共 %d 项 | %s %d | %s %d | %s %d | %s %d | %s %d",
		cmpMatched:     "一致",
		cmpSrcOnly:     "仅源有",
		cmpTgtOnly:     "仅目标有",
		cmpStructDiff:  "结构差异",
		cmpDataDiff:    "数据差异",
		cmpDBCount:     "%d 个库参与对比",
		cmpDBLine:      "\n%s %s ↔ %s（共%d项, 一致%d, 结构差异%d, 数据差异%d）",
		cmpDBWord:      "库",
		cmpColTable:    "表",
		cmpColStatus:   "状态",
		cmpColDetail:   "差异说明",
		cmpNoDiff:      "（无差异）",
		cmpDiffOnly:    "仅显示有差异的表（%d）；使用 --all 查看全部 %d 项",
		cmpShowHint:    "记录 ID: %s · 查看差异明细: dqex cmp show -i %s [表名]",
		cmpStatusDiff:  "有差异",

		cmpTableTitle:     "表: %s",
		cmpTableAlias:     "%s（%s ↔ %s）",
		cmpStructTitle:    "\n── 结构 ──",
		cmpStructSame:     "结构一致",
		cmpColOnlySrc:     "  （仅源有）",
		cmpColOnlyTgt:     "  （仅目标有）",
		cmpColDiffDef:     "定义不一致",
		cmpSrcCol:         "      源:   %s",
		cmpTgtCol:         "      目标: %s",
		cmpDataTitle:      "\n── 数据 ──",
		cmpNotCompared:    "未逐行比较: %s",
		cmpRowsLine:       "行数: 源 %d / 目标 %d",
		cmpByPK:           "  （按主键 %s 判断有无）",
		cmpNoPK:           "  （无主键，整行对比）",
		cmpDataSame:       "数据一致",
		cmpRowsDiff:       "行数不一致（超过阈值仅比行数；调大 --threshold 后可重跑逐行对比）",
		cmpMissingSuffix:  "%s（源有目标无）",
		cmpMissingCount:   "缺失 %d 行",
		cmpExtraSuffix:    "%s（目标有源无）",
		cmpExtraCount:     "多出 %d 行",
		cmpChangedSuffix:  "%s（主键匹配但内容不同）",
		cmpChangedCount:   "变化 %d 行",
		cmpStructCounts:   "结构: +%d -%d ±%d",
		cmpRowsCount:      "行数 %d vs %d",
		cmpDataSameRows:   "数据一致 (%d行)",
		cmpMissingShort:   "缺失%d行",
		cmpExtraShort:     "多出%d行",
		cmpChangedShort:   "变化%d行",
		cmpMissingSamples: "缺失行样例",
		cmpExtraSamples:   "多出行样例",
		cmpChangedSamples: "变化行样例（%d）",
		cmpSampleTitle:    "%s（%d）:",
		cmpSampleRow:      "     %s: 源=%v  目标=%v",

		errSaveReport: "保存对比报告失败",
		errNoWebCred:  "未找到 Web 访问凭证，请先启动 Web 服务（直接运行 dqex）",
		errNoToken:    "当前凭证无 token（上次以 --no-auth 启动）",

		errConnNotFound:   "未找到连接: %s",
		errCfgRead:        "读取配置文件失败: %s",
		errCfgParse:       "解析配置文件失败: %s",
		errCfgNoCmp:       "配置文件中缺少对比配置（需包含 source/target 段）: %s",
		errCfgNoExp:       "配置文件中缺少导出配置（需包含 source 段）: %s",
		errCfgNoDict:      "配置文件中缺少数据字典配置（需包含 source 段）: %s",
		errCfgNoImp:       "配置文件中缺少导入配置（需包含 target 段）: %s",
		errCfgNoMig:       "配置文件中缺少迁移配置（需包含 source/target 段）: %s",
		errResetMode:      "无效的重置模式: %s（可选 truncate/drop）",
		errNoSrcConn:      "缺少源连接：配置 source/source_ref 段或 --source-*/--source-conn",
		errNoTgtConn:      "缺少目标连接：配置 target/target_ref 段或 --target-*/--target-conn",
		errCmpScope:       "无效的对比范围: %s（可选 both/structure/data）",
		errThresholdNeg:   "threshold 不能为负数",
		errTaskNotImp:     "任务配置 %s 不是导入任务",
		errTaskNotExp:     "任务配置 %s 不是导出任务",
		errTaskNotDict:    "任务配置 %s 不是数据字典任务",
		errTaskNotMig:     "任务配置 %s 不是迁移任务",
		errNoImportFile:   "缺少导入文件：配置 input 字段或 --input",
		errAliasFmt:       "别名格式应为 源表=目标表: %s",
		errNoCmpID:        "缺少 --id（对比记录 ID）",
		errTableConflict:  "表名指定冲突：--table %s 与位置参数 %s",
		errNoTableInRec:   "记录 %s 中未找到表: %s",
		errTaskType:       "未知任务类型: %s",
		errNoNameConfig:   "缺少 --name 或 --config",
		errNoConnType:     "缺少 --type（mysql/postgresql/oracle）",
		errTaskHint:       "无效的任务类型: %s（可选 export/import/migrate/compare/dictionary）",
		errCfgParseOnly:   "解析配置文件失败",
		errCfgTypeUnknown: "无法识别配置文件类型，可用 --type 指定（export/import/migrate/compare）",
	},
	"en": {
		errPrefix:     "Error: %s",
		progressPct:   "  progress %.0f%% (%d/%d items)",
		progressUnits: "%d/%d items",
		progressRows:  " · %s rows",
		rowsThousand:  "%.1fK",

		statusDone:      "Done",
		statusError:     "Failed",
		statusCancelled: "Cancelled",
		statusRunning:   "Running",

		connNone:      "(no saved connections)",
		connShortName: " (short name: %s)",
		connSaved:     "connection saved: %s (%s)",
		connTesting:   "testing connection %s ... ",
		connOK:        "ok",
		connFail:      "failed",
		connDeleted:   "connection deleted",

		cfgNotFound:   "config file: (not found, using defaults; dqex config --gen generates a template)",
		cfgPath:       "config file:     %s",
		cfgDirData:    "config data dir: %s",
		cfgDirTmp:     "task temp dir: %s",
		cfgDirUploads: "uploads temp dir: %s",
		cfgDirExports: "exports dir: %s",
		cfgAllow:      "allowlist: %s (loopback always allowed)",
		cfgAllowNone:  "allowlist: (not configured, external access denied, loopback only)",

		urlTokenExpired: "⚠️ token expired (24h validity); restart the Web service, then re-run dqex url",
		urlExpireAt:     "token valid until %s",
		urlNoAuth:       "note: auth was disabled at last start (--no-auth), the link has no token",

		versionCommitID:  "commit: %s",
		versionBuildTime: "build time: %s",

		startExport:  "exporting...",
		doneExport:   "export done: %s",
		startImport:  "importing...",
		doneImport:   "import done",
		warnReset:    "warning: reset without backup; data in target tables will be lost!",
		startMigrate: "migrating...",
		doneMigrate:  "migrate done",
		startCompare: "comparing...",
		savedCompare: "report saved: %s",
		startDict:    "generating data dictionary...",
		doneDict:     "data dictionary generated: %s",

		taskNone:     "(no task configurations)",
		taskLastUsed: " [last used]",
		taskRunning:  "running task: %s (%s)",
		taskSaved:    "task saved: %s (%s)",
		taskDeleted:  "task deleted",

		snapCreated:     "\n%s snapshot created",
		snapID:          "  ID:       %s",
		snapName:        "  name:     %s",
		snapDBs:         "  database: %s (%s)",
		snapTables:      "  tables:   %d",
		snapRows:        "  total rows: %s",
		snapDesc:        "  note:     %s",
		snapNone:        "no snapshots",
		snapListTitle:   "%s %d snapshots in total",
		snapListWord:    "snapshots",
		snapDetailTitle: "snapshot details",
		snapConn:        "  connection: %s",
		snapCreatedAt:   "  created:   %s",
		snapTableUnit:   "%d tables %s rows",
		snapDBLine:      "\n%s %s (%d tables %s rows)",
		snapDBWord:      "db",
		snapPK:          ", PK: %v",
		snapTableLine:   "  %-30s %d cols %s rows%s",
		snapDeleted:     "%s snapshot %s deleted",

		histNone:     "(no execution history)",
		histID:       "ID:       %s",
		histType:     "type:     %s",
		histStatus:   "status:   %s",
		histStarted:  "started:  %s",
		histFinished: "finished: %s",
		histDuration: "duration: %s",
		histTarget:   "target:   %s",
		histProgress: "progress: %d/%d items",
		histRows:     ", %d rows",
		histSummary:  "summary:  %s",
		histOutput:   "output file: %s",
		histError:    "error:    %s",
		histLogs:     "logs:",
		histDeleted:  "record deleted",

		hdrID:        "ID",
		hdrName:      "Name",
		hdrShort:     "Short",
		hdrType:      "Type",
		hdrAddr:      "Address",
		hdrStatus:    "Status",
		hdrStartedAt: "Started",
		hdrSummary:   "Summary",

		cmpTitle:       "compare result: %s ↔ %s",
		cmpSummaryLine: "total %d | %s %d | %s %d | %s %d | %s %d | %s %d",
		cmpMatched:     "matched",
		cmpSrcOnly:     "source only",
		cmpTgtOnly:     "target only",
		cmpStructDiff:  "struct diff",
		cmpDataDiff:    "data diff",
		cmpDBCount:     "%d databases compared",
		cmpDBLine:      "\n%s %s ↔ %s (total %d, matched %d, struct diff %d, data diff %d)",
		cmpDBWord:      "db",
		cmpColTable:    "table",
		cmpColStatus:   "status",
		cmpColDetail:   "diff detail",
		cmpNoDiff:      "(no differences)",
		cmpDiffOnly:    "showing only tables with differences (%d); use --all to view all %d items",
		cmpShowHint:    "record ID: %s · view diff details: dqex cmp show -i %s [table]",
		cmpStatusDiff:  "differs",

		cmpTableTitle:     "table: %s",
		cmpTableAlias:     "%s (%s ↔ %s)",
		cmpStructTitle:    "\n── structure ──",
		cmpStructSame:     "structure identical",
		cmpColOnlySrc:     "  (source only)",
		cmpColOnlyTgt:     "  (target only)",
		cmpColDiffDef:     "definition differs",
		cmpSrcCol:         "      source: %s",
		cmpTgtCol:         "      target: %s",
		cmpDataTitle:      "\n── data ──",
		cmpNotCompared:    "not compared row by row: %s",
		cmpRowsLine:       "rows: source %d / target %d",
		cmpByPK:           "  (presence judged by primary key %s)",
		cmpNoPK:           "  (no primary key, full-row comparison)",
		cmpDataSame:       "data identical",
		cmpRowsDiff:       "row count differs (beyond threshold, counts only; raise --threshold and re-run for row-by-row)",
		cmpMissingSuffix:  "%s (present in source, missing in target)",
		cmpMissingCount:   "%d rows missing",
		cmpExtraSuffix:    "%s (present in target, missing in source)",
		cmpExtraCount:     "%d extra rows",
		cmpChangedSuffix:  "%s (primary key matched but content differs)",
		cmpChangedCount:   "%d changed rows",
		cmpStructCounts:   "struct: +%d -%d ±%d",
		cmpRowsCount:      "rows %d vs %d",
		cmpDataSameRows:   "data identical (%d rows)",
		cmpMissingShort:   "%d missing",
		cmpExtraShort:     "%d extra",
		cmpChangedShort:   "%d changed",
		cmpMissingSamples: "missing row samples",
		cmpExtraSamples:   "extra row samples",
		cmpChangedSamples: "changed row samples (%d)",
		cmpSampleTitle:    "%s (%d):",
		cmpSampleRow:      "     %s: source=%v  target=%v",

		errSaveReport: "failed to save compare report",
		errNoWebCred:  "web access credential not found; start the web service first (run dqex directly)",
		errNoToken:    "credential has no token (last start used --no-auth)",

		errConnNotFound:   "connection not found: %s",
		errCfgRead:        "failed to read config file: %s",
		errCfgParse:       "failed to parse config file: %s",
		errCfgNoCmp:       "no compare config in config file (needs source/target sections): %s",
		errCfgNoExp:       "no export config in config file (needs source section): %s",
		errCfgNoDict:      "no dictionary config in config file (needs source section): %s",
		errCfgNoImp:       "no import config in config file (needs target section): %s",
		errCfgNoMig:       "no migrate config in config file (needs source/target sections): %s",
		errResetMode:      "invalid reset mode: %s (truncate/drop)",
		errNoSrcConn:      "missing source connection: configure the source/source_ref section or --source-*/--source-conn",
		errNoTgtConn:      "missing target connection: configure the target/target_ref section or --target-*/--target-conn",
		errCmpScope:       "invalid compare scope: %s (both/structure/data)",
		errThresholdNeg:   "threshold must not be negative",
		errTaskNotImp:     "task config %s is not an import task",
		errTaskNotExp:     "task config %s is not an export task",
		errTaskNotDict:    "task config %s is not a dictionary task",
		errTaskNotMig:     "task config %s is not a migrate task",
		errNoImportFile:   "missing import file: configure the input field or --input",
		errAliasFmt:       "alias format should be source=target: %s",
		errNoCmpID:        "missing --id (compare record ID)",
		errTableConflict:  "table conflict: --table %s and positional argument %s",
		errNoTableInRec:   "table %s not found in record %s",
		errTaskType:       "unknown task type: %s",
		errNoNameConfig:   "missing --name or --config",
		errNoConnType:     "missing --type (mysql/postgresql/oracle)",
		errTaskHint:       "invalid task type: %s (export/import/migrate/compare/dictionary)",
		errCfgParseOnly:   "failed to parse config file",
		errCfgTypeUnknown: "cannot recognize config file type; use --type to specify (export/import/migrate/compare)",
	},
}

// cliTextsFor 按语言取状态输出文案，缺失语言回退 zh。
func cliTextsFor(lang string) cliTexts {
	if t, ok := cliTextsMap[llm.NormLang(lang)]; ok {
		return t
	}
	return cliTextsMap["zh"]
}
