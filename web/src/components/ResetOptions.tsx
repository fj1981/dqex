import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { RESET_MODE_LABEL, type ResetMode } from "@/types"

interface Props {
  resetMode: ResetMode
  backup: boolean
  onResetModeChange: (m: ResetMode) => void
  onBackupChange: (b: boolean) => void
}

// 重置数据选项（导入/迁移共用）
export default function ResetOptions({ resetMode, backup, onResetModeChange, onBackupChange }: Props) {
  return (
    <div className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">重置数据</h3>
        <p className="mt-1 text-xs text-muted-foreground">导入前对目标表的处理策略，默认直接追加</p>
      </div>
      <RadioGroup
        value={resetMode === "" ? "none" : resetMode}
        onValueChange={(v) => onResetModeChange(v === "none" ? "" : (v as ResetMode))}
      >
        {(Object.keys(RESET_MODE_LABEL) as ResetMode[]).map((m) => (
          <div key={m || "none"} className="flex items-center space-x-2">
            <RadioGroupItem value={m || "none"} id={`reset-${m || "none"}`} />
            <Label htmlFor={`reset-${m || "none"}`} className="font-normal">
              {RESET_MODE_LABEL[m]}
            </Label>
          </div>
        ))}
      </RadioGroup>

      {resetMode !== "" && (
        <>
          <label className="flex items-center space-x-2 pt-1">
            <Checkbox checked={backup} onCheckedChange={(v) => onBackupChange(v === true)} />
            <span className="text-sm">重置前在目标库创建备份表 __dbimpex_bak_*（导入成功后自动清理）</span>
          </label>
          <Alert>
            <AlertTriangle className="h-4 w-4" />
            <AlertDescription>
              {resetMode === "truncate"
                ? "“清空表”将删除目标库中相关表的所有现有数据，保留表结构。"
                : "“删除重建”将删除目标库中现有表并重新创建表结构。"}
            </AlertDescription>
          </Alert>
        </>
      )}
    </div>
  )
}
