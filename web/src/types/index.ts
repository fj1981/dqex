// ---- 数据库连接 ----

export interface DBConn {
  Key?: string
  Type: string
  SubType?: string
  Host: string
  Port: number
  Un: string
  Pw: string
  DBName?: string
  Service?: string
  SSLMode?: string
  Schema?: string
}

export interface ConnInfo {
  id: string // 主键（xid）
  name: string
  shortName?: string // 命令行简写/短名（如 prod/my-dev），可选
  env?: string // dev/test/staging/prod，用户录入时手动选择；留空视为 prod
  conn: DBConn
  subTypes: string[]
}

export function emptyConn(): DBConn {
  return { Type: "mysql", SubType: "mysql", Host: "", Port: 3306, Un: "", Pw: "", DBName: "" }
}

// 数据库子类型展示名：SubType 非版本号，而是兼容数据库产品；值等于类型名时为原生标准库
export const DB_SUBTYPE_LABEL: Record<string, string> = {
  mysql: "MySQL（原生）",
  oceanbase: "OceanBase",
  mariadb: "MariaDB",
  postgresql: "PostgreSQL（原生）",
  gaussdb: "GaussDB",
  kingbase: "KingbaseES",
  oracle: "Oracle（原生）",
  dameng: "达梦 Dameng",
}

// ---- 选项 ----

// 数据库及其表/对象清单（树形结构；未配置库时后端遍历所有库）
export interface DBTables {
  name: string
  tables: string[]
  // 库内对象：_views/_functions/_procedures → 对象名
  objects?: Record<string, string[]>
}

export type TableDataMode = "" | "skip" | "condition"

export interface TableCondition {
  tableName: string
  dataMode?: TableDataMode // 数据导出模式：空=全量, skip=不导出, condition=按条件
  query?: string           // 完整 SELECT 语句（condition 模式）
  /** @deprecated 旧版配置兼容，读取后归一化为 query */
  where?: string
  /** @deprecated 旧版配置兼容，读取后归一化为 query */
  columns?: string[]
}

// 表列信息（后端返回，用于条件编辑辅助）
export interface TableColumn {
  name: string
  dataType: string
  nullable: boolean
  primaryKey?: boolean
  default?: string
  autoIncrement?: boolean
}

export interface ExportOptions {
  sourceConn: string
  source?: DBConn | null
  outputDir: string
  taskName: string
  databases?: string[]
  tables?: string[]
  // 对象白名单，格式 _views/名称；undefined=全部，[]=不导出
  objects?: string[]
  conditions?: TableCondition[]
  schemaOnly: boolean
  dataOnly: boolean
  batchSize: number
  compress: boolean
  // 一致性快照导出（MySQL/PostgreSQL），跨表数据处于同一时间点
  singleTransaction: boolean
  // SQL 文件 gzip 压缩为 .sql.gz
  gzip: boolean
  // 兼容排序规则：将 MySQL 8.0 特有排序规则替换为 5.7 兼容版本，使导出 SQL 可导入低版本
  compatCollation: boolean
}

export type ResetMode = "" | "truncate" | "drop"

// 数据字典导出选项：产物为单个 .xlsx（总览 + 每库字段明细）
export interface DictionaryOptions {
  sourceConn: string
  source?: DBConn | null
  outputDir: string
  taskName: string
  databases?: string[]
  tables?: string[]
  compress: boolean
}

export interface ImportOptions {
  targetConn: string
  target?: DBConn | null
  inputPath: string
  resetMode: ResetMode
  backup: boolean
  batchSize: number
  // 兼容排序规则：将 MySQL 8.0 特有排序规则替换为 5.7 兼容版本
  compatCollation: boolean
}

export interface MigrateOptions {
  sourceConn: string
  targetConn: string
  source?: DBConn | null
  target?: DBConn | null
  // 选中的库（多选，空 = 连接配置的库）
  databases?: string[]
  tables?: string[]
  // 对象白名单，格式 _views/名称；undefined=全部，[]=不迁移；仅同类型迁移生效
  objects?: string[]
  conditions?: TableCondition[]
  schemaOnly: boolean
  dataOnly: boolean
  resetMode: ResetMode
  backup: boolean
  batchSize: number
  // 兼容排序规则：将 MySQL 8.0 特有排序规则替换为 5.7 兼容版本
  compatCollation: boolean
}

// ---- 对比 ----

// 表级对比配置：源表 ↔ 目标表（不同名但逻辑对应），可单独指定该表的忽略列
export interface TableAlias {
  source: string
  target: string
  // 表级忽略列：仅对该表数据对比生效，与全局 ignoreColumns 合并
  ignoreColumns?: string[]
}

export interface CompareDBPair {
  sourceDB: string
  targetDB: string
}

export interface CompareOptions {
  sourceConn: string
  targetConn: string
  source?: DBConn | null
  target?: DBConn | null
  // 多库对比：库对（按索引一一配对）；为空时回退到源/目标连接的库
  databases?: CompareDBPair[]
  // 多库对比：源库名 → 目标库名 覆盖映射（同名配对时无需填）
  dbMapping?: Record<string, string>
  // 选中的表（undefined/空 = 对比库内全部表），"库.表" 限定名或裸名
  tables?: string[]
  aliases?: TableAlias[]
  structureOnly: boolean
  dataOnly: boolean
  threshold: number // 数据逐行比较阈值，默认 1000
  ignoreColumns?: string[] // 全局忽略列：所有表数据对比时跳过（如 created_at/updated_at）
  forceData?: boolean // 结构不一致时仍强制对比数据（默认跳过）
}

export interface CompareColumnItem {
  name: string
  dataType: string
  nullable: boolean
  primaryKey: boolean
}

export interface CompareColumnDiff {
  matched: boolean
  sourceOnly: CompareColumnItem[]
  targetOnly: CompareColumnItem[]
  different: { name: string; source: CompareColumnItem; target: CompareColumnItem }[]
}

export interface CompareDataDiff {
  mode: "rows" | "count" | "skipped" // rows=逐行比较 / count=仅比较行数 / skipped=已跳过（如结构不一致）
  sourceRows: number
  targetRows: number
  equal: boolean
  keyColumns?: string[] // 有无判断依据的主键列（PK 模式）；空=无主键整行比较
  missing?: number // 源有目标无（rows 模式）
  extra?: number   // 目标有源无（rows 模式）
  changed?: number // 主键匹配但内容不同（PK 模式）
  missingSamples?: Record<string, unknown>[]
  extraSamples?: Record<string, unknown>[]
  changedSamples?: ChangedRow[] // 变化行样例（主键值 + 差异列双侧取值）
  sampleColumns?: string[] // 采样行列序（按源表列定义顺序）
  skippedReason?: string
}

// 变化行样例：主键取值 + 取值不一致的列双侧对照
export interface ChangedRow {
  key: Record<string, unknown>
  diffs: { column: string; source: unknown; target: unknown }[]
}

export interface CompareTableResult {
  name: string
  sourceName?: string
  targetName?: string
  sourceDB?: string
  targetDB?: string
  status: "both" | "source_only" | "target_only"
  columns?: CompareColumnDiff | null
  data?: CompareDataDiff | null
}

export interface CompareSummary {
  total: number
  matched: number
  sourceOnly: number
  targetOnly: number
  structureDiff: number
  dataDiff: number
}

export interface CompareResult {
  source: string
  target: string
  // 多库分组结果；旧数据可能仅含扁平 tables
  databases?: CompareDatabaseResult[]
  tables: CompareTableResult[]
  summary: CompareSummary
}

export interface CompareDatabaseResult {
  sourceDB: string
  targetDB: string
  tables: CompareTableResult[]
  summary: CompareSummary
}

// ---- 进度 ----

export interface Progress {
  state: string // idle/running/done/error/cancelled
  taskID: string
  totalUnits: number // 工作单元总数（表 + 对象）
  currentTable: string
  doneUnits: number
  totalRows: number
  doneRows: number
  percent: number
  message: string
  outputPath?: string
  durationMs?: number // 任务总耗时（仅终态回放时由执行历史填充）
  logs: string[]
}

// ---- 任务配置 / 执行历史 ----

export type TaskType = "export" | "import" | "migrate" | "compare" | "dictionary"

export interface TaskConfig {
  id: string
  name: string
  type: TaskType
  exportOpts?: ExportOptions | null
  importOpts?: ImportOptions | null
  migrateOpts?: MigrateOptions | null
  compareOpts?: CompareOptions | null
  dictionaryOpts?: DictionaryOptions | null
  createdAt: number
  updatedAt: number
  isLastUsed: boolean
}

export interface ExecutionRecord {
  id: string
  taskType: TaskType
  taskConfigId?: string
  status: string // running/done/error/cancelled
  startedAt: number
  finishedAt: number
  duration: number
  totalUnits: number // 工作单元数（表 + 对象）
  totalRows: number
  outputPath?: string
  fileSize?: number
  errorMsg?: string
  target?: string // 操作目标（连接名 + 库表对象），如 "生产库 · db1(93表)"
  summary: string
}

export interface ExportDesc {
  database: string
  exportTime: string
  dbType: string
  mode: string // schema+data / schemaOnly / dataOnly
  tables?: ExportDescTable[]
  objects?: Record<string, string[]> // _views/_functions/_procedures → 名称列表
}

export interface ExportDescTable {
  name: string
  rows: number
  query?: string // 完整 SELECT（按条件导出时记录）
  /** @deprecated 旧版导出文件兼容 */
  where?: string
  /** @deprecated 旧版导出文件兼容 */
  columns?: string[]
}

export interface ImportFileInfo {
  type: string // sql / zip
  size: number
  databases: string[]
  descs?: Record<string, ExportDesc> // 库名 → 导出描述
}

export const TASK_TYPE_LABEL: Record<string, string> = {
  export: "导出",
  import: "导入",
  migrate: "迁移",
  compare: "对比",
  dictionary: "数据字典",
  snapshot_compare: "快照对比",
}

// ---- 快照 ----

export interface SnapshotInfo {
  id: string
  name: string
  description: string
  connId: string
  connLabel: string
  dbName: string // 兼容字段（单库旧快照），新快照用 dbNames
  dbNames: string[]
  dbType: string
  tableCount: number
  totalRows: number
  createdAt: number
}

export interface SnapshotDatabaseInfo {
  dbName: string
  tableCount: number
  totalRows: number
  tables: SnapshotTableInfo[]
}

export interface SnapshotDetail extends SnapshotInfo {
  tables: SnapshotTableInfo[] // 兼容字段（单库旧快照）
  databases?: SnapshotDatabaseInfo[]
}

export interface SnapshotTableInfo {
  name: string
  rowCount: number
  columns: SnapshotColumnInfo[]
  primaryKey: string[]
  rowSamples?: Record<string, unknown>[]
}

export interface SnapshotColumnInfo {
  name: string
  dataType: string
  nullable: boolean
  primaryKey: boolean
  position: number
}

export interface SnapshotCompareOptions {
  snapshotId: string
  targetConn: string
  target?: DBConn | null
  // 快照库名 → 目标库名 映射（默认同名配对）
  dbMapping?: Record<string, string>
  tables?: string[]
}

export const RESET_MODE_LABEL: Record<string, string> = {
  "": "不重置（直接追加数据）",
  truncate: "清空表（TRUNCATE，保留表结构）",
  drop: "删除重建（DROP + CREATE）",
}
