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
import type { DictionaryOptions, TaskConfig } from "@/types"

const STEPS = ["选择源数据库", "选择库表", "开始执行"]

function defaultOptions(): DictionaryOptions {
  return {
    sourceConn: "",
    outputDir: "",
    taskName: "",
    databases: [],
    tables: [],
    compress: true,
  }
}

export default function DictionaryView() {
  const [searchParams, setSearchParams] = useSearchParams()
  const cachedTask = useAppStore((s) => s.lastTasks["dictionary"])
  const setLastTask = useAppStore((s) => s.setLastTask)
  const clearLastTask = useAppStore((s) => s.clearLastTask)
  const [step, setStep] = useState(0)
  const [opts, setOpts] = useState<DictionaryOptions>(() =>
    cachedTask?.dictionaryOpts ? { ...defaultOptions(), ...cachedTask.dictionaryOpts } : defaultOptions())
  const [taskConfigId, setTaskConfigId] = useState<string | undefined>(cachedTask?.id)
  const [runningTaskID, setRunningTaskID] = useState("")
  const [savedTasks, setSavedTasks] = useState<TaskConfig[]>([])
  const [saveOpen, setSaveOpen] = useState(false)

  const set = (patch: Partial<DictionaryOptions>) => setOpts((o) => ({ ...o, ...patch }))

  const loadSavedTasks = useCallback(async () => {
    try {
      setSavedTasks((await api.listTasks("dictionary")) || [])
    } catch { /* ignore */ }
  }, [])

  const applyTask = useCallback((task: TaskConfig) => {
    if (task.dictionaryOpts) {
      setOpts({ ...defaultOptions(), ...task.dictionaryOpts })
      setTaskConfigId(task.id)
      setLastTask(task)
    }
  }, [setLastTask])

  // URL 参数消费
  useEffect(() => {
    const taskParam = searchParams.get("task")
    const runningParam = searchParams.get("running")
    if (runningParam) {
      setRunningTaskID(runningParam)
      setStep(2)
      setSearchParams({}, { replace: true })
    } else if (taskParam) {
      api.getTask(taskParam).then(applyTask).catch((e: Error) => toast.error(e.message))
      setSearchParams({}, { replace: true })
    }
  }, [searchParams]) // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    loadSavedTasks()
    if (searchParams.get("task") || searchParams.get("running") || cachedTask) return
    api.getLastTask("dictionary").then(({ task }) => task && applyTask(task)).catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const startRun = async () => {
    if (!opts.sourceConn) {
      toast.error("请先选择源数据库连接")
      setStep(0)
      return
    }
    if ((opts.tables || []).length === 0) {
      toast.error("请至少选择一张表")
      setStep(1)
      return
    }
    try {
      const { taskID } = await api.startDictionary(opts, taskConfigId)
      setRunningTaskID(taskID)
      setStep(2)
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
        title="数据字典"
        description="导出数据库表结构及注释为 Excel 文档（总览 + 每库字段明细）"
        actions={
          <TaskConfigBar
            savedTasks={savedTasks}
            taskConfigId={taskConfigId}
            onApply={applyTask}
            onClear={() => { setOpts(defaultOptions()); setTaskConfigId(undefined); clearLastTask("dictionary") }}
            onSave={() => setSaveOpen(true)}
          />
        }
      />

      <StepWizard steps={STEPS} current={step} onStepClick={(i) => runningTaskID ? undefined : setStep(i)} />

      {step === 0 && (
        <div className={`mx-auto w-full space-y-4 ${CONN_SINGLE_W}`}>
          <ConnectionSelect
            title="源数据库"
            subtitle="选择要导出数据字典的数据库连接"
            value={opts.sourceConn}
            onChange={(name) => set({ sourceConn: name })}
          />
          <Hint>产物为单个 Excel 文档（.xlsx），包含总览页及各库字段明细，附注释信息。</Hint>
          <WizardFooter onNext={() => setStep(1)} />
        </div>
      )}

      {step === 1 && (
        <div className="space-y-4">
          <Hint>
            勾选要纳入字典的库与表；字典仅覆盖表结构（不含视图/函数/存储过程），不统计行数。
          </Hint>
          <TablePicker
            connId={opts.sourceConn}
            selected={opts.tables || []}
            selectedObjects={[]}
            selectedDBs={opts.databases || []}
            onDBsChange={(databases) => set({ databases })}
            conditions={[]}
            showObjects={false}
            onChange={(tables) => set({ tables })}
          />
          <WizardFooter onBack={() => setStep(0)} onNext={() => setStep(2)} />
        </div>
      )}

      {step === 2 && !runningTaskID && (
        <div className="mx-auto max-w-3xl space-y-4">
          <Card className="divide-y p-0">
            <div className="p-5">
              <Section title="输出设置">
                <div className="grid gap-2">
                  <CheckRow
                    checked={opts.compress}
                    onCheckedChange={(v) => set({ compress: v })}
                    label="压缩为 zip"
                    description="将生成的 xlsx 打包为 zip 文件"
                  />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-4">
                  <div className="space-y-1">
                    <Label>任务名称</Label>
                    <Input
                      value={opts.taskName}
                      onChange={(e) => set({ taskName: e.target.value })}
                      placeholder="用于生成文件名，如：dict_prod"
                    />
                  </div>
                  <div className="space-y-1">
                    <Label>输出目录（可选）</Label>
                    <Input
                      value={opts.outputDir}
                      onChange={(e) => set({ outputDir: e.target.value })}
                      placeholder="留空使用默认目录"
                    />
                  </div>
                </div>
              </Section>
            </div>
          </Card>
          <WizardFooter
            onBack={() => setStep(1)}
            next={
              <Button onClick={startRun}>
                <Play className="mr-1 h-4 w-4" /> 开始生成
              </Button>
            }
          />
        </div>
      )}

      {step === 2 && runningTaskID && (
        <ProgressView
          taskID={runningTaskID}
          taskType="dictionary"
          onSaveTask={() => setSaveOpen(true)}
          onBack={resetWizard}
        />
      )}

      <SaveTaskDialog
        open={saveOpen}
        onOpenChange={setSaveOpen}
        type="dictionary"
        existingId={taskConfigId}
        buildTask={() => ({ dictionaryOpts: opts })}
        onSaved={(t) => {
          setTaskConfigId(t.id)
          loadSavedTasks()
        }}
      />
    </div>
  )
}

function refreshHistory() {
  setTimeout(() => useAppStore.getState().loadHistory(), 800)
}
