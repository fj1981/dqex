import { useState, useRef, useEffect } from "react"
import { useTranslation } from "react-i18next"
import { ChevronDown, Search } from "lucide-react"
import type { TableColumn } from "@/types"
import { Badge } from "@/components/ui/badge"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { cn } from "@/lib/utils"

export interface ColumnOption {
  name: string
  dataType?: string
  primaryKey?: boolean
  isTime?: boolean
}

// 时间类列识别：常见创建/更新时间列名，或时间类型 + 创建/更新语义的列名
const TIME_EXACT = new Set([
  "created_at", "updated_at", "create_time", "update_time",
  "created", "updated", "createtime", "updatetime", "createdate", "updatedate",
  "gmt_create", "gmt_modified", "gmt_created", "gmt_update",
])
const TIME_TYPE = /datetime|timestamp/i
const TIME_NAME = /creat|updat|modif|gmt/i

export function isTimeColumn(col: TableColumn): boolean {
  const name = col.name.toLowerCase()
  return TIME_EXACT.has(name) || (TIME_TYPE.test(col.dataType) && TIME_NAME.test(name))
}

export function toColumnOptions(cols: TableColumn[]): ColumnOption[] {
  return cols.map((c) => ({
    name: c.name,
    dataType: c.dataType,
    primaryKey: !!c.primaryKey,
    isTime: isTimeColumn(c),
  }))
}

interface Props {
  options: ColumnOption[]
  value: string[]
  onChange: (cols: string[]) => void
  placeholder?: string
  loading?: boolean
  compact?: boolean // 行内紧凑高度（表级配置行）
  className?: string
}

// 忽略列多选：行内展开式下拉（checkbox 列表 + 搜索 + 时间列快捷勾选），
// 主键列禁选（引擎要求按主键对齐，忽略主键列无意义）
export default function ColumnMultiSelect({
  options, value, onChange, placeholder, loading, compact, className,
}: Props) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)
  const [query, setQuery] = useState("")
  const rootRef = useRef<HTMLDivElement>(null)

  // 点击外部关闭
  useEffect(() => {
    if (!open) return
    const onPointerDown = (e: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    document.addEventListener("mousedown", onPointerDown)
    return () => document.removeEventListener("mousedown", onPointerDown)
  }, [open])

  const selected = new Set(value.map((v) => v.toLowerCase()))
  const filtered = options.filter((o) => !query || o.name.toLowerCase().includes(query.toLowerCase()))
  const timeCols = options.filter((o) => o.isTime && !o.primaryKey).map((o) => o.name)

  const toggle = (name: string) => {
    const next = new Set(selected)
    if (next.has(name.toLowerCase())) next.delete(name.toLowerCase())
    else next.add(name.toLowerCase())
    // 按 options 原序输出，保持展示稳定
    onChange(options.filter((o) => next.has(o.name.toLowerCase())).map((o) => o.name))
  }

  return (
    <div ref={rootRef} className={cn("relative min-w-0 flex-1", className)}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className={cn(
          "flex w-full items-center gap-1.5 overflow-hidden rounded-md border border-input bg-transparent px-2 text-left shadow-xs",
          "focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-ring",
          compact ? "h-7 text-xs" : "h-8 text-sm",
        )}
      >
        {value.length === 0 ? (
          <span className="flex-1 truncate text-muted-foreground">{placeholder ?? t("colSelect.placeholder")}</span>
        ) : (
          <span className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
            {value.slice(0, 3).map((v) => (
              <Badge key={v} variant="secondary" className="max-w-28 shrink-0 font-mono text-[10px] font-normal">
                <span className="truncate">{v}</span>
              </Badge>
            ))}
            {value.length > 3 && <span className="shrink-0 text-[10px] text-muted-foreground">+{value.length - 3}</span>}
          </span>
        )}
        <ChevronDown className="ml-auto h-3.5 w-3.5 shrink-0 text-muted-foreground" />
      </button>

      {open && (
        <div className="absolute left-0 top-full z-50 mt-1 w-64 rounded-md border bg-card p-2 shadow-md">
          <div className="flex items-center gap-1.5">
            <div className="relative min-w-0 flex-1">
              <Search className="absolute left-2 top-1/2 h-3.5 w-3.5 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={query}
                onChange={(e) => setQuery(e.target.value)}
                placeholder={t("colSelect.search")}
                className="h-7 pl-7 text-xs"
              />
            </div>
            <button
              type="button"
              onClick={() => onChange([...new Set([...value, ...timeCols])].filter(Boolean))}
              disabled={timeCols.length === 0}
              className="shrink-0 rounded-md px-2 py-1 text-[11px] text-muted-foreground hover:bg-accent disabled:opacity-40"
            >
              {t("colSelect.checkTimeCols", { n: timeCols.length })}
            </button>
            <button
              type="button"
              onClick={() => onChange([])}
              disabled={value.length === 0}
              className="shrink-0 rounded-md px-2 py-1 text-[11px] text-muted-foreground hover:bg-accent disabled:opacity-40"
            >
              {t("common.clear")}
            </button>
          </div>
          <div className="scrollbar-thin mt-2 max-h-44 overflow-y-auto">
            {loading ? (
              <div className="py-3 text-center text-[11px] text-muted-foreground">{t("colSelect.loading")}</div>
            ) : filtered.length === 0 ? (
              <div className="py-3 text-center text-[11px] text-muted-foreground">{options.length === 0 ? t("colSelect.noCols") : t("colSelect.noMatch")}</div>
            ) : (
              filtered.map((o) => {
                const disabled = !!o.primaryKey
                const checked = selected.has(o.name.toLowerCase())
                return (
                  <label
                    key={o.name}
                    className={cn(
                      "flex cursor-pointer items-center gap-2 rounded px-1.5 py-1 hover:bg-accent/60",
                      disabled && "cursor-not-allowed opacity-50",
                    )}
                  >
                    <Checkbox
                      checked={checked}
                      disabled={disabled}
                      onCheckedChange={() => toggle(o.name)}
                    />
                    <span className="font-mono text-xs">{o.name}</span>
                    {o.isTime && !disabled && (
                      <Badge variant="outline" className="px-1 py-0 text-[10px] font-normal text-blue-600">{t("colSelect.time")}</Badge>
                    )}
                    {o.primaryKey && (
                      <Badge variant="outline" className="px-1 py-0 text-[10px] font-normal">{t("colSelect.pkLocked")}</Badge>
                    )}
                    <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">{o.dataType}</span>
                  </label>
                )
              })
            )}
          </div>
        </div>
      )}
    </div>
  )
}
