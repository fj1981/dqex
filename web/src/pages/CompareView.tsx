import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { toast } from "sonner"
import { ArrowRight, CheckCircle2, ChevronDown, ChevronUp, MoveRight, Play, ScrollText, Search, X } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import Hint from "@/components/Hint"
import TaskConfigBar from "@/components/TaskConfigBar"
import WizardFooter from "@/components/WizardFooter"
import * as api from "@/api"
import ConnectionPair from "@/components/ConnectionPair"
import PageHeader from "@/components/PageHeader"
import ProgressView from "@/components/ProgressView"
import SaveTaskDialog from "@/components/SaveTaskDialog"
import { Section } from "@/components/Section"
import StepWizard from "@/components/StepWizard"
import TablePicker from "@/components/TablePicker"
import ColumnMultiSelect, { isTimeColumn, toColumnOptions } from "@/components/ColumnMultiSelect"
import { CompareReport } from "@/components/CompareReport"
import { useAppStore } from "@/stores/app"
import { cn } from "@/lib/utils"
import type {
  CompareDBPair,
  CompareOptions,
  CompareResult,
  CompareTableResult,
  TableAlias,
  TableColumn,
  TaskConfig,
} from "@/types"

const STEPS = ["选择源和目标库", "选择表", "设置对比选项", "结果"]

// 对比范围：三选一互斥，取代原先两个相互制约的复选框
const COMPARE_SCOPES = [
  { key: "both", label: "结构 + 数据", desc: "列结构与数据同时对比（默认）", structureOnly: false, dataOnly: false },
  { key: "structure", label: "仅结构", desc: "只对比表结构差异，跳过数据", structureOnly: true, dataOnly: false },
  { key: "data", label: "仅数据", desc: "只对比数据差异，跳过结构", structureOnly: false, dataOnly: true },
]

function defaultOptions(): CompareOptions {
  return {
    sourceConn: "",
    targetConn: "",
    databases: [],
    tables: [],
    aliases: [],
    structureOnly: false,
    dataOnly: false,
    threshold: 1000,
    ignoreColumns: [],
    forceData: false,
  }
}

// "库.表" 限定名解析
const bareName = (t: string) => (t.includes(".") ? t.slice(t.indexOf(".") + 1) : t)
const dbOf = (t: string) => (t.includes(".") ? t.slice(0, t.indexOf(".")) : "")

// 对比页：四步向导（作用域为单个库对，支持表别名配对）
export default function CompareView() {
  const [searchParams, setSearchParams] = useSearchParams()
  // 会话内缓存的最近应用配置：挂载时同步初始化，避免空配置闪现后再回填
  const cachedTask = useAppStore((s) => s.lastTasks["compare"])
  const setLastTask = useAppStore((s) => s.setLastTask)
  const clearLastTask = useAppStore((s) => s.clearLastTask)
  const [step, setStep] = useState(0)
  const [opts, setOpts] = useState<CompareOptions>(() =>
    cachedTask?.compareOpts ? { ...defaultOptions(), ...cachedTask.compareOpts } : defaultOptions())
  const [taskConfigId, setTaskConfigId] = useState<string | undefined>(cachedTask?.id)
  const [runningTaskID, setRunningTaskID] = useState("")
  const [savedTasks, setSavedTasks] = useState<TaskConfig[]>([])
  const [saveOpen, setSaveOpen] = useState(false)
  const [taskState, setTaskState] = useState("running")
  const [report, setReport] = useState<CompareResult | null>(null)
  const [targetTables, setTargetTables] = useState<string[]>([])
  const [targetDBOptions, setTargetDBOptions] = useState<string[]>([])
  // 目标库列表加载状态：用于库映射卡片区分「正在加载 / 已加载为空 / 加载失败」三种 UI，
  // 避免下拉为空时无法判断是后端权限问题还是仍在加载中
  const [targetDBsLoaded, setTargetDBsLoaded] = useState(false)
  const [targetDBsLoading, setTargetDBsLoading] = useState(false)
  const [targetDBsError, setTargetDBsError] = useState("")
  const [tableCols, setTableCols] = useState<Record<string, TableColumn[]>>({})
  const [colsLoading, setColsLoading] = useState(false)
  const filledDefaults = useRef(false)
  const [aliasQuery, setAliasQuery] = useState("")
  const [aliasOnlyConfigured, setAliasOnlyConfigured] = useState(false)
  const [logs, setLogs] = useState<string[]>([])
  const [showLogs, setShowLogs] = useState(false)
  const connections = useAppStore((s) => s.connections)

  const set = (patch: Partial<CompareOptions>) => setOpts((o) => ({ ...o, ...patch }))

  // 按主键 id（兼容旧任务配置中的连接名）查找连接
  const findConn = (key: string) => connections.find((c) => c.id === key || c.name === key)

  // 选中的源库 / 目标库（多库对比）：从 databases 库对推导
  const sourceDBs = useMemo(() => (opts.databases || []).map((d) => d.sourceDB), [opts.databases])
  const targetDBs = useMemo(
    () => (opts.databases || []).map((d) => d.targetDB || d.sourceDB),
    [opts.databases],
  )
  // 库映射：源库名 → 目标库名（供 TablePicker 把目标库独有表归并到对应源库节点）
  const dbMapping = useMemo(() => {
    const m: Record<string, string> = {}
    for (const d of opts.databases || []) m[d.sourceDB] = d.targetDB || d.sourceDB
    return m
  }, [opts.databases])
  // 列信息拉取 / 单库作用域使用的主库（取第一个源库）
  const sourceDB = sourceDBs[0] || opts.source?.DBName || findConn(opts.sourceConn)?.conn.DBName || ""
  // 目标库树拉取 / 别名目标候选使用的主目标库（取第一个目标库）
  const targetDB = targetDBs[0] || opts.target?.DBName || findConn(opts.targetConn)?.conn.DBName || ""

  // 库选择变化时重建库对（优先保留已有映射的目标库，否则同名）
  const handleDBsChange = (databases: string[]) => {
    const prevMap = new Map((opts.databases || []).map((d) => [d.sourceDB, d.targetDB]))
    const pairs: CompareDBPair[] = databases.map((src) => ({ sourceDB: src, targetDB: prevMap.get(src) || src }))
    set({ databases: pairs })
  }

  // 设置某源库的对比目标库（同名配对时无需调用）
  const setDBMapping = (sourceDB: string, targetDB: string) => {
    set({
      databases: (opts.databases || []).map((d) =>
        d.sourceDB === sourceDB ? { ...d, targetDB } : d,
      ),
      dbMapping: { ...(opts.dbMapping || {}), [sourceDB]: targetDB },
    })
  }

  const loadSavedTasks = useCallback(async () => {
    try {
      setSavedTasks((await api.listTasks("compare")) || [])
    } catch { /* ignore */ }
  }, [])

  const applyTask = useCallback((task: TaskConfig) => {
    if (task.compareOpts) {
      setOpts({ ...defaultOptions(), ...task.compareOpts })
      setTaskConfigId(task.id)
      setLastTask(task)
    }
  }, [setLastTask])

  // URL 参数消费（task=编辑配置 / running=任务详情）
  useEffect(() => {
    const taskParam = searchParams.get("task")
    const runningParam = searchParams.get("running")
    if (runningParam) {
      setRunningTaskID(runningParam)
      setStep(3)
      setSearchParams({}, { replace: true })
    } else if (taskParam) {
      api.getTask(taskParam).then(applyTask).catch((e: Error) => toast.error(e.message))
      setSearchParams({}, { replace: true })
    }
  }, [searchParams]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    loadSavedTasks()
    // URL 参数优先（由上方 effect 消费）；其次会话缓存已同步初始化，无需异步回填；
    // 两者皆无才拉取上次配置（首次进入该页面）
    if (searchParams.get("task") || searchParams.get("running") || cachedTask) return
    api.getLastTask("compare").then(({ task }) => task && applyTask(task)).catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // 选好目标连接后加载目标库清单：供「库映射」下拉（选表步骤）与别名配对候选（选项步骤）使用
  // 拆分为两个 effect：targetDBOptions 只依赖 targetConn（库映射卡片稳定显示，不随用户勾选表而重拉）；
  // step=2 的表列表才依赖 targetDB/targetDBs（同名配对时只列该库的表）。
  useEffect(() => {
    if (!opts.targetConn) {
      setTargetDBOptions([])
      setTargetDBsLoaded(false)
      setTargetDBsLoading(false)
      setTargetDBsError("")
      setTargetTables([])
      return
    }
    let cancelled = false
    setTargetDBsLoading(true)
    setTargetDBsError("")
    api
      .getTableTree(opts.targetConn)
      .then(({ databases }) => {
        if (cancelled) return
        setTargetDBOptions(databases.map((d) => d.name))
        setTargetDBsLoaded(true)
      })
      .catch((e: Error) => {
        if (cancelled) return
        console.error("[CompareView] targetDBList error", e)
        setTargetDBOptions([])
        setTargetDBsError(e.message || "加载目标库列表失败")
        setTargetDBsLoaded(true)
      })
      .finally(() => {
        if (!cancelled) setTargetDBsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [opts.targetConn])

  // step=2 进入「设置对比选项」时，再按选中的目标库拉表清单，作为别名配对候选
  useEffect(() => {
    if (step !== 2 || !opts.targetConn) {
      return
    }
    let cancelled = false
    api
      .getTableTree(opts.targetConn, targetDB || undefined)
      .then(({ databases }) => {
        if (cancelled) return
        const want = new Set(targetDBs)
        const lst = want.size
          ? databases.filter((d) => want.has(d.name)).flatMap((d) => d.tables)
          : databases.flatMap((d) => d.tables)
        setTargetTables(lst)
      })
      .catch((e: Error) => {
        if (cancelled) return
        console.error("[CompareView] targetTables error", e)
        setTargetTables([])
      })
    return () => {
      cancelled = true
    }
  }, [step, opts.targetConn, targetDB, targetDBs])

  // 任务状态跟踪（ProgressView 内部另有订阅，此处用于触发结果拉取与留存日志）
  useEffect(() => {
    if (!runningTaskID) return
    setTaskState("running")
    setReport(null)
    setLogs([])
    setShowLogs(false)
    const close = api.subscribeProgress(runningTaskID, {
      onProgress: (p) => {
        setTaskState(p.state || "running")
        if (p.logs) setLogs(p.logs)
      },
      onDone: (p) => {
        setTaskState(p?.state || "done")
        if (p?.logs) setLogs(p.logs)
      },
      onError: () => setTaskState("error"),
    })
    return close
  }, [runningTaskID])

  // 任务完成后拉取对比报告（历史回放同样生效）
  useEffect(() => {
    if (taskState !== "done" || !runningTaskID || report) return
    api.getCompareResult(runningTaskID).then(setReport).catch(() => {})
  }, [taskState, runningTaskID, report])

  // 表级配置更新：同一源表只保留一条（目标表名 + 忽略列），两者皆空则删除条目
  const upsertAlias = (source: string, patch: { target?: string; ignoreColumns?: string[] }) => {
    const cur = (opts.aliases || []).find((a) => a.source === source)
    const next: TableAlias = {
      source,
      target: (patch.target ?? cur?.target ?? "").trim(),
      ignoreColumns: (patch.ignoreColumns ?? cur?.ignoreColumns ?? []).filter(Boolean),
    }
    const rest = (opts.aliases || []).filter((a) => a.source !== source)
    if (next.target || (next.ignoreColumns || []).length > 0) rest.push(next)
    set({ aliases: rest })
  }
  const setAlias = (source: string, target: string) => upsertAlias(source, { target })
  const setTableIgnore = (source: string, cols: string[]) => upsertAlias(source, { ignoreColumns: cols })

  const startRun = async () => {
    if (!opts.sourceConn || !opts.targetConn) {
      toast.error("请先选择源和目标数据库连接")
      setStep(0)
      return
    }
    if (opts.sourceConn === opts.targetConn) {
      toast.error("源和目标不能是同一个连接")
      setStep(0)
      return
    }
    // 别名目标重复校验（同一目标表被多个源表映射）
    const seen = new Set<string>()
    for (const a of opts.aliases || []) {
      const t = a.target.trim().toLowerCase()
      if (!t) continue
      if (seen.has(t)) {
        toast.error(`别名配对重复：多个源表映射到目标表 ${a.target}`)
        setStep(2)
        return
      }
      seen.add(t)
    }
    try {
      // 表范围完全由用户勾选决定：未勾选 = 对比库内全部表（后端 nil=全部）；
      // 勾选了则严格只对比所选表（限定名 "源库.表"）。运行时源用源库、目标用映射库，按裸名过滤。
      const payload: CompareOptions = {
        ...opts,
        tables: (opts.tables || []).length > 0 ? opts.tables : undefined,
        ignoreColumns: (opts.ignoreColumns || []).length > 0 ? opts.ignoreColumns : undefined,
        forceData: opts.forceData || undefined,
      }
      // 调试日志：库映射配置经过哪些层到达后端，避免修改多库对比时漏改某条链路
      console.log("[CompareView] startCompare payload", {
        dbs: payload.databases,
        dbMapping: payload.dbMapping,
        sources: sourceDBs,
        remappedCount,
      })
      const { taskID } = await api.startCompare(payload, taskConfigId)
      setRunningTaskID(taskID)
      setStep(3)
      setTimeout(() => useAppStore.getState().loadHistory(), 800)
    } catch (e) {
      toast.error(`启动失败: ${(e as Error).message}`)
    }
  }

  const selectedQualifiedTables = useMemo(() => [...new Set(opts.tables || [])], [opts.tables])

  // 进入选项步骤时拉取选中表的列信息（忽略列下拉候选）；多库场景下按限定名对应的源库分别拉取
  // 首次进入时默认回填时间类列（创建/更新时间），全局忽略同步预选
  const selectedTablesKey = selectedQualifiedTables.join(",")
  useEffect(() => {
    if (step !== 2 || !opts.sourceConn || selectedQualifiedTables.length === 0) return
    let cancelled = false
    setColsLoading(true)
    Promise.all(
      selectedQualifiedTables.map((q) => {
        const db = dbOf(q) || sourceDB
        const t = bareName(q)
        return api
          .getTableColumns(opts.sourceConn, db, t)
          .then((r) => r.columns || [])
          .catch(() => [] as TableColumn[])
      }),
    )
      .then((colsArr) => {
        if (cancelled) return
        const map: Record<string, TableColumn[]> = {}
        selectedQualifiedTables.forEach((q, i) => {
          map[q] = colsArr[i]
        })
        setTableCols(map)
        if (!filledDefaults.current) {
          filledDefaults.current = true
          setOpts((prev) => {
            const aliases = [...(prev.aliases || [])]
            selectedQualifiedTables.forEach((q, i) => {
              const times = colsArr[i].filter(isTimeColumn).map((c) => c.name)
              if (times.length === 0) return
              const idx = aliases.findIndex((a) => a.source === q)
              if (idx >= 0) {
                if ((aliases[idx].ignoreColumns || []).length === 0) {
                  aliases[idx] = { ...aliases[idx], ignoreColumns: times }
                }
              } else {
                aliases.push({ source: q, target: "", ignoreColumns: times })
              }
            })
            const timeUnion = [...new Set(colsArr.flat().filter(isTimeColumn).map((c) => c.name))]
            const ignoreColumns =
              !prev.ignoreColumns?.length && timeUnion.length > 0 ? timeUnion : prev.ignoreColumns
            return { ...prev, aliases, ignoreColumns }
          })
        }
      })
      .finally(() => {
        if (!cancelled) setColsLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [step, opts.sourceConn, sourceDB, selectedTablesKey]) // eslint-disable-line react-hooks/exhaustive-deps

  // 全局忽略列下拉候选：所有已加载表列的并集（同名去重）
  const globalColOptions = useMemo(() => {
    const seen = new Map<string, TableColumn>()
    Object.values(tableCols).forEach((cols) =>
      cols.forEach((c) => {
        const k = c.name.toLowerCase()
        if (!seen.has(k)) seen.set(k, c)
      }),
    )
    return toColumnOptions([...seen.values()])
  }, [tableCols])
  // 单表的表级配置条目（目标表名 + 忽略列）；source 使用限定名，实现多库同名表分别配置
  const aliasEntryOf = (src: string) => (opts.aliases || []).find((a) => a.source === src)
  const aliasOf = (src: string) => aliasEntryOf(src)?.target || ""
  const ignoreOf = (src: string) => (aliasEntryOf(src)?.ignoreColumns || []).join(",")
  const isConfigured = (src: string) => {
    const a = aliasEntryOf(src)
    return !!a && (!!a.target.trim() || (a.ignoreColumns || []).length > 0)
  }
  const configuredAliases = useMemo(
    () => (opts.aliases || []).filter((a) => a.target.trim() || (a.ignoreColumns || []).length > 0),
    [opts.aliases],
  )
  const scopeKey = opts.structureOnly ? "structure" : opts.dataOnly ? "data" : "both"
  // 库映射卡片：搜索过滤后的库对列表 + 已重命名数量；库多时可滚动并搜索
  const [dbMapQuery, setDbMapQuery] = useState("")
  const filteredDBPairs = useMemo(() => {
    const q = dbMapQuery.trim().toLowerCase()
    const pairs = opts.databases || []
    if (!q) return pairs
    return pairs.filter(
      (p) =>
        (p.sourceDB || "").toLowerCase().includes(q) ||
        (p.targetDB || "").toLowerCase().includes(q),
    )
  }, [opts.databases, dbMapQuery])
  const remappedCount = useMemo(
    () => (opts.databases || []).filter((p) => p.targetDB && p.targetDB !== p.sourceDB).length,
    [opts.databases],
  )
  // 表级配置列表过滤：搜索关键词 + 仅看已配置；表多时无需翻长列表
  const aliasList = selectedQualifiedTables.filter(
    (t) =>
      (!aliasOnlyConfigured || isConfigured(t)) &&
      (!aliasQuery.trim() ||
        t.toLowerCase().includes(aliasQuery.trim().toLowerCase()) ||
        aliasOf(t).toLowerCase().includes(aliasQuery.trim().toLowerCase())),
  )
  // 按库分组展示，多库场景下可清晰看到表所属库
  const aliasGroups = useMemo(() => {
    const map = new Map<string, string[]>()
    for (const q of aliasList) {
      const db = dbOf(q)
      const list = map.get(db) || []
      list.push(q)
      map.set(db, list)
    }
    return [...map.entries()].sort(([a], [b]) => a.localeCompare(b))
  }, [aliasList])
  // datalist 候选：与当前输入相关的目标表（避免表多时下拉巨长）
  const aliasCandidates = useMemo(() => {
    const q = aliasQuery.trim().toLowerCase()
    if (!q) return targetTables.slice(0, 50)
    return targetTables.filter((t) => t.toLowerCase().includes(q)).slice(0, 50)
  }, [targetTables, aliasQuery])

  return (
    <div className="mx-auto flex h-full max-w-5xl flex-col gap-4">
      <PageHeader
        title="对比数据库"
        description="比较两个数据库的表结构与数据差异（小表逐行比较，大表仅比行数）"
        actions={
          <TaskConfigBar
            savedTasks={savedTasks}
            taskConfigId={taskConfigId}
            onApply={applyTask}
            onClear={() => { setOpts(defaultOptions()); setTaskConfigId(undefined); clearLastTask("compare") }}
            onSave={() => setSaveOpen(true)}
          />
        }
      />

      <Card className="flex flex-1 flex-col gap-5 bg-gradient-to-br from-muted/50 via-muted/15 to-muted/85 p-5 dark:from-muted/30 dark:via-muted/10 dark:to-muted/55">
        <StepWizard steps={STEPS} current={step} onStepClick={(i) => !runningTaskID && setStep(i)} />

        {/* 数据源卡片布局共用 ConnectionPair，各任务页卡片尺寸统一 */}
        {step === 0 && (
          <ConnectionPair
          source={{
            title: "源数据库",
            subtitle: "对比基准",
            value: opts.sourceConn,
            onChange: (id) => set({ sourceConn: id, source: null, databases: [], tables: [], aliases: [] }),
          }}
          target={{
            title: "目标数据库",
            subtitle: "与源库比较",
            value: opts.targetConn,
            onChange: (id) => set({ targetConn: id, target: null }),
          }}
        >
          {opts.sourceConn && opts.sourceConn === opts.targetConn && (
            <Hint variant="warning">源和目标不能是同一个连接，请重新选择。</Hint>
          )}
          {!!opts.sourceConn && !!opts.targetConn && opts.sourceConn !== opts.targetConn && (
            <Hint>
              支持勾选多个库进行多库对比；表按名称匹配，不同名的同义表可在下一步配置别名配对。
            </Hint>
          )}
          <WizardFooter
            next={
              <Button
                disabled={!opts.sourceConn || !opts.targetConn || opts.sourceConn === opts.targetConn}
                onClick={() => setStep(1)}
              >
                下一步 <MoveRight className="ml-1 h-4 w-4" />
              </Button>
            }
          />
        </ConnectionPair>
      )}

      {step === 1 && (
        <div className="space-y-4">
          <Hint>
            勾选要对比的表；仅目标库有的表也可勾选（带标记，对比时报告为「仅目标有」）。勾选库节点将级联选中库下所有表，不勾选任何表则对比库内全部表。
          </Hint>
          <TablePicker
            connId={opts.sourceConn}
            extraConnId={opts.targetConn}
            extraDb={undefined}
            dbMapping={Object.keys(dbMapping).length > 0 ? dbMapping : undefined}
            selected={opts.tables || []}
            showObjects={false}
            selectedDBs={sourceDBs}
            onDBsChange={handleDBsChange}
            conditions={[]}
            onChange={(tables) => set({ tables })}
          />

          {/* 库级映射：每个源库指定对比的目标库（同名默认，可改为目标库的其他库） */}
          {sourceDBs.length > 0 && (
            <Card className="p-4">
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <div className="flex items-center gap-2">
                  <div className="text-sm font-medium">库映射</div>
                  <div className="text-xs text-muted-foreground">
                    源库 → 目标库（同名默认匹配）
                    {remappedCount > 0 && (
                      <span className="ml-2 rounded bg-primary/10 px-1.5 py-0.5 text-primary">
                        已重命名 {remappedCount}
                      </span>
                    )}
                  </div>
                </div>
                {sourceDBs.length > 6 && (
                  <div className="relative w-56">
                    <Search className="pointer-events-none absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                    <Input
                      className="h-7 pl-7 text-xs"
                      placeholder="搜索源库…"
                      value={dbMapQuery}
                      onChange={(e) => setDbMapQuery(e.target.value)}
                    />
                  </div>
                )}
              </div>
              <Hint variant={targetDBsError ? "warning" : "info"}>
                {targetDBsLoading ? (
                  <>目标连接的库列表加载中…</>
                ) : targetDBsError ? (
                  <>
                    目标库列表加载失败：{targetDBsError}。库映射将回退到同名匹配；如需映射到不同名的目标库，请检查目标连接的库访问权限（SHOW DATABASES / pg_database / all_users）后刷新。
                  </>
                ) : targetDBsLoaded && targetDBOptions.length === 0 ? (
                  <>
                    目标连接暂无可枚举的库（可能缺少全局元数据权限），当前将按同名配对；若两侧库名不同，请补全目标连接权限后刷新。
                  </>
                ) : targetDBOptions.length > 0 ? (
                  <>
                    已加载 {targetDBOptions.length} 个目标库；下拉里可改成不同名的目标库，已改的会高亮并在结果中以「源库 ↔ 目标库」展示。
                  </>
                ) : null}
              </Hint>
              <div className="scrollbar-thin mt-3 max-h-72 overflow-y-auto rounded border">
                <table className="w-full text-xs">
                  <thead className="sticky top-0 bg-muted/60 text-muted-foreground">
                    <tr>
                      <th className="w-1/2 px-3 py-1.5 text-left font-medium">源库</th>
                      <th className="w-1/2 px-3 py-1.5 text-left font-medium">目标库</th>
                    </tr>
                  </thead>
                  <tbody>
                    {filteredDBPairs.length === 0 && (
                      <tr>
                        <td colSpan={2} className="px-3 py-4 text-center text-xs text-muted-foreground">
                          无匹配的源库
                        </td>
                      </tr>
                    )}
                    {filteredDBPairs.map((pair) => {
                      const remapped = pair.targetDB && pair.targetDB !== pair.sourceDB
                      // 下拉候选：已加载则用 loaded 列表；否则回退到当前已配置的 targetDB（同名默认），保证用户能看到当前值
                      const candidates = targetDBOptions.length > 0
                        ? Array.from(new Set([pair.targetDB || pair.sourceDB, ...targetDBOptions]))
                        : [pair.targetDB || pair.sourceDB]
                      const currentVal = pair.targetDB || pair.sourceDB
                      return (
                        <tr
                          key={pair.sourceDB}
                          className={cn(
                            "border-t first:border-t-0",
                            remapped ? "bg-primary/5" : "even:bg-muted/30",
                          )}
                        >
                          <td
                            className="max-w-0 truncate px-3 py-1.5 font-mono"
                            title={pair.sourceDB}
                          >
                            {pair.sourceDB}
                          </td>
                          <td className="max-w-0 px-3 py-1.5">
                            <Select
                              value={currentVal}
                              onValueChange={(v) => setDBMapping(pair.sourceDB, v)}
                            >
                              <SelectTrigger
                                className={cn(
                                  "h-7 w-full font-mono text-xs",
                                  remapped ? "border-primary text-primary" : "",
                                )}
                              >
                                <SelectValue placeholder="选择目标库" />
                              </SelectTrigger>
                              <SelectContent>
                                {candidates.map((d) => (
                                  <SelectItem key={d} value={d} className="font-mono text-xs">
                                    {d}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                          </td>
                        </tr>
                      )
                    })}
                  </tbody>
                </table>
              </div>
            </Card>
          )}

          <WizardFooter onBack={() => setStep(0)} onNext={() => setStep(2)} />
        </div>
      )}

      {step === 2 && (
        // min-h-0 防止 Card 被 h-full 父容器拉伸，消除内容下方大片留白
        <div className="mx-auto flex w-full min-h-0 max-w-4xl flex-col gap-4">
          <Card className="divide-y p-0">
            {/* 对比选项：两列网格，标签在上控件在下，说明随行内对齐 */}
            <div className="space-y-5 p-5">
              <div className="grid gap-x-10 gap-y-5 md:grid-cols-2">
                <div>
                  <div className="mb-2 text-sm font-medium">对比内容</div>
                  <div className="flex flex-wrap gap-1.5">
                    {COMPARE_SCOPES.map((s) => (
                      <button
                        key={s.key}
                        type="button"
                        title={s.desc}
                        onClick={() => set({ structureOnly: s.structureOnly, dataOnly: s.dataOnly })}
                        className={cn(
                          "flex items-center gap-1.5 rounded-md border px-2.5 py-1.5 text-xs transition-colors",
                          scopeKey === s.key ? "border-primary/40 bg-primary/5 font-medium text-primary" : "text-muted-foreground hover:bg-accent/50",
                        )}
                      >
                        <span
                          className={cn(
                            "flex h-3 w-3 items-center justify-center rounded-full border",
                            scopeKey === s.key ? "border-primary" : "border-muted-foreground/40",
                          )}
                        >
                          {scopeKey === s.key && <span className="h-1.5 w-1.5 rounded-full bg-primary" />}
                        </span>
                        {s.label}
                      </button>
                    ))}
                  </div>
                </div>
                <div>
                  <div className="mb-2 text-sm font-medium">数据阈值</div>
                  <div className="flex h-8 items-center gap-2">
                    <Input
                      type="number"
                      className="h-8 w-28 text-xs"
                      value={opts.threshold || 0}
                      onChange={(e) => set({ threshold: Number(e.target.value) })}
                    />
                    <span className="text-xs text-muted-foreground">行以内逐行比，超出仅比行数</span>
                  </div>
                </div>
                <div>
                  <div className="mb-2 text-sm font-medium">忽略列（全局）</div>
                  <div className="flex items-start gap-2">
                    <ColumnMultiSelect
                      className="max-w-72"
                      options={globalColOptions}
                      value={opts.ignoreColumns || []}
                      onChange={(cols) => set({ ignoreColumns: cols })}
                      placeholder="选择全局忽略列"
                      loading={colsLoading}
                    />
                    <span className="mt-1.5 shrink-0 text-xs text-muted-foreground">所有表数据对比时跳过；时间列默认已选</span>
                  </div>
                </div>
                <div>
                  <div className="mb-2 text-sm font-medium">结构不一致时</div>
                  <label className="flex h-8 cursor-pointer items-center gap-2 text-xs">
                    <Checkbox
                      checked={opts.forceData || false}
                      onCheckedChange={(v) => set({ forceData: v === true })}
                    />
                    <span>强制对比数据</span>
                    <span className="text-muted-foreground">默认结构有差异则跳过数据对比</span>
                  </label>
                </div>
              </div>
              {/* 全局规则说明单独成行，不再随选项换行漂移 */}
              <div className="text-xs text-muted-foreground">逐行对比基于两侧公共列；差异样本每侧最多展示 20 行</div>
            </div>

            <div className="p-5">
              <Section
                title={`表级配置 · 别名配对 / 忽略列${configuredAliases.length ? ` · 已配置 ${configuredAliases.length} 项` : ""}`}
                description="源与目标表名不同时指定目标表名（留空按同名匹配）；可为单表设置忽略列，与全局忽略列合并生效"
              >
                {selectedQualifiedTables.length === 0 ? (
                  <div className="text-xs text-muted-foreground">
                    未勾选表（将对比库内全部表）；如需为特定表配置别名/忽略列，请返回上一步勾选。
                  </div>
                ) : (
                  <div className="space-y-2">
                    {/* 工具栏：搜索 + 仅看已配置 + 清空 */}
                    <div className="flex items-center gap-2">
                      <div className="relative flex-1">
                        <Search className="pointer-events-none absolute left-2.5 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
                        <Input
                          className="h-8 pl-8 text-xs"
                          placeholder="搜索表名或别名…"
                          value={aliasQuery}
                          onChange={(e) => setAliasQuery(e.target.value)}
                        />
                      </div>
                      <label className="flex shrink-0 cursor-pointer select-none items-center gap-1.5 text-xs text-muted-foreground">
                        <Checkbox
                          checked={aliasOnlyConfigured}
                          onCheckedChange={(v) => setAliasOnlyConfigured(v === true)}
                        />
                        仅看已配置
                      </label>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-8 shrink-0 text-xs text-muted-foreground"
                        disabled={configuredAliases.length === 0}
                        onClick={() => set({ aliases: [] })}
                      >
                        清空
                      </Button>
                    </div>
                    {/* 限高内滚：表多时无需翻长页；多库时按库分组展示库名 */}
                    <div className="scrollbar-thin max-h-72 space-y-1.5 overflow-y-auto pr-1">
                      {aliasGroups.length === 0 && (
                        <div className="py-4 text-center text-xs text-muted-foreground">无匹配的表</div>
                      )}
                      {aliasGroups.map(([db, tables]) => (
                        <div key={db || "default"} className="space-y-1">
                          {db && (
                            <div className="sticky top-0 z-10 bg-muted/50 px-2 py-1 text-xs font-medium text-muted-foreground">
                              库: {db}
                            </div>
                          )}
                          {tables.map((t) => (
                            <div
                              key={t}
                              className={cn(
                                "flex items-center gap-2 rounded-md border px-2.5 py-1.5",
                                isConfigured(t) ? "border-primary/30 bg-primary/5" : "border-transparent bg-muted/30",
                              )}
                            >
                              {/* 固定宽列 + 图标箭头：多行基线对齐，不再依赖文本字符 */}
                              <span className="w-40 shrink-0 truncate font-mono text-xs" title={t}>
                                {dbOf(t) ? t : bareName(t)}
                              </span>
                              <ArrowRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                              <Input
                                className="h-7 flex-1 text-xs"
                                list="compare-alias-targets"
                                placeholder="目标表名（默认同名）"
                                value={aliasOf(t)}
                                onChange={(e) => setAlias(t, e.target.value)}
                              />
                              <ColumnMultiSelect
                                compact
                                options={toColumnOptions(tableCols[t] || [])}
                                value={aliasEntryOf(t)?.ignoreColumns || []}
                                onChange={(cols) => setTableIgnore(t, cols)}
                                placeholder="选择该表忽略列"
                                loading={colsLoading && !tableCols[t]}
                              />
                              {isConfigured(t) && (
                                <button
                                  type="button"
                                  className="shrink-0 text-muted-foreground hover:text-foreground"
                                  title="清除该表的别名与忽略列"
                                  onClick={() => upsertAlias(t, { target: "", ignoreColumns: [] })}
                                >
                                  <X className="h-3.5 w-3.5" />
                                </button>
                              )}
                            </div>
                          ))}
                        </div>
                      ))}
                    </div>
                    <datalist id="compare-alias-targets">
                      {aliasCandidates.map((t) => <option key={t} value={t} />)}
                    </datalist>
                  </div>
                )}
              </Section>
            </div>
          </Card>
          <WizardFooter
            onBack={() => setStep(1)}
            next={
              <Button onClick={startRun}>
                <Play className="mr-1 h-4 w-4" /> 开始对比
              </Button>
            }
          />
        </div>
      )}

      {step === 3 && runningTaskID && (
        <div className="flex min-h-0 flex-1 flex-col gap-4">
          {taskState === "done" && report ? (
            /* 完成后进度面板压缩为单行状态条，日志可展开，把空间让给报告 */
            <div className="flex items-center gap-3 rounded-lg border border-green-500/30 bg-green-500/10 px-4 py-2 text-sm text-green-800 dark:text-green-300">
              <CheckCircle2 className="h-4 w-4 shrink-0 text-green-600 dark:text-green-400" />
              <span className="font-medium">执行完成</span>
              <span className="text-xs opacity-80">
                共 {report.summary.total} 项 · 一致 {report.summary.matched} · 差异 {report.summary.total - report.summary.matched}
              </span>
              <div className="ml-auto flex items-center gap-1">
                {logs.length > 0 && (
                  <Button variant="ghost" size="sm" className="h-7 text-xs text-green-800 hover:bg-green-100 hover:text-green-900" onClick={() => setShowLogs((v) => !v)}>
                    <ScrollText className="mr-1 h-3.5 w-3.5" /> 执行日志
                    {showLogs ? <ChevronUp className="ml-1 h-3.5 w-3.5" /> : <ChevronDown className="ml-1 h-3.5 w-3.5" />}
                  </Button>
                )}
              </div>
            </div>
          ) : (
            <ProgressView
              taskID={runningTaskID}
              taskType="compare"
              wide
              compactLog
              onSaveTask={() => setSaveOpen(true)}
              onBack={() => {
                setRunningTaskID("")
                setReport(null)
                setStep(0)
              }}
            />
          )}
          {showLogs && logs.length > 0 && (
            <div className="scrollbar-thin max-h-56 overflow-y-auto rounded-md bg-slate-950 p-3 text-slate-200">
              <div className="space-y-0.5 text-xs leading-relaxed">
                {logs.map((l, i) => (
                  <div key={i} className="whitespace-pre-wrap break-all">{l}</div>
                ))}
              </div>
            </div>
          )}
          {report && (
            <CompareReport
              result={report}
              onSaveTask={() => setSaveOpen(true)}
              onRestart={() => {
                setRunningTaskID("")
                setReport(null)
                setStep(0)
              }}
            />
          )}
        </div>
      )}

      </Card>

      <SaveTaskDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        type="compare"
        existingId={taskConfigId}
        buildTask={() => ({ compareOpts: opts })}
        onSaved={(t) => {
          setTaskConfigId(t.id)
          loadSavedTasks()
        }}
      />
    </div>
  )
}

// ==================== 对比报告 ====================

// 对比报告组件已提取至 @/components/CompareReport，供实时对比与快照对比共用
