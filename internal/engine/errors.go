package engine

import (
	"errors"
	"fmt"
)

// 结构化业务错误：携带注册表模板 key 与参数，展示层（CLI/Web）按当前语言渲染。
// Error() 以 zh 渲染兜底——历史任务记录、日志、未接入语言上下文的调用方保持中文，
// 不破坏既有行为；Msg(lang) 供展示层按语言输出。
//
// 用法：
//
//	无底层原因：return NewMsgErr("expNoDB")
//	带底层原因：return NewMsgErrf("expListTables", err, db)   // 原 fmt.Errorf("...: %w", err)
//
// 模板统一不含 ": %w" 后缀，Error()/Msg() 渲染时自动追加 ": <原因>"。

// MsgError 结构化业务错误
type MsgError struct {
	Key   string // 错误注册表模板 key
	Args  []any  // 模板参数
	Cause error  // 底层原因（原 ": %w" 包装），可为 nil
}

func (e *MsgError) Error() string {
	return renderMsgErr("zh", e)
}

// Msg 按指定语言渲染错误文本（含底层原因）
func (e *MsgError) Msg(lang string) string {
	return renderMsgErr(lang, e)
}

func (e *MsgError) Unwrap() error { return e.Cause }

func renderMsgErr(lang string, e *MsgError) string {
	tpl := engineErrFor(lang)[e.Key]
	if tpl == "" {
		tpl = e.Key // 注册表缺失时回退 key 本身，避免空串误导
	}
	s := sprintf(tpl, e.Args...)
	if e.Cause != nil {
		s += ": " + causeText(lang, e.Cause)
	}
	return s
}

// causeText 渲染底层原因：cause 同为 MsgError 时按同一语言递归渲染（嵌套错误链双语），
// 其余（驱动/系统错误）保持原样作为诊断细节。
func causeText(lang string, err error) string {
	if me := AsMsgErr(err); me != nil {
		return me.Msg(lang)
	}
	return err.Error()
}

// NewMsgErr 创建无底层原因的结构化错误
func NewMsgErr(key string, args ...any) error {
	return &MsgError{Key: key, Args: args}
}

// NewMsgErrf 创建带底层原因的结构化错误（等价原 fmt.Errorf("模板: %w", cause)）
func NewMsgErrf(key string, cause error, args ...any) error {
	return &MsgError{Key: key, Args: args, Cause: cause}
}

// AsMsgErr 提取错误链中的 *MsgError（无则返回 nil）
func AsMsgErr(err error) *MsgError {
	var me *MsgError
	if errors.As(err, &me) {
		return me
	}
	return nil
}

// IsCancelled 判断错误是否为任务取消（engine.MsgError 的 errCancelled key）。
// 供 service 层在按语言渲染错误后仍能识别取消语义（不依赖中文文案匹配）。
func IsCancelled(err error) bool {
	me := AsMsgErr(err)
	return me != nil && me.Key == errCancelled
}

// CancelledMsg 返回任务取消文案（按语言渲染，供 service 层终态消息使用）
func CancelledMsg(lang string) string {
	return engineErrFor(lang)[errCancelled]
}

// sprintf 包装：vet 不对自定义包装做格式串常量检查，供动态模板渲染使用
func sprintf(format string, a ...any) string {
	return fmt.Sprintf(format, a...)
}

// ---- 错误文本注册表 ----
// key 命名：err + 文件缩写 + 语义（如 errExpNoDB / errImpNoFile / errCmpConnSrc）。
// 新增语言只加条目；结构必须与 zh 完全对齐（缺失时 engineErrFor 回退 zh）。

// engineErrMap 语言 → (key → 模板)
var engineErrMap = map[string]map[string]string{
	"zh": {
		// 通用
		errCancelled: "任务已取消",
		errObjType:   "不支持的对象类型: %s",
		errObjDDL:    "未获取到 %s 的创建语句",
		errZipPath:   "非法的 zip 路径: %s",
		// conn
		errConnFail:   "连接数据库失败(%s@%s:%d)",
		errConnFailDB: "连接数据库失败(%s@%s:%d/%s)",
		// reset
		errRstCleanBak:  "清理旧备份表失败",
		errRstCreateBak: "创建备份表失败",
		errRstTrunc:     "清空表 %s 失败",
		errRstDrop:      "删除表 %s 失败",
		// snapshot
		errSnapConn:       "连接数据库失败",
		errSnapNoDB:       "未指定数据库",
		errSnapListTables: "获取表列表失败",
		errSnapEmpty:      "库 %s 内没有表",
		errSnapTableInfo:  "获取表信息失败",
		errSnapRead:       "读取快照文件失败",
		errSnapParse:      "解析快照数据失败",
		// snapshot_compare
		errScmpNoData:     "快照不包含任何库数据",
		errScmpConn:       "目标库连接失败",
		errScmpConnDB:     "目标库[%s]连接失败",
		errScmpListTables: "获取目标库[%s]表列表失败",
		// dictionary
		errDictNoSrc:       "未提供源数据库连接",
		errDictOutDir:      "创建输出目录失败",
		errDictExpDir:      "创建导出目录失败",
		errDictNoDB:        "请先选择要生成数据字典的数据库（连接配置或 databases 参数至少提供一个）",
		errDictListTables:  "获取库 %s 的表列表失败",
		errDictNoTables:    "所选库中没有可生成数据字典的表",
		errDictWrite:       "写入数据字典文件失败",
		errDictZipPack:     "打包 zip 失败",
		errDictExcelStyle:  "创建 Excel 样式失败",
		errDictSheet:       "创建工作表 %s 失败",
		errDictSheetDetail: "生成库 %s 的明细工作表失败",
		errDictRowLimit:    "库 %s 的表/字段数量超过 Excel 单表行数上限（%d），请拆分后按库分别生成",
		// exporter
		errExpNoSrc:       "未提供源数据库连接",
		errExpOutDir:      "创建输出目录失败",
		errExpExpDir:      "创建导出目录失败",
		errExpNoDB:        "未指定要导出的数据库（连接配置或 databases 参数至少提供一个）",
		errExpListTables:  "获取库 %s 的表列表失败",
		errExpNoDatabases: "没有可导出的数据库",
		errExpDB:          "导出库 %s 失败",
		errExpZipPack:     "打包 zip 失败",
		errExpDDL:         "导出表 %s.%s 建表语句失败",
		errExpData:        "导出表 %s.%s 数据失败",
		errExpObjects:     "导出库 %s 的数据库对象失败",
		errExpGenDDL:      "生成建表语句失败",
		errExpGenInsert:   "生成 INSERT 语句失败",
		// importer
		errImpGzip:     "解压 gzip 失败",
		errImpNoFile:   "导入文件不存在或无法读取",
		errImpZip:      "打开 zip 失败",
		errImpFormat:   "不支持的文件格式: %s（仅支持 .sql / .sql.gz / .zip）",
		errImpNoTgt:    "未提供目标数据库连接",
		errImpNoInput:  "未指定导入文件",
		errImpNoTgtDB:  "连接配置未指定目标库，无法导入单文件",
		errImpNoSQL:    "导入文件中没有可导入的 SQL",
		errImpEnsureDB: "确保目标库 %s 存在失败",
		errImpDB:       "导入库 %s 失败",
		errImpDialect:  "不支持的方言: %s",
		errImpExec:     "执行 SQL 失败(第 %d 块)",
		errImpRollback: "写入回滚产物失败",
		// contributor
		errCtbNoExport: "业务对象[%s]未注册导出回调",
		errCtbExport:   "业务对象[%s]导出失败",
		errCtbImport:   "业务对象[%s]导入失败",
		// migrator
		errMigNoConn:          "未提供源或目标数据库连接",
		errMigSrcConn:         "源库连接失败",
		errMigEnsureDB:        "确保目标库 %s 存在失败",
		errMigTgtConn:         "目标库连接失败",
		errMigListTables:      "获取源库表列表失败",
		errMigNoTablesObj:     "没有可迁移的表（跨类型迁移不支持仅迁移对象）",
		errMigNoSel:           "没有选择任何表或对象",
		errMigDDL:             "生成表 %s 建表语句失败",
		errMigCreateTable:     "创建目标表 %s 失败",
		errMigData:            "迁移表 %s 数据失败",
		errMigNoDDL:           "未获取到建表语句",
		errMigTypeUnsupported: "目标库类型 %s 不支持结构迁移",
		errMigBatchWrite:      "批量写入失败",
		// compare
		errCmpNoConn:        "未提供源或目标数据库连接",
		errCmpSrcConn:       "源库连接失败",
		errCmpTgtConn:       "目标库连接失败",
		errCmpSrcConnDB:     "源库[%s]连接失败",
		errCmpTgtConnDB:     "目标库[%s]连接失败",
		errCmpNoDB:          "请先选择对比的库",
		errCmpSrcListTables: "获取源库[%s]表列表失败",
		errCmpTgtListTables: "获取目标库[%s]表列表失败",
		errCmpAliasDup:      "别名配对配置重复: %s ↔ %s",
		errCmpSrcCols:       "获取源表列信息失败",
		errCmpTgtCols:       "获取目标表列信息失败",
		errCmpSrcRows:       "统计源表行数失败",
		errCmpTgtRows:       "统计目标表行数失败",
		errCmpSrcData:       "读取源表数据失败",
		errCmpTgtData:       "读取目标表数据失败",
		errCmpRowCountEmpty: "行数查询结果为空",
		errCmpRowCountParse: "行数解析失败",
		// meta
		errMetaType:        "不支持的数据库类型: %s",
		errMetaListTables:  "获取库 %s 的表列表失败",
		errMetaListDBs:     "获取数据库列表失败",
		errMetaAnchorDB:    "枚举数据库列表失败：请为该连接填写实例上实际存在的数据库名（如 postgres / TEST / security），或确认实例存在可连接的库",
		errMetaListSchemas: "获取 schema %s 的表列表失败",
		errMetaSchemaList:  "获取 schema 列表失败",
		errMetaTableInfo:   "获取表 %s 的元数据失败",
		errMetaTableEmpty:  "获取表 %s 的元数据失败: 返回空结构",
		// sqlgen
		errGenNoConn:       "数据库连接为空",
		errGenNoTable:      "表名不能为空",
		errGenKind:         "未知的生成类型: %s",
		errGenRowsLimit:    "一次最多生成 %d 行（当前 %d 行）",
		errGenColNotExist:  "列「%s」不存在于表 %s",
		errGenNoInline:     "字面量未内联（期望无参数绑定）",
		errGenNoPK:         "表「%s」无主键，无法按行定位",
		errGenRowMismatch:  "行数据与列清单数量不一致",
		errGenPKNotInCols:  "主键列「%s」不在列清单中",
		errGenNoInsertCols: "无可插入列（全部为跳过的自增列）",
		errGenRowMismatchN: "第 %d 行数据与列清单数量不一致",
		errGenInsert:       "生成 INSERT 失败",
		errGenNoRowData:    "缺少行数据",
		errGenPKOnly:       "表「%s」仅含主键列，无可更新列",
		errGenUpdate:       "生成 UPDATE 失败",
		errGenDelete:       "生成 DELETE 失败",
		errGenSelect:       "生成 SELECT 失败",
		errGenCellCond:     "单元格条件需要 1 列 1 值",
		errGenWhere:        "生成 WHERE 条件失败",
		errGenNoStmt:       "未生成任何语句",
		errFilterColEmpty:  "过滤列名不能为空",
		errFilterOp:        "未知的过滤操作符: %s",
		// sqlquery
		errQryForbidden:         "检测到禁止函数，已拦截: %s",
		errQryWriteOp:           "检测到写操作，请使用写操作接口执行: %s",
		errQryProcess:           "查询 SQL 处理失败",
		errQryFail:              "查询失败",
		errQryBigFieldFilter:    "大字段列「%s」不支持该过滤方式，仅支持「为空/非空」",
		errQryFilterColNotExist: "过滤列「%s」不存在于表 %s",
		errQrySortColNotExist:   "排序列「%s」不存在于表 %s",
		errQryParseResult:       "解析查询结果失败",
		errQryWriteHeader:       "写入表头失败",
		errQryWriteData:         "写入数据失败",
		errQryGenExcel:          "生成 Excel 失败",
		errQryExecFail:          "执行失败",
		errQryNoIdent:           "表名/目标列/主键不能为空",
		errQryBuildSQL:          "构建 SQL 失败",
		errQryNoPKIdent:         "表名/主键不能为空",
		errQryPKValueMismatch:   "主键列与值数量不一致",
		errQryNoColIdent:        "表名/列不能为空",
		errQryColValueMismatch:  "列与值数量不一致",
		errQryNoIdentAll:        "表名/列名/主键不能为空",
		errQryNoSQL:             "未检测到可执行的 SQL 语句",
		errQryConnUnavailable:   "连接不可用",
		// store（本地存储/加密，供 store 包复用本机制）
		errStoreOpen:           "打开 SQLite 存储失败",
		errStoreMigrate:        "存储迁移失败",
		errStoreRecordNotFound: "执行记录不存在: %s",
		errCryptoFormat:        "密文格式无效",
		errCryptoLen:           "密文长度无效",
		errCryptoDecrypt:       "解密失败（可能数据来自其他机器或已被篡改）",
	},
	"en": {
		// 通用
		errCancelled: "task cancelled",
		errObjType:   "unsupported object type: %s",
		errObjDDL:    "failed to get create statement for %s",
		errZipPath:   "illegal zip path: %s",
		// conn
		errConnFail:   "failed to connect to database (%s@%s:%d)",
		errConnFailDB: "failed to connect to database (%s@%s:%d/%s)",
		// reset
		errRstCleanBak:  "failed to clean old backup table",
		errRstCreateBak: "failed to create backup table",
		errRstTrunc:     "failed to truncate table %s",
		errRstDrop:      "failed to drop table %s",
		// snapshot
		errSnapConn:       "failed to connect to database",
		errSnapNoDB:       "no database specified",
		errSnapListTables: "failed to get table list",
		errSnapEmpty:      "no tables in db %s",
		errSnapTableInfo:  "failed to get table info",
		errSnapRead:       "failed to read snapshot file",
		errSnapParse:      "failed to parse snapshot data",
		// snapshot_compare
		errScmpNoData:     "snapshot contains no database data",
		errScmpConn:       "failed to connect to target database",
		errScmpConnDB:     "failed to connect to target database [%s]",
		errScmpListTables: "failed to get table list of target database [%s]",
		// dictionary
		errDictNoSrc:       "no source database connection provided",
		errDictOutDir:      "failed to create output directory",
		errDictExpDir:      "failed to create export directory",
		errDictNoDB:        "please select a database to generate the data dictionary (provide a connection config or the databases parameter)",
		errDictListTables:  "failed to get table list of database %s",
		errDictNoTables:    "no tables in the selected database for dictionary generation",
		errDictWrite:       "failed to write data dictionary file",
		errDictZipPack:     "failed to pack zip",
		errDictExcelStyle:  "failed to create Excel style",
		errDictSheet:       "failed to create sheet %s",
		errDictSheetDetail: "failed to generate detail sheet of database %s",
		errDictRowLimit:    "the table/column count of database %s exceeds the Excel single-sheet row limit (%d), please split and generate per database",
		// exporter
		errExpNoSrc:       "no source database connection provided",
		errExpOutDir:      "failed to create output directory",
		errExpExpDir:      "failed to create export directory",
		errExpNoDB:        "no database specified for export (provide a connection config or the databases parameter)",
		errExpListTables:  "failed to get table list of database %s",
		errExpNoDatabases: "no databases to export",
		errExpDB:          "failed to export database %s",
		errExpZipPack:     "failed to pack zip",
		errExpDDL:         "failed to export create statement of table %s.%s",
		errExpData:        "failed to export data of table %s.%s",
		errExpObjects:     "failed to export database objects of %s",
		errExpGenDDL:      "failed to generate create statement",
		errExpGenInsert:   "failed to generate INSERT statement",
		// importer
		errImpGzip:     "failed to decompress gzip",
		errImpNoFile:   "import file does not exist or cannot be read",
		errImpZip:      "failed to open zip",
		errImpFormat:   "unsupported file format: %s (only .sql / .sql.gz / .zip supported)",
		errImpNoTgt:    "no target database connection provided",
		errImpNoInput:  "no import file specified",
		errImpNoTgtDB:  "connection config has no target database, cannot import a single file",
		errImpNoSQL:    "no importable SQL in the import file",
		errImpEnsureDB: "failed to ensure target database %s exists",
		errImpDB:       "failed to import database %s",
		errImpDialect:  "unsupported dialect: %s",
		errImpExec:     "failed to execute SQL (block %d)",
		errImpRollback: "failed to write rollback artifact",
		// contributor
		errCtbNoExport: "contributor[%s] has no export callback registered",
		errCtbExport:   "contributor[%s] export failed",
		errCtbImport:   "contributor[%s] import failed",
		// migrator
		errMigNoConn:          "no source or target database connection provided",
		errMigSrcConn:         "failed to connect to source database",
		errMigEnsureDB:        "failed to ensure target database %s exists",
		errMigTgtConn:         "failed to connect to target database",
		errMigListTables:      "failed to get table list of source database",
		errMigNoTablesObj:     "no tables to migrate (cross-type migration does not support migrating objects only)",
		errMigNoSel:           "no tables or objects selected",
		errMigDDL:             "failed to generate create statement of table %s",
		errMigCreateTable:     "failed to create target table %s",
		errMigData:            "failed to migrate data of table %s",
		errMigNoDDL:           "failed to get create statement",
		errMigTypeUnsupported: "target database type %s does not support structure migration",
		errMigBatchWrite:      "failed to batch write",
		// compare
		errCmpNoConn:        "no source or target database connection provided",
		errCmpSrcConn:       "failed to connect to source database",
		errCmpTgtConn:       "failed to connect to target database",
		errCmpSrcConnDB:     "failed to connect to source database [%s]",
		errCmpTgtConnDB:     "failed to connect to target database [%s]",
		errCmpNoDB:          "please select databases to compare",
		errCmpSrcListTables: "failed to get table list of source database [%s]",
		errCmpTgtListTables: "failed to get table list of target database [%s]",
		errCmpAliasDup:      "duplicate alias pairing: %s ↔ %s",
		errCmpSrcCols:       "failed to get column info of source table",
		errCmpTgtCols:       "failed to get column info of target table",
		errCmpSrcRows:       "failed to count rows of source table",
		errCmpTgtRows:       "failed to count rows of target table",
		errCmpSrcData:       "failed to read data of source table",
		errCmpTgtData:       "failed to read data of target table",
		errCmpRowCountEmpty: "row count query returned empty",
		errCmpRowCountParse: "failed to parse row count",
		// meta
		errMetaType:        "unsupported database type: %s",
		errMetaListTables:  "failed to get table list of database %s",
		errMetaListDBs:     "failed to get database list",
		errMetaAnchorDB:    "failed to enumerate databases: please specify an existing database name (e.g. postgres / TEST / security) for this connection, or make sure a connectable database exists on the instance",
		errMetaListSchemas: "failed to get table list of schema %s",
		errMetaSchemaList:  "failed to get schema list",
		errMetaTableInfo:   "failed to get metadata of table %s",
		errMetaTableEmpty:  "failed to get metadata of table %s: returned empty structure",
		// sqlgen
		errGenNoConn:       "database connection is empty",
		errGenNoTable:      "table name cannot be empty",
		errGenKind:         "unknown generation type: %s",
		errGenRowsLimit:    "at most %d rows can be generated at once (currently %d)",
		errGenColNotExist:  "column \"%s\" does not exist in table %s",
		errGenNoInline:     "literal not inlined (expected no parameter binding)",
		errGenNoPK:         "table \"%s\" has no primary key, cannot locate rows",
		errGenRowMismatch:  "row data count does not match column list",
		errGenPKNotInCols:  "primary key column \"%s\" not in column list",
		errGenNoInsertCols: "no columns to insert (all are skipped auto-increment columns)",
		errGenRowMismatchN: "row %d data count does not match column list",
		errGenInsert:       "failed to generate INSERT",
		errGenNoRowData:    "missing row data",
		errGenPKOnly:       "table \"%s\" contains only primary key columns, no columns to update",
		errGenUpdate:       "failed to generate UPDATE",
		errGenDelete:       "failed to generate DELETE",
		errGenSelect:       "failed to generate SELECT",
		errGenCellCond:     "cell condition requires 1 column and 1 value",
		errGenWhere:        "failed to generate WHERE clause",
		errGenNoStmt:       "no statements generated",
		errFilterColEmpty:  "filter column name cannot be empty",
		errFilterOp:        "unknown filter operator: %s",
		// sqlquery
		errQryForbidden:         "forbidden function detected, blocked: %s",
		errQryWriteOp:           "write operation detected, please use the write API: %s",
		errQryProcess:           "failed to process query SQL",
		errQryFail:              "query failed",
		errQryBigFieldFilter:    "large-field column \"%s\" does not support this filter, only \"is null/is not null\"",
		errQryFilterColNotExist: "filter column \"%s\" does not exist in table %s",
		errQrySortColNotExist:   "sort column \"%s\" does not exist in table %s",
		errQryParseResult:       "failed to parse query result",
		errQryWriteHeader:       "failed to write header",
		errQryWriteData:         "failed to write data",
		errQryGenExcel:          "failed to generate Excel",
		errQryExecFail:          "execution failed",
		errQryNoIdent:           "table name/target column/primary key cannot be empty",
		errQryBuildSQL:          "failed to build SQL",
		errQryNoPKIdent:         "table name/primary key cannot be empty",
		errQryPKValueMismatch:   "primary key column and value count mismatch",
		errQryNoColIdent:        "table name/column cannot be empty",
		errQryColValueMismatch:  "column and value count mismatch",
		errQryNoIdentAll:        "table name/column name/primary key cannot be empty",
		errQryNoSQL:             "no executable SQL statement detected",
		errQryConnUnavailable:   "connection unavailable",
		// store（本地存储/加密，供 store 包复用本机制）
		errStoreOpen:           "failed to open SQLite storage",
		errStoreMigrate:        "failed to migrate storage",
		errStoreRecordNotFound: "execution record not found: %s",
		errCryptoFormat:        "invalid ciphertext format",
		errCryptoLen:           "invalid ciphertext length",
		errCryptoDecrypt:       "decryption failed (data may come from another machine or has been tampered with)",
	},
}

// 错误模板 key 常量（编译期防拼写错误）
const (
	errCancelled            = "errCancelled"
	errObjType              = "errObjType"
	errObjDDL               = "errObjDDL"
	errZipPath              = "errZipPath"
	errConnFail             = "errConnFail"
	errConnFailDB           = "errConnFailDB"
	errRstCleanBak          = "errRstCleanBak"
	errRstCreateBak         = "errRstCreateBak"
	errRstTrunc             = "errRstTrunc"
	errRstDrop              = "errRstDrop"
	errSnapConn             = "errSnapConn"
	errSnapNoDB             = "errSnapNoDB"
	errSnapListTables       = "errSnapListTables"
	errSnapEmpty            = "errSnapEmpty"
	errSnapTableInfo        = "errSnapTableInfo"
	errSnapRead             = "errSnapRead"
	errSnapParse            = "errSnapParse"
	errScmpNoData           = "errScmpNoData"
	errScmpConn             = "errScmpConn"
	errScmpConnDB           = "errScmpConnDB"
	errScmpListTables       = "errScmpListTables"
	errDictNoSrc            = "errDictNoSrc"
	errDictOutDir           = "errDictOutDir"
	errDictExpDir           = "errDictExpDir"
	errDictNoDB             = "errDictNoDB"
	errDictListTables       = "errDictListTables"
	errDictNoTables         = "errDictNoTables"
	errDictWrite            = "errDictWrite"
	errDictZipPack          = "errDictZipPack"
	errDictExcelStyle       = "errDictExcelStyle"
	errDictSheet            = "errDictSheet"
	errDictSheetDetail      = "errDictSheetDetail"
	errDictRowLimit         = "errDictRowLimit"
	errExpNoSrc             = "errExpNoSrc"
	errExpOutDir            = "errExpOutDir"
	errExpExpDir            = "errExpExpDir"
	errExpNoDB              = "errExpNoDB"
	errExpListTables        = "errExpListTables"
	errExpNoDatabases       = "errExpNoDatabases"
	errExpDB                = "errExpDB"
	errExpZipPack           = "errExpZipPack"
	errExpDDL               = "errExpDDL"
	errExpData              = "errExpData"
	errExpObjects           = "errExpObjects"
	errExpGenDDL            = "errExpGenDDL"
	errExpGenInsert         = "errExpGenInsert"
	errImpGzip              = "errImpGzip"
	errImpNoFile            = "errImpNoFile"
	errImpZip               = "errImpZip"
	errImpFormat            = "errImpFormat"
	errImpNoTgt             = "errImpNoTgt"
	errImpNoInput           = "errImpNoInput"
	errImpNoTgtDB           = "errImpNoTgtDB"
	errImpNoSQL             = "errImpNoSQL"
	errImpEnsureDB          = "errImpEnsureDB"
	errImpDB                = "errImpDB"
	errImpDialect           = "errImpDialect"
	errImpExec              = "errImpExec"
	errImpRollback          = "errImpRollback"
	errCtbNoExport          = "errCtbNoExport"
	errCtbExport            = "errCtbExport"
	errCtbImport            = "errCtbImport"
	errMigNoConn            = "errMigNoConn"
	errMigSrcConn           = "errMigSrcConn"
	errMigEnsureDB          = "errMigEnsureDB"
	errMigTgtConn           = "errMigTgtConn"
	errMigListTables        = "errMigListTables"
	errMigNoTablesObj       = "errMigNoTablesObj"
	errMigNoSel             = "errMigNoSel"
	errMigDDL               = "errMigDDL"
	errMigCreateTable       = "errMigCreateTable"
	errMigData              = "errMigData"
	errMigNoDDL             = "errMigNoDDL"
	errMigTypeUnsupported   = "errMigTypeUnsupported"
	errMigBatchWrite        = "errMigBatchWrite"
	errCmpNoConn            = "errCmpNoConn"
	errCmpSrcConn           = "errCmpSrcConn"
	errCmpTgtConn           = "errCmpTgtConn"
	errCmpSrcConnDB         = "errCmpSrcConnDB"
	errCmpTgtConnDB         = "errCmpTgtConnDB"
	errCmpNoDB              = "errCmpNoDB"
	errCmpSrcListTables     = "errCmpSrcListTables"
	errCmpTgtListTables     = "errCmpTgtListTables"
	errCmpAliasDup          = "errCmpAliasDup"
	errCmpSrcCols           = "errCmpSrcCols"
	errCmpTgtCols           = "errCmpTgtCols"
	errCmpSrcRows           = "errCmpSrcRows"
	errCmpTgtRows           = "errCmpTgtRows"
	errCmpSrcData           = "errCmpSrcData"
	errCmpTgtData           = "errCmpTgtData"
	errCmpRowCountEmpty     = "errCmpRowCountEmpty"
	errCmpRowCountParse     = "errCmpRowCountParse"
	errMetaType             = "errMetaType"
	errMetaListTables       = "errMetaListTables"
	errMetaListDBs          = "errMetaListDBs"
	errMetaAnchorDB         = "errMetaAnchorDB"
	errMetaListSchemas      = "errMetaListSchemas"
	errMetaSchemaList       = "errMetaSchemaList"
	errMetaTableInfo        = "errMetaTableInfo"
	errMetaTableEmpty       = "errMetaTableEmpty"
	errGenNoConn            = "errGenNoConn"
	errGenNoTable           = "errGenNoTable"
	errGenKind              = "errGenKind"
	errGenRowsLimit         = "errGenRowsLimit"
	errGenColNotExist       = "errGenColNotExist"
	errGenNoInline          = "errGenNoInline"
	errGenNoPK              = "errGenNoPK"
	errGenRowMismatch       = "errGenRowMismatch"
	errGenPKNotInCols       = "errGenPKNotInCols"
	errGenNoInsertCols      = "errGenNoInsertCols"
	errGenRowMismatchN      = "errGenRowMismatchN"
	errGenInsert            = "errGenInsert"
	errGenNoRowData         = "errGenNoRowData"
	errGenPKOnly            = "errGenPKOnly"
	errGenUpdate            = "errGenUpdate"
	errGenDelete            = "errGenDelete"
	errGenSelect            = "errGenSelect"
	errGenCellCond          = "errGenCellCond"
	errGenWhere             = "errGenWhere"
	errGenNoStmt            = "errGenNoStmt"
	errFilterColEmpty       = "errFilterColEmpty"
	errFilterOp             = "errFilterOp"
	errQryForbidden         = "errQryForbidden"
	errQryWriteOp           = "errQryWriteOp"
	errQryProcess           = "errQryProcess"
	errQryFail              = "errQryFail"
	errQryBigFieldFilter    = "errQryBigFieldFilter"
	errQryFilterColNotExist = "errQryFilterColNotExist"
	errQrySortColNotExist   = "errQrySortColNotExist"
	errQryParseResult       = "errQryParseResult"
	errQryWriteHeader       = "errQryWriteHeader"
	errQryWriteData         = "errQryWriteData"
	errQryGenExcel          = "errQryGenExcel"
	errQryExecFail          = "errQryExecFail"
	errQryNoIdent           = "errQryNoIdent"
	errQryBuildSQL          = "errQryBuildSQL"
	errQryNoPKIdent         = "errQryNoPKIdent"
	errQryPKValueMismatch   = "errQryPKValueMismatch"
	errQryNoColIdent        = "errQryNoColIdent"
	errQryColValueMismatch  = "errQryColValueMismatch"
	errQryNoIdentAll        = "errQryNoIdentAll"
	errQryNoSQL             = "errQryNoSQL"
	errQryConnUnavailable   = "errQryConnUnavailable"

	// ---- 跨包导出的错误 key（store 层复用本机制；llm 层保持纯客户端不依赖） ----
	errStoreOpen           = "errStoreOpen"
	ErrStoreOpen           = errStoreOpen
	errStoreMigrate        = "errStoreMigrate"
	ErrStoreMigrate        = errStoreMigrate
	errStoreRecordNotFound = "errStoreRecordNotFound"
	ErrStoreRecordNotFound = errStoreRecordNotFound
	errCryptoFormat        = "errCryptoFormat"
	ErrCryptoFormat        = errCryptoFormat
	errCryptoLen           = "errCryptoLen"
	ErrCryptoLen           = errCryptoLen
	errCryptoDecrypt       = "errCryptoDecrypt"
	ErrCryptoDecrypt       = errCryptoDecrypt
)

// engineErrFor 取指定语言的错误模板表，未知语言回退 zh
func engineErrFor(lang string) map[string]string {
	if m, ok := engineErrMap[normLang(lang)]; ok {
		return m
	}
	return engineErrMap["zh"]
}
