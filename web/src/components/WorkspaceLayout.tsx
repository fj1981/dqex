import { useCallback, useEffect, useRef, useState } from "react"
import { Group, Panel, Separator, useDefaultLayout } from "react-resizable-panels"
import { AlertCircle, Braces, Check, ChevronLeft, ChevronRight, Code2, Copy, FunctionSquare, List, Loader2, Plus, Sparkles, Star, Table2, View, X } from "lucide-react"
import { toast } from "sonner"
import { Badge } from "@/components/ui/badge"
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
import { setSqlEditor } from "@/lib/editorRef"
import { formatEditorSQL, formatSQL } from "@/lib/sqlFormat"
import { useClickOutside } from "@/lib/useClickOutside"
import { defaultFavoriteTitle } from "@/lib/sql"
import { prompt } from "@/components/ui/alert-dialog"
import { useQueryStore, type WorkspaceTab } from "@/stores/queryStore"
import { useObjectTreeStore } from "@/stores/objectTreeStore"
import { useFavoriteStore } from "@/stores/favoriteStore"
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
        if (alive) setAiStatus({ enabled: false, baseUrl: "", model: "", temperature: 0, maxTokens: 0, timeoutSec: 0, maxSchemaTables: 0, maxSchemaChars: 0, hasPrompt: false })
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
      ? "连接检测中…"
      : ping === "ok"
        ? `连接正常 · ${pingMs} ms（点击重新检测）`
        : ping === "fail"
          ? "连接不可用（点击重新检测）"
          : "点击检测连接"

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
      toast.error("当前连接已失效（可能被删除或重建），请重新选择连接")
      return
    }
    // connId 有效：重置失效标记（用户已手动重选，后续若再次删除可再次触发自愈）
    invalidatedRef.current = false
  }, [connId, connections, setConnId])

  // 查询 tab 编辑器/结果区上下分割布局，自动持久化到 localStorage
  const querySplit = useDefaultLayout({ id: "dbx-query-split" })
  // AI 面板横向宽度（右侧面板，可拖动调整，比例存 localStorage）
  const aiSplit = useDefaultLayout({ id: "dbx-ai-split" })

  const checkHealth = useCallback(async () => {
    if (!connId) return
    setPing("checking")
    try {
      const r = await pingConnection(connId)
      setPingMs(r.elapsedMs)
      setPing(r.ok ? "ok" : "fail")
      if (!r.ok) toast.error(`连接不可用: ${r.error || "未知错误"}`)
    } catch (e) {
      setPing("fail")
      toast.error(`连接检测失败: ${(e as Error).message}`)
    }
  }, [connId])

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
        toast.info("编辑器中没有可用的 SQL")
        return
      }
      setAiOpen(true)
      setQuickRequest({ action, text: sql })
    },
    [getTargetSql],
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
    if (t.kind === "object") {
      const meta = t.objType === "view"
        ? { icon: View, cls: "text-cyan-600" }
        : t.objType === "function" || t.objType === "procedure"
          ? { icon: FunctionSquare, cls: "text-violet-600" }
          : { icon: Table2, cls: "text-emerald-600" }
      const Icon = meta.icon
      // 表名拆分：头部可截断、尾部固定保留（同库时还省去 [库名]，空间更充裕）
      const { head, tail } = splitTableName(t.name)
      return (
        <>
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
              title="展开对象树"
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
                <SelectTrigger className="h-7 min-w-0 flex-1 px-2 text-xs">
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
                      <Badge variant="secondary" className="shrink-0 px-1.5 py-0 text-[10px] font-normal uppercase">
                        {conn.conn.Type}
                      </Badge>
                      <span className="truncate font-medium">{conn.name}</span>
                    </span>
                  ) : (
                    <span className="text-muted-foreground">选择连接…</span>
                  )}
                </SelectTrigger>
                <SelectContent>
                  {connections.length === 0 ? (
                    <div className="px-2 py-4 text-center text-sm text-muted-foreground">
                      暂无连接，请先在右侧面板「新建连接」
                    </div>
                  ) : (
                    connections.map((c) => (
                      <SelectItem key={c.id} value={c.id}>
                        <span className="flex items-center gap-2">
                          <Badge variant="secondary" className="px-1.5 py-0 text-[10px] font-normal uppercase">
                            {c.conn.Type}
                          </Badge>
                          <span className="truncate">{c.name}</span>
                          {c.shortName && (
                            <span className="text-xs font-mono text-muted-foreground">({c.shortName})</span>
                          )}
                          <span className="text-xs text-muted-foreground">
                            {c.conn.Host}:{c.conn.Port}
                          </span>
                        </span>
                      </SelectItem>
                    ))
                  )}
                </SelectContent>
              </Select>
              <span className="shrink-0 text-xs font-medium text-muted-foreground">对象</span>
              <Button
                variant="ghost"
                size="icon"
                className="h-6 w-6 shrink-0 text-muted-foreground hover:text-foreground"
                title="折叠对象树"
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
              title="向左滚动"
              onClick={() => scrollTabs(-1)}
            >
              <ChevronLeft className="h-3.5 w-3.5" />
            </button>
            <div
              ref={tabsScrollRef}
              className="flex min-w-0 items-center gap-1 overflow-x-auto pt-1 [scrollbar-width:none] [&::-webkit-scrollbar]:hidden"
            >
              {tabs.map((t) => (
                <ContextMenu key={t.id}>
                  <ContextMenuTrigger asChild>
                    <div
                      ref={t.id === activeId ? activeTabRef : null}
                      className={cn(
                        "group flex shrink-0 cursor-pointer items-center gap-1 rounded-t-md border border-b-0 px-2.5 py-1.5 text-xs transition-colors",
                        t.id === activeId
                          ? "border-border bg-background font-medium text-foreground"
                          : "border-transparent text-muted-foreground/60 hover:bg-accent hover:text-muted-foreground/80",
                      )}
                      onClick={() => setActiveTab(t.id)}
                      // 中键点击关闭（浏览器 tab 行为）
                      onAuxClick={(e) => {
                        if (e.button === 1) closeTab(t.id)
                      }}
                      // 双击重命名（仅 query tab）
                      onDoubleClick={() => {
                        if (t.kind === "query") {
                          setRenamingId(t.id)
                          setRenameValue(t.title)
                        }
                      }}
                    >
                      {renamingId === t.id ? (
                        <input
                          autoFocus
                          className="h-4 w-24 rounded-sm border border-border bg-background px-1 text-xs leading-4 outline-none focus:ring-1 focus:ring-ring"
                          value={renameValue}
                          onChange={(e) => setRenameValue(e.target.value)}
                          onClick={(e) => e.stopPropagation()}
                          onBlur={() => {
                            renameTab(t.id, renameValue)
                            setRenamingId(null)
                          }}
                          onKeyDown={(e) => {
                            if (e.key === "Enter") {
                              renameTab(t.id, renameValue)
                              setRenamingId(null)
                            } else if (e.key === "Escape") {
                              setRenamingId(null)
                            }
                          }}
                        />
                      ) : (
                        renderTabLabel(t)
                      )}
                      <button
                        type="button"
                        className="ml-0.5 rounded p-0.5 text-muted-foreground/50 hover:bg-accent hover:text-foreground"
                        onClick={(e) => {
                          e.stopPropagation()
                          closeTab(t.id)
                        }}
                      >
                        <X className="h-3 w-3" />
                      </button>
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent>
                    {t.kind === "query" && (
                      <>
                        <ContextMenuItem
                          onSelect={() => {
                            setRenamingId(t.id)
                            setRenameValue(t.title)
                          }}
                        >
                          重命名
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                      </>
                    )}
                    <ContextMenuItem onSelect={() => closeTab(t.id)}>关闭</ContextMenuItem>
                    <ContextMenuItem onSelect={() => closeOthers(t.id)}>关闭其他</ContextMenuItem>
                    <ContextMenuItem
                      disabled={tabs.findIndex((x) => x.id === t.id) === tabs.length - 1}
                      onSelect={() => closeRight(t.id)}
                    >
                      关闭右侧
                    </ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem onSelect={() => closeAll()}>关闭全部</ContextMenuItem>
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
              title="向右滚动"
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
                title="列出所有标签页"
                onClick={() => setTabListOpen((v) => !v)}
              >
                <List className="h-3.5 w-3.5" />
              </button>
              {tabListOpen && (
                <div className="scrollbar-thin absolute right-0 top-full z-50 mt-1 max-h-80 w-64 overflow-y-auto rounded-md border bg-popover p-1 text-popover-foreground shadow-md">
                  <div className="flex items-center justify-between px-2 py-1">
                    <span className="text-[11px] font-medium text-muted-foreground">标签页</span>
                    <span className="text-[10px] tabular-nums text-muted-foreground">{tabs.length}</span>
                  </div>
                  {tabs.length === 0 ? (
                    <div className="px-2 py-4 text-center text-xs text-muted-foreground">暂无标签页</div>
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

            {/* 右侧操作按钮组：脱敏 + 新建查询；连接切换已迁到左侧对象树顶部；
                pr-9 为右上角展开/收起按钮让位（面板收起时才需要） */}
            <div className={cn("ml-auto flex shrink-0 items-center gap-2 py-1 pl-3", !panelOpen && "pr-9")}>
              <label className="flex h-6 items-center gap-1.5 text-[11px] text-muted-foreground" title="敏感列（password/token/secret 等）结果统一打码">
                <Switch checked={mask} onCheckedChange={setMask} />
                脱敏
              </label>

              <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => addTab()}>
                <Plus className="mr-1 h-3.5 w-3.5" /> 新建查询
              </Button>

              {/* AI 助手开关（配置启用时显示）：纯图标，位于「新建查询」右侧，对应右侧 AI 面板 */}
              {aiStatus?.enabled && (
                <Button
                  size="icon"
                  variant={aiOpen ? "secondary" : "outline"}
                  className={cn("h-7 w-7", aiOpen && "border-violet-500/50 text-violet-600")}
                  onClick={() => setAiOpen((v) => !v)}
                  title={aiOpen ? "收起 AI 助手" : "打开 AI 助手（生成 / 解释 / 修复 / 优化 SQL）"}
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
                      placeholder="SELECT * FROM 表名;  (Cmd/Ctrl + Enter 执行，选中可仅执行选中部分)"
                      diffBase={aiPreviewing ? aiPreviewBase : undefined}
                      onApply={() => {
                        // 确认替换：保留当前编辑器内容，退出对比模式
                        setAiPreviewBase("")
                        setAiPreviewing(false)
                        toast.success("已应用 AI 生成的 SQL")
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
                      title={hasEditorSelection ? "格式化选中的 SQL" : "格式化 SQL（Shift+Alt+F）"}
                      onClick={() => {
                        const ed = sqlEditorRef.current
                        if (ed) formatEditorSQL(ed)
                      }}
                    >
                      <Braces className="h-3.5 w-3.5" /> 格式化
                    </Button>
                    {aiStatus?.enabled && (
                      <>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 text-xs text-muted-foreground hover:text-foreground"
                          title={hasEditorSelection ? "解释选中的 SQL" : "解释编辑器中的 SQL"}
                          onClick={() => triggerQuickAction("explain")}
                        >
                          <Sparkles className="h-3.5 w-3.5 text-violet-500" /> 解释
                        </Button>
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 text-xs text-muted-foreground hover:text-foreground"
                          title={hasEditorSelection ? "优化选中的 SQL" : "优化编辑器中的 SQL"}
                          onClick={() => triggerQuickAction("optimize")}
                        >
                          <Sparkles className="h-3.5 w-3.5 text-violet-500" /> 优化
                        </Button>
                        {/* 收藏当前 SQL：存入独立收藏表（按连接隔离），与右侧「收藏」Tab 联动 */}
                        <Button
                          size="sm"
                          variant="ghost"
                          className="h-7 gap-1 text-xs text-muted-foreground hover:text-amber-600"
                          title="收藏当前编辑器 SQL"
                          disabled={!queryActive || !queryActive.sql.trim()}
                          onClick={async () => {
                            if (!queryActive) return
                            const sql = queryActive.sql.trim()
                            if (!sql) {
                              toast.info("编辑器中没有可用的 SQL")
                              return
                            }
                            // 弹窗预填默认标题，用户可修改后回车快速保存
                            const title = await prompt({
                              title: "收藏 SQL",
                              description: "为这条 SQL 起个名字，方便日后查找回填",
                              defaultValue: defaultFavoriteTitle(sql),
                              placeholder: "如：每日活跃用户统计",
                              confirmText: "收藏",
                              required: "标题不能为空",
                            })
                            if (title == null) return
                            try {
                              await addFavorite(connId, sql, queryActive.db, queryActive.mode, title)
                              toast.success("已收藏")
                            } catch (e) {
                              toast.error(`收藏失败: ${(e as Error).message}`)
                            }
                          }}
                        >
                          <Star className="h-3.5 w-3.5" /> 收藏
                        </Button>
                      </>
                    )}
                    {queryActive.results.length > 0 && (() => {
                      const r = queryActive.results[queryActive.activeResult] ?? queryActive.results[0]
                      if (r.error) return null
                      // 行数与耗时统一在结果网格底部展示，这里只保留写操作影响行数与多结果集提示
                      const text = r.isWrite
                        ? `影响 ${r.affectedRows} 行`
                        : queryActive.results.length > 1
                          ? `共 ${queryActive.results.length} 个结果集`
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
                      <SelectTrigger className="h-7 w-auto min-w-[130px] max-w-[240px] px-2 text-xs" title="目标库">
                        {queryActive.db || <span className="text-muted-foreground">默认库</span>}
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="">默认库</SelectItem>
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
                        title="规范执行 = 系统规范化 SQL 语法，并自动限制最多返回 1000 行；原样执行 = 按你写的原样发库，不限制行数"
                      >
                        {SQL_EXEC_MODE_LABEL[queryActive.mode] ?? "规范执行"}
                      </SelectTrigger>
                      <SelectContent>
                        {(Object.keys(SQL_EXEC_MODE_LABEL) as SQLExecMode[]).map((m) => (
                          <SelectItem key={m} value={m}>
                            {SQL_EXEC_MODE_LABEL[m]}
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
                      执行 (⌘⏎)
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
                    <div className="flex items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
                      <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
                      <div className="min-w-0 flex-1">
                        <div className="font-medium text-destructive">执行失败</div>
                        <div className="mt-0.5 break-words text-foreground/80">{queryActive.error}</div>
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
                              text: `以下 SQL 执行报错：\n\n${failSql}\n\n报错信息：\n${queryActive.error}\n\n请修复`,
                            })
                          }}
                        >
                          <Sparkles className="h-3.5 w-3.5 text-violet-500" />
                          AI 修复
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
                              结果 {i + 1}
                              {r.isWrite ? " (写)" : r.error ? " (错)" : ""}
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
                              <span className="shrink-0 text-[11px] text-muted-foreground">实际执行</span>
                              <button
                                type="button"
                                className="min-w-0 flex-1 cursor-pointer truncate text-left font-mono text-[12px] text-foreground/80 hover:text-foreground hover:underline hover:decoration-dotted"
                                title="点击查看完整 SQL"
                                onClick={() => setSqlDetail(r.sql)}
                              >
                                {r.sql}
                              </button>
                            </div>
                            {r.isWrite || !r.rows ? (
                              // 写语句无结果集：不渲染 ResultGrid，展示成功状态卡片（影响行数 + 耗时）
                              <div className="flex min-h-0 flex-1 items-center justify-center">
                                <div className="flex items-center gap-5 rounded-lg border bg-muted/30 px-6 py-4">
                                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-emerald-500/15">
                                    <Check className="h-5 w-5 text-emerald-500" />
                                  </div>
                                  <div className="flex flex-col gap-0.5">
                                    <span className="text-sm font-medium">执行成功</span>
                                    <span className="text-xs text-muted-foreground">写语句 · 无结果集</span>
                                  </div>
                                  <div className="h-8 w-px shrink-0 bg-border" />
                                  <div className="flex items-center gap-6">
                                    <div className="flex flex-col gap-0.5">
                                      <span className="text-[11px] text-muted-foreground">影响行数</span>
                                      <span className="text-sm font-medium tabular-nums">{r.affectedRows}</span>
                                    </div>
                                    <div className="flex flex-col gap-0.5">
                                      <span className="text-[11px] text-muted-foreground">耗时</span>
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
                          <span>SQL 已保存，查询结果不随刷新保留</span>
                          <span className="text-xs text-muted-foreground/80">
                            点击「执行」重新查询即可查看数据
                          </span>
                        </>
                      ) : (
                        <span>编写 SQL 后点击「执行」，或从左侧对象树点击对象查看数据 / 结构 / DDL</span>
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
                          toast.info("未获取到编辑器光标位置，已改为追加到末尾")
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
              {connId ? "点击「新建查询」或从左侧对象树选择对象" : "请先在上方选择数据库连接"}
            </div>
          )}
        </main>
      </div>

      {/* 实际执行 SQL 详情弹窗：完整语句 + 一键复制 */}
      <Dialog open={sqlDetail !== null} onOpenChange={(v) => !v && setSqlDetail(null)}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>实际执行 SQL</DialogTitle>
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
                toast.success("已复制")
              }}
            >
              <Copy className="h-3.5 w-3.5" /> 复制
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}