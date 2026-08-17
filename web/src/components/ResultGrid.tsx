import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react"
import { ArrowDown, ArrowUp, BarChart3, ChevronLeft, ChevronRight, ChevronsUpDown, Columns3, Copy, Download, EyeOff, Filter, Pin, PinOff, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { useClickOutside } from "@/lib/useClickOutside"
import { applyFilters, computeColWidths, computeColumnStat, copyCellValue, copyToClipboard, downloadText, FILTER_OP_LABEL, FILTER_OPS, fmtNum, isNullCell, renderCellText, rowsToCSV, rowToTSV } from "@/lib/table"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import TableIcon from "@/components/ui/table-icon"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import CellEditor from "@/components/CellEditor"
import type { ColumnFilter, FilterOp, SortSpec, SQLQueryResult } from "@/types"

interface Props {
  result: SQLQueryResult
}

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

// 前端分页结果表：查询结果一次性返回后在前端切页。
// 单元格超长截断，悬停 title 查看完整内容；NULL 灰色斜体。支持全量内存排序 + 分页展示。
export default function ResultGrid({ result }: Props) {
  const { columns, rows } = result

  // 分页状态
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(100)
  const [jumpInput, setJumpInput] = useState("")

  // 排序状态：三态循环（无 → 升序 → 降序 → 无），对全量结果内存排序
  // 多列排序状态：Shift+点击叠加，普通点击单列三态循环
  const [sortSpecs, setSortSpecs] = useState<SortSpec[]>([])

  // 列过滤状态（前端内存过滤，AND 叠加）：filterCol 是当前打开过滤面板的列
  const [filters, setFilters] = useState<ColumnFilter[]>([])
  const [filterCol, setFilterCol] = useState<string | null>(null)
  const [filterDraft, setFilterDraft] = useState<{ op: FilterOp; value: string }>({ op: "contains", value: "" })

  // 列显隐状态
  const [hiddenColumns, setHiddenColumns] = useState<Set<string>>(new Set())
  // 冻结列状态：frozenUntil = 冻结边界列名（该列及左侧可见列均冻结）；null = 不冻结
  const [frozenUntil, setFrozenUntil] = useState<string | null>(null)
  const setFrozen = (col: string) => setFrozenUntil(col)
  const clearFrozen = () => setFrozenUntil(null)
  // 列管理面板开关
  const [showColumnPanel, setShowColumnPanel] = useState(false)
  const columnPanelRef = useRef<HTMLDivElement>(null)
  useClickOutside(columnPanelRef, () => setShowColumnPanel(false), showColumnPanel)
  const filterPanelRef = useRef<HTMLDivElement>(null)
  useClickOutside(filterPanelRef, () => setFilterCol(null), filterCol !== null)

  // 列统计开关
  const [showStats, setShowStats] = useState(false)

  // 全量排序后的行
  const sortedRows = useMemo(() => {
    if (sortSpecs.length === 0) return rows
    // 预解析每个排序列的列索引，避免 sort 内重复 findIndex
    const specIdx = sortSpecs
      .map((s) => ({ order: s.order, ci: columns.findIndex((c) => c.toLowerCase() === s.column.toLowerCase()) }))
      .filter((s) => s.ci >= 0)
    if (specIdx.length === 0) return rows
    const sorted = [...rows].sort((a, b) => {
      for (const { order, ci } of specIdx) {
        const r = compareCells(a[ci], b[ci])
        if (r !== 0) return order === "desc" ? -r : r
      }
      return 0
    })
    return sorted
  }, [rows, columns, sortSpecs])

  // 排序后再应用过滤（前端内存过滤，对全量结果生效）
  const filteredRows = useMemo(
    () => applyFilters(columns, sortedRows, filters),
    [columns, sortedRows, filters],
  )

  // 列统计（仅当前过滤+排序后的全量结果，前端内存计算）
  const colStats = useMemo(() => {
    if (!showStats) return []
    return columns.map((_, ci) => computeColumnStat(filteredRows, ci))
  }, [showStats, columns, filteredRows])

  const handleSort = (col: string, shiftKey = false) => {
    setSortSpecs((prev) => {
      const existing = prev.find((s) => s.column === col)
      if (shiftKey) {
        if (!existing) return [...prev, { column: col, order: "asc" }]
        if (existing.order === "asc") {
          return prev.map((s) => (s.column === col ? { column: col, order: "desc" as const } : s))
        }
        return prev.filter((s) => s.column !== col)
      }
      if (!existing) return [{ column: col, order: "asc" }]
      if (existing.order === "asc") return [{ column: col, order: "desc" }]
      return []
    })
    setPage(1)
  }

  // 打开过滤面板
  const openFilterPanel = (col: string) => {
    const existing = filters.find((f) => f.column === col)
    if (existing) {
      setFilterDraft({ op: existing.op, value: existing.value === null || existing.value === undefined ? "" : String(existing.value) })
    } else {
      setFilterDraft({ op: "contains", value: "" })
    }
    setFilterCol(col)
  }

  // 应用/清除/全清过滤（与 TableBrowser 语义一致）
  const applyFilter = (col: string) => {
    const meta = FILTER_OPS.find((f) => f.op === filterDraft.op)
    if (!meta) return
    if (meta.needValue && filterDraft.value.trim() === "") {
      clearColumnFilter(col)
      return
    }
    setFilters((prev) => {
      const next = prev.filter((f) => f.column !== col)
      next.push({ column: col, op: filterDraft.op, value: meta.needValue ? filterDraft.value : undefined })
      return next
    })
    setFilterCol(null)
    setPage(1)
  }
  const clearColumnFilter = (col: string) => {
    setFilters((prev) => prev.filter((f) => f.column !== col))
    setFilterCol(null)
  }
  const clearAllFilters = () => {
    setFilters([])
    setFilterCol(null)
  }
  const quickFilter = (col: string, cell: unknown, op: FilterOp) => {
    const isNullCell = cell === null || cell === undefined
    setFilters((prev) => {
      const next = prev.filter((f) => f.column !== col)
      // NULL 单元格：等于 → IS NULL；不等于/包含 → IS NOT NULL。
      // 避免生成 eq ""（匹配空字符串而非 NULL）造成语义错误。
      const resolvedOp: FilterOp = isNullCell ? (op === "eq" ? "isNull" : "isNotNull") : op
      next.push({ column: col, op: resolvedOp, value: isNullCell ? "" : String(cell) })
      return next
    })
    setPage(1)
  }
  const hasFilter = (col: string) => filters.some((f) => f.column === col)

  // 列显隐：切换某列的隐藏状态
  const toggleColumn = (col: string) => {
    setHiddenColumns((prev) => {
      const next = new Set(prev)
      if (next.has(col)) next.delete(col)
      else next.add(col)
      return next
    })
  }

  // 可见列：{ 列名, 原始列索引 }
  const visibleCols = useMemo(
    () => columns.map((name, idx) => ({ name, idx })).filter((c) => !hiddenColumns.has(c.name)),
    [columns, hiddenColumns],
  )

  const pages = filteredRows.length > 0 ? Math.max(1, Math.ceil(filteredRows.length / pageSize)) : 1
  // 当分页大小变化或结果缩小导致当前页越界时，回退到最后一页
  const safePage = Math.min(page, pages)
  const start = (safePage - 1) * pageSize
  const pageRows = useMemo(() => filteredRows.slice(start, start + pageSize), [filteredRows, start, pageSize])

  // 按内容自适应列宽：基于未排序的原始行采样，避免排序改变样本内容导致列宽跳动
  const colWidths = useMemo(() => computeColWidths(columns, rows), [columns, rows])
  // 表格最小宽度 = 各列宽之和：列多时溢出产生横向滚动（sticky 冻结生效），列少时仍填满容器
  const tableMinWidth = useMemo(() => colWidths.reduce((a, b) => a + b, 0), [colWidths])

  // 冻结列 → 左侧偏移 px（无 checkbox 列，从 0 开始）。
  // 冻结边界列被隐藏时视为无冻结，避免冻结范围意外扩大。
  const frozenOffsets = useMemo(() => {
    const map = new Map<string, number>()
    if (frozenUntil === null || !visibleCols.some((v) => v.name === frozenUntil)) return map
    let acc = 0
    for (const { name, idx } of visibleCols) {
      map.set(name, acc)
      if (name === frozenUntil) break
      acc += colWidths[idx] ?? 96
    }
    return map
  }, [visibleCols, colWidths, frozenUntil])

  // 单元格查看状态（只读弹层）
  const [viewing, setViewing] = useState<{ rowIndex: number; colIndex: number } | null>(null)
  // 右键菜单：状态放在子组件内部（menuRef 驱动），右键时不再触发整表重渲染
  const cellMenuRef = useRef<CellMenuHandle>(null)

  // ---- 键盘导航：聚焦单元格（rowIndex 为「当前页可见行序」；colIndex 为「可见列序」）----
  const [focusedCell, setFocusedCell] = useState<{ rowIndex: number; colIndex: number } | null>(null)
  // 可见列的原始索引列表（把「可见列序」映射回「原始列索引」）
  const visibleColIdxList = useMemo(() => visibleCols.map((v) => v.idx), [visibleCols])
  const gridScrollRef = useRef<HTMLDivElement>(null)

  // 聚焦单元格变化后：滚动到视区，避免被左侧冻结列遮挡（无 checkbox 列，冻结区从 0 开始）
  useEffect(() => {
    if (!focusedCell) return
    const container = gridScrollRef.current
    if (!container) return
    const rawCol = visibleColIdxList[focusedCell.colIndex]
    if (rawCol === undefined) return
    const cellEl = container.querySelector<HTMLTableCellElement>(`td[data-grid-cell="${focusedCell.rowIndex}:${rawCol}"]`)
    if (!cellEl) return
    const containerRect = container.getBoundingClientRect()
    const cellRect = cellEl.getBoundingClientRect()
    // 冻结区右边界：最后一个冻结列的 left + 该列宽
    const frozenCols = visibleCols.filter((v) => frozenOffsets.has(v.name))
    let frozenZoneWidth = 0
    for (const v of frozenCols) {
      const left = frozenOffsets.get(v.name) ?? 0
      const w = colWidths[v.idx] ?? 96
      frozenZoneWidth = Math.max(frozenZoneWidth, left + w)
    }
    const isFrozenCol = frozenOffsets.has(visibleCols[focusedCell.colIndex]?.name ?? "")
    if (!isFrozenCol) {
      const targetLeft = cellRect.left - containerRect.left + container.scrollLeft
      const wantLeft = frozenZoneWidth
      if (targetLeft < wantLeft || targetLeft + cellRect.width > container.clientWidth + container.scrollLeft) {
        container.scrollLeft = targetLeft - wantLeft
      }
    }
    const targetTop = cellRect.top - containerRect.top + container.scrollTop
    if (targetTop < container.scrollTop || targetTop + cellRect.height > container.scrollTop + container.clientHeight) {
      container.scrollTop = targetTop
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusedCell])

  // 数据变化导致聚焦越界时清除聚焦
  useEffect(() => {
    if (!focusedCell) return
    if (focusedCell.rowIndex >= pageRows.length || focusedCell.colIndex >= visibleColIdxList.length) {
      setFocusedCell(null)
    }
  }, [pageRows.length, visibleColIdxList.length, focusedCell])

  // 聚焦单元格键盘处理
  const handleGridKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (visibleColIdxList.length === 0) return
      if (!focusedCell) {
        if (["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight", "Enter"].includes(e.key) && pageRows.length > 0) {
          e.preventDefault()
          setFocusedCell({ rowIndex: 0, colIndex: 0 })
        }
        return
      }
      const rowCount = pageRows.length
      const colCount = visibleColIdxList.length
      const { rowIndex, colIndex } = focusedCell
      const move = (nr: number, nc: number) => {
        const rr = Math.min(Math.max(0, nr), Math.max(0, rowCount - 1))
        const cc = Math.min(Math.max(0, nc), Math.max(0, colCount - 1))
        setFocusedCell({ rowIndex: rr, colIndex: cc })
      }
      switch (e.key) {
        case "c":
        case "C":
          // Ctrl/Cmd+C 复制当前聚焦单元格值
          if (e.metaKey || e.ctrlKey) {
            e.preventDefault()
            const rawCol = visibleColIdxList[colIndex]
            const row = pageRows[rowIndex]
            if (rawCol !== undefined && row) {
              copyToClipboard(copyCellValue(row[rawCol]))
            }
          }
          break
        case "ArrowDown":
          e.preventDefault()
          move(rowIndex + 1, colIndex)
          break
        case "ArrowUp":
          e.preventDefault()
          move(rowIndex - 1, colIndex)
          break
        case "ArrowRight":
          e.preventDefault()
          move(rowIndex, colIndex + 1)
          break
        case "ArrowLeft":
          e.preventDefault()
          move(rowIndex, colIndex - 1)
          break
        case "Home":
          e.preventDefault()
          move(rowIndex, 0)
          break
        case "End":
          e.preventDefault()
          move(rowIndex, colCount - 1)
          break
        case "Enter": {
          // 只读查看当前单元格
          e.preventDefault()
          const rawCol = visibleColIdxList[colIndex]
          if (rawCol === undefined) break
          setViewing({ rowIndex: start + rowIndex, colIndex: rawCol })
          break
        }
        case "Escape":
          e.preventDefault()
          setFocusedCell(null)
          break
        default:
          break
      }
    },
    [focusedCell, pageRows, visibleColIdxList, start],
  )

  const goTo = (p: number) => setPage(Math.min(Math.max(1, p), pages))

  const commitJump = () => {
    const n = parseInt(jumpInput, 10)
    if (Number.isFinite(n)) goTo(n)
    setJumpInput("")
  }

  // 计算可点选的页码列表（首尾各 1 页 + 当前页前后各 2 页，中间用省略号）
  const pageItems = useMemo(() => {
    if (pages <= 1) return []
    const range: (number | "…")[] = []
    const sibling = 2
    const left = Math.max(2, safePage - sibling)
    const right = Math.min(pages - 1, safePage + sibling)
    range.push(1)
    if (left > 2) range.push("…")
    for (let p = left; p <= right; p++) range.push(p)
    if (right < pages - 1) range.push("…")
    if (pages > 1) range.push(pages)
    return range
  }, [safePage, pages])

  // 仅当「无列定义」时才显示全屏占位；只要有列（即使行被过滤空），仍渲染表头，
  // 保证用户可通过表头漏斗/列头右键清除或修改过滤条件，不被「无数据」卡死。
  if (columns.length === 0) {
    return (
      <div className="flex h-full items-center justify-center text-sm text-muted-foreground">暂无结果</div>
    )
  }

  return (
    <div className="flex h-full min-h-0 flex-col overflow-hidden rounded-md border">
      {/* 过滤状态条：显式提示已应用的过滤条件，避免用户误以为数据丢失 */}
      {filters.length > 0 && (
        <div className="flex shrink-0 flex-wrap items-center gap-1.5 border-b border-primary/20 bg-primary/5 px-2 py-1.5">
          <Filter className="h-3.5 w-3.5 shrink-0 text-primary" />
          <span className="text-xs font-medium text-primary">已应用 {filters.length} 个过滤条件</span>
          {filters.map((f, i) => (
            <span key={i} className="flex items-center gap-1 rounded bg-primary/10 px-1.5 py-0.5 text-xs text-primary">
              <span className="font-medium">{f.column}</span>
              <span className="text-muted-foreground">{FILTER_OP_LABEL[f.op]}</span>
              {f.value !== undefined && f.value !== null && f.value !== "" && (
                <span className="max-w-[120px] truncate">“{String(f.value)}”</span>
              )}
              <button type="button" className="ml-0.5 text-muted-foreground hover:text-foreground" onClick={() => clearColumnFilter(f.column)}>
                <X className="h-3 w-3" />
              </button>
            </span>
          ))}
          <button type="button" className="ml-1 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline" onClick={clearAllFilters}>
            清除全部
          </button>
        </div>
      )}
      <div
        ref={gridScrollRef}
        className="scrollbar-thin min-h-0 flex-1 overflow-auto outline-none"
        tabIndex={0}
        onKeyDown={handleGridKeyDown}
        onBlur={() => setFocusedCell(null)}
      >
        {/* 容器级单实例右键菜单：避免每行一个 ContextMenu 组件树导致右键时整表重渲染变慢 */}
        <ContextMenu onOpenChange={(open) => { if (!open) cellMenuRef.current?.hide() }}>
          <ContextMenuTrigger asChild>
            <table className="data-grid-table w-full border-separate border-spacing-0 text-[12px]" style={{ tableLayout: "fixed", minWidth: tableMinWidth }}>
          <thead>
            <tr className="bg-muted text-left" onContextMenu={(e) => e.stopPropagation()}>
              {visibleCols.map(({ name: c, idx: i }) => {
                const sortSpec = sortSpecs.find((s) => s.column === c)
                const sortIdx = sortSpec ? sortSpecs.findIndex((s) => s.column === c) : -1
                const filtered = hasFilter(c)
                const frozenLeft = frozenOffsets.get(c)
                return (
                  <ContextMenu key={i}>
                    <ContextMenuTrigger asChild>
                      <th
                        className={cn(
                          "sticky top-0 z-20 cursor-pointer select-none bg-muted px-2 py-1.5 font-medium text-muted-foreground hover:bg-muted/60",
                          frozenLeft !== undefined && "sticky left-0 top-0 z-30 bg-muted frozen-col",
                        )}
                        style={{ width: `${colWidths[i]}px`, ...(frozenLeft !== undefined ? { left: frozenLeft } : {}) }}
                        title={`点击排序：${c}（Shift+点击叠加多列排序）`}
                        onClick={(e) => handleSort(c, e.shiftKey)}
                      >
                        <div className="flex items-center gap-1">
                          <span className="min-w-0 flex-1 truncate">{c}</span>
                          {sortSpec ? (
                            <>
                              {sortSpec.order === "asc" ? <TableIcon icon={ArrowUp} className="text-primary" /> : <TableIcon icon={ArrowDown} className="text-primary" />}
                              {sortSpecs.length > 1 && <span className="text-[10px] font-semibold text-primary">{sortIdx + 1}</span>}
                            </>
                          ) : (
                            <TableIcon icon={ChevronsUpDown} className="opacity-0 group-hover:opacity-60" />
                          )}
                          <button
                            type="button"
                            className={cn(
                              "flex h-4 w-4 shrink-0 items-center justify-center rounded hover:bg-primary/10",
                              filtered ? "text-primary" : "text-muted-foreground/50 hover:text-muted-foreground",
                            )}
                            title={filtered ? "已过滤，点击编辑" : "筛选此列"}
                            onClick={(e) => {
                              e.stopPropagation()
                              openFilterPanel(c)
                            }}
                          >
                            <TableIcon icon={Filter} size={12} />
                          </button>
                        </div>
                        {/* 过滤面板 */}
                        {filterCol === c && (
                          <div
                            ref={filterPanelRef}
                            className="absolute right-0 top-full z-30 mt-1 w-56 rounded-md border bg-popover p-2 shadow-md"
                            onClick={(e) => e.stopPropagation()}
                          >
                            <div className="mb-1.5 flex items-center justify-between">
                              <span className="truncate text-xs font-semibold text-foreground">{c}</span>
                              <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => setFilterCol(null)}>
                                <X className="h-3.5 w-3.5" />
                              </button>
                            </div>
                            <Select value={filterDraft.op} onValueChange={(v) => setFilterDraft((d) => ({ ...d, op: v as FilterOp }))}>
                              <SelectTrigger className="h-7 w-full text-xs">
                                <SelectValue />
                              </SelectTrigger>
                              <SelectContent>
                                {FILTER_OPS.map((f) => (
                                  <SelectItem key={f.op} value={f.op}>{f.label}</SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            {FILTER_OPS.find((f) => f.op === filterDraft.op)?.needValue && (
                              <Input
                                className="mt-1.5 h-7 text-xs"
                                placeholder="输入过滤值"
                                value={filterDraft.value}
                                autoFocus
                                onChange={(e) => setFilterDraft((d) => ({ ...d, value: e.target.value }))}
                                onKeyDown={(e) => e.key === "Enter" && applyFilter(c)}
                              />
                            )}
                            <div className="mt-2 flex justify-end gap-1">
                              {filtered && (
                                <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => clearColumnFilter(c)}>
                                  清除
                                </Button>
                              )}
                              <Button size="sm" className="h-7 px-2 text-xs" onClick={() => applyFilter(c)}>
                                应用
                              </Button>
                            </div>
                          </div>
                        )}
                      </th>
                    </ContextMenuTrigger>
                    <ContextMenuContent>
                      <ContextMenuItem onSelect={() => { setSortSpecs((p) => p.some((s) => s.column === c && s.order === "asc") ? p.filter((s) => s.column !== c) : [...p.filter((s) => s.column !== c), { column: c, order: "asc" }]); setPage(1) }}>
                        <ArrowUp className="mr-2 h-3.5 w-3.5" /> 升序排序
                      </ContextMenuItem>
                      <ContextMenuItem onSelect={() => { setSortSpecs((p) => p.some((s) => s.column === c && s.order === "desc") ? p.filter((s) => s.column !== c) : [...p.filter((s) => s.column !== c), { column: c, order: "desc" }]); setPage(1) }}>
                        <ArrowDown className="mr-2 h-3.5 w-3.5" /> 降序排序
                      </ContextMenuItem>
                      {sortSpec && (
                        <ContextMenuItem onSelect={() => setSortSpecs((p) => p.filter((s) => s.column !== c))}>
                          <ChevronsUpDown className="mr-2 h-3.5 w-3.5" /> 取消排序
                        </ContextMenuItem>
                      )}
                      <ContextMenuSeparator />
                      <ContextMenuItem onSelect={() => openFilterPanel(c)}>
                        <Filter className="mr-2 h-3.5 w-3.5" /> 筛选此列
                      </ContextMenuItem>
                      <ContextMenuItem onSelect={() => { setFilters((p) => p.filter((f) => f.column !== c).concat({ column: c, op: "isNull" })); setPage(1) }}>
                        <Filter className="mr-2 h-3.5 w-3.5" /> 只看空值
                      </ContextMenuItem>
                      <ContextMenuItem onSelect={() => { setFilters((p) => p.filter((f) => f.column !== c).concat({ column: c, op: "isNotNull" })); setPage(1) }}>
                        <Filter className="mr-2 h-3.5 w-3.5" /> 只看非空
                      </ContextMenuItem>
                      <ContextMenuItem onSelect={() => toggleColumn(c)}>
                        <EyeOff className="mr-2 h-3.5 w-3.5" /> 隐藏此列
                      </ContextMenuItem>
                      <ContextMenuSeparator />
                      {/* 冻结列：固定到当前列（含左侧所有可见列）；右键当前边界列时显示「取消固定」 */}
                      {frozenUntil === c ? (
                        <ContextMenuItem onSelect={clearFrozen}>
                          <PinOff className="mr-2 h-3.5 w-3.5" /> 取消固定
                        </ContextMenuItem>
                      ) : (
                        <>
                          <ContextMenuItem onSelect={() => setFrozen(c)}>
                            <Pin className="mr-2 h-3.5 w-3.5" /> 固定到此列
                          </ContextMenuItem>
                          {frozenUntil !== null && (
                            <ContextMenuItem onSelect={clearFrozen}>
                              <PinOff className="mr-2 h-3.5 w-3.5" /> 取消固定
                            </ContextMenuItem>
                          )}
                        </>
                      )}
                      <ContextMenuItem onSelect={() => copyToClipboard(c)}>
                        <Copy className="mr-2 h-3.5 w-3.5" /> 复制列名
                      </ContextMenuItem>
                    </ContextMenuContent>
                  </ContextMenu>
                )
              })}
            </tr>
          </thead>
          <tbody>
            {pageRows.length === 0 ? (
              <tr onContextMenu={(e) => e.stopPropagation()}>
                <td colSpan={columns.length} className="px-2 py-8 text-center text-sm text-muted-foreground">
                  无匹配数据（已应用 {filters.length} 个过滤条件）
                </td>
              </tr>
            ) : (
              pageRows.map((row, ri) => (
                <tr key={start + ri}>
                      {visibleCols.map(({ name: colName, idx: ci }) => {
                        const cell = row[ci]
                        const text = renderCellText(cell)
                        const frozenLeft = frozenOffsets.get(colName)
                        return (
                          <td
                            key={ci}
                            data-grid-cell={`${ri}:${ci}`}
                            className={cn(
                              "cursor-pointer px-2 py-1",
                              // 冻结列：sticky 悬浮在其他列上方，边框由 .frozen-col 统一管理
                              frozenLeft !== undefined && "sticky left-0 z-10 frozen-col",
                              // 键盘聚焦：外描边高亮
                              focusedCell?.rowIndex === ri && focusedCell?.colIndex === visibleColIdxList.indexOf(ci) && "ring-2 ring-inset ring-primary",
                            )}
                            style={{
                              ...(frozenLeft !== undefined ? { left: frozenLeft } : {}),
                              // 所有单元格背景色统一内联绝对颜色（斑马 > 默认）
                              backgroundColor: (start + ri) % 2 === 1 ? "#f8fafc" : "#ffffff",
                            }}
                            title={`${text}\n（点击查看）`}
                            onClick={() => {
                              setFocusedCell({ rowIndex: ri, colIndex: visibleColIdxList.indexOf(ci) })
                              setViewing({ rowIndex: start + ri, colIndex: ci })
                            }}
                            onContextMenu={() => {
                              cellMenuRef.current?.show({ rowIndex: start + ri, colIndex: ci })
                            }}
                          >
                            <div className="truncate">
                              <span className={cn(
                                isNullCell(cell) && "italic text-muted-foreground/70",
                                typeof cell === "string" && cell === "" && "text-muted-foreground/70",
                              )}>{text}</span>
                            </div>
                          </td>
                        )
                      })}
                    </tr>
              ))
            )}
          </tbody>
          {showStats && (
            <tfoot onContextMenu={(e) => e.stopPropagation()}>
              <tr>
                {visibleCols.map(({ name: colName, idx: ci }) => {
                  const st = colStats[ci]
                  const frozenLeft = frozenOffsets.get(colName)
                  if (!st) {
                    return (
                      <td
                        key={ci}
                        className={cn(
                          "sticky bottom-0 z-20 bg-muted px-2 py-1 text-[11px] text-muted-foreground",
                          frozenLeft !== undefined && "sticky left-0 bottom-0 z-30 bg-muted frozen-col",
                        )}
                        style={frozenLeft !== undefined ? { left: frozenLeft } : undefined}
                      >
                        —
                      </td>
                    )
                  }
                  return (
                    <td
                      key={ci}
                      className={cn(
                        "sticky bottom-0 z-20 bg-muted px-2 py-1 text-[11px] text-muted-foreground",
                        frozenLeft !== undefined && "sticky left-0 bottom-0 z-30 bg-muted frozen-col",
                      )}
                      style={frozenLeft !== undefined ? { left: frozenLeft } : undefined}
                    >
                      {st.numeric ? (
                        <span className="tabular-nums">
                          Σ {fmtNum(st.sum)} · avg {fmtNum(st.avg)}
                          {st.nullCount > 0 ? ` · ${st.nullCount} 空` : ""}
                        </span>
                      ) : (
                        <span className="tabular-nums">{st.nullCount > 0 ? `${st.nullCount} 空` : "—"}</span>
                      )}
                    </td>
                  )
                })}
              </tr>
            </tfoot>
          )}
        </table>
          </ContextMenuTrigger>
          <ResultGridCellMenu ref={cellMenuRef} columns={columns} rows={filteredRows} onQuickFilter={quickFilter} />
        </ContextMenu>
      </div>

      {/* 分页条 */}
      <div className="flex shrink-0 flex-wrap items-center justify-between gap-2 border-t px-2 py-1.5">
        <div className="flex items-center gap-2">
          <span className="text-xs tabular-nums text-muted-foreground">
            共 {filteredRows.length} 行
          </span>
          <Button
            variant="outline"
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            disabled={filteredRows.length === 0}
            onClick={() =>
              downloadText(
                `query-result-${Date.now()}.csv`,
                rowsToCSV(
                  visibleCols.map((v) => v.name),
                  filteredRows.map((r) => visibleCols.map((v) => r[v.idx])),
                ),
              )
            }
          >
            <Download className="h-3.5 w-3.5" /> 导出 CSV
          </Button>
          <Button
            variant={showStats ? "secondary" : "outline"}
            size="sm"
            className="h-7 gap-1 px-2 text-xs"
            onClick={() => setShowStats((v) => !v)}
          >
            <BarChart3 className="h-3.5 w-3.5" /> 统计
          </Button>
          {/* 列管理：显示/隐藏列（隐藏列后从这里重新显示） */}
          <div className="relative" ref={columnPanelRef}>
            <Button
              variant={hiddenColumns.size > 0 || showColumnPanel ? "secondary" : "outline"}
              size="sm"
              className="h-7 gap-1 px-2 text-xs"
              onClick={() => setShowColumnPanel((v) => !v)}
            >
              <Columns3 className="h-3.5 w-3.5" /> 列
              {hiddenColumns.size > 0 && <span className="rounded bg-muted px-1 text-[10px]">{hiddenColumns.size}</span>}
            </Button>
            {showColumnPanel && (
              <div className="absolute bottom-full left-0 z-30 mb-1 max-h-80 w-64 overflow-auto rounded-md border bg-popover p-1.5 shadow-md">
                <div className="mb-1 flex items-center justify-between px-1">
                  <span className="text-xs font-semibold text-foreground">显示列</span>
                  <button type="button" className="text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline" onClick={() => setHiddenColumns(new Set())}>
                    全部显示
                  </button>
                </div>
                {columns.map((col) => {
                  const visible = !hiddenColumns.has(col)
                  return (
                    <label key={col} className="flex cursor-pointer items-center gap-1.5 rounded px-1 py-1 text-xs hover:bg-muted/60">
                      <Checkbox checked={visible} onCheckedChange={() => toggleColumn(col)} />
                      <span className={cn("min-w-0 flex-1 truncate", !visible && "text-muted-foreground")}>{col}</span>
                    </label>
                  )
                })}
              </div>
            )}
          </div>
          <Select value={String(pageSize)} onValueChange={(v) => { setPageSize(Number(v)); setPage(1) }}>
            <SelectTrigger className="h-7 w-[70px] px-2 text-xs">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {[50, 100, 200, 500, 1000].map((s) => (
                <SelectItem key={s} value={String(s)}>{s} 行/页</SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>

        <div className="flex items-center gap-1">
          <Button variant="outline" size="sm" className="h-7 w-7 p-0" disabled={safePage <= 1} onClick={() => goTo(safePage - 1)}>
            <ChevronLeft className="h-3.5 w-3.5" />
          </Button>

          {pageItems.map((item, i) =>
            item === "…" ? (
              <span key={`e-${i}`} className="px-1 text-xs text-muted-foreground">…</span>
            ) : (
              <Button
                key={item}
                variant={item === safePage ? "default" : "outline"}
                size="sm"
                className="h-7 min-w-7 px-1.5 text-xs"
                onClick={() => goTo(item)}
              >
                {item}
              </Button>
            )
          )}

          <Button variant="outline" size="sm" className="h-7 w-7 p-0" disabled={safePage >= pages} onClick={() => goTo(safePage + 1)}>
            <ChevronRight className="h-3.5 w-3.5" />
          </Button>

          <div className="ml-2 flex items-center gap-1">
            <span className="text-xs text-muted-foreground">跳至</span>
            <Input
              className="h-7 w-14 px-1.5 text-center text-xs tabular-nums"
              value={jumpInput}
              placeholder={String(safePage)}
              inputMode="numeric"
              onChange={(e) => setJumpInput(e.target.value.replace(/[^0-9]/g, ""))}
              onKeyDown={(e) => e.key === "Enter" && commitJump()}
              onBlur={commitJump}
            />
            <span className="text-xs text-muted-foreground">/ {pages} 页</span>
          </div>
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
              value={filteredRows[viewing.rowIndex]?.[viewing.colIndex]}
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

// 右键单元格菜单句柄：父组件通过 ref 调用，右键状态只在本组件内更新，
// 避免右键时触发整个表格组件重渲染（大表重渲染是右键弹出慢的根因）
type CellMenuHandle = { show: (cell: { rowIndex: number; colIndex: number }) => void; hide: () => void }

const ResultGridCellMenu = forwardRef<CellMenuHandle, {
  columns: string[]
  rows: unknown[][]
  onQuickFilter: (col: string, cell: unknown, op: FilterOp) => void
}>(({ columns, rows, onQuickFilter }, ref) => {
  const [cell, setCell] = useState<{ rowIndex: number; colIndex: number } | null>(null)
  useImperativeHandle(ref, () => ({
    show: (c) => setCell(c),
    hide: () => setCell(null),
  }))
  return (
    <ContextMenuContent>
      {cell !== null && cell.colIndex < columns.length ? (
        (() => {
          const col = columns[cell.colIndex]
          const cellVal = rows[cell.rowIndex]?.[cell.colIndex]
          const fullRow = rows[cell.rowIndex] ?? []
          const val = cellVal === null || cellVal === undefined ? "NULL" : String(cellVal)
          return (
            <>
              <ContextMenuItem onSelect={() => copyToClipboard(copyCellValue(cellVal))}>
                <Copy className="mr-2 h-3.5 w-3.5" /> 复制单元格值
              </ContextMenuItem>
              <ContextMenuItem onSelect={() => copyToClipboard(rowToTSV(fullRow))}>
                <Copy className="mr-2 h-3.5 w-3.5" /> 复制整行 (TSV)
              </ContextMenuItem>
              <ContextMenuItem onSelect={() => copyToClipboard(col)}>
                <Copy className="mr-2 h-3.5 w-3.5" /> 复制列名
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem onSelect={() => onQuickFilter(col, cellVal, "eq")}>
                <Filter className="mr-2 h-3.5 w-3.5" /> 等于此值：<span className="ml-1 max-w-[120px] truncate text-muted-foreground">{val}</span>
              </ContextMenuItem>
              <ContextMenuItem onSelect={() => onQuickFilter(col, cellVal, "neq")}>
                <Filter className="mr-2 h-3.5 w-3.5" /> 不等于此值
              </ContextMenuItem>
              <ContextMenuItem onSelect={() => onQuickFilter(col, cellVal, "contains")}>
                <Filter className="mr-2 h-3.5 w-3.5" /> 包含此值
              </ContextMenuItem>
            </>
          )
        })()
      ) : (
        <ContextMenuItem disabled>未选中单元格</ContextMenuItem>
      )}
    </ContextMenuContent>
  )
})
