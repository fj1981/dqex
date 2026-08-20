import { useCallback, useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
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
import { tKey } from "@/lib/i18n"
import type { DictionaryOptions, TaskConfig } from "@/types"

const STEPS = ["dictionary.step1", "dictionary.step2", "dictionary.step3"]

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
  const { t, i18n } = useTranslation()
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
      toast.error(t("dictionary.needSourceConn"))
      setStep(0)
      return
    }
    if ((opts.tables || []).length === 0) {
      toast.error(t("dictionary.needTable"))
      setStep(1)
      return
    }
    try {
      const { taskID } = await api.startDictionary({ ...opts, lang: i18n.language }, taskConfigId)
      setRunningTaskID(taskID)
      setStep(2)
      refreshHistory()
    } catch (e) {
      toast.error(t("dictionary.startFailed", { err: (e as Error).message }))
    }
  }

  const resetWizard = () => {
    setRunningTaskID("")
    setStep(0)
  }

  return (
    <div className="mx-auto flex h-full max-w-5xl flex-col gap-4">
      <PageHeader
        title={t("dictionary.title")}
        description={t("dictionary.desc")}
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

      <Card className="flex flex-1 flex-col gap-5 bg-gradient-to-br from-muted/50 via-muted/15 to-muted/85 p-5 dark:from-muted/30 dark:via-muted/10 dark:to-muted/55">
        <StepWizard steps={STEPS.map((s) => tKey(s))} current={step} onStepClick={(i) => runningTaskID ? undefined : setStep(i)} />

        {step === 0 && (
        <div className={`mx-auto w-full space-y-4 ${CONN_SINGLE_W}`}>
          <ConnectionSelect
            title={t("dictionary.sourceDb")}
            subtitle={t("dictionary.sourceDbDesc")}
            value={opts.sourceConn}
            onChange={(name) => set({ sourceConn: name })}
          />
          <Hint>{t("dictionary.hint1")}</Hint>
          <WizardFooter onNext={() => setStep(1)} />
        </div>
      )}

      {step === 1 && (
        <div className="space-y-4">
          <Hint>
            {t("dictionary.hint2")}
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
              <Section title={t("export.outputSettings")}>
                <div className="grid gap-2">
                  <CheckRow
                    checked={opts.compress}
                    onCheckedChange={(v) => set({ compress: v })}
                    label={t("export.compress")}
                    description={t("dictionary.compressDesc")}
                  />
                </div>
                <div className="mt-3 grid grid-cols-2 gap-4">
                  <div className="space-y-1">
                    <Label>{t("export.taskName")}</Label>
                    <Input
                      value={opts.taskName}
                      onChange={(e) => set({ taskName: e.target.value })}
                      placeholder={t("dictionary.taskNamePlaceholder")}
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
          </Card>
          <WizardFooter
            onBack={() => setStep(1)}
            next={
              <Button onClick={startRun}>
                <Play className="mr-1 h-4 w-4" /> {t("dictionary.start")}
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

      </Card>

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
