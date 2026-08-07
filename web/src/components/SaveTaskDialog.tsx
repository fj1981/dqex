import { useState } from "react"
import { toast } from "sonner"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import * as api from "@/api"
import type { TaskConfig, TaskType } from "@/types"

interface Props {
  open: boolean
  onOpenChange: (o: boolean) => void
  type: TaskType
  buildTask: () => Partial<TaskConfig>
  existingId?: string
  onSaved?: (task: TaskConfig) => void
}

// 保存任务配置弹窗
export default function SaveTaskDialog({ open, onOpenChange, type, buildTask, existingId, onSaved }: Props) {
  const [name, setName] = useState("")
  const [saving, setSaving] = useState(false)

  const doSave = async () => {
    if (!name.trim()) {
      toast.error("请输入配置名称")
      return
    }
    setSaving(true)
    try {
      const payload = { ...buildTask(), name: name.trim(), type }
      const saved = existingId
        ? await api.updateTask({ ...(payload as TaskConfig), id: existingId })
        : await api.saveTask(payload)
      toast.success("配置已保存")
      onOpenChange(false)
      onSaved?.(saved)
    } catch (e) {
      toast.error(`保存失败: ${(e as Error).message}`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[400px]">
        <DialogHeader>
          <DialogTitle>保存为任务配置</DialogTitle>
        </DialogHeader>
        <div className="space-y-2 py-2">
          <Label>配置名称</Label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="如：生产数据库每日备份"
            onKeyDown={(e) => e.key === "Enter" && doSave()}
          />
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>取消</Button>
          <Button onClick={doSave} disabled={saving}>{saving ? "保存中..." : "保存"}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
