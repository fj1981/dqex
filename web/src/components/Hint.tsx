import type { ReactNode } from "react"
import { cn } from "@/lib/utils"

interface Props {
  children: ReactNode
  variant?: "info" | "warning"
  className?: string
}

// 步骤内提示条：info=蓝色（说明），warning=琥珀色（警告）
export default function Hint({ children, variant = "info", className }: Props) {
  return (
    <div
      className={cn(
        "rounded-md border px-3 py-2 text-xs",
        variant === "info"
          ? "border-blue-500/30 bg-blue-500/10 text-blue-700 dark:text-blue-300"
          : "border-amber-500/30 bg-amber-500/10 text-amber-700 dark:text-amber-300",
        className,
      )}
    >
      {children}
    </div>
  )
}
