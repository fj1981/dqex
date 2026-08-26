import { useCallback, useEffect, useRef, useState } from "react"
import { Group, Panel, Separator, useDefaultLayout } from "react-resizable-panels"
import { useTranslation } from "react-i18next"
import { AlertCircle, Braces, Check, ChevronLeft, ChevronRight, Code2, Copy, FunctionSquare, GripVertical, Lightbulb, List, Loader2, Pin, Plus, SlidersHorizontal, SkipForward, Sparkles, Star, Table2, View, X } from "lucide-react"
import { toast } from "sonner"
import DbTypeIcon from "@/components/DbTypeIcon"
import { Button } from "@/components/ui/button"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import {
  ContextMenu,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
  ContextMenuTrigger,
} from "@/components/ui/context-menu"
import { cn } from "@/lib/utils"
import { friendlyDBError } from "@/lib/dbError"
import { setSqlEditor } from "@/lib/editorRef"
import { formatEditorSQL, formatSQL } from "@/lib/sqlFormat"
import { useClickOutside } from "@/lib/useClickOutside"
import { defaultFavoriteTitle } from "@/lib/sql"
import { tKey } from "@/lib/i18n"
import { prompt } from "@/components/ui/alert-dialog"
import { useQueryStore, type WorkspaceTab } from "@/stores/queryStore"
import { useObjectTreeStore } from "@/stores/objectTreeStore"
import { useFavoriteStore } from "@/stores/favoriteStore"
import { DEFAULT_EVICT_ORDER, type EvictCategory } from "@/lib/tabSettings"
import SqlEditor, { type SqlEditorInstance } from "@/components/SqlEditor"
import ResultGrid from "@/components/ResultGrid"
import ObjectTree from "@/components/ObjectTree"
import TableBrowser from "@/components/TableBrowser"
import { useAppStore } from "@/stores/app"
import { pingConnection } from "@/api/sql"
import { getAIStatus } from "@/api"
import { AIPanel } from "@/components/AIPanel"
import { SQL_EXEC_MODE_LABEL, type AIStatus, type ObjectNode, type SQLExecMode } from "@/types"

// 合并后的类 Navicat 工作区：左侧常驻对象树 + 右侧多 Tab（查询 / 对象）
// 对象树始终显示，与右侧区域左右分栏；视图/函数/过程按类型分组展示

// 表名拆分：尾部（最后一个 _ 之后的部分，如 templates/logs，区分度最高）固定保留、头部可截断。
// 长表名时实现「中间省略、尾部优先」：空间不足时头部渐隐出省略号，尾部始终完整可见。
const splitTableName = (name: string): { head: string; tail: string } => {
  const idx = name.lastIndexOf("_")
  if (idx >= 3 && name.length - idx - 1 >= 3) {
    return { head: name.slice(0, idx + 1), tail: name.slice(idx + 1) }
  }
  return { head: name, tail: "" }
}

export default function WorkspaceLayout() {
  const { t } = useTranslation()
  const { connections, panelOpen } = useAppStore()
  const {
    tabs,
    activeId,
    running,
    mask,
    connId,
    setConnId,
    addTab,
    openObjectTab,
    closeTab,
    closeOthers,
    closeRight,
    closeAll,
    renameTab,
    setActiveTab,
    updateTabSql,
    updateTabDb,
    updateTabMode,
    setActiveResult,
    runTab,
    setMask,
    persistFailed,
    clearPersistFailed,
    setObjectSubTab,
    setObjectPage,
    setObjectViewLayout,
    togglePinTab,
    tabSettings,
    updateTabSettings,
  } = useQueryStore()
  const { loadTree, nodes: treeNodes } = useObjectTreeStore()
  const { add: addFavorite } = useFavoriteStore()
  // 连接健康状态：null=未检测 / checking=检测中 / ok=正常 / fail=不可用
  const [ping, setPing] = useState<null | "checking" | "ok" | "fail">(null)
  const [pingMs, setPingMs] = useState(0)
  // 左侧对象树折叠状态
  const [treeCollapsed, setTreeCollapsed] = useState(false)
  // AI 助手：面板开关 + 能力状态（未配置时入口隐藏）
  const [aiOpen, setAiOpen] = useState(false)
  const [aiStatus, setAiStatus] = useState<AIStatus | null>(null)
  // AI 采纳预览：预览中标记 + 预览前的原始 SQL（用于 diff 高亮与「取消」回滚）
  const [aiPreviewing, setAiPreviewing] = useState(false)
  const [aiPreviewBase, setAiPreviewBase] = useState("")
  // AI 快捷触发请求：编辑器「解释/优化」、报错卡片「AI 修复」点击后设置，AIPanel 消费后清除
  const [quickRequest, setQuickRequest] = useState<{ action: "explain" | "optimize" | "fix"; text: string } | null>(null)
  // 主 SQL 编辑器实例 + 光标/选中状态（供 AI 插入定位：替换所选/插入光标/追加末尾）
  const sqlEditorRef = useRef<SqlEditorInstance | null>(null)
  const editorSelectionRef = useRef<{ hasSelection: boolean; selectionOffset: number; selectionLength: number; cursorOffset: number }>({ hasSelection: false, selectionOffset: -1, selectionLength: 0, cursorOffset: -1 })
  // 是否有选中（用 state 驱动 AIPanel 菜单项显示；仅在值变化时更新，避免光标移动导致高频重渲染）
  const [hasEditorSelection, setHasEditorSelection] = useState(false)
  useEffect(() => {
    let alive = true
    getAIStatus()
      .then((st) => {
        if (alive) setAiStatus(st)
      })
      .catch(() => {
        if (alive) setAiStatus({ enabled: false, baseUrl: "", model: "", temperature: 0, maxTokens: 0, timeoutSec: 0, maxSchemaChars: 0, hasPrompt: false })
      })
    return () => {
      alive = false
    }
  }, [])
  // 双击重命名状态
  const [renamingId, setRenamingId] = useState<string | null>(null)
  const [renameValue, setRenameValue] = useState("")
  // 顶层 tab 栏滚动容器 + 当前激活 tab 元素引用，用于 tab 过多时自动滚动到激活 tab
  const tabsScrollRef = useRef<HTMLDivElement>(null)
  const activeTabRef = useRef<HTMLDivElement>(null)
  // 左右滚动按钮：记录「能否继续向左右滚动」，用于禁用对应按钮
  const [canScrollLeft, setCanScrollLeft] = useState(false)
  const [canScrollRight, setCanScrollRight] = useState(false)
  // tab 纵向列表弹层：点击按钮展开，列出全部 tab 供快速激活
  const [tabListOpen, setTabListOpen] = useState(false)
  // tab 设置面板：最大标签页数 + 淘汰优先级排列
  const [tabSettingsOpen, setTabSettingsOpen] = useState(false)
  // 本地编辑状态（打开面板时从 store 读取，关闭时写回 store）
  const [localMaxTabs, setLocalMaxTabs] = useState(tabSettings.maxTabs ?? 20)
  const [localEvictOrder, setLocalEvictOrder] = useState<EvictCategory[]>((tabSettings.evictOrder ?? DEFAULT_EVICT_ORDER) as EvictCategory[])
  const [localMaxTabWidth, setLocalMaxTabWidth] = useState(tabSettings.maxTabWidth ?? 160)
  // 拖拽排序状态
  const [dragIdx, setDragIdx] = useState<number | null>(null)
  // 实际执行 SQL 详情弹层：点击「实际执行」语句查看完整内容（展示格式化后语句）
  const [sqlDetail, setSqlDetail] = useState<string | null>(null)
  const [sqlDetailFormatted, setSqlDetailFormatted] = useState<string>("")
  useEffect(() => {
    if (!sqlDetail) return
    let alive = true
    // 弹窗内展示格式化后的 SQL；失败时回退原文
    formatSQL(sqlDetail)
      .then((f) => alive && setSqlDetailFormatted(f))
      .catch(() => alive && setSqlDetailFormatted(sqlDetail))
    return () => {
      alive = false
    }
  }, [sqlDetail])
  const tabListRef = useRef<HTMLDivElement>(null)
  useClickOutside(tabListRef, () => setTabListOpen(false), tabListOpen)
  const tabSettingsRef = useRef<HTMLDivElement>(null)
  useClickOutside(tabSettingsRef, () => {
    // 关闭时保存设置到 store（store 会自动持久化到后端）
    updateTabSettings({
      maxTabs: localMaxTabs,
      evictOrder: localEvictOrder,
      maxTabWidth: localMaxTabWidth,
    })
    setTabSettingsOpen(false)
  }, tabSettingsOpen)

  // 更新左右滚动按钮的可用状态（依据当前 scrollLeft 与可滚动宽度）
  const updateScrollButtons = useCallback(() => {
    const el = tabsScrollRef.current
    if (!el) return
    setCanScrollLeft(el.scrollLeft > 0)
    setCanScrollRight(el.scrollLeft + el.clientWidth < el.scrollWidth - 1)
  }, [])

  // 点击左右按钮：按容器可见宽度的一定量横向滚动
  const scrollTabs = useCallback((dir: -1 | 1) => {
    const el = tabsScrollRef.current
    if (!el) return
    el.scrollBy({ left: dir * el.clientWidth * 0.8, behavior: "smooth" })
  }, [])

  const conn = connections.find((c) => c.id === connId)
  // 标记当前 connId 因失效被主动清空（连接被删除/重建），避免清空后又被「首次自动选第一个」逻辑立刻回填
  const invalidatedRef = useRef(false)

  // 库候选列表：对象树里 type=db 的节点名（空 = 连接默认库）
  const dbList = treeNodes.filter((n) => n.type === "db").map((n) => n.name)

  // 连接状态圆点的悬浮文案：状态 + 响应延时
  const statusTitle =
    ping === "checking"
      ? t("workspace.connChecking")
      : ping === "ok"
        ? t("workspace.connOk", { ms: pingMs })
        : ping === "fail"
          ? t("workspace.connFail")
          : t("workspace.connCheck")

  // 连接状态自愈：
  // 1) 首次进入若连接列表已加载、尚未选连接，自动选中第一个（供右侧历史面板感知）；
  // 2) 当前 connId 已不在连接列表（连接被删除/重建），清空 + 提示，保持「选择连接…」待用户手动重选，
  //    避免带着「幽灵 ID」请求后端报「连接配置不存在」。
  useEffect(() => {
    if (connections.length === 0) return
    if (!connId) {
      // 失效清空后：不自动回填，等待用户手动选择
      if (invalidatedRef.current) return
      setConnId(connections[0].id)
      return
    }
    if (!connections.some((c) => c.id === connId)) {
      invalidatedRef.current = true
      setConnId("")
      useObjectTreeStore.getState().clear()
      toast.error(t("workspace.connInvalid"))
      return
    }
    // connId 有效：重置失效标记（用户已手动重选，后续若再次删除可再次触发自愈）
    invalidatedRef.current = false
  }, [connId, connections, setConnId])

  // 查询 tab 编辑器/结果区上下分割布局，自动持久化到 localStorage
  const querySplit = useDefaultLayout({ id: "dqex-query-split" })
  // AI 面板横向宽度（右侧面板，可拖动调整，比例存 localStorage）
  const aiSplit = useDefaultLayout({ id: "dqex-ai-split" })

  const checkHealth = useCallback(async () => {
    if (!connId) return
    setPing("checking")
    try {
      const r = await pingConnection(connId)
      setPingMs(r.elapsedMs)
      setPing(r.ok ? "ok" : "fail")
      if (!r.ok) toast.error(t("workspace.connUnavailable", { msg: r.error || t("common.unknown") }))
    } catch (e) {
      setPing("fail")
      toast.error(t("workspace.connCheckFail", { msg: (e as Error).message }))
    }
  }, [connId, t])

  // 读取快捷动作（解释/优化）的作用对象 SQL，优先级：
  //   1. 编辑器当前选中部分（有选中）
  //   2. 上次实际执行的 SQL（选中执行=选中部分，全文执行=全文）
  //   3. 当前 query tab 的完整 SQL（兜底）
  const getTargetSql = useCallback((): string => {
    const ed = sqlEditorRef.current
    const m = ed?.getModel()
    const s = ed?.getSelection()
    if (m && s && !s.isEmpty()) {
      return m.getValueInRange(s).trim()
    }
    // 无选中或编辑器不可用：优先上次执行的 SQL，否则回退为当前 query tab 的完整 SQL
    const active = tabs.find((t) => t.id === activeId)
    if (active && active.kind === "query") {
      return (active.lastExecSql || active.sql).trim()
    }
    return ""
  }, [tabs, activeId])

  // 快捷触发解释/优化：打开 AI 面板并以指定动作立即发送（对象 = 选中 SQL 或全文）
  const triggerQuickAction = useCallback(
    (action: "explain" | "optimize") => {
      const sql = getTargetSql()
      if (!sql) {
        toast.info(t("workspace.noSql"))
        return
      }
      setAiOpen(true)
      setQuickRequest({ action, text: sql })
    },
    [getTargetSql, t],
  )

  // 切换激活 tab 时，自动把激活 tab 滚动到视区内（tab 过多时不依赖用户手动横向滚动）
  useEffect(() => {
    const el = activeTabRef.current
    if (!el) return
    // 使用 nearest：tab 已可见时不滚动，未可见时最小距离滚到视区边缘，避免不必要的位移
    el.scrollIntoView({ behavior: "smooth", block: "nearest", inline: "nearest" })
  }, [activeId])

  // 监听 tab 栏滚动，同步左右滚动按钮的可用状态；同时初始化一次（tabs 数量变化后也需重算）
  useEffect(() => {
    const el = tabsScrollRef.current
    if (!el) return
    updateScrollButtons()
    el.addEventListener("scroll", updateScrollButtons, { passive: true })
    // ResizeObserver 监听容器宽度变化（窗口缩放、tab 增删导致可滚动宽度变化）
    const ro = new ResizeObserver(updateScrollButtons)
    ro.observe(el)
    return () => {
      el.removeEventListener("scroll", updateScrollButtons)
      ro.disconnect()
    }
  }, [tabs, updateScrollButtons])

  // 连接就绪后：加载对象树 + 自动健康检测
  useEffect(() => {
    if (!connId) return
    loadTree(connId)
    checkHealth()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [connId])

  const active = tabs.find((t) => t.id === activeId)
  const queryActive = active && active.kind === "query" ? active : null
  const objectActive = active && active.kind === "object" ? active : null
  // 查询失败错误友好化：识别连接类错误（connection refused 等）时展示中文标题/原因/排查建议
  const qErr = queryActive?.error ? friendlyDBError(queryActive.error) : null

  // 同库去重：当前连接打开的多个对象 tab 若都在同一库，标签省略 [库名] 后缀（完整库名见 hover title），
  // 避免每个 tab 重复携带相同库名；跨库混开时才显示 [库名] 以区分
  const objectDbs = Array.from(new Set(tabs.filter((t) => t.kind === "object").map((t) => t.db)))
  const showDbSuffix = objectDbs.length > 1

  // 对象树点击：表/视图打开对象 tab（默认看数据），函数/过程打开对象 tab（仅 DDL）
  const handleOpenObject = (name: string, db: string, type: ObjectNode["type"]) => {
    if (type === "table" || type === "view" || type === "function" || type === "procedure") {
      openObjectTab(db, name, type)
    }
  }

  const renderTabLabel = (t: WorkspaceTab) => {
    const pinIcon = t.pinned && <Pin className="h-3.5 w-3.5 shrink-0 text-primary/70" />
    if (t.kind === "object") {
      const meta = t.objType === "view"
        ? { icon: View, cls: "text-cyan-600" }
        : t.objType === "function" || t.objType === "procedure"
          ? { icon: FunctionSquare, cls: "text-violet-600" }
          : { icon: Table2, cls: "text-emerald-600" }
      const Icon = meta.icon
      // 表名拆分：头部可截断、尾部固定保留（同库时还省去 [库名]，空间更充裕）。
      // PG 限定名 "schema.表" 先去 schema 前缀（tab 去重/查询仍用完整限定名），与对象树叶子展示一致
      const bare = t.name.includes(".") ? t.name.slice(t.name.indexOf(".") + 1) : t.name
      const { head, tail } = splitTableName(bare)
      return (
        <>
          {pinIcon}
          <Icon className={cn("h-3.5 w-3.5", meta.cls)} />
          {/* 表名：头部 flex-1 占满剩余空间可截断、尾部（最后一个 _ 后的部分）永远完整；
              跨库时才带 [库名] 后缀（最多 96px，可截断），不挤占表名空间。 */}
          <span className="flex min-w-0 max-w-40 items-center" title={`${t.db}.${t.name}`}>
            <span className="min-w-0 flex-1 truncate">{head}</span>
            {tail && <span className="shrink-0">{tail}</span>}
            {showDbSuffix && <span className="max-w-24 shrink-0 truncate text-muted-foreground">[{t.db}]</span>}
          </span>
        </>
      )
    }
    return (
      <>
        {pinIcon}
        <Code2 className="h-3.5 w-3.5 text-muted-foreground" />
        <span className="max-w-32 truncate">{t.title}</span>
      </>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col">
      {/* 左右分栏：左侧常驻对象树 + 右侧 Tab 内容区（顶部无独立连接栏） */}
      <div className="flex min-h-0 flex-1">
        {/* 左侧对象树（可折叠） */}
        {treeCollapsed ? (
          <div className="flex w-9 shrink-0 flex-col items-center border-r bg-muted/20 py-2">
            <Button
              variant="ghost"
              size="icon"
              className="h-7 w-7 text-muted-foreground hover:text-foreground"
              title={t("workspace.expandTree")}
              onClick={() => setTreeCollapsed(false)}
            >
              <ChevronRight className="h-4 w-4" />
            </Button>
          </div>
        ) : (
          <aside className="flex w-60 shrink-0 flex-col border-r bg-muted/20">
            <div className="flex shrink-0 items-center gap-1.5 border-b px-2 py-2">
              {/* 连接选择器：放在对象树顶部，与对象树同属「连接级」资源域，
                  切换连接时无需跨越整个 tab 区，操作更顺手。「对象」标题和折叠按钮放在右侧同一行。
                  健康圆点保留在 trigger 内，便于手动复检。 */}
              <Select value={connId} onValueChange={setConnId}>
                {/* 窄栏放不下 host:port 文本，完整地址挂 title 悬停可见（下拉选项里仍完整展示） */}
                <SelectTrigger
                  className="h-7 min-w-0 flex-1 px-2 text-xs"
                  title={conn ? `${conn.name} · ${conn.conn.Host}:${conn.conn.Port}` : undefined}
                >
                  {conn ? (
                    <span className="flex min-w-0 items-center gap-1.5">
                      <span
                        role="button"
                        tabIndex={0}
                        title={statusTitle}
                        onClick={(e) => {
                          e.stopPropagation()
                          checkHealth()
                        }}
                        onKeyDown={(e) => e.key === "Enter" && checkHealth()}
                        className={cn(
                          "h-2.5 w-2.5 shrink-0 rounded-full ring-2 ring-inset ring-foreground/10",
                          ping === "checking"
                            ? "animate-pulse bg-blue-500"
                            : ping === "ok"
                              ? "bg-emerald-500"
                              : ping === "fail"
                                ? "bg-destructive"
                                : "bg-muted-foreground",
                        )}
                      />
                      <DbTypeIcon type={conn.conn.Type} />
                      <span className="min-w-0 flex-1 truncate font-medium">{conn.name}</span>
                    </span>
                  ) : (
                    <span className="text-muted-foreground">{t("workspace.selectConn")}</span>
                  )}
                </SelectTrigger>
                <SelectContent className="w-[280px]">
                  {connections.length === 0 ? (
                    <div className="px-2 py-4 text-center text-sm text-muted-foreground">
                      {t("workspace.noConnNew")}
                    </div>
                  ) : (
                    connections.map((c) => (
                      <SelectItem
                        key={c.id}
                        value={c.id}
                        // 选中项浅色底 + 对勾，未选中仅悬停高亮
                        className="data-[state=checked]:bg-primary/5"
                      >
                        <span className="flex min-w-0 flex-col gap-0.5">
                          <span className="flex min-w-0 items-center gap-2">
                            <DbTypeIcon type={c.conn.Type} />
                            <span className="min-w-0 flex-1 truncate font-medium">{c.name}</span>
                            {c.shortName && (
                              <span className="shrink-0 text-xs font-mono text-muted-foreground">({c.shortName})</span>
                            )}
                          </span>
                          <span className="truncate pl-6 font-mono text-xs text-muted-foreground">
                            {c.conn.Host}:{c.conn.Port}
                          </span>
                        </span>
                      </SelectItem>
                    ))
                  )}
                </SelectContent>
              </Select>
              <span className="shrink-0 text-xs font-medium text-muted-foreground">{t("workspace.objects")}</span>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6 shrink-0 text-muted-foreground hover:text-foreground"
                title={t("workspace.collapseTree")}
                onClick={() => setTreeCollapsed(true)}
              >
                <ChevronLeft className="h-3.5 w-3.5" />
              </Button>
            </div>
            <ObjectTree onOpenObject={handleOpenObject} />
          </aside>
        )}

        {/* 右侧：Tab 栏 + 内容 */}
        <main className="flex min-h-0 min-w-0 flex-1 flex-col bg-background">
          {/* Tab 栏：左滚动按钮 + tab 列表 + 右滚动按钮（取代底部滚动条）+ 右侧操作按钮 */}
          <div className="flex shrink-0 items-center border-b bg-muted/20 pl-2 pr-2">
            {/* 左滚动按钮：有更多左侧 tab 时可点，点击向左滚动 */}
            <button
              type="button"
              className={cn(
                "flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors",
                canScrollLeft ? "hover:bg-accent hover:text-foreground" : "cursor-default opacity-30",
              )}
              disabled={!canScrollLeft}
              title={t("workspace.scrollLeft")}
              onClick={() => scrollTabs(-1)}
            >
              <ChevronLeft className="h-3.5 w-3.5" />
            </button>
            <div
              ref={tabsScrollRef}
              className="flex min-w-0 items-center gap-1 overflow-x-auto pt-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
            >
              {tabs.map((tab) => (
                <ContextMenu key={tab.id}>
                  <ContextMenuTrigger asChild>
                    <div
                      ref={tab.id === activeId ? activeTabRef : null}
                      style={{ maxWidth: localMaxTabWidth }}
                      className={cn(
                        "group flex shrink-0 cursor-pointer items-center gap-1 rounded-t-md border border-b-0 px-2.5 py-1.5 text-xs transition-colors",
                        tab.id === activeId
                          ? "border-border bg-background font-medium text-foreground"
                          : "border-transparent text-muted-foreground/60 hover:bg-accent hover:text-muted-foreground/80",
                      )}
                      onClick={() => setActiveTab(tab.id)}
                      // 中键点击关闭（浏览器 tab 行为）
                      onAuxClick={(e) => {
                        if (e.button === 1) closeTab(tab.id)
                      }}
                      // 双击重命名（仅 query tab）
                      onDoubleClick={() => {
                        if (tab.kind === "query") {
                          setRenamingId(tab.id)
                          setRenameValue(tab.title)
                        }
                      }}
                    >
                      {renamingId === tab.id ? (
                        <input
                          autoFocus
                          className="h-4 w-24 rounded-sm border border-border bg-background px-1 text-xs leading-4 outline-none focus:ring-1 focus:ring-ring"
                          value={renameValue}
                          onChange={(e) => setRenameValue(e.target.value)}
                          onClick={(e) => e.stopPropagation()}
                          onBlur={() => {
                            renameTab(tab.id, renameValue)
                            setRenamingId(null)
                          }}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") {
                              renameTab(tab.id, renameValue)
                              setRenamingId(null)
                            } else if (e.key === "Escape") {
                              setRenamingId(null)
                            }
                          }}
                        />
                      ) : (
                        renderTabLabel(tab)
                      )}
                      <button
                        type="button"
                        className="ml-0.5 rounded p-0.5 text-muted-foreground/50 hover:bg-accent hover:text-foreground"
                        onClick={(e) => {
                          e.stopPropagation()
                          closeTab(tab.id)
                        }}
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    <ContextMenuItem onSelect={() => togglePinTab(tab.id)}>
                      <Pin className="mr-1.5 h-3.5 w-3.5" />
                      {tab.pinned ? t("workspace.unpinTab") : t("workspace.pinTab")}
                    </ContextMenuItem>
                    {tab.kind === "query" && (
                      <>
                        <ContextMenuItem
                          onSelect={() => {
                            setRenamingId(tab.id)
                            setRenameValue(tab.title)
                          }}
                        >
                          {t("common.rename")}
                        </ContextMenuItem>
                      </>
                    )}
                    <ContextMenuSeparator />
                    <ContextMenuItem onSelect={() => closeTab(tab.id)}>{t("common.close")}</ContextMenuItem>
                    <ContextMenuItem onSelect={() => closeOthers(tab.id)}>{t("workspace.closeOthers")}</ContextMenuItem>
                    <ContextMenuItem
                      disabled={tabs.findIndex((x) => x.id === tab.id) === tabs.length - 1}
                      onSelect={() => closeRight(tab.id)}
                    >
                      {t("workspace.closeRight")}
                    </ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem onSelect={() => closeAll()}>{t("workspace.closeAll")}</ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              ))}
            </div>
            {/* 右滚动按钮：有更多右侧 tab 时可点，点击向右滚动 */}
            <button
              type="button"
              className={cn(
                "flex h-6 w-6 shrink-0 items-center justify-center rounded text-muted-foreground transition-colors",
                canScrollRight ? "hover:bg-accent hover:text-foreground" : "cursor-default opacity-30",
              )}
              disabled={!canScrollRight}
              title={t("workspace.scrollRight")}
              onClick={() => scrollTabs(1)}
            >
              <ChevronRight className="h-3.5 w-3.5" />
            </button>

            {/* tab 纵向列表：点击展开，纵向列出全部 tab 供快速激活（tab 过多时替代横向滚动查找） */}
            <div className="relative shrink-0" ref={tabListRef}>
              <button
                type="button"
                className={cn(
                  "flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
                  tabListOpen && "bg-accent text-foreground",
                )}
                title={t("workspace.listTabs")}
                onClick={() => setTabListOpen((v) => !v)}
              >
                <List className="h-3.5 w-3.5" />
              </button>
              {tabListOpen && (
                <div className="scrollbar-thin absolute right-0 top-full z-50 mt-1 max-h-80 w-64 overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-md">
                  <div className="flex items-center justify-between px-2 py-1">
                    <span className="text-[11px] font-medium text-muted-foreground">{t("workspace.tabs")}</span>
                    <span className="text-[10px] tabular-nums text-muted-foreground">{tabs.length}</span>
                  </div>
                  {tabs.length === 0 ? (
                    <div className="px-2 py-4 text-center text-xs text-muted-foreground">{t("workspace.noTabs")}</div>
                  ) : (
                    tabs.map((t) => (
                      <button
                        key={t.id}
                        type="button"
                        className={cn(
                          "flex w-full items-center gap-1.5 rounded-sm px-2 py-1.5 text-left text-xs transition-colors",
                          t.id === activeId ? "bg-primary/10 text-primary" : "hover:bg-accent",
                        )}
                        onClick={() => {
                          setActiveTab(t.id)
                          setTabListOpen(false)
                        }}
                      >
                        {renderTabLabel(t)}
                        {t.id === activeId && <Check className="ml-auto h-3.5 w-3.5 shrink-0" />}
                      </button>
                    ))
                  )}
                </div>
              )}
            </div>

            {/* Tab 设置按钮：最大标签页数 + 淘汰优先级排列（与 tab 列表按钮并排，都是 tab 管理操作） */}
            <div className="relative shrink-0" ref={tabSettingsRef}>
              <button
                type="button"
                className={cn(
                  "flex h-6 w-6 items-center justify-center rounded text-muted-foreground transition-colors hover:bg-accent hover:text-foreground",
                  tabSettingsOpen && "bg-accent text-foreground",
                )}
                title={t("workspace.tabSettings")}
                onClick={() => {
                  if (!tabSettingsOpen) {
                    // 打开时从 store 读取当前值
                    setLocalMaxTabs(tabSettings.maxTabs ?? 20)
                    setLocalEvictOrder((tabSettings.evictOrder ?? DEFAULT_EVICT_ORDER) as EvictCategory[])
                    setLocalMaxTabWidth(tabSettings.maxTabWidth ?? 160)
                  } else {
                    // 关闭时保存到 store
                    updateTabSettings({
                      maxTabs: localMaxTabs,
                      evictOrder: localEvictOrder,
                      maxTabWidth: localMaxTabWidth,
                    })
                  }
                  setTabSettingsOpen((v) => !v)
                }}
              >
                <SlidersHorizontal className="h-3.5 w-3.5" />
              </button>
              {tabSettingsOpen && (
                <div className="absolute right-0 top-full z-50 mt-1 w-72 rounded-md border bg-popover p-3 text-popover-foreground shadow-md">
                  <div className="mb-3">
                    <label className="mb-1 block text-[11px] font-medium text-muted-foreground">
                      {t("workspace.maxTabs")}
                    </label>
                    <input
                      type="number"
                      min={5}
                      max={100}
                      value={localMaxTabs}
                      onChange={(e) => {
                        const v = parseInt(e.target.value, 10)
                        if (!isNaN(v) && v >= 5 && v <= 100) setLocalMaxTabs(v)
                      }}
                      className="h-7 w-full rounded-md border bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
                    />
                  </div>
                  <div className="mb-3">
                    <label className="mb-1 block text-[11px] font-medium text-muted-foreground">
                      {t("workspace.maxTabWidth")}
                    </label>
                    <div className="mb-1 text-[10px] text-muted-foreground">{t("workspace.maxTabWidthHint")}</div>
                    <input
                      type="number"
                      min={80}
                      max={300}
                      value={localMaxTabWidth}
                      onChange={(e) => {
                        const v = parseInt(e.target.value, 10)
                        if (!isNaN(v) && v >= 80 && v <= 300) setLocalMaxTabWidth(v)
                      }}
                      className="h-7 w-full rounded-md border bg-background px-2 text-xs outline-none focus:ring-1 focus:ring-ring"
                    />
                  </div>
                  <div>
                    <label className="mb-1 block text-[11px] font-medium text-muted-foreground">
                      {t("workspace.evictPriority")}
                    </label>
                    <div className="mb-1 text-[10px] text-muted-foreground">{t("workspace.evictPriorityHint")}</div>
                    <div className="space-y-0.5">
                      {localEvictOrder.map((cat, idx) => (
                        <div
                          key={cat}
                          draggable
                          onDragStart={() => setDragIdx(idx)}
                          onDragOver={(e) => e.preventDefault()}
                          onDrop={() => {
                            if (dragIdx === null || dragIdx === idx) return
                            const next = [...localEvictOrder]
                            const [item] = next.splice(dragIdx, 1)
                            next.splice(idx, 0, item)
                            setLocalEvictOrder(next)
                            setDragIdx(null)
                          }}
                          onDragEnd={() => setDragIdx(null)}
                          className={cn(
                            "flex cursor-grab items-center gap-2 rounded-md border bg-background px-2 py-1.5 text-xs transition-colors active:cursor-grabbing",
                            dragIdx === idx && "opacity-50",
                          )}
                        >
                          <GripVertical className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                          <span className="flex-1">
                            {cat === "empty_query" && t("workspace.evictEmptyQuery")}
                            {cat === "sql_no_result" && t("workspace.evictSqlNoResult")}
                            {cat === "query_with_result" && t("workspace.evictQueryWithResult")}
                            {cat === "object_no_data" && t("workspace.evictObjectNoData")}
                            {cat === "object_with_data" && t("workspace.evictObjectWithData")}
                          </span>
                          <span className="text-[10px] text-muted-foreground">{idx + 1}</span>
                        </div>
                      ))}
                    </div>
                  </div>
                </div>
              )}
            </div>

            {/* 右侧操作按钮组：脱敏 + 新建查询；连接切换已迁到左侧对象树顶部；
                pr-9 为右上角展开/收起按钮让位（面板收起时才需要） */}
            <div className={cn("ml-auto flex shrink-0 items-center gap-2 py-1 pl-3", !panelOpen && "pr-9")}>
              <label className="flex h-6 items-center gap-1.5 text-[11px] text-muted-foreground" title={t("workspace.maskTitle")}>
                <Switch checked={mask} onCheckedChange={setMask} />
                {t("workspace.mask")}
              </label>

              <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => addTab()}>
                <Plus className="mr-1 h-3.5 w-3.5" /> {t("workspace.newQuery")}
              </Button>

              {/* AI 助手开关（配置启用时显示）：纯图标，位于「新建查询」右侧，对应右侧 AI 面板 */}
              {aiStatus?.enabled && (
                <Button
                  size="icon"
                  variant={aiOpen ? "secondary" : "outline"}
                  className={cn("h-7 w-7", aiOpen && "border-violet-500/50 text-violet-600")}
                  onClick={() => setAiOpen((v) => !v)}
                  title={aiOpen ? t("workspace.aiCollapse") : t("workspace.aiOpen")}
                >
                  <Sparkles className="h-3.5 w-3.5" />
                </Button>
              )}
            </div>
          </div>

          {/* 内容区 */}
          {queryActive ? (
            /* 查询 tab：编辑器 + 结果（中间分割条可拖动调整上下比例）；AI 面板在右侧，
               横向跨整个 tab 高度（含编辑器+结果区），宽度可拖动调整，比例存 localStorage */
            <Group orientation="horizontal" defaultLayout={aiSplit.defaultLayout} onLayoutChanged={aiSplit.onLayoutChanged} className="min-h-0 flex-1">
              {/* 左：编辑器 + 结果（上下分割） */}
              <Panel id="query" minSize="40" className="min-h-0 overflow-hidden">
                <Group orientation="vertical" defaultLayout={querySplit.defaultLayout} onLayoutChanged={querySplit.onLayoutChanged} className="h-full min-h-0">
              {/* 上：SQL 编辑器 + 工具栏 */}
              <Panel id="editor" defaultSize="50" minSize="15" className="flex min-h-0 flex-col overflow-hidden">
                <div className="flex min-w-0 flex-1 overflow-hidden">
                  <div className="flex min-h-0 min-w-0 flex-1 flex-col">
                    <SqlEditor
                      value={queryActive.sql}
                      onChange={(sql) => updateTabSql(queryActive.id, sql)}
                      onRun={(selection) => runTab(queryActive.id, selection)}
                      disabled={running}
                      placeholder={t("workspace.editorPlaceholder")}
                      diffBase={aiPreviewing ? aiPreviewBase : undefined}
                      onApply={() => {
                        // 确认替换：保留当前编辑器内容，退出对比模式
                        setAiPreviewBase("")
                        setAiPreviewing(false)
                        toast.success(t("workspace.appliedAI"))
                      }}
                      onCancel={() => {
                        // 取消：还原为替换前的原内容
                        updateTabSql(queryActive.id, aiPreviewBase)
                        setAiPreviewBase("")
                        setAiPreviewing(false)
                      }}
                      onReady={(ed) => {
                        sqlEditorRef.current = ed
                        setSqlEditor(ed)
                      }}
                      onSelectionChange={(info) => {
                        editorSelectionRef.current = {
                          hasSelection: info.hasSelection,
                          selectionOffset: info.selectionOffset,
                          selectionLength: info.selectionLength,
                          cursorOffset: info.cursorOffset,
                        }
                        // 仅在选中状态变化时更新 state，驱动菜单项显示（避免高频重渲染）
                        setHasEditorSelection((prev) => (prev === info.hasSelection ? prev : info.hasSelection))
                      }}
                    />
                  </div>
                </div>

                <div className="flex shrink-0 items-center justify-between border-t bg-muted/40 px-3 py-1.5">
                  {/* 左：AI 快捷动作（解释/优化）+ 成功时的结果统计（行数 / 耗时）；错误由下方结果区卡片展示，避免重复 */}
                  <div className="flex min-w-0 items-center gap-2">
                    <Button
                      size="sm"
                      variant="ghost"
                      className="h-7 gap-1 text-xs text-muted-foreground hover:text-foreground"
                      title={hasEditorSelection ? t("workspace.formatSelTitle") : t("workspace.formatTitle")}
                      onClick={() => {
                        const ed = sqlEditorRef.current
                        if (ed) formatEditorSQL(ed)
                      }}
                    >
                      <Braces className="h-3.5 w-3.5" /> {t("workspace.format")}
                    </Button>
                    {aiStatus?.enabled && (
                      <>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 text-xs text-muted-foreground hover:text-foreground"
                          title={hasEditorSelection ? t("workspace.explainSelTitle") : t("workspace.explainTitle")}
                          onClick={() => triggerQuickAction("explain")}
                        >
                          <Sparkles className="h-3.5 w-3.5 text-violet-500" /> {t("workspace.explain")}
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 text-xs text-muted-foreground hover:text-foreground"
                          title={hasEditorSelection ? t("workspace.optimizeSelTitle") : t("workspace.optimizeTitle")}
                          onClick={() => triggerQuickAction("optimize")}
                        >
                          <Sparkles className="h-3.5 w-3.5 text-violet-500" /> {t("workspace.optimize")}
                        </Button>
                        {/* 收藏当前 SQL：存入独立收藏表（按连接隔离），与右侧「收藏」Tab 联动 */}
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 text-xs text-muted-foreground hover:text-amber-600"
                          title={t("workspace.favoriteSqlTitle")}
                          disabled={!queryActive || !queryActive.sql.trim()}
                          onClick={async () => {
                            if (!queryActive) return
                            const sql = queryActive.sql.trim()
                            if (!sql) {
                              toast.info(t("workspace.noSql"))
                              return
                            }
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
                              await addFavorite(connId, sql, queryActive.db, queryActive.mode, title)
                              toast.success(t("app.favorited"))
                            } catch (e) {
                              toast.error(t("app.favoriteFailed", { msg: (e as Error).message }))
                            }
                          }}
                        >
                          <Star className="h-3.5 w-3.5" /> {t("app.favorite")}
                        </Button>
                      </>
                    )}
                    {queryActive.results.length > 0 && (() => {
                      const r = queryActive.results[queryActive.activeResult] ?? queryActive.results[0]
                      if (r.error) return null
                      // 行数与耗时统一在结果网格底部展示，这里只保留写操作影响行数与多结果集提示
                      const text = r.isWrite
                        ? t("common.affectedRows", { n: r.affectedRows })
                        : queryActive.results.length > 1
                          ? t("workspace.multiResults", { n: queryActive.results.length })
                          : ""
                      if (!text) return null
                      return (
                        <span className="text-xs tabular-nums text-muted-foreground">
                          {text}
                        </span>
                      )
                    })()}
                  </div>

                  {/* 右：库选择 + 执行模式 + 执行 */}
                  <div className="flex shrink-0 items-center gap-2">
                    <Select value={queryActive.db} onValueChange={(db) => updateTabDb(queryActive.id, db)}>
                      <SelectTrigger className="h-7 w-auto min-w-[130px] max-w-[240px] px-2 text-xs" title={t("workspace.targetDb")}>
                        {queryActive.db || <span className="text-muted-foreground">{t("workspace.defaultDb")}</span>}
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="">{t("workspace.defaultDb")}</SelectItem>
                        {dbList.map((db) => (
                          <SelectItem key={db} value={db}>
                            {db}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <Select
                      value={queryActive.mode}
                      onValueChange={(mode) => updateTabMode(queryActive.id, mode as SQLExecMode)}
                    >
                      <SelectTrigger
                        className="h-7 w-auto min-w-[100px] px-2 text-xs"
                        title={t("workspace.modeTitle")}
                      >
                        {tKey(SQL_EXEC_MODE_LABEL[queryActive.mode] ?? "app.execMode.transform")}
                      </SelectTrigger>
                      <SelectContent>
                        {(Object.keys(SQL_EXEC_MODE_LABEL) as SQLExecMode[]).map((m) => (
                          <SelectItem key={m} value={m}>
                            {tKey(SQL_EXEC_MODE_LABEL[m])}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    {/* 固定宽度避免执行中图标出现/消失时按钮宽度抖动 */}
                    <Button
                      size="sm"
                      className="h-7 w-[112px] text-xs"
                      onClick={() => {
                        const ed = sqlEditorRef.current
                        const m = ed?.getModel()
                        const s = ed?.getSelection()
                        const sel = m && s && !s.isEmpty() ? m.getValueInRange(s) : undefined
                        runTab(queryActive.id, sel)
                      }}
                      disabled={running}
                    >
                      {/* 固定宽度图标位：执行中显示 spinner，空闲时空占，保证文字不左右跳动 */}
                      <span className="flex w-4 shrink-0 items-center justify-center">
                        {running ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : null}
                      </span>
                      {t("workspace.run")}
                    </Button>
                  </div>
                </div>
              </Panel>

              {/* 分割条（可拖动） */}
              <Separator className="group relative z-10 flex items-center justify-center border-y bg-muted/40 transition-colors hover:bg-muted data-[resize-handle-active]:bg-primary/30">
                <div className="h-0.5 w-8 rounded-full bg-border transition-colors group-hover:bg-primary/60 group-data-[resize-handle-active]:bg-primary" />
              </Separator>

              {/* 下：结果区 */}
              <Panel id="result" defaultSize="50" minSize="15" className="min-h-0">
                <div className="h-full p-2 pt-1">
                  {queryActive.error ? (
                    <div className="flex max-h-full items-start gap-3 overflow-auto rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
                      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
                      <div className="min-w-0 flex-1">
                        <div className="font-medium text-destructive">{qErr?.title ?? t("common.execFailed")}</div>
                        {qErr ? (
                          <>
                            <div className="mt-0.5 break-words text-foreground/80">{qErr.reason}</div>
                            {/* 排查建议：可操作的具体步骤，替代笼统的「重试」引导 */}
                            <ul className="mt-1.5 space-y-0.5">
                              {qErr.advice.map((a, i) => (
                                <li key={i} className="flex items-start gap-1.5 text-xs text-foreground/70">
                                  <Lightbulb className="mt-0.5 h-3 w-3 shrink-0 text-amber-500" />
                                  <span className="min-w-0 break-words">{a}</span>
                                </li>
                              ))}
                            </ul>
                          </>
                        ) : (
                          <div className="mt-0.5 break-words text-foreground/80">{queryActive.error}</div>
                        )}
                      </div>
                      {/* 一键修复：自动打开 AI 面板并以「修复」动作附带报错信息 + 出错 SQL 触发。
                          作用对象 = 上次实际执行的 SQL（选中执行=选中部分，全文执行=全文），非编辑器全文 */}
                      {aiStatus?.enabled && (
                        <Button
                          size="sm"
                          variant="outline"
                          className="h-7 shrink-0 gap-1 text-xs"
                          onClick={() => {
                            setAiOpen(true)
                            const failSql = getTargetSql()
                            setQuickRequest({
                              action: "fix",
                              text: t("workspace.fixPrompt", { sql: failSql, err: queryActive.error }),
                            })
                          }}
                        >
                          <Sparkles className="h-3.5 w-3.5 text-violet-500" />
                          {t("workspace.aiFix")}
                        </Button>
                      )}
                    </div>
                  ) : queryActive.results.length > 0 ? (
                    <div className="flex h-full min-h-0 flex-col gap-1.5">
                      {/* 多结果集编号 tab（Navicat 式）：多语句批量执行时每个结果集一个 tab */}
                      {queryActive.results.length > 1 && (
                        <div className="flex shrink-0 items-center gap-1 border-b pb-1">
                          {queryActive.results.map((r, i) => (
                            <button
                              key={i}
                              onClick={() => setActiveResult(queryActive.id, i)}
                              className={cn(
                                "rounded px-2 py-0.5 text-[11px] transition-colors",
                                queryActive.activeResult === i
                                  ? "bg-primary text-primary-foreground"
                                  : "text-muted-foreground hover:bg-muted",
                              )}
                            >
                              {t("workspace.resultN", { n: i + 1 })}
                              {r.error ? (
                                <span className="font-semibold text-destructive">{t("workspace.resultError")}</span>
                              ) : r.skipped ? (
                                <span className="text-muted-foreground">{t("workspace.resultSkipped")}</span>
                              ) : r.isWrite ? (
                                t("workspace.resultWrite")
                              ) : (
                                ""
                              )}
                            </button>
                          ))}
                        </div>
                      )}
                      {(() => {
                        const r = queryActive.results[queryActive.activeResult] ?? queryActive.results[0]
                        return (
                          <>
                            {/* 实际执行 SQL：展示数据库真实收到的语句 */}
                            <div className="flex shrink-0 items-center gap-2 rounded-md border bg-muted/40 px-2.5 py-1.5">
                              <span className="shrink-0 text-[11px] text-muted-foreground">{t("workspace.actualExec")}</span>
                              <button
                                type="button"
                                className="min-w-0 flex-1 cursor-pointer truncate text-left font-mono text-[12px] text-foreground/80 hover:text-foreground hover:underline hover:decoration-dotted"
                                title={t("workspace.viewFullSQL")}
                                onClick={() => setSqlDetail(r.sql)}
                              >
                                {r.sql}
                              </button>
                            </div>
                            {r.error ? (
                              // 语句级执行失败：对应结果 tab 展示错误卡（语句原文 + 报错信息），
                              // 一键修复上下文 = 出错语句原文 + 报错（不再用「第 N 条」序号，大模型直接定位出错 SQL）
                              // 外层 overflow-auto + 卡片 m-auto：空间充足时居中；内容超高时整卡可滚动，不被结果区截断
                              <div className="flex min-h-0 flex-1 overflow-auto p-2">
                                <div className="m-auto flex w-full max-w-2xl items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
                                  <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
                                  <div className="min-w-0 flex-1">
                                    <div className="font-medium text-destructive">{t("workspace.stmtFailed")}</div>
                                    <div className="mt-1 max-h-40 overflow-auto break-words whitespace-pre-wrap rounded bg-muted/50 p-2 font-mono text-[12px] text-foreground/80">
                                      {r.sql}
                                    </div>
                                    <div className="mt-1.5 break-words text-foreground/80">{r.error}</div>
                                  </div>
                                  {aiStatus?.enabled && (
                                    <Button
                                      size="sm"
                                      variant="outline"
                                      className="h-7 shrink-0 gap-1 text-xs"
                                      onClick={() => {
                                        setAiOpen(true)
                                        setQuickRequest({
                                          action: "fix",
                                          text: t("workspace.fixPrompt", { sql: r.sql, err: r.error }),
                                        })
                                      }}
                                    >
                                      <Sparkles className="h-3.5 w-3.5 text-violet-500" />
                                      {t("workspace.aiFix")}
                                    </Button>
                                  )}
                                </div>
                              </div>
                            ) : r.skipped ? (
                              // 未执行：前面语句执行失败被跳过（无结果，仅占位提示）
                              <div className="flex min-h-0 flex-1 overflow-auto p-2">
                                <div className="m-auto flex items-center gap-5 rounded-lg border bg-muted/30 px-6 py-4">
                                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-muted">
                                    <SkipForward className="h-5 w-5 text-muted-foreground" />
                                  </div>
                                  <div className="flex flex-col gap-0.5">
                                    <span className="text-sm font-medium text-muted-foreground">{t("workspace.notExecuted")}</span>
                                    <span className="text-xs text-muted-foreground/70">{t("workspace.notExecutedDesc")}</span>
                                  </div>
                                </div>
                              </div>
                            ) : r.isWrite || !r.rows ? (
                              // 写语句无结果集：不渲染 ResultGrid，展示成功状态卡片（影响行数 + 耗时）
                              <div className="flex min-h-0 flex-1 overflow-auto p-2">
                                <div className="m-auto flex items-center gap-5 rounded-lg border bg-muted/30 px-6 py-4">
                                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-emerald-500/15">
                                    <Check className="h-5 w-5 text-emerald-500" />
                                  </div>
                                  <div className="flex flex-col gap-0.5">
                                    <span className="text-sm font-medium">{t("workspace.execSuccess")}</span>
                                    <span className="text-xs text-muted-foreground">{t("workspace.writeNoResult")}</span>
                                  </div>
                                  <div className="h-8 w-px shrink-0 bg-border" />
                                  <div className="flex items-center gap-6">
                                    <div className="flex flex-col gap-0.5">
                                      <span className="text-[11px] text-muted-foreground">{t("workspace.affectedRowsLabel")}</span>
                                      <span className="text-sm font-medium tabular-nums">{r.affectedRows}</span>
                                    </div>
                                    <div className="flex flex-col gap-0.5">
                                      <span className="text-[11px] text-muted-foreground">{t("workspace.elapsedLabel")}</span>
                                      <span className="text-sm font-medium tabular-nums">{r.elapsedMs} ms</span>
                                    </div>
                                  </div>
                                </div>
                              </div>
                            ) : (
                              <div className="min-h-0 flex-1">
                                <ResultGrid result={r} />
                              </div>
                            )}
                          </>
                        )
                      })()}
                    </div>
                  ) : (
                    <div className="flex h-full flex-col items-center justify-center gap-1.5 text-sm text-muted-foreground">
                      {queryActive.sql.trim() ? (
                        <>
                          <span>{t("workspace.sqlSaved")}</span>
                          <span className="text-xs text-muted-foreground/80">
                            {t("workspace.runAgain")}
                          </span>
                        </>
                      ) : (
                        <span>{t("workspace.writePrompt")}</span>
                      )}
                    </div>
                  )}
                </div>
              </Panel>
                </Group>
              </Panel>

              {/* 右侧 AI 面板：跨整个 tab 高度，宽度可拖动 */}
              {aiOpen && (
                <>
                  <Separator className="group relative z-10 flex items-center justify-center border-x bg-muted/40 transition-colors hover:bg-muted data-[resize-handle-active]:bg-primary/30">
                    <div className="h-8 w-0.5 rounded-full bg-border transition-colors group-hover:bg-primary/60 group-data-[resize-handle-active]:bg-primary" />
                  </Separator>
                  <Panel id="ai" defaultSize="24" minSize="18" maxSize="60" className="min-h-0 overflow-hidden">
                    <AIPanel
                      connId={connId}
                      db={queryActive.db}
                      tabId={queryActive.id}
                      quickRequest={quickRequest}
                      onQuickConsumed={() => setQuickRequest(null)}
                      onPreviewSql={(sql, action) => {
                        // 进入采纳预览：按插入动作（替换所选/插入光标/追加末尾）计算最终 SQL
                        const base = queryActive.sql
                        // 优先实时从编辑器实例读取光标/选中偏移（权威来源），
                        // 避免依赖 editorSelectionRef 缓存：该缓存经 onSelectionChange 异步上报，
                        // 若上报未及时触发会停留在初始 -1，导致「插入光标处/替换所选」分支静默不命中，
                        // 表现为点击后编辑器内容毫无变化（仅顶部确认横条出现）。
                        const ed = sqlEditorRef.current
                        let sel = editorSelectionRef.current
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
                        let final = base
                        let applied = false
                        if (action === "replace_all") {
                          // 全部替换：整个编辑器内容直接替换为生成的 SQL（忽略光标/选中）
                          final = sql
                          applied = true
                        } else if (action === "append") {
                          // 追加到末尾：与已有内容之间空一行（\n\n），避免 SQL 语句紧贴已有内容
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
                          // 无法定位（光标/选中无效）：回退为追加到末尾，并明确提示，避免静默无响应
                          toast.info(t("workspace.noCursor"))
                          final = base.trim() ? `${base.trim()}\n\n${sql}` : sql
                        }
                        setAiPreviewBase(queryActive.sql)
                        setAiPreviewing(true)
                        updateTabSql(queryActive.id, final)
                      }}
                      hasSelection={hasEditorSelection}
                      onClose={() => setAiOpen(false)}
                    />
                  </Panel>
                </>
              )}
            </Group>
          ) : objectActive ? (
            /* 对象 tab：数据 + 结构 + DDL。
                key 按 (connId|db|name|objType) 组合：切表时强制 remount，
                避免 React 复用 TableBrowser 实例导致 sortSpecs/filters/hiddenColumns
                等 useState 状态残留到新表（修复：切表时旧表的排序列被带到新表请求，
                后端 sqlquery.go:407 校验失败报「排序列「X」不存在于表 Y」）。
                此处不要用 tab.id 当 key：同一 tab 切换 subTab 时不需要 remount。 */
            <TableBrowser
              key={`${connId}|${objectActive.db}|${objectActive.name}|${objectActive.objType}`}
              connId={connId}
              db={objectActive.db}
              name={objectActive.name}
              objType={objectActive.objType}
              subTab={objectActive.subTab}
              page={objectActive.page}
              viewLayout={objectActive.viewLayout}
              onSubTabChange={(s) => setObjectSubTab(objectActive.id, s)}
              onPageChange={(p) => setObjectPage(objectActive.id, p)}
              onViewLayoutChange={(l) => setObjectViewLayout(objectActive.id, l)}
              running={running}
              persistFailed={persistFailed}
              onClearPersistFailed={clearPersistFailed}
            />
          ) : (
            /* 无 tab 空状态（切换连接后） */
            <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">
              {connId ? t("workspace.noTabHint") : t("workspace.needConn")}
            </div>
          )}
        </main>
      </div>

      {/* 实际执行 SQL 详情弹窗：完整语句 + 一键复制 */}
      <Dialog open={sqlDetail !== null} onOpenChange={(v) => !v && setSqlDetail(null)}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{t("workspace.actualExecTitle")}</DialogTitle>
          </DialogHeader>
          <pre className="h-[50vh] max-h-[60vh] overflow-auto whitespace-pre rounded-md border bg-muted/40 p-3 font-mono text-[12px] leading-5 text-foreground/90">
            {sqlDetailFormatted || sqlDetail}
          </pre>
          <div className="flex justify-end">
            <Button
              size="sm"
              variant="outline"
              className="h-7 gap-1 px-2 text-xs"
              onClick={async () => {
                if (!sqlDetail) return
                await navigator.clipboard.writeText(sqlDetailFormatted || sqlDetail)
                toast.success(t("common.copied"))
              }}
            >
              <Copy className="h-3.5 w-3.5" /> {t("common.copy")}
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}