import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRight, Database, FileCode2, FunctionSquare, Loader2, Table2, View } from "lucide-react"
import DBErrorCard from "@/components/DBErrorCard"
import { Input } from "@/components/ui/input"
import { useObjectTreeStore } from "@/stores/objectTreeStore"
import { cn } from "@/lib/utils"
import { tKey } from "@/lib/i18n"
import type { ObjectNode } from "@/types"

type ObjType = ObjectNode["type"]

const TYPE_ICON: Record<ObjType, { icon: typeof Table2; cls: string; labelKey: string }> = {
  db: { icon: Database, cls: "text-blue-600", labelKey: "objectTree.type.db" },
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

function LeafNode({ node, depth, dbName, onOpenObject }: {
  node: ObjectNode
  depth: number
  dbName: string
  onOpenObject: Props["onOpenObject"]
}) {
  const { t } = useTranslation()
  const { selected, selectNode } = useObjectTreeStore()
  const meta = TYPE_ICON[node.type] || TYPE_ICON.other
  const Icon = meta.icon
  const active = selected === node.name

  return (
    <button
      type="button"
      className={cn(
        "flex w-full items-center gap-1.5 rounded-md py-1 pr-2 text-left text-[12px] transition-colors",
        active ? "bg-primary/10 text-primary" : "text-foreground/85 hover:bg-accent",
      )}
      style={{ paddingLeft: depth * 14 + 6 }}
      title={tKey(meta.labelKey)}
      onClick={() => {
        selectNode(node.name)
        onOpenObject(node.name, dbName, node.type)
      }}
    >
      <Icon className={cn("h-3.5 w-3.5 shrink-0", meta.cls)} />
      <span className="truncate">{node.name}</span>
    </button>
  )
}

function GroupNode({ group, node, depth, dbName, onOpenObject }: {
  group: { type: ObjType; labelKey: string }
  node: ObjectNode
  depth: number
  dbName: string
  onOpenObject: Props["onOpenObject"]
}) {
  const { t } = useTranslation()
  const { expanded, toggleNode } = useObjectTreeStore()
  const [filter, setFilter] = useState("")
  const groupKey = `${node.name}:${group.type}`
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
        className="group flex items-center gap-1 rounded-md py-1 pr-2 text-[12px] hover:bg-accent"
        style={{ paddingLeft: depth * 14 + 4 }}
      >
        <button
          type="button"
          className="flex min-w-0 flex-1 items-center gap-1.5 text-left"
          onClick={() => toggleNode(groupKey)}
        >
          <ChevronRight className={cn("h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform", isOpen && "rotate-90")} />
          <span className="truncate font-medium text-muted-foreground">{tKey(group.labelKey)}</span>
          <span className="ml-auto text-[10px] tabular-nums text-muted-foreground">{children.length}</span>
        </button>
      </div>
      {isOpen && (
        <div className="mt-0.5">
          {needFilter && (
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
              仅当列表溢出（可滚动）时显示，滚动期间始终可操作。 */}
          <div ref={scrollRef} className="scrollbar-thin max-h-64 overflow-y-auto">
            {children
              .filter((c) => !filter || c.name.toLowerCase().includes(filter.toLowerCase()))
              .map((c) => (
                <LeafNode key={c.type + ":" + c.name} node={c} depth={depth + 1} dbName={dbName} onOpenObject={onOpenObject} />
              ))}
          </div>
        </div>
      )}
    </div>
  )
}

function DbNode({ node, onOpenObject }: { node: ObjectNode; onOpenObject: Props["onOpenObject"] }) {
  const { expanded, toggleNode } = useObjectTreeStore()
  const isOpen = !!expanded[node.name]
  const childCount = node.children?.length ?? 0
  const Icon = Database

  return (
    <div>
      <div className="group flex items-center gap-1 rounded-md py-1 pr-2 text-[12px] hover:bg-accent">
        <button type="button" className="flex min-w-0 flex-1 items-center gap-1.5 text-left" onClick={() => childCount > 0 && toggleNode(node.name)}>
          <ChevronRight className={cn("h-3.5 w-3.5 shrink-0 text-muted-foreground transition-transform", isOpen && "rotate-90")} />
          <Icon className="h-3.5 w-3.5 shrink-0 text-blue-600" />
          <span className="truncate font-medium">{node.name}</span>
          {childCount > 0 && <span className="ml-auto text-[10px] tabular-nums text-muted-foreground">{childCount}</span>}
        </button>
      </div>
      {isOpen && (
        <div className="mt-0.5">
          {GROUP_ORDER.map((group) => (
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
  const { nodes, loading, error, connId, loadTree } = useObjectTreeStore()

  if (loading) {
    return (
      <div className="flex items-center gap-2 p-4 text-xs text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> {t("objectTree.loading")}
      </div>
    )
  }
  if (error) {
    return (
      // 加载失败：友好化错误卡（标题/原因/排查建议 + 折叠原始错误 + 重试重新加载）
      <div className="flex flex-col items-center gap-3 p-4">
        <DBErrorCard error={error} onRetry={() => void loadTree(connId)} />
      </div>
    )
  }
  if (nodes.length === 0) {
    return <div className="p-4 text-center text-xs text-muted-foreground">{t("objectTree.noDb")}</div>
  }

  return (
    <div className="scrollbar-thin min-h-0 flex-1 overflow-y-auto p-2">
      {nodes.map((n) => (
        <DbNode key={n.type + ":" + n.name} node={n} onOpenObject={onOpenObject} />
      ))}
    </div>
  )
}