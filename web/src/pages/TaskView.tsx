import { useCallback, useEffect, useMemo, useState } from "react"
import { useNavigate } from "react-router-dom"
import { toast } from "sonner"
import { FileDown, FileUp, ArrowLeftRight, ClipboardList, Eye, Pencil, Play, Search, Trash2 } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import * as api from "@/api"
import PageHeader from "@/components/PageHeader"
import { useAppStore } from "@/stores/app"
import { TASK_TYPE_LABEL, type ExecutionRecord, type TaskConfig } from "@/types"
import { formatTime } from "@/lib/utils"

const TYPE_ICON: Record<string, { icon: React.ReactNode; cls: string }> = {
  export: { icon: <FileDown className="h-5 w-5" />, cls: "bg-blue-50 text-blue-500" },
  import: { icon: <FileUp className="h-5 w-5" />, cls: "bg-green-50 text-green-500" },
  migrate: { icon: <ArrowLeftRight className="h-5 w-5" />, cls: "bg-purple-50 text-purple-500" },
}

function taskSummary(task: TaskConfig): string[] {
  const lines: string[] = []
  if (task.type === "export" && task.exportOpts) {
    const o = task.exportOpts
    lines.push(`源: ${o.sourceConn || "-"}`)
    if (o.tables?.length) lines.push(`表: ${o.tables.slice(0, 5).join(", ")}${o.tables.length > 5 ? ` 等 ${o.tables.length} 张` : ""}`)
    lines.push(`模式: ${o.schemaOnly ? "仅结构" : o.dataOnly ? "仅数据" : "结构+数据"}${o.compress ? "，zip 打包" : ""}`)
  } else if (task.type === "import" && task.importOpts) {
    const o = task.importOpts
    lines.push(`目标: ${o.targetConn || "-"}`)
    // 仅展示文件名，隐藏服务器路径信息
    lines.push(`文件: ${o.inputPath ? o.inputPath.split(/[\\/]/).pop() : "-"}`)
    lines.push(`重置: ${o.resetMode || "不重置"}`)
  } else if (task.type === "migrate" && task.migrateOpts) {
    const o = task.migrateOpts
    lines.push(`源: ${o.sourceConn || "-"} → 目标: ${o.targetConn || "-"}`)
    if (o.tables?.length) lines.push(`表: ${o.tables.slice(0, 5).join(", ")}${o.tables.length > 5 ? ` 等 ${o.tables.length} 张` : ""}`)
    lines.push(`模式: ${o.schemaOnly ? "仅结构" : o.dataOnly ? "仅数据" : "结构+数据"}，重置: ${o.resetMode || "不重置"}`)
  }
  return lines
}

// 任务页：已保存的任务配置列表（默认首页）
export default function TaskView() {
  const navigate = useNavigate()
  const loadHistory = useAppStore((s) => s.loadHistory)
  const [typeFilter, setTypeFilter] = useState("all")
  const [keyword, setKeyword] = useState("")
  const [tasks, setTasks] = useState<TaskConfig[]>([])
  const [detail, setDetail] = useState<TaskConfig | null>(null)
  const [detailHistory, setDetailHistory] = useState<ExecutionRecord[]>([])

  const load = useCallback(async () => {
    try {
      const list = await api.listTasks(typeFilter === "all" ? undefined : typeFilter)
      setTasks(list || [])
    } catch (e) {
      toast.error((e as Error).message)
    }
  }, [typeFilter])

  useEffect(() => {
    load()
  }, [load])

  const filtered = useMemo(
    () => tasks.filter((t) => !keyword || t.name.toLowerCase().includes(keyword.toLowerCase())),
    [tasks, keyword],
  )

  const openDetail = async (task: TaskConfig) => {
    setDetail(task)
    try {
      const h = await api.listHistory({ taskConfigId: task.id })
      setDetailHistory(h || [])
    } catch {
      setDetailHistory([])
    }
  }

  const doRun = async (task: TaskConfig) => {
    try {
      const { taskID } = await api.runTask(task.id)
      toast.success(`任务已启动: ${taskID}`)
      loadHistory()
      navigate(`/${task.type}?running=${taskID}&task=${task.id}`)
    } catch (e) {
      toast.error(`执行失败: ${(e as Error).message}`)
    }
  }

  const doDelete = async (task: TaskConfig) => {
    try {
      await api.deleteTask(task.id)
      toast.success("已删除")
      load()
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  return (
    <div className="mx-auto max-w-5xl space-y-4">
      <PageHeader
        title="任务列表"
        description="已保存的任务配置，可一键重复执行"
        actions={
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
            <Input
              placeholder="搜索任务名称..."
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              className="w-48 pl-8"
            />
          </div>
        }
      />

      <Tabs value={typeFilter} onValueChange={setTypeFilter}>
        <TabsList>
          <TabsTrigger value="all">全部</TabsTrigger>
          <TabsTrigger value="export">导出</TabsTrigger>
          <TabsTrigger value="import">导入</TabsTrigger>
          <TabsTrigger value="migrate">迁移</TabsTrigger>
        </TabsList>
      </Tabs>

      {filtered.length === 0 && (
        <div className="flex flex-col items-center gap-2 rounded-lg border border-dashed bg-background py-16 text-center">
          <ClipboardList className="h-8 w-8 text-muted-foreground/50" />
          <div className="text-sm text-muted-foreground">暂无任务配置</div>
          <div className="text-xs text-muted-foreground">可在「导出 / 导入 / 迁移」页保存配置后在此统一管理</div>
        </div>
      )}

      <div className="grid gap-3">
        {filtered.map((task) => {
          const meta = TYPE_ICON[task.type]
          return (
            <Card key={task.id} className="p-4 transition-shadow hover:shadow-md">
              <div className="flex items-start justify-between gap-3">
                <div className="flex min-w-0 items-start gap-3">
                  <span className={`flex h-10 w-10 shrink-0 items-center justify-center rounded-lg ${meta?.cls || "bg-muted"}`}>
                    {meta?.icon}
                  </span>
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate font-medium">{task.name}</span>
                      <Badge variant="secondary" className="shrink-0 font-normal">{TASK_TYPE_LABEL[task.type]}</Badge>
                      {task.isLastUsed && <Badge variant="outline" className="shrink-0 font-normal">上次使用</Badge>}
                    </div>
                    <div className="mt-1 space-y-0.5 text-sm text-muted-foreground">
                      {taskSummary(task).map((l, i) => (
                        <div key={i} className="truncate">{l}</div>
                      ))}
                      <div className="text-xs">更新于 {formatTime(new Date(task.updatedAt).toISOString())}</div>
                    </div>
                  </div>
                </div>
                <div className="flex shrink-0 items-center gap-1">
                  <Button variant="ghost" size="icon" title="查看详情" onClick={() => openDetail(task)}>
                    <Eye className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="icon" title="编辑配置" onClick={() => navigate(`/${task.type}?task=${task.id}`)}>
                    <Pencil className="h-4 w-4" />
                  </Button>
                  <Button variant="ghost" size="icon" title="删除" onClick={() => doDelete(task)}>
                    <Trash2 className="h-4 w-4 text-destructive" />
                  </Button>
                  <Button size="sm" className="ml-1" onClick={() => doRun(task)}>
                    <Play className="mr-1 h-4 w-4" /> 执行
                  </Button>
                </div>
              </div>
            </Card>
          )
        })}
      </div>

      {/* 详情弹窗 */}
      <Dialog open={!!detail} onOpenChange={(o) => !o && setDetail(null)}>
        <DialogContent className="sm:max-w-[560px]">
          <DialogHeader>
            <DialogTitle>配置详情: {detail?.name}</DialogTitle>
          </DialogHeader>
          {detail && (
            <div className="space-y-3 text-sm">
              <div className="space-y-1">
                {taskSummary(detail).map((l, i) => (
                  <div key={i}>{l}</div>
                ))}
              </div>

              {/* 表及条件 */}
              {(() => {
                const conds =
                  detail.exportOpts?.conditions || detail.migrateOpts?.conditions || []
                if (conds.length === 0) return null
                return (
                  <div>
                    <div className="mb-1 font-medium">过滤条件:</div>
                    <ScrollArea className="h-28 rounded border p-2">
                      {conds.map((c) => (
                        <div key={c.tableName} className="break-all text-xs">
                          {c.tableName} |
                          {c.dataMode === "skip"
                            ? " 不导出数据"
                            : ` ${c.query || (c.where ? `WHERE ${c.where}` : "（无）")}${!c.query && c.columns?.length ? ` | 列: ${c.columns.join(",")}` : ""}`}
                        </div>
                      ))}
                    </ScrollArea>
                  </div>
                )
              })()}

              <div>
                <div className="mb-1 font-medium">执行历史:</div>
                {detailHistory.length === 0 && (
                  <div className="text-muted-foreground">暂无执行记录</div>
                )}
                <ScrollArea className="max-h-36 rounded border p-2">
                  {detailHistory.map((h) => (
                    <div key={h.id} className="flex justify-between text-xs py-0.5">
                      <span>{formatTime(new Date(h.startedAt).toISOString())}</span>
                      <span
                        className={
                          h.status === "done"
                            ? "text-green-600"
                            : h.status === "error"
                              ? "text-destructive"
                              : "text-muted-foreground"
                        }
                      >
                        {h.status === "done" ? "成功" : h.status === "error" ? "失败" : h.status}
                      </span>
                      <span className="text-muted-foreground">{h.summary}</span>
                    </div>
                  ))}
                </ScrollArea>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="ghost" onClick={() => setDetail(null)}>关闭</Button>
            <Button variant="outline" onClick={() => detail && navigate(`/${detail.type}?task=${detail.id}`)}>
              编辑配置
            </Button>
            <Button onClick={() => detail && doRun(detail)}>立即执行</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
