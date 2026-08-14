import { create } from "zustand"
import { clearSqlHistory, fetchSqlAudit, fetchSqlHistory } from "@/api/sql"
import type { SQLAuditEntry, SQLHistoryItem } from "@/types"

interface SqlHistoryState {
  connId: string
  items: SQLHistoryItem[]
  loading: boolean
  error: string | null
  // 审计（只读）
  auditItems: SQLAuditEntry[]
  auditLoading: boolean
  auditError: string | null
  load: (connId: string) => Promise<void>
  loadAudit: (connId: string) => Promise<void>
  clear: () => Promise<void>
}

const AUDIT_PAGE_SIZE = 200

export const useSqlHistoryStore = create<SqlHistoryState>((set, get) => ({
  connId: "",
  items: [],
  loading: false,
  error: null,
  auditItems: [],
  auditLoading: false,
  auditError: null,

  load: async (connId) => {
    set({ connId, loading: true, error: null })
    try {
      const items = await fetchSqlHistory(connId)
      set({ items: items || [], loading: false })
    } catch (e) {
      set({ loading: false, error: (e as Error).message })
    }
  },

  loadAudit: async (connId) => {
    set({ connId, auditLoading: true, auditError: null })
    try {
      const items = await fetchSqlAudit(connId, AUDIT_PAGE_SIZE, 0)
      set({ auditItems: items || [], auditLoading: false })
    } catch (e) {
      set({ auditLoading: false, auditError: (e as Error).message })
    }
  },

  clear: async () => {
    const { connId } = get()
    if (!connId) return
    await clearSqlHistory(connId)
    set({ items: [] })
  },
}))
