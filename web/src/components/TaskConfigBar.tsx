import { useTranslation } from "react-i18next"
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
  const { t } = useTranslation()
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
          <SelectValue placeholder={t("taskBar.loadPlaceholder")} />
        </SelectTrigger>
        <SelectContent>
          {savedTasks.length === 0 && (
            <div className="px-2 py-3 text-center text-xs text-muted-foreground">{t("taskBar.empty")}</div>
          )}
          {taskConfigId && (
            <SelectItem value="__clear__" className="text-muted-foreground">{t("taskBar.clear")}</SelectItem>
          )}
          {savedTasks.map((t) => (
            <SelectItem key={t.id} value={t.id}>{t.name}</SelectItem>
          ))}
        </SelectContent>
      </Select>
      <Button variant="outline" onClick={onSave}>
        <Bookmark className="mr-1 h-4 w-4" /> {t("taskBar.save")}
      </Button>
    </>
  )
}
