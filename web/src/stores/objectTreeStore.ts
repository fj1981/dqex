import { create } from "zustand"
import { getTableTree } from "@/api"
import { buildObjectTree } from "@/api/sql"
import type { ObjectNode } from "@/types"

interface ObjectTreeState {
  connId: string
  nodes: ObjectNode[]
  loading: boolean
  expanded: Record<string, boolean> // 节点名 → 展开态
  selected: string | null // 选中的表/视图名
  error: string | null
  loadTree: (connId: string, db?: string) => Promise<void>
  toggleNode: (name: string) => void
  selectNode: (name: string | null) => void
  clear: () => void
}

export const useObjectTreeStore = create<ObjectTreeState>((set, get) => ({
  connId: "",
  nodes: [],
  loading: false,
  expanded: {},
  selected: null,
  error: null,

  loadTree: async (connId, db) => {
    set({ connId, loading: true, error: null })
    try {
      const { databases } = await getTableTree(connId, db)
      set({ nodes: buildObjectTree(databases || []), loading: false, expanded: {} })
    } catch (e) {
      set({ loading: false, error: (e as Error).message })
    }
  },

  toggleNode: (name) => {
    set((s) => ({ expanded: { ...s.expanded, [name]: !s.expanded[name] } }))
  },

  selectNode: (name) => set({ selected: name }),

  clear: () => set({ connId: "", nodes: [], expanded: {}, selected: null, error: null }),
}))
