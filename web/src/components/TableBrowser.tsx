import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from "react"
import { AlertCircle, AlertTriangle, ArrowDown, ArrowUp, BarChart3, Check, ChevronLeft, ChevronRight, ChevronsRight, ChevronsUpDown, Columns3, Copy, Download, EyeOff, FileSpreadsheet, Filter, FunctionSquare, KeyRound, ListOrdered, Loader2, Maximize2, Minimize2, MoreHorizontal, Pencil, Pin, PinOff, Plus, RefreshCw, RotateCcw, ShieldCheck, Table2, Trash2, View, X } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { confirm } from "@/components/ui/alert-dialog"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import TableIcon from "@/components/ui/table-icon"
import CellEditor from "@/components/CellEditor"
import * as api from "@/api"
import { deleteTableRows, exportTableExcel, fetchTableData, fetchTableCellValue, getObjectDDL, insertTableRow, updateTableCell } from "@/api/sql"
import { cn } from "@/lib/utils"
import { computeColWidths, computeColumnStat, copyCellValue, copyToClipboard, downloadText, FILTER_OP_LABEL, FILTER_OPS, fmtNum, isNullCell, renderCellText, rowsToCSV, rowToTSV, rowsToTSV } from "@/lib/table"
import { useClickOutside } from "@/lib/useClickOutside"
import type { ColumnFilter, FilterOp, ObjectDDLType, SortSpec, TableColumn, TableViewLayout } from "@/types"

interface Props {
  connId: string
  db: string
  name: string
  objType: ObjectDDLType
  subTab: "data" | "struct" | "ddl"
  page: number
  viewLayout?: TableViewLayout
  onSubTabChange: (tab: "data" | "struct" | "ddl") => void
  onPageChange: (page: number) => void
  onViewLayoutChange?: (layout: TableViewLayout) => void
  /** 由 WorkspaceLayout 透传工作区级别状态，在分页条左侧常驻显示 */
  running?: boolean
  persistFailed?: boolean
  onClearPersistFailed?: () => void
}

// 对象类型 → 图标/文案
const TYPE_META: Record<ObjectDDLType, { icon: typeof Table2; label: string; cls: string }> = {
  table: { icon: Table2, label: "表", cls: "text-emerald-600" },
  view: { icon: View, label: "视图", cls: "text-cyan-600" },
  function: { icon: FunctionSquare, label: "函数", cls: "text-violet-600" },
  procedure: { icon: FunctionSquare, label: "存储过程", cls: "text-orange-600" },
}

// 大字段类型（列表查询时省略，点击单元格按需加载）：mediumtext 及以上 + 二进制 + 大对象。
// 注意：tinytext/text 属于常规文本，正常查询展示，不纳入懒加载省略。
const BIG_FIELD_TYPES = [
  "mediumtext", "longtext", "mediumblob", "longblob", "blob", "tinyblob",
  "binary", "varbinary", "bytea", "clob", "nclob", "json", "jsonb",
]

// isBigField 判断列数据类型是否为需要懒加载的大字段（大小写不敏感）
function isBigField(dataType: string): boolean {
  const t = dataType.toLowerCase()
  return BIG_FIELD_TYPES.some((b) => t.includes(b))
}

// 对象浏览器：数据（表/视图）+ 结构（表/视图）+ DDL（全部对象类型）
export default function TableBrowser({ connId, db, name, objType, subTab, page, viewLayout, onSubTabChange, onPageChange, onViewLayoutChange, running, persistFailed, onClearPersistFailed }: Props) {
  const [columns, setColumns] = useState<string[]>([])
  const [rows, setRows] = useState<unknown[][]>([])
  // 页大小：从持久化布局恢复（默认 100）
  const [pageSize, setPageSize] = useState(viewLayout?.pageSize ?? 100)
  const [total, setTotal] = useState(-1)
  const [jumpInput, setJumpInput] = useState("")
  const [loadingData, setLoadingData] = useState(false)
  const [loadedOnce, setLoadedOnce] = useState(false) // 是否曾成功加载过数据（区分首次加载与刷新）
  const [struct, setStruct] = useState<TableColumn[]>([])
  const [loadingStruct, setLoadingStruct] = useState(false)
  const [ddl, setDdl] = useState("")
  const [loadingDDL, setLoadingDDL] = useState(false)
  const [error, setError] = useState("") // 当前视图加载错误，内联显示而非 toast
  const [structError, setStructError] = useState("") // 列结构加载失败提示（编辑/大字段省略不可用，可重试）
  const structTableRef = useRef("") // 记录 struct 已加载自哪张表（connId|db|name），切换表时重新加载，避免主键串表
  // 记录「本组件实例绑定的表」：初始化为当前 tableKey，用于区分「首次挂载」与「真正切表」。
  // 首次挂载（含切 tab 回来）时不应清空视图状态（列宽/排序/过滤等从 viewLayout 恢复），
  // 只有 tableKey 真正变化（跨表）时才清空。注意：跨表在 WorkspaceLayout 里已由 key 强制 remount，
  // 此处是兜底，防止无 key 的路径下旧表视图状态串表。
  const viewStateTableRef = useRef(`${connId}|${db}|${name}`)

  // 单元格弹层状态（仅读查看 / 编辑共用：readonly 区分）
  const [editing, setEditing] = useState<{ rowIndex: number; colIndex: number; readonly?: boolean } | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState("")
  // 就地编辑状态：双击简单单元格时在单元格内联输入（非弹窗），回车/失焦保存、Esc 取消
  const [inlineEdit, setInlineEdit] = useState<{ rowIndex: number; colIndex: number } | null>(null)
  const [inlineValue, setInlineValue] = useState("")
  const [inlineSaving, setInlineSaving] = useState(false)

  // 新增行表单：inserting=true 打开；insertValues 记录每列的输入值（key=列名）
  const [inserting, setInserting] = useState(false)
  const [insertValues, setInsertValues] = useState<Record<string, string>>({})
  const [insertingSaving, setInsertingSaving] = useState(false)
  const [maximized, setMaximized] = useState(false) // 弹层最大化

  // 行选取 + 批量删除
  // 选中以「主键 key」标识（JSON 序列化主键值），跨页/排序后仍保留；切换表时清空（选中跟具体表相关）。
  const [selectedRows, setSelectedRows] = useState<Set<string>>(new Set())
  // 右键菜单：状态放在子组件内部（rowMenuRef 驱动），右键时不再触发整表重渲染
  const rowMenuRef = useRef<RowMenuHandle>(null)
  // 右键聚焦的行 DOM：纯 DOM class 高亮（避免 setState 触发整表重渲染）
  const focusedRowElRef = useRef<HTMLTableRowElement | null>(null)
  const focusRow = (el: HTMLTableRowElement) => {
    if (focusedRowElRef.current) focusedRowElRef.current.classList.remove("row-context-focused")
    focusedRowElRef.current = el
    el.classList.add("row-context-focused")
  }
  const clearFocusedRow = () => {
    if (focusedRowElRef.current) {
      focusedRowElRef.current.classList.remove("row-context-focused")
      focusedRowElRef.current = null
    }
  }
  const [deleting, setDeleting] = useState(false)

  // 排序状态（全局数据库排序）：三态循环 asc → desc → 无
  // 多列排序状态：Shift+点击叠加，普通点击单列（三态循环 asc → desc → 移除）
  const [sortSpecs, setSortSpecs] = useState<SortSpec[]>(viewLayout?.sortSpecs ?? [])

  // 列过滤状态（AND 叠加）：filters 是已应用的条件列表，filterDraft 是当前正在编辑的过滤面板状态
  const [filters, setFilters] = useState<ColumnFilter[]>(viewLayout?.filters ?? [])
  const [filterCol, setFilterCol] = useState<string | null>(null) // 当前打开过滤面板的列名
  const [filterDraft, setFilterDraft] = useState<{ op: FilterOp; value: string }>({ op: "contains", value: "" })

  // 列显隐状态：hiddenColumns 记录被隐藏的列名（Set 用于 O(1) 查找）
  const [hiddenColumns, setHiddenColumns] = useState<Set<string>>(new Set(viewLayout?.hiddenColumns ?? []))
  // 冻结列状态：frozenUntil = 冻结边界列名（该列及左侧可见列均冻结）；null = 未自定义（走默认）。
  // frozenTouched 标记用户是否明确固定过某列：仅真实列名视为自定义，null/""/undefined 均视为未设置（默认冻结主键）
  const [frozenUntil, setFrozenUntil] = useState<string | null>(viewLayout?.frozenUntil ? viewLayout.frozenUntil : null)
  const frozenTouched = useRef(!!viewLayout?.frozenUntil)
  // 列管理面板开关
  const [showColumnPanel, setShowColumnPanel] = useState(false)
  const columnPanelRef = useRef<HTMLElement>(null)
  // 列管理面板：点击外部关闭（ref 绑在「按钮 + 面板」共同容器上，保证按钮能正常 toggle 收放）
  useClickOutside(columnPanelRef, () => setShowColumnPanel(false), showColumnPanel)

  // 过滤面板：点击外部关闭（ref 绑定到当前打开的面板容器）
  const filterPanelRef = useRef<HTMLDivElement>(null)
  useClickOutside(filterPanelRef, () => setFilterCol(null), filterCol !== null)

  // 「更多」菜单：收纳低频操作（导出/重置视图等），避免工具栏按钮堆砌占空间
  const [moreMenuOpen, setMoreMenuOpen] = useState(false)
  const moreMenuRef = useRef<HTMLDivElement>(null)
  useClickOutside(moreMenuRef, () => setMoreMenuOpen(false), moreMenuOpen)

  // 「点击单元格时若有浮层打开，本次点击只负责关闭浮层、不聚焦单元格」的判定标记。
  // 时序：单元格 onMouseDown（冒泡先到）→ 记录此刻浮层是否打开 → document 级 useClickOutside 关闭浮层
  //      → 单元格 onClick → 若标记为 true 则跳过聚焦（符合「菜单打开时点外部只关菜单」惯例）。
  const dismissingOverlayRef = useRef(false)

  // 大字段懒加载缓存：key = `${rowIndex}:${colIndex}` → 真实值（null 表示已加载且为 NULL）
  const [bigValueMap, setBigValueMap] = useState<Map<string, unknown>>(new Map())
  const [bigLoading, setBigLoading] = useState(false)
  const [bigError, setBigError] = useState("")

  // 列结构（主键+类型）——数据 tab 也需要，用于 UPDATE 定位主键与判断编辑类型
  const colMetaMap = useMemo(() => {
    const m = new Map<string, TableColumn>()
    for (const c of struct) m.set(c.name, c)
    return m
  }, [struct])

  // 主键列名列表（按列顺序）
  const pkColumns = useMemo(() => struct.filter((c) => c.primaryKey).map((c) => c.name), [struct])

  // 大字段列名集合（列表查询时省略，点击单元格按需加载完整值）。
  // 仅在有主键时生效：无主键无法定位取值，大字段退化为正常查询展示。
  const bigFieldCols = useMemo(() => {
    const s = new Set<string>()
    if (pkColumns.length === 0) return s
    for (const c of struct) if (isBigField(c.dataType)) s.add(c.name)
    return s
  }, [struct, pkColumns.length])

  // 按内容自适应列宽（采样前 100 行估算）。
  // 仅依赖 columns：翻页/排序会改变 rows 顺序，若据此重算会让列宽跳动；
  // 列宽只随列名变化（切换表/视图）重算，行数据变化时保持稳定。
  // 大字段列省略了真实数据，列宽强制固定一个较窄的值（占位只有图标），避免占位列「按真实长字符串估算」后被撑宽。
  const BIG_FIELD_COL_W = 96
  // 用户手动拖拽调整的列宽（列名 → px），覆盖自适应估算；从 viewLayout 恢复，切表时重置
  const [colWidthOverrides, setColWidthOverrides] = useState<Record<string, number>>(viewLayout?.colWidths ?? {})
  // 列宽拖拽进行中的状态：记录正在拖拽的列名 + 拖拽起始位置/宽度
  const resizingRef = useRef<{ colName: string; startX: number; startWidth: number } | null>(null)
  const [resizingCol, setResizingCol] = useState<string | null>(null)
  // 「刚结束拖拽」标记：拖拽手柄 mousedown 阻止不了后续 click 冒泡到 <th> 触发排序，
  // 用此 ref 在 mouseup 后短暂标记，让紧随的 click 被忽略（消费一次）。
  const justResizedRef = useRef(false)

  // 列宽拖拽：在表头右边界按下鼠标开始，移动时实时调整，松开结束。
  // 用全局 mousemove/mouseup 监听（拖出表头也能持续），结束后写回 colWidthOverrides 持久化。
  const startResize = useCallback((e: React.MouseEvent, colName: string, currentWidth: number) => {
    e.preventDefault()
    e.stopPropagation()
    resizingRef.current = { colName, startX: e.clientX, startWidth: currentWidth }
    setResizingCol(colName)
  }, [])
  useEffect(() => {
    if (!resizingCol) return
    const onMove = (e: MouseEvent) => {
      const r = resizingRef.current
      if (!r) return
      const delta = e.clientX - r.startX
      const newWidth = Math.max(48, Math.round(r.startWidth + delta)) // 最小列宽 48px，避免拖成 0
      setColWidthOverrides((prev) => ({ ...prev, [r.colName]: newWidth }))
    }
    const onUp = () => {
      resizingRef.current = null
      setResizingCol(null)
      // 标记「刚结束拖拽」，让紧随的 click 被忽略（消费一次后复位）
      justResizedRef.current = true
      setTimeout(() => {
        justResizedRef.current = false
      }, 0)
    }
    document.addEventListener("mousemove", onMove)
    document.addEventListener("mouseup", onUp)
    return () => {
      document.removeEventListener("mousemove", onMove)
      document.removeEventListener("mouseup", onUp)
    }
  }, [resizingCol])
  const colWidths = useMemo(() => {
    const widths = computeColWidths(columns, rows)
    return columns.map((c, i) => {
      if (colWidthOverrides[c] !== undefined) return colWidthOverrides[c]
      return bigFieldCols.has(c) ? BIG_FIELD_COL_W : widths[i] ?? 96
    })
  }, [columns, bigFieldCols, colWidthOverrides])

  // 表格最小宽度：列宽总和 + 首列 checkbox（40px），列多时表格超出容器产生横向滚动，
  // 使「冻结首列」生效（sticky left-0）。列少时仍填满容器（min-w-full）。
  const tableMinWidth = useMemo(() => {
    const sum = colWidths.reduce((a, b) => a + b, 0)
    return objType === "table" ? sum + 40 : sum
  }, [colWidths, objType])

  const meta = TYPE_META[objType]
  const Icon = meta.icon
  // 表/视图可看数据与结构；函数/存储过程仅 DDL
  const hasData = objType === "table" || objType === "view"

  // 易变状态的最新值快照：loadData 内部读取 ref，避免把这些会在请求过程中被 setXxx 修改的
  // 状态放进 useCallback 依赖数组，导致「请求 → setState → 引用变化 → effect 重跑 → 再请求」的循环，
  // 每次点击对象重复发出多个相同的 /api/sql/table 请求。
  const sortSpecsRef = useRef(sortSpecs)
  sortSpecsRef.current = sortSpecs
  const filtersRef = useRef(filters)
  filtersRef.current = filters
  const structRef = useRef(struct)
  structRef.current = struct

  // 数据请求进行中标记：StrictMode 开发模式下 effect 会双触发（mount→unmount→mount），
  // 若上一次请求尚未完成就再次触发，会导致同一对象重复发出多个相同的 /api/sql/table 请求。
  // 用 ref 做防重入，确保同一时刻只存在一个在途请求。
  const loadDataInFlightRef = useRef(false)

  const loadData = useCallback(async () => {
    if (!connId || !name) return
    if (loadDataInFlightRef.current) return
    loadDataInFlightRef.current = true
    setLoadingData(true)
    setError("")
    // 表切换时立即清空 struct，避免竞态期（新表数据已显示、struct 仍为旧表）导致主键串表。
    // 用 viewStateTableRef 区分「首次挂载」与「真正切表」：首次挂载（含切 tab 回来）不进入此分支，
    // 保留从 viewLayout 恢复的列宽/排序/过滤等视图状态；仅 tableKey 真正变化时才清空。
    const tableKey = `${connId}|${db}|${name}`
    if (viewStateTableRef.current !== tableKey) {
      viewStateTableRef.current = tableKey
      setStruct([])
      setBigValueMap(new Map()) // 切表时清空大字段懒加载缓存（cell key 不再有效）
      setBigError("")
      setSelectedRows(new Set()) // 切表时清空行选中（选中跟具体表相关，旧表主键 key 失效）
      clearFocusedRow()
      setFilters([]) // 切表时清空过滤条件（过滤跟具体表相关，旧表列名失效）
      setFilterCol(null)
      // 排序/列显隐同样是「表级」状态：跨表复用会带入旧表的列名导致后端报错（如 JOB_STATE 不在 t_config）
      // （正常路径下由 WorkspaceLayout 的 key 强制 remount，这里是兜底）
      setSortSpecs([])
      setHiddenColumns(new Set())
      setFrozenUntil(null)
      frozenTouched.current = false
      setColWidthOverrides({}) // 切表时清空手动列宽（列宽跟具体表相关）
      setInlineEdit(null) // 切表时关闭就地编辑
    }
    try {
      // 表（非视图）先确保拿到列结构（主键 + 列类型），据此识别大字段列并在列表查询时省略，
      // 避免 BLOB/超长文本等大字段随列表一次性传输。struct 与表绑定，切换表时才重新拉取。
      let cols = structRef.current
      if (objType === "table" && structTableRef.current !== tableKey) {
        structTableRef.current = tableKey
        try {
          const sc = await api.getTableColumns(connId, db, name)
          cols = sc.columns
          setStruct(sc.columns)
          setStructError("")
        } catch (e) {
          // 列结构加载失败不阻塞数据展示，仅禁用编辑/大字段省略；顶部提示并支持重试
          setStructError(e instanceof Error ? e.message : String(e))
        }
      }
      // 大字段省略的前提：存在主键（否则无法按主键定位取值，退化为直接查询完整值）。
      const hasPK = cols.some((c) => c.primaryKey)
      const excludeColumns = hasPK ? cols.filter((c) => isBigField(c.dataType)).map((c) => c.name) : []
      // 列级状态清洗：sortSpecs/filters/hiddenColumns 里若有当前表不存在的列名（典型场景：
      // 之前因 TableBrowser 缺 key 导致旧表 sortSpecs 被错误持久化到本表 viewLayout），
      // 静默剔除，避免后端 sqlquery.go:407 报「排序列「X」不存在于表 Y」。
      let cleanSortSpecs = sortSpecsRef.current
      let cleanFilters = filtersRef.current
      let cleanHidden: Set<string> = hiddenColumns
      if (cols.length > 0) {
        const validColSet = new Set(cols.map((c) => c.name.toLowerCase()))
        const sanitizeColumn = (col: string) => (validColSet.has(col.toLowerCase()) ? col : "")
        cleanSortSpecs = sortSpecsRef.current.filter((s) => sanitizeColumn(s.column) === s.column)
        cleanFilters = filtersRef.current.filter((f) => sanitizeColumn(f.column) === f.column)
        cleanHidden = new Set(Array.from(hiddenColumns).filter((c) => sanitizeColumn(c) === c))
        if (cleanSortSpecs.length !== sortSpecsRef.current.length) setSortSpecs(cleanSortSpecs)
        if (cleanFilters.length !== filtersRef.current.length) setFilters(cleanFilters)
        if (cleanHidden.size !== hiddenColumns.size) setHiddenColumns(cleanHidden)
      }
      const res = await fetchTableData({
        connId,
        db,
        table: name,
        page,
        pageSize,
        sortSpecs: cleanSortSpecs.length > 0 ? cleanSortSpecs : undefined,
        excludeColumns,
        filters: cleanFilters.length > 0 ? cleanFilters : undefined,
      })
      setColumns(res.columns)
      setRows(res.rows)
      setTotal(res.total) // 分页接口一次返回全表总行数，无需再发 COUNT(*)
      setLoadedOnce(true)
    } catch (e) {
      setError((e as Error).message)
    } finally {
      loadDataInFlightRef.current = false
      setLoadingData(false)
    }
  }, [connId, db, name, page, pageSize, objType])

  // 列结构加载失败后的重试：直接重拉列结构，不清空用户已设置的过滤/排序，成功后刷新数据
  const retryStruct = useCallback(async () => {
    if (!connId || !name) return
    setStructError("")
    try {
      const sc = await api.getTableColumns(connId, db, name)
      setStruct(sc.columns)
      structTableRef.current = `${connId}|${db}|${name}`
      void loadData()
    } catch (e) {
      setStructError(e instanceof Error ? e.message : String(e))
    }
  }, [connId, db, name, loadData])

  const loadStruct = useCallback(async () => {
    if (!connId || !name) return
    setLoadingStruct(true)
    setError("")
    try {
      const res = await api.getTableColumns(connId, db, name)
      setStruct(res.columns)
      structTableRef.current = `${connId}|${db}|${name}`
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoadingStruct(false)
    }
  }, [connId, db, name])

  const loadDDL = useCallback(async () => {
    if (!connId || !name) return
    setLoadingDDL(true)
    setError("")
    try {
      const res = await getObjectDDL(connId, db, objType, name)
      setDdl(res.ddl)
    } catch (e) {
      setError((e as Error).message)
      setDdl("")
    } finally {
      setLoadingDDL(false)
    }
  }, [connId, db, objType, name])

  // 保存单元格编辑：named bind UPDATE（按主键定位）
  const handleSaveCell = useCallback(
    async (newValue: unknown) => {
      if (!editing) return
      const colName = columns[editing.colIndex]
      const row = rows[editing.rowIndex]
      if (colName === undefined || !row) return
      // 无主键无法安全 UPDATE
      if (pkColumns.length === 0) {
        setSaveError("该表无主键，无法定位行进行更新")
        return
      }
      // 构造主键值（按列顺序取该行的主键列值）。
      // 注意：元数据列名与 SELECT 返回列名可能存在大小写差异（如 ID_ vs id_），
      // 故用大小写不敏感匹配定位主键列索引。
      const pkValues: unknown[] = []
      for (const pk of pkColumns) {
        const idx = columns.findIndex((c) => c.toLowerCase() === pk.toLowerCase())
        if (idx < 0) {
          setSaveError(`无法定位主键列「${pk}」的值，已取消更新`)
          return
        }
        pkValues.push(row[idx])
      }
      setSaving(true)
      setSaveError("")
      try {
        await updateTableCell({
          connId,
          db,
          table: name,
          column: colName,
          value: newValue,
          pkColumns,
          pkValues,
        })
        setEditing(null)
        await loadData() // 刷新数据
      } catch (e) {
        setSaveError((e as Error).message)
      } finally {
        setSaving(false)
      }
    },
    [editing, columns, rows, pkColumns, connId, db, name, loadData],
  )

  // 就地编辑保存：与弹窗保存共用主键定位 + updateTableCell，但值类型转换更轻量。
  // 就地编辑只用于简单文本/数字列；大字段/JSON 等复杂值走弹窗（CellEditor 提供类型化编辑）。
  // 用 ref 防重入：Enter 保存后 input 立即失焦可能再次触发 onBlur 保存，避免重复请求。
  const inlineSavingRef = useRef(false)
  const saveInlineEdit = useCallback(async () => {
    if (!inlineEdit || inlineSavingRef.current) return
    const colName = columns[inlineEdit.colIndex]
    const row = rows[inlineEdit.rowIndex]
    if (colName === undefined || !row) return
    if (pkColumns.length === 0) {
      setSaveError("该表无主键，无法定位行进行更新")
      setInlineEdit(null)
      return
    }
    // 内联构造主键值（避免依赖后置声明的 buildPKValues）
    const pkValues: unknown[] = []
    for (const pk of pkColumns) {
      const idx = columns.findIndex((c) => c.toLowerCase() === pk.toLowerCase())
      if (idx < 0) {
        setSaveError(`无法定位主键列「${pk}」的值，已取消更新`)
        setInlineEdit(null)
        return
      }
      pkValues.push(row[idx])
    }
    // 值类型转换：按列数据类型 + 原值类型推断目标类型，避免把数字/布尔存成字符串
    const colMeta = colMetaMap.get(colName)
    const raw = inlineValue
    const oldVal = row[inlineEdit.colIndex]
    let newValue: unknown = raw
    if (raw === "" && colMeta?.nullable !== false) {
      newValue = null // 空串 + 可空 → NULL
    } else if (typeof oldVal === "number") {
      const n = Number(raw)
      if (raw !== "" && Number.isFinite(n)) newValue = n
    } else if (typeof oldVal === "boolean") {
      newValue = raw === "true" || raw === "1"
    }
    inlineSavingRef.current = true
    setInlineSaving(true)
    setSaveError("")
    try {
      await updateTableCell({
        connId,
        db,
        table: name,
        column: colName,
        value: newValue,
        pkColumns,
        pkValues,
      })
      setInlineEdit(null)
      await loadData() // 刷新数据
    } catch (e) {
      setSaveError((e as Error).message)
    } finally {
      inlineSavingRef.current = false
      setInlineSaving(false)
    }
  }, [inlineEdit, inlineValue, columns, rows, pkColumns, connId, db, name, colMetaMap, loadData])

  // 就地编辑取消（Esc / 失焦）
  const cancelInlineEdit = useCallback(() => {
    setInlineEdit(null)
    setInlineValue("")
  }, [])

  // 点击大字段单元格：复用编辑弹层（readonly 模式），按需按主键 + 列名取值。
  // 真实值缓存到 bigValueMap，避免重复请求；弹层用缓存值渲染（未取到时显示加载占位）。
  const handleViewBigCell = useCallback(
    async (rowIndex: number, colIndex: number) => {
      const colName = columns[colIndex]
      const row = rows[rowIndex]
      if (colName === undefined || !row) return
      const key = `${rowIndex}:${colIndex}`
      setEditing({ rowIndex, colIndex, readonly: true })
      // 已有缓存：不再请求
      if (bigValueMap.has(key)) return
      if (pkColumns.length === 0) {
        setBigError("该表无主键，无法定位行获取完整值")
        return
      }
      // 构造主键值（大小写不敏感匹配列索引）
      const pkValues: unknown[] = []
      for (const pk of pkColumns) {
        const idx = columns.findIndex((c) => c.toLowerCase() === pk.toLowerCase())
        if (idx < 0) {
          setBigError(`无法定位主键列「${pk}」的值`)
          return
        }
        pkValues.push(row[idx])
      }
      setBigLoading(true)
      setBigError("")
      try {
        const res = await fetchTableCellValue({ connId, db, table: name, column: colName, pkColumns, pkValues })
        setBigValueMap((m) => {
          const next = new Map(m)
          next.set(key, res.value)
          return next
        })
      } catch (e) {
        setBigError((e as Error).message)
      } finally {
        setBigLoading(false)
      }
    },
    [columns, rows, pkColumns, connId, db, name, bigValueMap],
  )

  // 构造某行的主键值数组（按 pkColumns 顺序，大小写不敏感匹配列索引）
  const buildPKValues = useCallback(
    (rowIndex: number): unknown[] | null => {
      const row = rows[rowIndex]
      if (!row) return null
      const vals: unknown[] = []
      for (const pk of pkColumns) {
        const idx = columns.findIndex((c) => c.toLowerCase() === pk.toLowerCase())
        if (idx < 0) return null
        vals.push(row[idx])
      }
      return vals
    },
    [rows, pkColumns, columns],
  )

  // 当前行的选中 key（主键值数组 JSON 序列化）；无主键/取不到返回 null
  const rowKey = useCallback(
    (rowIndex: number): string | null => {
      const vals = buildPKValues(rowIndex)
      return vals === null ? null : JSON.stringify(vals)
    },
    [buildPKValues],
  )

  // 删除选中行（批量）：入参为主键 key 列表，反序列化还原主键值，跨页也可删
  const handleDeleteRows = useCallback(
    async (keys: string[]) => {
      if (keys.length === 0) return
      if (pkColumns.length === 0) {
        setSaveError("该表无主键，无法定位行进行删除")
        return
      }
      // key → 主键值数组（JSON 反序列化）
      const payloadRows: unknown[][] = []
      for (const k of keys) {
        try {
          payloadRows.push(JSON.parse(k) as unknown[])
        } catch {
          setSaveError("选中数据异常，已取消删除")
          return
        }
      }
      // 构造确认文案：带上库表名 + 每行主键定位，避免用户误删
      const pkDetail = payloadRows
        .map((vals) => {
          const parts = pkColumns.map((pk, j) => `${pk} = ${vals[j] === null ? "NULL" : String(vals[j])}`).join("、")
          return `主键 ${parts}`
        })
        .slice(0, 5) // 最多展示前 5 行，避免过多撑爆弹窗
        .join("\n")
      const more = keys.length > 5 ? `\n… 等共 ${keys.length} 行` : ""
      const ok = await confirm({
        title: "删除行",
        description: `目标表：${db ? `${db}.` : ""}${name}\n共 ${keys.length} 行\n\n${pkDetail}${more}\n\n此操作不可恢复，确认删除？`,
        confirmText: "删除",
        danger: true,
      })
      if (!ok) return
      setDeleting(true)
      setSaveError("")
      try {
        await deleteTableRows({ connId, db, table: name, pkColumns, rows: payloadRows })
        // 删除成功后：仅移除已删除的 key（跨页可能还有其它页选中保留）
        setSelectedRows((prev) => {
          const next = new Set(prev)
          for (const k of keys) next.delete(k)
          return next
        })
        await loadData() // 刷新数据
      } catch (e) {
        setSaveError((e as Error).message)
      } finally {
        setDeleting(false)
      }
    },
    [pkColumns, connId, db, name, loadData],
  )

  // 新增行提交：组装用户填写的列与值，调用 INSERT；自增列跳过（由数据库生成）。
  const handleInsertRow = async () => {
    // 过滤掉空输入（未填的列视为不写入，交给数据库默认值/NULL）
    const cols: string[] = []
    const vals: unknown[] = []
    for (const col of columns) {
      const meta = colMetaMap.get(col)
      if (meta?.autoIncrement) continue // 自增列跳过
      const raw = insertValues[col]
      if (raw === undefined || raw === "") continue // 未填写：跳过（用默认值/NULL）
      cols.push(col)
      vals.push(raw)
    }
    if (cols.length === 0) {
      setSaveError("请至少填写一个字段")
      return
    }
    setInsertingSaving(true)
    setSaveError("")
    try {
      await insertTableRow({ connId, db, table: name, columns: cols, values: vals })
      setInserting(false)
      setInsertValues({})
      await loadData() // 刷新数据
    } catch (e) {
      setSaveError((e as Error).message)
    } finally {
      setInsertingSaving(false)
    }
  }

  // 切换单行选中（按主键 key）
  const toggleRow = (rowIndex: number) => {
    const k = rowKey(rowIndex)
    if (k === null) return // 无主键/取不到，无法选中
    setSelectedRows((prev) => {
      const next = new Set(prev)
      if (next.has(k)) next.delete(k)
      else next.add(k)
      return next
    })
  }

  // 当前页所有行的 key（过滤无效 key）
  const pageRowKeys = useMemo(
    () => rows.map((_, i) => rowKey(i)).filter((k): k is string => k !== null),
    [rows, rowKey],
  )

  // 全选/取消全选（当前页）：当前页所有行是否全部选中
  const allSelected = pageRowKeys.length > 0 && pageRowKeys.every((k) => selectedRows.has(k))
  const toggleSelectAll = () => {
    setSelectedRows((prev) => {
      const next = new Set(prev)
      if (allSelected) {
        for (const k of pageRowKeys) next.delete(k)
      } else {
        for (const k of pageRowKeys) next.add(k)
      }
      return next
    })
  }

  // 表头排序：三态循环（无 → 升序 → 降序 → 无）；排序后重置到第 1 页。
  // 大字段列（二进制/超长文本）不支持排序，直接忽略点击。
  const handleSort = (col: string, shiftKey = false) => {
    if (bigFieldCols.has(col)) return
    setSortSpecs((prev) => {
      const existing = prev.find((s) => s.column === col)
      // Shift+点击：叠加/切换该列排序（不影响其它列），若已存在则切换方向
      if (shiftKey) {
        if (!existing) return [...prev, { column: col, order: "asc" }]
        if (existing.order === "asc") {
          return prev.map((s) => (s.column === col ? { column: col, order: "desc" as const } : s))
        }
        return prev.filter((s) => s.column !== col)
      }
      // 普通点击：单列三态循环（asc → desc → 移除），替换其它列
      if (!existing) return [{ column: col, order: "asc" }]
      if (existing.order === "asc") return [{ column: col, order: "desc" }]
      return []
    })
    if (page !== 1) onPageChange(1)
  }

  // 打开某列的过滤面板：初始化草稿为该列当前已应用的条件（无则默认「包含」）
  const openFilterPanel = (col: string) => {
    const existing = filters.find((f) => f.column === col)
    if (existing) {
      setFilterDraft({ op: existing.op, value: existing.value === null || existing.value === undefined ? "" : String(existing.value) })
    } else {
      setFilterDraft({ op: "contains", value: "" })
    }
    setFilterCol(col)
  }

  // 应用某列的过滤条件（替换该列已有条件）；值按需校验（isNull/isNotNull 无需值）
  const applyFilter = (col: string) => {
    const meta = FILTER_OPS.find((f) => f.op === filterDraft.op)
    if (!meta) return
    const value = filterDraft.value
    if (meta.needValue && value.trim() === "") {
      // 需要值但未填：清除该列的过滤条件
      clearColumnFilter(col)
      return
    }
    const newFilter: ColumnFilter = {
      column: col,
      op: filterDraft.op,
      value: meta.needValue ? value : undefined,
    }
    setFilters((prev) => {
      const next = prev.filter((f) => f.column !== col)
      next.push(newFilter)
      return next
    })
    setFilterCol(null)
    if (page !== 1) onPageChange(1)
  }

  // 清除单列过滤条件
  const clearColumnFilter = (col: string) => {
    setFilters((prev) => prev.filter((f) => f.column !== col))
    setFilterCol(null)
  }

  // 清除全部过滤条件
  const clearAllFilters = () => {
    setFilters([])
    setFilterCol(null)
  }

  // 一键重置视图状态：清空排序/过滤/列显隐/手动列宽/冻结，恢复到表的默认视图。
  // 解决用户「加了排序过滤隐藏列之后，不知道数据怎么变少了、想回原始视图要逐列手动清」的痛点。
  const resetView = () => {
    setSortSpecs([])
    setFilters([])
    setFilterCol(null)
    setHiddenColumns(new Set())
    setColWidthOverrides({})
    setFrozenUntil(null)
    frozenTouched.current = false
  }
  // 是否存在可重置的视图状态（用于按钮高亮/禁用提示）
  const hasViewState = sortSpecs.length > 0 || filters.length > 0 || hiddenColumns.size > 0 || Object.keys(colWidthOverrides).length > 0 || frozenTouched.current

  // 右键单元格快速过滤：按当前单元格值直接设置该列条件（等于/不等于/包含）
  const quickFilter = (col: string, cell: unknown, op: FilterOp) => {
    const value = cell === null || cell === undefined ? "" : String(cell)
    const newFilter: ColumnFilter = { column: col, op, value }
    setFilters((prev) => {
      const next = prev.filter((f) => f.column !== col)
      next.push(newFilter)
      return next
    })
    if (page !== 1) onPageChange(1)
  }

  // 当前某列是否已应用过滤（表头漏斗高亮）
  const hasFilter = (col: string) => filters.some((f) => f.column === col)

  // 布局持久化：视图状态（过滤/排序/列显隐/页大小）变化时，统一打包回调给 store 持久化。
  // 单一写入点：所有变更都汇聚到这一处，避免散落各处漏存。
  useEffect(() => {
    onViewLayoutChange?.({
      ...(filters.length > 0 ? { filters } : {}),
      ...(sortSpecs.length > 0 ? { sortSpecs } : {}),
      ...(hiddenColumns.size > 0 ? { hiddenColumns: Array.from(hiddenColumns) } : {}),
      // 仅当用户明确固定过才持久化列名；否则显式写 null，清除旧的取消/默认状态（后端整体替换）
      ...(frozenTouched.current ? { frozenUntil: frozenUntil ?? "" } : { frozenUntil: null }),
      ...(Object.keys(colWidthOverrides).length > 0 ? { colWidths: colWidthOverrides } : {}),
      pageSize,
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filters, sortSpecs, hiddenColumns, pageSize, frozenUntil, colWidthOverrides])

  // 列显隐：切换某列的隐藏状态
  const toggleColumn = (col: string) => {
    setHiddenColumns((prev) => {
      const next = new Set(prev)
      if (next.has(col)) next.delete(col)
      else next.add(col)
      return next
    })
  }

  // 可见列：{ 列名, 原始列索引 }（用于渲染表头与单元格，隐藏列不渲染）
  const visibleCols = useMemo(
    () => columns.map((name, idx) => ({ name, idx })).filter((c) => !hiddenColumns.has(c.name)),
    [columns, hiddenColumns],
  )

  // 冻结边界：用户明确设置过则用设置值；否则默认冻结到「从左往右最后一个主键列」
  // （含左侧所有列，让全部主键列保持可见）；无主键则仅 checkbox 列
  const effectiveFrozenUntil = useMemo(() => {
    if (frozenTouched.current) return frozenUntil
    // 主键元数据列名与 SELECT 返回列名可能存在大小写差异，统一小写比较
    const pkSet = new Set(pkColumns.map((p) => p.toLowerCase()))
    for (let i = visibleCols.length - 1; i >= 0; i--) {
      if (pkSet.has(visibleCols[i].name.toLowerCase())) return visibleCols[i].name
    }
    return null
  }, [frozenUntil, pkColumns, visibleCols])

  // 冻结列 → 左侧偏移 px（checkbox 列固定占 40px）。
  // 冻结边界列被隐藏时视为无额外冻结，避免「边界消失 → 冻结范围意外扩大」。
  const frozenOffsets = useMemo(() => {
    const map = new Map<string, number>()
    if (effectiveFrozenUntil === null || !visibleCols.some((v) => v.name === effectiveFrozenUntil)) return map
    let acc = 40
    for (const { name, idx } of visibleCols) {
      map.set(name, acc)
      if (name === effectiveFrozenUntil) break
      acc += colWidths[idx] ?? 96
    }
    return map
  }, [visibleCols, colWidths, effectiveFrozenUntil])

  // 冻结操作：setFrozen 记录用户明确设置（持久化该值）；clearFrozen 撤销自定义 → 恢复智能默认（最后一个主键列）
  const setFrozen = (col: string) => {
    frozenTouched.current = true
    setFrozenUntil(col)
  }
  const clearFrozen = () => {
    frozenTouched.current = false
    setFrozenUntil(null)
  }

  // 列统计开关 + 每列统计（仅当前页，避免全表聚合开销）
  const [showStats, setShowStats] = useState(false)
  const colStats = useMemo(() => {
    if (!showStats) return []
    return columns.map((_, ci) => computeColumnStat(rows, ci))
  }, [showStats, columns, rows])

  // ---- 键盘导航：聚焦单元格（rowIndex 为当前页行索引；colIndex 为「可见列序」）----
  // focusedCell 为 null 表示未聚焦（键盘首次操作时才激活）
  const [focusedCell, setFocusedCell] = useState<{ rowIndex: number; colIndex: number } | null>(null)
  // 可见列的原始索引列表（用于键盘导航时把「可见列序」映射回「原始列索引」）
  const visibleColIdxList = useMemo(() => visibleCols.map((v) => v.idx), [visibleCols])
  // 数据表格横向滚动容器 ref（键盘导航聚焦时手动滚动，正确处理冻结列遮挡）
  const gridScrollRef = useRef<HTMLDivElement>(null)

  // 数据变化（翻页/排序/过滤/换表）导致聚焦单元格越界时，自动清除聚焦，避免指向不存在的单元格
  useEffect(() => {
    if (!focusedCell) return
    if (focusedCell.rowIndex >= rows.length || focusedCell.colIndex >= visibleColIdxList.length) {
      setFocusedCell(null)
    }
  }, [rows.length, visibleColIdxList.length, focusedCell])

  // 聚焦单元格变化后：把该单元格滚动到视区内，且避免被左侧冻结列（checkbox + sticky 列）遮挡。
  // 思路：冻结列用 sticky 始终固定在左侧；非冻结列横向滚动时会被 sticky 列遮住。
  // 因此对「非冻结列」，滚动目标是让它的左边缘贴到「冻结区右边界」，而非容器最左。
  useEffect(() => {
    if (!focusedCell) return
    const container = gridScrollRef.current
    if (!container) return
    const rawCol = visibleColIdxList[focusedCell.colIndex]
    if (rawCol === undefined) return
    // 通过 data 属性定位聚焦单元格的实际 <td>
    const cellEl = container.querySelector<HTMLTableCellElement>(`td[data-grid-cell="${focusedCell.rowIndex}:${rawCol}"]`)
    if (!cellEl) return
    const containerRect = container.getBoundingClientRect()
    const cellRect = cellEl.getBoundingClientRect()
    // 冻结区右边界（px）：checkbox 列(40) + 冻结列；非 table 无 checkbox，从 0 开始。
    // 计算「最后一个冻结列的 left + 该列宽」，即冻结区总宽度。
    const frozenCols = visibleCols.filter((v) => frozenOffsets.has(v.name))
    let frozenZoneWidth = objType === "table" ? 40 : 0
    for (const v of frozenCols) {
      const left = frozenOffsets.get(v.name) ?? 0
      const w = colWidths[v.idx] ?? 96
      frozenZoneWidth = Math.max(frozenZoneWidth, left + w)
    }
    const isFrozenCol = frozenOffsets.has(visibleCols[focusedCell.colIndex]?.name ?? "")
    // 横向滚动：非冻结列 → 让单元格左边缘对齐到冻结区右边界；冻结列 → 无需横滚
    if (!isFrozenCol) {
      const targetLeft = cellRect.left - containerRect.left + container.scrollLeft
      const wantLeft = frozenZoneWidth
      // 仅当需要时滚动（目标列当前不在 [冻结区右边界, 容器右边界] 可见范围内）
      if (targetLeft < wantLeft || targetLeft + cellRect.width > container.clientWidth + container.scrollLeft) {
        container.scrollLeft = targetLeft - wantLeft
      }
    }
    // 纵向滚动：行级 nearest（保持最小位移，避免整表跳动）
    const targetTop = cellRect.top - containerRect.top + container.scrollTop
    if (targetTop < container.scrollTop || targetTop + cellRect.height > container.scrollTop + container.clientHeight) {
      container.scrollTop = targetTop
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusedCell])

  // 聚焦单元格键盘处理：方向键移动 / Enter 编辑 / Esc 取消聚焦
  const handleGridKeyDown = useCallback(
    (e: React.KeyboardEvent) => {
      if (visibleColIdxList.length === 0) return
      // 未聚焦时：按方向键或 Enter 激活第一个单元格
      if (!focusedCell) {
        if (["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight", "Enter"].includes(e.key) && rows.length > 0) {
          e.preventDefault()
          setFocusedCell({ rowIndex: 0, colIndex: 0 })
        }
        return
      }
      const rowCount = rows.length
      const colCount = visibleColIdxList.length
      const { rowIndex, colIndex } = focusedCell
      const move = (nr: number, nc: number) => {
        const rr = Math.min(Math.max(0, nr), Math.max(0, rowCount - 1))
        const cc = Math.min(Math.max(0, nc), Math.max(0, colCount - 1))
        setFocusedCell({ rowIndex: rr, colIndex: cc })
      }
      switch (e.key) {
        case "c":
        case "C":
          // Ctrl/Cmd+C 复制当前聚焦单元格值
          if (e.metaKey || e.ctrlKey) {
            e.preventDefault()
            const rawCol = visibleColIdxList[colIndex]
            const row = rows[rowIndex]
            if (rawCol !== undefined && row) {
              copyToClipboard(copyCellValue(row[rawCol]))
            }
          }
          break
        case "ArrowDown":
          e.preventDefault()
          move(rowIndex + 1, colIndex)
          break
        case "ArrowUp":
          e.preventDefault()
          move(rowIndex - 1, colIndex)
          break
        case "ArrowRight":
          e.preventDefault()
          move(rowIndex, colIndex + 1)
          break
        case "ArrowLeft":
          e.preventDefault()
          move(rowIndex, colIndex - 1)
          break
        case "Home":
          e.preventDefault()
          move(rowIndex, 0)
          break
        case "End":
          e.preventDefault()
          move(rowIndex, colCount - 1)
          break
        case "Enter": {
          // 编辑当前单元格（table 类型才可编辑；view 只读查看）
          e.preventDefault()
          const rawCol = visibleColIdxList[colIndex]
          const row = rows[rowIndex]
          if (rawCol === undefined || !row) break
          if (objType === "table") {
            setEditing({ rowIndex, colIndex: rawCol })
          } else {
            handleViewBigCell(rowIndex, rawCol)
          }
          break
        }
        case "Escape":
          e.preventDefault()
          setFocusedCell(null)
          break
        default:
          break
      }
    },
    [focusedCell, rows, visibleColIdxList, objType, handleViewBigCell],
  )

  useEffect(() => {
    if (subTab === "data") loadData()
    else if (subTab === "struct") loadStruct()
    else loadDDL()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subTab, page, pageSize, name, sortSpecs, filters])

  const pages = total > 0 ? Math.max(1, Math.ceil(total / pageSize)) : -1

  // 切换分页大小：回到第 1 页
  const handlePageSizeChange = (size: number) => {
    setPageSize(size)
    if (page !== 1) onPageChange(1)
  }

  // 页码跳转（回车或失焦时提交）
  const commitJump = () => {
    const n = parseInt(jumpInput, 10)
    if (Number.isFinite(n) && pages > 0) {
      const target = Math.min(Math.max(1, n), pages)
      if (target !== page) onPageChange(target)
    }
    setJumpInput("")
  }

  // 计算可点选的页码列表（首尾各 1 页 + 当前页前后各 2 页，中间用省略号）
  const pageItems = useMemo(() => {
    if (pages <= 0) return []
    const range: (number | "…")[] = []
    const sibling = 2
    const left = Math.max(2, page - sibling)
    const right = Math.min(pages - 1, page + sibling)
    range.push(1)
    if (left > 2) range.push("…")
    for (let p = left; p <= right; p++) range.push(p)
    if (right < pages - 1) range.push("…")
    if (pages > 1) range.push(pages)
    return range
  }, [page, pages])

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* 对象名 + 类型 + 操作按钮 + Tab 切换（同处一行） */}
      <div className="flex shrink-0 flex-wrap items-center gap-2 border-b px-3 py-1.5">
        <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
          <Icon className={cn("h-3.5 w-3.5", meta.cls)} />
        </span>
        <span className="min-w-0 truncate font-medium">{name}</span>
        <Badge variant="secondary" className="shrink-0 px-1.5 py-0 text-[10px] font-normal">{meta.label}</Badge>
        {db && <Badge variant="secondary" className="shrink-0 px-1.5 py-0 text-[10px] font-normal">{db}</Badge>}

        {/* 数据视图的高频操作 + 「更多」菜单：与对象信息同行（不额外占垂直空间）。
            常驻高频：列 / 统计 / 新增行；低频（导出/重置/删除）收进「更多」菜单。 */}
        {subTab === "data" && columns.length > 0 && (
          <>
            <span className="ml-2 h-4 w-px shrink-0 bg-border" />
            {/* 新增行：写入主操作，常驻 */}
            {objType === "table" && (
              <Button
                size="sm"
                className="h-7 gap-1 px-2 text-xs"
                onClick={() => { setInserting(true); setInsertValues({}); setSaveError("") }}
              >
                <Plus className="h-3.5 w-3.5" /> 新增行
              </Button>
            )}
            {/* 删除选中：写入操作，选中行时常驻显示（重要的写入反馈，不放「更多」菜单） */}
            {objType === "table" && selectedRows.size > 0 && (
              <Button
                variant="destructive"
                size="sm"
                className="h-7 gap-1 px-2 text-xs"
                disabled={deleting}
                onClick={() => handleDeleteRows(Array.from(selectedRows))}
              >
                {deleting ? <Loader2 className="h-3 w-3 animate-spin" /> : <Trash2 className="h-3 w-3" />}
                删除 {selectedRows.size} 行
              </Button>
            )}
            {/* 「更多」菜单：导出 / 重置视图 */}
            <div className="relative" ref={moreMenuRef}>
              <Button
                variant="ghost"
                size="icon"
                className="h-7 w-7 text-muted-foreground hover:text-foreground"
                title="更多操作"
                onClick={() => setMoreMenuOpen((v) => !v)}
              >
                <MoreHorizontal className="h-4 w-4" />
              </Button>
              {moreMenuOpen && (
                <div className="absolute right-0 top-full z-30 mt-1 w-44 rounded-md border bg-popover p-1 text-popover-foreground shadow-md">
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-accent disabled:pointer-events-none disabled:opacity-40"
                    disabled={!hasViewState}
                    onClick={() => { resetView(); setMoreMenuOpen(false) }}
                  >
                    <RotateCcw className="h-3.5 w-3.5" /> 重置视图
                  </button>
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-accent disabled:pointer-events-none disabled:opacity-40"
                    disabled={rows.length === 0}
                    onClick={() => {
                      downloadText(
                        `${name}-page${page}-${Date.now()}.csv`,
                        rowsToCSV(
                          visibleCols.map((v) => v.name),
                          rows.map((r) => visibleCols.map((v) => r[v.idx])),
                        ),
                      )
                      setMoreMenuOpen(false)
                    }}
                  >
                    <Download className="h-3.5 w-3.5" /> 导出 CSV（当前页）
                  </button>
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 rounded-sm px-2 py-1.5 text-left text-xs hover:bg-accent disabled:pointer-events-none disabled:opacity-40"
                    disabled={loadingData}
                    onClick={async () => {
                      setMoreMenuOpen(false)
                      try {
                        await exportTableExcel({
                          connId,
                          db,
                          table: name,
                          page: 1,
                          pageSize: 1,
                          sortSpecs: sortSpecs.length > 0 ? sortSpecs : undefined,
                          filters: filters.length > 0 ? filters : undefined,
                        })
                      } catch (e) {
                        setSaveError(e instanceof Error ? e.message : String(e))
                      }
                    }}
                  >
                    <FileSpreadsheet className="h-3.5 w-3.5" /> 导出 Excel（全表）
                  </button>
                </div>
              )}
            </div>
          </>
        )}

        <div className="ml-auto flex shrink-0 items-center gap-1">
          {hasData && (
            <>
              <button
                type="button"
                className={cn(
                  "rounded-md px-2.5 py-1 text-xs transition-colors",
                  subTab === "data" ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-accent",
                )}
                onClick={() => onSubTabChange("data")}
              >
                数据
              </button>
              <button
                type="button"
                className={cn(
                  "rounded-md px-2.5 py-1 text-xs transition-colors",
                  subTab === "struct" ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-accent",
                )}
                onClick={() => onSubTabChange("struct")}
              >
                结构
              </button>
            </>
          )}
          <button
            type="button"
            className={cn(
              "rounded-md px-2.5 py-1 text-xs transition-colors",
              subTab === "ddl" ? "bg-primary/10 font-medium text-primary" : "text-muted-foreground hover:bg-accent",
            )}
            onClick={() => onSubTabChange("ddl")}
          >
            DDL
          </button>
        </div>
      </div>

      {/* 数据 */}
      {subTab === "data" ? (
        <div className="flex min-h-0 flex-1 flex-col p-2">
          {loadingData && !loadedOnce ? (
            // 首次加载：尚无旧数据可展示，显示居中 loading
            <div className="flex flex-1 items-center justify-center text-xs text-muted-foreground">
              <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> 加载数据...
            </div>
          ) : error ? (
            // 错误条：数据视图居中展示，左侧图标 + 错误标题 + 详细信息 + 刷新按钮
            <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-sm">
              <div className="flex max-w-md items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-4">
                <AlertCircle className="mt-0.5 h-5 w-5 shrink-0 text-destructive" />
                <div className="min-w-0 flex-1">
                  <div className="font-medium text-destructive">加载失败</div>
                  <div className="mt-0.5 break-words text-foreground/80">{error}</div>
                </div>
              </div>
              <Button variant="outline" size="sm" className="gap-1.5" onClick={() => void loadData()}>
                <RefreshCw className="h-3.5 w-3.5" /> 刷新
              </Button>
            </div>
          ) : columns.length === 0 ? (
            <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">无数据</div>
          ) : (
            <div className="relative flex min-h-0 flex-1 flex-col">
              {/* 刷新中：保留旧数据，顶部叠加细进度条，避免整表闪烁 */}
              {loadingData && (
                <div className="absolute inset-x-0 top-0 z-20 h-0.5 overflow-hidden rounded-full bg-primary/10">
                  <div className="h-full w-1/3 animate-[paging_1s_ease-in-out_infinite] rounded-full bg-primary" />
                </div>
              )}
              {/* 列结构加载失败提示：编辑与大字段省略依赖列结构，失败时提示并支持重试 */}
              {structError && (
                <div className="mb-1.5 flex shrink-0 items-center gap-2 rounded-md border border-destructive/40 bg-destructive/5 px-2 py-1.5 text-xs text-destructive">
                  <AlertCircle className="h-3.5 w-3.5 shrink-0" />
                  <span className="min-w-0 flex-1 break-words">
                    列结构加载失败，编辑与大字段查看暂不可用：{structError}
                  </span>
                  <Button variant="outline" size="sm" className="h-6 shrink-0 gap-1 px-2 text-xs" onClick={() => void retryStruct()}>
                    <RefreshCw className="h-3 w-3" /> 重试
                  </Button>
                </div>
              )}
              {/* 过滤状态条：显式提示已应用的过滤条件，避免用户误以为数据丢失 */}
              {filters.length > 0 && (
                <div className="mb-1.5 flex shrink-0 flex-wrap items-center gap-1.5 rounded-md border border-primary/20 bg-primary/5 px-2 py-1.5">
                  <Filter className="h-3.5 w-3.5 shrink-0 text-primary" />
                  <span className="text-xs font-medium text-primary">已应用 {filters.length} 个过滤条件</span>
                  {filters.map((f, i) => (
                    <span key={i} className="flex items-center gap-1 rounded bg-primary/10 px-1.5 py-0.5 text-xs text-primary">
                      <span className="font-medium">{f.column}</span>
                      <span className="text-muted-foreground">{FILTER_OP_LABEL[f.op]}</span>
                      {f.value !== undefined && f.value !== null && f.value !== "" && (
                        <span className="max-w-[120px] truncate">“{String(f.value)}”</span>
                      )}
                      <button type="button" className="ml-0.5 text-muted-foreground hover:text-foreground" onClick={() => clearColumnFilter(f.column)}>
                        <X className="h-3 w-3" />
                      </button>
                    </span>
                  ))}
                  <button type="button" className="ml-1 text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline" onClick={clearAllFilters}>
                    清除全部
                  </button>
                </div>
              )}
              <div
                ref={gridScrollRef}
                className="scrollbar-thin flex-1 overflow-auto rounded-md border outline-none"
                tabIndex={0}
                onKeyDown={handleGridKeyDown}
                onBlur={() => setFocusedCell(null)}
              >
                {/* 容器级单实例右键菜单：避免每行一个 ContextMenu 组件树导致右键时整表重渲染变慢 */}
                <ContextMenu onOpenChange={(open) => { if (!open) { rowMenuRef.current?.hide(); clearFocusedRow() } }}>
                  <ContextMenuTrigger asChild>
                <table className="data-grid-table w-full border-separate border-spacing-0 text-[12px]" style={{ tableLayout: "fixed", minWidth: tableMinWidth }}>
                <thead>
                  <tr className="bg-muted text-left" onContextMenu={(e) => e.stopPropagation()}>
                    {(objType === "table" || columns.length > 0) && (
                      <th
                        className={cn(
                          "sticky left-0 top-0 z-30 select-none bg-muted py-1.5 text-center font-normal frozen-col",
                          objType === "table" ? "w-16 px-1" : "w-9 px-1",
                        )}
                      >
                        <span className="flex items-center justify-center gap-0.5">
                          {/* 全选（仅 table 有行选择） */}
                          {objType === "table" && (
                            <span
                              className={cn(
                                "flex h-6 w-6 cursor-pointer items-center justify-center rounded hover:bg-muted-foreground/10",
                                allSelected ? "text-primary" : "text-muted-foreground/60",
                              )}
                              title={allSelected ? "取消全选" : "全选当前页"}
                              onClick={toggleSelectAll}
                            >
                              {allSelected ? (
                                <Check className="h-3.5 w-3.5" />
                              ) : (
                                <span className="text-xs">#</span>
                              )}
                            </span>
                          )}
                          {/* 列管理：放在表头序号旁，与「列」语义同域。
                              注意：columnPanelRef 绑在「按钮 + 面板」的共同容器上，
                              否则 useClickOutside 会把按钮判定为「外部」，导致面板无法用按钮收回。 */}
                          <span ref={columnPanelRef} className="relative flex h-6 w-6 items-center justify-center">
                            <button
                              type="button"
                              className={cn(
                                "flex h-5 w-5 items-center justify-center rounded hover:bg-muted-foreground/10",
                                (showColumnPanel || hiddenColumns.size > 0) ? "text-primary" : "text-muted-foreground/60",
                              )}
                              title="显示/隐藏列"
                              onClick={(e) => {
                                e.stopPropagation()
                                setShowColumnPanel((v) => !v)
                              }}
                            >
                              <Columns3 className="h-3.5 w-3.5" />
                            </button>
                            {showColumnPanel && (
                              <div
                                className="absolute left-0 top-full z-40 mt-1 max-h-80 w-64 overflow-auto rounded-md border bg-popover p-1.5 text-left shadow-md"
                                onClick={(e) => e.stopPropagation()}
                              >
                                <div className="mb-1 flex items-center justify-between px-1">
                                  <span className="text-xs font-semibold text-foreground">显示列</span>
                                  <button type="button" className="text-xs text-muted-foreground underline-offset-2 hover:text-foreground hover:underline" onClick={() => setHiddenColumns(new Set())}>
                                    全部显示
                                  </button>
                                </div>
                                {columns.map((col) => {
                                  const meta = colMetaMap.get(col)
                                  const visible = !hiddenColumns.has(col)
                                  return (
                                    <label
                                      key={col}
                                      className="flex cursor-pointer items-center gap-1.5 rounded px-1 py-1 text-xs hover:bg-muted/60"
                                    >
                                      <Checkbox
                                        checked={visible}
                                        onCheckedChange={() => toggleColumn(col)}
                                      />
                                      {meta?.primaryKey && <TableIcon icon={KeyRound} size={12} className="text-amber-500" />}
                                      {!meta?.primaryKey && meta?.unique && <TableIcon icon={ShieldCheck} size={12} className="text-blue-500" />}
                                      {!meta?.primaryKey && !meta?.unique && meta?.indexed && <TableIcon icon={ListOrdered} size={12} className="text-muted-foreground/70" />}
                                      <span className={cn("min-w-0 flex-1 truncate", !visible && "text-muted-foreground")}>{col}</span>
                                    </label>
                                  )
                                })}
                              </div>
                            )}
                          </span>
                        </span>
                      </th>
                    )}
                    {visibleCols.map(({ name: c, idx: i }) => {
                      const sortSpec = sortSpecs.find((s) => s.column === c)
                      const sortIdx = sortSpec ? sortSpecs.findIndex((s) => s.column === c) : -1
                      const big = bigFieldCols.has(c)
                      const filtered = hasFilter(c)
                      const meta = colMetaMap.get(c)
                      const isPK = pkColumns.includes(c)
                      const isUnique = !!meta?.unique
                      const isIndexed = !!meta?.indexed
                      const frozenLeft = frozenOffsets.get(c)
                      return (
                        <ContextMenu key={i}>
                          <ContextMenuTrigger asChild>
                            <th
                              className={cn(
                                "sticky top-0 z-20 select-none bg-muted px-2 py-1.5 font-medium text-muted-foreground",
                                big ? "cursor-default" : "cursor-pointer hover:bg-muted/60",
                                frozenLeft !== undefined && "sticky left-0 top-0 z-30 bg-muted frozen-col",
                              )}
                              style={{ width: `${colWidths[i]}px`, ...(frozenLeft !== undefined ? { left: frozenLeft } : {}) }}
                              title={
                                big
                                  ? "大字段不支持排序"
                                  : [
                                      meta?.dataType ? `类型 ${meta.dataType}` : "",
                                      isPK && "主键",
                                      isUnique && "唯一约束",
                                      isIndexed && "索引",
                                      `点击排序：${c}（Shift+点击叠加多列排序）`,
                                    ]
                                      .filter(Boolean)
                                      .join(" · ")
                              }
                              onClick={(e) => {
                                // 拖拽列宽结束后的 click 冒泡到此：忽略（避免误触发排序）
                                if (justResizedRef.current) {
                                  justResizedRef.current = false
                                  return
                                }
                                if (!big) handleSort(c, e.shiftKey)
                              }}
                            >
                              {/* 列宽拖拽手柄：绝对定位在表头右边界，hover 时显示竖线 */}
                              <span
                                className={cn(
                                  "absolute right-0 top-0 z-10 h-full w-1 cursor-col-resize",
                                  resizingCol === c ? "bg-primary/40" : "bg-transparent hover:bg-primary/30",
                                )}
                                title="拖动调整列宽"
                                onMouseDown={(e) => startResize(e, c, colWidths[i])}
                                onDoubleClick={(e) => {
                                  // 双击手柄：重置该列为自适应宽度
                                  e.stopPropagation()
                                  setColWidthOverrides((prev) => {
                                    const next = { ...prev }
                                    delete next[c]
                                    return next
                                  })
                                }}
                              />
                              <div className="flex items-center gap-1">
                                {isPK && <TableIcon icon={KeyRound} className="text-amber-500" />}
                                {isUnique && <TableIcon icon={ShieldCheck} className="text-blue-500" />}
                                {isIndexed && <TableIcon icon={ListOrdered} className="text-muted-foreground/70" />}
                                <span className="min-w-0 flex-1 truncate">{c}</span>
                                {sortSpec ? (
                                  <>
                                    {sortSpec.order === "asc" ? <TableIcon icon={ArrowUp} className="text-primary" /> : <TableIcon icon={ArrowDown} className="text-primary" />}
                                    {sortSpecs.length > 1 && <span className="text-[10px] font-semibold text-primary">{sortIdx + 1}</span>}
                                  </>
                                ) : (
                                  !big && <TableIcon icon={ChevronsUpDown} className="opacity-0 group-hover:opacity-60" />
                                )}
                                {/* 漏斗图标：打开过滤面板（大字段列仅允许为空/非空，仍可点） */}
                                <button
                                  type="button"
                                  className={cn(
                                    "flex h-4 w-4 shrink-0 items-center justify-center rounded hover:bg-primary/10",
                                    filtered ? "text-primary" : "text-muted-foreground/50 hover:text-muted-foreground",
                                  )}
                                  title={filtered ? "已过滤，点击编辑" : "筛选此列"}
                                  onClick={(e) => {
                                    e.stopPropagation()
                                    openFilterPanel(c)
                                  }}
                                >
                                  <TableIcon icon={Filter} size={12} />
                                </button>
                              </div>
                              {/* 过滤面板 */}
                              {filterCol === c && (
                                <div
                                  ref={filterPanelRef}
                                  className="absolute right-0 top-full z-30 mt-1 w-56 rounded-md border bg-popover p-2 shadow-md"
                                  onClick={(e) => e.stopPropagation()}
                                >
                                  <div className="mb-1.5 flex items-center justify-between">
                                    <span className="truncate text-xs font-semibold text-foreground">{c}</span>
                                    <button type="button" className="text-muted-foreground hover:text-foreground" onClick={() => setFilterCol(null)}>
                                      <X className="h-3.5 w-3.5" />
                                    </button>
                                  </div>
                                  <Select value={filterDraft.op} onValueChange={(v) => setFilterDraft((d) => ({ ...d, op: v as FilterOp }))}>
                                    <SelectTrigger className="h-7 w-full text-xs">
                                      <SelectValue />
                                    </SelectTrigger>
                                    <SelectContent>
                                      {FILTER_OPS.map((f) => (
                                        <SelectItem key={f.op} value={f.op}>{f.label}</SelectItem>
                                      ))}
                                    </SelectContent>
                                  </Select>
                                  {FILTER_OPS.find((f) => f.op === filterDraft.op)?.needValue && (
                                    <Input
                                      className="mt-1.5 h-7 text-xs"
                                      placeholder="输入过滤值"
                                      value={filterDraft.value}
                                      autoFocus
                                      onChange={(e) => setFilterDraft((d) => ({ ...d, value: e.target.value }))}
                                      onKeyDown={(e) => e.key === "Enter" && applyFilter(c)}
                                    />
                                  )}
                                  <div className="mt-2 flex justify-end gap-1">
                                    {filtered && (
                                      <Button variant="ghost" size="sm" className="h-7 px-2 text-xs" onClick={() => clearColumnFilter(c)}>
                                        清除
                                      </Button>
                                    )}
                                    <Button size="sm" className="h-7 px-2 text-xs" onClick={() => applyFilter(c)}>
                                      应用
                                    </Button>
                                  </div>
                                </div>
                              )}
                            </th>
                          </ContextMenuTrigger>
                          <ContextMenuContent>
                            {/* 列头右键聚合菜单：排序 / 筛选 / 隐藏 / 复制列名 */}
                            {!big && (
                              <>
                                <ContextMenuItem onSelect={() => { setSortSpecs((p) => p.some((s) => s.column === c && s.order === "asc") ? p.filter((s) => s.column !== c) : [...p.filter((s) => s.column !== c), { column: c, order: "asc" }]); if (page !== 1) onPageChange(1) }}>
                                  <ArrowUp className="mr-2 h-3.5 w-3.5" /> 升序排序
                                </ContextMenuItem>
                                <ContextMenuItem onSelect={() => { setSortSpecs((p) => p.some((s) => s.column === c && s.order === "desc") ? p.filter((s) => s.column !== c) : [...p.filter((s) => s.column !== c), { column: c, order: "desc" }]); if (page !== 1) onPageChange(1) }}>
                                  <ArrowDown className="mr-2 h-3.5 w-3.5" /> 降序排序
                                </ContextMenuItem>
                                {sortSpec && (
                                  <ContextMenuItem onSelect={() => setSortSpecs((p) => p.filter((s) => s.column !== c))}>
                                    <ChevronsUpDown className="mr-2 h-3.5 w-3.5" /> 取消排序
                                  </ContextMenuItem>
                                )}
                                <ContextMenuSeparator />
                              </>
                            )}
                            <ContextMenuItem onSelect={() => openFilterPanel(c)}>
                              <Filter className="mr-2 h-3.5 w-3.5" /> 筛选此列
                            </ContextMenuItem>
                            <ContextMenuItem onSelect={() => { setFilters((p) => p.filter((f) => f.column !== c).concat({ column: c, op: "isNull" })); if (page !== 1) onPageChange(1) }}>
                              <Filter className="mr-2 h-3.5 w-3.5" /> 只看空值
                            </ContextMenuItem>
                            <ContextMenuItem onSelect={() => { setFilters((p) => p.filter((f) => f.column !== c).concat({ column: c, op: "isNotNull" })); if (page !== 1) onPageChange(1) }}>
                              <Filter className="mr-2 h-3.5 w-3.5" /> 只看非空
                            </ContextMenuItem>
                            <ContextMenuItem onSelect={() => toggleColumn(c)}>
                              <EyeOff className="mr-2 h-3.5 w-3.5" /> 隐藏此列
                            </ContextMenuItem>
                            <ContextMenuSeparator />
                            {/* 冻结列：固定到当前列（含左侧所有可见列）；右键当前边界列时显示「取消固定」（撤销自定义，恢复默认主键冻结） */}
                            {effectiveFrozenUntil === c ? (
                              <ContextMenuItem onSelect={clearFrozen}>
                                <PinOff className="mr-2 h-3.5 w-3.5" /> 取消固定
                              </ContextMenuItem>
                            ) : (
                              <>
                                <ContextMenuItem onSelect={() => setFrozen(c)}>
                                  <Pin className="mr-2 h-3.5 w-3.5" /> 固定到此列
                                </ContextMenuItem>
                                {effectiveFrozenUntil !== null && (
                                  <ContextMenuItem onSelect={clearFrozen}>
                                    <PinOff className="mr-2 h-3.5 w-3.5" /> 取消固定
                                  </ContextMenuItem>
                                )}
                              </>
                            )}
                            <ContextMenuItem onSelect={() => copyToClipboard(c)}>
                              <Copy className="mr-2 h-3.5 w-3.5" /> 复制列名
                            </ContextMenuItem>
                          </ContextMenuContent>
                        </ContextMenu>
                      )
                    })}
                  </tr>
                </thead>
                <tbody>
                  {rows.length === 0 && (
                    <tr onContextMenu={(e) => e.stopPropagation()}>
                      <td colSpan={1 + visibleCols.length} className="px-2 py-8 text-center text-sm text-muted-foreground">
                        {filters.length > 0 ? `无匹配数据（已应用 ${filters.length} 个过滤条件，可通过表头漏斗或上方清除）` : "无数据"}
                      </td>
                    </tr>
                  )}
                  {rows.map((row, ri) => {
                    const rk = rowKey(ri)
                    const selected = rk !== null && selectedRows.has(rk)
                    return (
                      <tr
                        key={ri}
                      >
                            {objType === "table" ? (
                              <td
                                className="sticky left-0 z-10 cursor-pointer select-none px-2 py-1 frozen-col"
                                style={{ backgroundColor: selected ? "#dbeafe" : (ri) % 2 === 1 ? "#f8fafc" : "#ffffff" }}
                                title={selected ? "取消选中" : "选中该行"}
                                onClick={(e) => {
                                  e.stopPropagation()
                                  toggleRow(ri)
                                }}
                                onContextMenu={(e) => {
                                  focusRow(e.currentTarget.closest("tr") as HTMLTableRowElement)
                                  rowMenuRef.current?.show({ rowIndex: ri, colIndex: null })
                                }}
                              >
                                <span className="flex items-center justify-center gap-1">
                                  {selected && <Check className="h-3.5 w-3.5 shrink-0 text-primary" />}
                                  <span className={cn(
                                    "font-mono text-[11px] tabular-nums",
                                    selected ? "text-foreground" : "text-muted-foreground/50",
                                  )}>
                                    {(page - 1) * pageSize + ri + 1}
                                  </span>
                                </span>
                              </td>
                            ) : (
                              <td
                                className="select-none px-2 py-1 text-right font-mono text-[11px] tabular-nums text-muted-foreground/50"
                                style={{ backgroundColor: selected ? "#dbeafe" : (ri) % 2 === 1 ? "#f8fafc" : "#ffffff" }}
                                onContextMenu={(e) => {
                                  focusRow(e.currentTarget.closest("tr") as HTMLTableRowElement)
                                  rowMenuRef.current?.show({ rowIndex: ri, colIndex: null })
                                }}
                              >
                                {(page - 1) * pageSize + ri + 1}
                              </td>
                            )}
                            {visibleCols.map(({ name: colName, idx: ci }) => {
                              const cell = row[ci]
                              const isBig = bigFieldCols.has(colName)
                              const frozenLeft = frozenOffsets.get(colName)
                              return (
                                <td
                                  key={ci}
                                  data-grid-cell={`${ri}:${ci}`}
                                  className={cn(
                                    "group/cell cursor-pointer px-2 py-1",
                                    // 冻结列：sticky 悬浮在其他列上方，边框由 .frozen-col 统一管理
                                    frozenLeft !== undefined && "sticky left-0 z-10 frozen-col",
                                    // 键盘聚焦：外描边高亮，与右键聚焦的 row-context-focused 区分
                                    focusedCell?.rowIndex === ri && focusedCell?.colIndex === visibleColIdxList.indexOf(ci) && "ring-2 ring-inset ring-primary",
                                  )}
                                  style={{
                                    // 冻结列：left 偏移（水平固定位置）
                                    ...(frozenLeft !== undefined ? { left: frozenLeft } : {}),
                                    // 所有单元格背景色统一用内联绝对颜色（选中 > 斑马 > 默认），
                                    // 冻结列与非冻结列用同一套色值，避免 Tailwind 类与 hex 不一致
                                    backgroundColor: selected
                                      ? "#dbeafe"
                                      : (ri) % 2 === 1
                                        ? "#f8fafc"
                                        : "#ffffff",
                                  }}
                                  title={isBig ? "点击加载完整内容" : objType === "table" ? "双击就地编辑" : undefined}
                                  onMouseDown={() => {
                                    // 记录此刻是否有浮层打开：有则本次点击只关浮层，不聚焦单元格
                                    dismissingOverlayRef.current = moreMenuOpen || showColumnPanel || filterCol !== null
                                  }}
                                  onClick={() => {
                                    // 有浮层打开时的点击：只负责关闭浮层（由 useClickOutside 处理），不聚焦
                                    if (dismissingOverlayRef.current) {
                                      dismissingOverlayRef.current = false
                                      return
                                    }
                                    // 单击仅聚焦/选中（不弹窗），避免与双击就地编辑、编辑图标弹窗冲突。
                                    // 大字段列无就地编辑，单击直接加载完整内容。
                                    setFocusedCell({ rowIndex: ri, colIndex: visibleColIdxList.indexOf(ci) })
                                    if (isBig) {
                                      handleViewBigCell(ri, ci)
                                    }
                                  }}
                                  onDoubleClick={() => {
                                    // 双击就地编辑：仅 table 的非大字段列；大字段/复杂值走编辑图标弹窗
                                    if (objType === "table" && !isBig) {
                                      setFocusedCell({ rowIndex: ri, colIndex: visibleColIdxList.indexOf(ci) })
                                      setInlineEdit({ rowIndex: ri, colIndex: ci })
                                      setInlineValue(cell === null || cell === undefined ? "" : String(cell))
                                    }
                                  }}
                                  onContextMenu={(e) => {
                                    focusRow(e.currentTarget.closest("tr") as HTMLTableRowElement)
                                    rowMenuRef.current?.show({ rowIndex: ri, colIndex: ci })
                                  }}
                                >
                                  {inlineEdit?.rowIndex === ri && inlineEdit.colIndex === ci ? (
                                    // 就地编辑态：内联 input（回车保存 / Esc 取消 / 失焦保存）
                                    <input
                                      autoFocus
                                      className="h-6 w-full rounded-sm border border-primary bg-background px-1 text-[12px] outline-none ring-1 ring-primary"
                                      value={inlineValue}
                                      disabled={inlineSaving}
                                      onChange={(e) => setInlineValue(e.target.value)}
                                      onClick={(e) => e.stopPropagation()}
                                      onKeyDown={(e) => {
                                        e.stopPropagation()
                                        if (e.key === "Enter") {
                                          e.preventDefault()
                                          void saveInlineEdit()
                                        } else if (e.key === "Escape") {
                                          e.preventDefault()
                                          cancelInlineEdit()
                                        }
                                      }}
                                      onBlur={() => {
                                        if (!inlineSaving) void saveInlineEdit()
                                      }}
                                    />
                                  ) : isBig ? (
                                    <div className="flex items-center justify-center gap-1 whitespace-nowrap text-muted-foreground/60">
                                      <span className="text-xs leading-none">…</span>
                                      <ChevronsRight className="h-3 w-3 shrink-0" />
                                    </div>
                                  ) : (
                                    <div className="flex items-center gap-1.5">
                                      <div className="min-w-0 flex-1 truncate" title={renderCellText(cell)}>
                                        <span className={cn(
                                          isNullCell(cell) && "italic text-muted-foreground/70",
                                          typeof cell === "string" && cell === "" && "text-muted-foreground/70",
                                        )}>
                                          {renderCellText(cell)}
                                        </span>
                                      </div>
                                      {objType === "table" && (
                                        // 编辑图标：hover 显示，点击打开完整编辑弹窗（复杂值/长文本等）
                                        <button
                                          type="button"
                                          className="shrink-0 rounded-sm p-0.5 text-muted-foreground opacity-0 transition-opacity hover:bg-accent hover:text-foreground group-hover/cell:opacity-100"
                                          title="编辑（完整编辑器）"
                                          onClick={(e) => {
                                            e.stopPropagation()
                                            setEditing({ rowIndex: ri, colIndex: ci })
                                          }}
                                        >
                                          <Pencil className="h-3 w-3" />
                                        </button>
                                      )}
                                    </div>
                                  )}
                                </td>
                              )
                            })}
                          </tr>
                    )
                  })}
                </tbody>
                {/* 列统计行（仅当前页）：数值列显示 sum/avg，其余显示 NULL 计数 */}
                {showStats && (
                  <tfoot onContextMenu={(e) => e.stopPropagation()}>
                    <tr>
                      {objType === "table" && (
                        <td className="sticky left-0 bottom-0 z-30 bg-muted px-2 py-1 text-xs font-medium text-muted-foreground frozen-col">Σ</td>
                      )}
                      {visibleCols.map(({ name: colName, idx: ci }) => {
                        const st = colStats[ci]
                        const frozenLeft = frozenOffsets.get(colName)
                        if (!st) {
                          return (
                            <td
                              key={ci}
                              className={cn(
                                "sticky bottom-0 z-20 bg-muted px-2 py-1 text-[11px] text-muted-foreground",
                                frozenLeft !== undefined && "sticky left-0 bottom-0 z-30 bg-muted frozen-col",
                              )}
                              style={frozenLeft !== undefined ? { left: frozenLeft } : undefined}
                            >
                              —
                            </td>
                          )
                        }
                        return (
                          <td
                            key={ci}
                            className={cn(
                              "sticky bottom-0 z-20 bg-muted px-2 py-1 text-[11px] text-muted-foreground",
                              frozenLeft !== undefined && "sticky left-0 bottom-0 z-30 bg-muted frozen-col",
                            )}
                            style={frozenLeft !== undefined ? { left: frozenLeft } : undefined}
                          >
                            {st.numeric ? (
                              <span className="tabular-nums">
                                Σ {fmtNum(st.sum)} · avg {fmtNum(st.avg)}
                                {st.nullCount > 0 ? ` · ${st.nullCount} 空` : ""}
                              </span>
                            ) : (
                              <span className="tabular-nums">{st.nullCount > 0 ? `${st.nullCount} 空` : "—"}</span>
                            )}
                          </td>
                        )
                      })}
                    </tr>
                  </tfoot>
                )}
              </table>
                </ContextMenuTrigger>
                <TableBrowserRowMenu
                  ref={rowMenuRef}
                  columns={columns}
                  rows={rows}
                  bigFieldCols={bigFieldCols}
                  objType={objType}
                  selectedRows={selectedRows}
                  rowKey={rowKey}
                  page={page}
                  pageSize={pageSize}
                  pkColumns={pkColumns}
                  onQuickFilter={quickFilter}
                  onDeleteRows={handleDeleteRows}
                />
                </ContextMenu>
              </div>
            </div>
          )}

          {saveError && (
            <div className="mt-2 flex items-start gap-1.5 rounded-md border border-destructive/40 bg-destructive/5 px-2 py-1.5 text-xs text-destructive">
              <AlertCircle className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <span className="break-words">{saveError}</span>
            </div>
          )}

          {/* 分页条：固定到底部（即使无数据 / 出错也在最下方）。
              父容器为 flex-1 列向布局，用 mt-auto 把分页条推到内容底部；border-t 与 bg-muted/10 与上方内容分隔 */}
          <div className="mt-auto flex shrink-0 flex-wrap items-center justify-between gap-x-3 gap-y-1 border-t bg-muted/10 px-2 py-1 text-xs">
            <div className="flex flex-wrap items-center gap-3">
              {/* 就绪/执行中状态指示 */}
              {running ? (
                <span className="flex items-center gap-1 text-primary">
                  <Loader2 className="h-3.5 w-3.5 animate-spin" /> 执行中…
                </span>
              ) : (
                <span className="text-muted-foreground/70">就绪</span>
              )}
              {/* 工作区保存失败（持久化错误；只有从 WorkspaceLayout 透传才显示） */}
              {persistFailed && (
                <button
                  type="button"
                  className="flex items-center gap-1 text-destructive hover:underline"
                  title="工作区保存失败，请检查连接后重试；点击关闭提示"
                  onClick={onClearPersistFailed}
                >
                  <AlertTriangle className="h-3.5 w-3.5" /> 工作区保存失败
                </button>
              )}
              {/* 数据视图：总行数 / 当前加载时间 */}
              {subTab === "data" && (
                <>
                  <span className="h-3 w-px shrink-0 bg-border" />
                  <span className="tabular-nums text-muted-foreground">
                    {total >= 0 ? `共 ${total} 行` : "总行数未知"}
                  </span>
                </>
              )}
            </div>

            {/* 数据视图专属：分页/每页控件。非数据 subTab 保留最小占位避免布局抖动 */}
            <div className="flex flex-wrap items-center gap-1">
              {subTab === "data" ? (
                <>
                  <Button variant="outline" size="sm" className="h-7 w-7 p-0" disabled={page <= 1 || loadingData} onClick={() => onPageChange(page - 1)}>
                    <ChevronLeft className="h-3.5 w-3.5" />
                  </Button>

                  {pages > 0 && pageItems.map((item, i) =>
                    item === "…" ? (
                      <span key={`e-${i}`} className="px-1 text-xs text-muted-foreground">…</span>
                    ) : (
                      <Button
                        key={item}
                        variant={item === page ? "default" : "outline"}
                        size="sm"
                        className="h-7 min-w-7 px-1.5 text-xs"
                        disabled={loadingData}
                        onClick={() => onPageChange(item)}
                      >
                        {item}
                      </Button>
                    )
                  )}

                  <Button variant="outline" size="sm" className="h-7 w-7 p-0" disabled={(pages > 0 && page >= pages) || loadingData} onClick={() => onPageChange(page + 1)}>
                    <ChevronRight className="h-3.5 w-3.5" />
                  </Button>

                  <div className="ml-2 flex items-center gap-1">
                    <span className="text-xs text-muted-foreground">跳至</span>
                    <Input
                      className="h-7 w-14 px-1.5 text-center text-xs tabular-nums"
                      value={jumpInput}
                      placeholder={String(page)}
                      inputMode="numeric"
                      onChange={(e) => setJumpInput(e.target.value.replace(/[^0-9]/g, ""))}
                      onKeyDown={(e) => e.key === "Enter" && commitJump()}
                      onBlur={commitJump}
                    />
                    <span className="text-xs text-muted-foreground">/ {pages > 0 ? pages : "-"} 页</span>
                  </div>

                  <div className="ml-2 flex items-center gap-1">
                    <span className="text-xs text-muted-foreground">每页</span>
                    <Select value={String(pageSize)} onValueChange={(v) => handlePageSizeChange(Number(v))}>
                      <SelectTrigger className="h-7 w-[64px] px-2 text-xs">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {[50, 100, 200, 500, 1000].map((s) => (
                          <SelectItem key={s} value={String(s)}>{s}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>

                  {/* 统计开关：对表格数据的分析操作，与分页同层放表格下方右侧 */}
                  <Button
                    variant={showStats ? "secondary" : "ghost"}
                    size="sm"
                    className="ml-1 h-7 gap-1 px-2 text-xs text-muted-foreground hover:text-foreground"
                    title="显示/隐藏当前页每列的统计（合计/平均值/空值数）"
                    onClick={() => setShowStats((v) => !v)}
                  >
                    <BarChart3 className="h-3.5 w-3.5" /> 统计
                  </Button>
                </>
              ) : (
                /* 非数据 subTab 保留的右侧占位，避免布局抖动 */
                <span className="text-xs text-muted-foreground/60">—</span>
              )}
            </div>
          </div>

          {/* 单元格弹层（编辑 / 只读查看共用；大字段懒加载复用 readonly 模式） */}
          <Dialog open={editing !== null} onOpenChange={(open) => { if (!open) { setEditing(null); setMaximized(false) } }}>
            <DialogContent className={cn(
              "max-w-[720px]",
              maximized && "h-[92vh] max-w-[96vw] flex flex-col",
            )}>
              <DialogHeader className="shrink-0 pr-8">
                <DialogTitle className="flex items-center gap-2">
                  <span className="flex-1">{editing?.readonly ? "查看单元格" : "编辑单元格"}</span>
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-6 w-6 p-0"
                    title={maximized ? "还原" : "最大化"}
                    onClick={() => setMaximized((m) => !m)}
                  >
                    {maximized ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
                  </Button>
                </DialogTitle>
              </DialogHeader>
              {editing !== null && columns[editing.colIndex] !== undefined ? (() => {
                const key = `${editing.rowIndex}:${editing.colIndex}`
                const isBig = bigFieldCols.has(columns[editing.colIndex])
                // 大字段懒加载：未缓存且在请求中 → 展示 loading；已缓存 → 用缓存值；其它 → 来自行
                let cellValue: unknown = rows[editing.rowIndex]?.[editing.colIndex]
                if (isBig) {
                  if (bigValueMap.has(key)) {
                    cellValue = bigValueMap.get(key)
                  } else if (bigLoading) {
                    cellValue = undefined // CellEditor 渲染前先在下方展示 loading
                  }
                }
                return (
                  <>
                    {/* 大字段懒加载中的 loading 占位（在 CellEditor 上方，避免与 readonly 渲染冲突） */}
                    {isBig && bigLoading && !bigValueMap.has(key) && (
                      <div className="flex items-center justify-center py-8 text-xs text-muted-foreground">
                        <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> 加载完整内容...
                      </div>
                    )}
                    {/* 大字段加载错误 */}
                    {isBig && bigError && !bigValueMap.has(key) && !bigLoading && (
                      <div className="flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
                        <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
                        <div className="break-words text-destructive">{bigError}</div>
                      </div>
                    )}
                    {(isBig ? bigValueMap.has(key) : true) && (
                      <CellEditor
                        column={columns[editing.colIndex]}
                        dataType={colMetaMap.get(columns[editing.colIndex])?.dataType ?? ""}
                        value={cellValue}
                        nullable={colMetaMap.get(columns[editing.colIndex])?.nullable ?? true}
                        saving={saving}
                        readonly={editing.readonly}
                        maximized={maximized}
                        onSave={handleSaveCell}
                        onCancel={() => {
                          setEditing(null)
                          setBigError("")
                        }}
                      />
                    )}
                    {saveError && <div className="text-xs text-destructive">{saveError}</div>}
                  </>
                )
              })() : null}
            </DialogContent>
          </Dialog>

          {/* 新增行表单：列出所有非自增列，逐列输入，提交时 INSERT */}
          <Dialog open={inserting} onOpenChange={(open) => { if (!open) setInserting(false) }}>
            <DialogContent className="max-w-[560px]">
              <DialogHeader>
                <DialogTitle>新增行 · {name}</DialogTitle>
              </DialogHeader>
              <div className="scrollbar-thin max-h-[60vh] overflow-auto pr-1">
                <div className="space-y-2">
                  {columns.map((col) => {
                    const meta = colMetaMap.get(col)
                    if (meta?.autoIncrement) {
                      return (
                        <div key={col} className="flex items-center gap-2 text-xs text-muted-foreground">
                          <span className="w-40 shrink-0 truncate font-medium text-foreground">{col}</span>
                          <span className="text-muted-foreground">（自增，自动生成）</span>
                        </div>
                      )
                    }
                    return (
                      <div key={col} className="flex items-center gap-2">
                        <span className="w-40 shrink-0 truncate text-xs font-medium" title={`${col} · ${meta?.dataType ?? ""}`}>
                          {col}
                          {meta?.nullable === false && <span className="text-destructive">*</span>}
                        </span>
                        <Input
                          className="h-8 flex-1 text-xs"
                          placeholder={meta?.dataType ? `类型 ${meta.dataType}，可留空` : "可留空（NULL）"}
                          value={insertValues[col] ?? ""}
                          onChange={(e) => setInsertValues((v) => ({ ...v, [col]: e.target.value }))}
                        />
                      </div>
                    )
                  })}
                </div>
                {saveError && <div className="mt-2 text-xs text-destructive">{saveError}</div>}
              </div>
              <div className="mt-2 flex justify-end gap-2">
                <Button variant="ghost" size="sm" onClick={() => setInserting(false)}>取消</Button>
                <Button size="sm" disabled={insertingSaving} onClick={handleInsertRow}>
                  {insertingSaving ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> : <Plus className="mr-1 h-3.5 w-3.5" />}
                  插入
                </Button>
              </div>
            </DialogContent>
          </Dialog>
        </div>
      ) : subTab === "struct" ? (
        /* 结构 */
        <div className="scrollbar-thin min-h-0 flex-1 overflow-auto p-2">
          {loadingStruct ? (
            <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
              <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> 加载结构...
            </div>
          ) : error ? (
            // 结构视图错误条：内容居中，下方接刷新按钮（重试当前子视图加载）
            <div className="m-2 flex flex-col items-center justify-center gap-3 p-6 text-sm">
              <div className="flex max-w-md items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-4">
                <AlertCircle className="mt-0.5 h-5 w-5 shrink-0 text-destructive" />
                <div className="min-w-0 flex-1">
                  <div className="font-medium text-destructive">加载失败</div>
                  <div className="mt-0.5 break-words text-foreground/80">{error}</div>
                </div>
              </div>
              <Button variant="outline" size="sm" className="gap-1.5" onClick={() => void loadStruct()}>
                <RefreshCw className="h-3.5 w-3.5" /> 刷新
              </Button>
            </div>
          ) : (
            <table className="w-full border-separate border-spacing-0 text-[12px]">
              <thead>
                <tr className="bg-muted text-left">
                  {["列名", "类型", "可空", "主键", "唯一", "索引", "自增", "默认值"].map((h, i) => (
                    <th key={i} className="sticky top-0 z-20 border-b border-r bg-muted px-2 py-1.5 font-medium text-muted-foreground">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {struct.map((col, i) => (
                  <tr key={i} className="border-b">
                    <td className={cn("border-r px-2 py-1 font-medium", i % 2 === 1 && "bg-slate-50")}>{col.name}</td>
                    <td className={cn("border-r px-2 py-1 text-muted-foreground", i % 2 === 1 && "bg-slate-50")}>{col.dataType}</td>
                    <td className={cn("border-r px-2 py-1", i % 2 === 1 && "bg-slate-50")}>{col.nullable ? "是" : "否"}</td>
                    <td className={cn("border-r px-2 py-1", i % 2 === 1 && "bg-slate-50")}>{col.primaryKey ? <TableIcon icon={KeyRound} className="text-amber-500" /> : ""}</td>
                    <td className={cn("border-r px-2 py-1", i % 2 === 1 && "bg-slate-50")}>{col.unique ? <TableIcon icon={ShieldCheck} className="text-blue-500" /> : ""}</td>
                    <td className={cn("border-r px-2 py-1", i % 2 === 1 && "bg-slate-50")}>{col.indexed ? <TableIcon icon={ListOrdered} className="text-muted-foreground/70" /> : ""}</td>
                    <td className={cn("border-r px-2 py-1", i % 2 === 1 && "bg-slate-50")}>{col.autoIncrement ? "✓" : ""}</td>
                    <td className={cn("px-2 py-1 text-muted-foreground", i % 2 === 1 && "bg-slate-50")}>{col.default || "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      ) : (
        /* DDL */
        <div className="scrollbar-thin min-h-0 flex-1 overflow-auto p-2">
          {loadingDDL ? (
            <div className="flex h-full items-center justify-center text-xs text-muted-foreground">
              <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> 加载建表语句...
            </div>
          ) : error ? (
            // DDL 视图错误条：居中展示 + 下方刷新按钮
            <div className="flex flex-1 flex-col items-center justify-center gap-3 p-6 text-sm">
              <div className="flex max-w-md items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-4">
                <AlertCircle className="mt-0.5 h-5 w-5 shrink-0 text-destructive" />
                <div className="min-w-0 flex-1">
                  <div className="font-medium text-destructive">加载失败</div>
                  <div className="mt-0.5 break-words text-foreground/80">{error}</div>
                </div>
              </div>
              <Button variant="outline" size="sm" className="gap-1.5" onClick={() => void loadDDL()}>
                <RefreshCw className="h-3.5 w-3.5" /> 刷新
              </Button>
            </div>
          ) : ddl ? (
            <pre className="whitespace-pre-wrap break-words rounded-md bg-muted/40 p-3 font-mono text-[12px] leading-relaxed">{ddl}</pre>
          ) : (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">未获取到创建语句</div>
          )}
        </div>
      )}
    </div>
  )
}

// 右键行菜单句柄：父组件通过 ref 调用，右键状态只在本组件内更新，
// 避免右键时触发整个表格组件重渲染（大表重渲染是右键弹出慢的根因）
type RowMenuHandle = { show: (cell: { rowIndex: number; colIndex: number | null }) => void; hide: () => void }

const TableBrowserRowMenu = forwardRef<RowMenuHandle, {
  columns: string[]
  rows: unknown[][]
  bigFieldCols: Set<string>
  objType: ObjectDDLType
  selectedRows: Set<string>
  rowKey: (rowIndex: number) => string | null
  page: number
  pageSize: number
  pkColumns: string[]
  onQuickFilter: (col: string, cell: unknown, op: FilterOp) => void
  onDeleteRows: (keys: string[]) => void
}>(({ columns, rows, bigFieldCols, objType, selectedRows, rowKey, page, pageSize, pkColumns, onQuickFilter, onDeleteRows }, ref) => {
  const [cell, setCell] = useState<{ rowIndex: number; colIndex: number | null } | null>(null)
  useImperativeHandle(ref, () => ({
    show: (c) => setCell(c),
    hide: () => setCell(null),
  }))
  const rowIndex = cell?.rowIndex ?? null
  const colIndex = cell?.colIndex ?? null
  return (
    <ContextMenuContent>
      {rowIndex !== null && rows[rowIndex] !== undefined ? (
        (() => {
          const row = rows[rowIndex]
          const ri = rowIndex
          const rk = rowKey(ri)
          const selected = rk !== null && selectedRows.has(rk)
          return (
            <>
              {/* 复制：单元格值（单元格右键）/ 整行 TSV（行右键）/ 列名 */}
              {colIndex !== null && columns[colIndex] !== undefined ? (
                <>
                  <ContextMenuItem onSelect={() => copyToClipboard(copyCellValue(row[colIndex]))}>
                    <Copy className="mr-2 h-3.5 w-3.5" /> 复制单元格值
                  </ContextMenuItem>
                  <ContextMenuItem onSelect={() => copyToClipboard(rowToTSV(row))}>
                    <Copy className="mr-2 h-3.5 w-3.5" /> 复制整行 (TSV)
                  </ContextMenuItem>
                  <ContextMenuItem onSelect={() => copyToClipboard(columns[colIndex])}>
                    <Copy className="mr-2 h-3.5 w-3.5" /> 复制列名
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                </>
              ) : (
                <>
                  <ContextMenuItem onSelect={() => copyToClipboard(rowToTSV(row))}>
                    <Copy className="mr-2 h-3.5 w-3.5" /> 复制整行 (TSV)
                  </ContextMenuItem>
                  <ContextMenuSeparator />
                </>
              )}
              {/* 单元格右键：按此值快速过滤（table 与 view 均可用） */}
              {colIndex !== null && columns[colIndex] !== undefined && (() => {
                const col = columns[colIndex]
                const cellVal = row[colIndex]
                const big = bigFieldCols.has(col)
                const val = cellVal === null || cellVal === undefined ? "NULL" : String(cellVal)
                if (big) {
                  // 大字段列：仅支持为空/非空
                  return (
                    <>
                      <ContextMenuItem onSelect={() => onQuickFilter(col, cellVal, "isNotNull")}>
                        <Filter className="mr-2 h-3.5 w-3.5" /> 筛选：非空
                      </ContextMenuItem>
                      <ContextMenuItem onSelect={() => onQuickFilter(col, cellVal, "isNull")}>
                        <Filter className="mr-2 h-3.5 w-3.5" /> 筛选：为空
                      </ContextMenuItem>
                    </>
                  )
                }
                return (
                  <>
                    <ContextMenuItem onSelect={() => onQuickFilter(col, cellVal, "eq")}>
                      <Filter className="mr-2 h-3.5 w-3.5" /> 等于此值：<span className="ml-1 max-w-[120px] truncate text-muted-foreground">{val}</span>
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => onQuickFilter(col, cellVal, "neq")}>
                      <Filter className="mr-2 h-3.5 w-3.5" /> 不等于此值
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => onQuickFilter(col, cellVal, "contains")}>
                      <Filter className="mr-2 h-3.5 w-3.5" /> 包含此值
                    </ContextMenuItem>
                  </>
                )
              })()}
              {/* 行右键（无列上下文）：复制选中行 + 删除行（仅 table） */}
              {objType === "table" && colIndex === null && (
                <>
                  {selectedRows.size > 0 && (
                    <>
                      <ContextMenuItem onSelect={() => {
                        const selRows = rows.filter((_, i) => {
                          const rk2 = rowKey(i)
                          return rk2 !== null && selectedRows.has(rk2)
                        })
                        copyToClipboard(rowsToTSV(columns, selRows))
                      }}>
                        <Copy className="mr-2 h-3.5 w-3.5" /> 复制选中的 {selectedRows.size} 行 (TSV)
                      </ContextMenuItem>
                      <ContextMenuSeparator />
                    </>
                  )}
                  <ContextMenuItem
                    disabled={pkColumns.length === 0}
                    onSelect={() => onDeleteRows(selected && selectedRows.size > 1 ? Array.from(selectedRows) : rk ? [rk] : [])}
                  >
                    <Trash2 className="mr-2 h-3.5 w-3.5 text-destructive" />
                    {selected && selectedRows.size > 1
                      ? `删除选中的 ${selectedRows.size} 行`
                      : `删除第 ${(page - 1) * pageSize + ri + 1} 行`}
                  </ContextMenuItem>
                </>
              )}
            </>
          )
        })()
      ) : (
        <ContextMenuItem disabled>未选中行</ContextMenuItem>
      )}
    </ContextMenuContent>
  )
})