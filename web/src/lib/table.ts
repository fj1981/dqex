// 表格列宽自适应工具：按内容（列名 + 采样行）估算每列显示宽度，
// 供结果网格（ResultGrid）与表浏览（TableBrowser）共用，避免重复实现。

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
