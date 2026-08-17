import type { LucideIcon } from "lucide-react"
import { cn } from "@/lib/utils"

// TableIcon：表格内小图标统一封装，解决 lucide 图标在 12px 小尺寸下因
// stroke-width=2 + HiDPI 亚像素抗锯齿导致的发虚/模糊问题。
// 通过显式 strokeWidth + vector-effect 保持笔画锐利，尺寸可微调。
interface TableIconProps {
  icon: LucideIcon
  className?: string
  size?: number // 像素尺寸，默认 14（比默认 12px 更清晰）
  strokeWidth?: number // 默认 2.25，比 lucide 默认 2 更锐利
}

export default function TableIcon({ icon: Icon, className, size = 14, strokeWidth = 2.25 }: TableIconProps) {
  return (
    <Icon
      className={cn("shrink-0", className)}
      style={{ width: size, height: size }}
      strokeWidth={strokeWidth}
      vectorEffect="non-scaling-stroke"
    />
  )
}
