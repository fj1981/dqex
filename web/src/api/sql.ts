import { post, request } from "@/api"
import type {
  ObjectDDLResult,
  ObjectDDLType,
  ObjectNode,
  SQLAuditEntry,
  SQLHistoryItem,
  SQLPingResult,
  SQLQueryRequest,
  SQLQueryResult,
  TableDataRequest,
  TableDataResult,
  WorkspaceState,
} from "@/types"

// ---- SQL 查询终端 ----

export const querySql = (payload: SQLQueryRequest) =>
  post<SQLQueryResult>("/api/sql/query", payload)

// 统一执行（Navicat 式）：支持多语句批量执行 + 选中执行，返回结果集数组。
// 写操作确认由调用方（store）在发送前完成。
export const runSql = (payload: SQLQueryRequest) =>
  post<SQLQueryResult[]>("/api/sql/run", payload)

export const pingConnection = (connId: string) =>
  post<SQLPingResult>("/api/sql/ping", { connId })

export const execSql = (payload: { connId: string; db?: string; sql: string }) =>
  post<SQLQueryResult>("/api/sql/exec", payload)

export const fetchSqlHistory = (connId: string) =>
  request<SQLHistoryItem[]>(`/api/sql/history?connId=${encodeURIComponent(connId)}`)

export const clearSqlHistory = (connId: string) =>
  request<{ ok: boolean }>(`/api/sql/history?connId=${encodeURIComponent(connId)}`, { method: "DELETE" })

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
    body: JSON.stringify({ connId, tabs: state.tabs, activeId: state.activeId }),
  })

// 获取对象创建语句（表/视图/函数/存储过程 DDL）
export const getObjectDDL = (connId: string, db: string, type: ObjectDDLType, name: string) =>
  request<ObjectDDLResult>(
    `/api/sql/ddl?connId=${encodeURIComponent(connId)}&db=${encodeURIComponent(db)}&type=${encodeURIComponent(type)}&name=${encodeURIComponent(name)}`,
  )

// ---- 数据浏览器 / 对象树（复用连接表树接口） ----

// 从连接表树接口转换为对象树。
// 后端 objects 的 key 为 _views/_functions/_procedures（带下划线前缀，见 engine/objects.go objectKindDirs）
export const buildObjectTree = (databases: { name: string; tables: string[]; objects?: Record<string, string[]> }[]): ObjectNode[] =>
  databases.map((db) => ({
    name: db.name,
    type: "db",
    children: [
      ...(db.tables ?? []).map((t) => ({ name: t, type: "table" as const })),
      ...(db.objects?.["_views"] ?? []).map((v) => ({ name: v, type: "view" as const })),
      ...(db.objects?.["_functions"] ?? []).map((f) => ({ name: f, type: "function" as const })),
      ...(db.objects?.["_procedures"] ?? []).map((p) => ({ name: p, type: "procedure" as const })),
    ],
  }))

// 查询表数据（通过 SQL 终端生成分页 SELECT，支持全局排序）
export const fetchTableData = async (req: TableDataRequest): Promise<TableDataResult> => {
  const offset = (req.page - 1) * req.pageSize
  // 排序列名用反引号引用（防注入），ORDER BY 放在 LIMIT 之前
  const orderBy = req.sortColumn && req.sortOrder
    ? ` ORDER BY \`${req.sortColumn.replace(/`/g, "")}\` ${req.sortOrder === "desc" ? "DESC" : "ASC"}`
    : ""
  const sql = `SELECT * FROM \`${req.table}\`${orderBy} LIMIT ${req.pageSize} OFFSET ${offset}`
  // recordHistory: false —— 对象树点开自动生成的浏览查询，不写入 SQL 执行历史
  const result = await querySql({ connId: req.connId, db: req.db, sql, limit: req.pageSize, offset, recordHistory: false })
  // 执行失败时后端以 result.error 返回（HTTP 200），此处抛错交给调用方内联展示
  if (result.error) throw new Error(result.error)
  return {
    columns: result.columns ?? [],
    rows: result.rows ?? [],
    total: -1, // 全表总行数由单独 count 查询提供
    page: req.page,
    pageSize: req.pageSize,
  }
}

// 统计表总行数
export const countTableRows = async (connId: string, db: string, table: string): Promise<number> => {
  const sql = `SELECT COUNT(*) AS cnt FROM \`${table}\``
  // recordHistory: false —— 对象树点开自动生成的统计查询，不写入 SQL 执行历史
  const result = await querySql({ connId, db, sql, limit: 1, recordHistory: false })
  if (result.error) return -1
  if (!result.rows?.length) return 0
  const v = result.rows[0][0]
  return typeof v === "number" ? v : Number(v) || 0
}

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
