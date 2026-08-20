import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { Database, Info, Loader2 } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Separator } from "@/components/ui/separator"
import * as api from "@/api"
import type { VersionInfo } from "@/types"

// 关于弹窗：版本 + 构建时间 + 支持的数据库。低频查看信息，用弹窗比独立页面更轻量。
export default function AboutDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation()
  const [version, setVersion] = useState<VersionInfo | null>(null)

  useEffect(() => {
    if (!open) return
    let cancelled = false
    api.getVersion()
      .then((v) => { if (!cancelled) setVersion(v) })
      .catch(() => {})
    return () => { cancelled = true }
  }, [open])

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[420px]">
        <DialogHeader>
          <DialogTitle>{t("about.title")}</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* 品牌区 */}
          <div className="flex items-center gap-3">
            <span className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              <Database className="h-6 w-6" />
            </span>
            <div>
              <div className="text-base font-medium">{t("about.brand")}</div>
              <div className="text-sm text-muted-foreground">{t("about.tagline")}</div>
            </div>
          </div>

          <Separator />

          {/* 版本信息 */}
          {version ? (
            <div className="space-y-2 text-sm">
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-2 text-muted-foreground">
                  <Info className="h-4 w-4" />
                  {t("about.version")}
                </span>
                <span className="font-mono font-medium">dbx {version.version}</span>
              </div>
              {version.buildTime && (
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">{t("about.buildTime")}</span>
                  <span className="font-mono">{version.buildTime}</span>
                </div>
              )}
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">{t("about.dbTypes")}</span>
                <span className="font-mono">{version.dbTypes.join(" / ")}</span>
              </div>
            </div>
          ) : (
            <div className="flex items-center justify-center py-4">
              <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
