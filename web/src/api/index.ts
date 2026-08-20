import { toast } from "sonner"
import i18n from "@/lib/i18n"
import type {
  AIProvider,
  AIStatus,
  AIUsage,
  AISession,
  AISessionMeta,
  AISessionRecord,
  AppConfig,
  CompareOptions,
  CompareResult,
  ConfigInfo,
  ConnInfo,
  DBConn,
  DBTables,
  DirBrowseResult,
  DictionaryOptions,
  ExecutionRecord,
  ExportOptions,
  ImportFileInfo,
  ImportOptions,
  MigrateOptions,
  Progress,
  SnapshotCompareOptions,
  SnapshotDetail,
  SnapshotInfo,
  TableColumn,
  TaskConfig,
  VersionInfo,
} from "@/types"

// ---- 访问令牌（Web 服务默认启用 token 认证） ----
// 优先取 URL ?token=（启动日志给出的带令牌链接）并存入 sessionStorage，
// 随后从地址栏移除令牌，避免经浏览器历史 / Referer 泄漏；
// 后续页面导航不丢令牌；SSE/下载等无法自定义请求头的场景用 ?token= 携带
function resolveToken(): string {
  const params = new URLSearchParams(window.location.search)
  const urlToken = params.get("token")
  if (urlToken) {
    sessionStorage.setItem("dbx_token", urlToken)
    params.delete("token")
    const qs = params.toString()
    window.history.replaceState(null, "", window.location.pathname + (qs ? `?${qs}` : ""))
    return urlToken
  }
  return sessionStorage.getItem("dbx_token") || ""
}

const authToken = resolveToken()

// ---- 请求语言：统一以 ?lang= query 携带（cygin FromCtx 中 query 优先于 header），
// EventSource / 下载等无法自定义请求头的场景同样生效 ----
function withLang(url: string): string {
  const sep = url.includes("?") ? "&" : "?"
  return `${url}${sep}lang=${encodeURIComponent(i18n.language)}`
}

// ---- 401 全局提示：并发请求同时失败时节流，避免刷屏；
// 调用方可能吞掉异常（静默 catch），此处保证用户始终能看到认证失败原因
let lastAuthNotify = 0
function notifyAuthError(msg: string) {
  const now = Date.now()
  if (now - lastAuthNotify < 5000) return
  lastAuthNotify = now
  toast.error(msg, { duration: 8000 })
}

// ---- fetch 封装（cygin 统一响应 {code,msg,data}，code==0 成功） ----

export async function request<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(withLang(url), {
    ...init,
    headers: {
      "Accept-Language": i18n.language,
      ...(authToken ? { "X-Auth-Token": authToken } : {}),
      ...(init?.body && !(init.body instanceof FormData) ? { "Content-Type": "application/json" } : {}),
      ...init?.headers,
    },
  })
  if (res.status === 401) {
    // 优先展示服务端具体原因（如令牌过期提示重启），解析失败时用默认引导文案
    let msg = i18n.t("api.authFailed")
    try {
      const b = await res.json()
      if (b?.msg) msg = b.msg
    } catch {
      // 忽略：响应体非 JSON 时用默认文案
    }
    notifyAuthError(msg)
    throw new Error(msg)
  }
  let body: { code: number; msg?: string; details?: string[]; data?: T }
  try {
    body = await res.json()
  } catch {
    throw new Error(i18n.t("api.requestFailedHttp", { status: res.status }))
  }
  if (body.code !== 0) {
    // 优先展示 details 里的具体错误（如数据库错误），msg 作为兜底
    const detail = (body.details ?? []).filter(Boolean).join("；")
    throw new Error(detail || body.msg || i18n.t("api.requestFailedCode", { code: body.code }))
  }
  return body.data as T
}

export const post = <T>(url: string, data: unknown) =>
  request<T>(url, { method: "POST", body: JSON.stringify(data ?? {}) })

// ---- 连接管理 ----

export const listConnections = () => request<ConnInfo[]>("/api/connections")

// 新建/更新连接（id 非空为按主键更新），返回主键
export const saveConnection = (payload: { id?: string; name: string; shortName?: string; env?: string; conn: DBConn }) =>
  post<{ id: string; name: string; shortName?: string }>("/api/connections", payload)

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

export const startCompare = (options: CompareOptions, taskConfigId?: string) =>
  post<{ taskID: string }>("/api/compare", { options, taskConfigId })

export const startDictionary = (options: DictionaryOptions, taskConfigId?: string) =>
  post<{ taskID: string }>("/api/dictionary", { options, compress: options.compress, taskConfigId })

export const getCompareResult = (taskID: string) =>
  request<CompareResult>(`/api/compare/result?taskID=${encodeURIComponent(taskID)}`)

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

export const getVersion = () => request<VersionInfo>("/api/meta/version")

// ---- 全局配置 ----

export const getConfig = () => request<ConfigInfo>("/api/config")

export const saveConfig = (config: AppConfig) =>
  request<{ ok: boolean }>("/api/config", { method: "PUT", body: JSON.stringify(config) })

export const browseDirs = (path?: string) =>
  request<DirBrowseResult>(`/api/config/browse-dirs${path ? `?path=${encodeURIComponent(path)}` : ""}`)

// ---- AI 辅助 SQL ----

export const getAIStatus = () => request<AIStatus>("/api/ai/status")

export const getAIProviders = () => request<AIProvider[]>("/api/ai/providers")

export const saveAIProviders = (providers: AIProvider[]) =>
  post<{ ok: boolean }>("/api/ai/providers/save", { providers })

export const createAISession = (connId: string, db?: string, history?: { role: string; content: string }[], tabId?: string) =>
  post<AISession>("/api/ai/sessions", { connId, db, history, tabId })

export const deleteAISession = (sessionID: string) =>
  request<{ ok: boolean }>(`/api/ai/sessions/${encodeURIComponent(sessionID)}`, { method: "DELETE" })

// 删除某连接下指定 tab 的会话（tab 关闭时调用）
export const deleteAISessionByTab = (connId: string, tabId: string) =>
  request<{ ok: boolean }>(
    `/api/ai/sessions/by-tab?connId=${encodeURIComponent(connId)}&tabId=${encodeURIComponent(tabId)}`,
    { method: "DELETE" },
  )

export const resetAISession = (sessionID: string) =>
  post<{ ok: boolean }>(`/api/ai/sessions/${encodeURIComponent(sessionID)}/reset`, {})

// 列出某连接（可选指定 tab）的会话（新→旧，仅元信息），供前端恢复历史对话
export const listAISessions = (connId: string, tabId?: string) =>
  request<{ sessions: AISessionMeta[] }>(
    `/api/ai/sessions?connId=${encodeURIComponent(connId)}${tabId ? `&tabId=${encodeURIComponent(tabId)}` : ""}`,
  )

// 读取某会话的对话历史（含 messages/usage），供前端恢复展示
export const getAISessionHistory = (sessionID: string) =>
  request<{ session: AISessionRecord }>(`/api/ai/sessions/${encodeURIComponent(sessionID)}/history`)

export const getAISessionUsage = (sessionID: string) =>
  request<{ usage: AIUsage }>(`/api/ai/sessions/${encodeURIComponent(sessionID)}/usage`)

// 进程级累计 token（服务启动以来所有会话总消耗）
export const getAIProcessUsage = () =>
  request<{ processUsage: AIUsage }>("/api/ai/usage")

export const aiChat = (sessionID: string, text: string) =>
  post<{ content: string; usage: AIUsage }>("/api/ai/chat", { sessionID, text })

// SSE 流式对话：fetch + ReadableStream 解析（打字机效果），返回终止函数。
// 事件：delta {delta} / tool {name,args} / done {usage} / error {message}
// msgId：本次发起的唯一流水号（会话内递增），后端据此幂等去重，杜绝重复消息污染上下文。
export function aiChatStream(
  sessionID: string,
  text: string,
  handlers: {
    onDelta: (delta: string) => void
    onDone: (usage: AIUsage, schemaVerified: boolean) => void
    onError: (msg: string) => void
    onTool?: (name: string, args: string) => void
  },
  // 会话失效时后端透明重建所需的上下文
  rebuildCtx?: { connId?: string; db?: string; tabId?: string; history?: { role: string; content: string; msgId?: string }[] },
  action?: string,
  msgId?: string,
): () => void {
  const ctrl = new AbortController()
  // 总超时保险丝：后端也有 ai.timeout_sec 兜底，这里多一层保护，
  // 保证任何情况下（连接挂起、后端无响应、代理异常）加载态都能被复位。
  const TIMEOUT_MS = 180_000
  let finished = false
  const finish = (msg: string) => {
    if (finished) return
    finished = true
    handlers.onError(msg)
  }
  const fuse = setTimeout(() => {
    finish(i18n.t("api.aiTimeout", { sec: TIMEOUT_MS / 1000 }))
    ctrl.abort()
  }, TIMEOUT_MS)
  void (async () => {
    try {
      const res = await fetch(withLang("/api/ai/chat/stream"), {
        method: "POST",
        headers: {
          "Content-Type": "application/json",
          ...(authToken ? { "X-Auth-Token": authToken } : {}),
        },
        body: JSON.stringify({
          sessionID,
          text,
          action,
          msgId,
          connId: rebuildCtx?.connId,
          db: rebuildCtx?.db,
          tabId: rebuildCtx?.tabId,
          history: rebuildCtx?.history,
        }),
        signal: ctrl.signal,
      })
      if (!res.ok || !res.body) {
        finish(i18n.t("api.requestFailedHttp", { status: res.status }))
        return
      }
      const reader = res.body.getReader()
      const decoder = new TextDecoder()
      let buf = ""
      for (;;) {
        const { done, value } = await reader.read()
        if (done) break
        buf += decoder.decode(value, { stream: true })
        // 按 \n\n 切分事件
        let idx: number
        while ((idx = buf.indexOf("\n\n")) >= 0) {
          const raw = buf.slice(0, idx)
          buf = buf.slice(idx + 2)
          const event = /^event: (.+)$/m.exec(raw)?.[1] ?? ""
          const data = /^data: (.+)$/m.exec(raw)?.[1]
          if (!data) continue
          try {
            const obj = JSON.parse(data)
            if (event === "delta" && obj.delta) handlers.onDelta(String(obj.delta))
            else if (event === "tool" && obj.name && handlers.onTool) handlers.onTool(String(obj.name), String(obj.args ?? ""))
            else if (event === "done") {
              finished = true
              handlers.onDone(obj.usage as AIUsage, obj.schemaVerified === true)
            } else if (event === "error") {
              finished = true
              handlers.onError(String(obj.message ?? i18n.t("api.unknownError")))
            }
          } catch {
            // 忽略无法解析的分片
          }
        }
      }
      // 连接正常关闭但未收到 done/error：视为异常中断
      if (!finished) finish(i18n.t("api.streamInterrupted"))
    } catch (e) {
      if ((e as Error).name !== "AbortError") handlers.onError((e as Error).message)
    } finally {
      clearTimeout(fuse)
    }
  })()
  return () => {
    clearTimeout(fuse)
    ctrl.abort()
  }
}

// ---- 导出文件操作 ----

export const downloadUrl = (taskID: string) =>
  `/api/export/download/${encodeURIComponent(taskID)}${authToken ? `?token=${encodeURIComponent(authToken)}` : ""}`

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
  // EventSource 无法自定义请求头，令牌与语言均通过查询参数携带
  const es = new EventSource(withLang(`/api/progress/${encodeURIComponent(taskID)}${authToken ? `?token=${encodeURIComponent(authToken)}` : ""}`))
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
      handler.onError(i18n.t("api.connClosed"))
    }
    es.close()
  })
  return () => es.close()
}

// ---- 快照 ----

export interface CreateSnapshotParams {
  connId: string
  dbNames: string[]
  name: string
  description?: string
  includeSamples: boolean
  sampleLimit?: number // 每表采样行数；<=0 走后端默认 10
}

export async function createSnapshot(params: CreateSnapshotParams): Promise<{ id: string; name: string }> {
  return post<{ id: string; name: string }>("/api/snapshots", params)
}

export async function listSnapshots(): Promise<SnapshotInfo[]> {
  return request<SnapshotInfo[]>("/api/snapshots")
}

export async function getSnapshot(id: string): Promise<SnapshotDetail> {
  return request<SnapshotDetail>(`/api/snapshots/${encodeURIComponent(id)}`)
}

export async function deleteSnapshot(id: string): Promise<void> {
  return request<void>(`/api/snapshots/${encodeURIComponent(id)}`, { method: "DELETE" })
}

export async function startSnapshotCompare(opts: SnapshotCompareOptions, taskConfigId?: string): Promise<{ taskID: string }> {
  return post<{ taskID: string }>("/api/snapshots/compare", { options: opts, taskConfigId })
}

export async function getSnapshotCompareResult(taskID: string): Promise<CompareResult> {
  return request<CompareResult>(`/api/snapshots/compare/result?taskID=${encodeURIComponent(taskID)}`)
}
