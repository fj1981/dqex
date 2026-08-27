import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRight, Database, FileCode2, Folder, FunctionSquare, Loader2, RefreshCw, Table2, View, AlertCircle } from "lucide-react"
import DBErrorCard from "@/components/DBErrorCard"
import { Input } from "@/components/ui/input"
import { useObjectTreeStore } from "@/stores/objectTreeStore"
import { cn } from "@/lib/utils"
import { tKey } from "@/lib/i18n"
import type { ObjectNode } from "@/types"

type ObjType = ObjectNode["type"]

const TYPE_ICON: Record<ObjType, { icon: typeof Table2; cls: string; labelKey: string }> = {
  db: { icon: Database, cls: "text-blue-600", labelKey: "objectTree.type.db" },
  schema: { icon: Folder, cls: "text-amber-600", labelKey: "objectTree.type.schema" },
  table: { icon: Table2, cls: "text-emerald-600", labelKey: "objectTree.type.table" },
  view: { icon: View, cls: "text-cyan-600", labelKey: "objectTree.type.view" },
  function: { icon: FunctionSquare, cls: "text-violet-600", labelKey: "objectTree.type.function" },
  procedure: { icon: FileCode2, cls: "text-orange-600", labelKey: "objectTree.type.procedure" },
  other: { icon: FileCode2, cls: "text-muted-foreground", labelKey: "objectTree.type.other" },
}

// 库下对象的分组展示顺序（表/视图/函数/存储过程），分组标题在每类对象前
const GROUP_ORDER: { type: ObjType; labelKey: string }[] = [
  { type: "table", labelKey: "objectTree.group.table" },
  { type: "view", labelKey: "objectTree.group.view" },
  { type: "function", labelKey: "objectTree.group.function" },
  { type: "procedure", labelKey: "objectTree.group.procedure" },
  { type: "other", labelKey: "objectTree.group.other" },
]

interface Props {
  // 表/视图：打开数据浏览 tab；函数/过程等仅展示信息
  onOpenObject: (name: string, db: string, type: ObjType) => void
}

function LeafNode({ node, depth, dbName, onOpenObject, bareName = false }: {
  node: ObjectNode
  depth: number
  dbName: string
  onOpenObject: Props["onOpenObject"]
  // schema 节点下叶子显示裸名（schema 归属已由父节点体现），逻辑标识仍用限定名
  bareName?: boolean
}) {
  const { t } = useTranslation()
  const { selected, selectNode } = useObjectTreeStore()
  const meta = TYPE_ICON[node.type] || TYPE_ICON.other
  const Icon = meta.icon
  const active = selected === node.name
  // 去掉首段（schema 前缀），与后端限定名拆分口径一致（首个 . 分隔）
  const display = bareName ? node.name.slice(node.name.indexOf(".") + 1) : node.name

  return (
    <button
      type="button"
      className={cn(
        "flex w-full items-center gap-1.5 rounded-md py-1 pr-2 text-left text-[12px] transition-colors",
        active ? "bg-primary/10 text-primary" : "text-foreground/85 hover:bg-accent",
      )}
      style={{ paddingLeft: depth * 14 + 24 }}
      title={tKey(meta.labelKey)}
      onClick={() => {
        selectNode(node.name)
        onOpenObject(node.name, dbName, node.type)
      }}
    >
      <Icon className={cn("h-3.5 w-3.5 shrink-0", meta.cls)} />
      <span className="truncate">{display}</span>
    </button>
  )
}

function GroupNode({ group, node, depth, dbName, keyPrefix = "", onOpenObject, bareLeafs = false }: {
  group: { type: ObjType; labelKey: string }
  node: ObjectNode
  depth: number
  dbName: string
  // schema 分层下传入 "库名:schema名:" 前缀，避免不同库/schema 的分组展开 key 冲突；
  // MySQL/Oracle 无 schema 层时为空，展开 key 与历史一致（库名:类型）
  keyPrefix?: string
  onOpenObject: Props["onOpenObject"]
  // schema 节点下的分组叶子显示裸名（去除 schema 前缀）
  bareLeafs?: boolean
}) {
  const { t } = useTranslation()
  const { expanded, toggleNode } = useObjectTreeStore()
  const [filter, setFilter] = useState("")
  const groupKey = `${keyPrefix}${node.name}:${group.type}`
  const isOpen = !!expanded[groupKey]
  const children = (node.children || []).filter((c) => c.type === group.type)
  // 滚动容器引用 + 溢出状态：列表实际可滚动（内容超高）时才显示过滤框，与滚动出现条件严格对齐
  // （hooks 须在条件返回前调用，故放在 children 判空之前；isOpen 参与依赖：折叠态下容器未挂载，
  //  展开瞬间需重新测量，否则折叠期间 children 变化会导致展开后过滤框不出现）
  const scrollRef = useRef<HTMLDivElement>(null)
  const [needFilter, setNeedFilter] = useState(false)
  useEffect(() => {
    const el = scrollRef.current
    setNeedFilter(!!el && el.scrollHeight > el.clientHeight)
  }, [children.length, isOpen])
  if (children.length === 0) return null

  return (
    <div>
      <div
        className="group relative flex items-center gap-1 rounded-md py-1 pr-2 text-[12px] hover:bg-accent"
        style={{ paddingLeft: depth * 14 + 22 }}
      >
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
          onClick={() => toggleNode(groupKey)}
        >
          <ChevronRight className={cn("h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform", isOpen && "rotate-90")} />
          <span className="truncate pr-[42px] font-medium text-muted-foreground">{tKey(group.labelKey)}</span>
          <span className="absolute right-[30px] text-[10px] tabular-nums text-muted-foreground">{children.length}</span>
        </button>
      </div>
      {isOpen && (
        <div className="mt-0.5">
          {/* 过滤框：列表溢出（可滚动）或有过滤条件时显示。
              有过滤条件时必须保留，否则收起再展开后列表不再溢出，
              过滤框消失而 filter 状态仍在，用户将无法清除过滤。 */}
          {(needFilter || filter !== "") && (
            <div className="px-3 pb-1">
              <Input
                placeholder={t("objectTree.filter")}
                className="h-6 text-[11px]"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              />
            </div>
          )}
          {/* 子节点列表最大高度固定，内部滚动：避免单个分组（如表）过多把后续库挤到视区外，
              用户无需折叠整个库也能看到其他库。过滤框固定在顶部不参与滚动，
              列表溢出（可滚动）时显示，滚动期间始终可操作。 */}
          <div ref={scrollRef} className="scrollbar-thin max-h-64 overflow-y-auto">
            {children
              .filter((c) => !filter || c.name.toLowerCase().includes(filter.toLowerCase()))
              .map((c) => (
                <LeafNode key={c.type + ":" + c.name} node={c} depth={depth + 1} dbName={dbName} onOpenObject={onOpenObject} bareName={bareLeafs} />
              ))}
          </div>
        </div>
      )}
    </div>
  )
}

function SchemaNode({ dbName, node, onOpenObject }: {
  dbName: string // 所属库名（展开 key 与分组 key 前缀用）
  node: ObjectNode // schema 节点（PG 系），children 为表/视图/函数/过程
  onOpenObject: Props["onOpenObject"]
}) {
  const { t } = useTranslation()
  const { expanded, toggleNode, loadSchema, loadingNode } = useObjectTreeStore()
  const schemaKey = `${dbName}:${node.name}`
  const isOpen = !!expanded[schemaKey]
  const childCount = node.children?.length ?? 0
  // 未加载：灰显（schema 归属/表计数来自库加载结果），点击才请求对象清单
  const pending = node.loaded === false
  const loading = loadingNode === schemaKey
  const Icon = Folder
  // 分组展开 key 前缀：库名:schema名: 隔离（不同库/不同 schema 的同名分组不串展开态）
  const keyPrefix = `${dbName}:${node.name}:`

  return (
    <div>
      <div className="group relative flex items-center gap-1 rounded-md py-1 pr-2 text-[12px] hover:bg-accent" style={{ paddingLeft: 20 }}>
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
          onClick={() => {
            if (pending) {
              void loadSchema(dbName, node.name)
              return
            }
            if (childCount > 0) toggleNode(schemaKey)
          }}
        >
          {loading ? (
            <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" />
          ) : (
            <ChevronRight className={cn("h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform", isOpen && "rotate-90", pending && "opacity-0")} />
          )}
          <Icon className={cn("h-3.5 w-3.5 shrink-0", pending ? "text-muted-foreground/60" : "text-amber-600")} />
          <span className={cn("truncate pr-[42px]", pending && "text-muted-foreground/70")}>{node.name}</span>
          {node.error && <span title={node.error} className="shrink-0"><AlertCircle className="h-3 w-3 text-destructive" /></span>}
          {!pending && childCount > 0 && (
            <span className="absolute right-[30px] text-[10px] tabular-nums text-muted-foreground">{childCount}</span>
          )}
          {pending && !loading && node.count !== undefined && (
            <span className="absolute right-[30px] text-[10px] tabular-nums text-muted-foreground/60">{node.count}</span>
          )}
        </button>
        {/* 节点级刷新：始终渲染占位避免计数位移，pending/loading 时 invisible 隐藏 */}
        <button
          type="button"
          className={cn(
            "shrink-0 rounded p-0.5 text-muted-foreground transition-opacity",
            pending || loading ? "invisible" : "opacity-0 group-hover:opacity-100 hover:text-foreground",
          )}
          title={t("objectTree.refreshNode")}
          disabled={pending || loading}
          onClick={() => void loadSchema(dbName, node.name, true)}
        >
          <RefreshCw className="h-3.5 w-3.5 shrink-0" />
        </button>
      </div>
      {isOpen && !pending && (
        <div className="mt-0.5">
          {GROUP_ORDER.map((group) => (
            <GroupNode key={group.type} group={group} node={node} depth={1} dbName={dbName} keyPrefix={keyPrefix} onOpenObject={onOpenObject} bareLeafs />
          ))}
        </div>
      )}
    </div>
  )
}

function DbNode({ node, onOpenObject }: { node: ObjectNode; onOpenObject: Props["onOpenObject"] }) {
  const { t } = useTranslation()
  const { expanded, toggleNode, loadDb, loadingNode } = useObjectTreeStore()
  const isOpen = !!expanded[node.name]
  const childCount = node.children?.length ?? 0
  // 未加载：灰显（库未真正枚举），点击才连接加载 schema/对象清单
  const pending = node.loaded === false
  const loading = loadingNode === node.name
  const Icon = Database
  // PG 系：库下挂 schema 子节点（库 → schema → 分组 → 对象）；MySQL/Oracle：库下直接分组
  const schemaChildren = (node.children || []).filter((c) => c.type === "schema")

  return (
    <div>
      <div className="group relative flex items-center gap-1 rounded-md py-1 pr-2 text-[12px] hover:bg-accent">
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
          onClick={() => {
            if (pending) {
              void loadDb(node.name)
              return
            }
            if (childCount > 0) toggleNode(node.name)
          }}
        >
          {loading ? (
            <Loader2 className="h-3.5 w-3.5 shrink-0 animate-spin text-muted-foreground" />
          ) : (
            <ChevronRight className={cn("h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform", isOpen && "rotate-90", pending && "opacity-0")} />
          )}
          <Icon className={cn("h-3.5 w-3.5 shrink-0", pending ? "text-muted-foreground/60" : "text-blue-600")} />
          <span className={cn("truncate pr-[42px] font-medium", pending && "text-muted-foreground/70")}>{node.name}</span>
          {node.error && <span title={node.error} className="shrink-0"><AlertCircle className="h-3 w-3 text-destructive" /></span>}
          {!pending && childCount > 0 && (
            <span className="absolute right-[30px] text-[10px] tabular-nums text-muted-foreground">{childCount}</span>
          )}
          {pending && !loading && node.count !== undefined && (
            <span className="absolute right-[30px] text-[10px] tabular-nums text-muted-foreground/60">{node.count}</span>
          )}
        </button>
        {/* 节点级刷新：始终渲染占位避免计数位移，pending/loading 时 invisible 隐藏 */}
        <button
          type="button"
          className={cn(
            "shrink-0 rounded p-0.5 text-muted-foreground transition-opacity",
            pending || loading ? "invisible" : "opacity-0 group-hover:opacity-100 hover:text-foreground",
          )}
          title={t("objectTree.refreshNode")}
          disabled={pending || loading}
          onClick={() => void loadDb(node.name, true)}
        >
          <RefreshCw className="h-3.5 w-3.5 shrink-0" />
        </button>
      </div>
      {isOpen && !pending && (
        <div className="mt-0.5">
          {schemaChildren.length > 0
            ? schemaChildren.map((s) => (
                <SchemaNode key={s.name} dbName={node.name} node={s} onOpenObject={onOpenObject} />
              ))
            : GROUP_ORDER.map((group) => (
                <GroupNode key={group.type} group={group} node={node} depth={1} dbName={node.name} onOpenObject={onOpenObject} />
              ))}
        </div>
      )}
    </div>
  )
}

// 左侧对象树：库 → 表/视图/函数/存储过程（按类型分组），懒加载；点击叶子对象触发回调
export default function ObjectTree({ onOpenObject }: Props) {
  const { t } = useTranslation()
  const { nodes, loading, error, connId, loadTree, refreshTree } = useObjectTreeStore()

  return (
    <div className="flex h-full min-h-0 flex-col">
      {loading ? (
        <div className="flex items-center gap-2 p-4 text-xs text-muted-foreground">
          <Loader2 className="h-4 w-4 animate-spin" /> {t("objectTree.loading")}
        </div>
      ) : error ? (
        <div className="flex flex-col items-center gap-3 p-4">
          <DBErrorCard error={error} onRetry={() => void loadTree(connId)} />
        </div>
      ) : nodes.length === 0 ? (
        <div className="p-4 text-center text-xs text-muted-foreground">{t("objectTree.noDb")}</div>
      ) : (
        <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto p-2">
          {nodes.map((n) => (
            <DbNode key={n.type + ":" + n.name} node={n} onOpenObject={onOpenObject} />
          ))}
        </div>
      )}
    </div>
  )
}