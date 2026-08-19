// 表浏览右键「生成 SQL」的参数组装：
// 从当前页行数据 / 单元格值 / 过滤排序条件中提取 GenSQLPayload，
// 值保持原始类型由后端按方言转义内联（见 internal/engine/sqlgen.go），生成仅产出文本、不执行。

import type { GenSQLPayload } from "@/api/sql"
import type { ColumnFilter, GenSQLKind, SortSpec } from "@/types"

// 行级生成上下文（表浏览当前页）
export interface GenSQLContext {
  connId: string
  db: string
  table: string
  columns: string[] // 完整列清单（与 rows 列序一致）
  rows: unknown[][] // 当前页行数据
  pkColumns: string[] // 主键列名（update/delete/selectByPk 定位用）
  skipColumns?: string[] // 插入时跳过的列（自增列）
  filters?: ColumnFilter[] // 过滤条件（selectByFilter）
  sortSpecs?: SortSpec[] // 排序（selectByFilter）
}

// 行级生成：insert/delete 支持多行（选中行批量：insert 逐行语句、delete 合并为 IN 条件），
// 其余类型取首行做 WHERE/SET 定位。行数据或列清单为空返回 null（由调用方忽略，菜单项已按可用性置灰）。
export function buildRowPayload(ctx: GenSQLContext, kind: GenSQLKind, rowIndexes: number[]): GenSQLPayload | null {
  const rows = rowIndexes
    .map((i) => ctx.rows[i])
    .filter((r): r is unknown[] => Array.isArray(r))
  if (rows.length === 0 || ctx.columns.length === 0) return null
  const payload: GenSQLPayload = {
    connId: ctx.connId,
    db: ctx.db,
    table: ctx.table,
    kind,
    columns: ctx.columns,
    // insert/delete 支持多行；其余行级类型取首行
    rows: kind === "insert" || kind === "delete" ? rows : [rows[0]],
  }
  if (kind === "insert") {
    payload.skipColumns = ctx.skipColumns
  } else {
    payload.pkColumns = ctx.pkColumns
  }
  return payload
}

// 单元格条件生成：WHERE 条件列 = 单元格所在列，条件值 = 单元格值（nil → IS NULL）
export function buildCellPayload(ctx: GenSQLContext, rowIndex: number, colIndex: number): GenSQLPayload | null {
  const row = ctx.rows[rowIndex]
  const col = ctx.columns[colIndex]
  if (!Array.isArray(row) || !col) return null
  return {
    connId: ctx.connId,
    db: ctx.db,
    table: ctx.table,
    kind: "whereCell",
    columns: [col],
    rows: [[row[colIndex]]],
  }
}

// 按当前过滤条件 + 排序生成 SELECT（无过滤时生成全表 SELECT）
export function buildFilterPayload(ctx: GenSQLContext): GenSQLPayload | null {
  if (ctx.columns.length === 0) return null
  return {
    connId: ctx.connId,
    db: ctx.db,
    table: ctx.table,
    kind: "selectByFilter",
    columns: ctx.columns,
    rows: [],
    filters: ctx.filters,
    sortSpecs: ctx.sortSpecs,
  }
}
