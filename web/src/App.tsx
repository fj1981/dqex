import { useEffect } from "react"
import { HashRouter, NavLink, Route, Routes, useLocation, useNavigate } from "react-router-dom"
import { toast } from "sonner"
import {
  ArrowLeftRight,
  ClipboardList,
  Database,
  FileDown,
  FileUp,
  FolderOpen,
  PanelRightClose,
  PanelRightOpen,
  Plus,
  Scale,
  Trash2,
} from "lucide-react"
import * as api from "@/api"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Separator } from "@/components/ui/separator"
import { Toaster } from "@/components/ui/sonner"
import ConnectionDrawer from "@/components/ConnectionDrawer"
import { useAppStore } from "@/stores/app"
import TaskView from "@/pages/TaskView"
import ExportView from "@/pages/ExportView"
import ImportView from "@/pages/ImportView"
import MigrateView from "@/pages/MigrateView"
import CompareView from "@/pages/CompareView"
import { TASK_TYPE_LABEL } from "@/types"
import { cn, formatTime, shortPaths } from "@/lib/utils"

const NAV = [
  { path: "/", label: "任务列表", desc: "已保存的配置", icon: ClipboardList },
  { path: "/export", label: "导出", desc: "数据库 → 文件", icon: FileDown },
  { path: "/import", label: "导入", desc: "文件 → 数据库", icon: FileUp },
  { path: "/migrate", label: "迁移", desc: "数据库 → 数据库", icon: ArrowLeftRight },
  { path: "/compare", label: "对比", desc: "数据库 ↔ 数据库", icon: Scale },
]

// 历史状态样式：底色胶囊 + 状态圆点，强化状态辨识度
const STATUS_META: Record<string, { label: string; cls: string; dot: string }> = {
  done: { label: "成功", cls: "bg-green-50 text-green-700", dot: "bg-green-600" },
  error: { label: "失败", cls: "bg-red-50 text-destructive", dot: "bg-destructive" },
  running: { label: "运行中", cls: "bg-blue-50 text-blue-600", dot: "animate-pulse bg-blue-600" },
  cancelled: { label: "已取消", cls: "bg-muted text-muted-foreground", dot: "bg-muted-foreground" },
}

function Sidebar() {
  return (
    // 窄屏折叠为纯图标栏，避免挤压主内容区
    <aside className="flex w-14 shrink-0 flex-col border-r bg-muted/30 py-3 md:w-52">
      <nav className="flex flex-col gap-1 px-2 md:px-3">
        {NAV.map(({ path, label, desc, icon: Icon }) => (
          <NavLink
            key={path}
            to={path}
            end={path === "/"}
            title={label}
            className={({ isActive }) =>
              cn(
                "group relative flex items-center justify-center gap-3 rounded-md px-2 py-2 transition-colors md:justify-start md:px-3",
                isActive
                  ? "bg-primary/10 text-primary"
                  : "text-muted-foreground hover:bg-accent hover:text-foreground",
              )
            }
          >
            {({ isActive }) => (
              <>
                {isActive && (
                  <span className="absolute -left-2 top-1/2 h-5 w-1 -translate-y-1/2 rounded-r bg-primary md:-left-3" />
                )}
                <Icon className="h-4 w-4 shrink-0" />
                <span className="hidden min-w-0 md:block">
                  <span className="block text-sm font-medium leading-tight">{label}</span>
                  <span className="block text-[11px] leading-tight opacity-80">{desc}</span>
                </span>
              </>
            )}
          </NavLink>
        ))}
      </nav>
    </aside>
  )
}

function RightPanel() {
  const { panelOpen, togglePanel, connections, history, openDrawer, loadConnections, loadHistory } = useAppStore()
  const location = useLocation()
  const navigate = useNavigate()

  useEffect(() => {
    loadConnections()
    loadHistory()
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  // 在文件管理器中定位导出产物
  const openDir = async (taskID: string) => {
    try {
      await api.openExportDir(taskID)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const delRecord = async (taskID: string) => {
    if (!window.confirm("确定删除这条执行记录吗？（不会删除已导出的文件）")) return
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
      <div className="px-3 py-2 pr-10">
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

      <div className="px-3 py-2 text-xs font-medium text-muted-foreground">
        操作历史{history.length > 0 && <span className="ml-1 tabular-nums">({history.length})</span>}
      </div>
      {/* 用普通滚动容器而非 ScrollArea：radix viewport 内部的 display:table 包裹层会被 nowrap 内容撑宽，
          导致长错误文本把卡片推出视口、悬停按钮不可见 */}
      <div className="scrollbar-thin flex-1 overflow-y-auto px-3 pb-3">
        {history.length === 0 && (
          <div className="py-4 text-center text-xs text-muted-foreground">暂无执行记录</div>
        )}
        {history.map((h) => {
          const s = STATUS_META[h.status] || STATUS_META.cancelled
          return (
            <div
              key={h.id}
              role="button"
              tabIndex={0}
              title="点击查看执行详情"
              className="group mb-1.5 min-w-0 cursor-pointer overflow-hidden rounded-md border bg-background px-2.5 py-1.5 text-xs transition-colors hover:border-primary/40 hover:shadow-sm"
              onClick={() => navigate(`/${h.taskType}?running=${h.id}`)}
              onKeyDown={(e) => e.key === "Enter" && navigate(`/${h.taskType}?running=${h.id}`)}
            >
              <div className="flex items-center justify-between">
                <span className="text-sm font-medium">{TASK_TYPE_LABEL[h.taskType] || h.taskType}</span>
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
        </aside>
      )}
      {/* 悬浮切换按钮：不占布局空间，展开/收起始终固定在右上角同一位置 */}
      <Button
        variant="outline"
        size="icon"
        className={cn(
          "absolute right-2 top-2 z-40 h-7 w-7 rounded-full bg-background shadow-sm",
          panelOpen && "text-muted-foreground",
        )}
        title={panelOpen ? "收起面板" : "展开面板"}
        onClick={togglePanel}
      >
        {panelOpen ? <PanelRightClose className="h-4 w-4" /> : <PanelRightOpen className="h-4 w-4" />}
      </Button>
    </>
  )
}

function Layout() {
  const setPanelOpen = useAppStore((s) => s.setPanelOpen)

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
      <header className="flex h-12 shrink-0 items-center justify-between border-b px-4">
        <div className="flex items-center gap-2">
          <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Database className="h-4 w-4" />
          </span>
          <span className="font-medium">数据库工作台</span>
          <Badge variant="secondary" className="ml-1 font-normal">导入 · 导出 · 迁移 · 对比</Badge>
        </div>
        <span className="text-xs text-muted-foreground">MySQL / PostgreSQL / Oracle</span>
      </header>
      <div className="relative flex flex-1 overflow-hidden">
        <Sidebar />
        <main className="scrollbar-thin min-w-0 flex-1 overflow-y-auto bg-muted/20 p-6">
          <Routes>
            <Route path="/" element={<TaskView />} />
            <Route path="/export" element={<ExportView />} />
            <Route path="/import" element={<ImportView />} />
            <Route path="/migrate" element={<MigrateView />} />
            <Route path="/compare" element={<CompareView />} />
          </Routes>
        </main>
        <RightPanel />
      </div>
      <ConnectionDrawer />
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
    <HashRouter>
      <Layout />
    </HashRouter>
  )
}
