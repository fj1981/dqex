import { useState } from "react"
import { useTranslation } from "react-i18next"
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
  const { t } = useTranslation()
  const [name, setName] = useState("")
  const [saving, setSaving] = useState(false)

  const doSave = async () => {
    if (!name.trim()) {
      toast.error(t("saveTaskDialog.nameRequired"))
      return
    }
    setSaving(true)
    try {
      const payload = { ...buildTask(), name: name.trim(), type }
      const saved = existingId
        ? await api.updateTask({ ...(payload as TaskConfig), id: existingId })
        : await api.saveTask(payload)
      toast.success(t("saveTaskDialog.saved"))
      onOpenChange(false)
      onSaved?.(saved)
    } catch (e) {
      toast.error(t("saveTaskDialog.saveFailed", { err: (e as Error).message }))
    } finally {
      setSaving(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[400px]">
        <DialogHeader>
          <DialogTitle>{t("saveTaskDialog.title")}</DialogTitle>
        </DialogHeader>
        <div className="space-y-2 py-2">
          <Label>{t("saveTaskDialog.name")}</Label>
          <Input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder={t("saveTaskDialog.namePlaceholder")}
            onKeyDown={(e) => e.key === "Enter" && doSave()}
          />
        </div>
        <DialogFooter>
          <Button variant="ghost" onClick={() => onOpenChange(false)}>{t("common.cancel")}</Button>
          <Button onClick={doSave} disabled={saving}>{saving ? t("saveTaskDialog.saving") : t("saveTaskDialog.save")}</Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
