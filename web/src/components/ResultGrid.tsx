import { useEffect, useMemo, useRef, useState } from "react"
import { ArrowDown, ArrowUp, ChevronsUpDown } from "lucide-react"
import { cn } from "@/lib/utils"
import { computeColWidths, renderCell } from "@/lib/table"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import CellEditor from "@/components/CellEditor"
import type { SQLQueryResult } from "@/types"

interface Props {
  result: SQLQueryResult
}

const ROW_H = 32

// 本页排序比较：支持数字/字符串，NULL 始终排最后
function compareCells(a: unknown, b: unknown): number {
  const an = a === null || a === undefined
  const bn = b === null || b === undefined
  if (an && bn) return 0
  if (an) return 1 // NULL 排最后
  if (bn) return -1
  if (typeof a === "number" && typeof b === "number") return a - b
  const sa = String(a)
  const sb = String(b)
  return sa < sb ? -1 : sa > sb ? 1 : 0
}

// 轻量虚拟滚动结果表：仅渲染可视区行（固定行高），支持万行结果流畅滚动。
// 单元格超长截断，悬停 title 查看完整内容；NULL 灰色斜体。支持本页排序（前端内存排序）。
export default function ResultGrid({ result }: Props) {
  const { columns, rows } = result
  const [scrollTop, setScrollTop] = useState(0)
  const [viewH, setViewH] = useState(0)
  const scrollRef = useRef<HTMLDivElement>(null)

  // 排序状态：三态循环（无 → 升序 → 降序 → 无），仅本页内存排序
  const [sortColumn, setSortColumn] = useState("")
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("asc")

  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    setViewH(el.clientHeight)
    const ro = new ResizeObserver(() => setViewH(el.clientHeight))
    ro.observe(el)
    return () => ro.disconnect()
  }, [])

  // 本页排序后的行
  const sortedRows = useMemo(() => {
    if (!sortColumn) return rows
    const ci = columns.findIndex((c) => c.toLowerCase() === sortColumn.toLowerCase())
    if (ci < 0) return rows
    const sorted = [...rows].sort((a, b) => {
      const r = compareCells(a[ci], b[ci])
      return sortOrder === "desc" ? -r : r
    })
    return sorted
  }, [rows, columns, sortColumn, sortOrder])

  const handleSort = (col: string) => {
    if (sortColumn !== col) {
      setSortColumn(col)
      setSortOrder("asc")
    } else if (sortOrder === "asc") {
      setSortOrder("desc")
    } else {
      setSortColumn("")
      setSortOrder("asc")
    }
  }

  const totalH = sortedRows.length * ROW_H
  const start = Math.max(0, Math.floor(scrollTop / ROW_H) - 1)
  const end = Math.min(sortedRows.length, Math.ceil((scrollTop + viewH) / ROW_H) + 1)
  const visible = useMemo(() => sortedRows.slice(start, end), [sortedRows, start, end])
  // 按内容自适应列宽（采样前 100 行估算），避免均分导致的列宽失衡
  const colWidths = useMemo(() => computeColWidths(columns, sortedRows), [columns, sortedRows])

  // 单元格查看状态（只读弹层）
  const [viewing, setViewing] = useState<{ rowIndex: number; colIndex: number } | null>(null)

  if (rows.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
        {columns.length ? "查询成功，无数据" : "暂无结果"}
      </div>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-md border">
      <div
        ref={scrollRef}
        className="scrollbar-thin flex-1 overflow-auto"
        onScroll={(e) => setScrollTop(e.currentTarget.scrollTop)}
      >
        <div style={{ height: totalH }}>
          <table className="w-full border-collapse text-[12px]" style={{ tableLayout: "fixed" }}>
            <thead className="sticky top-0 z-10">
              <tr className="bg-muted text-left">
                {columns.map((c, i) => {
                  const active = sortColumn === c
                  return (
                    <th
                      key={i}
                      className="cursor-pointer select-none border-b border-r px-2 py-1.5 font-medium text-muted-foreground hover:bg-muted/60"
                      style={{ width: `${colWidths[i]}px` }}
                      title={`点击排序：${c}（本页排序）`}
                      onClick={() => handleSort(c)}
                    >
                      <div className="flex items-center gap-1">
                        <span className="min-w-0 flex-1 truncate">{c}</span>
                        {active ? (
                          sortOrder === "asc" ? <ArrowUp className="h-3 w-3 shrink-0 text-primary" /> : <ArrowDown className="h-3 w-3 shrink-0 text-primary" />
                        ) : (
                          <ChevronsUpDown className="h-3 w-3 shrink-0 opacity-0 group-hover:opacity-60" />
                        )}
                      </div>
                    </th>
                  )
                })}
              </tr>
            </thead>
            <tbody>
              {visible.map((row, ri) => (
                <tr key={start + ri} className={cn("border-b", (start + ri) % 2 === 1 && "bg-muted/30")}>
                  {row.map((cell, ci) => {
                    const text = renderCell(cell)
                    return (
                      <td
                        key={ci}
                        className="cursor-pointer border-r px-2 py-1 hover:bg-primary/5"
                        title={`${text}\n（点击查看）`}
                        onClick={() => setViewing({ rowIndex: start + ri, colIndex: ci })}
                      >
                        <div className="truncate">
                          <span className={cn(cell === null || cell === undefined ? "italic text-muted-foreground/70" : "")}>{text}</span>
                        </div>
                      </td>
                    )
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {/* 单元格只读查看弹层（查询结果不可编辑，仅渲染展示） */}
      <Dialog open={viewing !== null} onOpenChange={(open) => !open && setViewing(null)}>
        <DialogContent className="max-w-[720px]">
          <DialogHeader>
            <DialogTitle>查看单元格</DialogTitle>
          </DialogHeader>
          {viewing !== null && columns[viewing.colIndex] !== undefined ? (
            <CellEditor
              column={columns[viewing.colIndex]}
              dataType=""
              value={sortedRows[viewing.rowIndex]?.[viewing.colIndex]}
              nullable
              readonly
              onSave={() => {}}
              onCancel={() => setViewing(null)}
            />
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  )
}
