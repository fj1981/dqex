import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { CheckCircle2, Database, Loader2, PlugZap, Settings2, XCircle } from "lucide-react"
import DbTypeIcon from "@/components/DbTypeIcon"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
} from "@/components/ui/select"
import * as api from "@/api"
import { useAppStore } from "@/stores/app"
import { cn } from "@/lib/utils"

interface Props {
  title: string
  subtitle?: string
  value: string // 连接主键 id（兼容旧任务配置中的连接名）
  onChange: (id: string) => void
  // 成对布局（迁移/对比页）中启用 h-full + mt-auto 与另一侧等高；
  // 单卡页（导出/导入）必须关闭：flex 父容器会把卡片拉伸，多余高度被 mt-auto 变成中部空白
  fill?: boolean
}

// 连接选择卡片：下拉选择已保存连接 + 连接摘要 + 测试连接 + 管理连接
export default function ConnectionSelect({ title, subtitle, value, onChange, fill }: Props) {
  const { t } = useTranslation()
  const { connections, openDrawer } = useAppStore()
  const [tested, setTested] = useState<Record<string, boolean>>({})
  const [testing, setTesting] = useState(false)

  // 优先按主键匹配；旧任务配置存的是连接名，匹配后归一化为 id
  const selected = connections.find((c) => c.id === value) || connections.find((c) => c.name === value)

  useEffect(() => {
    if (value && selected && selected.id !== value) onChange(selected.id)
  }, [value, selected]) // eslint-disable-line react-hooks/exhaustive-deps

  const doTest = async () => {
    if (!selected) return
    setTesting(true)
    try {
      await api.testConnection({ id: selected.id })
      setTested((prev) => ({ ...prev, [value]: true }))
      toast.success(t("conn.testOk"))
    } catch (e) {
      setTested((prev) => ({ ...prev, [value]: false }))
      toast.error(t("conn.testFailMsg", { msg: (e as Error).message }))
    } finally {
      setTesting(false)
    }
  }

  // min-w-0：grid/flex 子项默认 min-width:auto，内容不可收缩部分会把 track/容器撑宽，
  // 必须显式归零才能让内部 truncate 生效（卡片宽度由布局约束，而非内容）

  // 连接摘要全文：截断后悬停 title 仍可看完整信息（host:port / 库 / service / schema）
  const summary = selected
    ? [`${selected.conn.Host}:${selected.conn.Port}`, selected.conn.DBName, selected.conn.Service, selected.conn.Schema]
        .filter(Boolean)
        .join(" / ")
    : ""

  return (
    <Card className={cn("flex min-w-0 flex-col p-4 transition-colors", fill && "h-full", selected && "border-primary/40")}>
      <div className="mb-3 flex items-center justify-between gap-x-2">
        {/* min-w-0：左侧标题块弹性收缩（按钮 shrink-0），副标题单行 truncate ——
            两侧卡片标题行高度恒定，下拉框/摘要条垂直位置严格对齐，不随文案长度折行变化 */}
        <div className="min-w-0">
          {/* 标题字重降为 medium：中文 semibold 常被合成为粗体，纯黑加粗会抢视觉焦点 */}
          <div className="flex items-center gap-2 text-sm font-medium">
            {/* 图标盒放大至 24px，避免小尺寸图标在紧凑布局中显得被压缩 */}
            <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
              <Database className="h-3.5 w-3.5 shrink-0" />
            </span>
            <span className="truncate">{title}</span>
          </div>
          {/* 副标题单行截断（长文案被按钮挤压时折行会撑高标题区，导致双卡下拉框错位），悬停看全文 */}
          {subtitle && <div className="mt-1 truncate text-xs text-muted-foreground" title={subtitle}>{subtitle}</div>}
        </div>
        {/* 图标式管理入口：文字按钮（英文 Manage Connections 宽达 ~180px）会把副标题挤到折行，
            图标 + title 悬停既保留可发现性又给标题区留足宽度 */}
        <Button
          variant="ghost"
          size="sm"
          className="h-7 w-7 shrink-0 p-0 text-muted-foreground hover:text-foreground"
          title={t("conn.manage")}
          onClick={() => openDrawer()}
        >
          <Settings2 className="h-3.5 w-3.5" />
        </Button>
      </div>

      <Select value={selected?.id} onValueChange={(v) => { onChange(v); setTested({}) }}>
        <SelectTrigger className={cn("h-11", !selected && "text-muted-foreground")}>
          {selected ? (
            // 单行不折行：图标/别名/地址 shrink-0 常驻，名称 flex-1 + truncate 弹性截断；
            // 卡片宽度固定（380px）且高度恒定（h-11），任意长度内容不改变布局
            <span
              className="flex min-w-0 items-center gap-2"
              title={`${selected.name} · ${selected.conn.Host}:${selected.conn.Port}`}
            >
              <DbTypeIcon type={selected.conn.Type} />
              <span className="min-w-0 flex-1 truncate font-medium">{selected.name}</span>
              {selected.shortName && <span className="shrink-0 text-xs text-muted-foreground">({selected.shortName})</span>}
              <span className="shrink-0 text-xs text-muted-foreground">
                {selected.conn.Host}:{selected.conn.Port}
              </span>
            </span>
          ) : (
            <span className="truncate">{t("conn.selectPlaceholder")}</span>
          )}
        </SelectTrigger>
        <SelectContent>
          {connections.length === 0 && (
            <div className="px-2 py-4 text-center text-sm text-muted-foreground">{t("conn.noConnsHint")}</div>
          )}
          {connections.map((c) => (
            <SelectItem
              key={c.id}
              value={c.id}
              // 选中项浅色底 + 对勾，未选中仅悬停高亮
              className="data-[state=checked]:bg-primary/5"
            >
              <span className="flex min-w-0 flex-col gap-0.5">
                <span className="flex min-w-0 items-center gap-2">
                  <DbTypeIcon type={c.conn.Type} />
                  <span className="min-w-0 flex-1 truncate font-medium">{c.name}</span>
                  {c.shortName && <span className="shrink-0 text-xs font-mono text-muted-foreground">({c.shortName})</span>}
                </span>
                <span className="truncate pl-6 font-mono text-xs text-muted-foreground">
                  {c.conn.Host}:{c.conn.Port}
                </span>
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {/* 摘要条固定渲染（未选连接时为占位）：保证各页卡片高度恒定；fill 模式下钉在底部支撑双卡等高 */}
      <div className={cn(fill ? "mt-auto pt-3" : "mt-3")}>
        <div className="flex min-h-11 items-center justify-between gap-x-3 rounded-md bg-muted/50 px-3 py-2">
          {selected ? (
            <>
              <div className="flex min-w-0 items-center gap-x-2 text-sm text-muted-foreground">
                {/* 单行截断 + title 悬停看全文：行高恒定，卡片布局不随内容变化 */}
                <span className="truncate text-xs" title={summary}>{summary}</span>
                {!selected.conn.DBName && !selected.conn.Service && (
                  <span className="shrink-0 whitespace-nowrap text-xs text-amber-600">{t("conn.noDb")}</span>
                )}
              </div>
              <div className="flex shrink-0 items-center gap-2">
                {tested[value] === true && (
                  <CheckCircle2 className="h-4 w-4 shrink-0 text-green-600" />
                )}
                {tested[value] === false && (
                  <XCircle className="h-4 w-4 shrink-0 text-destructive" />
                )}
                <Button variant="outline" size="sm" className="h-7 text-xs" onClick={doTest} disabled={testing}>
                  {testing ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> : <PlugZap className="mr-1 h-3.5 w-3.5" />}
                  {testing ? t("conn.testing") : t("conn.testConn")}
                </Button>
              </div>
            </>
          ) : (
            <span className="truncate text-xs text-muted-foreground">{t("conn.summaryPlaceholder")}</span>
          )}
        </div>
      </div>
    </Card>
  )
}
