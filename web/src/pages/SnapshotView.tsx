import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { Camera, CheckCircle2, ChevronDown, ChevronUp, Database, Loader2, Play, Plus, RotateCcw, ScrollText, Trash2 } from "lucide-react"
import DbTypeIcon from "@/components/DbTypeIcon"
import { Button } from "@/components/ui/button"
import { confirm } from "@/components/ui/alert-dialog"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import CreateSnapshotDialog from "@/components/CreateSnapshotDialog"
import PageHeader from "@/components/PageHeader"
import ProgressView from "@/components/ProgressView"
import { Section } from "@/components/Section"
import { CompareReport } from "@/components/CompareReport"
import * as api from "@/api"
import { useAppStore } from "@/stores/app"
import type { CompareResult, ConnInfo, Progress, SnapshotCompareOptions, SnapshotDetail, SnapshotInfo } from "@/types"

// 快照管理页：列表（左）+ 详情/对比（右），与实时对比并列的一级功能
export default function SnapshotView() {
  const { t } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()

  const [snapshots, setSnapshots] = useState<SnapshotInfo[]>([])
  const [selectedId, setSelectedId] = useState<string | null>(null)
  const [detail, setDetail] = useState<SnapshotDetail | null>(null)
  const [loadingList, setLoadingList] = useState(false)
  const [loadingDetail, setLoadingDetail] = useState(false)

  const [createOpen, setCreateOpen] = useState(false)
  const [search, setSearch] = useState("")

  // 对比态
  const [comparing, setComparing] = useState(false)
  const [runningTaskID, setRunningTaskID] = useState<string | null>(null)
  const [report, setReport] = useState<CompareResult | null>(null)
  const [logs, setLogs] = useState<string[]>([])
  const [showLogs, setShowLogs] = useState(false)
  const logRef = useRef<HTMLDivElement>(null)
  const busy = runningTaskID != null && !report // 对比进行中或未出报告时禁用；对比完成后可重新对比

  // 对比目标选择
  const [targetConn, setTargetConn] = useState("")
  // 快照库 → 目标库 映射（默认同名，仅在不同时设置）
  const [dbMapping, setDBMapping] = useState<Record<string, string>>({})
  // 目标连接下的库列表（用于快照对比的库映射下拉）
  const [targetDBOptions, setTargetDBOptions] = useState<string[]>([])

  const loadList = useCallback(() => {
    setLoadingList(true)
    api
      .listSnapshots()
      .then((list) => setSnapshots(list ?? []))
      .catch((e: Error) => toast.error(t("snapshot.loadFailed", { err: e.message })))
      .finally(() => setLoadingList(false))
  }, [t])

  useEffect(() => {
    loadList()
  }, [loadList])

  // URL 参数消费（running=任务详情）
  useEffect(() => {
    const runningParam = searchParams.get("running")
    if (runningParam) {
      setRunningTaskID(runningParam)
      setComparing(true)
      setReport(null)
      setSearchParams({}, { replace: true })
    }
  }, [searchParams]) // eslint-disable-line react-hooks/exhaustive-deps

  const loadDetail = useCallback((id: string) => {
    setLoadingDetail(true)
    setDetail(null)
    api
      .getSnapshot(id)
      .then(setDetail)
      .catch((e: Error) => toast.error(t("snapshot.loadDetailFailed", { err: e.message })))
      .finally(() => setLoadingDetail(false))
  }, [t])

  const connections = useAppStore((s) => s.connections)

  // 给定快照的默认目标连接：优先用 connId 匹配，否则按连接名/标签回退匹配
  const defaultTargetConn = useCallback(
    (info: SnapshotInfo): string => {
      if (!info) return ""
      const byId = connections.find((c) => c.id === info.connId)
      if (byId) return byId.id
      const byName = connections.find((c) => c.name === info.connLabel)
      return byName?.id || ""
    },
    [connections],
  )

  // 选中快照：加载详情并重置对比态，同时把目标连接默认回快照来源连接
  const selectSnapshot = useCallback(
    (id: string) => {
      setSelectedId(id)
      setReport(null)
      setComparing(false)
      setRunningTaskID(null)
      const info = snapshots.find((s) => s.id === id)
      setTargetConn(info ? defaultTargetConn(info) : "")
      setDBMapping({})
      loadDetail(id)
    },
    [loadDetail, snapshots, defaultTargetConn],
  )

  useEffect(() => {
    if (runningTaskID) return // 通过 URL 进入时详情由用户后续点击
    const first = snapshots[0]
    if (first && !selectedId) {
      setSelectedId(first.id)
      setTargetConn(defaultTargetConn(first))
    }
  }, [snapshots, selectedId, runningTaskID, defaultTargetConn])

  useEffect(() => {
    if (selectedId && !runningTaskID && !detail) loadDetail(selectedId)
  }, [selectedId, runningTaskID, detail, loadDetail])

  const selectedInfo = useMemo(
    () => snapshots.find((s) => s.id === selectedId) || null,
    [snapshots, selectedId],
  )

  // 搜索过滤：名称/数据库/连接名模糊匹配
  const filteredSnapshots = useMemo(() => {
    const q = search.trim().toLowerCase()
    if (!q) return snapshots
    return snapshots.filter(
      (s) =>
        s.name.toLowerCase().includes(q) ||
        s.dbName.toLowerCase().includes(q) ||
        s.connLabel.toLowerCase().includes(q),
    )
  }, [snapshots, search])

  // 加载目标连接下的库列表（用于快照对比的库映射下拉）
  useEffect(() => {
    if (!targetConn) {
      setTargetDBOptions([])
      return
    }
    api
      .getTableTree(targetConn)
      .then(({ databases }) => setTargetDBOptions(databases.map((d) => d.name)))
      .catch(() => setTargetDBOptions([]))
  }, [targetConn])

  // 日志输出时实时滚动到底部
  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [logs, showLogs])

  // 快照包含的库（多库分组，回退单库字段）
  const snapshotDatabases = useMemo(() => {
    if (!detail) return []
    if (detail.databases && detail.databases.length > 0) return detail.databases
    return detail.tables.length || detail.dbName
      ? [{ dbName: detail.dbName, tableCount: detail.tableCount, totalRows: detail.totalRows, tables: detail.tables }]
      : []
  }, [detail])

  const startCompare = useCallback(async () => {
    if (!selectedInfo || !targetConn) {
      toast.error(t("snapshot.needTargetConn"))
      return
    }
    setComparing(true)
    setReport(null)
    setLogs([])
    setShowLogs(false)
    try {
      // 仅提交非同名的映射项（同名无需下发）
      const mapping: Record<string, string> = {}
      for (const [src, tgt] of Object.entries(dbMapping)) {
        if (tgt && tgt !== src) mapping[src] = tgt
      }
      const opts: SnapshotCompareOptions = {
        snapshotId: selectedInfo.id,
        targetConn,
        dbMapping: Object.keys(mapping).length ? mapping : undefined,
      }
      const { taskID } = await api.startSnapshotCompare(opts)
      setRunningTaskID(taskID)
    } catch (e) {
      toast.error(t("snapshot.startCompareFailed", { err: (e as Error).message }))
      setComparing(false)
    }
  }, [selectedInfo, targetConn, dbMapping, t])

  const handleDone = useCallback(
    (p: Progress) => {
      if (!runningTaskID) return
      if (p.logs) setLogs(p.logs)
      if (p.state === "done") {
        api
          .getSnapshotCompareResult(runningTaskID)
          .then((res) => setReport(res))
          .catch((e: Error) => toast.error(t("snapshot.readResultFailed", { err: e.message })))
          .finally(() => setComparing(false))
      } else {
        setComparing(false)
        if (p.state === "error") toast.error(p.message || t("snapshot.compareFailed"))
      }
    },
    [runningTaskID, t],
  )

  const handleDelete = useCallback(async () => {
    if (!selectedInfo) return
    if (!(await confirm({ title: t("snapshot.deleteTitle"), description: t("snapshot.deleteDesc", { name: selectedInfo.name }), confirmText: t("common.delete"), danger: true }))) return
    try {
      await api.deleteSnapshot(selectedInfo.id)
      toast.success(t("snapshot.deleted"))
      setSelectedId(null)
      setDetail(null)
      loadList()
    } catch (e) {
      toast.error(t("snapshot.deleteFailed", { err: (e as Error).message }))
    }
  }, [selectedInfo, loadList, t])

  const resetCompare = useCallback(() => {
    setComparing(false)
    setRunningTaskID(null)
    setReport(null)
  }, [])

  return (
    // 负 margin 抵消外层 main 的 p-6，让工作区撑满视口；外层不滚动，只有内部列表滚动
    <div className="-m-6 flex h-[calc(100%+3rem)] flex-col">
      <div className="shrink-0 px-6 pb-4 pt-6">
        <PageHeader
          title={t("snapshot.title")}
          description={t("snapshot.desc")}
          actions={
            <Button variant="outline" onClick={() => setCreateOpen(true)}>
              <Plus className="mr-1 h-4 w-4" /> {t("snapshot.create")}
            </Button>
          }
        />
      </div>

      <div className="flex min-h-0 flex-1 gap-4 px-6 pb-6">
        {/* 左侧快照列表 */}
        <Card className="flex min-h-0 w-72 shrink-0 flex-col">
          <div className="border-b p-3">
            <Input
              placeholder={t("snapshot.searchPlaceholder")}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="h-8"
            />
          </div>
          <div className="scrollbar-thin min-h-0 flex-1 space-y-1 overflow-y-auto p-2">
            {loadingList && (
              <div className="flex items-center justify-center gap-2 py-8 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" /> {t("common.loading")}
              </div>
            )}
            {!loadingList && filteredSnapshots.length === 0 && (
              <div className="py-10 text-center text-sm text-muted-foreground">
                {snapshots.length === 0 ? t("snapshot.empty") : t("snapshot.noMatch")}
              </div>
            )}
            {filteredSnapshots.map((s) => (
              <button
                key={s.id}
                type="button"
                onClick={() => selectSnapshot(s.id)}
                className={
                  "w-full rounded-md border p-2.5 text-left transition-colors " +
                  (s.id === selectedId ? "border-primary bg-primary/5" : "hover:bg-accent")
                }
              >
                <div className="flex items-center gap-1.5">
                  <Camera className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium">{s.name}</span>
                </div>
                <div className="mt-1 flex items-center gap-1.5 text-xs text-muted-foreground">
                  <DbTypeIcon type={s.dbType} className="h-3.5 w-3.5 text-[7px]" />
                  <span className="truncate">{s.dbNames?.length ? s.dbNames.join(", ") : s.dbName}</span>
                </div>
                <div className="mt-1 text-[11px] text-muted-foreground">
                  {t("snapshot.tableCount", { n: s.tableCount })} · {s.createdAt ? new Date(s.createdAt * 1000).toLocaleString() : ""}
                </div>
              </button>
            ))}
          </div>
        </Card>

        {/* 右侧主操作区：统一工作区卡片，内部按需分区 */}
        <Card className="flex min-h-0 min-w-0 flex-1 flex-col gap-4 overflow-hidden bg-gradient-to-br from-muted/50 via-muted/15 to-muted/85 p-5 dark:from-muted/30 dark:via-muted/10 dark:to-muted/55">
          {detail && (
            <>
              {/* 快照摘要信息块 */}
              <div className="shrink-0">
                <div className="flex items-start justify-between gap-3">
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <h3 className="truncate text-base font-medium">{detail.name}</h3>
                      <DbTypeIcon type={detail.dbType} />
                    </div>
                    <div className="mt-0.5 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-xs text-muted-foreground">
                      <span className="flex items-center gap-1">
                        <Database className="h-3 w-3" />
                        {detail.connLabel} / {(detail.dbNames?.length ? detail.dbNames.join(", ") : detail.dbName) || "—"}
                      </span>
                      <span>{new Date(detail.createdAt * 1000).toLocaleString()}</span>
                      <span>{t("snapshot.tableCount", { n: detail.tableCount })} · {t("common.rows", { n: detail.totalRows.toLocaleString() })}</span>
                    </div>
                    {detail.description && (
                      <div className="mt-0.5 text-xs text-muted-foreground">{detail.description}</div>
                    )}
                  </div>
                  <div className="flex shrink-0 gap-2">
                    <Button variant="ghost" size="icon" onClick={handleDelete} disabled={busy} title={t("snapshot.deleteTitle")}>
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              </div>

              {/* 对比配置 — Section 分区，与其他页面风格一致 */}
              <div className="shrink-0">
              <Section title={t("snapshot.compareSection")} description={t("snapshot.compareSectionDesc")}>
                <div className="flex flex-wrap items-end gap-x-6 gap-y-2">
                  <div className="min-w-[200px] space-y-1">
                    <label className="text-xs font-medium text-muted-foreground">{t("snapshot.targetConn")}</label>
                    <Select value={targetConn} onValueChange={setTargetConn} disabled={busy}>
                      <SelectTrigger className="h-8 text-xs">
                        <SelectValue placeholder={t("snapshot.selectConnPlaceholder")} />
                      </SelectTrigger>
                      <SelectContent>
                        {connections.length === 0 && (
                          <div className="px-2 py-1.5 text-center text-xs text-muted-foreground">{t("snapshot.noConn")}</div>
                        )}
                        {connections.map((c) => (
                          <SelectItem key={c.id} value={c.id}>
                            <span className="flex items-center gap-1.5">
                              <DbTypeIcon type={c.conn.Type} className="h-3.5 w-3.5 text-[7px]" />
                              <span>{c.name}</span>
                            </span>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>

                  {/* 库映射 — 仅多库时展示 */}
                  {(snapshotDatabases.length > 1 || Object.keys(dbMapping).some(k => dbMapping[k] !== k)) && (
                    <div className="min-w-[200px] space-y-1">
                      <label className="text-xs font-medium text-muted-foreground">{t("compare.dbMapping")}</label>
                      {/* 限高内滚：库多时不撑高对比配置 Section */}
                      <div className="scrollbar-thin max-h-32 space-y-1 overflow-y-auto pr-1">
                      {snapshotDatabases.map((db) => {
                        const mapped = dbMapping[db.dbName] || db.dbName
                        return (
                          <div key={db.dbName} className="flex items-center gap-1.5 text-xs">
                            <span className="w-28 shrink-0 truncate font-mono" title={db.dbName}>{db.dbName}</span>
                            <span className="text-muted-foreground">→</span>
                            <Select
                              value={mapped}
                              onValueChange={(v) => setDBMapping((prev) => ({ ...prev, [db.dbName]: v }))}
                              disabled={busy}
                            >
                              <SelectTrigger className="h-7 w-36 text-xs">
                                <SelectValue placeholder={t("snapshot.targetDbPlaceholder")} />
                              </SelectTrigger>
                              <SelectContent>
                                {targetDBOptions.map((d) => (
                                  <SelectItem key={d} value={d}>{d}</SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          </div>
                        )
                      })}
                      </div>
                    </div>
                  )}

                  <div className="ml-auto pb-0.5">
                    <Button onClick={startCompare} disabled={!targetConn || busy} size="sm">
                      {report ? (
                        <><RotateCcw className="mr-1 h-3.5 w-3.5" /> {t("snapshot.recompare")}</>
                      ) : (
                        <><Play className="mr-1 h-3.5 w-3.5" /> {t("compare.start")}</>
                      )}
                    </Button>
                  </div>
                </div>
                {!targetConn && detail.connId && (
                  <p className="mt-2 text-xs text-amber-600">{t("snapshot.connNotFound")}</p>
                )}
              </Section>
              </div>

              {/* 包含的表预览（对比中/出报告时隐藏，避免与结果区重复）——唯一滚动区 */}
              {!busy && !report && (
              <section className="flex min-h-0 flex-1 flex-col space-y-3">
                <div className="shrink-0">
                  <h3 className="text-sm font-medium">{t("snapshot.containsTables", { n: detail.tableCount })}</h3>
                </div>
                <div className="shrink-0">
                  {snapshotDatabases.length > 1 && (
                    <p className="mb-1 text-xs text-muted-foreground">
                      {t("snapshot.dbCount", { n: snapshotDatabases.length })}
                    </p>
                  )}
                  {/* 表头 */}
                  <div className="mb-0.5 grid grid-cols-[minmax(0,2fr)_48px_1fr_64px] gap-x-3 border-b px-1 pb-1 text-xs font-medium text-muted-foreground">
                    <span>{t("snapshot.colTable")}</span>
                    <span>{t("snapshot.colColumns")}</span>
                    <span>{t("snapshot.colPK")}</span>
                    <span className="text-right">{t("snapshot.colRows")}</span>
                  </div>
                </div>
                <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto">
                  {snapshotDatabases.map((db) => (
                    <div key={db.dbName}>
                      {snapshotDatabases.length > 1 && (
                        <div className="sticky top-0 z-10 mt-1.5 flex items-center gap-1.5 bg-background px-1 py-0.5 text-xs font-medium text-muted-foreground">
                          <Database className="h-3 w-3" />
                          <span className="font-mono">{db.dbName}</span>
                          <span className="font-normal">（{t("snapshot.tableCount", { n: db.tableCount })} · {t("common.rows", { n: db.totalRows.toLocaleString() })}）</span>
                        </div>
                      )}
                      {db.tables.map((t) => (
                        <div
                          key={t.name}
                          className="grid grid-cols-[minmax(0,2fr)_48px_1fr_64px] gap-x-3 px-1 py-1 text-xs hover:bg-accent/50 rounded"
                        >
                          <span className="truncate font-mono" title={t.name}>{t.name}</span>
                          <span className="tabular-nums text-center">{t.columns.length}</span>
                          <span className="truncate text-muted-foreground" title={
                            Array.isArray(t.primaryKey) && t.primaryKey.length ? t.primaryKey.join(",") : ""
                          }>
                            {Array.isArray(t.primaryKey) && t.primaryKey.length ? t.primaryKey.join(",") : "—"}
                          </span>
                          <span className="text-right tabular-nums text-muted-foreground">{t.rowCount.toLocaleString()}</span>
                        </div>
                      ))}
                    </div>
                  ))}
                </div>
              </section>
              )}

              {/* 对比进度 / 报告（在快照信息与表列表下方追加） */}
              {runningTaskID && comparing && (
                <div className="shrink-0">
                  <ProgressView
                    taskID={runningTaskID}
                    taskType="snapshot_compare"
                    onDone={handleDone}
                    onBack={resetCompare}
                    wide
                    compactLog
                  />
                </div>
              )}

              {report && (
                <>
                  {/* 完成后状态条：与实时对比风格一致 */}
                  <div className="flex shrink-0 items-center gap-3 rounded-lg border border-green-500/30 bg-green-500/10 px-4 py-2 text-sm text-green-800 dark:text-green-300">
                    <CheckCircle2 className="h-4 w-4 shrink-0 text-green-600 dark:text-green-400" />
                    <span className="font-medium">{t("compare.done")}</span>
                    <span className="text-xs opacity-80">
                      {t("compare.doneSummary", { total: report.summary.total, matched: report.summary.matched, diff: report.summary.total - report.summary.matched })}
                    </span>
                    <div className="ml-auto flex items-center gap-1">
                      {logs.length > 0 && (
                        <Button variant="ghost" size="sm" className="h-7 text-xs text-green-800 hover:bg-green-100 hover:text-green-900" onClick={() => setShowLogs((v) => !v)}>
                          <ScrollText className="mr-1 h-3.5 w-3.5" /> {t("compare.execLog")}
                          {showLogs ? <ChevronUp className="ml-1 h-3.5 w-3.5" /> : <ChevronDown className="ml-1 h-3.5 w-3.5" />}
                        </Button>
                      )}
                    </div>
                  </div>
                  {showLogs && logs.length > 0 && (
                    <div ref={logRef} className="scrollbar-thin max-h-56 shrink-0 overflow-y-auto rounded-md bg-slate-950 p-3 text-slate-200">
                      <div className="space-y-0.5 text-xs leading-relaxed">
                        {logs.map((l, i) => (
                          <div key={i} className="whitespace-pre-wrap break-all">{l}</div>
                        ))}
                      </div>
                    </div>
                  )}
                  <div className="flex min-h-0 flex-1 flex-col">
                    <CompareReport result={report} />
                  </div>
                </>
              )}
            </>
          )}

          {!runningTaskID && !report && !detail && !loadingDetail && (
            <Card className="flex flex-col items-center justify-center gap-3 py-20">
              <Camera className="h-12 w-12 text-muted-foreground/40" />
              <div className="text-sm text-muted-foreground">{t("snapshot.selectHint")}</div>
              <Button variant="outline" onClick={() => setCreateOpen(true)}>
                <Plus className="mr-1 h-4 w-4" /> {t("snapshot.createHere")}
              </Button>
            </Card>
          )}

          {loadingDetail && (
            <div className="flex items-center justify-center gap-2 py-20 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> {t("snapshot.loadingDetail")}
            </div>
          )}
        </Card>
      </div>

      <CreateSnapshotDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={(id) => {
          loadList()
          selectSnapshot(id)
        }}
      />
    </div>
  )
}
