import { useCallback, useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { toast } from "sonner"
import { Play } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import Hint from "@/components/Hint"
import TaskConfigBar from "@/components/TaskConfigBar"
import WizardFooter from "@/components/WizardFooter"
import * as api from "@/api"
import { CONN_SINGLE_W } from "@/components/ConnectionPair"
import ConnectionSelect from "@/components/ConnectionSelect"
import PageHeader from "@/components/PageHeader"
import ProgressView from "@/components/ProgressView"
import SaveTaskDialog from "@/components/SaveTaskDialog"
import { CheckRow, Section } from "@/components/Section"
import StepWizard from "@/components/StepWizard"
import TablePicker from "@/components/TablePicker"
import { useAppStore } from "@/stores/app"
import type { ExportOptions, TaskConfig } from "@/types"

const STEPS = ["选择源数据库", "选择表和条件", "设置导出选项", "执行"]

function defaultOptions(): ExportOptions {
  return {
    sourceConn: "",
    outputDir: "",
    taskName: "",
    databases: [],
    tables: [],
    objects: [],
    conditions: [],
    schemaOnly: false,
    dataOnly: false,
    batchSize: 500,
    compress: true,
  }
}

// 导出页：四步向导
export default function ExportView() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [step, setStep] = useState(0)
  const [opts, setOpts] = useState<ExportOptions>(defaultOptions())
  const [taskConfigId, setTaskConfigId] = useState<string | undefined>()
  const [runningTaskID, setRunningTaskID] = useState("")
  const [savedTasks, setSavedTasks] = useState<TaskConfig[]>([])
  const [saveOpen, setSaveOpen] = useState(false)

  const set = (patch: Partial<ExportOptions>) => setOpts((o) => ({ ...o, ...patch }))

  const loadSavedTasks = useCallback(async () => {
    try {
      setSavedTasks((await api.listTasks("export")) || [])
    } catch { /* ignore */ }
  }, [])

  const applyTask = useCallback((task: TaskConfig) => {
    if (task.exportOpts) {
      setOpts({ ...defaultOptions(), ...task.exportOpts })
      setTaskConfigId(task.id)
    }
  }, [])

  // URL 参数消费（task=编辑配置 / running=进行中任务）：依赖 searchParams，
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
      api.getLastTask("export").then(({ task }) => task && applyTask(task)).catch(() => {})
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const startRun = async () => {
    if (!opts.sourceConn) {
      toast.error("请先选择源数据库连接")
      setStep(0)
      return
    }
    if ((opts.tables || []).length === 0 && (opts.objects || []).length === 0) {
      toast.error("请至少选择一张表或一个对象")
      setStep(1)
      return
    }
    try {
      const { taskID } = await api.startExport(opts, taskConfigId)
      setRunningTaskID(taskID)
      setStep(3)
      refreshHistory()
    } catch (e) {
      toast.error(`启动失败: ${(e as Error).message}`)
    }
  }

  const resetWizard = () => {
    setRunningTaskID("")
    setStep(0)
  }

  return (
    <div className="mx-auto flex h-full max-w-5xl flex-col gap-4">
      <PageHeader
        title="导出数据"
        description="将数据库结构及数据导出为 SQL 文件（可压缩为 zip）"
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

      <StepWizard steps={STEPS} current={step} onStepClick={(i) => runningTaskID ? undefined : setStep(i)} />

      {/* 单卡宽度与迁移/对比页双卡布局中的卡片同宽（CONN_SINGLE_W），各任务页卡片尺寸统一 */}
      {step === 0 && (
        <div className={`mx-auto w-full space-y-4 ${CONN_SINGLE_W}`}>
          <ConnectionSelect
            title="源数据库"
            subtitle="选择要导出的数据库连接"
            value={opts.sourceConn}
            onChange={(name) => set({ sourceConn: name })}
          />
          {/* 提示位置与其他任务页 step0 保持一致：连接卡下方、操作按钮上方 */}
          <Hint>导出的 SQL 为源库方言格式，之后只能导入到同类型数据库（如 MySQL 导出只能导入 MySQL）。</Hint>
          <WizardFooter onNext={() => setStep(1)} />
        </div>
      )}

      {step === 1 && (
        <div className="space-y-4">
          <Hint>
            勾选要导出的库、表与对象（视图/函数/存储过程）；不选择则不导出，可使用「全选」快捷操作。可为单张表设置数据导出条件（完整 SQL）与导出模式。
          </Hint>
          <TablePicker
            connId={opts.sourceConn}
            selected={opts.tables || []}
            selectedObjects={opts.objects || []}
            selectedDBs={opts.databases || []}
            onDBsChange={(databases) => set({ databases })}
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
              <Section title="导出内容" description="至少保留一项，默认同时导出结构与数据">
                <div className="grid gap-2">
                  <CheckRow
                    checked={!opts.dataOnly}
                    onCheckedChange={(v) => {
                      if (!v && opts.dataOnly) return // 至少保留一项
                      set({ schemaOnly: !v })
                    }}
                    label="导出结构"
                    description="生成 CREATE TABLE 等 DDL（含触发器），并导出视图/函数/存储过程"
                  />
                  <CheckRow
                    checked={!opts.schemaOnly}
                    onCheckedChange={(v) => {
                      if (!v && opts.schemaOnly) return // 至少保留一项
                      set({ dataOnly: !v })
                    }}
                    label="导出数据"
                    description="生成 INSERT 语句，按批量大小分批写入"
                  />
                </div>
              </Section>
            </div>

            <div className="p-5">
              <Section title="输出设置">
                <div className="grid gap-2">
                  <CheckRow
                    checked={opts.compress}
                    onCheckedChange={(v) => set({ compress: v })}
                    label="压缩为 zip"
                    description="取消则保留目录结构输出"
                  />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-4">
                  <div className="space-y-1">
                    <Label>任务名称</Label>
                    <Input
                      value={opts.taskName}
                      onChange={(e) => set({ taskName: e.target.value })}
                      placeholder="用于生成文件名，如：daily_backup"
                    />
                  </div>
                  <div className="space-y-1">
                    <Label>输出目录（可选）</Label>
                    <Input
                      value={opts.outputDir}
                      onChange={(e) => set({ outputDir: e.target.value })}
                      placeholder="留空使用默认（程序目录下的 .dbimpex-exports）"
                    />
                  </div>
                </div>
              </Section>
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
                <Play className="mr-1 h-4 w-4" /> 开始导出
              </Button>
            }
          />
        </div>
      )}

      {step === 3 && runningTaskID && (
        <ProgressView
          taskID={runningTaskID}
          taskType="export"
          onSaveTask={() => setSaveOpen(true)}
          onBack={resetWizard}
        />
      )}

      <SaveTaskDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        type="export"
        existingId={taskConfigId}
        buildTask={() => ({ exportOpts: opts })}
        onSaved={(t) => {
          setTaskConfigId(t.id)
          loadSavedTasks()
        }}
      />
    </div>
  )
}

// 刷新右侧面板执行历史（延迟执行等待后端写入）
function refreshHistory() {
  setTimeout(() => useAppStore.getState().loadHistory(), 800)
}
