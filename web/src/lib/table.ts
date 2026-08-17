// 表格列宽自适应工具：按内容（列名 + 采样行）估算每列显示宽度，
// 供结果网格（ResultGrid）与表浏览（TableBrowser）共用，避免重复实现。

import type { ColumnFilter, FilterOp } from "@/types"

const MIN_COL_W = 80 // 列最小宽度 px
const MAX_COL_W = 420 // 列最大宽度 px
const CELL_PADDING = 24 // 单元格左右 padding + 截断余量

// 估算字符串显示宽度：ASCII 字符按 1，CJK/全角按 2（等宽字体近似）
export function textWidth(s: string): number {
  let w = 0
  for (const ch of s) {
    const cp = ch.codePointAt(0) ?? 0
    // CJK 统一表意文字、全角标点、假名等按 2 倍宽
    w += cp > 0x2e80 ? 2 : 1
  }
  return w
}

// 单元格值 → 展示文本
export function renderCell(v: unknown): string {
  if (v === null || v === undefined) return "NULL"
  if (typeof v === "object") {
    try {
      return JSON.stringify(v)
    } catch {
      return String(v)
    }
  }
  return String(v)
}

// 单元格值 → 展示文本（区分 NULL 与空字符串）。
// NULL → "NULL"；空字符串 → "(空)"（区别于 NULL，避免数据核对误判）。
export function renderCellText(v: unknown): string {
  if (v === null || v === undefined) return "NULL"
  if (typeof v === "string" && v === "") return "(空)"
  if (typeof v === "object") {
    try {
      return JSON.stringify(v)
    } catch {
      return String(v)
    }
  }
  return String(v)
}

// 是否为 NULL（供渲染判断：NULL 用灰色斜体，空串用 (空) 标记）
export function isNullCell(v: unknown): boolean {
  return v === null || v === undefined
}

// ---- 复制共享逻辑（ResultGrid / TableBrowser 共用）----

// 单元格值 → 复制文本（复制时 NULL 复制为字面 "NULL"，空串复制为 ""）
export function copyCellValue(v: unknown): string {
  if (v === null || v === undefined) return "NULL"
  if (typeof v === "object") {
    try {
      return JSON.stringify(v)
    } catch {
      return String(v)
    }
  }
  return String(v)
}

// 一行数据 → TSV 文本（列间 tab 分隔，供「复制行」）
export function rowToTSV(row: unknown[]): string {
  return row.map(copyCellValue).join("\t")
}

// 多行 + 列名 → TSV 文本（首行列名，供「复制选中行」/拖选复制）
export function rowsToTSV(columns: string[], rows: unknown[][]): string {
  const header = columns.join("\t")
  const body = rows.map(rowToTSV)
  return [header, ...body].join("\n")
}

// 复制文本到剪贴板（失败时降级为隐藏 textarea + execCommand）
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    if (navigator.clipboard && window.isSecureContext) {
      await navigator.clipboard.writeText(text)
      return true
    }
  } catch {
    // 降级到 textarea 方案
  }
  try {
    const ta = document.createElement("textarea")
    ta.value = text
    ta.style.position = "fixed"
    ta.style.opacity = "0"
    document.body.appendChild(ta)
    ta.select()
    const ok = document.execCommand("copy")
    document.body.removeChild(ta)
    return ok
  } catch {
    return false
  }
}

// ---- 导出共享逻辑（CSV 前端生成 / Excel 后端）----

// CSV 单元格转义：含逗号/引号/换行时用引号包裹，引号翻倍
function csvEscape(v: unknown): string {
  const s = copyCellValue(v)
  if (s.includes(",") || s.includes('"') || s.includes("\n") || s.includes("\r")) {
    return `"${s.replace(/"/g, '""')}"`
  }
  return s
}

// 列名 + 多行 → CSV 文本（含 BOM，供 Excel 正确识别 UTF-8）
export function rowsToCSV(columns: string[], rows: unknown[][]): string {
  const header = columns.map(csvEscape).join(",")
  const body = rows.map((r) => r.map(csvEscape).join(","))
  return "\ufeff" + [header, ...body].join("\r\n")
}

// 触发浏览器下载：Blob + a[download]
export function downloadText(filename: string, text: string, mime = "text/csv;charset=utf-8") {
  const blob = new Blob([text], { type: mime })
  const url = URL.createObjectURL(blob)
  const a = document.createElement("a")
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

// ---- 列统计（仅当前可见数据，避免全表聚合开销与误导）----

export interface ColumnStat {
  nullCount: number   // NULL 计数
  numeric: boolean    // 是否数值列（可算 sum/avg）
  sum?: number        // 数值列求和
  avg?: number        // 数值列平均
  min?: number        // 数值列最小值
  max?: number        // 数值列最大值
}

// 数值格式化：千分位 + 最多 2 位小数（用于统计展示）
export function fmtNum(n: number | undefined): string {
  if (n === undefined || !Number.isFinite(n)) return "—"
  const rounded = Math.round(n * 100) / 100
  return rounded.toLocaleString("zh-CN", { maximumFractionDigits: 2 })
}

// 对 rows 的某一列（列索引 ci）做统计。数值判断：该列所有非 NULL 值均可转 number 时视为数值列。
export function computeColumnStat(rows: unknown[][], ci: number): ColumnStat {
  let nullCount = 0
  let numeric = true
  let sum = 0
  let count = 0
  let min = Infinity
  let max = -Infinity
  for (const row of rows) {
    const v = row[ci]
    if (v === null || v === undefined) {
      nullCount++
      continue
    }
    const n = Number(v)
    if (numeric && (typeof v === "number" || (typeof v === "string" && v.trim() !== "" && Number.isFinite(n)))) {
      sum += n
      count++
      if (n < min) min = n
      if (n > max) max = n
    } else if (typeof v !== "number" && !(typeof v === "string" && v.trim() !== "" && Number.isFinite(n))) {
      numeric = false
    }
  }
  if (!numeric || count === 0) {
    return { nullCount, numeric: false }
  }
  return {
    nullCount,
    numeric: true,
    sum,
    avg: sum / count,
    min: min === Infinity ? undefined : min,
    max: max === -Infinity ? undefined : max,
  }
}

// 采样前 N 行 + 列名，估算每列内容最大字符宽度 → 计算列宽（px）。
// 每字符约 7px（12px 等宽字体的近似），加 padding 余量，夹在 min/max 之间。
export function computeColWidths(columns: string[], rows: unknown[][], sampleN = 100): number[] {
  const n = columns.length
  const maxChars = columns.map((c) => textWidth(c))
  const sample = rows.slice(0, sampleN)
  for (const row of sample) {
    for (let i = 0; i < n; i++) {
      const w = textWidth(renderCell(row[i]))
      if (w > maxChars[i]) maxChars[i] = w
    }
  }
  return maxChars.map((c) => Math.min(MAX_COL_W, Math.max(MIN_COL_W, c * 7 + CELL_PADDING)))
}

// ---- 列过滤共享逻辑（TableBrowser 后端过滤 / ResultGrid 前端过滤共用）----

// 过滤操作符元数据：文案 + 是否需要输入值（isNull/isNotNull 无需值）
export const FILTER_OPS: { op: FilterOp; label: string; needValue: boolean }[] = [
  { op: "eq", label: "等于", needValue: true },
  { op: "neq", label: "不等于", needValue: true },
  { op: "contains", label: "包含", needValue: true },
  { op: "notContains", label: "不包含", needValue: true },
  { op: "startsWith", label: "开头是", needValue: true },
  { op: "endsWith", label: "结尾是", needValue: true },
  { op: "gt", label: "大于", needValue: true },
  { op: "gte", label: "大于等于", needValue: true },
  { op: "lt", label: "小于", needValue: true },
  { op: "lte", label: "小于等于", needValue: true },
  { op: "isNull", label: "为空", needValue: false },
  { op: "isNotNull", label: "非空", needValue: false },
]

// 过滤操作符文案映射（用于已应用过滤的 chips 展示）
export const FILTER_OP_LABEL: Record<FilterOp, string> = Object.fromEntries(
  FILTER_OPS.map((f) => [f.op, f.label]),
) as Record<FilterOp, string>

// 单值匹配：判断 cell 是否满足单个过滤条件（与后端 cydb 语义对齐）。
// 数值比较在字符串层面退化处理（前端内存过滤无法知晓列类型，采用宽松比较）。
export function matchesFilter(cell: unknown, op: FilterOp, value: unknown): boolean {
  const isNull = cell === null || cell === undefined
  switch (op) {
    case "isNull":
      return isNull
    case "isNotNull":
      return !isNull
  }
  if (isNull) return false // 其余操作符对 NULL 恒不匹配（与 SQL 语义一致）
  const cs = String(cell)
  const vs = value === null || value === undefined ? "" : String(value)
  switch (op) {
    case "eq":
      return cs === vs
    case "neq":
      return cs !== vs
    case "contains":
      return cs.includes(vs)
    case "notContains":
      return !cs.includes(vs)
    case "startsWith":
      return cs.startsWith(vs)
    case "endsWith":
      return cs.endsWith(vs)
    case "gt":
      return compareLoose(cell, value) > 0
    case "gte":
      return compareLoose(cell, value) >= 0
    case "lt":
      return compareLoose(cell, value) < 0
    case "lte":
      return compareLoose(cell, value) <= 0
    default:
      return false
  }
}

// 宽松比较：数值优先，非数值退化为字符串比较（NULL 已在上层排除）
function compareLoose(a: unknown, b: unknown): number {
  const an = Number(a)
  const bn = Number(b)
  if (Number.isFinite(an) && Number.isFinite(bn)) return an - bn
  const sa = String(a)
  const sb = String(b)
  return sa < sb ? -1 : sa > sb ? 1 : 0
}

// 行级过滤：对 rows 应用 AND 叠加的过滤条件，返回过滤后的行（前端内存过滤专用）。
// columns 用于把过滤条件里的列名（大小写不敏感）映射到列索引。
export function applyFilters(columns: string[], rows: unknown[][], filters: ColumnFilter[]): unknown[][] {
  if (filters.length === 0) return rows
  return rows.filter((row) =>
    filters.every((f) => {
      const ci = columns.findIndex((c) => c.toLowerCase() === f.column.toLowerCase())
      if (ci < 0) return false // 列不存在：该条件不匹配（安全兜底）
      return matchesFilter(row[ci], f.op, f.value)
    }),
  )
}
