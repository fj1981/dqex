import { useEffect, useMemo, useState } from "react"
import { HashRouter, Navigate, NavLink, Route, Routes, useLocation, useNavigate } from "react-router-dom"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { useTheme } from "next-themes"
import {
  ArrowLeftRight,
  BookOpenText,
  Camera,
  ClipboardList,
  Database,
  FileDown,
  FileUp,
  FolderOpen,
  Globe,
  HelpCircle,
  Info,
  Monitor,
  Moon,
  PanelRightClose,
  PanelRightOpen,
  Plus,
  Scale,
  ScrollText,
  Settings,
  ShieldCheck,
  Star,
  Sun,
  Terminal,
  Trash2,
  Zap,
} from "lucide-react"

import * as api from "@/api"
import DbTypeIcon from "@/components/DbTypeIcon"
import { SQLHistoryPanel } from "@/components/SQLPanel"
import EmbedShell from "@/components/EmbedShell"
import { isEmbedMode } from "@/lib/embedBus"
import { Button } from "@/components/ui/button"
import { confirm, prompt } from "@/components/ui/alert-dialog"
import { Separator } from "@/components/ui/separator"
import { Toaster } from "@/components/ui/sonner"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import ConnectionDrawer from "@/components/ConnectionDrawer"
import { useAppStore } from "@/stores/app"
import { useSqlHistoryStore } from "@/stores/sqlHistoryStore"
import { useFavoriteStore } from "@/stores/favoriteStore"
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
  TASK_TYPE_LABEL,
  type SQLAuditEntry,
  type SQLExecMode,
  type SQLFavorite,
  type SQLHistoryItem,
} from "@/types"
import { cn, formatTime, shortPaths } from "@/lib/utils"
import { defaultFavoriteTitle } from "@/lib/sql"
import { changeUILang, tKey } from "@/lib/i18n"
import { SUPPORTED_LANGS } from "@/locales"

const NAV = [
  { path: "/query", labelKey: "nav.query", descKey: "nav.queryDesc", icon: Terminal },
  { path: "/export", labelKey: "nav.export", descKey: "nav.exportDesc", icon: FileDown },
  { path: "/import", labelKey: "nav.import", descKey: "nav.importDesc", icon: FileUp },
  { path: "/migrate", labelKey: "nav.migrate", descKey: "nav.migrateDesc", icon: ArrowLeftRight },
  { path: "/compare", labelKey: "nav.compare", descKey: "nav.compareDesc", icon: Scale },
  { path: "/snapshots", labelKey: "nav.snapshots", descKey: "nav.snapshotsDesc", icon: Camera },
  { path: "/dictionary", labelKey: "nav.dictionary", descKey: "nav.dictionaryDesc", icon: BookOpenText },
]

// 历史状态样式：底色胶囊 + 状态圆点，强化状态辨识度（labelKey 见 locales: app.status.*）
const STATUS_META: Record<string, { labelKey: string; cls: string; dot: string }> = {
  done: { labelKey: "app.status.done", cls: "bg-green-500/10 text-green-700 dark:text-green-400", dot: "bg-green-600" },
  error: { labelKey: "app.status.error", cls: "bg-red-500/10 text-destructive", dot: "bg-destructive" },
  running: { labelKey: "app.status.running", cls: "bg-blue-500/10 text-blue-600 dark:text-blue-400", dot: "animate-pulse bg-blue-600" },
  cancelled: { labelKey: "app.status.cancelled", cls: "bg-muted text-muted-foreground", dot: "bg-muted-foreground" },
}


function TopNav() {
  const { t } = useTranslation()
  return (
    // 顶部横向菜单：随容器水平滚动，窄屏不换行
    <nav className="scrollbar-thin flex min-w-0 flex-1 items-center gap-1 self-stretch overflow-x-auto">
      {NAV.map(({ path, labelKey, descKey, icon: Icon }) => (
        <NavLink
          key={path}
          to={path}
          end={path === "/"}
          title={tKey(descKey)}
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
              <span className="text-sm font-medium">{tKey(labelKey)}</span>
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
  const { t } = useTranslation()
  const { panelOpen, togglePanel, connections, history, openDrawer, loadConnections, loadHistory } = useAppStore()
  const location = useLocation()
  const navigate = useNavigate()

  // 查询页语义切换：/query 下右侧面板展示「SQL 执行历史 + 审计」，其余页面展示任务级「操作历史」
  const isQuery = location.pathname === "/query"
  const queryConnId = useQueryStore((s) => s.connId)
  const queryActiveDb = useQueryStore((s) => {
    const t = s.tabs.find((x) => x.id === s.activeId && x.kind === "query")
    return t && t.kind === "query" ? t.db : undefined
  })
  const applySQL = useQueryStore((s) => s.applySQL)
  const applySQLByAction = useQueryStore((s) => s.applySQLByAction)
  const {
    items: sqlItems,
    load: loadSQLHistory,
    clear: clearSQLHistory,
    auditItems,
    loadAudit,
  } = useSqlHistoryStore()
  const { favorites, load: loadFavorites, add: addFavorite, remove: removeFavorite, rename: renameFavorite } =
    useFavoriteStore()

  const filterType = TYPE_BY_PATH[location.pathname]
  const records = filterType ? history.filter((h) => h.taskType === filterType) : history

  useEffect(() => {
    loadConnections()
    loadHistory()
  }, [location.pathname]) // eslint-disable-line react-hooks/exhaustive-deps

  // 查询页：加载 SQL 执行历史 + 审计；收藏为全局共享，进入查询页即加载（不随连接变化重拉）
  useEffect(() => {
    if (isQuery && queryConnId) {
      loadSQLHistory(queryConnId)
      loadAudit(queryConnId)
      loadFavorites()
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
    if (!(await confirm({ title: t("app.deleteRecordTitle"), description: t("app.deleteRecordDesc"), confirmText: t("common.delete"), danger: true }))) return
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
        <aside className="absolute inset-y-0 right-0 z-30 flex w-72 max-w-[calc(100%-1rem)] shrink-0 flex-col border-l bg-background shadow-xl lg:static lg:z-auto lg:max-w-none lg:bg-background lg:shadow-none">
      <div className="flex items-center justify-between px-3 py-2 pr-3">
        <span className="text-xs font-medium text-muted-foreground">
          {t("app.connections")}{connections.length > 0 && <span className="ml-1 tabular-nums">({connections.length})</span>}
        </span>
      </div>
      {/* 连接列表限高 40% 独立滚动：连接再多也不会挤压下方操作历史；
          同上用普通滚动容器，避免 radix table 包裹层被长文本撑宽 */}
      <div className="scrollbar-thin max-h-[40%] overflow-y-auto">
        <div className="px-3">
          {connections.length === 0 && (
            <div className="py-4 text-center text-xs text-muted-foreground">{t("app.noConnections")}</div>
          )}
          {connections.map((c) => (
            <button
              key={c.id}
              type="button"
              title={`${c.conn.Host}:${c.conn.Port} · ${t("app.editConnection")}`}
              className="mb-1 w-full rounded-md border bg-muted/20 px-2.5 py-1.5 text-left text-xs transition-colors hover:border-primary/40 hover:shadow-sm"
              onClick={() => openDrawer(c)}
            >
              <div className="flex items-center justify-between gap-2">
                <span className="truncate text-sm font-medium">{c.name}</span>
                <DbTypeIcon type={c.conn.Type} />
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
          <Plus className="mr-1 h-4 w-4" /> {t("app.newConnection")}
        </Button>
      </div>

      <Separator />

      {isQuery ? (
        <SQLHistoryPanel
          connId={queryConnId}
          currentDb={queryActiveDb}
          items={sqlItems}
          auditItems={auditItems}
          favorites={favorites}
          onClear={clearSQLHistory}
          onRefill={(sql, db, mode, action) => applySQLByAction(sql, db, mode, action)}
          onAddFavorite={async (sql, db, mode) => {
            // 弹窗预填默认标题，用户可修改后回车快速保存
            const title = await prompt({
              title: t("app.favoriteSQL"),
              description: t("app.favoriteSQLDesc"),
              defaultValue: defaultFavoriteTitle(sql),
              placeholder: t("app.favoritePlaceholder"),
              confirmText: t("app.favorite"),
              required: t("common.titleCannotBeEmpty"),
            })
            if (title == null) return
            try {
              await addFavorite(queryConnId, sql, db, mode, title)
              toast.success(t("app.favorited"))
            } catch (e) {
              toast.error(t("app.favoriteFailed", { msg: (e as Error).message }))
            }
          }}
          onDeleteFavorite={(id) => removeFavorite(id)}
          onRenameFavorite={(id, title) => renameFavorite(id, title)}
        />
      ) : (
        <>
          <div className="px-3 py-2 text-xs font-medium text-muted-foreground">
            {filterType ? t("app.historyOfType", { type: tKey(TASK_TYPE_LABEL[filterType] || filterType) }) : t("app.history")}
            {records.length > 0 && <span className="ml-1 tabular-nums">({records.length})</span>}
          </div>
          {/* 用普通滚动容器而非 ScrollArea：radix viewport 内部的 display:table 包裹层会被 nowrap 内容撑宽，
              导致长错误文本把卡片推出视口、悬停按钮不可见 */}
          <div className="scrollbar-thin flex-1 overflow-y-auto px-3 pb-3">
            {records.length === 0 && (
              <div className="py-4 text-center text-xs text-muted-foreground">
                {filterType ? t("app.noHistoryOfType") : t("app.noHistory")}
              </div>
            )}
            {records.map((h) => {
              const s = STATUS_META[h.status] || STATUS_META.cancelled
              return (
                <div
                  key={h.id}
                  role="button"
                  tabIndex={0}
                  title={t("app.viewDetail")}
                  className="group mb-1.5 min-w-0 cursor-pointer overflow-hidden rounded-md border bg-muted/20 px-2.5 py-1.5 text-xs transition-colors hover:border-primary/40 hover:shadow-sm"
                  onClick={() => navigate(`${PATH_BY_TYPE[h.taskType] || "/"}?running=${h.id}`)}
                  onKeyDown={(e) => e.key === "Enter" && navigate(`${PATH_BY_TYPE[h.taskType] || "/"}?running=${h.id}`)}
                >
                  <div className="flex items-center justify-between">
                    {/* 类型过滤下全部同类，省略类型名；全部视图保留以区分 */}
                    {!filterType && (
                      <span className="text-sm font-medium">{tKey(TASK_TYPE_LABEL[h.taskType] || h.taskType)}</span>
                    )}
                    <span className={cn("flex items-center gap-1 rounded-full px-1.5 py-0.5 font-medium", s.cls)}>
                      <span className={cn("h-1.5 w-1.5 rounded-full", s.dot)} />
                      {tKey(s.labelKey)}
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
                          title={t("app.locateExport")}
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
                          title={t("app.locateDictionary")}
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
                          title={t("app.deleteRecord")}
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


function Layout() {
  const navigate = useNavigate()
  const location = useLocation()
  const { t, i18n } = useTranslation()
  const setPanelOpen = useAppStore((s) => s.setPanelOpen)
  const panelOpen = useAppStore((s) => s.panelOpen)
  const togglePanel = useAppStore((s) => s.togglePanel)
  // 主题切换：浅色 / 深色 / 跟随系统（next-themes 持久化到 localStorage）
  const { theme, setTheme } = useTheme()
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
          <span className="font-medium">{t("app.title")}</span>
        </div>
        <TopNav />
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 shrink-0"
          title={t("app.tasks")}
          onClick={() => navigate("/tasks")}
        >
          <ClipboardList className={cn("h-4 w-4", location.pathname === "/tasks" ? "text-primary" : "text-muted-foreground")} />
        </Button>
        {/* 主题切换：浅色 / 深色 / 跟随系统（全局入口） */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" title={t("app.theme")}>
              {theme === "dark" ? <Moon className="h-4 w-4 text-muted-foreground" /> : theme === "light" ? <Sun className="h-4 w-4 text-muted-foreground" /> : <Monitor className="h-4 w-4 text-muted-foreground" />}
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => setTheme("light")}>
              <Sun className="mr-2 h-3.5 w-3.5" /> {t("app.light")}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setTheme("dark")}>
              <Moon className="mr-2 h-3.5 w-3.5" /> {t("app.dark")}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setTheme("system")}>
              <Monitor className="mr-2 h-3.5 w-3.5" /> {t("app.system")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
        {/* 语言切换：中文 / English（与设置页同源，localStorage 持久化） */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button variant="ghost" size="icon" className="h-8 w-8 shrink-0" title={t("app.language")}>
              <Globe className="h-4 w-4 text-muted-foreground" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            {SUPPORTED_LANGS.map((l) => (
              <DropdownMenuItem key={l.code} onClick={() => changeUILang(l.code)}>
                <span className={cn("mr-2 inline-block h-3.5 w-3.5", i18n.language === l.code ? "text-primary" : "text-muted-foreground/40")}>●</span>
                {l.label}
              </DropdownMenuItem>
            ))}
          </DropdownMenuContent>
        </DropdownMenu>
        <Button
          variant="ghost"
          size="icon"
          className="h-8 w-8 shrink-0"
          title={t("app.settings")}
          onClick={() => navigate("/settings")}
        >
          <Settings className={cn("h-4 w-4", location.pathname === "/settings" ? "text-primary" : "text-muted-foreground")} />
        </Button>
        {/* 帮助与关于：统一入口 */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="h-8 w-8 shrink-0"
              title={t("app.helpAbout")}
            >
              <HelpCircle className="h-4 w-4 text-muted-foreground" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end" className="w-40">
            <DropdownMenuItem onSelect={() => setHelpOpen(true)}>
              <BookOpenText className="h-4 w-4 text-muted-foreground" />
              {t("app.help")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem onSelect={() => setAboutOpen(true)}>
              <Info className="h-4 w-4 text-muted-foreground" />
              {t("app.about")}
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
            {/* 旧数据浏览入口重定向到合并后的工作台页 */}
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
            title={panelOpen ? t("app.panelOpen") : t("app.panelClose")}
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
  // 嵌入模式（#/embed/<view> 整页 iframe 嵌入）：渲染精简壳，不加载页头/右侧面板/连接抽屉
  // （docs/library-api-design.md 6.5.2；isEmbedMode 为模块级纯判定，同一页面生命周期内不变）
  const embedMode = isEmbedMode()
  const loadDBTypes = useAppStore((s) => s.loadDBTypes)
  const loadConnections = useAppStore((s) => s.loadConnections)

  useEffect(() => {
    if (embedMode) return // EmbedShell 内自行初始化
    loadDBTypes()
    loadConnections()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  if (embedMode) {
    return (
      <HashRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
        <Routes>
          <Route path="/embed/:view" element={<EmbedShell />} />
          {/* 无视图名 / 未知路径：同样交给 EmbedShell 渲染提示文案 */}
          <Route path="/embed" element={<EmbedShell />} />
          <Route path="*" element={<EmbedShell />} />
        </Routes>
      </HashRouter>
    )
  }

  return (
    <HashRouter future={{ v7_startTransition: true, v7_relativeSplatPath: true }}>
      <Layout />
    </HashRouter>
  )
}
