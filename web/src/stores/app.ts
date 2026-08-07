import { create } from "zustand"
import * as api from "@/api"
import type { ConnInfo, DBConn, ExecutionRecord } from "@/types"

interface AppState {
  // 连接
  connections: ConnInfo[]
  connsLoading: boolean
  dbTypes: Record<string, string[]>
  loadConnections: () => Promise<void>
  loadDBTypes: () => Promise<void>
  saveConnection: (id: string | undefined, name: string, conn: DBConn) => Promise<void>
  removeConnection: (id: string) => Promise<void>

  // 连接管理抽屉
  drawerOpen: boolean
  editingConn: ConnInfo | null // null=新建
  openDrawer: (editing?: ConnInfo | null) => void
  closeDrawer: () => void

  // 右侧面板
  panelOpen: boolean
  togglePanel: () => void
  setPanelOpen: (open: boolean) => void
  history: ExecutionRecord[]
  loadHistory: () => Promise<void>
}

export const useAppStore = create<AppState>((set, get) => ({
  connections: [],
  connsLoading: false,
  dbTypes: {},
  loadConnections: async () => {
    set({ connsLoading: true })
    try {
      const list = await api.listConnections()
      set({ connections: list || [] })
    } catch (e) {
      console.error(e)
    } finally {
      set({ connsLoading: false })
    }
  },
  loadDBTypes: async () => {
    try {
      const { types } = await api.getDBTypes()
      set({ dbTypes: types || {} })
    } catch (e) {
      console.error(e)
    }
  },
  saveConnection: async (id, name, conn) => {
    await api.saveConnection({ id, name, conn })
    await get().loadConnections()
  },
  removeConnection: async (id) => {
    await api.deleteConnection(id)
    await get().loadConnections()
  },

  drawerOpen: false,
  editingConn: null,
  openDrawer: (editing = null) => set({ drawerOpen: true, editingConn: editing }),
  closeDrawer: () => set({ drawerOpen: false, editingConn: null }),

  // 右侧面板：窄屏（<1024px，与 lg 断点一致）以浮层展示，默认收起避免遮挡内容
  panelOpen: typeof window === "undefined" ? true : window.matchMedia("(min-width: 1024px)").matches,
  togglePanel: () => set({ panelOpen: !get().panelOpen }),
  setPanelOpen: (open: boolean) => set({ panelOpen: open }),
  history: [],
  loadHistory: async () => {
    try {
      const list = await api.listHistory()
      set({ history: (list || []).slice(0, 20) })
    } catch (e) {
      console.error(e)
    }
  },
}))
