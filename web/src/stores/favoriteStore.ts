import { create } from "zustand"
import type { SQLFavorite } from "@/types"
import { addFavorite, deleteFavorite, listFavorites, renameFavorite } from "@/api/sql"

interface FavoriteState {
  favorites: SQLFavorite[]
  loading: boolean
  // 全局收藏，不按连接隔离；connId 仅用于新增时记录来源标记
  load: () => Promise<void>
  add: (connId: string, sql: string, db?: string, mode?: SQLExecMode, title?: string) => Promise<void>
  remove: (id: string) => Promise<void>
  rename: (id: string, title: string) => Promise<void>
}

export const useFavoriteStore = create<FavoriteState>((set, get) => ({
  favorites: [],
  loading: false,
  load: async () => {
    set({ loading: true })
    try {
      const favorites = await listFavorites()
      set({ favorites })
    } finally {
      set({ loading: false })
    }
  },
  add: async (connId, sql, db, mode, title) => {
    await addFavorite(connId, { sql, db, mode, title })
    await get().load()
  },
  remove: async (id) => {
    await deleteFavorite(id)
    set({ favorites: get().favorites.filter((f) => f.id !== id) })
  },
  rename: async (id, title) => {
    const t = title.trim()
    if (!t) return
    await renameFavorite(id, t)
    set({
      favorites: get().favorites.map((f) => (f.id === id ? { ...f, title: t } : f)),
    })
  },
}))
