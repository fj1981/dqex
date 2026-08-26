import { create } from "zustand"
import { getDatabases, getDbObjects, getDbSchemas, getSchemaObjects } from "@/api"
import { buildDbLeafs, buildDbStub, buildSchemaLeafs, buildSchemaStub } from "@/api/sql"
import type { ObjectNode } from "@/types"

interface ObjectTreeState {
  connId: string
  nodes: ObjectNode[] // 库节点；未加载的库/schema 节点 loaded=false（灰显，点击加载）
  loading: boolean // 第一层（库列表）加载中
  loadingNode: string | null // 正在加载的节点名（库名或 "库名:schema名"），用于行内 loading
  error: string | null // 第一层加载失败
  expanded: Record<string, boolean> // 节点名 → 展开态
  selected: string | null // 选中的表/视图名
  loadTree: (connId: string, force?: boolean) => Promise<void>
  loadDb: (db: string, force?: boolean) => Promise<void> // 点击库：PG 拉 schema 列表，MySQL/Oracle 拉库对象
  loadSchema: (db: string, schema: string, force?: boolean) => Promise<void> // 点击 schema：拉对象清单
  refreshTree: (connId: string) => Promise<void> // 树顶刷新：库列表不走缓存重拉，已展开的库自动重载
  toggleNode: (name: string) => void
  selectNode: (name: string | null) => void
  clear: () => void
}

// updateNode 不可变更新指定库节点（或库下 schema 节点，key 用 "库:schema" 定位）
const updateNode = (nodes: ObjectNode[], key: string, patch: Partial<ObjectNode>): ObjectNode[] =>
  nodes.map((n) => {
    if (n.type === "db" && n.name === key) return { ...n, ...patch }
    if (n.type === "db" && n.children) {
      const i = n.children.findIndex((c) => c.type === "schema" && `${n.name}:${c.name}` === key)
      if (i >= 0) {
        const children = [...n.children]
        children[i] = { ...children[i], ...patch }
        return { ...n, children }
      }
    }
    return n
  })

export const useObjectTreeStore = create<ObjectTreeState>((set, get) => ({
  connId: "",
  nodes: [],
  loading: false,
  loadingNode: null,
  error: null,
  expanded: {},
  selected: null,

  loadTree: async (connId, force = false) => {
    set({ connId, loading: true, error: null })
    try {
      const { databases } = await getDatabases(connId, force)
      set({ nodes: (databases || []).map(buildDbStub), loading: false, expanded: {} })
    } catch (e) {
      set({ loading: false, error: (e as Error).message })
    }
  },

  refreshTree: async (connId) => {
    // 保存已加载的库节点与展开态，刷新后自动重载（Navicat 式：整树刷新保持展开）
    const { nodes, expanded, loadDb, loadTree } = get()
    const loadedDbs = nodes.filter((n) => n.type === "db" && n.loaded).map((n) => n.name)
    const kept = expanded
    await loadTree(connId, true)
    set({ expanded: kept })
    await Promise.allSettled(loadedDbs.map((db) => loadDb(db, true)))
  },

  loadDb: async (db, force = false) => {
    const { connId, nodes } = get()
    // 已加载/正在加载不重复请求；force 刷新（节点级刷新）时跳过已加载守卫
    const node = nodes.find((n) => n.type === "db" && n.name === db)
    if (!node || get().loadingNode === db) return
    if (!force && node.loaded) return
    set({ loadingNode: db })
    try {
      const { schemas } = await getDbSchemas(connId, db, force)
      if (schemas && schemas.length > 0) {
        // PG 系：schema 列表（未加载态，带表计数）
        const schemaNodes = schemas.map((s) => buildSchemaStub(s.name, s.tableCount))
        set({ nodes: updateNode(get().nodes, db, { loaded: true, children: schemaNodes, count: schemas.length }), loadingNode: null })
      } else {
        // MySQL/Oracle 无 schema 层：直接加载库对象
        const { db: data } = await getDbObjects(connId, db, force)
        set({
          nodes: updateNode(get().nodes, db, { loaded: true, children: buildDbLeafs(data), count: data.tables?.length }),
          loadingNode: null,
        })
      }
      // 加载成功后自动展开该库
      set((s) => ({ expanded: { ...s.expanded, [db]: true } }))
    } catch (e) {
      set({
        nodes: updateNode(get().nodes, db, { error: (e as Error).message }),
        loadingNode: null,
      })
    }
  },

  loadSchema: async (db, schema, force = false) => {
    const { connId, nodes } = get()
    const key = `${db}:${schema}`
    const node = nodes.find((n) => n.type === "db" && n.name === db)
    const sch = node?.children?.find((c) => c.type === "schema" && c.name === schema)
    // 已加载/正在加载不重复请求；force 刷新（节点级刷新）时跳过已加载守卫
    if (!sch || get().loadingNode === key) return
    if (!force && sch.loaded) return
    set({ loadingNode: key })
    try {
      const { schema: data } = await getSchemaObjects(connId, db, schema, force)
      set({
        nodes: updateNode(get().nodes, key, {
          loaded: true,
          children: buildSchemaLeafs(data),
          count: (data.tables?.length ?? 0) + (data.objects?._views?.length ?? 0) + (data.objects?._functions?.length ?? 0) + (data.objects?._procedures?.length ?? 0),
        }),
        loadingNode: null,
      })
      // 加载成功后自动展开该 schema
      set((s) => ({ expanded: { ...s.expanded, [key]: true } }))
    } catch (e) {
      set({
        nodes: updateNode(get().nodes, key, { error: (e as Error).message }),
        loadingNode: null,
      })
    }
  },

  toggleNode: (name) => {
    set((s) => ({ expanded: { ...s.expanded, [name]: !s.expanded[name] } }))
  },

  selectNode: (name) => set({ selected: name }),

  clear: () => set({ connId: "", nodes: [], loading: false, loadingNode: null, expanded: {}, selected: null, error: null }),
}))
