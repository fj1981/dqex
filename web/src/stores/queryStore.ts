import { create } from "zustand"
import { fetchWorkspace, runSql, saveWorkspace } from "@/api/sql"
import { describeWriteOp, isWriteSQL, previewSQL } from "@/lib/sql"
import type { ObjectDDLType, SQLExecMode, SQLQueryResult, WorkspaceTab as WorkspaceTabDTO } from "@/types"

export type WorkspaceTabKind = "query" | "object"

// 查询 tab：SQL 编辑器 + 结果网格
export interface QueryTab {
  id: string
  kind: "query"
  seq: number // 查询序号（每连接独立递增，用于标题「查询 N」）
  title: string // 展示标题 "查询 N"（由 seq 派生）
  db: string // 目标库名；空 = 使用连接默认库
  sql: string
  mode: SQLExecMode // 执行模式：transform（转换+限制，默认）| raw（原始直传）
  running: boolean
  results: SQLQueryResult[] // 多语句批量执行的结果集数组（Navicat 式）
  activeResult: number // 当前展示的结果集索引（多结果集 tab 切换）
  error: string | null
  createdAt: number
}

// 对象 tab：从对象树打开（表/视图/函数/存储过程），展示数据/结构/DDL；同一对象去重打开
export interface ObjectTab {
  id: string
  kind: "object"
  db: string
  name: string
  objType: ObjectDDLType // table / view / function / procedure
  subTab: "data" | "struct" | "ddl"
  page: number
}

export type WorkspaceTab = QueryTab | ObjectTab

interface QueryState {
  connId: string
  tabs: WorkspaceTab[]
  activeId: string
  running: boolean
  mask: boolean // 结果集脱敏开关（敏感列统一打码）
  setConnId: (connId: string) => void
  addTab: (db?: string) => void // 新建查询 tab（可选指定目标库，空 = 连接默认库）
  openObjectTab: (db: string, name: string, objType: ObjectDDLType) => void // 打开对象 tab（已存在则激活）
  closeTab: (id: string) => void
  closeOthers: (id: string) => void // 关闭除 id 外的所有 tab
  closeRight: (id: string) => void // 关闭 id 右侧的所有 tab
  closeAll: () => void // 关闭所有 tab
  renameTab: (id: string, title: string) => void // 重命名 query tab
  setActiveTab: (id: string) => void
  updateTabSql: (id: string, sql: string) => void
  updateTabDb: (id: string, db: string) => void
  updateTabMode: (id: string, mode: SQLExecMode) => void
  setObjectSubTab: (id: string, subTab: "data" | "struct" | "ddl") => void
  setObjectPage: (id: string, page: number) => void
  setMask: (mask: boolean) => void
  runActive: (selection?: string) => Promise<void>
  runTab: (id: string, selection?: string) => Promise<void>
  clearTabResult: (id: string) => void
  setActiveResult: (id: string, index: number) => void
  // 回填 SQL 到当前激活的 query tab（无 query tab 时新建一个）；供右侧 SQL 历史面板点击回填
  // db/mode 为执行时的上下文，回填时一并还原到 tab 的库选择与执行模式
  applySQL: (sql: string, db?: string, mode?: SQLExecMode) => void
}

// 全局自增仅用于生成唯一 id（不参与标题序号统计）
let idSeq = 0
const nextId = (prefix: string) => `${prefix}-${Date.now()}-${idSeq++}`

// 当前连接已有 query tab 的最大序号（object tab 不参与）；无 query tab 时返回 0
function maxQuerySeq(tabs: WorkspaceTab[]): number {
  let max = 0
  for (const t of tabs) {
    if (t.kind === "query" && t.seq > max) max = t.seq
  }
  return max
}

// 新建 query tab：序号 = 当前连接已有 query tab 最大序号 + 1（每连接独立统计）
const newQueryTab = (db = "", seq: number): QueryTab => ({
  id: nextId("query"),
  kind: "query",
  seq,
  title: `查询 ${seq}`,
  db,
  sql: "",
  mode: "transform", // 默认：转换 + 限制
  running: false,
  results: [],
  activeResult: 0,
  error: null,
  createdAt: Date.now(),
})

// ---- tab 自动持久化（后端 SQLite，按连接独立保存；query + object tab 都恢复） ----
//
// 方案 A：只持久化「可重跑」上下文（sql/db/mode/seq/title + object 定位），
// 不持久化结果集 results / 瞬时状态 running/error/activeResult —— 结果集靠重新执行恢复，
// 避免大结果集撑爆后端存储，且与「SQL 历史可回填重跑」的设计一致。

// 从内存 WorkspaceTab 转后端 DTO（剥离 results/running 等瞬时字段）
function toDTO(t: WorkspaceTab): WorkspaceTabDTO {
  if (t.kind === "query") {
    const defaultTitle = `查询 ${t.seq}`
    return {
      id: t.id,
      kind: "query",
      seq: t.seq,
      db: t.db,
      sql: t.sql,
      mode: t.mode,
      ...(t.title !== defaultTitle ? { title: t.title } : {}),
    }
  }
  return { id: t.id, kind: "object", db: t.db, name: t.name, objType: t.objType, subTab: t.subTab }
}

// 从后端 DTO 转内存 WorkspaceTab（保留原 id；结果集/瞬时状态按初始值复位）
function fromDTO(d: WorkspaceTabDTO): WorkspaceTab {
  if (d.kind === "query") {
    const base: QueryTab = {
      ...newQueryTab(d.db ?? "", d.seq ?? 0),
      id: d.id || nextId("query"),
      sql: d.sql ?? "",
    }
    if (d.title) base.title = d.title
    if (d.mode === "raw") base.mode = "raw"
    return base
  }
  return {
    id: d.id || nextId("obj"),
    kind: "object",
    db: d.db ?? "",
    name: d.name ?? "",
    objType: (d.objType ?? "table") as ObjectDDLType,
    subTab: (d.subTab ?? "data") as "data" | "struct" | "ddl",
    page: 1,
  }
}

// 从后端恢复某连接的 tabs；无记录时返回空
async function restoreConn(connId: string): Promise<{ tabs: WorkspaceTab[]; activeId: string }> {
  try {
    const state = await fetchWorkspace(connId)
    const tabs = (state.tabs ?? []).map(fromDTO)
    let activeId = state.activeId || ""
    if (!tabs.some((t) => t.id === activeId)) {
      activeId = tabs[0]?.id ?? ""
    }
    return { tabs, activeId }
  } catch {
    // 加载失败（如后端不可用）不阻塞，返回空工作区
    return { tabs: [], activeId: "" }
  }
}

// 保存当前内存中的连接状态到后端（fire-and-forget，失败不阻塞主流程）
function persistCurrent(connId: string, tabs: WorkspaceTab[], activeId: string) {
  if (!connId) return
  const state = { tabs: tabs.map(toDTO), activeId }
  saveWorkspace(connId, state).catch(() => {
    // 忽略持久化失败，不影响主流程
  })
}

export const useQueryStore = create<QueryState>((set, get) => ({
  connId: "",
  tabs: [],
  activeId: "",
  running: false,
  mask: false,

  setConnId: (connId) => {
    const { connId: prev, tabs, activeId } = get()
    if (prev === connId) return
    // 先保存旧连接的工作区（若已进入过某连接），再异步恢复新连接的工作区
    if (prev) {
      persistCurrent(prev, tabs, activeId)
    }
    set({ connId, tabs: [], activeId: "", running: false })
    // 异步恢复目标连接的工作区
    void (async () => {
      const restored = await restoreConn(connId)
      // 恢复期间用户可能已切换连接，仅在仍停留在目标连接时应用
      if (get().connId === connId) {
        set({ tabs: restored.tabs, activeId: restored.activeId })
      }
    })()
  },

  setMask: (mask) => set({ mask }),

  addTab: (db = "") => {
    const { connId, tabs } = get()
    // 序号 = 当前连接已有 query tab 最大序号 + 1（每连接独立，object tab 不参与）
    const tab = newQueryTab(db, maxQuerySeq(tabs) + 1)
    const nextTabs = [...tabs, tab]
    set({ tabs: nextTabs, activeId: tab.id })
    persistCurrent(connId, nextTabs, tab.id)
  },

  openObjectTab: (db, name, objType) => {
    const { connId, tabs } = get()
    // 去重：同一对象已有 tab 时仅激活，不重复打开
    const existing = tabs.find(
      (t) => t.kind === "object" && t.db === db && t.name === name && t.objType === objType,
    )
    if (existing) {
      set({ activeId: existing.id })
      persistCurrent(connId, tabs, existing.id)
      return
    }
    // 表/视图默认看数据，函数/存储过程只有 DDL
    const initialSubTab: ObjectTab["subTab"] = objType === "table" || objType === "view" ? "data" : "ddl"
    const tab: ObjectTab = {
      id: nextId("obj"),
      kind: "object",
      db,
      name,
      objType,
      subTab: initialSubTab,
      page: 1,
    }
    const nextTabs = [...tabs, tab]
    set({ tabs: nextTabs, activeId: tab.id })
    persistCurrent(connId, nextTabs, tab.id)
  },

  closeTab: (id) => {
    const { connId, tabs, activeId } = get()
    const target = tabs.find((t) => t.id === id)
    if (!target) return
    const idx = tabs.findIndex((t) => t.id === id)
    const next = tabs.filter((t) => t.id !== id)
    let nextActive = activeId
    if (activeId === id) {
      nextActive = next.length > 0 ? next[Math.min(idx, next.length - 1)].id : ""
    }
    set({ tabs: next, activeId: nextActive })
    persistCurrent(connId, next, nextActive)
  },

  closeOthers: (id) => {
    const { connId, tabs } = get()
    const next = tabs.filter((t) => t.id === id)
    set({ tabs: next, activeId: id })
    persistCurrent(connId, next, id)
  },

  closeRight: (id) => {
    const { connId, tabs, activeId } = get()
    const idx = tabs.findIndex((t) => t.id === id)
    if (idx < 0) return
    const next = tabs.slice(0, idx + 1)
    // 若当前激活的 tab 在被关闭的右侧，回退激活到 id
    const nextActive = tabs.slice(idx + 1).some((t) => t.id === activeId) ? id : activeId
    set({ tabs: next, activeId: nextActive })
    persistCurrent(connId, next, nextActive)
  },

  closeAll: () => {
    const { connId } = get()
    set({ tabs: [], activeId: "" })
    persistCurrent(connId, [], "")
  },

  renameTab: (id, title) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) =>
      t.id === id && t.kind === "query" ? { ...t, title: title.trim() || t.title } : t,
    ) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent(connId, next, activeId)
  },

  setActiveTab: (id) => {
    set({ activeId: id })
    const { connId, tabs } = get()
    persistCurrent(connId, tabs, id)
  },

  updateTabSql: (id, sql) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, sql } : t)) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent(connId, next, activeId)
  },

  updateTabDb: (id, db) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, db } : t)) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent(connId, next, activeId)
  },

  updateTabMode: (id, mode) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, mode } : t)) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent(connId, next, activeId)
  },

  setObjectSubTab: (id, subTab) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) =>
      t.id === id && t.kind === "object" ? { ...t, subTab, page: 1 } : t,
    ) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent(connId, next, activeId)
  },

  setObjectPage: (id, page) =>
    set((s) => ({
      tabs: s.tabs.map((t) => (t.id === id && t.kind === "object" ? { ...t, page } : t)) as WorkspaceTab[],
    })),

  clearTabResult: (id) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, results: [], activeResult: 0, error: null } : t)) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent(connId, next, activeId)
  },

  setActiveResult: (id, index) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, activeResult: index } : t)) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent(connId, next, activeId)
  },

  runActive: async (selection?: string) => {
    const { activeId } = get()
    if (activeId) await get().runTab(activeId, selection)
  },

  runTab: async (id, selection) => {
    const { connId, tabs } = get()
    const tab = tabs.find((t) => t.id === id)
    if (!tab || tab.kind !== "query" || !connId) return
    // 选中执行（Navicat 式）：有选中文本时只执行选中部分，否则执行整个编辑器内容
    const sql = (selection ?? tab.sql).trim()
    if (!sql) {
      set((s) => ({
        tabs: s.tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, error: "请输入 SQL" } : t)) as WorkspaceTab[],
      }))
      return
    }
    set((s) => ({
      running: true,
      tabs: s.tabs.map((t) =>
        t.id === id && t.kind === "query" ? { ...t, running: true, error: null } : t,
      ) as WorkspaceTab[],
    }))
    try {
      // 写操作统一确认：检测到 INSERT/UPDATE/DELETE/DDL 时弹一次确认，避免误点
      if (isWriteSQL(sql) && !window.confirm(`检测到 ${describeWriteOp(sql)} 写操作，确认执行？\n\n${previewSQL(sql)}`)) {
        set((s) => ({
          running: false,
          tabs: s.tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, running: false } : t)) as WorkspaceTab[],
        }))
        return
      }
      const mask = get().mask
      // 统一走 /run 接口（Navicat 式批量执行）：后端按分号分割多语句，逐条判断读写并执行，返回结果集数组
      const results = await runSql({ connId, db: tab.db, sql, mask, mode: tab.mode })
      const next = get().tabs.map((t) =>
        t.id === id && t.kind === "query"
          ? { ...t, running: false, results, activeResult: 0, error: null }
          : t,
      ) as WorkspaceTab[]
      set({ running: false, tabs: next })
      // 结果持久化：刷新/切换连接后恢复上次查询结果
      persistCurrent(connId, next, get().activeId)
    } catch (e) {
      set((s) => ({
        running: false,
        tabs: s.tabs.map((t) =>
          t.id === id && t.kind === "query" ? { ...t, running: false, error: (e as Error).message } : t,
        ) as WorkspaceTab[],
      }))
    }
  },

  // 回填 SQL：若当前激活的是 query tab 则直接写入其编辑器；否则新建一个 query tab 并写入。
  // db/mode 为执行上下文，一并还原到 tab 的库选择与执行模式。
  applySQL: (sql, db, mode) => {
    const { connId, tabs, activeId } = get()
    const active = tabs.find((t) => t.id === activeId)
    if (active && active.kind === "query") {
      const next = tabs.map((t) =>
        t.id === activeId
          ? {
              ...t,
              sql,
              ...(db !== undefined ? { db } : {}),
              ...(mode ? { mode } : {}),
            }
          : t,
      ) as WorkspaceTab[]
      set({ tabs: next })
      persistCurrent(connId, next, activeId)
      return
    }
    // 无激活 query tab：新建一个（序号递增），并带入上下文
    const tab = newQueryTab(db ?? "", maxQuerySeq(tabs) + 1)
    tab.sql = sql
    if (mode) tab.mode = mode
    const next = [...tabs, tab]
    set({ tabs: next, activeId: tab.id })
    persistCurrent(connId, next, tab.id)
  },
}))
