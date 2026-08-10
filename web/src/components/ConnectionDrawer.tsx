import { useEffect, useState } from "react"
import { toast } from "sonner"
import { Check, Eye, EyeOff, Loader2, PlugZap, Plus, Trash2 } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { ScrollArea } from "@/components/ui/scroll-area"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import * as api from "@/api"
import { useAppStore } from "@/stores/app"
import { cn } from "@/lib/utils"
import { emptyConn, DB_SUBTYPE_LABEL, type DBConn } from "@/types"

const DEFAULT_PORTS: Record<string, number> = {
  mysql: 3306,
  postgresql: 5432,
  oracle: 1521,
}

// 连接管理弹窗：上方为已保存连接列表（点击载入表单），下方为连接配置表单
export default function ConnectionDrawer() {
  const { drawerOpen, closeDrawer, editingConn, connections, dbTypes, saveConnection, removeConnection, loadDBTypes } = useAppStore()
  const [name, setName] = useState("")
  const [conn, setConn] = useState<DBConn>(emptyConn())
  // 当前表单加载的是哪个连接（主键 id 用于更新，name 仅展示）
  const [loadedId, setLoadedId] = useState("")
  const [loadedName, setLoadedName] = useState("")
  const [showPw, setShowPw] = useState(false)
  const [testing, setTesting] = useState(false)
  const [saving, setSaving] = useState(false)

  useEffect(() => {
    if (!drawerOpen) return
    setShowPw(false)
    if (editingConn) {
      setName(editingConn.name)
      setConn({ ...editingConn.conn })
      setLoadedId(editingConn.id)
      setLoadedName(editingConn.name)
    } else {
      setName("")
      setConn(emptyConn())
      setLoadedId("")
      setLoadedName("")
    }
    if (Object.keys(dbTypes).length === 0) loadDBTypes()
  }, [drawerOpen, editingConn]) // eslint-disable-line react-hooks/exhaustive-deps

  const set = (patch: Partial<DBConn>) => setConn((c) => ({ ...c, ...patch }))

  const loadIntoForm = (id: string, n: string, c: DBConn) => {
    setName(n)
    setConn({ ...c })
    setLoadedId(id)
    setLoadedName(n)
  }

  const resetForm = () => {
    setName("")
    setConn(emptyConn())
    setLoadedId("")
    setLoadedName("")
  }

  const changeType = (type: string) => {
    const subTypes = dbTypes[type] || []
    set({
      Type: type,
      SubType: subTypes.includes(conn.SubType || "") ? conn.SubType : subTypes[0] || "",
      Port: DEFAULT_PORTS[type] || conn.Port,
    })
  }

  const doTest = async () => {
    setTesting(true)
    try {
      await api.testConnection({ conn })
      toast.success("连接成功!")
    } catch (e) {
      toast.error(`连接失败: ${(e as Error).message}`)
    } finally {
      setTesting(false)
    }
  }

  const doSave = async () => {
    if (!name.trim()) {
      toast.error("请填写连接名称")
      return
    }
    if (!conn.Host && conn.Type !== "oracle") {
      toast.error("请填写主机地址")
      return
    }
    setSaving(true)
    try {
      await saveConnection(loadedId || undefined, name.trim(), conn)
      toast.success("连接已保存")
      closeDrawer()
    } catch (e) {
      toast.error(`保存失败: ${(e as Error).message}`)
    } finally {
      setSaving(false)
    }
  }

  const doDelete = async (id: string, n: string) => {
    // 删除前确认，防止误点
    if (!window.confirm(`确定删除连接「${n}」吗？`)) return
    try {
      await removeConnection(id)
      toast.success(`已删除连接 ${n}`)
      // 若删除的正是当前正在编辑的连接，清空表单
      if (id === loadedId) resetForm()
    } catch (e) {
      toast.error(`删除失败: ${(e as Error).message}`)
    }
  }

  const subTypes = dbTypes[conn.Type] || []
  const isOracle = conn.Type === "oracle"
  const isNew = loadedId === ""

  return (
    <Dialog open={drawerOpen} onOpenChange={(o) => !o && closeDrawer()}>
      <DialogContent className="sm:max-w-[860px]">
        <DialogHeader>
          <DialogTitle>连接管理</DialogTitle>
        </DialogHeader>

        {/* 左右分栏（Navicat 式）：items-stretch 两列等高，左侧面板拉伸至与右侧表单同高 */}
        <div className="grid grid-cols-[220px_1fr] items-stretch gap-5">
          <div className="flex flex-col overflow-hidden rounded-md border">
            <div className="border-b bg-muted/40 px-3 py-2 text-xs text-muted-foreground">已保存连接</div>
            <ScrollArea className="scrollbar-thin min-h-0 flex-1">
              {connections.length === 0 && (
                <div className="px-3 py-6 text-center text-xs text-muted-foreground">暂无连接，点击下方新建</div>
              )}
              {connections.map((c) => {
                const active = c.id === loadedId
                return (
                  <div
                    key={c.id}
                    role="button"
                    tabIndex={0}
                    onClick={() => loadIntoForm(c.id, c.name, c.conn)}
                    onKeyDown={(e) => e.key === "Enter" && loadIntoForm(c.id, c.name, c.conn)}
                    className={cn(
                      "group flex cursor-pointer items-center justify-between gap-1 border-b px-3 py-2 transition-colors last:border-b-0",
                      active ? "bg-primary/5" : "hover:bg-accent/50",
                    )}
                  >
                    <div className="min-w-0">
                      <div className="flex items-center gap-1.5">
                        <span className={cn("truncate text-sm", active && "font-medium text-primary")}>{c.name}</span>
                        {c.conn.SubType && !c.conn.SubType.toLowerCase().startsWith(c.conn.Type) && (
                          <Badge variant="secondary" className="shrink-0 rounded px-1 py-0 text-[10px] font-normal leading-4">
                            {DB_SUBTYPE_LABEL[c.conn.SubType] || c.conn.SubType}
                          </Badge>
                        )}
                      </div>
                      <div className="truncate text-xs text-muted-foreground">
                        {c.conn.Type} · {c.conn.Host}:{c.conn.Port}
                      </div>
                    </div>
                    <Button
                      variant="ghost"
                      size="icon"
                      className="h-6 w-6 shrink-0 text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100"
                      title="删除连接"
                      onClick={(e) => {
                        e.stopPropagation()
                        doDelete(c.id, c.name)
                      }}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </Button>
                  </div>
                )
              })}
            </ScrollArea>
            <div className="border-t p-2">
              <Button variant="outline" size="sm" className="w-full" onClick={resetForm}>
                <Plus className="mr-1 h-4 w-4" /> 新建连接
              </Button>
            </div>
          </div>

          {/* 连接配置表单：12 列栅格统一各行比例，避免单字段独占半行 */}
          <div className="space-y-4">
            <div className="text-xs text-muted-foreground">{isNew ? "新建连接" : `编辑连接：${loadedName}`}</div>

            {/* 连接名称 + 数据库类型 + 数据库产品（产品从属于类型，切换类型时联动重置） */}
            <div className="grid grid-cols-12 gap-3">
              <div className="col-span-4 space-y-1.5">
                <Label>连接名称</Label>
                <Input value={name} onChange={(e) => setName(e.target.value)} placeholder="如：MySQL-生产" />
              </div>
              <div className="col-span-4 space-y-1.5">
                <Label>数据库类型</Label>
                <Select value={conn.Type} onValueChange={changeType}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {Object.keys(dbTypes).map((t) => (
                      <SelectItem key={t} value={t}>{t}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {subTypes.length > 1 && (
                <div className="col-span-4 space-y-1.5">
                  <Label>数据库产品</Label>
                  <Select value={conn.SubType || ""} onValueChange={(v) => set({ SubType: v })}>
                    <SelectTrigger><SelectValue placeholder="选择产品" /></SelectTrigger>
                    <SelectContent>
                      {subTypes.map((s) => (
                        <SelectItem key={s} value={s}>{DB_SUBTYPE_LABEL[s] || s}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              )}
            </div>

            {/* 主机 + 端口 */}
            <div className="grid grid-cols-12 gap-3">
              <div className="col-span-7 space-y-1.5">
                <Label>主机</Label>
                <Input value={conn.Host} onChange={(e) => set({ Host: e.target.value })} placeholder="127.0.0.1" />
              </div>
              <div className="col-span-5 space-y-1.5">
                <Label>端口</Label>
                <Input
                  type="number"
                  value={conn.Port || ""}
                  onChange={(e) => set({ Port: Number(e.target.value) })}
                />
              </div>
            </div>

            {/* 用户名 + 密码 */}
            <div className="grid grid-cols-12 gap-3">
              <div className="col-span-7 space-y-1.5">
                <Label>用户名</Label>
                <Input value={conn.Un} onChange={(e) => set({ Un: e.target.value })} />
              </div>
              <div className="col-span-5 space-y-1.5">
                <Label>密码</Label>
                <div className="relative">
                  <Input
                    type={showPw ? "text" : "password"}
                    className="pr-9"
                    value={conn.Pw}
                    onChange={(e) => set({ Pw: e.target.value })}
                  />
                  <button
                    type="button"
                    tabIndex={-1}
                    title={showPw ? "隐藏密码" : "显示密码"}
                    className="absolute right-2 top-1/2 -translate-y-1/2 text-muted-foreground hover:text-foreground"
                    onClick={() => setShowPw((v) => !v)}
                  >
                    {showPw ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                </div>
              </div>
            </div>

            {/* 数据库 / Service Name */}
            <div className="space-y-1.5">
              {isOracle ? (
                <>
                  <Label>Service Name</Label>
                  <Input value={conn.Service || ""} onChange={(e) => set({ Service: e.target.value })} placeholder="ORCL" />
                </>
              ) : (
                <>
                  <Label>数据库</Label>
                  <Input
                    value={conn.DBName || ""}
                    onChange={(e) => set({ DBName: e.target.value })}
                    placeholder="库名，可留空（后续按库选择）"
                  />
                </>
              )}
            </div>

            {/* Schema + SSL Mode（仅 PostgreSQL） */}
            {conn.Type === "postgresql" && (
              <div className="grid grid-cols-12 gap-3">
                <div className="col-span-6 space-y-1.5">
                  <Label>Schema</Label>
                  <Input value={conn.Schema || ""} onChange={(e) => set({ Schema: e.target.value })} placeholder="public" />
                </div>
                <div className="col-span-6 space-y-1.5">
                  <Label>SSL Mode</Label>
                  <Select value={conn.SSLMode || "disable"} onValueChange={(v) => set({ SSLMode: v })}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      <SelectItem value="disable">disable</SelectItem>
                      <SelectItem value="require">require</SelectItem>
                      <SelectItem value="verify-ca">verify-ca</SelectItem>
                      <SelectItem value="verify-full">verify-full</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
              </div>
            )}
          </div>
        </div>

        {/* 底部操作区：次要操作靠左，主操作（保存）固定在最右 */}
        <div className="flex items-center justify-between border-t pt-4">
          <Button variant="outline" onClick={doTest} disabled={testing}>
            {testing ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <PlugZap className="mr-1 h-4 w-4" />}
            {testing ? "测试中..." : "测试连接"}
          </Button>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={closeDrawer}>取消</Button>
            <Button onClick={doSave} disabled={saving}>
              {saving ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Check className="mr-1 h-4 w-4" />}
              {saving ? "保存中..." : "保存"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
