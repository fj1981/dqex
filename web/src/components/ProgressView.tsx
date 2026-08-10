import { useEffect, useRef, useState } from "react"
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
import type { Progress as ProgressInfo } from "@/types"

interface Props {
  taskID: string
  taskType: string
  onSaveTask?: () => void
  onBack: () => void
  // 宽度与外层结果视图对齐（去掉本组件的 max-w 限制）
  wide?: boolean
  // 日志区固定限高：下方还有结果视图时使用，避免进度面板独占视口
  compactLog?: boolean
}

function formatDuration(sec: number): string {
  const m = Math.floor(sec / 60)
  const s = sec % 60
  return m > 0 ? `${m} 分 ${s} 秒` : `${s} 秒`
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
export default function ProgressView({ taskID, taskType, onSaveTask, onBack, wide, compactLog }: Props) {
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

  // 耗时口径一致：终态回放优先用执行历史的总耗时，实时监控用本地计时
  const elapsedSec = progress?.durationMs ? Math.round(progress.durationMs / 1000) : elapsed
  const logs = progress?.logs || []

  const doCancel = async () => {
    try {
      await api.cancelTask(taskID)
    } catch (e) {
      toast.error(`取消失败: ${(e as Error).message}`)
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
      label: "执行完成",
      icon: <CheckCircle2 className="h-5 w-5 shrink-0 text-green-600" />,
      cls: "border-green-200 bg-green-50 text-green-800",
    },
    error: {
      label: "执行失败",
      icon: <XCircle className="h-5 w-5 shrink-0 text-destructive" />,
      cls: "border-red-200 bg-red-50 text-red-800",
    },
    cancelled: {
      label: "已取消",
      icon: <XCircle className="h-5 w-5 shrink-0 text-muted-foreground" />,
      cls: "border-border bg-muted/40 text-muted-foreground",
    },
    running: {
      label: "执行中",
      icon: <Loader2 className="h-5 w-5 shrink-0 animate-spin text-primary" />,
      cls: "border-blue-200 bg-blue-50 text-blue-800",
    },
  }
  const meta = statusMeta[state] || statusMeta.running

  // 错误文案：横幅只显示首行摘要（超长截断），长错误通过「查看详情」弹窗展示全文
  const errFull = state === "error" ? shortPaths(errorMsg || progress?.message || "未知错误") : ""
  const errTooLong = errFull.length > 120 || errFull.includes("\n")
  const errFirstLine = errFull.split("\n")[0]
  const errBrief = errFirstLine.length > 160 ? errFirstLine.slice(0, 160) + "…" : errFirstLine

  const copyError = async () => {
    try {
      await navigator.clipboard.writeText(errorMsg || progress?.message || "")
      toast.success("已复制错误详情")
    } catch {
      toast.error("复制失败")
    }
  }

  return (
    <div className={cn("mx-auto flex w-full min-h-0 flex-col gap-3", !compactLog && "flex-1", !wide && "max-w-3xl")}>
      {/* 状态横幅 */}
      <div className={cn("flex shrink-0 items-start gap-3 rounded-lg border px-4 py-2.5", meta.cls)}>
        {meta.icon}
        <div className="min-w-0 flex-1">
          <div className="text-sm font-medium">{meta.label}</div>
          <div className="mt-0.5 break-all text-xs opacity-80">
            {state === "error"
              ? (errTooLong ? errBrief : errFull)
              : shortPaths(progress?.message || "") || (isRunning ? "任务正在执行，请勿关闭页面..." : "")}
          </div>
        </div>
        {state === "error" && errTooLong && (
          <Button
            variant="outline"
            size="sm"
            className="h-7 shrink-0 border-red-300 bg-white/60 text-xs text-red-800 hover:bg-white"
            onClick={() => setErrDetailOpen(true)}
          >
            查看详情
          </Button>
        )}
      </div>

      {/* 错误详情弹窗：长错误全文展示，可复制 */}
      <Dialog open={errDetailOpen} onOpenChange={setErrDetailOpen}>
        <DialogContent className="flex h-[560px] max-w-[760px] flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 text-destructive">
              <XCircle className="h-4 w-4" /> 执行失败详情
            </DialogTitle>
          </DialogHeader>
          <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto whitespace-pre-wrap break-all rounded-md border bg-red-50/50 p-3 font-mono text-xs leading-relaxed text-red-900">
            {errorMsg || progress?.message || "未知错误"}
          </div>
          <div className="flex shrink-0 justify-end gap-2">
            <Button variant="outline" size="sm" onClick={copyError}>
              <Copy className="mr-1 h-3.5 w-3.5" /> 复制
            </Button>
            <Button size="sm" onClick={() => setErrDetailOpen(false)}>关闭</Button>
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
              <XCircle className="mr-1 h-3.5 w-3.5" /> 取消任务
            </Button>
          )}
        </div>

        {/* 统计条：四项紧凑单行，长内容截断 hover 可见 */}
        <div className="grid shrink-0 grid-cols-2 gap-2 md:grid-cols-4">
          <StatBlock
            label="进度（表/对象）"
            value={String(progress?.doneUnits || 0)}
            sub={`/ ${progress?.totalUnits || 0}`}
          />
          <StatBlock
            label="已处理行数"
            value={(progress?.doneRows || 0).toLocaleString()}
            sub={progress?.totalRows ? `/ ${progress.totalRows.toLocaleString()}` : undefined}
          />
          <StatBlock label="耗时" value={formatDuration(elapsedSec)} />
          <StatBlock
            label="当前表"
            value={progress?.currentTable || "-"}
            title={progress?.currentTable}
          />
        </div>

        {progress?.outputPath && (
          <div className="shrink-0 rounded-md border bg-muted/30 px-2.5 py-2">
            <div className="text-xs text-muted-foreground">输出文件</div>
            <div className="mt-0.5 truncate text-xs" title={progress.outputPath}>
              {progress.outputPath.split(/[\\/]/).pop()}
            </div>
          </div>
        )}

        {/* 日志（终端风格）：默认弹性填满剩余高度；紧凑模式下限高内滚，为下方结果视图让位 */}
        <div className={cn("flex flex-col", !compactLog && "min-h-0 flex-1")}>
          <div className="mb-1.5 flex shrink-0 items-center gap-1.5 text-sm font-medium">
            <Terminal className="h-4 w-4 text-muted-foreground" /> 实时日志
          </div>
          <div
            ref={logRef}
            className={cn(
              "scrollbar-thin overflow-y-auto rounded-md bg-slate-950 p-3 text-slate-200",
              compactLog ? "max-h-44" : "min-h-[180px] flex-1",
            )}
          >
            <div className="space-y-0.5 text-xs leading-relaxed">
              {logs.length === 0 && !isRunning && (
                <div className="text-slate-500">任务已结束，该记录未留存执行日志</div>
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
            {state === "done" && taskType === "export" && (
              <>
                <Button variant="outline" size="sm" asChild>
                  <a href={api.downloadUrl(taskID)} download>
                    <Download className="mr-1 h-4 w-4" /> 下载文件
                  </a>
                </Button>
                <Button variant="outline" size="sm" onClick={doOpenDir}>
                  <FolderOpen className="mr-1 h-4 w-4" /> 打开文件夹
                </Button>
              </>
            )}
            {state === "done" && onSaveTask && (
              <Button variant="outline" size="sm" onClick={onSaveTask}>保存为任务配置</Button>
            )}
            <Button size="sm" onClick={onBack}>
              <RotateCcw className="mr-1 h-4 w-4" /> 重新开始
            </Button>
          </div>
        )}
      </Card>
    </div>
  )
}
