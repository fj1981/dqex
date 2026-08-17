import { useState } from "react"
import { ChevronRight, Database, FileCode2, FolderTree, FunctionSquare, Loader2, Table2, View } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { useObjectTreeStore } from "@/stores/objectTreeStore"
import { cn } from "@/lib/utils"
import type { ObjectNode } from "@/types"

type ObjType = ObjectNode["type"]

const TYPE_ICON: Record<ObjType, { icon: typeof Table2; cls: string; label: string }> = {
  db: { icon: Database, cls: "text-blue-600", label: "数据库" },
  table: { icon: Table2, cls: "text-emerald-600", label: "表" },
  view: { icon: View, cls: "text-cyan-600", label: "视图" },
  function: { icon: FunctionSquare, cls: "text-violet-600", label: "函数" },
  procedure: { icon: FileCode2, cls: "text-orange-600", label: "存储过程" },
  other: { icon: FileCode2, cls: "text-muted-foreground", label: "对象" },
}

// 库下对象的分组展示顺序（表/视图/函数/存储过程），分组标题在每类对象前
const GROUP_ORDER: { type: ObjType; label: string }[] = [
  { type: "table", label: "表" },
  { type: "view", label: "视图" },
  { type: "function", label: "函数" },
  { type: "procedure", label: "存储过程" },
  { type: "other", label: "其他对象" },
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
  const { selected, selectNode } = useObjectTreeStore()
  const meta = TYPE_ICON[node.type] || TYPE_ICON.other
  const Icon = meta.icon
  const active = selected === node.name

  return (
    <button
      type="button"
      className={cn(
        "flex w-full items-center gap-1.5 rounded-md py-1 pr-2 text-left text-[12px] transition-colors",
        active ? "bg-primary/10 text-primary" : "text-foreground/80 hover:bg-accent",
      )}
      style={{ paddingLeft: depth * 14 + 6 }}
      title={meta.label}
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
  group: { type: ObjType; label: string }
  node: ObjectNode
  depth: number
  dbName: string
  onOpenObject: Props["onOpenObject"]
}) {
  const { expanded, toggleNode } = useObjectTreeStore()
  const [filter, setFilter] = useState("")
  const groupKey = `${node.name}:${group.type}`
  const isOpen = !!expanded[groupKey]
  const children = (node.children || []).filter((c) => c.type === group.type)
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
          <span className="truncate font-medium text-muted-foreground">{group.label}</span>
          <span className="ml-auto text-[10px] tabular-nums text-muted-foreground">{children.length}</span>
        </button>
      </div>
      {isOpen && (
        <div className="mt-0.5">
          {children.length > 20 && (
            <div className="px-3 pb-1">
              <Input
                placeholder="过滤..."
                className="h-6 text-[11px]"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
              />
            </div>
          )}
          {/* 子节点列表最大高度固定，内部滚动：避免单个分组（如表）过多把后续库挤到视区外，
              用户无需折叠整个库也能看到其他库。过滤输入框固定在顶部不参与滚动。 */}
          <div className="scrollbar-thin max-h-64 overflow-y-auto">
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
  const { nodes, loading, error } = useObjectTreeStore()

  if (loading) {
    return (
      <div className="flex items-center gap-2 p-4 text-xs text-muted-foreground">
        <Loader2 className="h-4 w-4 animate-spin" /> 加载对象树...
      </div>
    )
  }
  if (error) {
    return (
      <div className="flex flex-col items-center gap-2 p-4 text-center text-xs text-destructive">
        <FolderTree className="h-5 w-5" />
        <span>{error}</span>
        <Button size="sm" variant="outline" className="h-7 text-xs" onClick={() => window.location.reload()}>
          重试
        </Button>
      </div>
    )
  }
  if (nodes.length === 0) {
    return <div className="p-4 text-center text-xs text-muted-foreground">无可用库，请检查连接</div>
  }

  return (
    <div className="scrollbar-thin flex-1 overflow-y-auto p-2">
      {nodes.map((n) => (
        <DbNode key={n.type + ":" + n.name} node={n} onOpenObject={onOpenObject} />
      ))}
    </div>
  )
}