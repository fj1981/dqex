import type {
  ConnInfo,
  DBConn,
  DBTables,
  ExecutionRecord,
  ExportOptions,
  ImportFileInfo,
  ImportOptions,
  MigrateOptions,
  Progress,
  TableColumn,
  TaskConfig,
} from "@/types"

// ---- fetch 封装（cygin 统一响应 {code,msg,data}，code==0 成功） ----

async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: {
      "Accept-Language": "zh",
      ...(init?.body && !(init.body instanceof FormData) ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  })
  let body: { code: number; msg?: string; data?: T }
  try {
    body = await res.json()
  } catch {
    throw new Error(`请求失败 (HTTP ${res.status})`)
  }
  if (body.code !== 0) {
    throw new Error(body.msg || `请求失败 (code=${body.code})`)
  }
  return body.data as T
}

const post = <T>(url: string, data: unknown) =>
  request<T>(url, { method: "POST", body: JSON.stringify(data ?? {}) })

// ---- 连接管理 ----

export const listConnections = () => request<ConnInfo[]>("/api/connections")

// 新建/更新连接（id 非空为按主键更新），返回主键
export const saveConnection = (payload: { id?: string; name: string; conn: DBConn }) =>
  post<{ id: string; name: string }>("/api/connections", payload)

export const deleteConnection = (id: string) =>
  request<{ ok: boolean }>(`/api/connections/${encodeURIComponent(id)}`, { method: "DELETE" })

export const testConnection = (payload: { id?: string; conn?: DBConn }) =>
  post<{ ok: boolean }>("/api/connections/test", payload)

export const getTableTree = (id: string, db?: string) =>
  request<{ databases: DBTables[] }>(`/api/connections/${encodeURIComponent(id)}/tables${db ? `?db=${encodeURIComponent(db)}` : ""}`)

export const getTableColumns = (id: string, db: string, table: string) =>
  request<{ columns: TableColumn[] }>(`/api/connections/${encodeURIComponent(id)}/columns?db=${encodeURIComponent(db)}&table=${encodeURIComponent(table)}`)

// ---- 导出 / 导入 / 迁移 ----

export const startExport = (options: ExportOptions, taskConfigId?: string) =>
  post<{ taskID: string }>("/api/export", { options, compress: options.compress, taskConfigId })

export const startImport = (options: ImportOptions, taskConfigId?: string) =>
  post<{ taskID: string }>("/api/import", { options, backup: options.backup, taskConfigId })

export const startMigrate = (options: MigrateOptions, taskConfigId?: string) =>
  post<{ taskID: string }>("/api/migrate", { options, backup: options.backup, taskConfigId })

export const uploadImportFile = async (file: File): Promise<{ path: string; name?: string; info?: ImportFileInfo }> => {
  const form = new FormData()
  form.append("file", file)
  return request<{ path: string; name?: string; info?: ImportFileInfo }>("/api/import/upload", { method: "POST", body: form })
}

export const inspectImportFile = (path: string) =>
  post<ImportFileInfo>("/api/import/inspect", { path })

export const cancelTask = (taskID: string) =>
  post<{ ok: boolean }>(`/api/cancel/${encodeURIComponent(taskID)}`, {})

// ---- 任务配置 ----

export const listTasks = (type?: string) =>
  request<TaskConfig[]>(`/api/tasks${type ? `?type=${type}` : ""}`)

export const saveTask = (task: Partial<TaskConfig>) =>
  post<TaskConfig>("/api/tasks", task)

export const getTask = (id: string) =>
  request<TaskConfig>(`/api/tasks/detail?id=${encodeURIComponent(id)}`)

export const updateTask = (task: TaskConfig) =>
  request<TaskConfig>("/api/tasks/update", { method: "PUT", body: JSON.stringify(task) })

export const deleteTask = (id: string) =>
  request<{ ok: boolean }>(`/api/tasks?id=${encodeURIComponent(id)}`, { method: "DELETE" })

export const getLastTask = (type: string) =>
  request<{ task: TaskConfig | null }>(`/api/tasks/last/${type}`)

export const runTask = (id: string) =>
  post<{ taskID: string }>("/api/tasks/run", { id })

// ---- 执行历史 ----

export const listHistory = (params?: { type?: string; taskConfigId?: string }) => {
  const qs = new URLSearchParams()
  if (params?.type) qs.set("type", params.type)
  if (params?.taskConfigId) qs.set("taskConfigId", params.taskConfigId)
  const q = qs.toString()
  return request<ExecutionRecord[]>(`/api/history${q ? `?${q}` : ""}`)
}

export const getHistory = (taskID: string) =>
  request<ExecutionRecord>(`/api/history/${encodeURIComponent(taskID)}`)

// ---- 元数据 ----

export const getDBTypes = () =>
  request<{ types: Record<string, string[]> }>("/api/meta/dbtypes")

// ---- 导出文件操作 ----

export const downloadUrl = (taskID: string) => `/api/export/download/${encodeURIComponent(taskID)}`

export const openExportDir = (taskID: string) =>
  post<{ ok: boolean }>(`/api/export/open-dir/${encodeURIComponent(taskID)}`, {})

export const deleteHistory = (taskID: string) =>
  post<{ ok: boolean }>(`/api/history/del/${encodeURIComponent(taskID)}`, {})

// ---- SSE 进度订阅 ----

export interface ProgressHandler {
  onProgress: (p: Progress) => void
  onDone: (p: Progress) => void
  onError: (msg: string) => void
}

export function subscribeProgress(taskID: string, handler: ProgressHandler): () => void {
  const es = new EventSource(`/api/progress/${encodeURIComponent(taskID)}`)
  es.addEventListener("progress", (e) => {
    try {
      handler.onProgress(JSON.parse(e.data))
    } catch { /* ignore */ }
  })
  es.addEventListener("done", (e) => {
    try {
      handler.onDone(JSON.parse(e.data))
    } catch {
      handler.onDone({} as Progress)
    }
    es.close()
  })
  es.addEventListener("error", (e) => {
    const me = e as MessageEvent
    if (me.data) {
      handler.onError(String(me.data))
    } else if (es.readyState === EventSource.CLOSED) {
      handler.onError("连接已断开")
    }
    es.close()
  })
  return () => es.close()
}
