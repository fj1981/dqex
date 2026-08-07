import { useCallback, useEffect, useRef, useState } from "react"
import { useSearchParams } from "react-router-dom"
import { toast } from "sonner"
import { ChevronRight, FileArchive, Play, Search, UploadCloud } from "lucide-react"
import { Badge } from "@/components/ui/badge"
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
import { Section } from "@/components/Section"
import StepWizard from "@/components/StepWizard"
import { useAppStore } from "@/stores/app"
import { cn, formatBytes } from "@/lib/utils"
import type { ExportDescTable, ImportFileInfo, ImportOptions, TaskConfig } from "@/types"

const STEPS = ["选择目标数据库", "指定导入文件", "设置导入选项", "执行"]

// 取路径的文件名部分（隐藏服务器目录信息）
function baseName(p: string): string {
  return p.split(/[\\/]/).pop() || p
}

// ---- 表分组展示 ----

interface TableGroup {
  prefix: string
  tables: ExportDescTable[]
  rows: number
}

// 按表名前缀（首个下划线前段）分组；不足 3 张表的零散组合并入「其他」
function groupTables(tables: ExportDescTable[]): TableGroup[] {
  const map = new Map<string, ExportDescTable[]>()
  for (const t of tables) {
    const i = t.name.indexOf("_")
    const key = i > 0 ? t.name.slice(0, i) : ""
    if (!map.has(key)) map.set(key, [])
    map.get(key)!.push(t)
  }
  const groups: TableGroup[] = []
  const others: ExportDescTable[] = []
  for (const [prefix, list] of map) {
    list.sort((a, b) => a.name.localeCompare(b.name))
    if (!prefix || list.length < 3) others.push(...list)
    else groups.push({ prefix, tables: list, rows: list.reduce((s, t) => s + t.rows, 0) })
  }
  groups.sort((a, b) => a.prefix.localeCompare(b.prefix))
  if (others.length > 0) {
    others.sort((a, b) => a.name.localeCompare(b.name))
    groups.push({ prefix: "其他", tables: others, rows: others.reduce((s, t) => s + t.rows, 0) })
  }
  return groups
}

// 分组表列表：按前缀分组可折叠，组头展示表数与总行数
function DbTableGroups({ db, tables, filter, expanded, onToggle }: {
  db: string
  tables: ExportDescTable[]
  filter: string
  expanded: Record<string, boolean>
  onToggle: (key: string, open: boolean) => void
}) {
  const kw = filter.trim().toLowerCase()
  const visible = kw ? tables.filter((t) => t.name.toLowerCase().includes(kw)) : tables
  const groups = groupTables(visible)
  // 表不多时默认展开全部分组，多则默认折叠按需展开
  const defaultOpen = tables.length <= 24
  if (groups.length === 0) {
    return <div className="py-2 text-center text-xs text-muted-foreground">无匹配表</div>
  }
  return (
    <div className="space-y-1">
      {groups.map((g) => {
        const key = `${db}:${g.prefix}`
        const open = kw ? true : (expanded[key] ?? defaultOpen)
        return (
          <div key={key} className="overflow-hidden rounded-md border bg-background">
            <button
              type="button"
              className="flex w-full items-center gap-1.5 px-2 py-1 text-xs transition-colors hover:bg-accent/50"
              onClick={() => onToggle(key, !open)}
            >
              <ChevronRight className={cn("h-3 w-3 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
              <span className="font-medium">{g.prefix}</span>
              <span className="text-muted-foreground">{g.tables.length} 张表</span>
              {g.rows > 0 && (
                <span className="ml-auto font-medium tabular-nums">{g.rows.toLocaleString()} 行</span>
              )}
            </button>
            {open && (
              <div className="flex flex-wrap gap-1 border-t px-2 py-1.5">
                {g.tables.map((t) => (
                  <Badge
                    key={t.name}
                    variant="outline"
                    className="font-normal text-xs"
                    title={
                      t.query ||
                      (t.where ? `WHERE ${t.where}` : "") +
                      (t.columns ? ` COLUMNS: ${t.columns.join(", ")}` : "")
                    }
                  >
                    {t.name}
                    {t.rows > 0 && <span className="ml-1 text-muted-foreground">{t.rows}</span>}
                  </Badge>
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

// 对象列表（视图/函数/存储过程）：可折叠，避免长名单直接平铺占屏
function DbObjectGroups({ db, objects, expanded, onToggle }: {
  db: string
  objects: Record<string, string[]>
  expanded: Record<string, boolean>
  onToggle: (key: string, open: boolean) => void
}) {
  return (
    <div className="space-y-1">
      {Object.entries(objects).map(([kind, names]) => {
        const key = `${db}:obj:${kind}`
        const open = expanded[key] ?? names.length <= 8
        const label = kind === "_views" ? "视图" : kind === "_functions" ? "函数" : "存储过程"
        return (
          <div key={kind} className="overflow-hidden rounded-md border bg-background">
            <button
              type="button"
              className="flex w-full items-center gap-1.5 px-2 py-1 text-xs transition-colors hover:bg-accent/50"
              onClick={() => onToggle(key, !open)}
            >
              <ChevronRight className={cn("h-3 w-3 shrink-0 text-muted-foreground transition-transform", open && "rotate-90")} />
              <span className="font-medium">{label}</span>
              <span className="text-muted-foreground">{names.length} 个</span>
            </button>
            {open && (
              <div className="flex flex-wrap gap-1 border-t px-2 py-1.5">
                {names.map((n) => (
                  <Badge key={n} variant="secondary" className="font-normal text-xs">{n}</Badge>
                ))}
              </div>
            )}
          </div>
        )
      })}
    </div>
  )
}

function defaultOptions(): ImportOptions {
  return {
    targetConn: "",
    inputPath: "",
    resetMode: "",
    backup: true,
    batchSize: 500,
  }
}

// 导入页：四步向导
export default function ImportView() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [step, setStep] = useState(0)
  const [opts, setOpts] = useState<ImportOptions>(defaultOptions())
  const [fileInfo, setFileInfo] = useState<ImportFileInfo | null>(null)
  const [inspecting, setInspecting] = useState(false)
  const [uploading, setUploading] = useState(false)
  const [taskConfigId, setTaskConfigId] = useState<string | undefined>()
  const [runningTaskID, setRunningTaskID] = useState("")
  const [savedTasks, setSavedTasks] = useState<TaskConfig[]>([])
  const [saveOpen, setSaveOpen] = useState(false)
  const [dragOver, setDragOver] = useState(false)
  // 输入框展示名：隐藏服务器路径，上传/应用任务后仅显示文件名；
  // opts.inputPath 仍保存真实路径供接口使用，用户手动编辑时清空回到路径模式
  const [inputLabel, setInputLabel] = useState("")
  const [tableFilter, setTableFilter] = useState("")
  const [expandedGroups, setExpandedGroups] = useState<Record<string, boolean>>({})
  const fileRef = useRef<HTMLInputElement>(null)

  const set = (patch: Partial<ImportOptions>) => setOpts((o) => ({ ...o, ...patch }))

  const loadSavedTasks = useCallback(async () => {
    try {
      setSavedTasks((await api.listTasks("import")) || [])
    } catch { /* ignore */ }
  }, [])

  const applyTask = useCallback((task: TaskConfig) => {
    if (task.importOpts) {
      setOpts({ ...defaultOptions(), ...task.importOpts })
      setTaskConfigId(task.id)
      setFileInfo(null)
      const p = task.importOpts.inputPath || ""
      setInputLabel(p && /[\\/]/.test(p) ? baseName(p) : "")
      if (task.importOpts.inputPath) {
        api.inspectImportFile(task.importOpts.inputPath).then(setFileInfo).catch(() => {})
      }
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
      api.getLastTask("import").then(({ task }) => task && applyTask(task)).catch(() => {})
    }
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // 解析文件变化时重置筛选与分组折叠状态
  useEffect(() => {
    setTableFilter("")
    setExpandedGroups({})
  }, [fileInfo])

  const doInspect = async () => {
    if (!opts.inputPath) return
    setInspecting(true)
    try {
      const info = await api.inspectImportFile(opts.inputPath)
      setFileInfo(info)
    } catch (e) {
      setFileInfo(null)
      toast.error(`解析失败: ${(e as Error).message}`)
    } finally {
      setInspecting(false)
    }
  }

  const doUpload = async (file: File) => {
    setUploading(true)
    try {
      const { path, name, info } = await api.uploadImportFile(file)
      set({ inputPath: path })
      setInputLabel(name || baseName(path))
      if (info) setFileInfo(info)
      toast.success("上传成功")
    } catch (e) {
      toast.error(`上传失败: ${(e as Error).message}`)
    } finally {
      setUploading(false)
    }
  }

  const startRun = async () => {
    if (!opts.targetConn) {
      toast.error("请先选择目标数据库连接")
      setStep(0)
      return
    }
    if (!opts.inputPath) {
      toast.error("请先指定导入文件")
      setStep(1)
      return
    }
    try {
      const { taskID } = await api.startImport(opts, taskConfigId)
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
        title="导入数据"
        description="将 SQL 文件或 zip 备份包导入到目标数据库"
        actions={
          <TaskConfigBar
            savedTasks={savedTasks}
            taskConfigId={taskConfigId}
            onApply={applyTask}
            onClear={() => { setOpts(defaultOptions()); setTaskConfigId(undefined); setFileInfo(null); setInputLabel("") }}
            onSave={() => setSaveOpen(true)}
          />
        }
      />

      <StepWizard steps={STEPS} current={step} onStepClick={(i) => !runningTaskID && setStep(i)} />

      {step === 0 && (
        <div className="mx-auto max-w-2xl space-y-4">
          <Hint>
            导入文件需与目标库为同类型数据库（如 MySQL 导出的文件只能导入 MySQL，不支持跨类型转换）。
          </Hint>
          <ConnectionSelect
            title="目标数据库"
            subtitle="选择要导入到的数据库连接"
            value={opts.targetConn}
            onChange={(name) => set({ targetConn: name })}
          />
          <WizardFooter
            onNext={() => setStep(1)}
            next={<Button disabled={!opts.targetConn} onClick={() => setStep(1)}>下一步</Button>}
          />
        </div>
      )}

      {step === 1 && (
        <div className="mx-auto flex w-full max-w-3xl min-h-0 flex-1 flex-col gap-4">
          {/* 已有解析结果时才全高弹性（详情区内部滚动）；未选文件时卡片按内容自然高度，避免拖拽区被拉伸撑满 */}
          <Card className={cn("flex flex-col gap-4 p-5", fileInfo && "min-h-0 flex-1 overflow-hidden")}>
            <div className="space-y-1">
              <Label>文件路径（支持 .sql 或 .zip）</Label>
              <div className="flex gap-2">
                <Input
                  value={inputLabel || opts.inputPath}
                  onChange={(e) => {
                    set({ inputPath: e.target.value })
                    setInputLabel("")
                    setFileInfo(null)
                  }}
                  placeholder="/path/to/backup.zip"
                />
                <Button variant="outline" onClick={doInspect} disabled={!opts.inputPath || inspecting}>
                  <Search className="mr-1 h-4 w-4" /> {inspecting ? "解析中..." : "解析"}
                </Button>
              </div>
            </div>

            {/* 拖拽上传区 */}
            <div
              role="button"
              tabIndex={0}
              onClick={() => fileRef.current?.click()}
              onKeyDown={(e) => e.key === "Enter" && fileRef.current?.click()}
              onDragOver={(e) => {
                e.preventDefault()
                setDragOver(true)
              }}
              onDragLeave={() => setDragOver(false)}
              onDrop={(e) => {
                e.preventDefault()
                setDragOver(false)
                const f = e.dataTransfer.files?.[0]
                if (f) doUpload(f)
              }}
              className={cn(
                "flex cursor-pointer items-center justify-center rounded-lg border-2 border-dashed transition-colors",
                // 已有解析结果时收缩为紧凑横条；未选文件时保持适中的固定高度，不随页面拉伸高
                fileInfo ? "gap-2 px-4 py-2 text-xs" : "h-40 flex-col gap-2 px-6 text-center",
                dragOver ? "border-primary bg-primary/5" : "hover:border-primary/40 hover:bg-accent/40",
              )}
            >
              <UploadCloud className={cn(fileInfo ? "h-4 w-4" : "h-8 w-8", dragOver ? "text-primary" : "text-muted-foreground")} />
              <div className={fileInfo ? undefined : "text-sm"}>
                {uploading ? "上传中..." : fileInfo ? "重新选择文件，或将新文件拖拽到此处" : "点击选择文件，或将文件拖拽到此处"}
              </div>
              {!fileInfo && <div className="text-xs text-muted-foreground">支持 .sql 与 .zip 格式</div>}
            </div>
            <input
              ref={fileRef}
              type="file"
              accept=".sql,.zip"
              className="hidden"
              onChange={(e) => {
                const f = e.target.files?.[0]
                if (f) doUpload(f)
                e.target.value = ""
              }}
            />

            {fileInfo && (
              <div className="flex min-h-0 flex-1 flex-col rounded-md border bg-muted/30 p-3 text-sm">
                <div className="mb-2 flex shrink-0 items-center gap-2">
                  <FileArchive className="h-4 w-4 text-muted-foreground" />
                  <Badge variant="secondary">{fileInfo.type === "zip" ? "zip 包" : "SQL 文件"}</Badge>
                  <span className="text-muted-foreground">{formatBytes(fileInfo.size)}</span>
                  <span className="ml-auto flex items-center gap-1 text-xs font-medium text-green-600">已解析</span>
                </div>
                <div className="shrink-0 text-muted-foreground">包含 {fileInfo.databases.length} 个数据库：</div>
                <div className="mt-1 flex shrink-0 flex-wrap gap-1">
                  {fileInfo.databases.map((d) => (
                    <Badge key={d} variant="outline" className="font-normal">{d}</Badge>
                  ))}
                </div>
                {/* 展示 desc 详细信息：筛选栏吸顶，列表区弹性占满剩余高度并内部滚动，滚动条仅限此区域 */}
                {fileInfo.descs && Object.keys(fileInfo.descs).length > 0 && (
                  <div className="mt-3 flex min-h-0 flex-1 flex-col border-t pt-3">
                    <div className="mb-2 flex shrink-0 items-center justify-end">
                      <div className="relative">
                        <Search className="absolute left-2 top-1/2 h-3 w-3 -translate-y-1/2 text-muted-foreground" />
                        <Input
                          className="h-7 w-44 pl-7 text-xs"
                          placeholder="筛选表名"
                          value={tableFilter}
                          onChange={(e) => setTableFilter(e.target.value)}
                        />
                      </div>
                    </div>
                    <div className="scrollbar-thin min-h-0 flex-1 space-y-4 overflow-y-auto pr-1">
                      {fileInfo.databases.map((db) => {
                        const desc = fileInfo.descs?.[db]
                        if (!desc) return null
                        const totalRows = (desc.tables || []).reduce((s, t) => s + t.rows, 0)
                        return (
                          <div key={db} className="space-y-2">
                            <div className="flex flex-wrap items-center gap-2 text-xs">
                              <span className="text-sm font-medium">{db}</span>
                              <Badge variant="outline" className="font-normal">{desc.dbType}</Badge>
                              <Badge variant="outline" className="font-normal">
                                {desc.mode === "schemaOnly" ? "仅结构" : desc.mode === "dataOnly" ? "仅数据" : "结构+数据"}
                              </Badge>
                              {desc.tables && desc.tables.length > 0 && (
                                <span className="text-muted-foreground">
                                  {desc.tables.length} 张表{totalRows > 0 && <span className="tabular-nums"> · {totalRows.toLocaleString()} 行</span>}
                                </span>
                              )}
                              <span className="ml-auto text-muted-foreground" title="导出时间">{desc.exportTime}</span>
                            </div>
                            {/* 表列表：按前缀分组 + 可折叠 + 可筛选 */}
                            {desc.tables && desc.tables.length > 0 && (
                              <DbTableGroups
                                db={db}
                                tables={desc.tables}
                                filter={tableFilter}
                                expanded={expandedGroups}
                                onToggle={(key, open) => setExpandedGroups((m) => ({ ...m, [key]: open }))}
                              />
                            )}
                            {/* 对象列表：可折叠 */}
                            {desc.objects && Object.keys(desc.objects).length > 0 && (
                              <DbObjectGroups
                                db={db}
                                objects={desc.objects}
                                expanded={expandedGroups}
                                onToggle={(key, open) => setExpandedGroups((m) => ({ ...m, [key]: open }))}
                              />
                            )}
                          </div>
                        )
                      })}
                    </div>
                  </div>
                )}
              </div>
            )}
          </Card>
          <WizardFooter
            onBack={() => setStep(0)}
            next={<Button disabled={!fileInfo} onClick={() => setStep(2)}>下一步</Button>}
          />
        </div>
      )}

      {step === 2 && (
        <div className="mx-auto max-w-3xl space-y-4">
          <Card className="divide-y p-0">
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
            next={<Button onClick={startRun}><Play className="mr-1 h-4 w-4" /> 开始导入</Button>}
          />
        </div>
      )}

      {step === 3 && runningTaskID && (
        <ProgressView
          taskID={runningTaskID}
          taskType="import"
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
        type="import"
        existingId={taskConfigId}
        buildTask={() => ({ importOpts: opts })}
        onSaved={(t) => {
          setTaskConfigId(t.id)
          loadSavedTasks()
        }}
      />
    </div>
  )
}
