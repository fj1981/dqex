import { Check } from "lucide-react"
import { cn } from "@/lib/utils"

interface Props {
  steps: string[]
  current: number
  onStepClick?: (index: number) => void
}

// 步骤条：圆形序号 + 连接线 + 标签，已完成步骤可回跳。
// 每步固定等宽（flex-1），连接线绝对定位只画在相邻圆点之间，
// 保证不同页面/文案长度下圆点位置一致，且首尾无多余线条
export default function StepWizard({ steps, current, onStepClick }: Props) {
  return (
    <div className="flex items-start rounded-lg border bg-background px-6 py-4">
      {steps.map((label, i) => {
        const done = i < current
        const active = i === current
        const clickable = !!onStepClick && i < current
        return (
          <div key={label} className="relative flex min-w-0 flex-1 flex-col items-center">
            {/* 连接线：从上一步圆点中心到当前圆点中心（横跨一个等分单元格） */}
            {i > 0 && (
              <div
                className={cn(
                  "absolute right-1/2 top-[13px] h-0.5 w-full rounded transition-colors",
                  i <= current ? "bg-primary/70" : "bg-border",
                )}
              />
            )}
            <button
              type="button"
              disabled={!clickable}
              onClick={() => clickable && onStepClick(i)}
              className={cn(
                "group relative z-10 flex flex-col items-center gap-1.5 outline-none",
                clickable && "cursor-pointer",
              )}
              title={clickable ? `返回「${label}」` : label}
            >
              <span
                className={cn(
                  "flex h-7 w-7 shrink-0 items-center justify-center rounded-full border-2 text-xs font-semibold transition-all",
                  active && "border-primary bg-primary text-primary-foreground ring-4 ring-primary/15",
                  done && "border-primary bg-primary text-primary-foreground group-hover:opacity-85",
                  !active && !done && "border-border bg-background text-muted-foreground",
                )}
              >
                {done ? <Check className="h-3.5 w-3.5" /> : i + 1}
              </span>
              <span
                className={cn(
                  "text-center text-sm leading-tight",
                  active ? "font-medium text-primary" : done ? "font-medium text-foreground" : "text-muted-foreground",
                )}
              >
                {label}
              </span>
            </button>
          </div>
        )
      })}
    </div>
  )
}
