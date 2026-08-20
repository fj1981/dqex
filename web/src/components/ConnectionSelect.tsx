import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { CheckCircle2, Database, Loader2, PlugZap, Settings2, XCircle } from "lucide-react"
import { Badge } from "@/components/ui/badge"
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

  return (
    <Card className={cn("flex flex-col p-4 transition-colors", fill && "h-full", selected && "border-primary/40")}>
      <div className="mb-3 flex items-center justify-between">
        <div>
          {/* 标题字重降为 medium：中文 semibold 常被合成为粗体，纯黑加粗会抢视觉焦点 */}
          <div className="flex items-center gap-2 text-sm font-medium">
            {/* 图标盒放大至 24px，避免小尺寸图标在紧凑布局中显得被压缩 */}
            <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md bg-primary/10 text-primary">
              <Database className="h-3.5 w-3.5 shrink-0" />
            </span>
            {title}
          </div>
          {/* 副标题直接完整展示（允许换行），不用 hover 提示 */}
          {subtitle && <div className="mt-1 text-xs text-muted-foreground">{subtitle}</div>}
        </div>
        <Button variant="ghost" size="sm" className="h-7 text-xs text-muted-foreground hover:text-foreground" onClick={() => openDrawer()}>
          <Settings2 className="mr-1 h-3.5 w-3.5" /> {t("conn.manage")}
        </Button>
      </div>

      <Select value={selected?.id} onValueChange={(v) => { onChange(v); setTested({}) }}>
        <SelectTrigger className={cn("h-11", !selected && "text-muted-foreground")}>
          {selected ? (
            <span className="flex items-center gap-2">
              <Badge variant="secondary" className="px-1.5 py-0 text-[10px] font-normal uppercase">
                {selected.conn.Type}
              </Badge>
              <span className="truncate font-medium">{selected.name}</span>
              {selected.shortName && <span className="shrink-0 text-xs text-muted-foreground">({selected.shortName})</span>}
            </span>
          ) : (
            <span>{t("conn.selectPlaceholder")}</span>
          )}
        </SelectTrigger>
        <SelectContent>
          {connections.length === 0 && (
            <div className="px-2 py-4 text-center text-sm text-muted-foreground">{t("conn.noConnsHint")}</div>
          )}
          {connections.map((c) => (
            <SelectItem key={c.id} value={c.id}>
              <span className="flex items-center gap-2">
                <Badge variant="secondary" className="px-1.5 py-0 text-[10px] font-normal uppercase">
                  {c.conn.Type}
                </Badge>
                {c.name}
                {c.shortName && <span className="text-xs text-muted-foreground font-mono">({c.shortName})</span>}
                <span className="text-xs text-muted-foreground">
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
                <span className="truncate text-xs">
                  {selected.conn.Host}:{selected.conn.Port}
                  {selected.conn.DBName ? ` / ${selected.conn.DBName}` : ""}
                  {selected.conn.Service ? ` / ${selected.conn.Service}` : ""}
                  {selected.conn.Schema ? ` / ${selected.conn.Schema}` : ""}
                </span>
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
            <span className="text-xs text-muted-foreground">{t("conn.summaryPlaceholder")}</span>
          )}
        </div>
      </div>
    </Card>
  )
}
