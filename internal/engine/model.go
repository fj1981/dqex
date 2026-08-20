// Package engine 核心引擎层：导出/导入/迁移。
// 注意：为避免循环依赖，引擎层所需的公共模型定义在本包，
// 根包 model.go 通过类型别名对外暴露。
package engine

import (
	"gitlab.mycyclone.com/rpa-platform/pk-infrakit-g/pkg/cydb/def"
)

// DBConnInfo 应用层连接信息。
// def.DBConnection 已包含 SubType 字段：它不是版本号，而是“使用该类型兼容模式的具体数据库产品”
// （如 mysql 兼容的 "oceanbase"/"mariadb"、postgresql 兼容的 "gaussdb"/"kingbase"、oracle 兼容的 "dameng"），
// 供方言层按产品语法差异生成 SQL；空值或等于类型名表示原生标准库。
// 此处内嵌复用，JSON/YAML 序列化时字段平铺。
type DBConnInfo struct {
	def.DBConnection `json:",inline" yaml:",inline"`
}

// TableDataMode 表数据导出模式
type TableDataMode string

const (
	TableDataModeAll       TableDataMode = ""          // 默认：导出所有数据
	TableDataModeSkip      TableDataMode = "skip"      // 不导出数据
	TableDataModeCondition TableDataMode = "condition" // 根据条件导出数据
)

// TableCondition 表级数据过滤条件：统一为完整 SELECT（Query）。
// Where/Columns 为旧版配置字段，仅读取兼容（归一化拼装为 SELECT，见 conditionQuery），不再写入新值。
type TableCondition struct {
	TableName string        `json:"tableName" yaml:"tableName"`
	DataMode  TableDataMode `json:"dataMode,omitempty" yaml:"dataMode,omitempty"` // 数据导出模式
	Query     string        `json:"query,omitempty" yaml:"query,omitempty"`       // 完整 SELECT 语句（仅 dataMode=condition 时生效）
	Where     string        `json:"where,omitempty" yaml:"where,omitempty"`       // Deprecated: 旧版 WHERE 条件，仅读取兼容
	Columns   []string      `json:"columns,omitempty" yaml:"columns,omitempty"`   // Deprecated: 旧版导出列，仅读取兼容
}

// ExportOptions 导出选项
type ExportOptions struct {
	SourceConn string           `json:"sourceConn" yaml:"sourceConn"` // 已保存连接名（与 Source 二选一）
	Source     *DBConnInfo      `json:"source,omitempty" yaml:"source,omitempty"`
	OutputDir  string           `json:"outputDir" yaml:"outputDir"` // 导出根目录，默认数据目录下 exports/
	TaskName   string           `json:"taskName" yaml:"taskName"`   // 用于生成 zip 文件名
	Databases  []string         `json:"databases" yaml:"databases"` // 指定库（空=连接配置的库）
	Tables     []string         `json:"tables" yaml:"tables"`       // 指定表（nil=全部，空数组=不导出）
	Objects    []string         `json:"objects" yaml:"objects"`     // 指定对象，格式 _views/名称（nil=全部，空数组=不导出）
	Conditions []TableCondition `json:"conditions" yaml:"conditions"`
	SchemaOnly bool             `json:"schemaOnly" yaml:"schemaOnly"` // 仅导出结构
	DataOnly   bool             `json:"dataOnly" yaml:"dataOnly"`     // 仅导出数据
	BatchSize  int              `json:"batchSize" yaml:"batchSize"`
	Compress   bool             `json:"compress" yaml:"compress"` // 是否打包 zip，默认 true
	// SingleTransaction 一致性快照导出（等同 mysqldump --single-transaction）：
	// 全程在同一事务内读取，跨表数据处于同一时间点，避免运行中库导出出不一致状态
	SingleTransaction bool `json:"singleTransaction" yaml:"singleTransaction"`
	// Gzip 将 SQL 文件 gzip 压缩为 .sql.gz（导入侧透明解压；与 Compress 同时开启时 zip 内为 .sql.gz）
	Gzip bool `json:"gzip" yaml:"gzip"`
	// CompatCollation 兼容排序规则：将 MySQL 8.0 特有排序规则（如 utf8mb4_0900_ai_ci）
	// 替换为 MySQL 5.7 兼容的 utf8mb4_unicode_ci，使导出 SQL 可在低版本 MySQL 上执行。
	// 仅对 MySQL 导出的建表 DDL 生效（表级和列级 COLLATE）。
	CompatCollation bool `json:"compatCollation" yaml:"compatCollation"`
	// Lang 任务日志/产物文案语言（zh/en），默认 zh
	Lang string `json:"lang,omitempty" yaml:"lang,omitempty"`
}

// DictionaryOptions 数据字典导出选项：按选定库表生成单个 .xlsx 数据字典（总览 + 每库字段明细）
type DictionaryOptions struct {
	SourceConn string      `json:"sourceConn" yaml:"sourceConn"` // 已保存连接名（与 Source 二选一）
	Source     *DBConnInfo `json:"source,omitempty" yaml:"source,omitempty"`
	OutputDir  string      `json:"outputDir" yaml:"outputDir"`           // 导出根目录，默认数据目录下 exports/
	TaskName   string      `json:"taskName" yaml:"taskName"`             // 用于生成产物文件名
	Databases  []string    `json:"databases" yaml:"databases"`           // 指定库（空=连接配置的库）
	Tables     []string    `json:"tables" yaml:"tables"`                 // 指定表（nil=全部，空数组=不导出）
	Compress   bool        `json:"compress" yaml:"compress"`             // 是否打包 zip，默认 true
	Lang       string      `json:"lang,omitempty" yaml:"lang,omitempty"` // 产物文案语言（zh/en），默认 zh
}

// ResetMode 重置数据模式
type ResetMode string

const (
	ResetNone     ResetMode = ""         // 不重置，直接追加（默认）
	ResetTruncate ResetMode = "truncate" // 清空表（TRUNCATE），保留表结构
	ResetDrop     ResetMode = "drop"     // 删除重建（DROP + CREATE）
)

// ImportOptions 导入选项
type ImportOptions struct {
	TargetConn string      `json:"targetConn" yaml:"targetConn"`
	Target     *DBConnInfo `json:"target,omitempty" yaml:"target,omitempty"`
	InputPath  string      `json:"inputPath" yaml:"inputPath"` // 支持 .sql 单文件 或 .zip 包
	ResetMode  ResetMode   `json:"resetMode" yaml:"resetMode"` // 重置模式: "" / "truncate" / "drop"
	Backup     bool        `json:"backup" yaml:"backup"`       // 重置前在目标库创建备份表（默认 true，仅 reset != none 时生效）
	BatchSize  int         `json:"batchSize" yaml:"batchSize"`
	TempDir    string      `json:"-" yaml:"-"` // 任务处理临时目录（zip 解压），空=系统临时目录；运行时注入不入任务配置
	// CompatCollation 兼容排序规则：将 SQL 文件中的 MySQL 8.0 特有排序规则（如 utf8mb4_0900_ai_ci）
	// 替换为 MySQL 5.7 兼容的 utf8mb4_unicode_ci。仅对 MySQL 目标的导入生效。
	CompatCollation bool `json:"compatCollation" yaml:"compatCollation"`
	// Lang 任务日志语言（zh/en），默认 zh
	Lang string `json:"lang,omitempty" yaml:"lang,omitempty"`
}

// MigrateOptions 迁移选项
type MigrateOptions struct {
	SourceConn string           `json:"sourceConn" yaml:"sourceConn"`
	TargetConn string           `json:"targetConn" yaml:"targetConn"`
	Source     *DBConnInfo      `json:"source,omitempty" yaml:"source,omitempty"`
	Target     *DBConnInfo      `json:"target,omitempty" yaml:"target,omitempty"`
	Tables     []string         `json:"tables" yaml:"tables"`   // 指定表（nil=全部，空数组=不迁移）
	Objects    []string         `json:"objects" yaml:"objects"` // 指定对象，格式 _views/名称（nil=全部，空数组=不迁移；仅同类型生效）
	Conditions []TableCondition `json:"conditions" yaml:"conditions"`
	SchemaOnly bool             `json:"schemaOnly" yaml:"schemaOnly"`
	DataOnly   bool             `json:"dataOnly" yaml:"dataOnly"`
	ResetMode  ResetMode        `json:"resetMode" yaml:"resetMode"` // 重置模式: "" / "truncate" / "drop"
	Backup     bool             `json:"backup" yaml:"backup"`       // 重置前在目标库创建备份表（默认 true）
	BatchSize  int              `json:"batchSize" yaml:"batchSize"`
	// CompatCollation 兼容排序规则：将 MySQL 8.0 特有排序规则（如 utf8mb4_0900_ai_ci）
	// 替换为 MySQL 5.7 兼容的 utf8mb4_unicode_ci。
	// 仅对 MySQL 同类型迁移的建表 DDL 生效（表级和列级 COLLATE）。
	CompatCollation bool `json:"compatCollation" yaml:"compatCollation"`
	// Lang 任务日志/产物文案语言（zh/en），默认 zh
	Lang string `json:"lang,omitempty" yaml:"lang,omitempty"`
}

// Progress 任务进度信息
type Progress struct {
	State        string   `json:"state"` // idle/running/done/error/cancelled
	TaskID       string   `json:"taskID"`
	TotalUnits   int      `json:"totalUnits"` // 工作单元总数（表 + 视图/函数/存储过程等对象）
	CurrentTable string   `json:"currentTable"`
	DoneUnits    int      `json:"doneUnits"`
	TotalRows    int64    `json:"totalRows"`
	DoneRows     int64    `json:"doneRows"`
	Percent      float64  `json:"percent"`
	Message      string   `json:"message"`
	OutputPath   string   `json:"outputPath,omitempty"` // 导出完成后的文件路径
	DurationMs   int64    `json:"durationMs,omitempty"` // 任务总耗时（仅终态回放时由执行历史填充，实时推送时前端自行计时）
	Logs         []string `json:"logs"`
}

// ProgressFunc 进度回调
type ProgressFunc func(Progress)

// BackupTablePrefix 重置备份表前缀
const BackupTablePrefix = "__dbimpex_bak_"

// DefaultBatchSize 默认批量大小
const DefaultBatchSize = 500

// ---- 数据库对比 ----

// CompareOptions 对比选项
// 多库对比：SourceDatabases/TargetDatabases 为要对比的源库/目标库列表（按索引一一配对）；
// DBMapping 提供源库名→目标库名的覆盖映射（未按索引配对时的回退，如 db_prod→db_test）；
// Tables 为 nil 时对比所有选中库的全部表（项为 "库.表" 限定名或裸表名）。
// 源/目标不同名的同义表通过 Aliases 配对。
type CompareOptions struct {
	SourceConn    string            `json:"sourceConn" yaml:"sourceConn"`
	TargetConn    string            `json:"targetConn" yaml:"targetConn"`
	Source        *DBConnInfo       `json:"source,omitempty" yaml:"source,omitempty"`
	Target        *DBConnInfo       `json:"target,omitempty" yaml:"target,omitempty"`
	Databases     []CompareDBPair   `json:"databases,omitempty" yaml:"databases,omitempty"` // 库对（源库↔目标库），按索引配对
	DBMapping     map[string]string `json:"dbMapping,omitempty" yaml:"dbMapping,omitempty"` // 源库名→目标库名覆盖映射
	Tables        []string          `json:"tables,omitempty" yaml:"tables,omitempty"`
	Aliases       []TableAlias      `json:"aliases,omitempty" yaml:"aliases,omitempty"`
	StructureOnly bool              `json:"structureOnly" yaml:"structureOnly"` // 仅比结构
	DataOnly      bool              `json:"dataOnly" yaml:"dataOnly"`           // 仅比数据
	Threshold     int               `json:"threshold" yaml:"threshold"`         // 数据逐行比较阈值，默认 1000
	IgnoreColumns []string          `json:"ignoreColumns,omitempty" yaml:"ignoreColumns,omitempty"`
	ForceData     bool              `json:"forceData,omitempty" yaml:"forceData,omitempty"`
	// Lang 任务日志语言（zh/en），默认 zh
	Lang string `json:"lang,omitempty" yaml:"lang,omitempty"`
}

// CompareDBPair 对比的库对（源库 ↔ 目标库）
type CompareDBPair struct {
	SourceDB string `json:"sourceDB" yaml:"sourceDB"` // 源库名（oracle 为 schema）
	TargetDB string `json:"targetDB" yaml:"targetDB"` // 目标库名
}

// TableAlias 表级对比配置：源表 ↔ 目标表（不同名但逻辑对应，Target 空=同名匹配），
// Source 支持限定名 "库.表"，用于多库场景下精确配置某个库的同名表。
// 并可单独指定该表数据对比的忽略列（与全局忽略列合并生效）
type TableAlias struct {
	Source        string   `json:"source" yaml:"source"`
	Target        string   `json:"target" yaml:"target"`
	IgnoreColumns []string `json:"ignoreColumns,omitempty" yaml:"ignoreColumns,omitempty"` // 表级忽略列（列名大小写不敏感）
}

// CompareResult 对比结果（多库分组，序列化落盘为 JSON 报告）
type CompareResult struct {
	Source    string                  `json:"source"`           // 源连接标签
	Target    string                  `json:"target"`           // 目标连接标签
	Databases []CompareDatabaseResult `json:"databases"`        // 每个库对的结果（多库分组）
	Tables    []CompareTableResult    `json:"tables,omitempty"` // 兼容字段：所有库表的扁平汇总（旧数据可读，新写入不再依赖）
	Summary   CompareSummary          `json:"summary"`          // 汇总统计（所有库合计）
}

// CompareDatabaseResult 单个库对（源库 ↔ 目标库）的对比结果
type CompareDatabaseResult struct {
	SourceDB string               `json:"sourceDB"` // 源库名（oracle 为 schema）
	TargetDB string               `json:"targetDB"` // 目标库名
	Tables   []CompareTableResult `json:"tables"`   // 该库内表级结果
	Summary  CompareSummary       `json:"summary"`  // 该库汇总统计
}

// CompareSummary 对比汇总计数
type CompareSummary struct {
	Total         int `json:"total"`         // 配对总数
	Matched       int `json:"matched"`       // 完全一致
	SourceOnly    int `json:"sourceOnly"`    // 仅源有
	TargetOnly    int `json:"targetOnly"`    // 仅目标有
	StructureDiff int `json:"structureDiff"` // 结构差异
	DataDiff      int `json:"dataDiff"`      // 数据差异（含仅计数不一致）
}

// CompareTableResult 单表（配对）对比结果
// Status: source_only=仅源有 / target_only=仅目标有 / both=两侧均有
type CompareTableResult struct {
	Name       string      `json:"name"`                 // 展示名：同名取表名，别名配对为 "源表 ↔ 目标表"
	SourceName string      `json:"sourceName,omitempty"` // 源侧实际表名
	TargetName string      `json:"targetName,omitempty"` // 目标侧实际表名
	SourceDB   string      `json:"sourceDB,omitempty"`   // 源侧库名（oracle 为 schema），多库展示用
	TargetDB   string      `json:"targetDB,omitempty"`   // 目标侧库名，多库展示用
	Status     string      `json:"status"`
	Columns    *ColumnDiff `json:"columns,omitempty"` // 结构差异（DataOnly 时为 nil）
	Data       *DataDiff   `json:"data,omitempty"`    // 数据差异（StructureOnly 时为 nil）
}

// ColumnDiff 列级结构差异
type ColumnDiff struct {
	Matched    bool             `json:"matched"`
	SourceOnly []ColumnItem     `json:"sourceOnly"` // 源有目标无的列
	TargetOnly []ColumnItem     `json:"targetOnly"` // 目标有源无的列
	Different  []ColumnItemDiff `json:"different"`  // 类型/可空/主键不一致的列
}

// ColumnItem 单列信息
type ColumnItem struct {
	Name           string `json:"name"`
	DataType       string `json:"dataType"`                 // 原始类型（GetOrginalDataType），用于展示
	NormalizedType string `json:"normalizedType,omitempty"` // 按方言归一后的类型（写入时固化，diff 优先用此比对）
	Nullable       bool   `json:"nullable"`
	PrimaryKey     bool   `json:"primaryKey"`
}

// ColumnItemDiff 不一致列的双侧信息（供前端并排展示）
type ColumnItemDiff struct {
	Name   string     `json:"name"`
	Source ColumnItem `json:"source"`
	Target ColumnItem `json:"target"`
}

// DataDiff 数据差异
// Mode: rows=逐行比较 / count=仅比较行数（超阈值）
type DataDiff struct {
	Mode           string           `json:"mode"`
	SourceRows     int64            `json:"sourceRows"`
	TargetRows     int64            `json:"targetRows"`
	Equal          bool             `json:"equal"`                // count 模式：行数相等；rows 模式：无差异
	KeyColumns     []string         `json:"keyColumns,omitempty"` // 有无判断依据的主键列（PK 模式）；空=无主键整行比较
	Missing        int              `json:"missing,omitempty"`    // 源有目标无的行数（rows 模式）
	Extra          int              `json:"extra,omitempty"`      // 目标有源无的行数（rows 模式）
	Changed        int              `json:"changed,omitempty"`    // 主键匹配但内容不同的行数（PK 模式）
	MissingSamples []map[string]any `json:"missingSamples,omitempty"`
	ExtraSamples   []map[string]any `json:"extraSamples,omitempty"`
	ChangedSamples []ChangedRow     `json:"changedSamples,omitempty"` // 变化行样例（主键值 + 差异列双侧取值）
	SampleColumns  []string         `json:"sampleColumns,omitempty"`  // 采样行列序（按源表列定义顺序），供前端按序渲染
	SkippedReason  string           `json:"skippedReason,omitempty"`  // 未逐行比较的原因说明
}

// ChangedRow 变化行样例：主键取值 + 差异列双侧取值（PK 模式：主键匹配但内容不同）
type ChangedRow struct {
	Key   map[string]any `json:"key"`   // 主键列取值
	Diffs []ValueDiff    `json:"diffs"` // 取值不一致的列
}

// ValueDiff 单列取值差异
type ValueDiff struct {
	Column string `json:"column"`
	Source any    `json:"source"`
	Target any    `json:"target"`
}

// ---- 数据库快照 ----

// Snapshot 快照完整数据（单个快照文件内容，与索引分离以减少列表加载开销）
// 支持覆盖多库：Databases 为各库快照，根上的 DBName/Tables 仅作单库兼容读取（v1 旧文件）
type Snapshot struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	ConnID      string             `json:"connId"`    // 来源连接 ID（允许连接被删除后快照仍可读）
	ConnLabel   string             `json:"connLabel"` // 冗余连接名称
	DBType      string             `json:"dbType"`
	CreatedAt   int64              `json:"createdAt"` // unix 秒
	TableCount  int                `json:"tableCount"`
	TotalRows   int64              `json:"totalRows"`
	Databases   []SnapshotDatabase `json:"databases"`        // 多库快照
	DBName      string             `json:"dbName,omitempty"` // 兼容字段（单库旧快照），新写入不再使用
	Tables      []SnapshotTable    `json:"tables,omitempty"` // 兼容字段（单库旧快照）
}

// SnapshotDatabase 单库快照
type SnapshotDatabase struct {
	DBName     string          `json:"dbName"`
	TableCount int             `json:"tableCount"`
	TotalRows  int64           `json:"totalRows"`
	Tables     []SnapshotTable `json:"tables"`
}

// SnapshotTable 单表快照
type SnapshotTable struct {
	Name       string           `json:"name"`
	RowCount   int64            `json:"rowCount"`
	Columns    []SnapshotColumn `json:"columns"`
	PrimaryKey []string         `json:"primaryKey,omitempty"`
	RowSamples []map[string]any `json:"rowSamples,omitempty"` // 前 N 行采样（创建时可选）
}

// SnapshotColumn 列快照
type SnapshotColumn struct {
	Name           string `json:"name"`
	DataType       string `json:"dataType"`                 // 原始类型
	NormalizedType string `json:"normalizedType,omitempty"` // 按快照库方言归一后的类型（写入时固化）
	Nullable       bool   `json:"nullable"`
	PrimaryKey     bool   `json:"primaryKey"`
	Position       int    `json:"position"`
}

// SnapshotInfo 快照摘要（索引文件条目，不包含表详情）
// DBNames 为多库列表；DBName 兼容字段（单库旧快照）
type SnapshotInfo struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	ConnID      string   `json:"connId"`
	ConnLabel   string   `json:"connLabel"`
	DBNames     []string `json:"dbNames"`
	DBName      string   `json:"dbName,omitempty"` // 兼容字段
	DBType      string   `json:"dbType"`
	TableCount  int      `json:"tableCount"`
	TotalRows   int64    `json:"totalRows"`
	CreatedAt   int64    `json:"createdAt"` // unix 秒
}

// CreateSnapshotOptions 创建快照选项
type CreateSnapshotOptions struct {
	IncludeSamples bool // 是否保存前 N 行采样数据
	SampleLimit    int  // 采样行数上限，默认 10
	// Lang 任务日志语言（zh/en），默认 zh
	Lang string
}

// SnapshotCompareOptions 快照对比选项
type SnapshotCompareOptions struct {
	SnapshotID string            `json:"snapshotId"`
	TargetConn string            `json:"targetConn"`
	Target     *DBConnInfo       `json:"target,omitempty"`
	DBMapping  map[string]string `json:"dbMapping,omitempty"` // 快照库名→目标库名映射（默认同名）
	Tables     []string          `json:"tables,omitempty"`    // 限定对比的表，nil=全部（"库.表" 或裸名）
	// Lang 任务日志语言（zh/en），默认 zh
	Lang string `json:"lang,omitempty"`
}

// ExportDesc 导出描述文件（.desc）内容，与 .sql 文件同名，JSON 格式
// 导入时直接读取此文件即可获取导出元信息，无需解析 SQL 内容
type ExportDesc struct {
	Database   string              `json:"database"`          // 数据库名
	ExportTime string              `json:"exportTime"`        // 导出时间
	DBType     string              `json:"dbType"`            // 数据库类型（mysql/postgresql/oracle）
	Mode       string              `json:"mode"`              // 导出模式：schema+data / schemaOnly / dataOnly
	Tables     []ExportDescTable   `json:"tables,omitempty"`  // 表列表（含行数、条件）
	Objects    map[string][]string `json:"objects,omitempty"` // 对象：_views/_functions/_procedures → 名称列表
}

// ExportDescTable 导出描述中的表信息
// Where/Columns 为旧版导出文件字段，仅读取兼容（展示旧文件用），新导出一律只写 Query
type ExportDescTable struct {
	Name    string   `json:"name"`
	Rows    int64    `json:"rows"`
	Query   string   `json:"query,omitempty"`   // 完整 SELECT（按条件导出时记录）
	Where   string   `json:"where,omitempty"`   // Deprecated: 旧版导出文件的 WHERE 条件
	Columns []string `json:"columns,omitempty"` // Deprecated: 旧版导出文件的导出列
}
