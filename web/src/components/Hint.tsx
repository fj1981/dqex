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
          ? "border-blue-200 bg-blue-50/60 text-blue-700"
          : "border-amber-200 bg-amber-50 text-amber-700",
        className,
      )}
    >
      {children}
    </div>
  )
}
