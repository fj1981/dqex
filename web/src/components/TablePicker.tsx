import { useEffect, useMemo, useRef, useState } from "react"
import { Braces, ChevronDown, ChevronRight, ChevronUp, Code2, Database, Eye, Filter, KeyRound, ListOrdered, Loader2, Plus, Settings2, ShieldCheck, Table2, Trash2, Workflow, X, AlertCircle } from "lucide-react"
import { useTranslation } from "react-i18next"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { confirm } from "@/components/ui/alert-dialog"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Separator } from "@/components/ui/separator"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Textarea } from "@/components/ui/textarea"
import { Label } from "@/components/ui/label"
import TableIcon from "@/components/ui/table-icon"
import * as api from "@/api"
import { NO_VALUE_OPS, restoreSelectViaAst, serializeWhere, type WhereCond } from "@/lib/sqlCond"
import type { DBSchema, DBTables, TableColumn, TableCondition, TableDataMode } from "@/types"
import { tKey } from "@/lib/i18n"
import { cn } from "@/lib/utils"

// ---- WHERE 条件可视化构建 ----

const WHERE_OPERATORS = [
  { value: "=", label: "tablePicker.whereOp.eq" },
  { value: "!=", label: "tablePicker.whereOp.neq" },
  { value: ">", label: "tablePicker.whereOp.gt" },
  { value: "<", label: "tablePicker.whereOp.lt" },
  { value: ">=", label: "tablePicker.whereOp.gte" },
  { value: "<=", label: "tablePicker.whereOp.lte" },
  { value: "LIKE", label: "tablePicker.whereOp.like" },
  { value: "NOT LIKE", label: "tablePicker.whereOp.notLike" },
  { value: "IN", label: "tablePicker.whereOp.in" },
  { value: "NOT IN", label: "tablePicker.whereOp.notIn" },
  { value: "IS NULL", label: "tablePicker.whereOp.isNull" },
  { value: "IS NOT NULL", label: "tablePicker.whereOp.isNotNull" },
]

// 尝试将 WHERE 字符串解析为可视化条件（best-effort，失败则返回 ok=false）
function parseWhere(where: string): { conds: WhereCond[]; ok: boolean } {
  if (!where.trim()) return { conds: [], ok: true }
  const tokens = where.split(/\s+(AND|OR)\s+/i)
  const conds: WhereCond[] = []
  for (let i = 0; i < tokens.length; i += 2) {
    const expr = tokens[i].trim()
    if (!expr) continue
    const connector = (i > 0 ? tokens[i - 1].toUpperCase() : "AND") as "AND" | "OR"
    // IS NULL / IS NOT NULL
    let m = expr.match(/^(\w+)\s+(IS\s+NOT\s+NULL|IS\s+NULL)$/i)
    if (m) { conds.push({ column: m[1], operator: m[2].toUpperCase().replace(/\s+/g, " "), value: "", connector }); continue }
    // IN (...) / NOT IN (...)
    m = expr.match(/^(\w+)\s+(NOT\s+IN|IN)\s*(\([^)]*\))$/i)
    if (m) { conds.push({ column: m[1], operator: m[2].toUpperCase().replace(/\s+/g, " "), value: m[3], connector }); continue }
    // LIKE / NOT LIKE
    m = expr.match(/^(\w+)\s+(NOT\s+LIKE|LIKE)\s+(.+)$/i)
    if (m) { conds.push({ column: m[1], operator: m[2].toUpperCase().replace(/\s+/g, " "), value: m[3], connector }); continue }
    // 比较运算符
    m = expr.match(/^(\w+)\s*(=|!=|<>|>=|<=|>|<)\s*(.+)$/)
    if (m) { conds.push({ column: m[1], operator: m[2] === "<>" ? "!=" : m[2], value: m[3], connector }); continue }
    return { conds: [], ok: false }
  }
  return { conds, ok: true }
}

// 拼装完整 SELECT（可视化模式保存/切换时使用）
function buildSelect(table: string, cols: string[], where: string): string {
  const colStr = cols.length > 0 ? cols.join(", ") : "*"
  const sql = `SELECT ${colStr} FROM ${table}`
  return where.trim() ? `${sql} WHERE ${where.trim()}` : sql
}

// 尝试将完整 SELECT 还原为 导出列 + WHERE（best-effort，失败则 ok=false）
function parseSelect(query: string): { cols: string[]; where: string; ok: boolean } {
  const m = query.trim().match(/^select\s+([\s\S]+?)\s+from\s+\S+(?:\s+where\s+([\s\S]+?))?\s*;?\s*$/i)
  if (!m) return { cols: [], where: "", ok: false }
  const raw = m[1].trim()
  const cols = raw === "*" ? [] : raw.split(",").map((c) => c.trim().replace(/^[`"\[]|[`"\]]$/g, "")).filter(Boolean)
  return { cols, where: (m[2] || "").trim(), ok: true }
}

interface Props {
  connId: string
  db?: string
  // 附加连接（如对比场景的目标库）：仅存在于该连接的表会合并展示并加标记，使其可被勾选
  extraConnId?: string
  extraDb?: string
  extraLabel?: string // 附加来源标记文案，默认「仅目标库有」
  // 库映射：源库名 → 目标库名（大小写不敏感）。合并附加树时按映射把目标库归并到对应源库节点下
  dbMapping?: Record<string, string>
  selected: string[]
  // 选中的对象（格式 _views/名称）
  selectedObjects?: string[]
  // 是否展示并选择对象（默认 true；跨类型迁移传 false）
  showObjects?: boolean
  // 多库模式：勾选要处理的库（勾选级联选中库下所有表与对象）
  selectedDBs?: string[]
  onDBsChange?: (dbs: string[]) => void
  conditions: TableCondition[]
  onChange: (selected: string[], objects: string[], conditions: TableCondition[]) => void
}

// 对象分组定义（dir 与后端 zip 子目录/对象白名单格式一致）
const OBJECT_GROUPS = [
  { dir: "_views", label: "objectTree.group.view", Icon: Eye },
  { dir: "_functions", label: "objectTree.group.function", Icon: Braces },
  { dir: "_procedures", label: "objectTree.group.procedure", Icon: Workflow },
] as const

// 表选择器：左侧 库→分组(表/视图/函数/存储过程)→项 树形；右侧已选表及条件 + 已选对象
export default function TablePicker({
  connId,
  db,
  extraConnId,
  extraDb,
  extraLabel,
  dbMapping,
  selected,
  selectedObjects = [],
  showObjects = true,
  selectedDBs,
  onDBsChange,
  conditions,
  onChange,
}: Props) {
  const { t: tr } = useTranslation()
  const effectiveExtraLabel = extraLabel ?? tr("tablePicker.extraOnlyTarget")
  const [tree, setTree] = useState<DBTables[]>([])
  const [extraTree, setExtraTree] = useState<DBTables[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")
  const [keyword, setKeyword] = useState("")
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({})
  const [editing, setEditing] = useState<string | null>(null)
  const [editQuery, setEditQuery] = useState("") // 完整 SELECT（SQL 模式编辑内容）
  const [editCols, setEditCols] = useState<string[]>([]) // 选中的导出列
  const [editDataMode, setEditDataMode] = useState<TableDataMode>("") // 数据导出模式
  const [columns, setColumns] = useState<TableColumn[]>([])
  const [colsLoading, setColsLoading] = useState(false)
  const [colsError, setColsError] = useState("")
  const whereRef = useRef<HTMLTextAreaElement>(null)
  // WHERE 条件编辑：可视化 vs SQL 双模式
  const [whereMode, setWhereMode] = useState<"visual" | "sql">("visual")
  const [visConds, setVisConds] = useState<WhereCond[]>([])
  const [showColRef, setShowColRef] = useState(false) // SQL 模式列参考折叠
  const [colRefPos, setColRefPos] = useState<{ top: number; right: number } | null>(null) // 列参考面板 fixed 锚点
  // 异步 AST 回填令牌：弹窗关闭/重开时作废旧回填
  const restoreSeq = useRef(0)

  const closeEdit = () => {
    restoreSeq.current++
    setEditing(null)
  }

  useEffect(() => {
    console.log("[TablePicker] sourceTree effect run", { connId, db })
    if (!connId) {
      console.log("[TablePicker] sourceTree: no connId, skip")
      setTree([])
      return
    }
    setLoading(true)
    setError("")
    api
      .getDatabases(connId)
      .then((r) => {
        const names = r.databases || []
        // 分级第一层：仅库名（未加载态），点击库/单库模式自动加载对象
        const stubs: DBTables[] = names.map((n) => ({ name: n, tables: [] }))
        setTree(stubs)
        // 单库模式（db 指定）：自动加载该库（不存在于列表也尝试，便于权限受限场景）
        if (db) {
          if (!names.includes(db)) stubs.push({ name: db, tables: [] })
          void loadDb(db, "src")
        }
        // 初始全部折叠：库节点与各分组节点
        const c: Record<string, boolean> = {}
        stubs.forEach((d) => {
          c[dbKey(d.name)] = true
        })
        setCollapsed(c)
      })
      .catch((e: Error) => {
        console.error("[TablePicker] sourceTree error", { connId, db }, e)
        setTree([])
        setError(e.message)
      })
      .finally(() => {
        console.log("[TablePicker] sourceTree finally, setLoading(false)", { connId, db })
        setLoading(false)
      })
  }, [connId, db]) // eslint-disable-line react-hooks/exhaustive-deps

  // 分级加载第二层：单库对象全量（getDbObjects 后端为单库枚举，MySQL/Oracle/PG 通用）
  const [loadingDb, setLoadingDb] = useState<string | null>(null)
  const loadDb = async (dbName: string, side: "src" | "extra") => {
    const key = `${side}:${dbName}`
    if (loadingDb === key) return
    setLoadingDb(key)
    try {
      const { db: data } = await api.getDbObjects(side === "src" ? connId : extraConnId || "", dbName)
      const loaded = { ...data, _loaded: true }
      if (side === "src") {
        setTree((prev) => prev.map((d) => (d.name === dbName ? loaded : d)))
      } else {
        setExtraTree((prev) => prev.map((d) => (d.name === dbName ? loaded : d)))
      }
      // 加载成功后自动展开该库，并初始化其分组/schema 折叠态
      setCollapsed((c) => ({ ...c, [dbKey(dbName)]: false, [grpKey(dbName, "tables")]: true }))
      OBJECT_GROUPS.forEach((g) => setCollapsed((c) => ({ ...c, [grpKey(dbName, g.dir)]: true })))
      ;(data.schemas || []).forEach((s) => setCollapsed((c) => ({ ...c, [`sch:${dbName}:${s.name}`]: true })))
    } catch (e) {
      const msg = (e as Error).message
      if (side === "src") {
        setTree((prev) => prev.map((d) => (d.name === dbName ? { ...d, _error: msg } : d)))
      } else {
        setExtraTree((prev) => prev.map((d) => (d.name === dbName ? { ...d, _error: msg } : d)))
      }
    } finally {
      setLoadingDb(null)
    }
  }

  // 附加连接树加载（对比场景：目标库独有表也可勾选）；同样分级：仅库名 → 点击/单库自动加载
  const [extraError, setExtraError] = useState("")
  useEffect(() => {
    if (!extraConnId) {
      setExtraTree([])
      setExtraError("")
      return
    }
    setExtraError("")
    api
      .getDatabases(extraConnId)
      .then((r) => {
        const names = r.databases || []
        const stubs: DBTables[] = names.map((n) => ({ name: n, tables: [] }))
        setExtraTree(stubs)
        if (extraDb) {
          if (!names.includes(extraDb)) stubs.push({ name: extraDb, tables: [] })
          void loadDb(extraDb, "extra")
        }
      })
      .catch((e: Error) => {
        console.error("[TablePicker] extraTree error", { extraConnId, extraDb }, e)
        setExtraTree([])
        setExtraError(e.message || tr("tablePicker.extraLoadFailed"))
      })
  }, [extraConnId, extraDb, tr]) // eslint-disable-line react-hooks/exhaustive-deps

  const dbKey = (db: string) => `db:${db}`
  const grpKey = (db: string, dir: string) => `grp:${db}:${dir}`

  // 选择项 ID 统一用限定形式 "库.表" / "库.schema.表" / "库.schema.目录/名"，避免跨库/schema 同名串选
  const qual = (dbName: string, id: string) => `${dbName}.${id}`
  // 解析 ID 的库前缀（裸 ID 返回 ""）
  const entryDB = (id: string): string => {
    const i = id.indexOf(".")
    return i > 0 ? id.slice(0, i) : ""
  }
  // 去掉库前缀得到裸名（用于取列信息等 API，PG 下为 "schema.表" 限定名）
  const stripDB = (id: string) => {
    const i = id.indexOf(".")
    return i > 0 ? id.slice(i + 1) : id
  }
  // 点号计数：区分裸名(0) / 库.表(1) / 库.schema.表(2)
  const dots = (id: string) => id.split(".").length - 1
  // 旧格式限定名（"库.表" / "库.目录/名"）：需升级为带 schema 的格式（PG 分层）
  const isLegacyQual = (id: string) => dots(id) === 1
  // 库是否按 schema 分层（PG 系）
  const hasSchemas = (d: DBTables) => (d.schemas?.length ?? 0) > 0
  // 库下表/对象的全部 ID（PG 带 "schema." 前缀）
  const dbTableIds = (d: DBTables): string[] =>
    hasSchemas(d)
      ? (d.schemas || []).flatMap((s) => s.tables.map((t) => qual(d.name, `${s.name}.${t}`)))
      : d.tables.map((t) => qual(d.name, t))
  // 叶子显示名："库.schema.表" → "表"；"库.目录/名" → "名"（schema 归属由分组节点体现）
  const leafName = (id: string) => {
    const s = stripDB(id)
    const i = s.indexOf("/")
    return i > 0 ? s.slice(i + 1) : s.split(".").pop() || s
  }

  // 把库树展开为裸表条目（无 schema 层时 schema 为 ""），用于合并附加树时按 schema 维度去重
  const expandTables = (d: DBTables): { schema: string; table: string }[] =>
    hasSchemas(d)
      ? (d.schemas || []).flatMap((s) => s.tables.map((t) => ({ schema: s.name, table: t })))
      : d.tables.map((t) => ({ schema: "", table: t }))

  // 树 = 源库全部表 ∪ 映射目标库全部表，按「映射前源库名」聚合去重。
  // 规则（始终一致，不管是否配置映射）：
  //  - 库节点名永远用映射前的源库名（即使该源库在目标侧被改名映射，节点名也不变）。
  //  - 库节点下的表 = 源库该库的全部表 ∪ 该源库映射到的目标库下的全部表（按裸名去重）。
  //  - 未配置映射时默认同名配对（源库 → 目标库同名库）。
  //  - 仅存在于映射目标库、源库没有的表，记入 sourceOnlySet（左侧标「仅目标库有」），不参与强制选中。
  // 注意：dbMapping 是对象引用，父组件每次渲染可能给出新引用但内容相同，直接作为依赖会
  // 引发无限重渲染；改用其内容序列化串 dbMappingKey 作为依赖，内容不变则不重算。
  const dbMappingKey = dbMapping ? JSON.stringify(dbMapping) : ""
  const [mergedTree, setMergedTree] = useState<DBTables[]>([])
  const [sourceOnlySet, setSourceOnlySet] = useState<Set<string>>(new Set<string>())
  useEffect(() => {
    if (extraTree.length === 0) {
      setMergedTree(tree)
      setSourceOnlySet(new Set<string>())
      return
    }
    // 源库名(小写) → 其映射目标库们的裸名集合
    // 优先级：显式 dbMapping > 默认同名；空对象/空值都视为未配置
    const parsed: Record<string, string> | undefined = dbMappingKey ? JSON.parse(dbMappingKey) : undefined
    const hasRealMapping = !!(parsed && Object.keys(parsed).length > 0)
    const entries: [string, string][] = hasRealMapping
      ? Object.entries(parsed).filter(([k, v]) => k && v)
      : tree.map((d) => [d.name, d.name])
    const targetDBsOf = new Map<string, string[]>()
    for (const [src, tgt] of entries) {
      const arr = targetDBsOf.get(src.toLowerCase()) || []
      arr.push(tgt)
      targetDBsOf.set(src.toLowerCase(), arr)
    }
    // 目标库名(小写) → 目标库裸表名集合
    const extraByDB = new Map<string, string[]>()
    for (const ed of extraTree) extraByDB.set(ed.name.toLowerCase(), ed.tables)

    const merged: DBTables[] = tree.map((d) => ({ ...d, tables: [...d.tables], schemas: d.schemas?.map((s) => ({ ...s, tables: [...s.tables], objects: s.objects })) }))
    const onlySet = new Set<string>()
    for (const d of merged) {
      const targets = targetDBsOf.get(d.name.toLowerCase()) || []
      if (hasSchemas(d)) {
        // PG：按 schema.表 维度合并目标库表（同名表在不同 schema 下可分别展示）
        const have = new Set(expandTables(d).map((e) => `${e.schema.toLowerCase()}.${e.table.toLowerCase()}`))
        for (const tgtDB of targets) {
          const tgt = extraTree.find((e) => e.name.toLowerCase() === tgtDB.toLowerCase())
          if (!tgt) continue
          for (const s of tgt.schemas ?? []) {
            for (const t of s.tables) {
              if (have.has(`${s.name.toLowerCase()}.${t.toLowerCase()}`)) continue
              const sch = d.schemas?.find((x) => x.name.toLowerCase() === s.name.toLowerCase())
              if (sch) sch.tables.push(t)
              else d.schemas?.push({ name: s.name, tables: [t] })
              have.add(`${s.name.toLowerCase()}.${t.toLowerCase()}`)
              onlySet.add(qual(d.name, `${s.name}.${t}`)) // 库名用映射前的源库名
            }
          }
        }
        // 同步裸名表集合（计数/兼容旧逻辑）
        d.tables = Array.from(new Set((d.schemas || []).flatMap((s) => s.tables)))
        continue
      }
      const have = new Set(d.tables.map((t) => t.toLowerCase()))
      for (const tgtDB of targets) {
        const tgtTables = extraByDB.get(tgtDB.toLowerCase())
        if (!tgtTables) continue
        for (const t of tgtTables) {
          if (!have.has(t.toLowerCase())) {
            d.tables.push(t)
            have.add(t.toLowerCase())
            onlySet.add(qual(d.name, t)) // 库名用映射前的源库名
          }
        }
      }
    }
    setMergedTree(merged)
    setSourceOnlySet(onlySet)
  }, [tree, extraTree, dbMappingKey])

  const singleMode = mergedTree.length <= 1

  // 某库的全部对象 ID（PG 为 "库.schema.目录/名"）
  const dbObjectIds = (d: DBTables): string[] => {
    if (!showObjects) return []
    if (hasSchemas(d)) {
      return (d.schemas || []).flatMap((s) =>
        OBJECT_GROUPS.flatMap((g) => (s.objects?.[g.dir] || []).map((n) => qual(d.name, `${s.name}.${g.dir}/${n}`))),
      )
    }
    return OBJECT_GROUPS.flatMap((g) => (d.objects?.[g.dir] || []).map((n) => qual(d.name, `${g.dir}/${n}`)))
  }

  // 关键字过滤：表名与对象名同时匹配，保留有命中的库/schema 与分组
  const filteredTree = useMemo<DBTables[]>(() => {
    if (!keyword) return mergedTree
    const kw = keyword.toLowerCase()
    return mergedTree
      .map((d) => {
        const tables = d.tables.filter((t) => t.toLowerCase().includes(kw))
        const objects: Record<string, string[]> = {}
        OBJECT_GROUPS.forEach((g) => {
          const names = (d.objects?.[g.dir] || []).filter((n) => n.toLowerCase().includes(kw))
          if (names.length > 0) objects[g.dir] = names
        })
        if (hasSchemas(d)) {
          const schemas = (d.schemas || [])
            .map((s) => {
              const st = s.tables.filter((t) => t.toLowerCase().includes(kw))
              const so: Record<string, string[]> = {}
              if (showObjects) {
                OBJECT_GROUPS.forEach((g) => {
                  const names = (s.objects?.[g.dir] || []).filter((n) => n.toLowerCase().includes(kw))
                  if (names.length > 0) so[g.dir] = names
                })
              }
              return { ...s, tables: st, objects: Object.keys(so).length > 0 ? so : undefined }
            })
            .filter((s) => s.tables.length > 0 || (s.objects && Object.keys(s.objects).length > 0))
          return { ...d, tables, objects: showObjects ? objects : undefined, schemas }
        }
        return { ...d, tables, objects: showObjects ? objects : undefined }
      })
      .filter((d) => d.tables.length > 0 || (d.schemas?.length ?? 0) > 0 || Object.keys(d.objects || {}).length > 0)
  }, [mergedTree, keyword, showObjects])

  const totalTables = mergedTree.reduce((n, d) => n + dbTableIds(d).length, 0)
  const totalObjects = showObjects ? tree.reduce((n, d) => n + dbObjectIds(d).length, 0) : 0

  // 右侧「已选内容」严格只展示用户手动勾选的表（selected 内均为 "源库.表" 或 "源库.schema.表" 限定名）。
  const effectiveSelected = useMemo<string[]>(() => selected, [selected])

  const allVisibleTables = filteredTree.flatMap((d) => dbTableIds(d))
  const allVisibleObjects = showObjects ? filteredTree.flatMap((d) => dbObjectIds(d)) : []
  const allChecked = (allVisibleTables.length + allVisibleObjects.length) > 0 &&
    allVisibleTables.every((t) => selected.includes(t)) &&
    allVisibleObjects.every((o) => selectedObjects.includes(o))

  const findDBOf = (tableId: string) => {
    const dbName = entryDB(tableId)
    return dbName ? mergedTree.find((d) => d.name === dbName) : mergedTree.find((d) => d.tables.includes(tableId))
  }
  const findDBOfObject = (id: string) => {
    const dbName = entryDB(id)
    return dbName ? mergedTree.find((d) => d.name === dbName) : mergedTree.find((d) => dbObjectIds(d).includes(id))
  }

  // 归一化旧格式条目（裸名 / "库.表" 旧配置）：升级为当前树结构（PG 下补 schema 前缀），树中无法解析的清除
  useEffect(() => {
    if (loading || mergedTree.length === 0) return
    const legacy = (id: string) => !entryDB(id) || isLegacyQual(id)
    if (!selected.some(legacy) && !selectedObjects.some(legacy)) return
    const findSchemaOf = (d: DBTables, bare: string): string => {
      if (!hasSchemas(d)) return ""
      for (const s of d.schemas || []) {
        if (s.tables.some((x) => x.toLowerCase() === bare.toLowerCase())) return s.name
      }
      return ""
    }
    const upTable = (t: string) => {
      if (isLegacyQual(t)) {
        // "库.表"：在该库下定位同名表并补 schema 前缀（找不到则保持原样，后端仍可按裸名匹配）
        const dbName = entryDB(t)
        const bare = stripDB(t)
        const d = mergedTree.find((x) => x.name === dbName)
        const sch = d ? findSchemaOf(d, bare) : ""
        return sch ? qual(dbName, `${sch}.${bare}`) : t
      }
      const d = mergedTree.find((x) => x.tables.some((tt) => tt.toLowerCase() === t.toLowerCase()))
      if (!d) return ""
      const sch = findSchemaOf(d, t)
      return sch ? qual(d.name, `${sch}.${t}`) : qual(d.name, t)
    }
    const upObj = (o: string) => {
      if (isLegacyQual(o)) {
        // "库.目录/名"：在该库各 schema 的对象分组中定位并补 schema 前缀
        const dbName = entryDB(o)
        const rest = stripDB(o)
        const slash = rest.indexOf("/")
        const dir = slash > 0 ? rest.slice(0, slash) : ""
        const name = slash > 0 ? rest.slice(slash + 1) : rest
        const d = mergedTree.find((x) => x.name === dbName)
        if (d && hasSchemas(d)) {
          for (const s of d.schemas || []) {
            if (dir && (s.objects?.[dir] || []).some((x) => x.toLowerCase() === name.toLowerCase())) return qual(dbName, `${s.name}.${rest}`)
          }
        }
        return o
      }
      const slash = o.indexOf("/")
      const dir = slash > 0 ? o.slice(0, slash) : ""
      const name = slash > 0 ? o.slice(slash + 1) : o
      for (const d of mergedTree) {
        if (hasSchemas(d)) {
          for (const s of d.schemas || []) {
            if (dir && (s.objects?.[dir] || []).some((x) => x.toLowerCase() === name.toLowerCase())) return qual(d.name, `${s.name}.${o}`)
          }
        } else if (dbObjectIds(d).some((id) => stripDB(id) === o)) {
          return qual(d.name, o)
        }
      }
      return ""
    }
    const nextTables = Array.from(new Set(selected.map((t) => (entryDB(t) && !isLegacyQual(t) ? t : upTable(t))).filter(Boolean)))
    const nextObjs = Array.from(new Set(selectedObjects.map((o) => (entryDB(o) && !isLegacyQual(o) ? o : upObj(o))).filter(Boolean)))
    const nextConds = conditions
      .map((c) => (entryDB(c.tableName) && !isLegacyQual(c.tableName) ? c : { ...c, tableName: upTable(c.tableName) }))
      .filter((c) => c.tableName)
    onChange(nextTables, nextObjs, nextConds)
  }, [loading, mergedTree, selected, selectedObjects, conditions, onChange])

  // 同步 selectedDBs（多库导出模式）：勾选项→纳入所属库；取消最后一项→移除库
  const syncDBs = (nextSelected: string[], nextObjects: string[]) => {
    if (!onDBsChange) return
    const cur = selectedDBs || []
    const allItems = [...nextSelected, ...nextObjects]
    const keep: string[] = []
    for (const d of mergedTree) {
      const hasTable = nextSelected.some((t) => {
        const dbName = entryDB(t)
        return dbName ? dbName === d.name : d.tables.includes(t)
      })
      const hasObj = nextObjects.some((o) => {
        const dbName = entryDB(o)
        return dbName ? dbName === d.name : dbObjectIds(d).includes(o)
      })
      if (hasTable || hasObj) {
        if (!keep.includes(d.name)) keep.push(d.name)
      }
    }
    // 保留显式选中的库（selectedDBs 中有但当前无子项选中 = 整库导出）
    for (const name of cur) if (!keep.includes(name) && mergedTree.some((d) => d.name === name)) keep.push(name)
    void allItems
    onDBsChange(keep)
  }

  const toggleTable = (table: string, checked: boolean) => {
    const next = checked ? [...selected, table] : selected.filter((t) => t !== table)
    const nextConds = checked ? conditions : conditions.filter((c) => c.tableName !== table)
    syncDBs(next, selectedObjects)
    onChange(next, selectedObjects, nextConds)
  }

  const toggleObject = (id: string, checked: boolean) => {
    const next = checked ? [...selectedObjects, id] : selectedObjects.filter((o) => o !== id)
    syncDBs(selected, next)
    onChange(selected, next, conditions)
  }

  const toggleAll = (checked: boolean) => {
    if (onDBsChange) {
      const visibleDBs = filteredTree.map((d) => d.name)
      const cur = selectedDBs || []
      onDBsChange(checked ? Array.from(new Set([...cur, ...visibleDBs])) : cur.filter((n) => !visibleDBs.includes(n)))
    }
    if (checked) {
      onChange(
        Array.from(new Set([...selected, ...allVisibleTables])),
        Array.from(new Set([...selectedObjects, ...allVisibleObjects])),
        conditions,
      )
    } else {
      onChange(
        selected.filter((t) => !allVisibleTables.includes(t)),
        selectedObjects.filter((o) => !allVisibleObjects.includes(o)),
        conditions.filter((c) => !allVisibleTables.includes(c.tableName)),
      )
    }
  }

  // 库节点勾选：选中/取消该库下所有表与对象（无 onDBsChange 时仅级联子项，不记录库）
  const toggleDB = (name: string, checked: boolean) => {
    const cur = selectedDBs || []
    const d = mergedTree.find((x) => x.name === name)
    if (!d) return
    const dbTables = dbTableIds(d)
    const dbObjs = dbObjectIds(d)
    onDBsChange?.(checked ? Array.from(new Set([...cur, name])) : cur.filter((n) => n !== name))
    if (checked) {
      onChange(
        Array.from(new Set([...selected, ...dbTables])),
        Array.from(new Set([...selectedObjects, ...dbObjs])),
        conditions,
      )
    } else {
      onChange(
        selected.filter((t) => !dbTables.includes(t)),
        selectedObjects.filter((o) => !dbObjs.includes(o)),
        conditions.filter((c) => !dbTables.includes(c.tableName)),
      )
    }
  }

  // 分组勾选：选中/取消该分组下所有项
  const toggleGroup = (dbName: string, dir: string, ids: string[], checked: boolean) => {
    if (dir === "tables") {
      const next = checked
        ? Array.from(new Set([...selected, ...ids]))
        : selected.filter((t) => !ids.includes(t))
      syncDBs(next, selectedObjects)
      onChange(next, selectedObjects, checked ? conditions : conditions.filter((c) => !ids.includes(c.tableName)))
    } else {
      const next = checked
        ? Array.from(new Set([...selectedObjects, ...ids]))
        : selectedObjects.filter((o) => !ids.includes(o))
      syncDBs(selected, next)
      onChange(selected, next, conditions)
    }
  }

  const openEdit = (table: string) => {
    const cond = conditions.find((c) => c.tableName === table)
    setEditing(table)
    setEditCols(cond?.columns || []) // 兼容旧配置：列选择仍可在可视化模式使用
    setEditDataMode(cond?.dataMode || "") // 加载数据模式
    setShowColRef(false) // 列参考默认折叠
    setColRefPos(null)
    // 完整 SELECT：先进 SQL 模式，AST 回填成功时自动切可视化（含重建校验保证语义不变）
    // 旧版 WHERE 片段：尝试解析为可视化条件，失败则退回 SQL 模式
    let parsed: { conds: WhereCond[]; ok: boolean } = { conds: [], ok: false }
    if (cond?.query?.trim()) {
      const q = cond.query.trim()
      setEditQuery(q)
      setVisConds([])
      setWhereMode("sql")
      const token = ++restoreSeq.current
      void restoreSelectViaAst(q).then((r) => {
        if (restoreSeq.current !== token || !r.ok) return // 弹窗已关闭/重开，或不可回填
        setEditCols(r.cols)
        setVisConds(r.conds)
        setWhereMode("visual")
      })
    } else {
      const legacyWhere = cond?.where || ""
      setEditQuery(legacyWhere)
      parsed = parseWhere(legacyWhere)
      if (parsed.ok) {
        setVisConds(parsed.conds)
        setWhereMode("visual")
      } else {
        setVisConds([])
        setWhereMode("sql")
      }
    }
    // 查找表所属库并加载列信息（附加来源表从附加连接取列）
    const dbEntry = findDBOf(table)
    const dbName = dbEntry?.name || db || ""
    const colsConnId = sourceOnlySet.has(table) && extraConnId ? extraConnId : connId
    setColumns([])
    setColsError("")
    setColsLoading(true)
    api
      .getTableColumns(colsConnId, dbName, stripDB(table))
      .then((r) => {
        setColumns(r.columns || [])
        // 如果可视化条件中有空列名，用第一个列填充
        if (parsed.ok && parsed.conds.length > 0) {
          const firstCol = r.columns?.[0]?.name || ""
          setVisConds(parsed.conds.map((c) => c.column ? c : { ...c, column: firstCol }))
        }
      })
      .catch((e: Error) => setColsError(e.message))
      .finally(() => setColsLoading(false))
  }

  // 点击列名插入到 SQL textarea 光标位置（SQL 模式）
  const insertColumn = (colName: string) => {
    const ta = whereRef.current
    if (!ta) {
      setEditQuery((prev) => prev + colName)
      return
    }
    const start = ta.selectionStart ?? editQuery.length
    const end = ta.selectionEnd ?? editQuery.length
    const newText = editQuery.slice(0, start) + colName + editQuery.slice(end)
    setEditQuery(newText)
    requestAnimationFrame(() => {
      ta.focus()
      ta.setSelectionRange(start + colName.length, start + colName.length)
    })
  }

  // 切换导出列选中
  const toggleCol = (name: string, checked: boolean) => {
    setEditCols(checked ? [...editCols, name] : editCols.filter((n) => n !== name))
  }

  // 可视化条件操作
  const addCond = () => {
    const firstCol = columns[0]?.name || ""
    setVisConds([...visConds, { column: firstCol, operator: "=", value: "", connector: "AND" }])
  }
  const updateCond = (idx: number, patch: Partial<WhereCond>) => {
    setVisConds(visConds.map((c, i) => (i === idx ? { ...c, ...patch } : c)))
  }
  const removeCond = (idx: number) => {
    setVisConds(visConds.filter((_, i) => i !== idx))
  }

  // 切换模式：可视化→SQL 拼成完整 SELECT；SQL→可视化 三级降级回填（AST → 正则 → 确认丢弃）
  const switchMode = async (mode: "visual" | "sql") => {
    if (mode === whereMode) return
    if (mode === "sql") {
      // 可视化→SQL：导出列 + 可视化条件拼装为完整 SELECT（表名用去掉库前缀的可执行名）
      setEditQuery(buildSelect(stripDB(editing || ""), editCols, serializeWhere(visConds)))
      setWhereMode(mode)
      return
    }
    // SQL→可视化
    if (!editQuery.trim()) {
      setVisConds([])
      setWhereMode(mode)
      return
    }
    const sql = editQuery
    // 一级：AST 回填（懒加载 node-sql-parser，含重建等价校验）
    const restored = await restoreSelectViaAst(sql)
    if (restored.ok) {
      setEditCols(restored.cols)
      setVisConds(restored.conds)
      setWhereMode(mode)
      return
    }
    // 二级：正则回填（简单 SELECT 分解 + WHERE 片段解析）
    const sel = parseSelect(sql)
    const parsed = parseWhere(sel.ok ? sel.where : sql)
    if (sel.ok && parsed.ok) {
      const firstCol = columns[0]?.name || ""
      setEditCols(sel.cols)
      setVisConds(parsed.conds.map((c) => c.column ? c : { ...c, column: firstCol }))
      setWhereMode(mode)
      return
    }
    // 三级：复杂语法无法还原，确认后丢弃
    const ok = await confirm({
      title: tr("tablePicker.confirmModeTitle"),
      description: tr("tablePicker.confirmModeDesc"),
      confirmText: tr("tablePicker.continueSwitch"),
    })
    if (!ok) return // 留在 SQL 模式
    setVisConds([{ column: columns[0]?.name || "", operator: "=", value: "", connector: "AND" }])
    setWhereMode(mode)
  }

  const saveEdit = () => {
    if (!editing) return
    const next = conditions.filter((c) => c.tableName !== editing)
    // skip 模式：只记录 dataMode
    if (editDataMode === "skip") {
      next.push({ tableName: editing, dataMode: "skip" })
    } else {
      // 条件归一化为完整 SELECT：SQL 模式直接取文本，可视化模式由 导出列+条件 拼装
      let query = ""
      if (whereMode === "sql") {
        query = editQuery.trim().replace(/;+\s*$/, "")
        if (query && !/^select\s/i.test(query)) {
          // 兼容旧版只写 WHERE 片段的写法
          query = buildSelect(stripDB(editing), [], query)
        }
        // SELECT * FROM 表（未指定列也无条件）等价全量，不记录
        const sel = parseSelect(query)
        if (sel.ok && sel.cols.length === 0 && !sel.where) query = ""
      } else {
        const whereStr = serializeWhere(visConds)
        if (whereStr || editCols.length > 0) query = buildSelect(stripDB(editing), editCols, whereStr)
      }
      if (editDataMode === "condition") {
        next.push(query ? { tableName: editing, dataMode: "condition", query } : { tableName: editing, dataMode: "condition" })
      } else if (query) {
        next.push({ tableName: editing, query })
      }
    }
    onChange(selected, selectedObjects, next)
    closeEdit()
  }

  // 表行
  const tableRow = (t: string) => {
    const isChecked = selected.includes(t)
    const cond = conditions.find((c) => c.tableName === t)
    const dataMode = cond?.dataMode || ""
    const hasCond = !!cond && (dataMode === "condition" || !!cond.query || !!cond.where || (cond.columns?.length ?? 0) > 0)
    return (
      <div
        key={t}
        className={`flex items-center justify-between rounded px-2 py-1.5 transition-colors hover:bg-accent ${isChecked ? "bg-primary/5" : ""}`}
      >
        <label className="flex flex-1 cursor-pointer items-center gap-2 text-sm">
          <Checkbox checked={isChecked} onCheckedChange={(v) => toggleTable(t, v === true)} />
          <Table2 className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          {/* 树中库名已由父节点体现，仅显示裸表名（schema 归属由 schema 节点体现）；title 保留限定名便于悬停确认 */}
          <span className="truncate" title={t}>{leafName(t)}</span>
          {sourceOnlySet.has(t) && (
            <span className="rounded bg-amber-500/10 px-1 py-0.5 text-[10px] text-amber-600 dark:text-amber-400">{effectiveExtraLabel}</span>
          )}
          {dataMode === "skip" && (
            <span className="rounded bg-muted px-1 py-0.5 text-[10px] text-muted-foreground">{tr("tablePicker.skipData")}</span>
          )}
          {hasCond && (
            <span title={tr("tablePicker.hasCond")} className="flex items-center gap-0.5 text-xs text-blue-600">
              <Filter className="h-3 w-3" /> {tr("tablePicker.byCond")}
            </span>
          )}
        </label>
        {isChecked && (
          <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={() => openEdit(t)}>
            <Settings2 className="mr-1 h-3 w-3" /> {tr("tablePicker.settings")}
          </Button>
        )}
      </div>
    )
  }

  // 对象行
  const objectRow = (id: string, Icon: typeof Eye) => {
    const name = id.split("/")[1]
    const isChecked = selectedObjects.includes(id)
    return (
      <div
        key={id}
        className={`flex items-center px-2 py-1.5 rounded transition-colors hover:bg-accent ${isChecked ? "bg-primary/5" : ""}`}
      >
        <label className="flex flex-1 cursor-pointer items-center gap-2 text-sm">
          <Checkbox checked={isChecked} onCheckedChange={(v) => toggleObject(id, v === true)} />
          <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="truncate">{name}</span>
        </label>
      </div>
    )
  }

  // 分组区块（表 / 视图 / 函数 / 存储过程）；keyPrefix 用于 schema 分层时隔离各 schema 的折叠状态
  const groupSection = (
    dbName: string,
    dir: string,
    label: string,
    Icon: typeof Table2,
    ids: string[],
    keyPrefix = "",
  ) => {
    if (ids.length === 0) return null
    const key = keyPrefix ? `${keyPrefix}:${dir}` : grpKey(dbName, dir)
    const isCol = collapsed[key]
    const sel = dir === "tables" ? selected : selectedObjects
    const selCount = ids.filter((id) => sel.includes(id)).length
    const state: boolean | "indeterminate" = selCount === ids.length ? true : selCount > 0 ? "indeterminate" : false
    return (
      <div className="py-0.5">
        <div className="flex items-center gap-1.5 rounded px-1 py-1 hover:bg-accent/50">
          <Button
            variant="ghost"
            size="sm"
            className="h-5 w-5 p-0"
            onClick={() => setCollapsed((c) => ({ ...c, [key]: !c[key] }))}
          >
            {isCol ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
          </Button>
          <Checkbox
            checked={state}
            onCheckedChange={(v) => toggleGroup(dbName, dir, ids, v === true)}
          />
          <Icon className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
          <span className="flex-1 cursor-pointer text-xs font-medium text-foreground" onClick={() => setCollapsed((c) => ({ ...c, [key]: !c[key] }))}>
            {label}
            <span className="ml-1.5 font-normal text-muted-foreground">{ids.length}</span>
            {selCount > 0 && <span className="ml-1.5 font-normal text-primary">{tr("tablePicker.selectedCount", { n: selCount })}</span>}
          </span>
        </div>
        {!isCol && <div className="ml-5 border-l border-border/60 pl-3">{dir === "tables" ? ids.map(tableRow) : ids.map((id) => objectRow(id, Icon))}</div>}
      </div>
    )
  }

  // schema 节点（PG 分层：库 → schema → 分组 → 项）；勾选级联该 schema 下全部表与对象
  const schemaSection = (d: DBTables, s: DBSchema) => {
    const sk = `sch:${d.name}:${s.name}`
    const isCol = collapsed[sk]
    const ids = s.tables.map((t) => qual(d.name, `${s.name}.${t}`))
    const objIds = showObjects
      ? OBJECT_GROUPS.flatMap((g) => (s.objects?.[g.dir] || []).map((n) => qual(d.name, `${s.name}.${g.dir}/${n}`)))
      : []
    const selCount = ids.filter((t) => selected.includes(t)).length + objIds.filter((o) => selectedObjects.includes(o)).length
    const total = ids.length + objIds.length
    const checked: boolean | "indeterminate" = selCount === total ? true : selCount > 0 ? "indeterminate" : false
    const toggle = (v: boolean) => {
      const nextSel = v ? Array.from(new Set([...selected, ...ids])) : selected.filter((t) => !ids.includes(t))
      const nextObj = v ? Array.from(new Set([...selectedObjects, ...objIds])) : selectedObjects.filter((o) => !objIds.includes(o))
      syncDBs(nextSel, nextObj)
      onChange(nextSel, nextObj, v ? conditions : conditions.filter((c) => !ids.includes(c.tableName)))
    }
    return (
      <div key={s.name} className="py-0.5">
        <div className="flex items-center gap-1.5 rounded px-1 py-1 hover:bg-accent/50">
          <Button
            variant="ghost"
            size="sm"
            className="h-5 w-5 p-0"
            onClick={() => setCollapsed((c) => ({ ...c, [sk]: !c[sk] }))}
          >
            {isCol ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
          </Button>
          <Checkbox checked={checked} onCheckedChange={(v) => toggle(v === true)} />
          <span
            className="flex-1 cursor-pointer text-xs font-medium text-foreground"
            onClick={() => setCollapsed((c) => ({ ...c, [sk]: !c[sk] }))}
          >
            {s.name}
            <span className="ml-1.5 font-normal text-muted-foreground">{total}</span>
            {selCount > 0 && <span className="ml-1.5 font-normal text-primary">{tr("tablePicker.selectedCount", { n: selCount })}</span>}
          </span>
        </div>
        {!isCol && (
          <div className="ml-4 border-l border-border/60 pl-2">
            {groupSection(d.name, "tables", tr("objectTree.group.table"), Table2, ids, sk)}
            {showObjects && OBJECT_GROUPS.map((g) =>
              groupSection(d.name, g.dir, tKey(g.label), g.Icon, (s.objects?.[g.dir] || []).map((n) => qual(d.name, `${s.name}.${g.dir}/${n}`)), sk),
            )}
          </div>
        )}
      </div>
    )
  }

  // 库节点（多库树形模式）；未加载的库灰显，点击才加载对象清单
  const dbSection = (d: DBTables) => {
    const dk = dbKey(d.name)
    const isCol = collapsed[dk]
    // 分级加载：_loaded !== true 表示仅库名（灰显，点击加载）
    const pending = d._loaded !== true
    const loadingThis = loadingDb === `src:${d.name}`
    const tableIds = dbTableIds(d)
    const objIds = dbObjectIds(d)
    const selTables = tableIds.filter((t) => selected.includes(t)).length
    const selObjs = objIds.filter((o) => selectedObjects.includes(o)).length
    const selTotal = selTables + selObjs
    const total = tableIds.length + objIds.length
    const dbChecked: boolean | "indeterminate" =
      total === 0
        ? !!selectedDBs?.includes(d.name)
        : selTotal === total ? true : selTotal > 0 ? "indeterminate" : !!selectedDBs?.includes(d.name)
    const onClickRow = () => {
      if (pending) {
        void loadDb(d.name, "src")
        return
      }
      setCollapsed((c) => ({ ...c, [dk]: !c[dk] }))
    }
    return (
      <div key={d.name} className="border-b last:border-0">
        <div className={cn("flex items-center gap-1.5 rounded px-2 py-1.5", pending ? "bg-muted/30" : "bg-muted/60")}>
          <Button
            variant="ghost"
            size="sm"
            className="h-5 w-5 p-0"
            onClick={onClickRow}
          >
            {loadingThis ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : isCol ? <ChevronRight className="h-3.5 w-3.5" /> : <ChevronDown className="h-3.5 w-3.5" />}
          </Button>
          {onDBsChange && (
            <Checkbox checked={dbChecked} onCheckedChange={(v) => toggleDB(d.name, v === true)} />
          )}
          <Database className={cn("h-3.5 w-3.5 shrink-0", pending ? "text-muted-foreground/50" : "text-muted-foreground")} />
          <span
            className={cn("flex-1 cursor-pointer text-sm font-medium", pending && "text-muted-foreground/70")}
            onClick={onClickRow}
          >
            {d.name}
            {d._error && <span title={d._error} className="ml-2 inline-flex align-middle"><AlertCircle className="h-3 w-3 text-destructive" /></span>}
            {!pending && (
              <span className="ml-2 text-xs font-normal text-muted-foreground">
                {tr("tablePicker.tablesCount", { n: tableIds.length })}{showObjects && objIds.length > 0 ? tr("tablePicker.totalObjects", { n: objIds.length }) : ""}
              </span>
            )}
            {!pending && selTotal > 0 && <span className="ml-2 text-xs font-normal text-primary">{tr("tablePicker.selectedCount", { n: selTotal })}</span>}
          </span>
        </div>
        {!isCol && !pending && (
          <div className="ml-5 border-l border-border/60 pl-3">
            {hasSchemas(d)
              ? (d.schemas || []).map((s) => schemaSection(d, s))
              : (
                <>
                  {groupSection(d.name, "tables", tr("objectTree.group.table"), Table2, tableIds)}
                  {showObjects && OBJECT_GROUPS.map((g) =>
                    groupSection(d.name, g.dir, tKey(g.label), g.Icon, (d.objects?.[g.dir] || []).map((n) => qual(d.name, `${g.dir}/${n}`))),
                  )}
                </>
              )}
          </div>
        )}
      </div>
    )
  }

  // 单库模式：直接渲染分组（无库头）
  const singleGroups = (d: DBTables) => (
    <div className="ml-2 border-l border-border/60 pl-3">
      {hasSchemas(d)
        ? (d.schemas || []).map((s) => schemaSection(d, s))
        : (
          <>
            {groupSection(d.name, "tables", tr("objectTree.group.table"), Table2, d.tables.map((t) => qual(d.name, t)))}
            {showObjects && OBJECT_GROUPS.map((g) =>
              groupSection(d.name, g.dir, tKey(g.label), g.Icon, (d.objects?.[g.dir] || []).map((n) => qual(d.name, `${g.dir}/${n}`))),
            )}
          </>
        )}
    </div>
  )

  return (
    <div className="grid min-h-0 flex-1 grid-cols-2 gap-4">
      {/* 左：可用表与对象（库→分组→项 树形） */}
      <Card className="flex min-h-0 flex-col p-3">
        <div className="mb-2 flex items-center justify-between">
          <span className="text-sm font-medium">
            {!singleMode ? tr("tablePicker.availableContentMulti", { dbs: tree.length, tables: totalTables }) : tr("tablePicker.availableContent", { tables: totalTables })}{showObjects && totalObjects > 0 ? tr("tablePicker.totalObjects", { n: totalObjects }) : ""}{tr("tablePicker.availableTail")}
          </span>
          <label className="flex items-center gap-1.5 text-sm text-muted-foreground">
            <Checkbox checked={allChecked} onCheckedChange={(v) => toggleAll(v === true)} />
            {tr("tablePicker.selectAll")}
          </label>
        </div>
        {extraConnId && extraError && (
          <div className="mb-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-2.5 py-1.5 text-xs text-amber-700 dark:text-amber-300">
            {tr("tablePicker.extraLoadFailDesc", { err: extraError })}
          </div>
        )}
        {extraConnId && !extraError && extraTree.length === 0 && !loading && (
          <div className="mb-2 rounded-md border border-amber-500/30 bg-amber-500/10 px-2.5 py-1.5 text-xs text-amber-700 dark:text-amber-300">
            {tr("tablePicker.extraEmptyDesc")}
          </div>
        )}
        <Input
          placeholder={tr("tablePicker.searchPlaceholder")}
          value={keyword}
          onChange={(e) => setKeyword(e.target.value)}
          className="mb-2 h-8"
        />
        {/* 列表区只限制最大高度（不超卡片剩余空间）：内容少时自适应高度不出现空白，内容多时内部滚动 */}
        <div className="min-h-0 flex-1">
          {/* 原生滚动容器（ScrollArea 的 Viewport h-full 在 Root 高度 auto 时无法解析，超高内容被裁剪无滚动） */}
          <div className="scrollbar-thin max-h-full overflow-y-auto rounded border pr-1">
          {loading && (
            <div className="flex items-center justify-center py-8 text-muted-foreground">
              <Loader2 className="mr-2 h-4 w-4 animate-spin" /> {tr("tablePicker.loading")}
            </div>
          )}
          {error && <div className="p-3 text-sm text-destructive">{tr("tablePicker.loadFailed", { err: error })}</div>}
          {!loading && !error && filteredTree.length === 0 && (
            <div className="py-8 text-center text-sm text-muted-foreground">{tr("tablePicker.noData")}</div>
          )}
          {!loading && !error && singleMode && filteredTree[0] && (filteredTree[0]._loaded === true ? singleGroups(filteredTree[0]) : (
            <div className="flex items-center justify-center gap-2 py-6 text-sm text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" /> {tr("tablePicker.loading")}
            </div>
          ))}
          {!loading && !error && !singleMode && filteredTree.map(dbSection)}
          </div>
        </div>
      </Card>

      {/* 右：已选表及条件 + 已选对象 */}
      <Card className="flex min-h-0 flex-col p-3">
        <div className="mb-2 text-sm font-medium">
          {tr("tablePicker.selectedContent", { tables: effectiveSelected.length })}{showObjects ? tr("tablePicker.totalObjects", { n: selectedObjects.length }) : ""}{tr("tablePicker.availableTail")}
        </div>
        <div className="min-h-0 flex-1">
          {/* 原生滚动容器（ScrollArea 的 Viewport h-full 在 Root 高度 auto 时无法解析，超高内容被裁剪无滚动） */}
          <div className="scrollbar-thin max-h-full overflow-y-auto rounded border pr-1">
          {effectiveSelected.length === 0 && selectedObjects.length === 0 && (
            <div className="py-8 text-center text-sm text-muted-foreground">{tr("tablePicker.nothingSelected")}</div>
          )}
          {effectiveSelected.map((t) => {
            const isExtra = sourceOnlySet.has(t)
            const cond = conditions.find((c) => c.tableName === t)
            return (
              <div key={t} className="border-b px-2 py-2 last:border-0">
                <div className="flex items-center justify-between">
                  <span className="flex items-center gap-1.5 text-sm font-medium" title={t}>
                    <Table2 className="h-3.5 w-3.5 text-muted-foreground" /> {t}
                    {isExtra && (
                      <span className="flex items-center gap-0.5 rounded bg-amber-500/10 px-1 py-0.5 text-[10px] font-normal text-amber-600 dark:text-amber-400">
                        {effectiveExtraLabel}
                      </span>
                    )}
                    {cond && (
                      <span className="flex items-center gap-0.5 rounded bg-blue-500/10 px-1 py-0.5 text-[10px] font-normal text-blue-600 dark:text-blue-400">
                        <Filter className="h-2.5 w-2.5" /> {tr("tablePicker.hasCondition")}
                      </span>
                    )}
                  </span>
                  <div className="flex gap-1">
                    <Button variant="ghost" size="sm" className="h-6 px-2 text-xs" title={tr("tablePicker.setCondition")} onClick={() => openEdit(t)}>
                      <Settings2 className="h-3 w-3" />
                    </Button>
                    {!isExtra && (
                      <Button variant="ghost" size="sm" className="h-6 px-2" title={tr("tablePicker.remove")} onClick={() => toggleTable(t, false)}>
                        <Trash2 className="h-3 w-3 text-destructive" />
                      </Button>
                    )}
                  </div>
                </div>
                <div className="mt-1 break-all text-xs text-muted-foreground">
                  {cond?.query
                    ? tr("tablePicker.sqlLabel", { sql: cond.query })
                    : cond?.where
                      ? tr("tablePicker.whereLegacy", { where: cond.where })
                      : tr("tablePicker.noCondFull")}
                </div>
                {!cond?.query && cond?.columns && cond.columns.length > 0 && (
                  <div className="break-all text-xs text-muted-foreground">{tr("tablePicker.colsLabel", { cols: cond.columns.join(",") })}</div>
                )}
              </div>
            )
          })}
          {selectedObjects.length > 0 && (
            <div className="border-t pt-1">
              {selectedObjects.map((id) => {
                const parts = id.split("/")
                const name = parts[parts.length - 1]
                const dir = parts.length > 1 ? (parts[parts.length - 2].split(".").pop() || "") : ""
                const g = OBJECT_GROUPS.find((x) => x.dir === dir)
                const Icon = g?.Icon ?? Eye
                return (
                  <div key={id} className="flex items-center justify-between border-b px-2 py-2 last:border-0">
                    <span className="flex items-center gap-1.5 text-sm">
                      <Icon className="h-3.5 w-3.5 text-muted-foreground" />
                      <span className="text-xs font-normal text-muted-foreground">{g ? tKey(g.label) : ""}</span>
                      {name}
                    </span>
                    <Button variant="ghost" size="sm" className="h-6 px-2" title={tr("tablePicker.remove")} onClick={() => toggleObject(id, false)}>
                      <Trash2 className="h-3 w-3 text-destructive" />
                    </Button>
                  </div>
                )
              })}
            </div>
          )}
          </div>
        </div>
      </Card>

      {/* 条件编辑弹窗 */}
      <Dialog open={!!editing} onOpenChange={(o) => !o && closeEdit()}>
        <DialogContent className="sm:max-w-[760px] h-[580px] flex flex-col" onEscapeKeyDown={(e) => e.preventDefault()}>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Table2 className="h-4 w-4 text-muted-foreground" />
              {editing}
              <span className="text-xs font-normal text-muted-foreground">{tr("tablePicker.condSettings")}</span>
            </DialogTitle>
          </DialogHeader>

          {/* 可滚动内容区域 */}
          <div className="flex-1 overflow-y-auto space-y-4 py-2">

          {/* 数据导出模式选择 */}
          <div className="space-y-1.5">
            <Label className="text-xs">{tr("tablePicker.dataExport")}</Label>
            <div className="flex gap-2">
              <button
                type="button"
                onClick={() => setEditDataMode("")}
                className={`flex-1 rounded-md border px-3 py-2 text-left text-xs transition-colors ${
                  editDataMode === "" ? "border-primary bg-primary/5 text-primary" : "border-border hover:bg-accent"
                }`}
              >
                <div className="font-medium">{tr("tablePicker.exportAll")}</div>
                <div className="mt-0.5 text-[10px] text-muted-foreground">{tr("tablePicker.exportAllDesc")}</div>
              </button>
              <button
                type="button"
                onClick={() => setEditDataMode("condition")}
                className={`flex-1 rounded-md border px-3 py-2 text-left text-xs transition-colors ${
                  editDataMode === "condition" ? "border-primary bg-primary/5 text-primary" : "border-border hover:bg-accent"
                }`}
              >
                <div className="font-medium">{tr("tablePicker.exportByCond")}</div>
                <div className="mt-0.5 text-[10px] text-muted-foreground">{tr("tablePicker.exportByCondDesc")}</div>
              </button>
              <button
                type="button"
                onClick={() => setEditDataMode("skip")}
                className={`flex-1 rounded-md border px-3 py-2 text-left text-xs transition-colors ${
                  editDataMode === "skip" ? "border-primary bg-primary/5 text-primary" : "border-border hover:bg-accent"
                }`}
              >
                <div className="font-medium">{tr("tablePicker.exportSkip")}</div>
                <div className="mt-0.5 text-[10px] text-muted-foreground">{tr("tablePicker.schemaOnlyDesc")}</div>
              </button>
            </div>
          </div>

          {/* 条件设置区：可视化 / SQL 为整体开关（含导出列选择） */}
          <div className={`space-y-2 ${editDataMode !== "condition" ? "opacity-50 pointer-events-none" : ""}`}>
            <div className="flex items-center justify-between">
              <Label className="text-xs">
                {whereMode === "visual" ? tr("tablePicker.condVisual") : tr("tablePicker.condFullSelect")}
              </Label>
              <div className="flex items-center gap-0.5 rounded-md border p-0.5">
                <button
                  type="button"
                  onClick={() => switchMode("visual")}
                  className={`flex items-center gap-1 rounded px-2 py-0.5 text-[11px] transition-colors ${
                    whereMode === "visual" ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent"
                  }`}
                >
                  <Filter className="h-3 w-3" /> {tr("tablePicker.visual")}
                </button>
                <button
                  type="button"
                  onClick={() => switchMode("sql")}
                  className={`flex items-center gap-1 rounded px-2 py-0.5 text-[11px] transition-colors ${
                    whereMode === "sql" ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-accent"
                  }`}
                >
                  <Code2 className="h-3 w-3" /> {tr("tablePicker.sqlMode")}
                </button>
              </div>
            </div>

            {/* 导出列选择（仅可视化模式：SQL 模式下导出列包含在 SELECT 中） */}
            {whereMode === "visual" && (
              <div className="space-y-1.5">
                <div className="flex items-center justify-between">
                  <Label className="text-xs">{tr("tablePicker.specifyCols")}</Label>
                  {columns.length > 0 && (
                    <div className="flex gap-2">
                      <button type="button" className="text-[10px] text-primary hover:underline" onClick={() => setEditCols(columns.map((c) => c.name))}>{tr("tablePicker.selectAll")}</button>
                      <button type="button" className="text-[10px] text-muted-foreground hover:underline" onClick={() => setEditCols([])}>{tr("tablePicker.clear")}</button>
                    </div>
                  )}
                </div>
                {colsLoading ? (
                  <div className="py-2 text-xs text-muted-foreground">{tr("tablePicker.loadingCols")}</div>
                ) : columns.length > 0 ? (
                  <div className="flex max-h-[100px] flex-wrap gap-1.5 overflow-y-auto rounded-md border p-2">
                    {columns.map((col) => {
                      const checked = editCols.includes(col.name)
                      return (
                        <button
                          key={col.name}
                          type="button"
                          onClick={() => toggleCol(col.name, !checked)}
                          className={`flex items-center gap-1 rounded px-1.5 py-0.5 text-[11px] transition-colors ${
                            checked ? "bg-primary text-primary-foreground" : "bg-muted text-muted-foreground hover:bg-accent"
                          }`}
                        >
                          {col.name}
                          {col.primaryKey && <TableIcon icon={KeyRound} size={12} />}
                          {!col.primaryKey && col.unique && <TableIcon icon={ShieldCheck} size={12} />}
                          {!col.primaryKey && !col.unique && col.indexed && <TableIcon icon={ListOrdered} size={12} />}
                        </button>
                      )
                    })}
                  </div>
                ) : (
                  <div className="py-2 text-xs text-muted-foreground">{tr("tablePicker.noCols")}</div>
                )}
              </div>
            )}

            {/* 查询条件 */}
            {whereMode === "visual" && (
              <Label className="text-xs">{tr("tablePicker.condOptional")}</Label>
            )}

            {/* 条件编辑区域 */}
            <div>
            {/* 可视化模式 */}
            {whereMode === "visual" && (
              <div className="space-y-1.5 rounded-md border p-2">
                {visConds.length === 0 && (
                  <div className="py-1 text-center text-xs text-muted-foreground">{tr("tablePicker.noCond")}</div>
                )}
                {visConds.map((c, i) => (
                  <div key={i} className="flex items-center gap-1.5">
                    {/* 连接符（首行无） */}
                    {i > 0 ? (
                      <Select
                        value={c.connector}
                        onValueChange={(v) => updateCond(i, { connector: v as "AND" | "OR" })}
                      >
                        <SelectTrigger className="h-7 w-[70px] shrink-0 text-xs"><SelectValue /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="AND">AND</SelectItem>
                          <SelectItem value="OR">OR</SelectItem>
                        </SelectContent>
                      </Select>
                    ) : (
                      <div className="w-[70px] shrink-0" />
                    )}

                    {/* 列名下拉 */}
                    <Select
                      value={c.column}
                      onValueChange={(v) => updateCond(i, { column: v })}
                    >
                      <SelectTrigger className="h-7 flex-1 text-xs"><SelectValue placeholder={tr("tablePicker.selectCol")} /></SelectTrigger>
                      <SelectContent>
                        {colsLoading ? (
                          <div className="flex items-center justify-center py-2 text-xs text-muted-foreground">
                            <Loader2 className="mr-1 h-3 w-3 animate-spin" /> {tr("tablePicker.loading")}
                          </div>
                        ) : columns.length === 0 ? (
                          <div className="py-2 text-center text-xs text-muted-foreground">{tr("tablePicker.noCols")}</div>
                        ) : (
                          columns.map((col) => (
                            <SelectItem key={col.name} value={col.name}>
                              <span className="font-mono">{col.name}</span>
                              <span className="ml-1.5 text-[10px] text-muted-foreground">{col.dataType}</span>
                              {col.primaryKey && <span className="ml-1 text-[9px] text-amber-500">PK</span>}
                            </SelectItem>
                          ))
                        )}
                      </SelectContent>
                    </Select>

                    {/* 运算符下拉 */}
                    <Select
                      value={c.operator}
                      onValueChange={(v) => updateCond(i, { operator: v })}
                    >
                      <SelectTrigger className="h-7 w-[130px] shrink-0 text-xs"><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {WHERE_OPERATORS.map((op) => (
                          <SelectItem key={op.value} value={op.value}>{tKey(op.label)}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>

                    {/* 值输入（IS NULL / IS NOT NULL 无值） */}
                    {!NO_VALUE_OPS.includes(c.operator) && (
                      <Input
                        className="h-7 flex-1 font-mono text-xs"
                        value={c.value}
                        onChange={(e) => updateCond(i, { value: e.target.value })}
                        placeholder={
                          c.operator === "IN" || c.operator === "NOT IN"
                            ? "1, 2, 3"
                            : c.operator === "LIKE" || c.operator === "NOT LIKE"
                              ? "%keyword%"
                              : tr("tablePicker.value")
                        }
                      />
                    )}

                    {/* 删除行 */}
                    <Button
                      variant="ghost"
                      size="sm"
                      className="h-7 w-7 shrink-0 p-0 text-muted-foreground hover:text-destructive"
                      onClick={() => removeCond(i)}
                    >
                      <X className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                ))}

                {/* 添加条件 */}
                <Button
                  variant="outline"
                  size="sm"
                  className="h-7 w-full border-dashed text-xs"
                  onClick={addCond}
                  disabled={colsLoading || columns.length === 0}
                >
                  <Plus className="mr-1 h-3 w-3" /> {tr("tablePicker.addCond")}
                </Button>

                {/* WHERE 预览 */}
                {visConds.length > 0 && (
                  <div className="rounded bg-muted/50 px-2 py-1.5 font-mono text-[11px] text-muted-foreground">
                    <span className="font-medium text-foreground">WHERE </span>
                    {serializeWhere(visConds)}
                  </div>
                )}
                {colsError && (
                  <div className="text-[11px] text-destructive">{tr("tablePicker.colsLoadFailed", { err: colsError })}</div>
                )}
              </div>
            )}

            {/* SQL 模式 */}
            {whereMode === "sql" && (
              <div className="space-y-1.5">
                <div className="relative">
                  <Textarea
                    ref={whereRef}
                    value={editQuery}
                    onChange={(e) => setEditQuery(e.target.value)}
                    placeholder={`SELECT * FROM ${stripDB(editing || "table")} WHERE id > 0`}
                    rows={12}
                    className="font-mono text-xs"
                  />
                  {/* 插入列下拉菜单 */}
                  {!colsLoading && columns.length > 0 && (
                    <>
                      <Button
                        variant="outline"
                        size="sm"
                        className="absolute bottom-1.5 right-1.5 h-6 gap-1 px-2 text-[10px]"
                        onClick={(e) => {
                          if (showColRef) {
                            setShowColRef(false)
                            setColRefPos(null)
                            return
                          }
                          // 以按钮位置为锚点，面板向上浮出（fixed 定位，不受弹窗滚动区裁剪）
                          const r = e.currentTarget.getBoundingClientRect()
                          setColRefPos({ top: r.top - 6, right: window.innerWidth - r.right })
                          setShowColRef(true)
                        }}
                      >
                        <Plus className="h-3 w-3" /> {tr("tablePicker.insertCol")}
                        {showColRef ? <ChevronUp className="h-3 w-3" /> : <ChevronRight className="h-3 w-3" />}
                      </Button>
                      {showColRef && colRefPos && (
                        <>
                          {/* 透明遮罩捕获外部点击 */}
                          <div className="fixed inset-0 z-40" onClick={() => { setShowColRef(false); setColRefPos(null) }} />
                          <div
                            style={{ top: colRefPos.top, right: colRefPos.right }}
                            className="fixed z-50 max-h-[220px] w-[280px] -translate-y-full overflow-y-auto rounded-md border bg-popover p-1 shadow-lg"
                          >
                            {columns.map((col) => (
                              <button
                                key={col.name}
                                type="button"
                                onClick={() => { insertColumn(col.name); setShowColRef(false); setColRefPos(null) }}
                                className="flex w-full items-center gap-1.5 rounded px-2 py-1 text-left text-xs hover:bg-accent"
                              >
                                {col.primaryKey && <TableIcon icon={KeyRound} className="text-amber-500" />}
                                {!col.primaryKey && col.unique && <TableIcon icon={ShieldCheck} className="text-blue-500" />}
                                {!col.primaryKey && !col.unique && col.indexed && <TableIcon icon={ListOrdered} className="text-muted-foreground/70" />}
                                <span className="font-mono">{col.name}</span>
                                <span className="ml-auto shrink-0 text-[10px] text-muted-foreground">{col.dataType}</span>
                              </button>
                            ))}
                          </div>
                        </>
                      )}
                    </>
                  )}
                </div>
                {colsError && (
                  <div className="text-[11px] text-destructive">{tr("tablePicker.colsLoadFailed", { err: colsError })}</div>
                )}
              </div>
            )}
            </div>
          </div>
          </div>

          <DialogFooter>
            <Button variant="ghost" onClick={closeEdit}>{tr("common.cancel")}</Button>
            <Button onClick={saveEdit}>{tr("common.confirm")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
