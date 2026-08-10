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
  conn: DBConn
  subTypes: string[]
}

export function emptyConn(): DBConn {
  return { Type: "mysql", SubType: "8.0", Host: "", Port: 3306, Un: "", Pw: "", DBName: "" }
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
}

export type ResetMode = "" | "truncate" | "drop"

export interface ImportOptions {
  targetConn: string
  target?: DBConn | null
  inputPath: string
  resetMode: ResetMode
  backup: boolean
  batchSize: number
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
}

// ---- 对比 ----

// 表级对比配置：源表 ↔ 目标表（不同名但逻辑对应），可单独指定该表的忽略列
export interface TableAlias {
  source: string
  target: string
  // 表级忽略列：仅对该表数据对比生效，与全局 ignoreColumns 合并
  ignoreColumns?: string[]
}

export interface CompareOptions {
  sourceConn: string
  targetConn: string
  source?: DBConn | null
  target?: DBConn | null
  databases?: string[]
  // 选中的表（undefined/空 = 对比库内全部表）
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

export type TaskType = "export" | "import" | "migrate" | "compare"

export interface TaskConfig {
  id: string
  name: string
  type: TaskType
  exportOpts?: ExportOptions | null
  importOpts?: ImportOptions | null
  migrateOpts?: MigrateOptions | null
  compareOpts?: CompareOptions | null
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
}

export const RESET_MODE_LABEL: Record<string, string> = {
  "": "不重置（直接追加数据）",
  truncate: "清空表（TRUNCATE，保留表结构）",
  drop: "删除重建（DROP + CREATE）",
}
