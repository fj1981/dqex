import { useCallback, useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
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
import { tKey } from "@/lib/i18n"
import { useAppStore } from "@/stores/app"
import type { ExportOptions, TaskConfig } from "@/types"

const STEPS = ["export.step1", "export.step2", "export.step3", "export.step4"]

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
    singleTransaction: true,
    gzip: false,
    compatCollation: false,
  }
}

// 导出页：四步向导
export default function ExportView() {
  const { t } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()
  // 会话内缓存的最近应用配置：挂载时同步初始化，避免空配置闪现后再回填
  const cachedTask = useAppStore((s) => s.lastTasks["export"])
  const setLastTask = useAppStore((s) => s.setLastTask)
  const clearLastTask = useAppStore((s) => s.clearLastTask)
  const [step, setStep] = useState(0)
  const [opts, setOpts] = useState<ExportOptions>(() =>
    cachedTask?.exportOpts ? { ...defaultOptions(), ...cachedTask.exportOpts } : defaultOptions())
  const [taskConfigId, setTaskConfigId] = useState<string | undefined>(cachedTask?.id)
  const [runningTaskID, setRunningTaskID] = useState("")
  const [savedTasks, setSavedTasks] = useState<TaskConfig[]>([])
  const [saveOpen, setSaveOpen] = useState(false)

  const set = (patch: Partial<ExportOptions>) => setOpts((o) => ({ ...o, ...patch }))

  // 新任务（无缓存配置）时，compatCollation 默认采用设置页的全局值；已加载任务配置不覆盖
  useEffect(() => {
    if (cachedTask?.exportOpts) return
    api.getConfig()
      .then((d) => {
        setOpts((o) => ({ ...o, compatCollation: !!d.config.compatCollation }))
      })
      .catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const loadSavedTasks = useCallback(async () => {
    try {
      setSavedTasks((await api.listTasks("export")) || [])
    } catch { /* ignore */ }
  }, [])

  const applyTask = useCallback((task: TaskConfig) => {
    if (task.exportOpts) {
      setOpts({ ...defaultOptions(), ...task.exportOpts })
      setTaskConfigId(task.id)
      setLastTask(task)
    }
  }, [setLastTask])

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
    // URL 参数优先（由上方 effect 消费）；其次会话缓存已同步初始化，无需异步回填；
    // 两者皆无才拉取上次配置（首次进入该页面）
    if (searchParams.get("task") || searchParams.get("running") || cachedTask) return
    api.getLastTask("export").then(({ task }) => task && applyTask(task)).catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const startRun = async () => {
    if (!opts.sourceConn) {
      toast.error(t("export.needSourceConn"))
      setStep(0)
      return
    }
    if ((opts.tables || []).length === 0 && (opts.objects || []).length === 0) {
      toast.error(t("export.needTable"))
      setStep(1)
      return
    }
    try {
      const { taskID } = await api.startExport(opts, taskConfigId)
      setRunningTaskID(taskID)
      setStep(3)
      refreshHistory()
    } catch (e) {
      toast.error(t("export.startFailed", { err: (e as Error).message }))
    }
  }

  const resetWizard = () => {
    setRunningTaskID("")
    setStep(0)
  }

  return (
    <div className="mx-auto flex h-full max-w-5xl flex-col gap-4">
      <PageHeader
        title={t("export.title")}
        description={t("export.desc")}
        actions={
          <TaskConfigBar
            savedTasks={savedTasks}
            taskConfigId={taskConfigId}
            onApply={applyTask}
            onClear={() => { setOpts(defaultOptions()); setTaskConfigId(undefined); clearLastTask("export"); api.getConfig().then((d) => setOpts((o) => ({ ...o, compatCollation: !!d.config.compatCollation }))).catch(() => {}) }}
            onSave={() => setSaveOpen(true)}
          />
        }
      />

      <Card className="flex flex-1 flex-col gap-5 bg-gradient-to-br from-muted/50 via-muted/15 to-muted/85 p-5 dark:from-muted/30 dark:via-muted/10 dark:to-muted/55">
        <StepWizard steps={STEPS.map((s) => tKey(s))} current={step} onStepClick={(i) => runningTaskID ? undefined : setStep(i)} />

        {/* 单卡宽度与迁移/对比页双卡布局中的卡片同宽（CONN_SINGLE_W），各任务页卡片尺寸统一 */}
        {step === 0 && (
          <div className={`mx-auto w-full space-y-4 ${CONN_SINGLE_W}`}>
            <ConnectionSelect
              title={t("export.sourceDb")}
              subtitle={t("export.sourceDbDesc")}
              value={opts.sourceConn}
              onChange={(name) => set({ sourceConn: name })}
            />
            {/* 提示位置与其他任务页 step0 保持一致：连接卡下方、操作按钮上方 */}
            <Hint>{t("export.hint1")}</Hint>
            <WizardFooter onNext={() => setStep(1)} />
          </div>
        )}

      {step === 1 && (
        <div className="space-y-4">
          <Hint>
            {t("export.hint2")}
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
              <Section title={t("export.exportContent")} description={t("export.exportContentDesc")}>
                <div className="grid gap-2">
                  <CheckRow
                    checked={!opts.dataOnly}
                    onCheckedChange={(v) => {
                      if (!v && opts.dataOnly) return // 至少保留一项
                      set({ schemaOnly: !v })
                    }}
                    label={t("export.schemaOnly")}
                    description={t("export.schemaOnlyDesc")}
                  />
                  <CheckRow
                    checked={!opts.schemaOnly}
                    onCheckedChange={(v) => {
                      if (!v && opts.schemaOnly) return // 至少保留一项
                      set({ dataOnly: !v })
                    }}
                    label={t("export.dataOnly")}
                    description={t("export.dataOnlyDesc")}
                  />
                </div>
              </Section>
            </div>

            <div className="p-5">
              <Section title={t("export.outputSettings")}>
                <div className="grid gap-2">
                  <CheckRow
                    checked={opts.singleTransaction}
                    onCheckedChange={(v) => set({ singleTransaction: v })}
                    label={t("export.snapshot")}
                    description={t("export.snapshotDesc")}
                  />
                  <CheckRow
                    checked={opts.compress}
                    onCheckedChange={(v) => set({ compress: v })}
                    label={t("export.compress")}
                    description={t("export.compressDesc")}
                  />
                  <CheckRow
                    checked={opts.gzip}
                    onCheckedChange={(v) => set({ gzip: v })}
                    label={t("export.gzip")}
                    description={t("export.gzipDesc")}
                  />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-4">
                  <div className="space-y-1">
                    <Label>{t("export.taskName")}</Label>
                    <Input
                      value={opts.taskName}
                      onChange={(e) => set({ taskName: e.target.value })}
                      placeholder={t("export.taskNamePlaceholder")}
                    />
                  </div>
                  <div className="space-y-1">
                    <Label>{t("export.outputDir")}</Label>
                    <Input
                      value={opts.outputDir}
                      onChange={(e) => set({ outputDir: e.target.value })}
                      placeholder={t("export.outputDirPlaceholder")}
                    />
                  </div>
                </div>
              </Section>
            </div>

            <div className="p-5">
              <Section title={t("export.performance")} description={t("export.performanceDesc")}>
                <div className="space-y-1">
                  <Label>{t("export.batchSize")}</Label>
                  <Input
                    type="number"
                    className="w-40"
                    value={opts.batchSize || 0}
                    onChange={(e) => set({ batchSize: Number(e.target.value) })}
                  />
                </div>
              </Section>
            </div>

            <div className="p-5">
              <Section title={t("export.compat")} description={t("export.compatDesc")}>
                <CheckRow
                  checked={opts.compatCollation}
                  onCheckedChange={(v) => set({ compatCollation: v })}
                  label={t("export.compatCollation")}
                  description={t("export.compatCollationDesc")}
                />
              </Section>
            </div>
          </Card>
          <WizardFooter
            onBack={() => setStep(1)}
            next={
              <Button onClick={startRun}>
                <Play className="mr-1 h-4 w-4" /> {t("export.start")}
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
      </Card>

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
