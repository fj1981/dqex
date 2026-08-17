import { useEffect, useState } from "react"
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
          <DialogTitle>关于</DialogTitle>
        </DialogHeader>

        <div className="space-y-4">
          {/* 品牌区 */}
          <div className="flex items-center gap-3">
            <span className="flex h-12 w-12 shrink-0 items-center justify-center rounded-lg bg-primary text-primary-foreground">
              <Database className="h-6 w-6" />
            </span>
            <div>
              <div className="text-base font-medium">数据库工作台 dbx</div>
              <div className="text-sm text-muted-foreground">数据库导入 / 导出 / 迁移 / 对比 / 数据字典工具</div>
            </div>
          </div>

          <Separator />

          {/* 版本信息 */}
          {version ? (
            <div className="space-y-2 text-sm">
              <div className="flex items-center justify-between">
                <span className="flex items-center gap-2 text-muted-foreground">
                  <Info className="h-4 w-4" />
                  版本
                </span>
                <span className="font-mono font-medium">dbx {version.version}</span>
              </div>
              {version.buildTime && (
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">构建时间</span>
                  <span className="font-mono">{version.buildTime}</span>
                </div>
              )}
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">支持的数据库</span>
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
