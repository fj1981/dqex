import { Bookmark } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import type { TaskConfig } from "@/types"

interface Props {
  savedTasks: TaskConfig[]
  taskConfigId?: string
  onApply: (t: TaskConfig) => void
  onClear: () => void
  onSave: () => void
}

// 页面头配置条：加载已保存配置下拉（含清空 + 空状态）+ 保存配置按钮
// 导出/导入/迁移三页通用
export default function TaskConfigBar({ savedTasks, taskConfigId, onApply, onClear, onSave }: Props) {
  return (
    <>
      <Select
        value={taskConfigId || undefined}
        onValueChange={(id) => {
          if (id === "__clear__") return onClear()
          const t = savedTasks.find((x) => x.id === id)
          if (t) onApply(t)
        }}
      >
        <SelectTrigger className="w-52">
          <SelectValue placeholder="加载已保存配置..." />
        </SelectTrigger>
        <SelectContent>
          {savedTasks.length === 0 && (
            <div className="px-2 py-3 text-center text-xs text-muted-foreground">暂无已保存配置</div>
          )}
          {taskConfigId && (
            <SelectItem value="__clear__" className="text-muted-foreground">✕ 清空已选配置</SelectItem>
          )}
          {savedTasks.map((t) => (
            <SelectItem key={t.id} value={t.id}>{t.name}</SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button variant="outline" onClick={onSave}>
        <Bookmark className="mr-1 h-4 w-4" /> 保存配置
      </Button>
    </>
  )
}
