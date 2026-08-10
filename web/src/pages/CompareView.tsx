import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { toast } from "sonner"
import { ArrowRight, CheckCircle2, ChevronDown, ChevronRight, ChevronUp, MoveRight, Play, RotateCcw, Save, ScrollText, Search, X } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
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
import { useAppStore } from "@/stores/app"
import { cn } from "@/lib/utils"
import type {
  ChangedRow,
  CompareColumnItem,
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

// "库.表" 限定名剥离库前缀（对比按裸表名匹配）
const bareName = (t: string) => (t.includes(".") ? t.slice(t.indexOf(".") + 1) : t)

// 对比页：四步向导（作用域为单个库对，支持表别名配对）
export default function CompareView() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [step, setStep] = useState(0)
  const [opts, setOpts] = useState<CompareOptions>(defaultOptions())
  const [taskConfigId, setTaskConfigId] = useState<string | undefined>()
  const [runningTaskID, setRunningTaskID] = useState("")
  const [savedTasks, setSavedTasks] = useState<TaskConfig[]>([])
  const [saveOpen, setSaveOpen] = useState(false)
  const [taskState, setTaskState] = useState("running")
  const [report, setReport] = useState<CompareResult | null>(null)
  const [targetTables, setTargetTables] = useState<string[]>([])
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

  // 目标库当前作用域名：inline 覆盖优先，其次连接配置的库
  const targetDB = opts.target?.DBName || findConn(opts.targetConn)?.conn.DBName || ""

  // 源库作用域（单库对）：inline 覆盖优先，其次连接配置的库，再退到选中库
  const sourceDB = opts.source?.DBName || findConn(opts.sourceConn)?.conn.DBName || (opts.databases || [])[0] || ""

  // 源连接未配置库时，以选中的第一个库 inline 覆盖源/目标库名（对比作用域为单个库对）
  const pickSourceDB = (dbName: string) => {
    const src = findConn(opts.sourceConn)
    const tgt = findConn(opts.targetConn)
    set({
      source: src ? { ...src.conn, DBName: dbName } : opts.source,
      target: tgt && !tgt.conn.DBName ? { ...tgt.conn, DBName: dbName } : opts.target,
    })
  }

  const handleDBsChange = (databases: string[]) => {
    set({ databases })
    const src = findConn(opts.sourceConn)
    if (src && !src.conn.DBName && databases.length > 0) pickSourceDB(databases[0])
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
    }
  }, [])

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
    // 无 URL 参数时才回填上次配置，避免覆盖参数恢复的状态
    if (!searchParams.get("task") && !searchParams.get("running")) {
      api.getLastTask("compare").then(({ task }) => task && applyTask(task)).catch(() => {})
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // 进入选项步骤时加载目标库表清单（别名配置辅助输入）
  useEffect(() => {
    if (step !== 2 || !opts.targetConn) return
    api
      .getTableTree(opts.targetConn, targetDB || undefined)
      .then(({ databases }) => setTargetTables(databases.flatMap((d) => d.tables)))
      .catch(() => setTargetTables([]))
  }, [step, opts.targetConn, targetDB])

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
      // 未勾选表 = 对比库内全部表（后端 nil=全部）
      const payload: CompareOptions = {
        ...opts,
        tables: (opts.tables || []).length > 0 ? opts.tables : undefined,
        ignoreColumns: (opts.ignoreColumns || []).length > 0 ? opts.ignoreColumns : undefined,
        forceData: opts.forceData || undefined,
      }
      const { taskID } = await api.startCompare(payload, taskConfigId)
      setRunningTaskID(taskID)
      setStep(3)
      setTimeout(() => useAppStore.getState().loadHistory(), 800)
    } catch (e) {
      toast.error(`启动失败: ${(e as Error).message}`)
    }
  }

  const selectedBareTables = (opts.tables || []).map(bareName)

  // 进入选项步骤时拉取选中表的列信息（忽略列下拉候选）；
  // 首次进入时默认回填时间类列（创建/更新时间），全局忽略同步预选
  const selectedTablesKey = selectedBareTables.join(",")
  useEffect(() => {
    if (step !== 2 || !opts.sourceConn || !sourceDB || !selectedTablesKey) return
    let cancelled = false
    setColsLoading(true)
    Promise.all(
      selectedBareTables.map((t) =>
        api
          .getTableColumns(opts.sourceConn, sourceDB, t)
          .then((r) => r.columns || [])
          .catch(() => [] as TableColumn[]),
      ),
    )
      .then((colsArr) => {
        if (cancelled) return
        const map: Record<string, TableColumn[]> = {}
        selectedBareTables.forEach((t, i) => {
          map[t] = colsArr[i]
        })
        setTableCols(map)
        if (!filledDefaults.current) {
          filledDefaults.current = true
          setOpts((prev) => {
            const aliases = [...(prev.aliases || [])]
            selectedBareTables.forEach((t, i) => {
              const times = colsArr[i].filter(isTimeColumn).map((c) => c.name)
              if (times.length === 0) return
              const idx = aliases.findIndex((a) => a.source === t)
              if (idx >= 0) {
                if ((aliases[idx].ignoreColumns || []).length === 0) {
                  aliases[idx] = { ...aliases[idx], ignoreColumns: times }
                }
              } else {
                aliases.push({ source: t, target: "", ignoreColumns: times })
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
  // 单表的表级配置条目（目标表名 + 忽略列）
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
  // 表级配置列表过滤：搜索关键词 + 仅看已配置；表多时无需翻长列表
  const aliasList = selectedBareTables.filter(
    (t) =>
      (!aliasOnlyConfigured || isConfigured(t)) &&
      (!aliasQuery.trim() ||
        t.toLowerCase().includes(aliasQuery.trim().toLowerCase()) ||
        aliasOf(t).toLowerCase().includes(aliasQuery.trim().toLowerCase())),
  )
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
            onClear={() => { setOpts(defaultOptions()); setTaskConfigId(undefined) }}
            onSave={() => setSaveOpen(true)}
          />
        }
      />

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
              对比范围为单个库对；表按名称匹配，不同名的同义表可在下一步配置别名配对。
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
            extraDb={targetDB || undefined}
            selected={opts.tables || []}
            showObjects={false}
            selectedDBs={opts.databases || []}
            onDBsChange={handleDBsChange}
            conditions={[]}
            onChange={(tables) => set({ tables })}
          />
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
                {selectedBareTables.length === 0 ? (
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
                    {/* 限高内滚：表多时无需翻长页 */}
                    <div className="scrollbar-thin max-h-72 space-y-1.5 overflow-y-auto pr-1">
                      {aliasList.length === 0 && (
                        <div className="py-4 text-center text-xs text-muted-foreground">无匹配的表</div>
                      )}
                      {aliasList.map((t) => (
                        <div
                          key={t}
                          className={cn(
                            "flex items-center gap-2 rounded-md border px-2.5 py-1.5",
                            isConfigured(t) ? "border-primary/30 bg-primary/5" : "border-transparent bg-muted/30",
                          )}
                        >
                          {/* 固定宽列 + 图标箭头：多行基线对齐，不再依赖文本字符 */}
                          <span className="w-40 shrink-0 truncate font-mono text-xs" title={t}>{t}</span>
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
            <div className="flex items-center gap-3 rounded-lg border border-green-200 bg-green-50 px-4 py-2 text-sm text-green-800">
              <CheckCircle2 className="h-4 w-4 shrink-0 text-green-600" />
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

// 汇总过滤项：数值颜色与差异语义对齐，点击切换过滤
const FILTERS = [
  { key: "", label: "全部", cls: "" },
  { key: "matched", label: "一致", cls: "text-green-600" },
  { key: "source_only", label: "仅源有", cls: "text-amber-600" },
  { key: "target_only", label: "仅目标有", cls: "text-blue-600" },
  { key: "structure", label: "结构差异", cls: "text-red-600" },
  { key: "data", label: "数据差异", cls: "text-red-600" },
]

function matchesFilter(t: CompareTableResult, f: string): boolean {
  if (f === "") return true
  if (f === "source_only") return t.status === "source_only"
  if (f === "target_only") return t.status === "target_only"
  if (t.status !== "both") return false
  if (f === "matched") return (t.columns?.matched ?? true) && (t.data?.equal ?? true)
  if (f === "structure") return !!t.columns && !t.columns.matched
  // 跳过类（结构不一致等）不计入数据差异，与后端汇总口径一致
  if (f === "data") return !!t.data && !t.data.equal && t.data.mode !== "skipped"
  return true
}

function fmtVal(v: unknown): string {
  if (v === null || v === undefined) return "NULL"
  if (typeof v === "object") return JSON.stringify(v)
  return String(v)
}

// 单元格预览：XML/BLOB 类大字段截断展示，避免采样表被超长内容撑坏
function cellPreview(v: unknown): string {
  const s = fmtVal(v)
  return s.length > 160 ? `${s.slice(0, 160)}…` : s
}

function colDesc(c: CompareColumnItem): string {
  return `${c.dataType}${c.primaryKey ? " · 主键" : ""}${c.nullable ? "" : " · 非空"}`
}

// 数据差异摘要：省略零值项，避免“缺失19行/多出0行”这类冗余信息
function tableDataDesc(d: NonNullable<CompareTableResult["data"]>): string {
  if (d.mode === "count") return `行数 ${d.sourceRows} vs ${d.targetRows}`
  if (d.skippedReason) return d.skippedReason
  if (d.equal) return `数据一致 (${d.sourceRows}行)`
  const parts: string[] = []
  if (d.missing) parts.push(`缺失${d.missing}行`)
  if (d.extra) parts.push(`多出${d.extra}行`)
  if (d.changed) parts.push(`变化${d.changed}行`)
  return parts.join(" / ") || "有差异"
}

// 行明细采样表格（缺失/多出各最多 20 条）；colOrder 按源表列定义顺序渲染
function SampleTable({ title, rows, colOrder }: { title: string; rows?: Record<string, unknown>[]; colOrder?: string[] }) {
  if (!rows || rows.length === 0) return null
  // 优先按后端给出的列序渲染；兼容旧数据回退到首行 key 顺序
  const cols = colOrder && colOrder.length > 0 ? colOrder.filter((c) => c in rows[0]) : Object.keys(rows[0])
  return (
    <div className="min-w-0">
      <div className="mb-1 text-xs font-medium text-muted-foreground">
        {title}（{rows.length} 条）
      </div>
      {/* 列宽按内容自适应：短列窄、长文本列封顶截断；w-max+min-w-full 兼顾少列占满与多列横向滚动 */}
      <div className="scrollbar-thin max-h-80 overflow-auto rounded-md border">
        <table className="w-max min-w-full text-xs">
          <thead className="sticky top-0 bg-muted">
            <tr>
              {cols.map((c) => (
                <th key={c} className="whitespace-nowrap px-2 py-1 text-left font-medium">{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => (
              <tr key={i} className="border-t">
                {cols.map((c) => (
                  <td key={c} className="max-w-52 truncate px-2 py-1" title={cellPreview(r[c])}>
                    {cellPreview(r[c])}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// 变化行采样表格（PK 模式）：主键取值 + 差异列源/目标对照
function ChangedTable({ rows }: { rows?: ChangedRow[] }) {
  if (!rows || rows.length === 0) return null
  return (
    <div className="min-w-0">
      <div className="mb-1 text-xs font-medium text-muted-foreground">
        主键匹配但内容不同（变化）（{rows.length} 条）
      </div>
      <div className="scrollbar-thin max-h-80 overflow-auto rounded-md border">
        <table className="w-max min-w-full text-xs">
          <thead className="sticky top-0 bg-muted">
            <tr>
              <th className="whitespace-nowrap px-2 py-1 text-left font-medium">主键</th>
              <th className="whitespace-nowrap px-2 py-1 text-left font-medium">差异列</th>
              <th className="whitespace-nowrap px-2 py-1 text-left font-medium">源</th>
              <th className="whitespace-nowrap px-2 py-1 text-left font-medium">目标</th>
            </tr>
          </thead>
          <tbody>
            {rows.flatMap((r, i) =>
              r.diffs.map((d, j) => (
                <tr key={`${i}-${j}`} className="border-t">
                  {j === 0 && (
                    <td rowSpan={r.diffs.length} className="max-w-52 truncate px-2 py-1 align-top font-mono" title={Object.entries(r.key).map(([k, v]) => `${k}=${v}`).join("  ")}>
                      {Object.entries(r.key).map(([k, v]) => `${k}=${cellPreview(v)}`).join("  ")}
                    </td>
                  )}
                  <td className="whitespace-nowrap px-2 py-1 font-mono">{d.column}</td>
                  <td className="max-w-52 truncate px-2 py-1" title={cellPreview(d.source)}>{cellPreview(d.source)}</td>
                  <td className="max-w-52 truncate px-2 py-1" title={cellPreview(d.target)}>{cellPreview(d.target)}</td>
                </tr>
              )),
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// 行状态徽章：列表行与明细弹窗标题共用
function statusBadgeOf(t: CompareTableResult) {
  if (t.status === "source_only") return <Badge variant="secondary" className="bg-amber-50 text-amber-700">仅源有</Badge>
  if (t.status === "target_only") return <Badge variant="secondary" className="bg-blue-50 text-blue-700">仅目标有</Badge>
  if ((t.columns?.matched ?? true) && (t.data?.equal ?? true)) return <Badge variant="secondary" className="bg-green-50 text-green-700">一致</Badge>
  return <Badge variant="secondary" className="bg-red-50 text-red-700">有差异</Badge>
}

// 表差异明细：弹窗内容，列级差异 + 缺失/多出采样行，内部滚动
function TableDiffDetail({ t }: { t: CompareTableResult }) {
  // 采样表分列：仅当两侧都有数据时才双列，否则单表占满弹窗宽度
  const hasMissing = !!(t.data?.missingSamples && t.data.missingSamples.length > 0)
  const hasExtra = !!(t.data?.extraSamples && t.data.extraSamples.length > 0)
  return (
    <div className="scrollbar-thin max-h-[72vh] space-y-3 overflow-y-auto pr-1">
      {t.columns && !t.columns.matched && (
        <div className="space-y-2">
          {t.columns.sourceOnly.length > 0 && (
            <div className="text-xs">
              <span className="font-medium text-amber-700">源有目标无：</span>
              {t.columns.sourceOnly.map((c) => (
                <span key={c.name} className="ml-2 font-mono">{c.name} <span className="text-muted-foreground">({colDesc(c)})</span></span>
              ))}
            </div>
          )}
          {t.columns.targetOnly.length > 0 && (
            <div className="text-xs">
              <span className="font-medium text-blue-700">目标多出：</span>
              {t.columns.targetOnly.map((c) => (
                <span key={c.name} className="ml-2 font-mono">{c.name} <span className="text-muted-foreground">({colDesc(c)})</span></span>
              ))}
            </div>
          )}
          {t.columns.different.length > 0 && (
            <div className="scrollbar-thin overflow-auto rounded-md border">
              <table className="w-full text-xs">
                <thead className="bg-muted">
                  <tr>
                    <th className="px-2 py-1 text-left font-medium">列名</th>
                    <th className="px-2 py-1 text-left font-medium">源</th>
                    <th className="px-2 py-1 text-left font-medium">目标</th>
                  </tr>
                </thead>
                <tbody>
                  {t.columns.different.map((d) => (
                    <tr key={d.name} className="border-t">
                      <td className="px-2 py-1 font-mono">{d.name}</td>
                      <td className="px-2 py-1 font-mono text-muted-foreground">{colDesc(d.source)}</td>
                      <td className="px-2 py-1 font-mono text-muted-foreground">{colDesc(d.target)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {t.data && !t.data.equal && (
        <div className="space-y-3">
          {t.data.skippedReason && (
            <div className="text-xs text-muted-foreground">{t.data.skippedReason}</div>
          )}
          {t.data.mode === "rows" && (
            <div className="text-xs text-muted-foreground">
              {t.data.keyColumns && t.data.keyColumns.length > 0
                ? `按主键 ${t.data.keyColumns.join(",")} 判断有无，内容对比判断变化`
                : "无主键，整行对比（变化会表现为缺失+多出）"}
            </div>
          )}
          {t.data.mode === "rows" && (
            <div className={cn("grid gap-3", hasMissing && hasExtra && "lg:grid-cols-2")}>
              <SampleTable title="源有目标无（缺失）" rows={t.data.missingSamples} colOrder={t.data.sampleColumns} />
              <SampleTable title="目标有源无（多出）" rows={t.data.extraSamples} colOrder={t.data.sampleColumns} />
            </div>
          )}
          {t.data.mode === "rows" && <ChangedTable rows={t.data.changedSamples} />}
        </div>
      )}
    </div>
  )
}

// 对比报告：汇总统计 + 表级结果列表（限高内滚，差异明细弹窗查看）
function CompareReport({ result, onSaveTask, onRestart }: { result: CompareResult; onSaveTask?: () => void; onRestart?: () => void }) {
  const [filter, setFilter] = useState("")
  const [showMatched, setShowMatched] = useState(false)
  const [detail, setDetail] = useState<CompareTableResult | null>(null)
  const s = result.summary

  const counts: Record<string, number> = {
    "": s.total,
    matched: s.matched,
    source_only: s.sourceOnly,
    target_only: s.targetOnly,
    structure: s.structureDiff,
    data: s.dataDiff,
  }

  const toggleDetail = (t: CompareTableResult) => setDetail(t)

  const tables = result.tables.filter((t) => {
    if (!matchesFilter(t, filter)) return false
    // 非过滤模式下默认隐藏完全一致的表（表多时减少无信息量行）
    if (filter === "" && !showMatched && t.status === "both" && (t.columns?.matched ?? true) && (t.data?.equal ?? true)) return false
    return true
  })

  return (
    <Card className="space-y-3 p-4">
      <div className="flex flex-wrap items-center gap-2">
        <div className="text-sm font-medium">对比报告</div>
        <span className="text-xs text-muted-foreground">
          {result.source} ↔ {result.target} · 对比时点快照，期间数据变动可能影响结果
        </span>
        <div className="ml-auto flex items-center gap-2">
          {onSaveTask && (
            <Button variant="outline" size="sm" className="h-7 text-xs" onClick={onSaveTask}>
              <Save className="mr-1 h-3.5 w-3.5" /> 保存为任务配置
            </Button>
          )}
          {onRestart && (
            <Button size="sm" className="h-7 text-xs" onClick={onRestart}>
              <RotateCcw className="mr-1 h-3.5 w-3.5" /> 重新开始
            </Button>
          )}
        </div>
      </div>

      {/* 汇总统计卡片：与进度页 StatBlock 风格对齐，点击切换过滤；零计数灰化 */}
      <div className="grid grid-cols-3 gap-2 md:grid-cols-6">
        {FILTERS.map(({ key, label, cls }) => (
          <button
            key={key}
            type="button"
            className={cn(
              "rounded-md border px-2.5 py-2 text-left transition-colors",
              filter === key ? "border-primary bg-primary/10" : "bg-muted/30 hover:bg-accent",
              counts[key] === 0 && filter !== key && "opacity-50",
            )}
            onClick={() => setFilter(key)}
          >
            <div className="truncate text-xs text-muted-foreground">{label}</div>
            <div className={cn("mt-0.5 text-base font-medium tabular-nums", cls)}>
              {counts[key]}
            </div>
          </button>
        ))}
      </div>

      {/* 一致项显示开关：默认只看差异，表多时列表更短 */}
      <div className="flex items-center justify-between">
        <span className="text-xs text-muted-foreground">
          {filter === "" && !showMatched
            ? `仅显示有差异的表（${tables.length}）；一致 ${s.matched} 项已隐藏`
            : `共 ${tables.length} 项`}
        </span>
        <label className="flex cursor-pointer select-none items-center gap-1.5 text-xs text-muted-foreground">
          <Checkbox checked={showMatched} onCheckedChange={(v) => setShowMatched(v === true)} />
          显示一致项
        </label>
      </div>

      {tables.length === 0 && (
        <div className="py-6 text-center text-xs text-muted-foreground">无符合条件的表</div>
      )}

      {/* 表列表限高内滚，避免页面被撑长 */}
      <div className="scrollbar-thin max-h-[520px] space-y-1.5 overflow-y-auto pr-1">
        {tables.map((t) => {
          const hasDetail =
            t.status === "both" &&
            ((t.columns && !t.columns.matched) || (t.data && !t.data.equal))
          return (
            <div key={t.name} className="rounded-md border bg-background">
              {/* 摘要行：有差异的行点击查看弹窗明细 */}
              <button
                type="button"
                className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm"
                onClick={() => hasDetail && toggleDetail(t)}
              >
                {hasDetail ? (
                  <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                ) : (
                  <span className="w-3.5 shrink-0" />
                )}
                <span className="min-w-0 truncate font-mono text-xs" title={t.name}>{t.name}</span>
                {statusBadgeOf(t)}
                <span className="ml-auto flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
                  {t.columns && (
                    <span>
                      {t.columns.matched
                        ? "结构一致"
                        : `结构: +${t.columns.sourceOnly.length} -${t.columns.targetOnly.length} ±${t.columns.different.length}`}
                    </span>
                  )}
                  {t.data && (
                    <span className="tabular-nums">{tableDataDesc(t.data)}</span>
                  )}
                </span>
              </button>
            </div>
          )
        })}
      </div>

      {/* 差异明细弹窗：宽幅展示，采样表与列差异可容纳更多数据 */}
      <Dialog open={!!detail} onOpenChange={(o) => !o && setDetail(null)}>
        <DialogContent className="max-w-5xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 font-mono text-base">
              {detail?.name}
              {detail && statusBadgeOf(detail)}
            </DialogTitle>
            {detail && (
              <DialogDescription>
                {[
                  detail.columns
                    ? detail.columns.matched
                      ? "结构一致"
                      : `结构差异 +${detail.columns.sourceOnly.length} -${detail.columns.targetOnly.length} ±${detail.columns.different.length}`
                    : "",
                  detail.data ? tableDataDesc(detail.data) : "",
                ].filter(Boolean).join(" · ")}
              </DialogDescription>
            )}
          </DialogHeader>
          {detail && <TableDiffDetail t={detail} />}
        </DialogContent>
      </Dialog>
    </Card>
  )
}
