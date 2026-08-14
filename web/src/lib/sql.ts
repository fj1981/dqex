// SQL 语句分类与危险检测（前端第一道护栏；后端 engine 有第二道强制拦截）

const WRITE_RE = /^\s*(insert|update|delete|drop|alter|create|truncate|rename|grant|revoke|call|replace|merge|comment|lock|unlock)\b/i
const READ_RE = /^\s*(select|show|desc|describe|explain|use|with|set|begin|commit|rollback|start|kill|check|help)\b/i

/** 是否为写操作（INSERT/UPDATE/DELETE/DDL 等）。多语句按分号拆分，任一为写则视为写。 */
export function isWriteSQL(sql: string): boolean {
  const stmts = sql
    .split(";")
    .map((s) => s.trim())
    .filter(Boolean)
  if (stmts.length === 0) return false
  // 空语句块默认按读处理
  return stmts.some((s) => WRITE_RE.test(s))
}

/** 是否为只读查询（SELECT/SHOW 等） */
export function isReadSQL(sql: string): boolean {
  const s = sql.trim()
  if (READ_RE.test(s)) return true
  // 无法识别时默认按读处理（后端会二次校验）
  return true
}

/** 提取危险操作提示（用于确认弹窗文案） */
export function describeWriteOp(sql: string): string {
  const m = sql.trim().match(/^\s*(\w+)/i)
  return m ? m[1].toUpperCase() : "写操作"
}

/** 提取首个语句用于提示（截断超长） */
export function previewSQL(sql: string, max = 120): string {
  const s = sql.trim().replace(/\s+/g, " ")
  return s.length > max ? s.slice(0, max) + "…" : s
}
