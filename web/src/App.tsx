import { useEffect, useState } from "react"
import { HashRouter, Navigate, NavLink, Route, Routes, useLocation, useNavigate } from "react-router-dom"
import { toast } from "sonner"
import {
  ArrowLeftRight,
  BookOpenText,
  Camera,
  ClipboardList,
  Database,
  FileDown,
  FileUp,
  FolderOpen,
  HelpCircle,
  Info,
  PanelRightClose,
  PanelRightOpen,
  Plus,
  Scale,
  ScrollText,
  Settings,
  Terminal,
  Trash2,
} from "lucide-react"
import * as api from "@/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { confirm } from "@/components/ui/alert-dialog"
import { Separator } from "@/components/ui/separator"
import { Toaster } from "@/components/ui/sonner"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import ConnectionDrawer from "@/components/ConnectionDrawer"
import { useAppStore } from "@/stores/app"
import { useSqlHistoryStore } from "@/stores/sqlHistoryStore"
import { useQueryStore } from "@/stores/queryStore"
import TaskView from "@/pages/TaskView"
import ExportView from "@/pages/ExportView"
import ImportView from "@/pages/ImportView"
import MigrateView from "@/pages/MigrateView"
import CompareView from "@/pages/CompareView"
import DictionaryView from "@/pages/DictionaryView"
import SnapshotView from "@/pages/SnapshotView"
import QueryView from "@/pages/QueryView"
import SettingsView from "@/pages/SettingsView"
import AboutDialog from "@/components/AboutDialog"
import HelpDialog from "@/components/HelpDialog"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  SQL_EXEC_MODE_LABEL,
  TASK_TYPE_LABEL,
  type SQLAuditEntry,
  type SQLExecMode,
  type SQLHistoryItem,
} from "@/types"
import { cn, formatTime, shortPaths } from "@/lib/utils"

const NAV = [
  { path: "/query", label: "查询浏览", desc: "SQL · 对象 · 表数据", icon: Terminal },
  { path: "/export", label: "导出", desc: "数据库 → 文件", icon: FileDown },
  { path: "/import", label: "导入", desc: "文件 → 数据库", icon: FileUp },
  { path: "/migrate", label: "迁移", desc: "数据库 → 数据库", icon: ArrowLeftRight },
  { path: "/compare", label: "对比", desc: "数据库 ↔ 数据库", icon: Scale },
  { path: "/snapshots", label: "快照", desc: "数据库快照 ↔ 对比", icon: Camera },
  { path: "/dictionary", label: "数据字典", desc: "结构 → Excel", icon: BookOpenText },
]

// 历史状态样式：底色胶囊 + 状态圆点，强化状态辨识度
const STATUS_META: Record<string, { label: string; cls: string; dot: string }> = {
  done: { label: "成功", cls: "bg-green-50 text-green-700", dot: "bg-green-600" },
  error: { label: "失败", cls: "bg-red-50 text-destructive", dot: "bg-destructive" },
  running: { label: "运行中", cls: "bg-blue-50 text-blue-600", dot: "animate-pulse bg-blue-600" },
  cancelled: { label: "已取消", cls: "bg-muted text-muted-foreground", dot: "bg-muted-foreground" },
}

function TopNav() {
  return (
    // 顶部横向菜单：随容器水平滚动，窄屏不换行
    <nav className="scrollbar-thin flex min-w-0 flex-1 items-center gap-1 self-stretch overflow-x-auto">
      {NAV.map(({ path, label, desc, icon: Icon }) => (
        <NavLink
          key={path}
          to={path}
          end={path === "/"}
          title={desc}
          className={({ isActive }) =>
            cn(
              "group relative flex h-full shrink-0 items-center gap-2 px-3 transition-colors",
              isActive
                ? "text-primary"
                : "text-muted-foreground hover:bg-accent hover:text-foreground",
            )
          }
        >
          {({ isActive }) => (
            <>
              <Icon className="h-4 w-4 shrink-0" />
              <span className="text-sm font-medium">{label}</span>
              {isActive && (
                <span className="absolute inset-x-2 bottom-0 h-0.5 rounded-full bg-primary" />
              )}
            </>
          )}
        </NavLink>
      ))}
    </nav>
  )
}

// 左侧导航 ↔ 操作历史联动：功能页内只显示该类型的执行记录，任务列表等页面显示全部
const TYPE_BY_PATH: Record<string, string> = {
  "/export": "export",
  "/import": "import",
  "/migrate": "migrate",
  "/compare": "compare",
  "/snapshots": "snapshot_compare",
  "/dictionary": "dictionary",
}

// 历史记录 taskType → 功能页路径（snapshot_compare 共享 /snapshots 页面）
const PATH_BY_TYPE: Record<string, string> = {
  export: "/export",
  import: "/import",
  migrate: "/migrate",
  compare: "/compare",
  snapshot_compare: "/snapshots",
  dictionary: "/dictionary",
}

function RightPanel() {
  const { panelOpen, togglePanel, connections, history, openDrawer, loadConnections, loadHistory } = useAppStore()
  const location = useLocation()
  const navigate = useNavigate()

  // 查询页语义切换：/query 下右侧面板展示「SQL 执行历史 + 审计」，其余页面展示任务级「操作历史」
  const isQuery = location.pathname === "/query"
  const queryConnId = useQueryStore((s) => s.connId)
  const applySQL = useQueryStore((s) => s.applySQL)
  const {
    items: sqlItems,
    load: loadSQLHistory,
    clear: clearSQLHistory,
    auditItems,
    loadAudit,
  } = useSqlHistoryStore()

  const filterType = TYPE_BY_PATH[location.pathname]
  const records = filterType ? history.filter((h) => h.taskType === filterType) : history

  useEffect(() => {
    loadConnections()
    loadHistory()
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  // 查询页：按当前查询连接加载 SQL 执行历史 + 审计（切换连接或进入查询页时刷新）
  useEffect(() => {
    if (isQuery && queryConnId) {
      loadSQLHistory(queryConnId)
      loadAudit(queryConnId)
    }
  }, [isQuery, queryConnId]) // eslint-disable-line react-hooks/exhaustive-deps

  // 在文件管理器中定位导出产物
  const openDir = async (taskID: string) => {
    try {
      await api.openExportDir(taskID)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const delRecord = async (taskID: string) => {
    if (!(await confirm({ title: "删除执行记录", description: "确定删除这条执行记录吗？（会同步删除其导出/对比产物文件）", confirmText: "删除", danger: true }))) return
    try {
      await api.deleteHistory(taskID)
      loadHistory()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  return (
    <>
      {/* 窄屏浮层模式：半透明遮罩 + 点击面板外部收起；宽屏下不渲染 */}
      {panelOpen && (
        <div className="absolute inset-0 z-20 bg-black/25 lg:hidden" onClick={togglePanel} />
      )}
      {/* 面板本体：窄屏为浮层，宽屏为常规侧栏；收起时不渲染 */}
      {panelOpen && (
        <aside className="absolute inset-y-0 right-0 z-30 flex w-72 max-w-[calc(100%-1rem)] shrink-0 flex-col border-l bg-background shadow-xl lg:static lg:z-auto lg:max-w-none lg:bg-muted/20 lg:shadow-none">
      <div className="flex items-center justify-between px-3 py-2 pr-3">
        <span className="text-xs font-medium text-muted-foreground">
          数据库连接{connections.length > 0 && <span className="ml-1 tabular-nums">({connections.length})</span>}
        </span>
      </div>
      {/* 连接列表限高 40% 独立滚动：连接再多也不会挤压下方操作历史；
          同上用普通滚动容器，避免 radix table 包裹层被长文本撑宽 */}
      <div className="scrollbar-thin max-h-[40%] overflow-y-auto">
        <div className="px-3">
          {connections.length === 0 && (
            <div className="py-4 text-center text-xs text-muted-foreground">暂无连接，点击下方按钮新建</div>
          )}
          {connections.map((c) => (
            <button
              key={c.id}
              type="button"
              title={`${c.conn.Host}:${c.conn.Port} · 点击编辑连接`}
              className="mb-1 w-full rounded-md border bg-background px-2.5 py-1.5 text-left text-xs transition-colors hover:border-primary/40 hover:shadow-sm"
              onClick={() => openDrawer(c)}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="truncate text-sm font-medium">{c.name}</span>
                <Badge variant="secondary" className="shrink-0 px-1.5 py-0 text-[10px] font-normal uppercase">
                  {c.conn.Type}
                </Badge>
              </div>
              <div className="truncate text-[11px] leading-snug text-muted-foreground">
                {c.conn.Host}:{c.conn.Port}
                {c.conn.DBName ? ` / ${c.conn.DBName}` : c.conn.Service ? ` / ${c.conn.Service}` : ""}
              </div>
            </button>
          ))}
        </div>
      </div>
      <div className="px-3 py-2">
        <Button variant="outline" size="sm" className="w-full" onClick={() => openDrawer()}>
          <Plus className="mr-1 h-4 w-4" /> 新建连接
        </Button>
      </div>

      <Separator />

      {isQuery ? (
        <SQLHistoryPanel
          connId={queryConnId}
          items={sqlItems}
          auditItems={auditItems}
          onClear={clearSQLHistory}
          onApply={applySQL}
        />
      ) : (
        <>
          <div className="px-3 py-2 text-xs font-medium text-muted-foreground">
            {filterType ? `${TASK_TYPE_LABEL[filterType] || filterType}历史` : "操作历史"}
            {records.length > 0 && <span className="ml-1 tabular-nums">({records.length})</span>}
          </div>
          {/* 用普通滚动容器而非 ScrollArea：radix viewport 内部的 display:table 包裹层会被 nowrap 内容撑宽，
              导致长错误文本把卡片推出视口、悬停按钮不可见 */}
          <div className="scrollbar-thin flex-1 overflow-y-auto px-3 pb-3">
            {records.length === 0 && (
              <div className="py-4 text-center text-xs text-muted-foreground">
                {filterType ? "该类型暂无执行记录" : "暂无执行记录"}
              </div>
            )}
            {records.map((h) => {
              const s = STATUS_META[h.status] || STATUS_META.cancelled
              return (
                <div
                  key={h.id}
                  role="button"
                  tabIndex={0}
                  title="点击查看执行详情"
                  className="group mb-1.5 min-w-0 cursor-pointer overflow-hidden rounded-md border bg-background px-2.5 py-1.5 text-xs transition-colors hover:border-primary/40 hover:shadow-sm"
                  onClick={() => navigate(`${PATH_BY_TYPE[h.taskType] || "/"}?running=${h.id}`)}
                  onKeyDown={(e) => e.key === "Enter" && navigate(`${PATH_BY_TYPE[h.taskType] || "/"}?running=${h.id}`)}
                >
                  <div className="flex items-center justify-between">
                    {/* 类型过滤下全部同类，省略类型名；全部视图保留以区分 */}
                    {!filterType && (
                      <span className="text-sm font-medium">{TASK_TYPE_LABEL[h.taskType] || h.taskType}</span>
                    )}
                    <span className={cn("flex items-center gap-1 rounded-full px-1.5 py-0.5 font-medium", s.cls)}>
                      <span className={cn("h-1.5 w-1.5 rounded-full", s.dot)} />
                      {s.label}
                    </span>
                  </div>
                  {/* 操作目标（环境 + 对象）：固定单行截断，悬停 title 查看完整内容 */}
                  {h.target && (
                    <div className="mt-0.5 min-w-0 truncate text-foreground/75" title={h.target}>
                      {h.target}
                    </div>
                  )}
                  <div className="mt-0.5 flex items-center justify-between text-muted-foreground">
                    <span className="shrink-0 tabular-nums">{formatTime(new Date(h.startedAt).toISOString()).slice(5, 16)}</span>
                    {/* 默认展示摘要/错误（弹性占满剩余宽度并截断）；悬停时隐藏，为操作按钮让位 */}
                    {h.errorMsg ? (
                      <span className="ml-2 min-w-0 flex-1 truncate text-destructive group-hover:hidden" title={shortPaths(h.errorMsg)}>
                        {shortPaths(h.errorMsg)}
                      </span>
                    ) : (
                      h.summary && <span className="ml-2 min-w-0 flex-1 truncate group-hover:hidden">{h.summary}</span>
                    )}
                    <span className="hidden shrink-0 items-center gap-0.5 group-hover:flex">
                      {h.taskType === "export" && h.status === "done" && h.outputPath && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-5 w-5 text-muted-foreground hover:text-foreground"
                          title="在文件管理器中定位导出文件"
                          onClick={(e) => {
                            e.stopPropagation()
                            openDir(h.id)
                          }}
                        >
                          <FolderOpen className="h-3 w-3" />
                        </Button>
                      )}
                      {h.taskType === "dictionary" && h.status === "done" && h.outputPath && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-5 w-5 text-muted-foreground hover:text-foreground"
                          title="在文件管理器中定位数据字典文件"
                          onClick={(e) => {
                            e.stopPropagation()
                            openDir(h.id)
                          }}
                        >
                          <FolderOpen className="h-3 w-3" />
                        </Button>
                      )}
                      {h.status !== "running" && (
                        <Button
                          variant="ghost"
                          size="icon"
                          className="h-5 w-5 text-muted-foreground hover:text-destructive"
                          title="删除记录"
                          onClick={(e) => {
                            e.stopPropagation()
                            delRecord(h.id)
                          }}
                        >
                          <Trash2 className="h-3 w-3" />
                        </Button>
                      )}
                    </span>
                  </div>
                </div>
              )
            })}
          </div>
        </>
      )}
        </aside>
      )}
    </>
  )
}

// SQL 记录面板：查询页右侧，含「执行历史 / 审计」两个 Tab。
// 执行历史：用户手写 SQL，可回填重跑；审计：全量只读，含对象树自动查询与单元格编辑。
function SQLHistoryPanel({
  connId,
  items,
  auditItems,
  onClear,
  onApply,
}: {
  connId: string
  items: SQLHistoryItem[]
  auditItems: SQLAuditEntry[]
  onClear: () => Promise<void>
  onApply: (sql: string, db?: string, mode?: SQLExecMode) => void
}) {
  const [tab, setTab] = useState<"history" | "audit">("history")

  const clear = async () => {
    if (!(await confirm({ title: "清空 SQL 历史", description: "确认清空当前连接的全部 SQL 执行历史？", confirmText: "清空", danger: true }))) return
    try {
      await onClear()
      toast.success("SQL 历史已清空")
    } catch (e) {
      toast.error(`清空失败: ${(e as Error).message}`)
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-3 pb-1">
        <Tabs value={tab} onValueChange={(v) => setTab(v as "history" | "audit")}>
          <TabsList className="w-full">
            <TabsTrigger value="history" className="flex-1 text-xs">
              执行历史{items.length > 0 && <span className="ml-1 tabular-nums">({items.length})</span>}
            </TabsTrigger>
            <TabsTrigger value="audit" className="flex-1 text-xs">
              审计{auditItems.length > 0 && <span className="ml-1 tabular-nums">({auditItems.length})</span>}
            </TabsTrigger>
          </TabsList>
        </Tabs>
      </div>

      {tab === "history" ? (
        <>
          <div className="flex items-center justify-end px-3 py-1">
            {items.length > 0 && (
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 text-muted-foreground hover:text-destructive"
                title="清空 SQL 历史"
                onClick={clear}
              >
                <Trash2 className="h-3 w-3" />
              </Button>
            )}
          </div>
          <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-3 pb-3">
            {!connId ? (
              <div className="py-4 text-center text-xs text-muted-foreground">请先在查询页选择数据库连接</div>
            ) : items.length === 0 ? (
              <div className="py-4 text-center text-xs text-muted-foreground">暂无 SQL 执行记录</div>
            ) : (
              items.map((h) => (
                <div
                  key={h.id}
                  role="button"
                  tabIndex={0}
                  title="点击回填到当前查询编辑器"
                  className="group mb-1.5 min-w-0 cursor-pointer overflow-hidden rounded-md border bg-background px-2.5 py-1.5 text-xs transition-colors hover:border-primary/40 hover:shadow-sm"
                  onClick={() => onApply(h.sql, h.db, h.mode)}
                  onKeyDown={(e) => e.key === "Enter" && onApply(h.sql, h.db, h.mode)}
                >
                  <div className="flex items-center justify-between">
                    <span className="flex items-center gap-1.5">
                      <span
                        className={cn(
                          "h-1.5 w-1.5 rounded-full",
                          h.status === "error" ? "bg-destructive" : "bg-green-600",
                        )}
                      />
                      {h.isWrite && (
                        <span className="rounded bg-destructive/10 px-1 py-px text-[10px] font-medium text-destructive">写</span>
                      )}
                      {h.db && <span className="rounded bg-muted px-1 py-px text-[10px] text-muted-foreground">{h.db}</span>}
                    </span>
                    <span className="shrink-0 tabular-nums text-muted-foreground">
                      {formatTime(new Date(h.createdAt).toISOString()).slice(5, 16)}
                    </span>
                  </div>
                  <div className="mt-0.5 line-clamp-2 break-all font-mono text-[11px] leading-4 text-foreground/80">
                    {h.sql}
                  </div>
                  <div className="mt-0.5 flex items-center justify-between text-[11px] text-muted-foreground">
                    <span className="tabular-nums">
                      {h.status === "ok"
                        ? h.isWrite
                          ? `影响 ${h.rowCount} 行`
                          : `${h.rowCount} 行 · ${h.elapsedMs}ms`
                        : h.error || "执行失败"}
                    </span>
                    {h.mode && <span className="text-[10px]">{SQL_EXEC_MODE_LABEL[h.mode]}</span>}
                  </div>
                </div>
              ))
            )}
          </div>
        </>
      ) : (
        <AuditList connId={connId} items={auditItems} />
      )}
    </div>
  )
}

// 审计面板：只读展示，含来源标记（手写/对象树/单元格编辑）
function AuditList({ connId, items }: { connId: string; items: SQLAuditEntry[] }) {
  const SOURCE_LABEL: Record<string, { text: string; cls: string }> = {
    manual: { text: "手写", cls: "bg-primary/10 text-primary" },
    tree: { text: "浏览", cls: "bg-muted text-muted-foreground" },
    cell: { text: "编辑", cls: "bg-amber-500/10 text-amber-600" },
  }

  const renderValue = (v: unknown): string => {
    if (v === null || v === undefined) return "NULL"
    if (typeof v === "object") return JSON.stringify(v)
    return String(v)
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="flex items-center gap-1 px-3 py-1 text-[10px] text-muted-foreground">
        <ScrollText className="h-3 w-3" />
        审计仅记录、不可删除
      </div>
      <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-3 pb-3">
        {!connId ? (
          <div className="py-4 text-center text-xs text-muted-foreground">请先在查询页选择数据库连接</div>
        ) : items.length === 0 ? (
          <div className="py-4 text-center text-xs text-muted-foreground">暂无审计记录</div>
        ) : (
          items.map((a) => {
            const src = SOURCE_LABEL[a.source] || SOURCE_LABEL.manual
            return (
              <div
                key={a.id}
                className="mb-1.5 min-w-0 overflow-hidden rounded-md border bg-background px-2.5 py-1.5 text-xs"
              >
                <div className="flex items-center justify-between">
                  <span className="flex items-center gap-1.5">
                    <span
                      className={cn(
                        "h-1.5 w-1.5 rounded-full",
                        a.status === "error" ? "bg-destructive" : "bg-green-600",
                      )}
                    />
                    <span className={cn("rounded px-1 py-px text-[10px] font-medium", src.cls)}>{src.text}</span>
                    {a.isWrite && (
                      <span className="rounded bg-destructive/10 px-1 py-px text-[10px] font-medium text-destructive">写</span>
                    )}
                    {a.db && <span className="rounded bg-muted px-1 py-px text-[10px] text-muted-foreground">{a.db}</span>}
                  </span>
                  <span className="shrink-0 tabular-nums text-muted-foreground">
                    {formatTime(new Date(a.createdAt).toISOString()).slice(5, 16)}
                  </span>
                </div>

                {/* 单元格编辑：结构化展示真实参数 */}
                {a.source === "cell" && a.table ? (
                  <div className="mt-1 space-y-0.5 font-mono text-[11px] leading-4 text-foreground/80">
                    <div>
                      <span className="text-muted-foreground">表 </span>
                      {a.table}
                      <span className="text-muted-foreground"> . 列 </span>
                      {a.column}
                    </div>
                    <div className="break-all">
                      <span className="text-muted-foreground">值 </span>
                      <span className="text-foreground">{renderValue(a.newValue)}</span>
                    </div>
                    {a.pkColumns && a.pkColumns.length > 0 && (
                      <div className="break-all">
                        <span className="text-muted-foreground">条件 </span>
                        {a.pkColumns.map((pk, i) => (
                          <span key={i}>
                            {i > 0 && " AND "}
                            {pk} = {renderValue(a.pkValues?.[i])}
                          </span>
                        ))}
                      </div>
                    )}
                  </div>
                ) : (
                  <div className="mt-0.5 line-clamp-2 break-all font-mono text-[11px] leading-4 text-foreground/80">
                    {a.sql}
                  </div>
                )}

                <div className="mt-0.5 flex items-center justify-between text-[11px] text-muted-foreground">
                  <span className="tabular-nums">
                    {a.status === "ok"
                      ? a.isWrite
                        ? `影响 ${a.rowCount} 行`
                        : `${a.rowCount} 行 · ${a.elapsedMs}ms`
                      : a.error || "执行失败"}
                  </span>
                  {a.mode && <span className="text-[10px]">{SQL_EXEC_MODE_LABEL[a.mode]}</span>}
                </div>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

function Layout() {
  const navigate = useNavigate()
  const location = useLocation()
  const setPanelOpen = useAppStore((s) => s.setPanelOpen)
  const panelOpen = useAppStore((s) => s.panelOpen)
  const togglePanel = useAppStore((s) => s.togglePanel)
  const [aboutOpen, setAboutOpen] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)

  // 设置 / 任务列表页：右侧「连接 + 历史」面板与展开按钮均不展示，让出完整空间
  const hidePanel = location.pathname === "/settings" || location.pathname === "/tasks"

  // 窗口从宽变窄（跌破 lg 断点）时自动收起右侧面板，避免浮层遮挡内容
  useEffect(() => {
    const mq = window.matchMedia("(min-width: 1024px)")
    const onChange = (e: MediaQueryListEvent) => {
      if (!e.matches) setPanelOpen(false)
    }
    mq.addEventListener("change", onChange)
    return () => mq.removeEventListener("change", onChange)
  }, [setPanelOpen])

  return (
    <div className="flex h-screen flex-col">
      <header className="flex h-12 shrink-0 items-center gap-4 border-b px-4">
        <div className="flex shrink-0 items-center gap-2">
          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Database className="h-4 w-4" />
          </span>
          <span className="font-medium">数据库工作台</span>
        </div>
        <TopNav />
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 shrink-0"
          title="任务列表"
          onClick={() => navigate("/tasks")}
        >
          <ClipboardList className={cn("h-4 w-4", location.pathname === "/tasks" ? "text-primary" : "text-muted-foreground")} />
        </Button>
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 shrink-0"
          title="设置"
          onClick={() => navigate("/settings")}
        >
          <Settings className={cn("h-4 w-4", location.pathname === "/settings" ? "text-primary" : "text-muted-foreground")} />
        </Button>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8 shrink-0"
              title="帮助与关于"
            >
              <HelpCircle className="h-4 w-4 text-muted-foreground" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-40">
            <DropdownMenuItem onSelect={() => setHelpOpen(true)}>
              <BookOpenText className="h-4 w-4 text-muted-foreground" />
              使用说明
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => setAboutOpen(true)}>
              <Info className="h-4 w-4 text-muted-foreground" />
              关于
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </header>
      <div className="relative flex flex-1 overflow-hidden">
        <main className="scrollbar-thin min-w-0 flex-1 overflow-y-auto bg-muted/20 p-6">
          <Routes>
            <Route path="/" element={<Navigate to="/query" replace />} />
            <Route path="/query" element={<QueryView />} />
            <Route path="/export" element={<ExportView />} />
            <Route path="/import" element={<ImportView />} />
            <Route path="/migrate" element={<MigrateView />} />
            <Route path="/compare" element={<CompareView />} />
            <Route path="/snapshots" element={<SnapshotView />} />
            <Route path="/dictionary" element={<DictionaryView />} />
            <Route path="/settings" element={<SettingsView />} />
            <Route path="/tasks" element={<TaskView />} />
            {/* 旧数据浏览入口重定向到合并后的查询浏览页 */}
            <Route path="/browser" element={<Navigate to="/query" replace />} />
          </Routes>
        </main>
        {!hidePanel && <RightPanel />}
        {/* 展开/收起按钮：固定右上角同一位置，避免跳动；新建查询容器已预留右侧 padding 让位 */}
        {!hidePanel && (
          <Button
            variant="outline"
            size="icon"
            className="absolute right-2 top-1 z-40 h-7 w-7 rounded-full bg-background shadow-sm"
            title={panelOpen ? "收起面板" : "展开面板"}
            onClick={togglePanel}
          >
            {panelOpen ? <PanelRightClose className="h-4 w-4" /> : <PanelRightOpen className="h-4 w-4" />}
          </Button>
        )}
      </div>
      <ConnectionDrawer />
      <AboutDialog open={aboutOpen} onOpenChange={setAboutOpen} />
      <HelpDialog open={helpOpen} onOpenChange={setHelpOpen} />
      <Toaster position="top-center" richColors />
    </div>
  )
}

export default function App() {
  const loadDBTypes = useAppStore((s) => s.loadDBTypes)
  const loadConnections = useAppStore((s) => s.loadConnections)

  useEffect(() => {
    loadDBTypes()
    loadConnections()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <HashRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <Layout />
    </HashRouter>
  )
}
