import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import {
  CheckCircle2,
  Copy,
  Download,
  FolderOpen,
  Loader2,
  RotateCcw,
  Terminal,
  XCircle,
} from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Progress } from "@/components/ui/progress"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import * as api from "@/api"
import { useAppStore } from "@/stores/app"
import { cn, shortPaths } from "@/lib/utils"
import { tKey } from "@/lib/i18n"
import i18n from "@/lib/i18n"
import type { Progress as ProgressInfo } from "@/types"

interface Props {
  taskID: string
  taskType: string
  onSaveTask?: () => void
  onBack: () => void
  // 完成（含失败/取消）回调：外部页面据此拉取最终结果
  onDone?: (p: ProgressInfo) => void
  // 宽度与外层结果视图对齐（去掉本组件的 max-w 限制）
  wide?: boolean
  // 日志区固定限高：下方还有结果视图时使用，避免进度面板独占视口
  compactLog?: boolean
}

function formatDuration(sec: number): string {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return m > 0 ? i18n.t("progress.durationMin", { m, s }) : i18n.t("progress.durationSec", { s })
}

// 紧凑统计块：固定单行，长内容截断靠 title 悬停查看
function StatBlock({ label, value, sub, title }: { label: string; value: string; sub?: string; title?: string }) {
  return (
    <div className="min-w-0 rounded-md border bg-muted/30 px-2.5 py-2">
      <div className="text-xs text-muted-foreground">{label}</div>
      <div className="mt-0.5 truncate text-base font-medium tabular-nums" title={title}>
        {value}
        {sub && <span className="text-xs font-normal text-muted-foreground"> {sub}</span>}
      </div>
    </div>
  )
}

// 任务执行进度视图（SSE 实时推送）
export default function ProgressView({ taskID, taskType, onSaveTask, onBack, onDone, wide, compactLog }: Props) {
  const { t } = useTranslation()
  const [progress, setProgress] = useState<ProgressInfo | null>(null)
  const [errorMsg, setErrorMsg] = useState("")
  const [errDetailOpen, setErrDetailOpen] = useState(false)
  const [elapsed, setElapsed] = useState(0)
  const logRef = useRef<HTMLDivElement>(null)
  const startRef = useRef(Date.now())

  useEffect(() => {
    if (!taskID) return
    const close = api.subscribeProgress(taskID, {
      onProgress: (p) => setProgress(p),
      onDone: (p) => {
        if (p && p.taskID) setProgress(p)
        if (p && onDone) onDone(p)
      },
      onError: (msg) => setErrorMsg(msg),
    })
    return close
  }, [taskID])

  const state = progress?.state || "running"
  const isRunning = state === "running" || state === "idle"

  // 运行中每秒刷新耗时
  useEffect(() => {
    if (!isRunning) return
    const t = setInterval(() => {
      setElapsed(Math.floor((Date.now() - startRef.current) / 1000))
    }, 1000)
    return () => clearInterval(t)
  }, [isRunning])

  // 任务进入终态（完成/失败/取消）后刷新右侧操作历史：
  // 后端先持久化终态再推送 SSE，此刻读取到的已是最新状态；延迟少量时间兼容写入耗时。
  // 注意：终态时后端会连发 progress+done 两个事件，progress 引用连续变化，
  // 这里不可返回 clearTimeout 清理（会把刚挂上的定时器取消导致列表永远不刷新）；
  // ref 已保证只调度一次，无需清理
  const refreshedRef = useRef(false)
  useEffect(() => {
    if (isRunning || !progress || refreshedRef.current) return
    refreshedRef.current = true
    setTimeout(() => useAppStore.getState().loadHistory(), 500)
  }, [isRunning, progress])

  useEffect(() => {
    if (logRef.current) logRef.current.scrollTop = logRef.current.scrollHeight
  }, [progress?.logs])

  // 后端 percent 已是 0-100 的百分数（非 0-1 比例），不可再乘 100
  const percent = Math.min(100, Math.round(progress?.percent || 0))

  // 当前表只显示表名（限定名取最后一段），完整限定名用 title 悬停展示
  const currentTableFull = progress?.currentTable || "-"
  const currentTableName = currentTableFull === "-" ? "-" : (currentTableFull.split('.').pop() || currentTableFull)

  // 耗时口径一致：终态回放优先用执行历史的总耗时，实时监控用本地计时
  const elapsedSec = progress?.durationMs ? Math.round(progress.durationMs / 1000) : elapsed
  const logs = progress?.logs || []

  const doCancel = async () => {
    try {
      await api.cancelTask(taskID)
    } catch (e) {
      toast.error(t("progress.cancelFailed", { err: (e as Error).message }))
    }
  }

  const doOpenDir = async () => {
    try {
      await api.openExportDir(taskID)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const statusMeta: Record<string, { label: string; icon: React.ReactNode; cls: string }> = {
    done: {
      label: "progress.statusDone",
      icon: <CheckCircle2 className="h-5 w-5 shrink-0 text-green-600 dark:text-green-400" />,
      cls: "border-green-500/30 bg-green-500/10 text-green-800 dark:text-green-300",
    },
    error: {
      label: "progress.statusError",
      icon: <XCircle className="h-5 w-5 shrink-0 text-destructive" />,
      cls: "border-red-500/30 bg-red-500/10 text-red-800 dark:text-red-300",
    },
    cancelled: {
      label: "progress.statusCancelled",
      icon: <XCircle className="h-5 w-5 shrink-0 text-muted-foreground" />,
      cls: "border-border bg-muted/40 text-muted-foreground",
    },
    running: {
      label: "progress.statusRunning",
      icon: <Loader2 className="h-5 w-5 shrink-0 animate-spin text-primary" />,
      cls: "border-blue-500/30 bg-blue-500/10 text-blue-800 dark:text-blue-300",
    },
  }
  const meta = statusMeta[state] || statusMeta.running

  // 错误文案：横幅只显示首行摘要（超长截断），长错误通过「查看详情」弹窗展示全文
  const errFull = state === "error" ? shortPaths(errorMsg || progress?.message || t("progress.unknownError")) : ""
  const errTooLong = errFull.length > 120 || errFull.includes("\n")
  const errFirstLine = errFull.split("\n")[0]
  const errBrief = errFirstLine.length > 160 ? errFirstLine.slice(0, 160) + "…" : errFirstLine

  const copyError = async () => {
    try {
      await navigator.clipboard.writeText(errorMsg || progress?.message || "")
      toast.success(t("progress.copied"))
    } catch {
      toast.error(t("progress.copyFailed"))
    }
  }

  return (
    <div className={cn("mx-auto flex w-full min-h-0 flex-col gap-3", !compactLog && "flex-1", !wide && "max-w-3xl")}>
      {/* 状态横幅 */}
      <div className={cn("flex shrink-0 items-start gap-3 rounded-lg border px-4 py-2.5", meta.cls)}>
        {meta.icon}
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{tKey(meta.label)}</div>
          <div className="mt-0.5 break-all text-xs opacity-80">
            {state === "error"
              ? (errTooLong ? errBrief : errFull)
              : isRunning
                ? (progress?.currentTable ? `${t("progress.statCurrent")}: ${currentTableName}` : t("progress.runningHint"))
                : shortPaths(progress?.message || "")}
          </div>
        </div>
        {state === "error" && errTooLong && (
          <Button
            variant="outline"
            size="sm"
            className="h-7 shrink-0 border-red-500/30 bg-background text-xs text-red-800 dark:text-red-300"
            onClick={() => setErrDetailOpen(true)}
          >
            {t("progress.viewDetail")}
          </Button>
        )}
      </div>

      {/* 错误详情弹窗：长错误全文展示，可复制 */}
      <Dialog open={errDetailOpen} onOpenChange={setErrDetailOpen}>
        <DialogContent className="flex h-[560px] max-w-[760px] flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <XCircle className="h-4 w-4" /> {t("progress.errorDetailTitle")}
            </DialogTitle>
          </DialogHeader>
          <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto whitespace-pre-wrap break-all rounded-md border border-red-500/30 bg-red-500/10 p-3 font-mono text-xs leading-relaxed text-red-900 dark:text-red-300">
            {errorMsg || progress?.message || t("progress.unknownError")}
          </div>
          <div className="flex shrink-0 justify-end gap-2">
            <Button variant="outline" size="sm" onClick={copyError}>
              <Copy className="mr-1 h-3.5 w-3.5" /> {t("common.copy")}
            </Button>
            <Button size="sm" onClick={() => setErrDetailOpen(false)}>{t("common.close")}</Button>
          </div>
        </DialogContent>
      </Dialog>

      <Card className={cn("flex flex-col gap-3.5 p-4", !compactLog && "min-h-0 flex-1")}>
        {/* 进度行：百分比 + 进度条占满剩余宽度，取消按钮内联不再独占一行 */}
        <div className="flex shrink-0 items-center gap-3">
          <span className="w-14 shrink-0 text-2xl font-semibold tabular-nums">{percent}%</span>
          <Progress value={percent} className="h-2 min-w-0 flex-1" />
          {isRunning && (
            <Button variant="destructive" size="sm" className="h-7 shrink-0 px-2.5" onClick={doCancel}>
              <XCircle className="mr-1 h-3.5 w-3.5" /> {t("progress.cancelTask")}
            </Button>
          )}
        </div>

        {/* 统计条：四项紧凑单行，长内容截断 hover 可见 */}
        <div className="grid shrink-0 grid-cols-2 gap-2 md:grid-cols-4">
          <StatBlock
            label={t("progress.statUnits")}
            value={String(progress?.doneUnits || 0)}
            sub={`/ ${progress?.totalUnits || 0}`}
          />
          <StatBlock
            label={t("progress.statRows")}
            value={(progress?.doneRows || 0).toLocaleString()}
            sub={progress?.totalRows ? `/ ${progress.totalRows.toLocaleString()}` : undefined}
          />
          <StatBlock label={t("progress.statElapsed")} value={formatDuration(elapsedSec)} />
          <StatBlock
            label={t("progress.statCurrent")}
            value={currentTableName}
            title={currentTableFull}
          />
        </div>

        {progress?.outputPath && (
          <div className="shrink-0 rounded-md border bg-muted/30 px-2.5 py-2">
            <div className="text-xs text-muted-foreground">{t("progress.outputFile")}</div>
            <div className="mt-0.5 truncate text-xs" title={progress.outputPath}>
              {progress.outputPath.split(/[\\/]/).pop()}
            </div>
          </div>
        )}

        {/* 日志（终端风格）：默认弹性填满剩余高度；紧凑模式下限高内滚，为下方结果视图让位 */}
        <div className={cn("flex flex-col", !compactLog && "min-h-0 flex-1")}>
          <div className="mb-1.5 flex shrink-0 items-center gap-1.5 text-sm font-medium">
            <Terminal className="h-4 w-4 text-muted-foreground" /> {t("progress.logTitle")}
          </div>
          <div
            ref={logRef}
            className={cn(
              "scrollbar-thin overflow-y-auto rounded-md bg-slate-950 p-3 text-slate-200",
              compactLog ? "max-h-44" : "min-h-[120px] max-h-[40vh] flex-1",
            )}
          >
            <div className="space-y-0.5 text-xs leading-relaxed">
              {logs.length === 0 && !isRunning && (
                <div className="text-slate-500">{t("progress.noLog")}</div>
              )}
              {logs.map((l, i) => (
                <div key={i} className="whitespace-pre-wrap break-all">{l}</div>
              ))}
              {isRunning && <div className="animate-pulse text-slate-500">▋</div>}
            </div>
          </div>
        </div>

        {/* 操作（终态才展示；运行中取消按钮已在进度行内联） */}
        {!isRunning && (
          <div className="flex shrink-0 items-center justify-end gap-2 border-t pt-3">
            {state === "done" && (taskType === "export" || taskType === "dictionary") && (
              <>
                <Button variant="outline" size="sm" asChild>
                  <a href={api.downloadUrl(taskID)} download>
                    <Download className="mr-1 h-4 w-4" /> {t("progress.download")}
                  </a>
                </Button>
                <Button variant="outline" size="sm" onClick={doOpenDir}>
                  <FolderOpen className="mr-1 h-4 w-4" /> {t("progress.openDir")}
                </Button>
              </>
            )}
            {state === "done" && onSaveTask && (
              <Button variant="outline" size="sm" onClick={onSaveTask}>{t("progress.saveTask")}</Button>
            )}
            <Button size="sm" onClick={onBack}>
              <RotateCcw className="mr-1 h-4 w-4" /> {t("progress.restart")}
            </Button>
          </div>
        )}
      </Card>
    </div>
  )
}
