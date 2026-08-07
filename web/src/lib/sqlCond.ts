// 表条件可视化模型与 SQL 回填：
// 可视化条件行（WhereCond）+ 序列化为 WHERE 字符串 + 基于 AST 的完整 SELECT 回填。
// AST 解析依赖 node-sql-parser，通过动态 import 懒加载，不计入首屏 bundle。

// 可视化条件行
export interface WhereCond {
  column: string
  operator: string
  value: string
  // 与前一行的连接符（首行忽略）
  connector: "AND" | "OR"
}

// 无值运算符
export const NO_VALUE_OPS = ["IS NULL", "IS NOT NULL"]

// 序列化可视化条件 → WHERE 字符串（左到右连接，不加括号）
export function serializeWhere(conds: WhereCond[]): string {
  if (conds.length === 0) return ""
  return conds
    .map((c, i) => {
      let expr: string
      if (NO_VALUE_OPS.includes(c.operator)) {
        expr = `${c.column} ${c.operator}`
      } else if (c.operator === "IN" || c.operator === "NOT IN") {
        const v = c.value.trim()
        expr = `${c.column} ${c.operator} ${v.startsWith("(") ? v : `(${v})`}`
      } else {
        expr = `${c.column} ${c.operator} ${c.value}`
      }
      return i === 0 ? expr : `${c.connector} ${expr}`
    })
    .join(" ")
}

// AST 回填结果
export interface SqlRestore {
  ok: boolean
  // 导出列（空 = 全部列）
  cols: string[]
  // 还原出的可视化条件（按左到右顺序）
  conds: WhereCond[]
}

// AST 运算符 → 可视化运算符（不在表内的运算符回填失败，走降级）
const OP_MAP: Record<string, string> = {
  "=": "=",
  "!=": "!=",
  "<>": "!=",
  ">": ">",
  "<": "<",
  ">=": ">=",
  "<=": "<=",
  "LIKE": "LIKE",
  "NOT LIKE": "NOT LIKE",
  "IN": "IN",
  "NOT IN": "NOT IN",
  "IS": "IS NULL",
  "IS NOT": "IS NOT NULL",
}

// eslint-disable-next-line @typescript-eslint/no-explicit-any
type AstNode = any

// 尝试用 AST 将完整 SELECT 还原为 导出列 + 可视化条件（懒加载 node-sql-parser）。
// 仅支持：单表、简单列选择、扁平 AND/OR 比较条件链；其余返回 ok=false 由调用方降级
export async function restoreSelectViaAst(sql: string): Promise<SqlRestore> {
  const fail: SqlRestore = { ok: false, cols: [], conds: [] }
  try {
    const { Parser } = await import("node-sql-parser")
    const parser = new Parser()
    // 本工具对接 mysql/postgresql/oracle，node-sql-parser 不支持 oracle 方言，依次尝试
    let ast: AstNode = null
    for (const db of ["mysql", "postgresql"]) {
      try {
        ast = parser.astify(sql, { database: db })
        break
      } catch {
        /* 尝试下一方言 */
      }
    }
    if (!ast) return fail
    const node = Array.isArray(ast) ? ast[0] : ast
    if (!node || node.type !== "select") return fail
    // 单表：FROM 仅一项且为普通表；JOIN/子查询超出可视化模型
    if (!Array.isArray(node.from) || node.from.length !== 1) return fail
    const from0 = node.from[0]
    if (typeof from0.table !== "string" || from0.join || from0.expr?.type === "select") return fail
    // GROUP/ORDER/LIMIT/HAVING/WITH 无法映射回可视化条件
    if (node.groupby || node.orderby || node.limit || node.having || node.with) return fail

    // 列：仅支持简单列引用（* = 全部列；表达式/别名无法回填）
    const cols: string[] = []
    const columns = node.columns ?? []
    for (const c of columns) {
      const e = c?.expr
      if (!e || e.type !== "column_ref" || typeof e.column !== "string") return fail
      if (e.column === "*") {
        if (columns.length !== 1) return fail
        continue
      }
      if (c.as) return fail
      cols.push(e.column)
    }

    // WHERE：AND/OR 二元表达式树按左到右展开为条件链
    const conds: WhereCond[] = []
    if (node.where) {
      if (!collectConds(node.where, conds) || conds.length === 0) return fail
      // 重建校验：还原结果重新拼装的 WHERE 必须生成等价 AST，
      // 防止显式括号/优先级差异被静默抹平导致语义变化
      const rebuiltAst = tryParse(parser, `SELECT 1 FROM _t WHERE ${serializeWhere(conds)}`)
      if (!rebuiltAst?.where) return fail
      if (JSON.stringify(normalizeAst(rebuiltAst.where)) !== JSON.stringify(normalizeAst(node.where))) {
        return fail
      }
    }
    return { ok: true, cols, conds }
  } catch {
    return fail
  }
}

// 解析辅助：失败返回 null
function tryParse(parser: { astify: (sql: string, opt: { database: string }) => AstNode }, sql: string): AstNode {
  try {
    const ast = parser.astify(sql, { database: "mysql" })
    return Array.isArray(ast) ? ast[0] : ast
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
  } catch {
    return null
  }
}

// 展开 WHERE 树为条件链：中序遍历，右子树首个条件以当前节点运算符连接左子树
function collectConds(node: AstNode, out: WhereCond[]): boolean {
  if (!node || node.type !== "binary_expr") return false
  const op = String(node.operator || "").toUpperCase()
  if (op === "AND" || op === "OR") {
    if (!collectConds(node.left, out)) return false
    const before = out.length
    if (!collectConds(node.right, out)) return false
    if (out.length > before) out[before].connector = op as "AND" | "OR"
    return true
  }
  const uiOp = OP_MAP[op]
  if (!uiOp) return false
  const left = node.left
  if (!left || left.type !== "column_ref" || typeof left.column !== "string" || left.column === "*") return false
  const value = renderValue(node.right, op)
  if (value === null) return false
  out.push({ column: left.column, operator: uiOp, value, connector: "AND" })
  return true
}

// 值节点 → 文本（不可表示时返回 null 触发降级）
function renderValue(node: AstNode, op: string): string | null {
  if (op === "IS" || op === "IS NOT") return "" // IS [NOT] NULL 无值
  if (!node) return null
  switch (node.type) {
    case "number":
      return String(node.value)
    case "bool":
      return node.value ? "TRUE" : "FALSE"
    case "null":
      return "NULL"
    case "single_quote_string":
    case "string":
      return `'${String(node.value).replace(/'/g, "''")}'`
    case "column_ref":
      return typeof node.column === "string" ? node.column : null
    case "expr_list": {
      const parts = (node.value || []).map((v: AstNode) => renderValue(v, ""))
      return parts.includes(null) ? null : `(${parts.join(", ")})`
    }
    default:
      return null
  }
}

// 归一化 AST 用于等价比较：<> → !=；列引用去掉表前缀与 collate（回填后重新生成不含这些修饰）
function normalizeAst(node: AstNode): AstNode {
  if (Array.isArray(node)) return node.map(normalizeAst)
  if (node && typeof node === "object") {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const o: Record<string, any> = {}
    for (const [k, v] of Object.entries(node)) {
      if (node.type === "column_ref" && (k === "table" || k === "collate")) continue
      o[k] = k === "operator" && v === "<>" ? "!=" : normalizeAst(v)
    }
    return o
  }
  return node
}
