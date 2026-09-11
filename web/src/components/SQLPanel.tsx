import { useEffect, useMemo, useState } from "react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { useNavigate } from "react-router-dom"
import { FolderOpen, ScrollText, ShieldCheck, Star, Trash2, Zap } from "lucide-react"

import { confirm, prompt } from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useAppStore } from "@/stores/app"
import { useSqlHistoryStore } from "@/stores/sqlHistoryStore"
import { useFavoriteStore } from "@/stores/favoriteStore"
import { useQueryStore } from "@/stores/queryStore"
import { TASK_TYPE_LABEL, type SQLAuditEntry, type SQLExecMode, type SQLFavorite, type SQLHistoryItem } from "@/types"
import { cn, formatTime, shortPaths } from "@/lib/utils"
import { defaultFavoriteTitle } from "@/lib/sql"
import { tKey } from "@/lib/i18n"

const MODE_META: Record<SQLExecMode, { labelKey: string; icon: typeof ShieldCheck; cls: string }> = {
  transform: { labelKey: "app.execMode.transform", icon: ShieldCheck, cls: "text-blue-600" },
  raw: { labelKey: "app.execMode.raw", icon: Zap, cls: "text-amber-600" },
}

function ModeBadge({ mode }: { mode: SQLExecMode }) {
  const { t } = useTranslation()
  const meta = MODE_META[mode]
  const Icon = meta.icon
  return (
    <span className="inline-flex items-center gap-0.5 text-[10px] text-muted-foreground" title={tKey(meta.labelKey)}>
      <Icon className={cn("h-3.5 w-3.5 shrink-0", meta.cls)} strokeWidth={1.5} />
    </span>
  )
}

// SQL 记录面板：查询页右侧，含「执行历史 / 收藏 / 审计」三个 Tab。
// 执行历史：用户手写 SQL，可回填重跑；收藏：用户主动、跨会话、按用户域隔离；审计：全量只读。
// 历史/收藏回填均采用与 AI 面板一致的「四动作」菜单（全部替换/插入光标处/追加末尾/替换所选）。
export function SQLHistoryPanel({
  connId,
  currentDb,
  items,
  auditItems,
  favorites,
  onClear,
  onRefill,
  onAddFavorite,
  onDeleteFavorite,
  onRenameFavorite,
}: {
  connId: string
  currentDb?: string
  items: SQLHistoryItem[]
  auditItems: SQLAuditEntry[]
  favorites: SQLFavorite[]
  onClear: () => Promise<void>
  onRefill: (
    sql: string,
    db: string | undefined,
    mode: SQLExecMode | undefined,
    action: "replace_all" | "replace_selection" | "insert_cursor" | "append",
  ) => void
  onAddFavorite: (sql: string, db?: string, mode?: SQLExecMode) => Promise<void>
  onDeleteFavorite: (id: string) => Promise<void>
  onRenameFavorite: (id: string, title: string) => Promise<void>
}) {
  const [tab, setTab] = useState<"history" | "favorite" | "audit">("history")
  const { t } = useTranslation()

  const clear = async () => {
    if (!(await confirm({ title: t("app.clearHistoryTitle"), description: t("app.clearHistoryDesc"), confirmText: t("common.delete"), danger: true }))) return
    try {
      await onClear()
      toast.success(t("app.historyCleared"))
    } catch (e) {
      toast.error(t("app.clearFailedMsg", { msg: (e as Error).message }))
    }
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="shrink-0 px-3 pb-1 pt-3">
        <Tabs value={tab} onValueChange={(v) => setTab(v as "history" | "favorite" | "audit")}>
          <TabsList className="w-full">
            <TabsTrigger value="history" className="w-1/3 shrink-0 truncate text-xs">
              {t("app.tabHistory")}{items.length > 0 && <span className="ml-1 tabular-nums">({items.length})</span>}
            </TabsTrigger>
            <TabsTrigger value="favorite" className="w-1/3 shrink-0 truncate text-xs">
              {t("app.tabFavorite")}{favorites.length > 0 && <span className="ml-1 tabular-nums">({favorites.length})</span>}
            </TabsTrigger>
            <TabsTrigger value="audit" className="w-1/3 shrink-0 truncate text-xs">
              {t("app.tabAudit")}{auditItems.length > 0 && <span className="ml-1 tabular-nums">({auditItems.length})</span>}
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
                title={t("app.clearHistoryTitle")}
                onClick={clear}
              >
                <Trash2 className="h-3 w-3" />
              </Button>
            )}
          </div>
          <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-3 pb-3">
            {!connId ? (
              <div className="py-4 text-center text-xs text-muted-foreground">{t("app.needConn")}</div>
            ) : items.length === 0 ? (
              <div className="py-4 text-center text-xs text-muted-foreground">{t("app.noSQLHistory")}</div>
            ) : (
              items.map((h) => (
                <HistoryOrFavoriteCard
                  key={h.id}
                  sql={h.sql}
                  statusDot={h.status === "error" ? "bg-destructive" : "bg-green-600"}
                  badges={
                    <>
                      {h.isWrite && (
                        <span className="rounded bg-destructive/10 px-1 py-px text-[10px] font-medium text-destructive">{t("app.badgeWrite")}</span>
                      )}
                      {h.db && <span className="rounded bg-muted px-1 py-px text-[10px] text-muted-foreground">{h.db}</span>}
                      {h.mode && <ModeBadge mode={h.mode} />}
                    </>
                  }
                  timeText={`${h.status === "ok" ? (h.isWrite ? t("common.affectedRows", { n: h.rowCount }) : t("common.rowsWithMs", { n: h.rowCount, ms: h.elapsedMs })) : h.error || t("common.execFailed")}`}
                  onRefill={(action) => onRefill(h.sql, h.db, h.mode, action)}
                  onFavorite={() => onAddFavorite(h.sql, h.db, h.mode)}
                />
              ))
            )}
          </div>
        </>
      ) : tab === "favorite" ? (
        <FavoriteList
          connId={connId}
          currentDb={currentDb}
          favorites={favorites}
          onRefill={(f, action) => onRefill(f.sql, f.db, f.mode, action)}
          onDelete={onDeleteFavorite}
          onRename={onRenameFavorite}
        />
      ) : (
        <AuditList connId={connId} items={auditItems} />
      )}
    </div>
  )
}

// 回填动作菜单：与 AI 面板完全一致，降低学习成本。仅「全部替换」还原 db/mode 上下文。
const REFILL_ACTIONS: {
  value: "replace_all" | "replace_selection" | "insert_cursor" | "append"
  labelKey: string
}[] = [
  { value: "replace_all", labelKey: "app.refill.replaceAll" },
  { value: "insert_cursor", labelKey: "app.refill.insertCursor" },
  { value: "append", labelKey: "app.refill.append" },
  { value: "replace_selection", labelKey: "app.refill.replaceSelection" },
]

// 历史/收藏通用卡片：点击展开「回填方式」菜单；hover 出收藏按钮。
function HistoryOrFavoriteCard({
  title,
  sql,
  statusDot,
  badges,
  timeText,
  onRefill,
  onFavorite,
  onDelete,
}: {
  title?: string
  sql: string
  statusDot: string
  badges: React.ReactNode
  timeText: string
  onRefill: (action: "replace_all" | "replace_selection" | "insert_cursor" | "append") => void
  onFavorite?: () => void
  onDelete?: () => void
}) {
  const [menuOpen, setMenuOpen] = useState(false)
  const { t } = useTranslation()

  return (
    <div
      role="button"
      tabIndex={0}
      title={t("app.chooseRefill")}
      className="group mb-1.5 min-w-0 cursor-pointer overflow-hidden rounded-md border bg-muted/20 px-2.5 py-1.5 text-xs transition-colors hover:border-primary/40 hover:shadow-sm"
      onClick={() => setMenuOpen((v) => !v)}
      onKeyDown={(e) => e.key === "Enter" && setMenuOpen((v) => !v)}
    >
      <div className="flex items-center justify-between">
        <span className="flex items-center gap-1.5">
          <span className={cn("h-1.5 w-1.5 rounded-full", statusDot)} />
          {badges}
        </span>
        <span className="flex shrink-0 items-center gap-1">
          {(onFavorite || onDelete) && (
            <span className="flex items-center opacity-0 transition-opacity group-hover:opacity-100">
              {onFavorite && (
                <button
                  type="button"
                  className="flex h-5 w-5 items-center justify-center rounded text-amber-500 hover:bg-amber-100 hover:text-amber-600 dark:hover:bg-amber-500/15"
                  title={t("app.favorite")}
                  onClick={(e) => {
                    e.stopPropagation()
                    onFavorite()
                  }}
                >
                  <Star className="h-3.5 w-3.5" />
                </button>
              )}
              {onDelete && (
                <button
                  type="button"
                  className="flex h-5 w-5 items-center justify-center rounded text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                  title={t("common.delete")}
                  onClick={(e) => {
                    e.stopPropagation()
                    onDelete()
                  }}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </button>
              )}
            </span>
          )}
          {title && <span className="max-w-[6rem] truncate whitespace-nowrap tabular-nums text-muted-foreground">{title}</span>}
        </span>
      </div>
      <div className="mt-0.5 line-clamp-2 break-all font-mono text-[11px] leading-4 text-muted-foreground">{sql}</div>
      {menuOpen && (
        <div className="mt-1.5 flex flex-nowrap gap-1 overflow-x-auto border-t border-border/60 pt-1.5">
          {REFILL_ACTIONS.map((a) => (
            <button
              key={a.value}
              type="button"
              className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground hover:bg-primary/10 hover:text-primary"
              onClick={(e) => {
                e.stopPropagation()
                onRefill(a.value)
                setMenuOpen(false)
              }}
            >
              {tKey(a.labelKey)}
            </button>
          ))}
        </div>
      )}
    </div>
  )
}

// 收藏列表：跨连接可见 Tab（按用户域隔离）。跨连接时显示来源标记，回填若连接/库不一致给提示。
function FavoriteList({
  connId,
  currentDb,
  favorites,
  onRefill,
  onDelete,
  onRename,
}: {
  connId?: string
  currentDb?: string
  favorites: SQLFavorite[]
  onRefill: (f: SQLFavorite, action: "replace_all" | "replace_selection" | "insert_cursor" | "append") => void
  onDelete: (id: string) => Promise<void>
  onRename: (id: string, title: string) => Promise<void>
}) {
  const [editingId, setEditingId] = useState<string | null>(null)
  const [draft, setDraft] = useState("")
  const { t } = useTranslation()
  // 连接 id → 友好名称映射：让收藏来源标记显示连接名而非内部 id
  const connections = useAppStore((s) => s.connections)
  const connName = useMemo(() => {
    const m = new Map<string, string>()
    for (const c of connections) m.set(c.id, c.name)
    return m
  }, [connections])
  const connLabel = (id?: string) => (id ? connName.get(id) || id : "")

  const startRename = (f: SQLFavorite) => {
    setEditingId(f.id)
    setDraft(f.title)
  }
  const commitRename = async () => {
    const id = editingId
    const title = draft.trim()
    setEditingId(null)
    if (id && title) {
      try {
        await onRename(id, title)
      } catch (e) {
        toast.error(t("app.renameFailed", { msg: (e as Error).message }))
      }
    }
  }

  // 跨连接/跨库的不一致提示：收藏来源与当前不同，回填时（尤其全部替换会切库）需告知
  const mismatchHint = (f: SQLFavorite): string | null => {
    const connDiff = connId && f.connId && f.connId !== connId
    const dbDiff = f.db && currentDb && f.db !== currentDb
    const cName = connLabel(f.connId)
    if (connDiff && dbDiff) return t("app.favMismatchConnDb", { conn: cName, db: f.db })
    if (connDiff) return t("app.favMismatchConn", { conn: cName })
    if (dbDiff) return t("app.favMismatchDb", { db: f.db, current: currentDb })
    return null
  }

  return (
    <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-3 pb-3">
      {favorites.length === 0 ? (
        <div className="py-4 text-center text-xs text-muted-foreground">{t("app.noFavorites")}</div>
      ) : (
        favorites.map((f) => {
          const hint = mismatchHint(f)
          return (
            <div key={f.id} className="group mb-1.5 min-w-0 overflow-hidden rounded-md border bg-muted/20 px-2.5 py-1.5 text-xs">
              <div className="flex items-center justify-between gap-1">
                {editingId === f.id ? (
                  <input
                    autoFocus
                    value={draft}
                    onChange={(e) => setDraft(e.target.value)}
                    onBlur={commitRename}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") commitRename()
                      if (e.key === "Escape") setEditingId(null)
                    }}
                    className="min-w-0 flex-1 rounded border border-primary/40 px-1 py-0.5 text-[11px] outline-none"
                  />
                ) : (
                  <span
                    className="min-w-0 flex-1 cursor-text truncate font-medium text-foreground/90"
                    title={t("app.dblclickRename")}
                    onDoubleClick={() => startRename(f)}
                  >
                    {f.title}
                  </span>
                )}
                <span className="flex shrink-0 items-center gap-1 opacity-0 transition-opacity group-hover:opacity-100">
                  <button
                    type="button"
                    className="flex h-4 w-4 items-center justify-center rounded text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                    title={t("app.deleteIrreversible")}
                    onClick={async () => {
                      if (!(await confirm({ title: t("app.deleteFavoriteTitle"), description: t("app.deleteFavoriteDesc", { title: f.title }), confirmText: t("common.delete"), danger: true }))) return
                      try {
                        await onDelete(f.id)
                      } catch (e) {
                        toast.error(t("app.deleteFailed", { msg: (e as Error).message }))
                      }
                    }}
                  >
                    <Trash2 className="h-3 w-3" />
                  </button>
                </span>
              </div>
              <div
                role="button"
                tabIndex={0}
                title={t("app.chooseRefill")}
                className="mt-0.5 cursor-pointer"
                onClick={(e) => {
                  const menu = (e.currentTarget.querySelector("[data-refill-menu]") as HTMLElement) || null
                  if (menu) menu.classList.toggle("hidden")
                }}
              >
                <div className="line-clamp-2 break-all font-mono text-[11px] leading-4 text-foreground/80">{f.sql}</div>
                <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-[10px] text-muted-foreground">
                  {/* 来源标记：来自哪个连接/库（跨连接可见） */}
                  {f.connId && <span className="rounded bg-muted px-1 py-px">{t("app.fromConn", { name: connLabel(f.connId) })}</span>}
                  {f.db && <span className="rounded bg-muted px-1 py-px">{t("app.fromDb", { db: f.db })}</span>}
                  {f.mode && <ModeBadge mode={f.mode} />}
                </div>
                {hint && <div className="mt-0.5 text-[10px] text-amber-600">⚠ {hint}</div>}
                <div data-refill-menu className="mt-1.5 hidden flex flex-nowrap gap-1 overflow-x-auto border-t border-border/60 pt-1.5">
                  {REFILL_ACTIONS.map((a) => (
                    <button
                      key={a.value}
                      type="button"
                      className="shrink-0 rounded bg-muted px-1.5 py-0.5 text-[10px] text-muted-foreground hover:bg-primary/10 hover:text-primary"
                      onClick={(ev) => {
                        ev.stopPropagation()
                        if (hint && a.value === "replace_all") {
                          toast.warning(`${hint}；${t("app.replaceAllWarn")}`)
                        } else if (hint) {
                          toast.warning(hint)
                        }
                        onRefill(f, a.value)
                      }}
                    >
                      {tKey(a.labelKey)}
                    </button>
                  ))}
                </div>
              </div>
            </div>
          )
        })
      )}
    </div>
  )
}

// 审计面板：只读展示，含来源标记（手写/对象树/单元格编辑）
function AuditList({ connId, items }: { connId: string; items: SQLAuditEntry[] }) {
  const { t } = useTranslation()
  const SOURCE_LABEL: Record<string, { labelKey: string; cls: string }> = {
    manual: { labelKey: "app.sourceManual", cls: "bg-primary/10 text-primary" },
    tree: { labelKey: "app.sourceTree", cls: "bg-muted text-muted-foreground" },
    cell: { labelKey: "common.edit", cls: "bg-amber-500/10 text-amber-600 dark:text-amber-400" },
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
        {t("app.auditReadonly")}
      </div>
      <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-3 pb-3">
        {!connId ? (
          <div className="py-4 text-center text-xs text-muted-foreground">{t("app.needConn")}</div>
        ) : items.length === 0 ? (
          <div className="py-4 text-center text-xs text-muted-foreground">{t("app.noAudit")}</div>
        ) : (
          items.map((a) => {
            const src = SOURCE_LABEL[a.source] || SOURCE_LABEL.manual
            return (
              <div
                key={a.id}
                className="mb-1.5 min-w-0 overflow-hidden rounded-md border bg-muted/20 px-2.5 py-1.5 text-xs"
              >
                <div className="flex items-center justify-between">
                  <span className="flex items-center gap-1.5">
                    <span
                      className={cn(
                        "h-1.5 w-1.5 rounded-full",
                        a.status === "error" ? "bg-destructive" : "bg-green-600",
                      )}
                    />
                    <span className={cn("rounded px-1 py-px text-[10px] font-medium", src.cls)}>{tKey(src.labelKey)}</span>
                    {a.isWrite && (
                      <span className="rounded bg-destructive/10 px-1 py-px text-[10px] font-medium text-destructive">{t("app.badgeWrite")}</span>
                    )}
                    {a.db && <span className="rounded bg-muted px-1 py-px text-[10px] text-muted-foreground">{a.db}</span>}
                  </span>
                  <span className="shrink-0 tabular-nums text-muted-foreground">
                    {formatTime(new Date(a.createdAt).toISOString()).slice(5, 16)}
                  </span>
                </div>

                {/* 单元格编辑：结构化展示真实参数 */}
                {a.source === "cell" && a.table ? (
                  <div className="mt-1 space-y-0.5 font-mono text-[11px] leading-4 text-muted-foreground">
                    <div>
                      <span className="text-muted-foreground">{t("app.fieldTable")}</span>
                      {a.table}
                      <span className="text-muted-foreground">{t("app.fieldColumn")}</span>
                      {a.column}
                    </div>
                    <div className="break-all">
                      <span className="text-muted-foreground">{t("app.fieldValue")}</span>
                      <span className="text-foreground">{renderValue(a.newValue)}</span>
                    </div>
                    {a.pkColumns && a.pkColumns.length > 0 && (
                      <div className="break-all">
                        <span className="text-muted-foreground">{t("app.fieldWhere")}</span>
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
                  <div className="mt-0.5 line-clamp-2 break-all font-mono text-[11px] leading-4 text-muted-foreground">
                    {a.sql}
                  </div>
                )}

                <div className="mt-0.5 flex items-center justify-between text-[11px] text-muted-foreground">
                  <span className="tabular-nums">
                    {a.status === "ok"
                      ? a.isWrite
                        ? t("common.affectedRows", { n: a.rowCount })
                        : t("common.rowsWithMs", { n: a.rowCount, ms: a.elapsedMs })
                      : a.error || t("common.execFailed")}
                  </span>
                  {a.mode && <ModeBadge mode={a.mode} />}
                </div>
              </div>
            )
          })
        )}
      </div>
    </div>
  )
}

// QuerySidePanel：查询页侧面板的自包含封装（主壳 Layout 与嵌入壳 EmbedShell 共用）。
// 内部完成 store 接线：历史/审计随连接加载，收藏全局加载；回填走 queryStore 四动作。
export function QuerySidePanel() {
  const { t } = useTranslation()
  const queryConnId = useQueryStore((s) => s.connId)
  const queryActiveDb = useQueryStore((s) => {
    const t = s.tabs.find((x) => x.id === s.activeId && x.kind === "query")
    return t && t.kind === "query" ? t.db : undefined
  })
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

  // 历史/审计随连接加载；收藏全局加载（不随连接变化重拉）
  useEffect(() => {
    if (queryConnId) {
      loadSQLHistory(queryConnId)
      loadAudit(queryConnId)
      loadFavorites()
    }
  }, [queryConnId]) // eslint-disable-line react-hooks/exhaustive-deps

  return (
    <SQLHistoryPanel
      connId={queryConnId}
      currentDb={queryActiveDb}
      items={sqlItems}
      auditItems={auditItems}
      favorites={favorites}
      onClear={async () => {
        if (queryConnId) await clearSQLHistory()
      }}
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
  )
}

// 任务操作历史面板：任务类页面（导出/导入/迁移/对比/快照/字典）右侧的执行记录列表。
// 主壳 RightPanel 与嵌入壳共用逻辑的独立封装：
//   - keepPath=false（主壳）：点击记录跨页跳转到对应功能页（PATH_BY_TYPE）；
//   - keepPath=true（嵌入）：嵌入路由固定（#/embed/<view>），仅回写 ?running=<id> 参数，
//     由当前 View 自行响应并恢复任务进行态。
export function TaskHistoryPanel({ taskType, keepPath }: { taskType?: string; keepPath?: boolean }) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const { history, loadHistory } = useAppStore()

  // 历史状态样式：底色胶囊 + 状态圆点（与主壳一致）
  const STATUS_META: Record<string, { labelKey: string; cls: string; dot: string }> = {
    done: { labelKey: "app.status.done", cls: "bg-green-500/10 text-green-700 dark:text-green-400", dot: "bg-green-600" },
    error: { labelKey: "app.status.error", cls: "bg-red-500/10 text-destructive", dot: "bg-destructive" },
    running: { labelKey: "app.status.running", cls: "bg-blue-500/10 text-blue-600 dark:text-blue-400", dot: "animate-pulse bg-blue-600" },
    cancelled: { labelKey: "app.status.cancelled", cls: "bg-muted text-muted-foreground", dot: "bg-muted-foreground" },
  }

  const records = taskType ? history.filter((h) => h.taskType === taskType) : history

  useEffect(() => {
    loadHistory()
  }, [taskType]) // eslint-disable-line react-hooks/exhaustive-deps

  const openRecord = (id: string, type: string) => {
    navigate(keepPath ? `?running=${id}` : `${PATH_BY_TYPE[type] || "/"}?running=${id}`)
  }

  const openDir = async (taskID: string) => {
    try {
      const { openExportDir } = await import("@/api")
      await openExportDir(taskID)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const delRecord = async (taskID: string) => {
    if (!(await confirm({ title: tKey("app.deleteRecordTitle"), description: tKey("app.deleteRecordDesc"), confirmText: tKey("common.delete"), danger: true }))) return
    try {
      const { deleteHistory } = await import("@/api")
      await deleteHistory(taskID)
      loadHistory()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  return (
    <>
      <div className="px-3 py-2 text-xs font-medium text-muted-foreground">
        {taskType ? t("app.historyOfType", { type: tKey(TASK_TYPE_LABEL[taskType] || taskType) }) : t("app.history")}
        {records.length > 0 && <span className="ml-1 tabular-nums">({records.length})</span>}
      </div>
      <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto px-3 pb-3">
        {records.length === 0 && (
          <div className="py-4 text-center text-xs text-muted-foreground">
            {taskType ? t("app.noHistoryOfType") : t("app.noHistory")}
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
              onClick={() => openRecord(h.id, h.taskType)}
              onKeyDown={(e) => e.key === "Enter" && openRecord(h.id, h.taskType)}
            >
              <div className="flex items-center justify-between">
                {!taskType && (
                  <span className="text-sm font-medium">{tKey(TASK_TYPE_LABEL[h.taskType] || h.taskType)}</span>
                )}
                <span className={cn("flex items-center gap-1 rounded-full px-1.5 py-0.5 font-medium", s.cls)}>
                  <span className={cn("h-1.5 w-1.5 rounded-full", s.dot)} />
                  {tKey(s.labelKey)}
                </span>
              </div>
              {h.target && (
                <div className="mt-0.5 min-w-0 truncate text-foreground/75" title={h.target}>
                  {h.target}
                </div>
              )}
              <div className="mt-0.5 flex items-center justify-between text-muted-foreground">
                <span className="shrink-0 tabular-nums">{formatTime(new Date(h.startedAt).toISOString()).slice(5, 16)}</span>
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
  )
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
