import type { SQLQueryResult } from "@/types"

// ---- SQL 查询结果前端本地缓存（IndexedDB） ----
//
// 设计：查询结果集不落后端（大结果集会撑爆 SQLite），改存浏览器本地 IndexedDB。
// 结果按「连接 + 库 + SQL 文本」关联到对应 query tab 的可重跑上下文，
// 刷新页面 / 重开连接时按 key 从本地缓存读回，做到「刷新不丢结果、不重跑」。
// 后端 workspace 表仍只持久化 tab 布局（sql/db/mode 等），结果集由本模块兜底。

const DB_NAME = "dbimpex"
const STORE = "query-results"
const DB_VERSION = 1

// 过期策略：条目保留 TTL（毫秒，默认 7 天）；每连接最多保留 N 个结果条目（LRU 淘汰最旧）。
const RESULT_TTL_MS = 7 * 24 * 60 * 60 * 1000
const MAX_RESULTS_PER_CONN = 50

interface CacheEntry {
  key: string
  connId: string
  results: SQLQueryResult[]
  savedAt: number
}

// 生成结果缓存 key：连接 + 库 + SQL 文本（与 tab 的可重跑上下文一一对应）
export function resultCacheKey(connId: string, db: string, sql: string): string {
  return `${connId}\u0000${db}\u0000${sql}`
}

function openDB(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, DB_VERSION)
    req.onupgradeneeded = () => {
      const db = req.result
      if (!db.objectStoreNames.contains(STORE)) {
        const store = db.createObjectStore(STORE, { keyPath: "key" })
        store.createIndex("connId", "connId", { unique: false })
        store.createIndex("savedAt", "savedAt", { unique: false })
      }
    }
    req.onsuccess = () => resolve(req.result)
    req.onerror = () => reject(req.error)
  })
}

// saveResult 保存某次查询结果（异步，失败静默——本地缓存不可用不阻塞主流程）。
export function saveQueryResult(connId: string, db: string, sql: string, results: SQLQueryResult[]): void {
  if (!connId || !sql || results.length === 0) return
  const entry: CacheEntry = { key: resultCacheKey(connId, db, sql), connId, results, savedAt: Date.now() }
  void openDB()
    .then((db) => {
      return new Promise<void>((resolve, reject) => {
        const tx = db.transaction(STORE, "readwrite")
        tx.objectStore(STORE).put(entry)
        tx.oncomplete = () => {
          db.close()
          resolve()
        }
        tx.onerror = () => {
          db.close()
          reject(tx.error)
        }
      })
    })
    .then(() => {
      // 写入后顺带清理过期 / 超量条目（懒清理，LRU）
      void purgeExpired(connId)
    })
    .catch(() => {
      /* IndexedDB 不可用（隐私模式等），忽略 */
    })
}

// loadQueryResult 按 key 读取结果；未命中返回 null。
export function loadQueryResult(connId: string, db: string, sql: string): Promise<SQLQueryResult[] | null> {
  if (!connId || !sql) return Promise.resolve(null)
  const key = resultCacheKey(connId, db, sql)
  return openDB()
    .then((db) => {
      return new Promise<SQLQueryResult[] | null>((resolve, reject) => {
        const tx = db.transaction(STORE, "readonly")
        const req = tx.objectStore(STORE).get(key)
        req.onsuccess = () => {
          db.close()
          const entry = req.result as CacheEntry | undefined
          if (!entry) return resolve(null)
          // 命中但已过期：视为未命中并顺带删除
          if (Date.now() - entry.savedAt > RESULT_TTL_MS) {
            void removeQueryResult(key)
            return resolve(null)
          }
          resolve(entry.results)
        }
        req.onerror = () => {
          db.close()
          reject(req.error)
        }
      })
    })
    .catch(() => null)
}

// removeQueryResult 删除单条结果（供关闭 tab / 清空结果时清理本地缓存）。
export function removeQueryResult(key: string): void {
  void openDB()
    .then((db) => {
      return new Promise<void>((resolve) => {
        const tx = db.transaction(STORE, "readwrite")
        tx.objectStore(STORE).delete(key)
        tx.oncomplete = () => {
          db.close()
          resolve()
        }
      })
    })
    .catch(() => {})
}

// purgeExpired 清理某连接的过期条目 + 超出上限的旧条目（LRU：按 savedAt 淘汰最旧）。
function purgeExpired(connId: string): Promise<void> {
  return openDB()
    .then((db) => {
      return new Promise<void>((resolve) => {
        const tx = db.transaction(STORE, "readwrite")
        const store = tx.objectStore(STORE)
        const idx = store.index("connId")
        const req = idx.getAll(connId)
        req.onsuccess = () => {
          const entries = (req.result as CacheEntry[]).sort((a, b) => b.savedAt - a.savedAt)
          const now = Date.now()
          let kept = 0
          for (const e of entries) {
            // 过期或超出每连接上限 → 删除
            if (now - e.savedAt > RESULT_TTL_MS || kept >= MAX_RESULTS_PER_CONN) {
              store.delete(e.key)
            } else {
              kept++
            }
          }
          db.close()
          resolve()
        }
        req.onerror = () => {
          db.close()
          resolve()
        }
      })
    })
    .catch(() => {})
}
