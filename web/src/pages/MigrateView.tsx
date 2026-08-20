import { useCallback, useEffect, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
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
import ConnectionPair from "@/components/ConnectionPair"
import PageHeader from "@/components/PageHeader"
import ProgressView from "@/components/ProgressView"
import ResetOptions from "@/components/ResetOptions"
import SaveTaskDialog from "@/components/SaveTaskDialog"
import { CheckRow, Section } from "@/components/Section"
import StepWizard from "@/components/StepWizard"
import TablePicker from "@/components/TablePicker"
import { useAppStore } from "@/stores/app"
import { tKey } from "@/lib/i18n"
import type { MigrateOptions, TaskConfig } from "@/types"

const STEPS = ["migrate.step1", "migrate.step2", "migrate.step3", "migrate.step4"]

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
    compatCollation: false,
  }
}

// 迁移页：四步向导（支持跨数据库类型：数据以行数据中转，结构自动转换）
export default function MigrateView() {
  const { t } = useTranslation()
  const [searchParams, setSearchParams] = useSearchParams()
  // 会话内缓存的最近应用配置：挂载时同步初始化，避免空配置闪现后再回填
  const cachedTask = useAppStore((s) => s.lastTasks["migrate"])
  const setLastTask = useAppStore((s) => s.setLastTask)
  const clearLastTask = useAppStore((s) => s.clearLastTask)
  const [step, setStep] = useState(0)
  const [opts, setOpts] = useState<MigrateOptions>(() =>
    cachedTask?.migrateOpts ? { ...defaultOptions(), ...cachedTask.migrateOpts } : defaultOptions())
  const [taskConfigId, setTaskConfigId] = useState<string | undefined>(cachedTask?.id)
  const [runningTaskID, setRunningTaskID] = useState("")
  const [savedTasks, setSavedTasks] = useState<TaskConfig[]>([])
  const [saveOpen, setSaveOpen] = useState(false)
  const connections = useAppStore((s) => s.connections)

  const set = (patch: Partial<MigrateOptions>) => setOpts((o) => ({ ...o, ...patch }))

  // 新任务（无缓存配置）时，compatCollation 默认采用设置页的全局值；已加载任务配置不覆盖
  useEffect(() => {
    if (cachedTask?.migrateOpts) return
    api.getConfig()
      .then((d) => {
        setOpts((o) => ({ ...o, compatCollation: !!d.config.compatCollation }))
      })
      .catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

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
      setLastTask(task)
    }
  }, [setLastTask])

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
    // URL 参数优先（由上方 effect 消费）；其次会话缓存已同步初始化，无需异步回填；
    // 两者皆无才拉取上次配置（首次进入该页面）
    if (searchParams.get("task") || searchParams.get("running") || cachedTask) return
    api.getLastTask("migrate").then(({ task }) => task && applyTask(task)).catch(() => {})
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const startRun = async () => {
    if (!opts.sourceConn || !opts.targetConn) {
      toast.error(t("migrate.needBothConn"))
      setStep(0)
      return
    }
    if (opts.sourceConn === opts.targetConn) {
      toast.error(t("migrate.sameConn"))
      setStep(0)
      return
    }
    if ((opts.tables || []).length === 0 && (opts.objects || []).length === 0) {
      toast.error(t("migrate.needTable"))
      setStep(1)
      return
    }
    if (crossType && (opts.objects || []).length > 0) {
      toast.error(t("migrate.crossTypeNoObjects"))
      setStep(1)
      return
    }
    try {
      const { taskID } = await api.startMigrate(opts, taskConfigId)
      setRunningTaskID(taskID)
      setStep(3)
      setTimeout(() => useAppStore.getState().loadHistory(), 800)
    } catch (e) {
      toast.error(t("migrate.startFailed", { err: (e as Error).message }))
    }
  }

  return (
    <div className="mx-auto flex h-full max-w-5xl flex-col gap-4">
      <PageHeader
        title={t("migrate.title")}
        description={t("migrate.desc")}
        actions={
          <TaskConfigBar
            savedTasks={savedTasks}
            taskConfigId={taskConfigId}
            onApply={applyTask}
            onClear={() => { setOpts(defaultOptions()); setTaskConfigId(undefined); clearLastTask("migrate"); api.getConfig().then((d) => setOpts((o) => ({ ...o, compatCollation: !!d.config.compatCollation }))).catch(() => {}) }}
            onSave={() => setSaveOpen(true)}
          />
        }
      />

      <Card className="flex flex-1 flex-col gap-5 bg-gradient-to-br from-muted/50 via-muted/15 to-muted/85 p-5 dark:from-muted/30 dark:via-muted/10 dark:to-muted/55">
        <StepWizard steps={STEPS.map((s) => tKey(s))} current={step} onStepClick={(i) => !runningTaskID && setStep(i)} />

        {/* 数据源卡片布局共用 ConnectionPair，各任务页卡片尺寸统一 */}
        {step === 0 && (
          <ConnectionPair
          source={{
            title: t("migrate.sourceDb"),
            subtitle: t("migrate.sourceDbDesc"),
            value: opts.sourceConn,
            onChange: (name) => set({ sourceConn: name, source: null, databases: [], tables: [], objects: [], conditions: [] }),
          }}
          target={{
            title: t("migrate.targetDb"),
            subtitle: t("migrate.targetDbDesc"),
            value: opts.targetConn,
            onChange: (name) => set({ targetConn: name, target: null }),
          }}
        >
          {opts.sourceConn && opts.sourceConn === opts.targetConn && (
            <Hint variant="warning">{t("migrate.sameConnHint")}</Hint>
          )}
          {crossType && (
            <Hint>
              {t("migrate.crossHint", { src: srcType, tgt: tgtType })}
            </Hint>
          )}
          {!crossType && !!opts.sourceConn && !!opts.targetConn && opts.sourceConn !== opts.targetConn && (
            <Hint>
              {t("migrate.sameHint")}
            </Hint>
          )}
          <WizardFooter
            onNext={() => setStep(1)}
            next={
              <Button
                disabled={!opts.sourceConn || !opts.targetConn || opts.sourceConn === opts.targetConn}
                onClick={() => setStep(1)}
              >
                {t("common.next")} <MoveRight className="ml-1 h-4 w-4" />
              </Button>
            }
          />
        </ConnectionPair>
      )}

      {step === 1 && (
        <div className="space-y-4">
          <Hint>
            {t("migrate.hint1")}
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
              <Section title={t("migrate.content")} description={t("migrate.contentDesc")}>
                <div className="grid gap-2">
                  <CheckRow
                    checked={!opts.dataOnly}
                    onCheckedChange={(v) => {
                      if (!v && opts.dataOnly) return
                      set({ schemaOnly: !v })
                    }}
                    label={t("migrate.schemaOnly")}
                    description={t("migrate.schemaOnlyDesc")}
                  />
                  <CheckRow
                    checked={!opts.schemaOnly}
                    onCheckedChange={(v) => {
                      if (!v && opts.schemaOnly) return
                      set({ dataOnly: !v })
                    }}
                    label={t("migrate.dataOnly")}
                    description={t("migrate.dataOnlyDesc")}
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
              <Section title={t("export.performance")} description={t("migrate.performanceDesc")}>
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
                  description={t("migrate.compatCollationDesc")}
                />
              </Section>
            </div>
          </Card>
          <WizardFooter
            onBack={() => setStep(1)}
            next={
              <Button onClick={startRun}>
                <Play className="mr-1 h-4 w-4" /> {t("migrate.start")}
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
      </Card>

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
