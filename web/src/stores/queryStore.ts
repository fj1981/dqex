import { create } from "zustand"
import { toast } from "sonner"
import { confirm } from "@/components/ui/alert-dialog"
import { deleteAISessionByTab } from "@/api"
import { fetchWorkspace, runSql, saveWorkspace } from "@/api/sql"
import { describeWriteOp, isWriteSQL, previewSQL } from "@/lib/sql"
import i18n from "@/lib/i18n"
import { getSqlEditor } from "@/lib/editorRef"
import { loadQueryResult, removeQueryResult, resultCacheKey, saveQueryResult } from "@/lib/queryResultCache"
import type { ObjectDDLType, SQLExecMode, SQLQueryResult, TableViewLayout, WorkspaceTab as WorkspaceTabDTO } from "@/types"

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
  lastExecSql: string // 上次实际执行的 SQL 文本（选中执行=选中部分，全文执行=全文）；不持久化，供「修复/解释/优化」定位作用对象
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
  viewLayout?: TableViewLayout // 表浏览视图布局（过滤/排序/列显隐/页大小），随 tab 持久化
}

export type WorkspaceTab = QueryTab | ObjectTab

interface QueryState {
  connId: string
  tabs: WorkspaceTab[]
  activeId: string
  running: boolean
  mask: boolean // 结果集脱敏开关（敏感列统一打码）
  persistFailed: boolean // 工作区持久化失败标记（用于 UI 提示，下次成功保存时清除）
  clearPersistFailed: () => void
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
  setObjectViewLayout: (id: string, layout: TableViewLayout) => void
  setMask: (mask: boolean) => void
  runActive: (selection?: string) => Promise<void>
  runTab: (id: string, selection?: string) => Promise<void>
  clearTabResult: (id: string) => void
  setActiveResult: (id: string, index: number) => void
  // 回填 SQL 到当前激活的 query tab（无 query tab 时新建一个）；供右侧 SQL 历史面板点击回填
  // db/mode 为执行时的上下文，回填时一并还原到 tab 的库选择与执行模式
  applySQL: (sql: string, db?: string, mode?: SQLExecMode) => void
  // 按回填动作应用 SQL（与 AI 面板一致）；仅 replace_all 还原 db/mode，其余动作只插文本
  applySQLByAction: (
    sql: string,
    db: string | undefined,
    mode: SQLExecMode | undefined,
    action: "replace_all" | "replace_selection" | "insert_cursor" | "append",
  ) => void
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
  title: i18n.t("query.tabTitle", { n: seq }),
  db,
  sql: "",
  mode: "transform", // 默认：转换 + 限制
  running: false,
  results: [],
  activeResult: 0,
  error: null,
  lastExecSql: "",
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
    const defaultTitle = i18n.t("query.tabTitle", { n: t.seq })
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
  return {
    id: t.id,
    kind: "object",
    db: t.db,
    name: t.name,
    objType: t.objType,
    subTab: t.subTab,
    ...(t.viewLayout ? { viewLayout: t.viewLayout } : {}),
  }
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
    ...(d.viewLayout ? { viewLayout: d.viewLayout } : {}),
  }
}

// 从后端恢复某连接的 tabs；无记录时返回空。
// query tab 恢复后，异步从本地 IndexedDB 缓存回填结果集（刷新/重开连接不丢结果、不重跑）。
async function restoreConn(connId: string): Promise<{ tabs: WorkspaceTab[]; activeId: string }> {
  try {
    const state = await fetchWorkspace(connId)
    const tabs = (state.tabs ?? []).map(fromDTO)
    let activeId = state.activeId || ""
    if (!tabs.some((t) => t.id === activeId)) {
      activeId = tabs[0]?.id ?? ""
    }
    // 异步回填 query tab 的结果集缓存（不阻塞工作区恢复）
    void (async () => {
      for (const t of tabs) {
        if (t.kind !== "query" || !t.sql) continue
        const cached = await loadQueryResult(connId, t.db, t.sql)
        if (cached) {
          useQueryStore.setState((s) => ({
            tabs: s.tabs.map((x) => (x.id === t.id && x.kind === "query" ? { ...x, results: cached, activeResult: 0 } : x)),
          }))
        }
      }
    })()
    return { tabs, activeId }
  } catch {
    // 加载失败（如后端不可用）不阻塞，返回空工作区
    return { tabs: [], activeId: "" }
  }
}

// 批量清理被关闭 query tab 的本地结果缓存 + 后端 AI 会话（均随 tab 生命周期，关闭即失效）。
function cleanupResultCaches(connId: string, tabs: WorkspaceTab[]) {
  if (!connId) return
  for (const t of tabs) {
    if (t.kind !== "query") continue
    if (t.sql) {
      removeQueryResult(resultCacheKey(connId, t.db, t.sql))
    }
    // 关闭 query tab 时删除其对应的 AI 会话（按 tab 隔离，关闭即清除，不留孤儿记录）
    void deleteAISessionByTab(connId, t.id).catch(() => {})
  }
}

// 立即落盘：直接把给定快照写入后端（fire-and-forget，失败不阻塞主流程）。
// 仅用于「切换连接前保存旧连接」等必须即时写入、不能等待防抖的场景。
function persistNow(connId: string, tabs: WorkspaceTab[], activeId: string) {
  if (!connId) return
  const state = { tabs: tabs.map(toDTO), activeId }
  saveWorkspace(connId, state).catch((e) => {
    // 持久化失败不阻塞主流程，但记录告警并标记状态，便于用户感知排查
    console.warn(`[workspace] 工作区持久化失败 conn=${connId}:`, e)
    useQueryStore.setState({ persistFailed: true })
  })
}

// 统一的工作区持久化防抖：停顿 300ms 后才真正落盘。
// 一次用户操作（如点击对象）会触发一串连续的状态更新（openObjectTab → setActiveTab → setObjectViewLayout），
// 若每次都立即落盘，会在几十毫秒内对同一连接发出多条相同的 PUT /workspace。
// 这里统一走防抖：窗口内多次变更只落盘一次（回调时重新读最新 store 状态，不会被旧快照覆盖）。
let persistTimer: ReturnType<typeof setTimeout> | null = null
function persistCurrent() {
  if (persistTimer) clearTimeout(persistTimer)
  persistTimer = setTimeout(() => {
    persistTimer = null
    const { connId, tabs, activeId } = useQueryStore.getState()
    persistNow(connId, tabs, activeId)
  }, 300)
}

export const useQueryStore = create<QueryState>((set, get) => ({
  connId: "",
  tabs: [],
  activeId: "",
  running: false,
  mask: false,
  persistFailed: false,

  clearPersistFailed: () => set({ persistFailed: false }),

  setConnId: (connId) => {
    const { connId: prev, tabs, activeId } = get()
    if (prev === connId) return
    // 清掉 SQL 输入的防抖持久化，避免窗口期结束后误把旧连接快照写进新连接
    if (persistTimer) {
      clearTimeout(persistTimer)
      persistTimer = null
    }
    // 先保存旧连接的工作区（若已进入过某连接），再异步恢复新连接的工作区
    // 必须立即落盘：防抖窗口内 get() 已切到新连接，不能用 persistCurrent（会读错连接）
    if (prev) {
      persistNow(prev, tabs, activeId)
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
    persistCurrent()
  },

  openObjectTab: (db, name, objType) => {
    const { connId, tabs } = get()
    // 去重：同一对象已有 tab 时仅激活，不重复打开
    const existing = tabs.find(
      (t) => t.kind === "object" && t.db === db && t.name === name && t.objType === objType,
    )
    if (existing) {
      set({ activeId: existing.id })
      persistCurrent()
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
    persistCurrent()
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
    // 关闭 query tab 时清理其本地结果缓存 + 后端 AI 会话
    cleanupResultCaches(connId, [target])
    set({ tabs: next, activeId: nextActive })
    persistCurrent()
  },

  closeOthers: (id) => {
    const { connId, tabs } = get()
    const removed = tabs.filter((t) => t.id !== id)
    const next = tabs.filter((t) => t.id === id)
    cleanupResultCaches(connId, removed)
    set({ tabs: next, activeId: id })
    persistCurrent()
  },

  closeRight: (id) => {
    const { connId, tabs, activeId } = get()
    const idx = tabs.findIndex((t) => t.id === id)
    if (idx < 0) return
    const next = tabs.slice(0, idx + 1)
    const removed = tabs.slice(idx + 1)
    cleanupResultCaches(connId, removed)
    // 若当前激活的 tab 在被关闭的右侧，回退激活到 id
    const nextActive = tabs.slice(idx + 1).some((t) => t.id === activeId) ? id : activeId
    set({ tabs: next, activeId: nextActive })
    persistCurrent()
  },

  closeAll: () => {
    const { connId, tabs } = get()
    cleanupResultCaches(connId, tabs)
    set({ tabs: [], activeId: "" })
    persistCurrent()
  },

  renameTab: (id, title) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) =>
      t.id === id && t.kind === "query" ? { ...t, title: title.trim() || t.title } : t,
    ) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent()
  },

  setActiveTab: (id) => {
    set({ activeId: id })
    const { connId, tabs } = get()
    persistCurrent()
  },

  updateTabSql: (id, sql) => {
    const { tabs } = get()
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, sql } : t)) as WorkspaceTab[]
    set({ tabs: next })
    // 统一走防抖持久化（SQL 输入与结构操作共用同一防抖窗口）
    persistCurrent()
  },

  updateTabDb: (id, db) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, db } : t)) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent()
  },

  updateTabMode: (id, mode) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, mode } : t)) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent()
  },

  setObjectSubTab: (id, subTab) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) =>
      t.id === id && t.kind === "object" ? { ...t, subTab, page: 1 } : t,
    ) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent()
  },

  setObjectPage: (id, page) =>
    set((s) => ({
      tabs: s.tabs.map((t) => (t.id === id && t.kind === "object" ? { ...t, page } : t)) as WorkspaceTab[],
    })),

  setObjectViewLayout: (id, layout) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) =>
      t.id === id && t.kind === "object" ? { ...t, viewLayout: layout } : t,
    ) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent()
  },

  clearTabResult: (id) => {
    const { connId, tabs, activeId } = get()
    const target = tabs.find((t) => t.id === id)
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, results: [], activeResult: 0, error: null } : t)) as WorkspaceTab[]
    set({ tabs: next })
    // 清空结果时同步删除本地缓存（结果与 SQL 绑定，SQL 未变则缓存键不变，需主动删除）
    if (target && target.kind === "query" && target.sql) {
      removeQueryResult(resultCacheKey(connId, target.db, target.sql))
    }
    persistCurrent()
  },

  setActiveResult: (id, index) => {
    const { connId, tabs, activeId } = get()
    const next = tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, activeResult: index } : t)) as WorkspaceTab[]
    set({ tabs: next })
    persistCurrent()
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
        tabs: s.tabs.map((t) => (t.id === id && t.kind === "query" ? { ...t, error: i18n.t("query.enterSQL") } : t)) as WorkspaceTab[],
      }))
      return
    }
    set((s) => ({
      running: true,
      tabs: s.tabs.map((t) =>
        t.id === id && t.kind === "query" ? { ...t, running: true, error: null, results: [], activeResult: 0 } : t,
      ) as WorkspaceTab[],
    }))
    try {
      // 写操作统一确认：检测到 INSERT/UPDATE/DELETE/DDL 时弹一次确认，避免误点
      if (isWriteSQL(sql) && !(await confirm({ title: i18n.t("query.confirmWriteTitle"), description: i18n.t("query.confirmWriteDesc", { op: describeWriteOp(sql), sql: previewSQL(sql) }), confirmText: i18n.t("query.confirmExec"), danger: true }))) {
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
          ? { ...t, running: false, results, activeResult: 0, error: null, lastExecSql: sql }
          : t,
      ) as WorkspaceTab[]
      set({ running: false, tabs: next })
      // 结果集写本地 IndexedDB 缓存（后端只持久化 tab 上下文；结果靠本地缓存刷新后恢复）
      saveQueryResult(connId, tab.db, sql, results)
      // 持久化 tab 的 SQL 上下文（结果集不落盘：toDTO 已剥离 results）
      persistCurrent()
    } catch (e) {
      set((s) => ({
        running: false,
        tabs: s.tabs.map((t) =>
          t.id === id && t.kind === "query" ? { ...t, running: false, error: (e as Error).message, lastExecSql: sql } : t,
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
      persistCurrent()
      return
    }
    // 无激活 query tab：新建一个（序号递增），并带入上下文
    const tab = newQueryTab(db ?? "", maxQuerySeq(tabs) + 1)
    tab.sql = sql
    if (mode) tab.mode = mode
    const next = [...tabs, tab]
    set({ tabs: next, activeId: tab.id })
    persistCurrent()
  },

  // 按指定回填动作应用 SQL，与 AI 面板行为一致（全部替换/插入光标处/追加末尾/替换所选）。
  // 仅「全部替换」还原 db/mode 上下文；其余动作只插文本，不切换当前库/模式（避免误切库）。
  applySQLByAction: (sql, db, mode, action) => {
    if (action === "replace_all") {
      get().applySQL(sql, db, mode)
      return
    }
    const { tabs, activeId } = get()
    const active = tabs.find((t) => t.id === activeId && t.kind === "query") as
      | { id: string; sql: string }
      | undefined
    if (!active) {
      get().applySQL(sql, db, mode)
      return
    }
    const ed = getSqlEditor()
    let sel = { hasSelection: false, selectionOffset: -1, selectionLength: 0, cursorOffset: -1 }
    if (ed) {
      const m = ed.getModel()
      const s = ed.getSelection()
      if (m && s) {
        sel = {
          hasSelection: !s.isEmpty(),
          selectionOffset: s.isEmpty() ? -1 : m.getOffsetAt(s.getStartPosition()),
          selectionLength: s.isEmpty() ? 0 : m.getValueInRange(s).length,
          cursorOffset: m.getOffsetAt(s.getPosition()),
        }
      }
    }
    const base = active.sql
    let final = base
    let applied = false
    if (action === "append") {
      final = base.trim() ? `${base.trim()}\n\n${sql}` : sql
      applied = true
    } else if (action === "replace_selection" && sel.hasSelection && sel.selectionOffset >= 0) {
      final = base.slice(0, sel.selectionOffset) + sql + base.slice(sel.selectionOffset + sel.selectionLength)
      applied = true
    } else if (action === "insert_cursor" && sel.cursorOffset >= 0) {
      final = base.slice(0, sel.cursorOffset) + sql + base.slice(sel.cursorOffset)
      applied = true
    }
    if (!applied) {
      toast.info(i18n.t("workspace.noCursor"))
      final = base.trim() ? `${base.trim()}\n\n${sql}` : sql
    }
    get().updateTabSql(active.id, final)
  },
}))
