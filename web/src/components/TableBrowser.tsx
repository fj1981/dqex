import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { AlertCircle, ArrowDown, ArrowUp, ChevronLeft, ChevronRight, ChevronsUpDown, FunctionSquare, Loader2, Pencil, Table2, View } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import CellEditor from "@/components/CellEditor"
import * as api from "@/api"
import { fetchTableData, countTableRows, getObjectDDL, updateTableCell } from "@/api/sql"
import { cn } from "@/lib/utils"
import { computeColWidths } from "@/lib/table"
import type { ObjectDDLType, TableColumn } from "@/types"

interface Props {
  connId: string
  db: string
  name: string
  objType: ObjectDDLType
  subTab: "data" | "struct" | "ddl"
  page: number
  onSubTabChange: (tab: "data" | "struct" | "ddl") => void
  onPageChange: (page: number) => void
}

// 对象类型 → 图标/文案
const TYPE_META: Record<ObjectDDLType, { icon: typeof Table2; label: string; cls: string }> = {
  table: { icon: Table2, label: "表", cls: "text-emerald-600" },
  view: { icon: View, label: "视图", cls: "text-cyan-600" },
  function: { icon: FunctionSquare, label: "函数", cls: "text-violet-600" },
  procedure: { icon: FunctionSquare, label: "存储过程", cls: "text-orange-600" },
}

// 对象浏览器：数据（表/视图）+ 结构（表/视图）+ DDL（全部对象类型）
export default function TableBrowser({ connId, db, name, objType, subTab, page, onSubTabChange, onPageChange }: Props) {
  const [columns, setColumns] = useState<string[]>([])
  const [rows, setRows] = useState<unknown[][]>([])
  const [pageSize] = useState(100)
  const [total, setTotal] = useState(-1)
  const [loadingData, setLoadingData] = useState(false)
  const [struct, setStruct] = useState<TableColumn[]>([])
  const [loadingStruct, setLoadingStruct] = useState(false)
  const [ddl, setDdl] = useState("")
  const [loadingDDL, setLoadingDDL] = useState(false)
  const [error, setError] = useState("") // 当前视图加载错误，内联显示而非 toast
  const totalFetched = useRef(false) // 同一对象实例只统计一次总行数
  const structTableRef = useRef("") // 记录 struct 已加载自哪张表（connId|db|name），切换表时重新加载，避免主键串表

  // 单元格编辑状态
  const [editing, setEditing] = useState<{ rowIndex: number; colIndex: number } | null>(null)
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState("")

  // 排序状态（全局数据库排序）：三态循环 asc → desc → 无
  const [sortColumn, setSortColumn] = useState("")
  const [sortOrder, setSortOrder] = useState<"asc" | "desc">("asc")

  // 列结构（主键+类型）——数据 tab 也需要，用于 UPDATE 定位主键与判断编辑类型
  const colMetaMap = useMemo(() => {
    const m = new Map<string, TableColumn>()
    for (const c of struct) m.set(c.name, c)
    return m
  }, [struct])

  // 主键列名列表（按列顺序）
  const pkColumns = useMemo(() => struct.filter((c) => c.primaryKey).map((c) => c.name), [struct])

  // 按内容自适应列宽（采样前 100 行估算）
  const colWidths = useMemo(() => computeColWidths(columns, rows), [columns, rows])

  const meta = TYPE_META[objType]
  const Icon = meta.icon
  // 表/视图可看数据与结构；函数/存储过程仅 DDL
  const hasData = objType === "table" || objType === "view"

  const loadData = useCallback(async () => {
    if (!connId || !name) return
    setLoadingData(true)
    setError("")
    // 表切换时立即清空 struct，避免竞态期（新表数据已显示、struct 仍为旧表）导致主键串表
    const tableKey = `${connId}|${db}|${name}`
    if (structTableRef.current !== tableKey) {
      setStruct([])
    }
    try {
      const res = await fetchTableData({ connId, db, table: name, page, pageSize, sortColumn: sortColumn || undefined, sortOrder: sortColumn ? sortOrder : undefined })
      setColumns(res.columns)
      setRows(res.rows)
      // 表（非视图）额外加载列结构：拿到主键 + 列类型，支撑单元格编辑。
      // struct 与表绑定（structTableRef 记录来源表），切换表时重新加载，避免主键串表。
      if (objType === "table" && structTableRef.current !== tableKey) {
        structTableRef.current = tableKey
        try {
          const sc = await api.getTableColumns(connId, db, name)
          setStruct(sc.columns)
        } catch {
          // 列结构加载失败不阻塞数据展示，仅禁用编辑
        }
      }
      if (!totalFetched.current) {
        totalFetched.current = true
        try {
          setTotal(await countTableRows(connId, db, name))
        } catch {
          setTotal(-1)
        }
      }
    } catch (e) {
      setError((e as Error).message)
    } finally {
      setLoadingData(false)
    }
  }, [connId, db, name, page, pageSize, objType, sortColumn, sortOrder])

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

  // 表头排序：三态循环（无 → 升序 → 降序 → 无）；排序后重置到第 1 页
  const handleSort = (col: string) => {
    if (sortColumn !== col) {
      setSortColumn(col)
      setSortOrder("asc")
    } else if (sortOrder === "asc") {
      setSortOrder("desc")
    } else {
      setSortColumn("")
      setSortOrder("asc")
    }
    if (page !== 1) onPageChange(1)
  }

  useEffect(() => {
    if (subTab === "data") loadData()
    else if (subTab === "struct") loadStruct()
    else loadDDL()
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [subTab, page, name])

  const pages = total > 0 ? Math.max(1, Math.ceil(total / pageSize)) : -1

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* 对象名 + 类型 + Tab 切换 */}
      <div className="flex shrink-0 items-center gap-2 border-b px-3 py-2">
        <span className="flex h-6 w-6 items-center justify-center rounded-md bg-primary/10 text-primary">
          <Icon className={cn("h-3.5 w-3.5", meta.cls)} />
        </span>
        <span className="truncate font-medium">{name}</span>
        <Badge variant="secondary" className="px-1.5 py-0 text-[10px] font-normal">{meta.label}</Badge>
        {db && <Badge variant="secondary" className="px-1.5 py-0 text-[10px] font-normal">{db}</Badge>}
        <div className="ml-auto flex gap-1">
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
          {loadingData ? (
            <div className="flex flex-1 items-center justify-center text-xs text-muted-foreground">
              <Loader2 className="mr-1.5 h-4 w-4 animate-spin" /> 加载数据...
            </div>
          ) : error ? (
            // 错误条：顶部紧凑展示，左侧图标 + 错误标题 + 详细信息；不占满整个高度
            <div className="m-2 flex shrink-0 items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
              <div className="min-w-0 flex-1">
                <div className="font-medium text-destructive">加载失败</div>
                <div className="mt-0.5 break-words text-foreground/80">{error}</div>
              </div>
            </div>
          ) : rows.length === 0 ? (
            <div className="flex flex-1 items-center justify-center text-sm text-muted-foreground">无数据</div>
          ) : (
            <div className="scrollbar-thin flex-1 overflow-auto rounded-md border">
              <table className="w-full border-collapse text-[12px]" style={{ tableLayout: "fixed" }}>
                <thead className="sticky top-0 z-10">
                  <tr className="bg-muted text-left">
                    {columns.map((c, i) => {
                      const active = sortColumn === c
                      return (
                        <th
                          key={i}
                          className="cursor-pointer select-none border-b border-r px-2 py-1.5 font-medium text-muted-foreground hover:bg-muted/60"
                          style={{ width: `${colWidths[i]}px` }}
                          title={`点击排序：${c}`}
                          onClick={() => handleSort(c)}
                        >
                          <div className="flex items-center gap-1">
                            <span className="min-w-0 flex-1 truncate">{c}</span>
                            {active ? (
                              sortOrder === "asc" ? <ArrowUp className="h-3 w-3 shrink-0 text-primary" /> : <ArrowDown className="h-3 w-3 shrink-0 text-primary" />
                            ) : (
                              <ChevronsUpDown className="h-3 w-3 shrink-0 opacity-0 group-hover:opacity-60" />
                            )}
                          </div>
                        </th>
                      )
                    })}
                  </tr>
                </thead>
                <tbody>
                  {rows.map((row, ri) => (
                    <tr key={ri} className={cn("border-b", ri % 2 === 1 && "bg-muted/30")}>
                      {row.map((cell, ci) => (
                        <td
                          key={ci}
                          className="group/cell cursor-pointer border-r px-2 py-1 hover:bg-primary/5"
                          title={objType === "table" ? "点击编辑" : undefined}
                          onClick={() => objType === "table" && setEditing({ rowIndex: ri, colIndex: ci })}
                        >
                          <div className="flex items-center gap-1.5">
                            <div className="min-w-0 flex-1 truncate" title={cell === null || cell === undefined ? "NULL" : String(cell)}>
                              <span className={cn(cell === null || cell === undefined && "italic text-muted-foreground/70")}>
                                {cell === null || cell === undefined ? "NULL" : String(cell)}
                              </span>
                            </div>
                            {objType === "table" && (
                              <Pencil className="h-3 w-3 shrink-0 text-muted-foreground opacity-0 transition-opacity group-hover/cell:opacity-100" />
                            )}
                          </div>
                        </td>
                      ))}
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          {/* 分页条 */}
          <div className="mt-2 flex shrink-0 items-center justify-between">
            <span className="text-xs tabular-nums text-muted-foreground">
              {total >= 0 ? `共 ${total} 行` : "总行数未知"}
              {total >= 0 && pages > 0 ? ` · ${page} / ${pages}` : ""}
            </span>
            <div className="flex items-center gap-1">
              <Button variant="outline" size="sm" className="h-7 w-7 p-0" disabled={page <= 1 || loadingData} onClick={() => onPageChange(page - 1)}>
                <ChevronLeft className="h-3.5 w-3.5" />
              </Button>
              <Button variant="outline" size="sm" className="h-7 w-7 p-0" disabled={(pages > 0 && page >= pages) || loadingData} onClick={() => onPageChange(page + 1)}>
                <ChevronRight className="h-3.5 w-3.5" />
              </Button>
            </div>
          </div>

          {/* 单元格编辑弹层 */}
          <Dialog open={editing !== null} onOpenChange={(open) => !open && setEditing(null)}>
            <DialogContent className="max-w-[720px]">
              <DialogHeader>
                <DialogTitle>编辑单元格</DialogTitle>
              </DialogHeader>
              {editing !== null && columns[editing.colIndex] !== undefined ? (
                <CellEditor
                  column={columns[editing.colIndex]}
                  dataType={colMetaMap.get(columns[editing.colIndex])?.dataType ?? ""}
                  value={rows[editing.rowIndex]?.[editing.colIndex]}
                  nullable={colMetaMap.get(columns[editing.colIndex])?.nullable ?? true}
                  saving={saving}
                  onSave={handleSaveCell}
                  onCancel={() => setEditing(null)}
                />
              ) : null}
              {saveError && <div className="text-xs text-destructive">{saveError}</div>}
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
            <div className="m-2 flex items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
              <div className="min-w-0 flex-1">
                <div className="font-medium text-destructive">加载失败</div>
                <div className="mt-0.5 break-words text-foreground/80">{error}</div>
              </div>
            </div>
          ) : (
            <table className="w-full border-collapse text-[12px]">
              <thead className="sticky top-0 z-10">
                <tr className="bg-muted text-left">
                  {["列名", "类型", "可空", "主键", "自增", "默认值"].map((h, i) => (
                    <th key={i} className="border-b border-r px-2 py-1.5 font-medium text-muted-foreground">{h}</th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {struct.map((col, i) => (
                  <tr key={i} className={cn("border-b", i % 2 === 1 && "bg-muted/30")}>
                    <td className="border-r px-2 py-1 font-medium">{col.name}</td>
                    <td className="border-r px-2 py-1 text-muted-foreground">{col.dataType}</td>
                    <td className="border-r px-2 py-1">{col.nullable ? "是" : "否"}</td>
                    <td className="border-r px-2 py-1">{col.primaryKey ? "✓" : ""}</td>
                    <td className="border-r px-2 py-1">{col.autoIncrement ? "✓" : ""}</td>
                    <td className="px-2 py-1 text-muted-foreground">{col.default || "—"}</td>
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
            <div className="flex items-start gap-3 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm">
              <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" />
              <div className="min-w-0 flex-1">
                <div className="font-medium text-destructive">加载失败</div>
                <div className="mt-0.5 break-words text-foreground/80">{error}</div>
              </div>
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