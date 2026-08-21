import { cn } from "@/lib/utils"

// 数据库类型品牌徽标：品牌色 + 缩写（纯本地内联实现，不依赖任何外部图标库/字体/CDN）
// 与 dqex 零依赖离线部署架构保持一致；未知类型回退为 muted 底 + 类型前两字母
const TYPE_META: Record<string, { bg: string; abbr: string }> = {
  mysql:      { bg: "#00758F", abbr: "MY" }, // MySQL 海豚蓝
  mariadb:    { bg: "#003545", abbr: "MA" },
  oceanbase:  { bg: "#2A7DE1", abbr: "OB" },
  postgresql: { bg: "#336791", abbr: "PG" }, // PostgreSQL 大象蓝
  gaussdb:    { bg: "#1E5AA8", abbr: "GS" },
  kingbase:   { bg: "#B08D3E", abbr: "KB" },
  oracle:     { bg: "#F80000", abbr: "OR" }, // Oracle 红
  dameng:     { bg: "#5B5EA6", abbr: "DM" },
}

interface Props {
  type: string
  className?: string
}

export default function DbTypeIcon({ type, className }: Props) {
  const key = type.toLowerCase()
  const meta = TYPE_META[key]
  return (
    <span
      title={type}
      className={cn(
        "inline-flex h-4 w-4 shrink-0 select-none items-center justify-center rounded-[4px] text-[8px] font-bold leading-none text-white",
        !meta && "bg-muted text-muted-foreground",
        className,
      )}
      style={meta ? { backgroundColor: meta.bg } : undefined}
    >
      {meta?.abbr || key.slice(0, 2).toUpperCase()}
    </span>
  )
}
