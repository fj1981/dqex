package engine

import "strings"

// 任务进度日志文本注册表（tracker.log 的格式串双语）。
// 语言经 Options.Lang 传入 tracker；未知语言回退 zh，新增语言只加 map 条目。
// 注意：进度日志同时用于 Web SSE 推送与 CLI 终端展示，产物语言 = 任务发起时语言。

type engineTexts struct {
	// exporter
	expNoTables        string // 库 %s 无选中的表，仅处理对象
	expStart           string // 开始导出: %d 个库, %d 张表 → %s
	expNoFKWarn        string // 警告: 库 %s 未获取到前置约束语句，导入带外键依赖的库可能失败
	zipPack            string // 打包 zip: %s
	expDone            string // 导出完成: %d 表, %d 行 → %s
	expIsolationWarn   string // 警告: 设置会话隔离级别失败: %v（退化为普通导出）
	expNoSnapshot      string // 库类型 %s 不支持一致性快照导出，按普通方式导出
	expSnapshotFail    string // 警告: 开启快照事务失败: %v（退化为普通导出）
	expTxIsolationWarn string // 警告: 设置事务隔离级别失败: %v（退化为普通导出）
	expSnapshotOn      string // 一致性快照已启用：全部表在同一事务内读取
	expStructDone      string // %s.%s 结构导出完成
	expSkipData        string // %s.%s 跳过数据导出（dataMode=skip）
	expDataDone        string // %s.%s 数据导出完成 (%d 行)
	expDescFail        string // 写入描述文件失败（已跳过）: %v
	expSortFail        string // 表依赖排序不可用（将按原顺序导出）: %v
	expObjFail         string // 导出%s %s.%s 失败（已跳过）: %v
	expObjDone         string // %s.%s/%s 导出完成

	// importer
	impStart  string // 开始导入: %d 个库
	impDBDone string // 库 %s 导入完成 (%d 条语句)
	impDone   string // 导入完成: %d 个库, %d 条语句

	// reset
	rstBackup string // 已创建备份表 %s
	rstTrunc  string // 已清空表 %s
	rstDrop   string // 已删除表 %s（等待重建）

	// snapshot
	snapStart string // 开始创建快照: 库 %s, %d 张表
	snapFail  string // 表 %s 快照失败（已跳过）: %v
	snapRow   string // 表 %s: %d 行, %d 列
	snapDone  string // 快照创建完成: %d 张表, 共 %d 行

	// snapshot compare
	scmpDone       string // 快照对比完成（%d 个库）: 共%d项, 一致%d, 仅快照有%d, 仅当前有%d, 结构差异%d, 数据差异%d
	scmpStart      string // 开始快照对比库 %s → 当前 %s: %d 组表配对
	scmpStructFail string // 表 %s 结构对比失败（已跳过）: %v
	scmpRowFail    string // 表 %s 行数统计失败（已跳过）: %v

	// compare
	cmpDone       string // 对比完成（%d 个库）: 共%d项, 一致%d, 仅源有%d, 仅目标有%d, 结构差异%d, 数据差异%d
	cmpStart      string // 开始对比库对 %s ↔ %s: %d 组表配对, 数据阈值=%d
	cmpStructFail string // 表 %s 结构对比失败（已跳过）: %v
	cmpDataFail   string // 表 %s 数据对比失败（已跳过）: %v

	// dictionary
	dictNoTables   string // 库 %s 无选中的表，跳过
	dictStart      string // 开始生成数据字典: %d 个库, %d 张表
	dictStructFail string // 获取表 %s.%s 结构失败（将写入占位行）: %v
	dictCols       string // %s.%s 元数据采集完成 (%d 列)
	dictDone       string // 数据字典生成完成: %d 库 %d 表 → %s

	// migrator
	migStart       string // 开始%s: %d 张表, 重置模式=%s
	migCreate      string // 已创建目标表 %s
	migTableDone   string // 表 %s 迁移完成 (%d 行)
	migCleanFail   string // 清理备份表失败（可忽略）: %v
	migDone        string // 迁移完成: %d 张表, %d 行
	migCancel      string // 任务已取消，跳过剩余对象迁移
	migObjFail     string // 获取%s %s.%s 失败（已跳过）: %v
	migObjExecFail string // 目标库执行%s %s.%s 失败（已跳过）: %v
	migObjDone     string // %s.%s/%s 迁移完成

	// migrator 辅助
	modeSame  string // 同类型迁移
	modeCross string // 跨类型迁移(%s → %s)
	resetDesc map[ResetMode]string

	// 数据对比跳过原因 / 表结果描述（tableResultDesc）
	skipStructDiff string // 结构不一致，已跳过数据对比（--force-data 可强制）
	skipRowsThresh string // 行数（源 %d / 目标 %d）超过阈值 %d，仅比较行数
	skipNoCols     string // 无公共列，跳过数据对比
	skipAllIgnored string // 公共列均被忽略，跳过数据对比
	skipStructRows string // 结构不一致，已跳过行数对比
	skipRowChanged string // 行数变化: 快照 %d → 当前 %d
	descSrcOnly    string // 仅源库存在
	descTgtOnly    string // 仅目标库存在
	descStructSame string // 结构一致
	descStructDiff string // 结构差异(源独有%d列/目标独有%d列/%d列不一致)
	descDataSame   string // 数据一致(%d行)
	descRowDiff    string // 行数不一致(源%d/目标%d)
	descDataDiff   string // 数据差异(缺失%d行/多出%d行)
}

// engineTextsMap 进度日志语言注册表：新增语言只加条目，结构对齐 zh
var engineTextsMap = map[string]engineTexts{
	"zh": {
		expNoTables:        "库 %s 无选中的表，仅处理对象",
		expStart:           "开始导出: %d 个库, %d 张表 → %s",
		expNoFKWarn:        "警告: 库 %s 未获取到前置约束语句，导入带外键依赖的库可能失败",
		zipPack:            "打包 zip: %s",
		expDone:            "导出完成: %d 表, %d 行 → %s",
		expIsolationWarn:   "警告: 设置会话隔离级别失败: %v（退化为普通导出）",
		expNoSnapshot:      "库类型 %s 不支持一致性快照导出，按普通方式导出",
		expSnapshotFail:    "警告: 开启快照事务失败: %v（退化为普通导出）",
		expTxIsolationWarn: "警告: 设置事务隔离级别失败: %v（退化为普通导出）",
		expSnapshotOn:      "一致性快照已启用：全部表在同一事务内读取",
		expStructDone:      "%s.%s 结构导出完成",
		expSkipData:        "%s.%s 跳过数据导出（dataMode=skip）",
		expDataDone:        "%s.%s 数据导出完成 (%d 行)",
		expDescFail:        "写入描述文件失败（已跳过）: %v",
		expSortFail:        "表依赖排序不可用（将按原顺序导出）: %v",
		expObjFail:         "导出%s %s.%s 失败（已跳过）: %v",
		expObjDone:         "%s.%s/%s 导出完成",
		impStart:           "开始导入: %d 个库",
		impDBDone:          "库 %s 导入完成 (%d 条语句)",
		impDone:            "导入完成: %d 个库, %d 条语句",
		rstBackup:          "已创建备份表 %s",
		rstTrunc:           "已清空表 %s",
		rstDrop:            "已删除表 %s（等待重建）",
		snapStart:          "开始创建快照: 库 %s, %d 张表",
		snapFail:           "表 %s 快照失败（已跳过）: %v",
		snapRow:            "表 %s: %d 行, %d 列",
		snapDone:           "快照创建完成: %d 张表, 共 %d 行",
		scmpDone:           "快照对比完成（%d 个库）: 共%d项, 一致%d, 仅快照有%d, 仅当前有%d, 结构差异%d, 数据差异%d",
		scmpStart:          "开始快照对比库 %s → 当前 %s: %d 组表配对",
		scmpStructFail:     "表 %s 结构对比失败（已跳过）: %v",
		scmpRowFail:        "表 %s 行数统计失败（已跳过）: %v",
		cmpDone:            "对比完成（%d 个库）: 共%d项, 一致%d, 仅源有%d, 仅目标有%d, 结构差异%d, 数据差异%d",
		cmpStart:           "开始对比库对 %s ↔ %s: %d 组表配对, 数据阈值=%d",
		cmpStructFail:      "表 %s 结构对比失败（已跳过）: %v",
		cmpDataFail:        "表 %s 数据对比失败（已跳过）: %v",
		dictNoTables:       "库 %s 无选中的表，跳过",
		dictStart:          "开始生成数据字典: %d 个库, %d 张表",
		dictStructFail:     "获取表 %s.%s 结构失败（将写入占位行）: %v",
		dictCols:           "%s.%s 元数据采集完成 (%d 列)",
		dictDone:           "数据字典生成完成: %d 库 %d 表 → %s",
		migStart:           "开始%s: %d 张表, 重置模式=%s",
		migCreate:          "已创建目标表 %s",
		migTableDone:       "表 %s 迁移完成 (%d 行)",
		migCleanFail:       "清理备份表失败（可忽略）: %v",
		migDone:            "迁移完成: %d 张表, %d 行",
		migCancel:          "任务已取消，跳过剩余对象迁移",
		migObjFail:         "获取%s %s.%s 失败（已跳过）: %v",
		migObjExecFail:     "目标库执行%s %s.%s 失败（已跳过）: %v",
		migObjDone:         "%s.%s/%s 迁移完成",
		modeSame:           "同类型迁移",
		modeCross:          "跨类型迁移(%s → %s)",
		resetDesc: map[ResetMode]string{
			ResetTruncate: "清空表",
			ResetDrop:     "删除重建",
			ResetNone:     "不重置",
		},
		skipStructDiff: "结构不一致，已跳过数据对比（--force-data 可强制）",
		skipRowsThresh: "行数（源 %d / 目标 %d）超过阈值 %d，仅比较行数",
		skipNoCols:     "无公共列，跳过数据对比",
		skipAllIgnored: "公共列均被忽略，跳过数据对比",
		skipStructRows: "结构不一致，已跳过行数对比",
		skipRowChanged: "行数变化: 快照 %d → 当前 %d",
		descSrcOnly:    "仅源库存在",
		descTgtOnly:    "仅目标库存在",
		descStructSame: "结构一致",
		descStructDiff: "结构差异(源独有%d列/目标独有%d列/%d列不一致)",
		descDataSame:   "数据一致(%d行)",
		descRowDiff:    "行数不一致(源%d/目标%d)",
		descDataDiff:   "数据差异(缺失%d行/多出%d行)",
	},
	"en": {
		expNoTables:        "db %s has no selected tables, objects only",
		expStart:           "exporting: %d db(s), %d tables → %s",
		expNoFKWarn:        "warning: failed to get FK constraints for db %s; importing databases with FK dependencies may fail",
		zipPack:            "packing zip: %s",
		expDone:            "export done: %d tables, %d rows → %s",
		expIsolationWarn:   "warning: failed to set session isolation level: %v (falling back to normal export)",
		expNoSnapshot:      "db type %s does not support consistent snapshot export, exporting normally",
		expSnapshotFail:    "warning: failed to open snapshot transaction: %v (falling back to normal export)",
		expTxIsolationWarn: "warning: failed to set transaction isolation level: %v (falling back to normal export)",
		expSnapshotOn:      "consistent snapshot enabled: all tables read in one transaction",
		expStructDone:      "%s.%s structure exported",
		expSkipData:        "%s.%s data export skipped (dataMode=skip)",
		expDataDone:        "%s.%s data exported (%d rows)",
		expDescFail:        "failed to write description file (skipped): %v",
		expSortFail:        "table dependency sorting unavailable (exporting in original order): %v",
		expObjFail:         "exporting %s %s.%s failed (skipped): %v",
		expObjDone:         "%s.%s/%s exported",
		impStart:           "importing: %d db(s)",
		impDBDone:          "db %s imported (%d statements)",
		impDone:            "import done: %d db(s), %d statements",
		rstBackup:          "backup table created: %s",
		rstTrunc:           "table cleared: %s",
		rstDrop:            "table dropped (to be recreated): %s",
		snapStart:          "creating snapshot: db %s, %d tables",
		snapFail:           "snapshot of table %s failed (skipped): %v",
		snapRow:            "table %s: %d rows, %d columns",
		snapDone:           "snapshot created: %d tables, %d rows in total",
		scmpDone:           "snapshot compare done (%d db(s)): %d items total, %d matched, %d snapshot-only, %d current-only, %d structure diffs, %d data diffs",
		scmpStart:          "comparing snapshot db %s → current %s: %d table pairs",
		scmpStructFail:     "structure compare of table %s failed (skipped): %v",
		scmpRowFail:        "row count of table %s failed (skipped): %v",
		cmpDone:            "compare done (%d db(s)): %d items total, %d matched, %d source-only, %d target-only, %d structure diffs, %d data diffs",
		cmpStart:           "comparing db pair %s ↔ %s: %d table pairs, data threshold=%d",
		cmpStructFail:      "structure compare of table %s failed (skipped): %v",
		cmpDataFail:        "data compare of table %s failed (skipped): %v",
		dictNoTables:       "db %s has no selected tables, skipped",
		dictStart:          "generating data dictionary: %d db(s), %d tables",
		dictStructFail:     "failed to get structure of table %s.%s (placeholder row will be written): %v",
		dictCols:           "%s.%s metadata collected (%d columns)",
		dictDone:           "data dictionary generated: %d db(s) %d tables → %s",
		migStart:           "starting %s: %d tables, reset mode=%s",
		migCreate:          "target table created: %s",
		migTableDone:       "table %s migrated (%d rows)",
		migCleanFail:       "failed to clean backup tables (ignorable): %v",
		migDone:            "migration done: %d tables, %d rows",
		migCancel:          "task cancelled, skipping remaining object migration",
		migObjFail:         "failed to get %s %s.%s (skipped): %v",
		migObjExecFail:     "failed to execute %s %s.%s on target db (skipped): %v",
		migObjDone:         "%s.%s/%s migrated",
		modeSame:           "same-type migration",
		modeCross:          "cross-type migration (%s → %s)",
		resetDesc: map[ResetMode]string{
			ResetTruncate: "truncate",
			ResetDrop:     "drop & recreate",
			ResetNone:     "none",
		},
		skipStructDiff: "structures differ, data comparison skipped (--force-data to force)",
		skipRowsThresh: "row count (source %d / target %d) exceeds threshold %d, comparing row counts only",
		skipNoCols:     "no common columns, data comparison skipped",
		skipAllIgnored: "all common columns ignored, data comparison skipped",
		skipStructRows: "structures differ, row count comparison skipped",
		skipRowChanged: "row count changed: snapshot %d → current %d",
		descSrcOnly:    "source-only",
		descTgtOnly:    "target-only",
		descStructSame: "structure identical",
		descStructDiff: "structure diff (%d source-only / %d target-only / %d different)",
		descDataSame:   "data identical (%d rows)",
		descRowDiff:    "row count differs (source %d / target %d)",
		descDataDiff:   "data diff (%d missing / %d extra)",
	},
}

// normLang 语言归一化：en 前缀 → en，其余（含空/未知）→ zh。
// 引擎层自包含实现，避免反向依赖 llm 包。
func normLang(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	if strings.HasPrefix(l, "en") {
		return "en"
	}
	return "zh"
}

// engineTextsFor 取语言进度文本，未知语言回退 zh
func engineTextsFor(lang string) engineTexts {
	if t, ok := engineTextsMap[normLang(lang)]; ok {
		return t
	}
	return engineTextsMap["zh"]
}
