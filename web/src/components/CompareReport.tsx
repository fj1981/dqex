import { useState } from "react"
import { ChevronRight, Database, RotateCcw, Save } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { cn } from "@/lib/utils"
import type { ChangedRow, CompareColumnItem, CompareDatabaseResult, CompareResult, CompareTableResult } from "@/types"

// 汇总过滤项：数值颜色与差异语义对齐，点击切换过滤
const FILTERS = [
  { key: "", label: "全部", cls: "" },
  { key: "matched", label: "一致", cls: "text-green-600" },
  { key: "source_only", label: "仅源有", cls: "text-amber-600" },
  { key: "target_only", label: "仅目标有", cls: "text-blue-600" },
  { key: "structure", label: "结构差异", cls: "text-red-600" },
  { key: "data", label: "数据差异", cls: "text-red-600" },
]

function matchesFilter(t: CompareTableResult, f: string): boolean {
  if (f === "") return true
  if (f === "source_only") return t.status === "source_only"
  if (f === "target_only") return t.status === "target_only"
  if (t.status !== "both") return false
  if (f === "matched") return (t.columns?.matched ?? true) && (t.data?.equal ?? true)
  if (f === "structure") return !!t.columns && !t.columns.matched
  // 跳过类（结构不一致等）不计入数据差异，与后端汇总口径一致
  if (f === "data") return !!t.data && !t.data.equal && t.data.mode !== "skipped"
  return true
}

function fmtVal(v: unknown): string {
  if (v === null || v === undefined) return "NULL"
  if (typeof v === "object") return JSON.stringify(v)
  return String(v)
}

// 单元格预览：XML/BLOB 类大字段截断展示，避免采样表被超长内容撑坏
function cellPreview(v: unknown): string {
  const s = fmtVal(v)
  return s.length > 160 ? `${s.slice(0, 160)}…` : s
}

function colDesc(c: CompareColumnItem): string {
  return `${c.dataType}${c.primaryKey ? " · 主键" : ""}${c.nullable ? "" : " · 非空"}`
}

// 单列各维度的类型/可空/主键差异标记：用于结构差异明细里逐维度高亮不一致项
function colDims(c: CompareColumnItem): { type: string; nullable: string; pk: string } {
  const type = c.normalizedType && c.normalizedType !== c.dataType ? `${c.dataType}(${c.normalizedType})` : c.dataType
  return {
    type,
    nullable: c.nullable ? "可空" : "非空",
    pk: c.primaryKey ? "主键" : "—",
  }
}

// 数据差异摘要：省略零值项，避免“缺失19行/多出0行”这类冗余信息
function tableDataDesc(d: NonNullable<CompareTableResult["data"]>): string {
  if (d.mode === "count") return `行数 ${d.sourceRows} vs ${d.targetRows}`
  if (d.skippedReason) return d.skippedReason
  if (d.equal) return `数据一致 (${d.sourceRows}行)`
  const parts: string[] = []
  if (d.missing) parts.push(`缺失${d.missing}行`)
  if (d.extra) parts.push(`多出${d.extra}行`)
  if (d.changed) parts.push(`变化${d.changed}行`)
  return parts.join(" / ") || "有差异"
}

// 行明细采样表格（缺失/多出各最多 20 条）；colOrder 按源表列定义顺序渲染
function SampleTable({ title, rows, colOrder }: { title: string; rows?: Record<string, unknown>[]; colOrder?: string[] }) {
  if (!rows || rows.length === 0) return null
  // 优先按后端给出的列序渲染；兼容旧数据回退到首行 key 顺序
  const cols = colOrder && colOrder.length > 0 ? colOrder.filter((c) => c in rows[0]) : Object.keys(rows[0])
  return (
    <div className="min-w-0">
      <div className="mb-1 text-xs font-medium text-muted-foreground">
        {title}（{rows.length} 条）
      </div>
      {/* 列宽按内容自适应：短列窄、长文本列封顶截断；w-max+min-w-full 兼顾少列占满与多列横向滚动 */}
      <div className="scrollbar-thin max-h-80 overflow-auto rounded-md border">
        <table className="w-max min-w-full text-xs">
          <thead className="sticky top-0 bg-muted">
            <tr>
              {cols.map((c) => (
                <th key={c} className="whitespace-nowrap px-2 py-1 text-left font-medium">{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((r, i) => (
              <tr key={i} className="border-t">
                {cols.map((c) => (
                  <td key={c} className="max-w-52 truncate px-2 py-1" title={cellPreview(r[c])}>
                    {cellPreview(r[c])}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// 变化行采样表格（PK 模式）：主键取值 + 差异列源/目标对照
function ChangedTable({ rows }: { rows?: ChangedRow[] }) {
  if (!rows || rows.length === 0) return null
  return (
    <div className="min-w-0">
      <div className="mb-1 text-xs font-medium text-muted-foreground">
        主键匹配但内容不同（变化）（{rows.length} 条）
      </div>
      <div className="scrollbar-thin max-h-80 overflow-auto rounded-md border">
        <table className="w-max min-w-full text-xs">
          <thead className="sticky top-0 bg-muted">
            <tr>
              <th className="whitespace-nowrap px-2 py-1 text-left font-medium">主键</th>
              <th className="whitespace-nowrap px-2 py-1 text-left font-medium">差异列</th>
              <th className="whitespace-nowrap px-2 py-1 text-left font-medium">源</th>
              <th className="whitespace-nowrap px-2 py-1 text-left font-medium">目标</th>
            </tr>
          </thead>
          <tbody>
            {rows.flatMap((r, i) =>
              r.diffs.map((d, j) => (
                <tr key={`${i}-${j}`} className="border-t">
                  {j === 0 && (
                    <td rowSpan={r.diffs.length} className="max-w-52 truncate px-2 py-1 align-top font-mono" title={Object.entries(r.key).map(([k, v]) => `${k}=${v}`).join("  ")}>
                      {Object.entries(r.key).map(([k, v]) => `${k}=${cellPreview(v)}`).join("  ")}
                    </td>
                  )}
                  <td className="whitespace-nowrap px-2 py-1 font-mono">{d.column}</td>
                  <td className="max-w-52 truncate px-2 py-1" title={cellPreview(d.source)}>{cellPreview(d.source)}</td>
                  <td className="max-w-52 truncate px-2 py-1" title={cellPreview(d.target)}>{cellPreview(d.target)}</td>
                </tr>
              )),
            )}
          </tbody>
        </table>
      </div>
    </div>
  )
}

// 行状态徽章：列表行与明细弹窗标题共用
// 多库场景下展示限定名：单库保持原表名；同名表展示 db.table；别名表展示 db.src ↔ db.tgt
function qualifiedName(t: CompareTableResult): string {
  const src = t.sourceDB ? `${t.sourceDB}.${t.sourceName || t.name}` : t.sourceName || t.name
  if (t.status !== "both") return src
  const tgt = t.targetDB ? `${t.targetDB}.${t.targetName || t.name}` : t.targetName || t.name
  return src === tgt ? src : `${src} ↔ ${tgt}`
}

function statusBadgeOf(t: CompareTableResult) {
  if (t.status === "source_only") return <Badge variant="secondary" className="bg-amber-500/10 text-amber-700 dark:text-amber-400">仅源有</Badge>
  if (t.status === "target_only") return <Badge variant="secondary" className="bg-blue-500/10 text-blue-700 dark:text-blue-400">仅目标有</Badge>
  if ((t.columns?.matched ?? true) && (t.data?.equal ?? true)) return <Badge variant="secondary" className="bg-green-500/10 text-green-700 dark:text-green-400">一致</Badge>
  return <Badge variant="secondary" className="bg-red-500/10 text-red-700 dark:text-red-400">有差异</Badge>
}

// 单侧独有列块：差异多时默认折叠（仅展示前几项 + 计数），点击展开全部
// 默认折叠阈值：列数超过 10 折叠；阈内直接全部展示，避免少列也要点一下
const COL_ONLY_COLLAPSE_THRESHOLD = 10

function ColumnOnlyBlock({
  label,
  count,
  color,
  columns,
}: {
  label: string
  count: number
  color: "amber" | "blue"
  columns: CompareColumnItem[]
}) {
  const [expanded, setExpanded] = useState(false)
  const collapsed = !expanded && count > COL_ONLY_COLLAPSE_THRESHOLD
  const items = collapsed ? columns.slice(0, COL_ONLY_COLLAPSE_THRESHOLD) : columns
  const colorCls =
    color === "amber"
      ? {
          border: "border-amber-500/30",
          bg: "bg-amber-500/10",
          title: "text-amber-800 dark:text-amber-300",
          name: "text-amber-900 dark:text-amber-200",
          desc: "text-amber-700/70 dark:text-amber-400/70",
        }
      : {
          border: "border-blue-500/30",
          bg: "bg-blue-500/10",
          title: "text-blue-800 dark:text-blue-300",
          name: "text-blue-900 dark:text-blue-200",
          desc: "text-blue-700/70 dark:text-blue-400/70",
        }
  return (
    <div className={cn("rounded-md border px-3 py-2 text-xs", colorCls.border, colorCls.bg)}>
      <button
        type="button"
        className="mb-1.5 flex w-full items-center gap-1.5 text-left"
        onClick={() => count > COL_ONLY_COLLAPSE_THRESHOLD && setExpanded((v) => !v)}
      >
        <span className={cn("font-medium", colorCls.title)}>
          {label}（{count} 列）
        </span>
        {count > COL_ONLY_COLLAPSE_THRESHOLD && (
          <span className="text-muted-foreground">
            {collapsed ? "展开全部" : "收起"}
          </span>
        )}
      </button>
      <div className="flex flex-wrap gap-x-3 gap-y-1">
        {items.map((c) => (
          <span key={c.name} className={cn("font-mono", colorCls.name)}>
            {c.name}
            <span className={cn("ml-1", colorCls.desc)}>({colDesc(c)})</span>
          </span>
        ))}
        {collapsed && (
          <span className="font-mono text-muted-foreground">… 共 {count} 列</span>
        )}
      </div>
    </div>
  )
}

// 表差异明细：弹窗内容，列级差异 + 缺失/多出采样行，内部滚动
function TableDiffDetail({ t }: { t: CompareTableResult }) {
  // 采样表分列：仅当两侧都有数据时才双列，否则单表占满弹窗宽度
  const hasMissing = !!(t.data?.missingSamples && t.data.missingSamples.length > 0)
  const hasExtra = !!(t.data?.extraSamples && t.data.extraSamples.length > 0)
  return (
    <div className="scrollbar-thin max-h-[72vh] space-y-3 overflow-y-auto pr-1">
      {t.status === "source_only" && (
        <div className="rounded-md border border-amber-500/30 bg-amber-500/10 px-3 py-2 text-xs text-amber-800 dark:text-amber-300">
          该表仅存在于源库，目标库中不存在。
        </div>
      )}
      {t.status === "target_only" && (
        <div className="rounded-md border border-blue-500/30 bg-blue-500/10 px-3 py-2 text-xs text-blue-800 dark:text-blue-300">
          该表仅存在于目标库，源库中不存在。
        </div>
      )}
      {t.status === "both" && (t.columns?.matched ?? true) && (t.data?.equal ?? true) && (
        <div className="rounded-md border bg-muted px-3 py-2 text-xs text-muted-foreground">
          结构与数据均一致。
        </div>
      )}
      {t.columns && !t.columns.matched && (
        <div className="space-y-3">
          {t.columns.sourceOnly.length > 0 && (
            <ColumnOnlyBlock
              label="源有目标无"
              count={t.columns.sourceOnly.length}
              color="amber"
              columns={t.columns.sourceOnly}
            />
          )}
          {t.columns.targetOnly.length > 0 && (
            <ColumnOnlyBlock
              label="目标多出"
              count={t.columns.targetOnly.length}
              color="blue"
              columns={t.columns.targetOnly}
            />
          )}
          {t.columns.different.length > 0 && (
            <div className="scrollbar-thin max-h-80 overflow-auto rounded-md border">
              <table className="w-full text-xs">
                <thead className="sticky top-0 z-10 bg-muted">
                  <tr>
                    <th className="sticky left-0 z-10 bg-muted px-2 py-1.5 text-left font-medium">列名</th>
                    <th colSpan={2} className="border-l px-2 py-1.5 text-left font-medium">类型</th>
                    <th colSpan={2} className="border-l px-2 py-1.5 text-left font-medium">可空</th>
                    <th colSpan={2} className="border-l px-2 py-1.5 text-left font-medium">主键</th>
                  </tr>
                  <tr className="bg-muted/60 text-[11px] text-muted-foreground">
                    <th className="sticky left-0 z-10 bg-muted/60 px-2 py-1 text-left font-normal"></th>
                    <th className="border-l px-2 py-1 text-left font-normal">源</th>
                    <th className="px-2 py-1 text-left font-normal">目标</th>
                    <th className="border-l px-2 py-1 text-left font-normal">源</th>
                    <th className="px-2 py-1 text-left font-normal">目标</th>
                    <th className="border-l px-2 py-1 text-left font-normal">源</th>
                    <th className="px-2 py-1 text-left font-normal">目标</th>
                  </tr>
                </thead>
                <tbody>
                  {t.columns.different.map((d) => {
                    const s = colDims(d.source)
                    const tg = colDims(d.target)
                    // 不一致维度红色高亮，一致维度灰化；左右两列横向对比更紧凑易读
                    const cell = (src: string, tgt: string) => (
                      <>
                        <td className={cn("border-l px-2 py-1.5 align-top font-mono", src !== tgt ? "text-red-600 font-medium" : "text-muted-foreground")}>{src}</td>
                        <td className={cn("px-2 py-1.5 align-top font-mono", src !== tgt ? "text-red-600 font-medium" : "text-muted-foreground")}>{tgt}</td>
                      </>
                    )
                    return (
                      <tr key={d.name} className="border-t">
                        <td className="sticky left-0 z-10 bg-background px-2 py-1.5 font-mono align-top font-medium">{d.name}</td>
                        {cell(s.type, tg.type)}
                        {cell(s.nullable, tg.nullable)}
                        {cell(s.pk, tg.pk)}
                      </tr>
                    )
                  })}
                </tbody>
              </table>
              <div className="border-t px-2 py-1.5 text-[11px] text-muted-foreground">
                每维度左右两列分别为源 / 目标；<span className="text-red-600">红色</span>为不一致维度
              </div>
            </div>
          )}
        </div>
      )}

      {t.data && !t.data.equal && (
        <div className="space-y-3">
          {t.data.skippedReason && (
            <div className="text-xs text-muted-foreground">{t.data.skippedReason}</div>
          )}
          {t.data.mode === "count" && (
            <div className="rounded-md border bg-muted px-3 py-2 text-xs text-muted-foreground">
              仅比对行数（超阈值未逐行比对）：源 {t.data.sourceRows} 行，目标 {t.data.targetRows} 行。
            </div>
          )}
          {t.data.mode === "rows" && (
            <div className="text-xs text-muted-foreground">
              {t.data.keyColumns && t.data.keyColumns.length > 0
                ? `按主键 ${t.data.keyColumns.join(",")} 判断有无，内容对比判断变化`
                : "无主键，整行对比（变化会表现为缺失+多出）"}
            </div>
          )}
          {t.data.mode === "rows" && (
            <div className={cn("grid gap-3", hasMissing && hasExtra && "lg:grid-cols-2")}>
              <SampleTable title="源有目标无（缺失）" rows={t.data.missingSamples} colOrder={t.data.sampleColumns} />
              <SampleTable title="目标有源无（多出）" rows={t.data.extraSamples} colOrder={t.data.sampleColumns} />
            </div>
          )}
          {t.data.mode === "rows" && <ChangedTable rows={t.data.changedSamples} />}
        </div>
      )}
    </div>
  )
}

// 对比报告：汇总统计 + 表级结果列表（限高内滚，差异明细弹窗查看）
// 实时对比与快照对比共用此组件，统一报告展示体验
export function CompareReport({ result, onSaveTask, onRestart }: { result: CompareResult; onSaveTask?: () => void; onRestart?: () => void }) {
  const [filter, setFilter] = useState("")
  const [showMatched, setShowMatched] = useState(false)
  const [detail, setDetail] = useState<CompareTableResult | null>(null)
  const s = result.summary

  const counts: Record<string, number> = {
    "": s.total,
    matched: s.matched,
    source_only: s.sourceOnly,
    target_only: s.targetOnly,
    structure: s.structureDiff,
    data: s.dataDiff,
  }

  const toggleDetail = (t: CompareTableResult) => setDetail(t)

  // 多库分组：优先 result.databases，旧数据回退到扁平 result.tables 包装为单组
  const groups: CompareDatabaseResult[] =
    result.databases && result.databases.length > 0
      ? result.databases
      : [{ sourceDB: "", targetDB: "", tables: result.tables, summary: result.summary }]

  const allTables = groups.flatMap((g) => g.tables)
  const visibleTables = allTables.filter((t) => {
    if (!matchesFilter(t, filter)) return false
    // 非过滤模式下默认隐藏完全一致的表（表多时减少无信息量行）
    if (filter === "" && !showMatched && t.status === "both" && (t.columns?.matched ?? true) && (t.data?.equal ?? true)) return false
    return true
  })

  return (
    <Card className="flex min-h-0 flex-1 flex-col gap-3 p-4">
      <div className="flex shrink-0 flex-wrap items-center gap-2">
        <div className="text-sm font-medium">对比报告</div>
        <span className="text-xs text-muted-foreground">
          {result.source} ↔ {result.target}
          {groups.length > 1 ? (
            <span>
              {" "}
              ·{" "}
              {groups
                .map((g) => `${g.sourceDB}${g.targetDB !== g.sourceDB ? `→${g.targetDB}` : ""}`)
                .join(", ")}
            </span>
          ) : groups.length === 1 && groups[0].sourceDB ? (
            <span>
              {" "}
              · {groups[0].sourceDB}
              {groups[0].targetDB !== groups[0].sourceDB ? ` → ${groups[0].targetDB}` : ""}
            </span>
          ) : null}
          {" · 对比时点快照，期间数据变动可能影响结果"}
        </span>
        <div className="ml-auto flex items-center gap-2">
          {onSaveTask && (
            <Button variant="outline" size="sm" className="h-7 text-xs" onClick={onSaveTask}>
              <Save className="mr-1 h-3.5 w-3.5" /> 保存为任务配置
            </Button>
          )}
          {onRestart && (
            <Button size="sm" className="h-7 text-xs" onClick={onRestart}>
              <RotateCcw className="mr-1 h-3.5 w-3.5" /> 重新开始
            </Button>
          )}
        </div>
      </div>

      {/* 汇总统计卡片：与进度页 StatBlock 风格对齐，点击切换过滤；零计数灰化 */}
      <div className="grid shrink-0 grid-cols-3 gap-2 md:grid-cols-6">
        {FILTERS.map(({ key, label, cls }) => (
          <button
            key={key}
            type="button"
            className={cn(
              "rounded-md border px-2.5 py-2 text-left transition-colors",
              filter === key ? "border-primary bg-primary/10" : "bg-muted/30 hover:bg-accent",
              counts[key] === 0 && filter !== key && "opacity-50",
            )}
            onClick={() => setFilter(key)}
          >
            <div className="truncate text-xs text-muted-foreground">{label}</div>
            <div className={cn("mt-0.5 text-base font-medium tabular-nums", cls)}>
              {counts[key]}
            </div>
          </button>
        ))}
      </div>

      {/* 一致项显示开关：默认只看差异，表多时列表更短 */}
      <div className="flex shrink-0 items-center justify-between">
        <span className="text-xs text-muted-foreground">
          {filter === "" && !showMatched
            ? `仅显示有差异的表（${visibleTables.length}）；一致 ${s.matched} 项已隐藏`
            : `共 ${visibleTables.length} 项`}
        </span>
        <label className="flex cursor-pointer select-none items-center gap-1.5 text-xs text-muted-foreground">
          <Checkbox checked={showMatched} onCheckedChange={(v) => setShowMatched(v === true)} />
          显示一致项
        </label>
      </div>

      {/* 表列表内部滚动：上方统计/过滤固定不动；多库对比按库分组折叠 */}
      <div className="scrollbar-thin min-h-0 flex-1 space-y-3 overflow-y-auto pr-1">
        {visibleTables.length === 0 && (
          <div className="py-6 text-center text-xs text-muted-foreground">无符合条件的表</div>
        )}
        {groups.map((g, gi) => {
          const gtables = g.tables.filter((t) => {
            if (!matchesFilter(t, filter)) return false
            if (filter === "" && !showMatched && t.status === "both" && (t.columns?.matched ?? true) && (t.data?.equal ?? true)) return false
            return true
          })
          if (gtables.length === 0) return null
          return (
            <div key={`${g.sourceDB}-${gi}`} className="space-y-1.5">
              {groups.length > 1 && (
                <div className="sticky top-0 z-10 -mx-1 flex items-center gap-2 bg-background/95 px-1 py-1 text-xs font-medium text-muted-foreground backdrop-blur">
                  <Database className="h-3.5 w-3.5" />
                  <span className="font-mono">
                    {g.sourceDB ? g.sourceDB : "(库)"}
                    {g.targetDB && g.targetDB !== g.sourceDB && (
                      <span className="text-muted-foreground"> → {g.targetDB}</span>
                    )}
                  </span>
                  <span className="ml-auto tabular-nums">{gtables.length} 项</span>
                </div>
              )}
              {gtables.map((t, ti) => {
                // 凡是被标记为“有差异”或整表缺失/多出的表都可点开查看明细
                // 对应 statusBadgeOf 的“有差异”判定，避免徽章显示有差异却点不开
                const hasDiff =
                  t.status !== "both" ||
                  !((t.columns?.matched ?? true) && (t.data?.equal ?? true))
                const hasDetail = hasDiff
                const qname = qualifiedName(t)
                return (
                  <div key={`${g.sourceDB}-${g.targetDB}-${t.name}-${ti}`} className="rounded-md border bg-background">
                    <button
                      type="button"
                      className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm"
                      onClick={() => hasDetail && toggleDetail(t)}
                    >
                      {hasDetail ? (
                        <ChevronRight className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                      ) : (
                        <span className="w-3.5 shrink-0" />
                      )}
                      <span className="min-w-0 truncate font-mono text-xs" title={qname}>{qname}</span>
                      {statusBadgeOf(t)}
                      <span className="ml-auto flex shrink-0 items-center gap-2 text-xs text-muted-foreground">
                        {t.columns && (
                          <span>
                            {t.columns.matched
                              ? "结构一致"
                              : `结构: +${t.columns.sourceOnly.length} -${t.columns.targetOnly.length} ±${t.columns.different.length}`}
                          </span>
                        )}
                        {t.data && (
                          <span className="tabular-nums">{tableDataDesc(t.data)}</span>
                        )}
                      </span>
                    </button>
                  </div>
                )
              })}
            </div>
          )
        })}
      </div>

      {/* 差异明细弹窗：宽幅展示，采样表与列差异可容纳更多数据 */}
      <Dialog open={!!detail} onOpenChange={(o) => !o && setDetail(null)}>
        <DialogContent className="max-w-5xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2 font-mono text-base">
              {detail && qualifiedName(detail)}
              {detail && statusBadgeOf(detail)}
            </DialogTitle>
            {detail && (
              <DialogDescription>
                {[
                  detail.columns
                    ? detail.columns.matched
                      ? "结构一致"
                      : `结构差异 +${detail.columns.sourceOnly.length} -${detail.columns.targetOnly.length} ±${detail.columns.different.length}`
                    : "",
                  detail.data ? tableDataDesc(detail.data) : "",
                ].filter(Boolean).join(" · ")}
              </DialogDescription>
            )}
          </DialogHeader>
          {detail && <TableDiffDetail t={detail} />}
        </DialogContent>
      </Dialog>
    </Card>
  )
}
