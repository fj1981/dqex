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
  unique?: boolean // 唯一约束（非主键）
  indexed?: boolean // 普通索引（非主键/非唯一）
  comment?: string // 列注释（可为空）
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
  lang?: string // 产物文案语言（zh/en），缺省后端回退 zh
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
  normalizedType?: string
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

// 任务类型 → i18n key（文案见 locales: app.taskType.*），渲染时 t(TASK_TYPE_LABEL[x])
export const TASK_TYPE_LABEL: Record<string, string> = {
  export: "app.taskType.export",
  import: "app.taskType.import",
  migrate: "app.taskType.migrate",
  compare: "app.taskType.compare",
  dictionary: "app.taskType.dictionary",
  snapshot_compare: "app.taskType.snapshot_compare",
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

// 重置模式 → i18n key（文案见 locales: app.resetMode.*），渲染时 t(RESET_MODE_LABEL[x])
export const RESET_MODE_LABEL: Record<string, string> = {
  "": "app.resetMode.none",
  truncate: "app.resetMode.truncate",
  drop: "app.resetMode.drop",
}

// ---- SQL 查询终端 ----

// 快速生成 SQL 的类型（表浏览右键 → 后端按方言生成可执行语句）
export type GenSQLKind =
  | "insert" // 行/多行 INSERT（跳过自增列）
  | "update" // 单行 UPDATE（SET 非主键列，WHERE 主键）
  | "delete" // 单行 DELETE（WHERE 主键）
  | "selectByPk" // 单行 SELECT（WHERE 主键）
  | "selectByFilter" // 按当前过滤条件 + 排序 SELECT
  | "whereCell" // 单元格等于值条件 SELECT

// SQL 执行模式：每个查询视图独立选择（用户可见文案：规范执行 / 原样执行）
//   transform = 防护 + 智能：系统规范化 SQL 语法 + 自动补行数上限（最多 1000 行），默认
//   raw       = 原样执行：按用户所写原样发库，不限制行数（用于特殊语法兜底，由用户自行负责）
export type SQLExecMode = "transform" | "raw"

// SQL 执行模式 → i18n key（文案见 locales: app.execMode.*），渲染时 t(SQL_EXEC_MODE_LABEL[x])
export const SQL_EXEC_MODE_LABEL: Record<SQLExecMode, string> = {
  transform: "app.execMode.transform",
  raw: "app.execMode.raw",
}

export interface SQLQueryRequest {
  connId: string
  db?: string // 目标库名（点对象树查表时传入，覆盖连接默认库）
  sql: string
  limit?: number
  offset?: number
  mask?: boolean // 结果集脱敏：敏感列（password/token/secret 等）统一打码
  mode?: SQLExecMode // 执行模式：transform（默认，规范执行）| raw（原样执行）
}

export interface SQLPingResult {
  ok: boolean
  elapsedMs: number
  error?: string
}

// 对象创建语句（建表/视图/函数/过程 DDL）
export type ObjectDDLType = "table" | "view" | "function" | "procedure"

export interface ObjectDDLResult {
  type: ObjectDDLType
  name: string
  ddl: string
}

export interface SQLQueryResult {
  columns: string[]
  rows: unknown[][]
  rowCount: number
  affectedRows: number
  elapsedMs: number
  sql: string
  isWrite: boolean
  warnings: string[]
  error?: string // 执行失败原因（非空时表示失败结果）
  skipped?: boolean // 未执行：因前面语句失败而跳过的语句（仅占位，无结果）
}

export interface SQLHistoryItem {
  id: string
  connId: string
  db?: string // 执行时目标库名（空=连接默认库）
  mode?: SQLExecMode // 执行模式 transform/raw
  sql: string
  isWrite: boolean
  rowCount: number
  elapsedMs: number
  status: "ok" | "error"
  error?: string
  createdAt: number
}

// 审计日志条目（安全兜底，全量、只读、不可删）
export type SQLAuditSource = "manual" | "tree" | "cell"

export interface SQLAuditEntry {
  id: string
  connId: string
  db?: string
  mode?: SQLExecMode
  source: SQLAuditSource // manual=用户手写 / tree=对象树自动查询 / cell=单元格内联编辑
  sql: string
  isWrite: boolean
  rowCount: number
  elapsedMs: number
  status: "ok" | "error"
  error?: string
  createdAt: number
  // 单元格内联编辑的结构化参数（source=cell 时才有值）
  table?: string
  column?: string
  newValue?: unknown
  pkColumns?: string[]
  pkValues?: unknown[]
}

// SQL 收藏（用户主动、跨会话、按连接隔离，不受执行历史环形上限影响）
// 仅在「全部替换」回填动作下才还原 db/mode 上下文，其余动作只插文本。
export interface SQLFavorite {
  id: string
  connId: string
  title: string // 默认取 SQL 去注释后首行前 40 字符，可重命名
  db?: string // 执行上下文：目标库（仅 replace_all 回填时还原）
  mode?: SQLExecMode // 执行模式（仅 replace_all 回填时还原）
  sql: string
  createdAt: number
}

// ---- 全局配置（config.yaml） ----

export interface DirConfig {
  data: string
  tmp: string
  uploads: string
  exports: string
  compares: string
  snapshots: string
}

export interface WebConfig {
  allow: string[] // 访问来源白名单（IP/CIDR/域名），留空 = 不限制
}

// AI 辅助 SQL 配置（OpenAI 兼容协议，可对接 OpenAI / 国产模型）
export interface AIConfig {
  baseUrl: string // OpenAI 兼容端点，如 https://api.openai.com/v1 或国内中转
  apiKey: string // API Key（保存后回显为掩码，重存时后端保留原值）
  model: string // 模型名，如 gpt-4o-mini / deepseek-chat
  temperature: number // 温度 0-2，默认 0.2
  maxTokens: number // 单次回复最大 token，默认 2048
  timeoutSec: number // 请求超时（秒），默认 60
  maxSchemaChars: number // 表结构文本字符上限，默认 20000
  systemPrompt: string // 自定义 system prompt 模板（支持 {dialect}/{schema} 占位符），留空用内置默认
}

export interface AppConfig {
  dirs: DirConfig
  web: WebConfig
  ai: AIConfig
  // 兼容排序规则：将 MySQL 8.0 特有排序规则替换为 5.7 兼容版本（全局默认）
  compatCollation: boolean
  // 全局 debug 日志开关：输出 debug 及以上级别日志（含 AI 链路），等效命令行 --debug；修改后重启服务生效
  debug: boolean
}

// AI 能力状态（后端掩码后返回，供前端门控 AI 面板）
export interface AIStatus {
  enabled: boolean
  baseUrl: string
  model: string
  temperature: number
  maxTokens: number
  timeoutSec: number
  maxSchemaChars: number
  hasPrompt: boolean
}

// AI 模型信息（名称 + 上下文窗口 + 单次回复上限）
export interface AIModelInfo {
  name: string // 模型名称
  context: number // 上下文窗口大小（K tokens）
  maxTokens: number // 单次回复最大 token（0 表示未设置）
}

// AI 厂商预设（供前端下拉选择，简化配置）
export interface AIProvider {
  id: string // 厂商标识（如 openai / deepseek / custom）
  name: string // 显示名称
  baseUrl: string // OpenAI 兼容端点
  models: AIModelInfo[] // 该厂商可用模型（含上下文窗口）
  builtin?: boolean // 是否内置厂商（仅展示用）
}

// AI token 消耗（与后端 llm.Usage 对齐）
export interface AIUsage {
  prompt_tokens: number
  completion_tokens: number
  total_tokens: number
}

export interface AISession {
  sessionID: string
  dbName: string
  dialect: string
}

// AI 会话持久化记录（与后端 store.AISessionRecord 对齐）
export interface AISessionRecord {
  id: string
  connId: string
  tabId: string
  db: string
  dialect?: string
  messages: {
    role: string
    content: string
    tool_calls?: unknown[]
    tool_call_id?: string
    tool_name?: string
    // 后端在 user 消息上附加的元信息：action 为动作类型，raw 为原始输入（纯 SQL / 需求），
    // msg_id 为该条 user 消息的发起流水号（会话内递增，幂等去重依据）
    extra?: { action?: string; raw?: string; msg_id?: string }
  }[]
  usage?: AIUsage
  createdAt: number
  updatedAt: number
}

// AI 会话元信息（列表项，不含消息体）
export interface AISessionMeta {
  id: string
  connId: string
  tabId: string
  db: string
  createdAt: number
  updatedAt: number
}

export interface ResolvedDirs {
  data: string
  tmp: string
  uploads: string
  exports: string
  compares: string
  snapshots: string
}

export interface ConfigInfo {
  config: AppConfig
  resolved: ResolvedDirs
  configFile: string // 全局配置文件路径（空 = 未发现）
  dataDirOverride: boolean // --data-dir 是否覆盖了 dirs.data（此时目录修改不生效）
}

// 目录浏览（设置页目录选择器）：后端仅返回目录、范围限制在用户主目录内
export interface DirEntry {
  name: string
  path: string
}

export interface DirBrowseResult {
  path: string // 当前浏览路径
  parent: string // 上级目录（已到范围边界时为空）
  root: string // 浏览范围根目录（用户主目录）
  entries: DirEntry[] // 子目录列表
}

export interface VersionInfo {
  version: string
  commitId?: string
  buildTime: string
  dbTypes: string[]
  /** 开源构建（后端 -tags opensource）时为 true，展示项目 Git 地址与联系方式 */
  showLinks?: boolean
}

// ---- 查询工作区（后端持久化，按连接，可重跑上下文不含结果集） ----

export interface WorkspaceTab {
  id: string
  kind: "query" | "object"
  seq?: number // 查询序号（query tab 专用）
  title?: string // 展示标题（query tab 重命名后）
  db?: string // 目标库名（空 = 连接默认库）
  sql?: string // 查询 SQL（query tab 专用）
  mode?: SQLExecMode // 执行模式（query tab 专用）
  name?: string // object tab：对象名
  objType?: ObjectDDLType // object tab：table / view / function / procedure
  subTab?: "data" | "struct" | "ddl" // object tab
  viewLayout?: TableViewLayout // object tab：表浏览视图布局（过滤/排序/列显隐/页大小）
  pinned?: boolean // 固定标签页（不参与自动淘汰）
}

// 工作区标签页设置（后端持久化）
export interface TabSettings {
  maxTabs?: number // 最大标签页数（默认 20，范围 5~100）
  evictOrder?: string[] // 淘汰优先级顺序（5 类分类的排列）
  maxTabWidth?: number // 标签页最大宽度（像素，默认 160，范围 80~300）
}

export interface WorkspaceState {
  tabs: WorkspaceTab[]
  activeId: string
  tabSettings?: TabSettings // 标签页设置
}

// ---- 数据浏览器 / 对象树 ----
export interface ObjectNode {
  name: string
  type: "db" | "table" | "view" | "function" | "procedure" | "other"
  children?: ObjectNode[]
}

// 列过滤操作符（与后端 engine.FilterOp 严格对齐）
export type FilterOp =
  | "eq" | "neq"          // 等于 / 不等于
  | "contains" | "notContains" // 包含 / 不包含
  | "startsWith" | "endsWith"  // 开头是 / 结尾是
  | "gt" | "gte" | "lt" | "lte" // 大于 / 大于等于 / 小于 / 小于等于
  | "isNull" | "isNotNull"     // 为空 / 非空（无需输入值）

// 单列过滤条件（值仅在 isNull/isNotNull 时为空）
export interface ColumnFilter {
  column: string
  op: FilterOp
  value?: unknown
}

// 单列排序规格（多列排序：按数组顺序叠加 ORDER BY，优先级从高到低）
export interface SortSpec {
  column: string
  order: "asc" | "desc"
}

export interface TableDataRequest {
  connId: string
  db: string
  table: string
  page: number
  pageSize: number
  sortSpecs?: SortSpec[] // 多列排序（按顺序叠加 ORDER BY，全局排序）
  excludeColumns?: string[] // 省略的大字段列名（二进制/超长文本，列表不取真实值）
  filters?: ColumnFilter[] // 列过滤条件（AND 叠加，后端过滤）
}

// 表浏览视图布局：跟随 object tab 持久化（过滤/排序/列显隐/页大小）。
// 页码不持久化（数据会变，恢复旧页码无意义）。
export interface TableViewLayout {
  filters?: ColumnFilter[] // 过滤条件
  sortSpecs?: SortSpec[]   // 多列排序
  hiddenColumns?: string[] // 隐藏列名
  pageSize?: number        // 页大小偏好
  frozenUntil?: string | null // 冻结边界列（该列及左侧可见列冻结；null/空 = 仅 checkbox 列冻结）
  colWidths?: Record<string, number> // 用户手动调整的列宽（列名 → px），覆盖自适应估算
}

export interface TableDataResult {
  columns: string[]
  rows: unknown[][]
  total: number // 全表总行数（-1 表示未知/统计失败）
  page: number
  pageSize: number
  excludeColumns?: string[] // 被省略的大字段列名（值为 NULL，前端渲染占位）
}

