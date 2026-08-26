import i18n from "@/lib/i18n"
import { post, request } from "@/api"
import type {
  ColumnFilter,
  DBSchema,
  DBTables,
  GenSQLKind,
  ObjectDDLResult,
  ObjectDDLType,
  ObjectNode,
  SortSpec,
  SQLAuditEntry,
  SQLExecMode,
  SQLFavorite,
  SQLHistoryItem,
  SQLPingResult,
  SQLQueryRequest,
  SQLQueryResult,
  TableDataRequest,
  TableDataResult,
  WorkspaceState,
} from "@/types"

// ---- SQL 查询终端 ----

// 统一执行（Navicat 式）：支持多语句批量执行 + 选中执行，返回结果集数组。
// 写操作确认由调用方（store）在发送前完成。
export const runSql = (payload: SQLQueryRequest) =>
  post<SQLQueryResult[]>("/api/sql/run", payload)

export const pingConnection = (connId: string) =>
  post<SQLPingResult>("/api/sql/ping", { connId })

export const fetchSqlHistory = (connId: string) =>
  request<SQLHistoryItem[]>(`/api/sql/history?connId=${encodeURIComponent(connId)}`)

export const clearSqlHistory = (connId: string) =>
  request<{ ok: boolean }>(`/api/sql/history?connId=${encodeURIComponent(connId)}`, { method: "DELETE" })

export const listFavorites = () =>
  request<SQLFavorite[]>(`/api/sql/favorites`, { method: "GET" })

export const addFavorite = (connId: string, params: {
  sql: string
  db?: string
  mode?: SQLExecMode
  title?: string
}) =>
  post<{ ok: boolean; id: string }>(`/api/sql/favorites?connId=${encodeURIComponent(connId)}`, {
    connId,
    ...params,
  })

export const deleteFavorite = (id: string) =>
  request<{ ok: boolean }>(`/api/sql/favorites?id=${encodeURIComponent(id)}`, { method: "DELETE" })

export const renameFavorite = (id: string, title: string) =>
  post<{ ok: boolean }>(`/api/sql/favorites`, {
    id,
    title,
  })

// 审计日志（只读）：分页读取，按连接过滤；connId 为空 = 全部连接
export const fetchSqlAudit = (connId: string, limit = 100, offset = 0) =>
  request<SQLAuditEntry[]>(
    `/api/sql/audit?connId=${encodeURIComponent(connId)}&limit=${limit}&offset=${offset}`,
  )

// ---- 查询工作区（后端持久化，按连接） ----

export const fetchWorkspace = (connId: string) =>
  request<WorkspaceState>(`/api/sql/workspace?connId=${encodeURIComponent(connId)}`)

export const saveWorkspace = (connId: string, state: WorkspaceState) =>
  request<{ ok: boolean }>(`/api/sql/workspace`, {
    method: "PUT",
    body: JSON.stringify({
      connId,
      tabs: state.tabs,
      activeId: state.activeId,
      tabSettings: state.tabSettings,
    }),
  })

// 获取对象创建语句（表/视图/函数/存储过程 DDL）
export const getObjectDDL = (connId: string, db: string, type: ObjectDDLType, name: string) =>
  request<ObjectDDLResult>(
    `/api/sql/ddl?connId=${encodeURIComponent(connId)}&db=${encodeURIComponent(db)}&type=${encodeURIComponent(type)}&name=${encodeURIComponent(name)}`,
  )

// ---- 数据浏览器 / 对象树（复用连接表树接口） ----

// 从连接表树接口转换为对象树。
// 后端 objects 的 key 为 _views/_functions/_procedures（带下划线前缀，见 engine/objects.go objectKindDirs）。
// PG 系返回 schemas 分层（库 → schema → 分组 → 对象），叶子名称带 "schema." 前缀，
// 打开对象时可直接作为限定名传给后端；MySQL/Oracle 保持 库 → 分组 → 对象 结构。
export const buildObjectTree = (databases: { name: string; tables: string[]; objects?: Record<string, string[]>; schemas?: DBSchema[] }[]): ObjectNode[] =>
  databases.map((db) => ({
    name: db.name,
    type: "db",
    children: db.schemas?.length
      ? db.schemas.map((s) => ({
          name: s.name,
          type: "schema" as const,
          children: [
            ...(s.tables ?? []).map((t) => ({ name: `${s.name}.${t}`, type: "table" as const })),
            ...(s.objects?.["_views"] ?? []).map((v) => ({ name: `${s.name}.${v}`, type: "view" as const })),
            ...(s.objects?.["_functions"] ?? []).map((f) => ({ name: `${s.name}.${f}`, type: "function" as const })),
            ...(s.objects?.["_procedures"] ?? []).map((p) => ({ name: `${s.name}.${p}`, type: "procedure" as const })),
          ],
        }))
      : [
          ...(db.tables ?? []).map((t) => ({ name: t, type: "table" as const })),
          ...(db.objects?.["_views"] ?? []).map((v) => ({ name: v, type: "view" as const })),
          ...(db.objects?.["_functions"] ?? []).map((f) => ({ name: f, type: "function" as const })),
          ...(db.objects?.["_procedures"] ?? []).map((p) => ({ name: p, type: "procedure" as const })),
        ],
  }))

// ---- 对象树分级加载构建辅助 ----

// buildDbStub 未加载的库节点（灰显，点击加载）
export const buildDbStub = (name: string): ObjectNode => ({ name, type: "db", loaded: false })

// buildSchemaStub 未加载的 schema 节点（灰显 + 表计数，点击加载）
export const buildSchemaStub = (name: string, tableCount: number): ObjectNode => ({
  name,
  type: "schema",
  count: tableCount,
  loaded: false,
})

// buildSchemaLeafs 已加载 schema 的叶子节点（表/视图/函数/过程，限定名）
export const buildSchemaLeafs = (s: DBSchema): ObjectNode[] => [
  ...(s.tables ?? []).map((t) => ({ name: `${s.name}.${t}`, type: "table" as const })),
  ...(s.objects?.["_views"] ?? []).map((v) => ({ name: `${s.name}.${v}`, type: "view" as const })),
  ...(s.objects?.["_functions"] ?? []).map((f) => ({ name: `${s.name}.${f}`, type: "function" as const })),
  ...(s.objects?.["_procedures"] ?? []).map((p) => ({ name: `${s.name}.${p}`, type: "procedure" as const })),
]

// buildDbLeafs 已加载库的非 PG 叶子节点（MySQL/Oracle 无 schema 层，裸名）
export const buildDbLeafs = (db: DBTables): ObjectNode[] => [
  ...(db.tables ?? []).map((t) => ({ name: t, type: "table" as const })),
  ...(db.objects?.["_views"] ?? []).map((v) => ({ name: v, type: "view" as const })),
  ...(db.objects?.["_functions"] ?? []).map((f) => ({ name: f, type: "function" as const })),
  ...(db.objects?.["_procedures"] ?? []).map((p) => ({ name: p, type: "procedure" as const })),
]

// buildDbNode 已加载库节点：PG 系 children 为 schema 节点；MySQL/Oracle 为分组叶子
export const buildDbNode = (db: DBTables): ObjectNode => ({
  name: db.name,
  type: "db",
  loaded: true,
  children: db.schemas?.length
    ? db.schemas.map((s) => ({ name: s.name, type: "schema" as const, loaded: true, children: buildSchemaLeafs(s) }))
    : buildDbLeafs(db),
})

// 查询表数据：走专用分页接口，一次返回当前页数据与全表总行数（total）。
// 后端内部用 cydb 的 SQLStmt.Count 计算总数，不写审计/历史，避免二次 COUNT 的审计冗余。
export const fetchTableData = async (req: TableDataRequest): Promise<TableDataResult> => {
  const result = await post<TableDataResult>("/api/sql/table", {
    connId: req.connId,
    db: req.db,
    table: req.table,
    page: req.page,
    pageSize: req.pageSize,
    sortSpecs: req.sortSpecs,
    excludeColumns: req.excludeColumns,
    filters: req.filters,
  })
  return result
}

// 表数据导出 Excel：直接 fetch 返回文件流（响应非 JSON，不走 request 封装）。
// 成功时触发浏览器下载；失败时解析 cygin 错误响应抛错。
export const exportTableExcel = async (req: TableDataRequest, maxRows = 100000): Promise<void> => {
  const authToken = sessionStorage.getItem("dbx_token") || "" // 与 api/index.ts 的 resolveToken 保持一致
  const res = await fetch("/api/sql/table-export", {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
      ...(authToken ? { "X-Auth-Token": authToken } : {}),
    },
    body: JSON.stringify({
      connId: req.connId,
      db: req.db,
      table: req.table,
      sortSpecs: req.sortSpecs,
      filters: req.filters,
      maxRows,
    }),
  })
  if (!res.ok) {
    // 尝试解析错误响应
    try {
      const b = await res.json()
      const detail = (b?.details ?? []).filter(Boolean).join("；")
      throw new Error(detail || b?.msg || i18n.t("api.exportFailed", { status: res.status }))
    } catch (e) {
      if (e instanceof Error && e.message) throw e
      throw new Error(i18n.t("api.exportFailed", { status: res.status }))
    }
  }
  const blob = await res.blob()
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = `${req.table}.xlsx`
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

// 表浏览大字段单元格取值请求（按主键 + 列名定位单行单列）
export interface CellValuePayload {
  connId: string
  db: string
  table: string
  column: string
  pkColumns: string[]
  pkValues: unknown[]
}

// 获取单个大字段单元格的完整值（懒加载）
export const fetchTableCellValue = (payload: CellValuePayload) =>
  post<{ value: unknown }>("/api/sql/cell-value", payload)

// 表浏览单元格更新请求（named bind 更新）
export interface UpdateCellPayload {
  connId: string
  db: string
  table: string
  column: string
  value: unknown
  pkColumns: string[]
  pkValues: unknown[]
}

// 更新表浏览中的单个单元格，返回影响行数
export const updateTableCell = (payload: UpdateCellPayload) =>
  post<{ affectedRows: number }>("/api/sql/cell", payload)

// 表浏览整行删除请求（按主键定位，支持批量）
export interface DeleteRowsPayload {
  connId: string
  db: string
  table: string
  pkColumns: string[]
  rows: unknown[][] // 每行主键值数组（与 pkColumns 顺序一致）
}

// 删除表浏览中选中的整行（批量），返回累计影响行数
export const deleteTableRows = (payload: DeleteRowsPayload) =>
  post<{ affectedRows: number }>("/api/sql/delete-rows", payload)

// 表浏览新增行请求（用户显式填写的列与值）
export interface InsertRowPayload {
  connId: string
  db: string
  table: string
  columns: string[]
  values: unknown[]
}

// 表浏览新增一行（INSERT），返回影响行数
export const insertTableRow = (payload: InsertRowPayload) =>
  post<{ affectedRows: number }>("/api/sql/insert-row", payload)

// 表浏览快速生成 SQL 请求（右键菜单 → 后端按方言生成可执行语句，生成不执行）
export interface GenSQLPayload {
  connId: string
  db: string
  table: string
  kind: GenSQLKind
  columns: string[]
  rows: unknown[][] // 行数据（insert 支持多行；whereCell 时 [[值]]）
  pkColumns?: string[] // 主键列名（update/delete/selectByPk）
  skipColumns?: string[] // 跳过的列（insert 的自增列）
  filters?: ColumnFilter[] // 过滤条件（selectByFilter）
  sortSpecs?: SortSpec[] // 排序（selectByFilter）
}

// 快速生成 SQL 文本（方言正确的可执行语句，多条以分号换行分隔）
export const generateSQL = (payload: GenSQLPayload) =>
  post<{ sql: string }>("/api/sql/generate", payload)
