import type { ReactNode } from "react"
import { useTranslation } from "react-i18next"
import { ArrowLeft, ArrowRight } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

interface Props {
  onBack?: () => void
  onNext?: () => void
  /** 自定义右侧内容（如"开始执行"按钮），不传则用默认"下一步"按钮 */
  next?: ReactNode
  className?: string
}

// 步骤向导底部导航：左=上一步，右=下一步（或自定义操作）
// 不传 onBack 时仅渲染右侧按钮（右对齐），三页通用
export default function WizardFooter({ onBack, onNext, next, className }: Props) {
  const { t } = useTranslation()
  if (!onBack) {
    return <div className={cn("flex justify-end", className)}>{next ?? <Button onClick={onNext}>{t("wizard.next")} <ArrowRight className="ml-1 h-4 w-4" /></Button>}</div>
  }
  return (
    <div className={cn("flex justify-between", className)}>
      <Button variant="outline" onClick={onBack}>
        <ArrowLeft className="mr-1 h-4 w-4" /> {t("wizard.prev")}
      </Button>
      {next ?? <Button onClick={onNext}>{t("wizard.next")} <ArrowRight className="ml-1 h-4 w-4" /></Button>}
    </div>
  )
}
