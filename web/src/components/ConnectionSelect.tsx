import { useEffect, useState } from "react"
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
}

// 连接选择卡片：下拉选择已保存连接 + 连接摘要 + 测试连接 + 管理连接
export default function ConnectionSelect({ title, subtitle, value, onChange }: Props) {
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
      setTested((t) => ({ ...t, [value]: true }))
      toast.success("连接成功")
    } catch (e) {
      setTested((t) => ({ ...t, [value]: false }))
      toast.error(`连接失败: ${(e as Error).message}`)
    } finally {
      setTesting(false)
    }
  }

  return (
    <Card className={cn("p-4 transition-colors", selected && "border-primary/40")}>
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
          {subtitle && <div className="mt-1 text-xs text-muted-foreground">{subtitle}</div>}
        </div>
        <Button variant="ghost" size="sm" className="h-7 text-xs text-muted-foreground hover:text-foreground" onClick={() => openDrawer()}>
          <Settings2 className="mr-1 h-3.5 w-3.5" /> 管理连接
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
            </span>
          ) : (
            <span>选择数据库连接...</span>
          )}
        </SelectTrigger>
        <SelectContent>
          {connections.length === 0 && (
            <div className="px-2 py-4 text-center text-sm text-muted-foreground">暂无连接，请先「管理连接」新建</div>
          )}
          {connections.map((c) => (
            <SelectItem key={c.id} value={c.id}>
              <span className="flex items-center gap-2">
                <Badge variant="secondary" className="px-1.5 py-0 text-[10px] font-normal uppercase">
                  {c.conn.Type}
                </Badge>
                {c.name}
                <span className="text-xs text-muted-foreground">
                  {c.conn.Host}:{c.conn.Port}
                </span>
              </span>
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {selected && (
        <div className="mt-3 flex items-center justify-between rounded-md bg-muted/50 px-3 py-2">
          <div className="min-w-0 text-sm text-muted-foreground">
            <span className="text-xs">
              {selected.conn.Host}:{selected.conn.Port}
              {selected.conn.DBName ? ` / ${selected.conn.DBName}` : ""}
              {selected.conn.Service ? ` / ${selected.conn.Service}` : ""}
              {selected.conn.Schema ? ` / ${selected.conn.Schema}` : ""}
            </span>
            {!selected.conn.DBName && !selected.conn.Service && (
              <span className="ml-2 text-xs text-amber-600">未指定库</span>
            )}
          </div>
          <div className="flex shrink-0 items-center gap-2">
            {tested[value] === true && (
              <span className="flex items-center gap-1 text-xs font-medium text-green-600">
                <CheckCircle2 className="h-3.5 w-3.5" /> 已连通
              </span>
            )}
            {tested[value] === false && (
              <span className="flex items-center gap-1 text-xs font-medium text-destructive">
                <XCircle className="h-3.5 w-3.5" /> 连接失败
              </span>
            )}
            <Button variant="outline" size="sm" className="h-7 text-xs" onClick={doTest} disabled={testing}>
              {testing ? <Loader2 className="mr-1 h-3.5 w-3.5 animate-spin" /> : <PlugZap className="mr-1 h-3.5 w-3.5" />}
              {testing ? "测试中..." : "测试连接"}
            </Button>
          </div>
        </div>
      )}
    </Card>
  )
}
