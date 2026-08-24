// Tab 栏设置：最大标签页数 + 淘汰优先级排列（纯前端，存 localStorage）

export type EvictCategory =
  | "empty_query"        // 空查询 tab：无 SQL + 无结果
  | "sql_no_result"      // 有 SQL 但无结果的查询 tab
  | "query_with_result"  // 有结果的查询 tab
  | "object_no_data"     // 无数据的数据对象 tab
  | "object_with_data"   // 有数据的数据对象 tab

// 默认淘汰优先级（数组末尾 = 最先被关闭）
// 逻辑：写过 SQL 的 tab 最后淘汰（用户投入了工作），空 tab 最先淘汰
export const DEFAULT_EVICT_ORDER: EvictCategory[] = [
  "query_with_result",  // 最后淘汰：有 SQL 有结果，用户 actively working
  "sql_no_result",      // 有 SQL 无结果，用户至少写了 SQL
  "object_with_data",   // 有数据的对象 tab，用户浏览过
  "object_no_data",     // 无数据的对象 tab，只是打开了
  "empty_query",        // 最先淘汰：空查询 tab
]

const LS_KEY_MAX_TABS = "dqex-max-tabs"
const LS_KEY_EVICT_ORDER = "dqex-evict-order"
const LS_KEY_MAX_TAB_WIDTH = "dqex-max-tab-width"
const DEFAULT_MAX_TABS = 20
const DEFAULT_MAX_TAB_WIDTH = 160 // px，tab 最大宽度

export function getMaxTabs(): number {
  try {
    const v = localStorage.getItem(LS_KEY_MAX_TABS)
    if (v) {
      const n = parseInt(v, 10)
      if (n >= 5 && n <= 100) return n
    }
  } catch { /* ignore */ }
  return DEFAULT_MAX_TABS
}

export function setMaxTabs(n: number): void {
  try {
    localStorage.setItem(LS_KEY_MAX_TABS, String(Math.max(5, Math.min(100, n))))
  } catch { /* ignore */ }
}

export function getEvictOrder(): EvictCategory[] {
  try {
    const v = localStorage.getItem(LS_KEY_EVICT_ORDER)
    if (v) {
      const arr = JSON.parse(v) as EvictCategory[]
      // 校验：必须是 5 个合法分类的排列
      if (Array.isArray(arr) && arr.length === 5) {
        const valid = new Set<EvictCategory>(["empty_query", "sql_no_result", "query_with_result", "object_no_data", "object_with_data"])
        const allValid = arr.every((c) => valid.has(c)) && new Set(arr).size === 5
        if (allValid) return arr
      }
    }
  } catch { /* ignore */ }
  return [...DEFAULT_EVICT_ORDER]
}

export function setEvictOrder(order: EvictCategory[]): void {
  try {
    localStorage.setItem(LS_KEY_EVICT_ORDER, JSON.stringify(order))
  } catch { /* ignore */ }
}

export function getMaxTabWidth(): number {
  try {
    const v = localStorage.getItem(LS_KEY_MAX_TAB_WIDTH)
    if (v) {
      const n = parseInt(v, 10)
      if (n >= 80 && n <= 300) return n
    }
  } catch { /* ignore */ }
  return DEFAULT_MAX_TAB_WIDTH
}

export function setMaxTabWidth(n: number): void {
  try {
    localStorage.setItem(LS_KEY_MAX_TAB_WIDTH, String(Math.max(80, Math.min(300, n))))
  } catch { /* ignore */ }
}
