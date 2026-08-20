// 数据库操作错误友好展示卡：优先展示可读的中文标题、原因与排查建议，
// 原始错误（驱动/连接层文本）默认折叠，避免时间戳、堆栈等对普通用户无意义的信息干扰；
// 无法识别的错误模式原样展示（dbError.ts 仅在明确匹配时才翻译，绝不硬翻译误导排查方向）。
import { useState } from "react"
import { useTranslation } from "react-i18next"
import { AlertCircle, ChevronDown, Lightbulb, RefreshCw } from "lucide-react"
import { Button } from "@/components/ui/button"
import { friendlyDBError } from "@/lib/dbError"
import { cn } from "@/lib/utils"

interface Props {
  error: string // 原始错误文本（后端 msg/details）
  onRetry?: () => void // 重试回调（不传则不显示按钮）
  retryLabel?: string // 重试按钮文案，默认「重试」
  className?: string
}

// 原始错误去重：后端日志 details 常把同一错误拼接两遍（如 "...,连接数据库失败(...): dial tcp ...: connection refused"），
// 展示前按标点分割去重，避免用户看到重复文本
function dedupe(raw: string): string {
  const parts = raw
    .split(/[,，；;]/)
    .map((p) => p.trim())
    .filter(Boolean)
  return [...new Set(parts)].join("，")
}

export default function DBErrorCard({ error, onRetry, retryLabel, className }: Props) {
  const { t } = useTranslation()
  const friendly = friendlyDBError(error)
  const detail = dedupe(error)
  const [showDetail, setShowDetail] = useState(false)

  return (
    <div
      className={cn(
        "flex max-w-xl items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-4 text-sm",
        className,
      )}
    >
      <AlertCircle className="mt-0.5 h-5 w-5 shrink-0 text-destructive" />
      <div className="min-w-0 flex-1">
        <div className="font-medium text-destructive">{friendly ? friendly.title : t("dbError.opFailed")}</div>
        {friendly ? (
          <>
            <div className="mt-1 break-words text-foreground/80">{friendly.reason}</div>
            {/* 排查建议：可操作的具体步骤，替代笼统的「重试」引导 */}
            <ul className="mt-2 space-y-1">
              {friendly.advice.map((a, i) => (
                <li key={i} className="flex items-start gap-1.5 text-xs text-foreground/70">
                  <Lightbulb className="mt-0.5 h-3 w-3 shrink-0 text-amber-500" />
                  <span className="min-w-0 break-words">{a}</span>
                </li>
              ))}
            </ul>
            {/* 原始错误折叠：识别出友好原因后，详细文本默认收起，需要时再展开 */}
            <button
              type="button"
              className="mt-2 flex items-center gap-0.5 text-xs text-muted-foreground hover:text-foreground"
              onClick={() => setShowDetail(!showDetail)}
            >
              <ChevronDown className={cn("h-3 w-3 transition-transform", showDetail && "rotate-180")} />
              {showDetail ? t("dbError.collapseDetail") : t("dbError.viewDetail")}
            </button>
            {showDetail && (
              <pre className="mt-1 max-h-32 overflow-auto whitespace-pre-wrap break-words rounded bg-muted/50 p-2 font-mono text-[11px] text-foreground/70">
                {detail}
              </pre>
            )}
          </>
        ) : (
          <div className="mt-0.5 break-words text-foreground/80">{detail}</div>
        )}
        {onRetry && (
          <div className="mt-3">
            <Button variant="outline" size="sm" className="gap-1.5" onClick={onRetry}>
              <RefreshCw className="h-3.5 w-3.5" /> {retryLabel ?? t("common.retry")}
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
