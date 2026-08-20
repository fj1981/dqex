import { useCallback, useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { FileDown, FileUp, ArrowLeftRight, Scale, BookOpenText, ClipboardList, Eye, Pencil, Play, Search, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import * as api from "@/api"
import PageHeader from "@/components/PageHeader"
import { useAppStore } from "@/stores/app"
import { TASK_TYPE_LABEL, type ConnInfo, type ExecutionRecord, type TaskConfig } from "@/types"
import { formatTime } from "@/lib/utils"
import { tKey } from "@/lib/i18n"
import i18n from "@/lib/i18n"

const TYPE_ICON: Record<string, { icon: React.ReactNode; cls: string }> = {
  export: { icon: <FileDown className="h-5 w-5" />, cls: "bg-blue-500/10 text-blue-600 dark:text-blue-400" },
  import: { icon: <FileUp className="h-5 w-5" />, cls: "bg-green-500/10 text-green-600 dark:text-green-400" },
  migrate: { icon: <ArrowLeftRight className="h-5 w-5" />, cls: "bg-purple-500/10 text-purple-600 dark:text-purple-400" },
  compare: { icon: <Scale className="h-5 w-5" />, cls: "bg-orange-500/10 text-orange-600 dark:text-orange-400" },
  dictionary: { icon: <BookOpenText className="h-5 w-5" />, cls: "bg-cyan-500/10 text-cyan-600 dark:text-cyan-400" },
}

// 连接主键（或旧配置中的连接名）转显示名，未命中时回退原值
function connLabel(key: string | undefined, conns: ConnInfo[]): string {
  if (!key) return "-"
  return conns.find((c) => c.id === key || c.name === key)?.name || key
}

// 表清单摘要：前 3 张 + 总数，避免长名单撑高卡片；完整清单由悬停 title 展示
function tablesSummary(tables?: string[]): string {
  if (!tables?.length) return ""
  const head = tables.slice(0, 3).join("，")
  return tables.length > 3 ? i18n.t("task.etcTables", { head, n: tables.length }) : head
}

function taskSummary(task: TaskConfig, conns: ConnInfo[]): string[] {
  const lines: string[] = []
  if (task.type === "export" && task.exportOpts) {
    const o = task.exportOpts
    lines.push(i18n.t("task.sumSource", { conn: connLabel(o.sourceConn, conns) }))
    const tbl = tablesSummary(o.tables)
    if (tbl) lines.push(i18n.t("task.sumTables", { tbl }))
    const mode = o.schemaOnly ? i18n.t("task.modeSchema") : o.dataOnly ? i18n.t("task.modeData") : i18n.t("task.modeBoth")
    lines.push(i18n.t("task.sumMode", { mode, zip: o.compress ? i18n.t("task.zipSuffix") : "" }))
  } else if (task.type === "import" && task.importOpts) {
    const o = task.importOpts
    lines.push(i18n.t("task.sumTarget", { conn: connLabel(o.targetConn, conns) }))
    // 仅展示文件名，隐藏服务器路径信息；未选文件时不占位
    const file = o.inputPath ? o.inputPath.split(/[\\/]/).pop() : ""
    if (file) lines.push(i18n.t("task.sumFile", { file }))
    lines.push(i18n.t("task.sumReset", { mode: o.resetMode || i18n.t("task.noReset") }))
  } else if (task.type === "migrate" && task.migrateOpts) {
    const o = task.migrateOpts
    lines.push(`${connLabel(o.sourceConn, conns)} → ${connLabel(o.targetConn, conns)}`)
    const tbl = tablesSummary(o.tables)
    if (tbl) lines.push(i18n.t("task.sumTables", { tbl }))
    const mode = o.schemaOnly ? i18n.t("task.modeSchema") : o.dataOnly ? i18n.t("task.modeData") : i18n.t("task.modeBoth")
    lines.push(i18n.t("task.sumModeReset", { mode, reset: o.resetMode || i18n.t("task.noReset") }))
  } else if (task.type === "compare" && task.compareOpts) {
    const o = task.compareOpts
    lines.push(`${connLabel(o.sourceConn, conns)} ↔ ${connLabel(o.targetConn, conns)}`)
    const tbl = tablesSummary(o.tables)
    if (tbl) lines.push(i18n.t("task.sumTables", { tbl }))
    if (o.aliases?.length) lines.push(i18n.t("task.sumAliases", { n: o.aliases.length }))
    const extras: string[] = []
    extras.push(o.structureOnly ? i18n.t("task.modeSchema") : o.dataOnly ? i18n.t("task.modeData") : i18n.t("task.modeBoth"))
    if (!o.structureOnly) extras.push(i18n.t("task.sumThreshold", { n: o.threshold }))
    if (o.ignoreColumns?.length) extras.push(i18n.t("task.sumIgnoreCols", { n: o.ignoreColumns.length }))
    if (o.forceData) extras.push(i18n.t("compare.forceData"))
    lines.push(extras.join("，"))
  } else if (task.type === "dictionary" && task.dictionaryOpts) {
    const o = task.dictionaryOpts
    lines.push(i18n.t("task.sumSource", { conn: connLabel(o.sourceConn, conns) }))
    const tbl = tablesSummary(o.tables)
    if (tbl) lines.push(i18n.t("task.sumTables", { tbl }))
    lines.push(i18n.t("task.sumMode", { mode: i18n.t("task.dictMode"), zip: o.compress ? i18n.t("task.zipSuffix") : "" }))
  }
  return lines
}

// 任务页：已保存的任务配置列表（默认首页）
export default function TaskView() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const loadHistory = useAppStore((s) => s.loadHistory)
  const connections = useAppStore((s) => s.connections)
  const [typeFilter, setTypeFilter] = useState("all")
  const [keyword, setKeyword] = useState("")
  const [tasks, setTasks] = useState<TaskConfig[]>([])
  const [detail, setDetail] = useState<TaskConfig | null>(null)
  const [detailHistory, setDetailHistory] = useState<ExecutionRecord[]>([])

  const load = useCallback(async () => {
    try {
      const list = await api.listTasks(typeFilter === "all" ? undefined : typeFilter)
      setTasks(list || [])
    } catch (e) {
      toast.error((e as Error).message)
    }
  }, [typeFilter])

  useEffect(() => {
    load()
  }, [load])

  const filtered = useMemo(
    () => tasks.filter((t) => !keyword || t.name.toLowerCase().includes(keyword.toLowerCase())),
    [tasks, keyword],
  )

  const openDetail = async (task: TaskConfig) => {
    setDetail(task)
    try {
      const h = await api.listHistory({ taskConfigId: task.id })
      setDetailHistory(h || [])
    } catch {
      setDetailHistory([])
    }
  }

  const doRun = async (task: TaskConfig) => {
    try {
      const { taskID } = await api.runTask(task.id)
      toast.success(t("task.started", { id: taskID }))
      loadHistory()
      navigate(`/${task.type}?running=${taskID}&task=${task.id}`)
    } catch (e) {
      toast.error(t("task.execFailed", { err: (e as Error).message }))
    }
  }

  const doDelete = async (task: TaskConfig) => {
    try {
      await api.deleteTask(task.id)
      toast.success(t("task.deleted"))
      load()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  return (
    <div className="mx-auto max-w-5xl space-y-4">
      <PageHeader
        title={t("task.title")}
        description={t("task.desc")}
        actions={
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder={t("task.searchPlaceholder")}
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              className="w-48 pl-8"
            />
          </div>
        }
      />

      <Tabs value={typeFilter} onValueChange={setTypeFilter}>
        <TabsList>
          <TabsTrigger value="all">{t("task.tabAll")}</TabsTrigger>
          <TabsTrigger value="export">{tKey(TASK_TYPE_LABEL.export)}</TabsTrigger>
          <TabsTrigger value="import">{tKey(TASK_TYPE_LABEL.import)}</TabsTrigger>
          <TabsTrigger value="migrate">{tKey(TASK_TYPE_LABEL.migrate)}</TabsTrigger>
          <TabsTrigger value="compare">{tKey(TASK_TYPE_LABEL.compare)}</TabsTrigger>
          <TabsTrigger value="dictionary">{tKey(TASK_TYPE_LABEL.dictionary)}</TabsTrigger>
        </TabsList>
      </Tabs>

      {filtered.length === 0 && (
        <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed bg-background py-16 text-center">
          <ClipboardList className="h-8 w-8 text-muted-foreground/50" />
          <div className="text-sm text-muted-foreground">{t("task.empty")}</div>
          <div className="text-xs text-muted-foreground">{t("task.emptyHint")}</div>
        </div>
      )}

      <div className="grid gap-3">
        {filtered.map((task) => {
          const meta = TYPE_ICON[task.type]
          return (
            <Card key={task.id} className="p-4 transition-shadow hover:shadow-md">
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 items-start gap-3">
                  <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${meta?.cls || "bg-muted"}`}>
                    {meta?.icon}
                  </span>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium">{task.name}</span>
                      <Badge variant="secondary" className="shrink-0 font-normal">{tKey(TASK_TYPE_LABEL[task.type])}</Badge>
                      {task.isLastUsed && <Badge variant="outline" className="shrink-0 font-normal">{t("task.lastUsed")}</Badge>}
                    </div>
                    <div className="mt-1 space-y-0.5 text-sm text-muted-foreground">
                      {taskSummary(task, connections).map((l, i) => (
                        <div key={i} className="truncate" title={l}>{l}</div>
                      ))}
                      <div className="text-xs">{t("task.updatedAt", { time: formatTime(new Date(task.updatedAt).toISOString()) })}</div>
                    </div>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button variant="ghost" size="icon" title={t("task.viewDetail")} onClick={() => openDetail(task)}>
                    <Eye className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="icon" title={t("task.editConfig")} onClick={() => navigate(`/${task.type}?task=${task.id}`)}>
                    <Pencil className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="icon" title={t("common.delete")} onClick={() => doDelete(task)}>
                    <Trash2 className="h-4 w-4 text-destructive" />
                  </Button>
                  <Button size="sm" className="ml-1" onClick={() => doRun(task)}>
                    <Play className="mr-1 h-4 w-4" /> {t("task.run")}
                  </Button>
                </div>
              </div>
            </Card>
          )
        })}
      </div>

      {/* 详情弹窗 */}
      <Dialog open={!!detail} onOpenChange={(o) => !o && setDetail(null)}>
        <DialogContent className="sm:max-w-[560px]">
          <DialogHeader>
            <DialogTitle>{t("task.detailTitle", { name: detail?.name })}</DialogTitle>
          </DialogHeader>
          {detail && (
            <div className="space-y-3 text-sm">
              <div className="space-y-1">
                {taskSummary(detail, connections).map((l, i) => (
                  <div key={i}>{l}</div>
                ))}
              </div>

              {/* 表及条件 */}
              {(() => {
                const conds =
                  detail.exportOpts?.conditions || detail.migrateOpts?.conditions || []
                if (conds.length === 0) return null
                return (
                  <div>
                    <div className="mb-1 font-medium">{t("task.condTitle")}</div>
                    <ScrollArea className="h-28 rounded border p-2">
                      {conds.map((c) => (
                        <div key={c.tableName} className="break-all text-xs">
                          {c.tableName} |
                          {c.dataMode === "skip"
                            ? t("task.noExportData")
                            : ` ${c.query || (c.where ? `WHERE ${c.where}` : t("task.noCond"))}${!c.query && c.columns?.length ? t("task.colsSuffix", { cols: c.columns.join(",") }) : ""}`}
                        </div>
                      ))}
                    </ScrollArea>
                  </div>
                )
              })()}

              {/* 对比任务：表别名配对与忽略列 */}
              {detail.compareOpts && (detail.compareOpts.aliases?.length || detail.compareOpts.ignoreColumns?.length) ? (
                <div>
                  {detail.compareOpts.aliases && detail.compareOpts.aliases.length > 0 && (
                    <div className="mb-2">
                      <div className="mb-1 font-medium">{t("task.aliasTitle")}</div>
                      <ScrollArea className="max-h-28 rounded border p-2">
                        {detail.compareOpts.aliases.map((a) => (
                          <div key={`${a.source}-${a.target}`} className="break-all text-xs">
                            {a.source} ↔ {a.target}
                            {a.ignoreColumns?.length ? t("task.ignoreSuffix", { cols: a.ignoreColumns.join(",") }) : ""}
                          </div>
                        ))}
                      </ScrollArea>
                    </div>
                  )}
                  {detail.compareOpts.ignoreColumns && detail.compareOpts.ignoreColumns.length > 0 && (
                    <div className="break-all text-xs text-muted-foreground">
                      {t("task.globalIgnore", { cols: detail.compareOpts.ignoreColumns.join(", ") })}
                    </div>
                  )}
                </div>
              ) : null}

              <div>
                <div className="mb-1 font-medium">{t("task.historyTitle")}</div>
                {detailHistory.length === 0 && (
                  <div className="text-muted-foreground">{t("task.noHistory")}</div>
                )}
                <ScrollArea className="max-h-36 rounded border p-2">
                  {detailHistory.map((h) => (
                    <div key={h.id} className="flex justify-between text-xs py-0.5">
                      <span>{formatTime(new Date(h.startedAt).toISOString())}</span>
                      <span
                        className={
                          h.status === "done"
                            ? "text-green-600"
                            : h.status === "error"
                              ? "text-destructive"
                              : "text-muted-foreground"
                        }
                      >
                        {h.status === "done" ? t("common.success") : h.status === "error" ? t("common.failed") : h.status}
                      </span>
                      <span className="text-muted-foreground">{h.summary}</span>
                    </div>
                  ))}
                </ScrollArea>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDetail(null)}>{t("common.close")}</Button>
            <Button variant="outline" onClick={() => detail && navigate(`/${detail.type}?task=${detail.id}`)}>
              {t("task.editConfig")}
            </Button>
            <Button onClick={() => detail && doRun(detail)}>{t("task.runNow")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
