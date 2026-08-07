import type { ReactNode } from "react"
import { Checkbox } from "@/components/ui/checkbox"

interface SectionProps {
  title: string
  description?: string
  children: ReactNode
}

// 表单分节：标题 + 说明 + 内容（选项页统一分组布局）
export function Section({ title, description, children }: SectionProps) {
  return (
    <section className="space-y-3">
      <div>
        <h3 className="text-sm font-medium">{title}</h3>
        {/* 说明宽度不足时单行截断，悬停 title 查看全文 */}
        {description && (
          <p className="mt-1 truncate text-xs text-muted-foreground" title={description}>
            {description}
          </p>
        )}
      </div>
      {children}
    </section>
  )
}

interface CheckRowProps {
  checked: boolean
  onCheckedChange: (v: boolean) => void
  label: string
  description?: string
  disabled?: boolean
}

// 复选行：复选框 + 标签 + 说明文字
export function CheckRow({ checked, onCheckedChange, label, description, disabled }: CheckRowProps) {
  return (
    <label
      className={`flex items-start gap-2.5 rounded-md border px-3 py-2.5 transition-colors ${
        checked ? "border-primary/40 bg-primary/5" : "hover:bg-accent/50"
      } ${disabled ? "opacity-60" : "cursor-pointer"}`}
    >
      <Checkbox
        className="mt-0.5"
        checked={checked}
        disabled={disabled}
        onCheckedChange={(v) => onCheckedChange(v === true)}
      />
      <span>
        <span className="block text-sm font-medium leading-tight">{label}</span>
        {description && <span className="mt-0.5 block text-xs text-muted-foreground">{description}</span>}
      </span>
    </label>
  )
}
