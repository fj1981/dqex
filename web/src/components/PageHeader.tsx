import type { ReactNode } from "react"

interface Props {
  title: string
  description?: string
  actions?: ReactNode
}

// 统一页面头：标题 + 描述 + 右侧操作区
export default function PageHeader({ title, description, actions }: Props) {
  return (
    <div className="flex items-start justify-between gap-4">
      <div className="min-w-0">
        {/* 页面标题用 medium：中文加粗会显得黑重、抢视觉焦点 */}
        <h2 className="text-xl font-medium leading-tight">{title}</h2>
        {/* 描述宽度不足时单行截断，悬停 title 查看全文 */}
        {description && (
          <p className="mt-1.5 truncate text-sm text-muted-foreground" title={description}>
            {description}
          </p>
        )}
      </div>
      {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
    </div>
  )
}
