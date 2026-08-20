import { useTranslation } from "react-i18next"
import { AlertTriangle } from "lucide-react"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { RadioGroup, RadioGroupItem } from "@/components/ui/radio-group"
import { RESET_MODE_LABEL, type ResetMode } from "@/types"
import { tKey } from "@/lib/i18n"

interface Props {
  resetMode: ResetMode
  backup: boolean
  onResetModeChange: (m: ResetMode) => void
  onBackupChange: (b: boolean) => void
}

// 重置数据选项（导入/迁移共用）
export default function ResetOptions({ resetMode, backup, onResetModeChange, onBackupChange }: Props) {
  const { t } = useTranslation()
  return (
    <div className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">{t("resetOptions.title")}</h3>
        <p className="mt-1 text-xs text-muted-foreground">{t("resetOptions.desc")}</p>
      </div>
      <RadioGroup
        value={resetMode === "" ? "none" : resetMode}
        onValueChange={(v) => onResetModeChange(v === "none" ? "" : (v as ResetMode))}
      >
        {(Object.keys(RESET_MODE_LABEL) as ResetMode[]).map((m) => (
          <div key={m || "none"} className="flex items-center space-x-2">
            <RadioGroupItem value={m || "none"} id={`reset-${m || "none"}`} />
            <Label htmlFor={`reset-${m || "none"}`} className="font-normal">
              {tKey(RESET_MODE_LABEL[m])}
            </Label>
          </div>
        ))}
      </RadioGroup>

      {resetMode !== "" && (
        <>
          <label className="flex items-center space-x-2 pt-1">
            <Checkbox checked={backup} onCheckedChange={(v) => onBackupChange(v === true)} />
            <span className="text-sm">{t("resetOptions.backup")}</span>
          </label>
          <Alert>
            <AlertTriangle className="h-4 w-4" />
            <AlertDescription>
              {resetMode === "truncate"
                ? t("resetOptions.truncateWarn")
                : t("resetOptions.dropWarn")}
            </AlertDescription>
          </Alert>
        </>
      )}
    </div>
  )
}
