import { useCallback, useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { toast } from "sonner"
import { MoveRight, Play } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import Hint from "@/components/Hint"
import TaskConfigBar from "@/components/TaskConfigBar"
import WizardFooter from "@/components/WizardFooter"
import * as api from "@/api"
import ConnectionSelect from "@/components/ConnectionSelect"
import PageHeader from "@/components/PageHeader"
import ProgressView from "@/components/ProgressView"
import ResetOptions from "@/components/ResetOptions"
import SaveTaskDialog from "@/components/SaveTaskDialog"
import { CheckRow, Section } from "@/components/Section"
import StepWizard from "@/components/StepWizard"
import TablePicker from "@/components/TablePicker"
import { useAppStore } from "@/stores/app"
import type { MigrateOptions, TaskConfig } from "@/types"

const STEPS = ["选择源和目标库", "选择表和条件", "设置迁移选项", "执行"]

function defaultOptions(): MigrateOptions {
  return {
    sourceConn: "",
    targetConn: "",
    databases: [],
    tables: [],
    objects: [],
    conditions: [],
    schemaOnly: false,
    dataOnly: false,
    resetMode: "",
    backup: true,
    batchSize: 500,
  }
}

// 迁移页：四步向导（支持跨数据库类型：数据以行数据中转，结构自动转换）
export default function MigrateView() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [step, setStep] = useState(0)
  const [opts, setOpts] = useState<MigrateOptions>(defaultOptions())
  const [taskConfigId, setTaskConfigId] = useState<string | undefined>()
  const [runningTaskID, setRunningTaskID] = useState("")
  const [savedTasks, setSavedTasks] = useState<TaskConfig[]>([])
  const [saveOpen, setSaveOpen] = useState(false)
  const connections = useAppStore((s) => s.connections)

  const set = (patch: Partial<MigrateOptions>) => setOpts((o) => ({ ...o, ...patch }))

  // 按主键 id（兼容旧任务配置中的连接名）查找连接
  const findConn = (key: string) => connections.find((c) => c.id === key || c.name === key)

  // 源/目标类型不同时提示跨类型迁移的限制（数据可迁，结构自动转换，触发器/索引等不保证）
  const srcType = findConn(opts.sourceConn)?.conn.Type
  const tgtType = findConn(opts.targetConn)?.conn.Type
  const crossType = !!opts.sourceConn && !!opts.targetConn && !!srcType && !!tgtType && srcType !== tgtType

  // 源连接未配置库时，以选中的第一个库 inline 覆盖源/目标库名（后端仍按单库执行）
  const pickSourceDB = (dbName: string) => {
    const src = findConn(opts.sourceConn)
    const tgt = findConn(opts.targetConn)
    set({
      source: src ? { ...src.conn, DBName: dbName } : opts.source,
      target: tgt && !tgt.conn.DBName ? { ...tgt.conn, DBName: dbName } : opts.target,
    })
  }

  // 库节点多选：勾选/取消级联子项由 TablePicker 处理；此处额外维护源库 inline 覆盖
  const handleDBsChange = (databases: string[]) => {
    set({ databases })
    const src = findConn(opts.sourceConn)
    if (src && !src.conn.DBName && databases.length > 0) pickSourceDB(databases[0])
  }

  // 跨类型迁移不支持对象：切换为跨类型时清空已选对象
  useEffect(() => {
    if (crossType && (opts.objects || []).length > 0) set({ objects: [] })
  }, [crossType]) // eslint-disable-line react-hooks/exhaustive-deps

  const loadSavedTasks = useCallback(async () => {
    try {
      setSavedTasks((await api.listTasks("migrate")) || [])
    } catch { /* ignore */ }
  }, [])

  const applyTask = useCallback((task: TaskConfig) => {
    if (task.migrateOpts) {
      setOpts({ ...defaultOptions(), ...task.migrateOpts })
      setTaskConfigId(task.id)
    }
  }, [])

  // URL 参数消费（task=编辑配置 / running=任务详情）：依赖 searchParams，
  // 保证已挂载页面内点击历史记录跳转（同路由仅参数变化）也能生效
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
      api.getLastTask("migrate").then(({ task }) => task && applyTask(task)).catch(() => {})
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

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
    if ((opts.tables || []).length === 0 && (opts.objects || []).length === 0) {
      toast.error("请至少选择一张表或一个对象")
      setStep(1)
      return
    }
    if (crossType && (opts.objects || []).length > 0) {
      toast.error("跨类型迁移不支持迁移对象，请取消勾选对象")
      setStep(1)
      return
    }
    try {
      const { taskID } = await api.startMigrate(opts, taskConfigId)
      setRunningTaskID(taskID)
      setStep(3)
      setTimeout(() => useAppStore.getState().loadHistory(), 800)
    } catch (e) {
      toast.error(`启动失败: ${(e as Error).message}`)
    }
  }

  return (
    <div className="mx-auto flex h-full max-w-5xl flex-col gap-4">
      <PageHeader
        title="迁移数据"
        description="在两个数据库之间迁移结构与数据，支持跨数据库类型"
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

      {step === 0 && (
        <div className="space-y-4">
          <div className="grid grid-cols-[1fr_auto_1fr] items-start gap-3">
            <ConnectionSelect
              title="源数据库"
              subtitle="从中读取结构与数据"
              value={opts.sourceConn}
              onChange={(name) => set({ sourceConn: name, source: null, databases: [], tables: [], objects: [], conditions: [] })}
            />
            <div className="flex h-11 items-center justify-center self-center">
              <span className="flex h-9 w-9 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm">
                <MoveRight className="h-5 w-5" />
              </span>
            </div>
            <ConnectionSelect
              title="目标数据库"
              subtitle="将数据写入此处"
              value={opts.targetConn}
              onChange={(name) => set({ targetConn: name, target: null })}
            />
          </div>
          {opts.sourceConn && opts.sourceConn === opts.targetConn && (
            <Hint variant="warning">源和目标不能是同一个连接，请重新选择。</Hint>
          )}
          {crossType && (
            <Hint>
              跨类型迁移（{srcType} → {tgtType}）：数据以行数据中转可正常迁移，表结构自动转换；
              触发器为源库方言对象无法自动转换，不会迁移；视图/函数/存储过程也不迁移（如需请使用同类型迁移）。
            </Hint>
          )}
          {!crossType && !!opts.sourceConn && !!opts.targetConn && opts.sourceConn !== opts.targetConn && (
            <Hint>
              同类型迁移：表结构（含触发器）与数据全量迁移，表完成后追加迁移视图/函数/存储过程。
            </Hint>
          )}
          <WizardFooter
            onNext={() => setStep(1)}
            next={
              <Button
                disabled={!opts.sourceConn || !opts.targetConn || opts.sourceConn === opts.targetConn}
                onClick={() => setStep(1)}
              >
                下一步 <MoveRight className="ml-1 h-4 w-4" />
              </Button>
            }
          />
        </div>
      )}

      {step === 1 && (
        <div className="space-y-4">
          <Hint>
            从源库勾选要迁移的库、表与对象（对象仅同类型迁移支持）；勾选库节点将级联选中库下所有表与对象，不选则不迁移。
          </Hint>
          <TablePicker
            connId={opts.sourceConn}
            selected={opts.tables || []}
            selectedObjects={opts.objects || []}
            showObjects={!crossType}
            selectedDBs={opts.databases || []}
            onDBsChange={handleDBsChange}
            conditions={opts.conditions || []}
            onChange={(tables, objects, conditions) => set({ tables, objects, conditions })}
          />
          <WizardFooter onBack={() => setStep(0)} onNext={() => setStep(2)} />
        </div>
      )}

      {step === 2 && (
        <div className="mx-auto max-w-3xl space-y-4">
          <Card className="divide-y p-0">
            <div className="p-5">
              <Section title="迁移内容" description="至少保留一项，默认同时迁移结构与数据">
                <div className="grid gap-2">
                  <CheckRow
                    checked={!opts.dataOnly}
                    onCheckedChange={(v) => {
                      if (!v && opts.dataOnly) return
                      set({ schemaOnly: !v })
                    }}
                    label="迁移结构"
                    description="生成 CREATE TABLE 等 DDL 语句（跨库类型时自动转换方言）"
                  />
                  <CheckRow
                    checked={!opts.schemaOnly}
                    onCheckedChange={(v) => {
                      if (!v && opts.schemaOnly) return
                      set({ dataOnly: !v })
                    }}
                    label="迁移数据"
                    description="生成 INSERT 语句，按批量大小分批写入"
                  />
                </div>
              </Section>
            </div>

            <div className="p-5">
              <ResetOptions
                resetMode={opts.resetMode}
                backup={opts.backup}
                onResetModeChange={(m) => set({ resetMode: m })}
                onBackupChange={(b) => set({ backup: b })}
              />
            </div>

            <div className="p-5">
              <Section title="性能" description="批量大小影响单次 INSERT 的行数，过大可能占用更多内存">
                <Input
                  type="number"
                  className="w-40"
                  value={opts.batchSize || 0}
                  onChange={(e) => set({ batchSize: Number(e.target.value) })}
                />
              </Section>
            </div>
          </Card>
          <WizardFooter
            onBack={() => setStep(1)}
            next={
              <Button onClick={startRun}>
                <Play className="mr-1 h-4 w-4" /> 开始迁移
              </Button>
            }
          />
        </div>
      )}

      {step === 3 && runningTaskID && (
        <ProgressView
          taskID={runningTaskID}
          taskType="migrate"
          onSaveTask={() => setSaveOpen(true)}
          onBack={() => {
            setRunningTaskID("")
            setStep(0)
          }}
        />
      )}

      <SaveTaskDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        type="migrate"
        existingId={taskConfigId}
        buildTask={() => ({ migrateOpts: opts })}
        onSaved={(t) => {
          setTaskConfigId(t.id)
          loadSavedTasks()
        }}
      />
    </div>
  )
}
