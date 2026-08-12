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
import { Separator } from "@/components/ui/separator"
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

type Env = "prod" | "staging" | "test" | "dev"

const ENV_OPTIONS: { value: Env; label: string; emoji: string; tone: string }[] = [
  { value: "prod",    label: "生产 prod",    emoji: "●", tone: "bg-red-100 text-red-700 border-red-200" },
  { value: "staging", label: "预发 staging", emoji: "●", tone: "bg-blue-100 text-blue-700 border-blue-200" },
  { value: "test",    label: "测试 test",    emoji: "●", tone: "bg-amber-100 text-amber-700 border-amber-200" },
  { value: "dev",     label: "开发 dev",     emoji: "●", tone: "bg-emerald-100 text-emerald-700 border-emerald-200" },
]

function envTone(env?: string) {
  return ENV_OPTIONS.find((e) => e.value === env)?.tone || "bg-muted text-muted-foreground border-transparent"
}

const DEFAULT_PORTS: Record<string, number> = {
  mysql: 3306,
  postgresql: 5432,
  oracle: 1521,
}

// 连接管理弹窗：上方为已保存连接列表（点击载入表单），下方为连接配置表单
export default function ConnectionDrawer() {
  const { drawerOpen, closeDrawer, editingConn, connections, dbTypes, saveConnection, removeConnection, loadDBTypes } = useAppStore()
  const [name, setName] = useState("")
  const [shortName, setShortName] = useState("")
  const [env, setEnv] = useState("")
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
      setShortName(editingConn.shortName || "")
      setEnv(editingConn.env || "")
      setConn({ ...editingConn.conn })
      setLoadedId(editingConn.id)
      setLoadedName(editingConn.name)
    } else {
      setName("")
      setShortName("")
      setEnv("")
      setConn(emptyConn())
      setLoadedId("")
      setLoadedName("")
    }
    if (Object.keys(dbTypes).length === 0) loadDBTypes()
  }, [drawerOpen, editingConn]) // eslint-disable-line react-hooks/exhaustive-deps

  const set = (patch: Partial<DBConn>) => setConn((c) => ({ ...c, ...patch }))

  const loadIntoForm = (id: string, n: string, sn: string, e: string, c: DBConn) => {
    setName(n)
    setShortName(sn)
    setEnv(e)
    setConn({ ...c })
    setLoadedId(id)
    setLoadedName(n)
  }

  const resetForm = () => {
    setName("")
    setShortName("")
    setEnv("")
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
    // 短名校验：仅允许字母、数字、连字符、下划线
    const sn = shortName.trim()
    if (sn && !/^[a-zA-Z0-9_-]+$/.test(sn)) {
      toast.error("短名仅允许字母、数字、连字符、下划线")
      return
    }
    setSaving(true)
    try {
      await saveConnection(loadedId || undefined, name.trim(), sn, env, conn)
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
      <DialogContent className="sm:max-w-[720px] md:max-w-[780px]">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <DialogTitle>连接管理</DialogTitle>
            <span className="text-xs text-muted-foreground">({connections.length})</span>
          </div>
        </DialogHeader>

        {/* 左右分栏（Navicat 式）：左侧列表 + 右侧表单 */}
        <div className="-mx-1 grid grid-cols-[200px_1fr] items-stretch gap-4">
          <div className="flex flex-col overflow-hidden rounded-md border bg-muted/20">
            <div className="border-b bg-muted/40 px-3 py-1.5 text-xs font-medium text-muted-foreground">已保存连接</div>
            <ScrollArea className="scrollbar-thin min-h-0 flex-1">
              {connections.length === 0 && (
                <div className="px-3 py-8 text-center text-xs text-muted-foreground">暂无连接，点击下方新建</div>
              )}
              {connections.map((c) => {
                const active = c.id === loadedId
                return (
                  <div
                    key={c.id}
                    role="button"
                    tabIndex={0}
                    onClick={() => loadIntoForm(c.id, c.name, c.shortName || "", c.env || "", c.conn)}
                    onKeyDown={(e) => e.key === "Enter" && loadIntoForm(c.id, c.name, c.shortName || "", c.env || "", c.conn)}
                    className={cn(
                      "group cursor-pointer border-b px-3 py-2 transition-colors last:border-b-0",
                      active ? "bg-primary/10" : "hover:bg-accent/50",
                    )}
                  >
                    <div className="flex items-center gap-1.5">
                      <span className={cn("h-2 w-2 shrink-0 rounded-full", envTone(c.env).split(" ")[0])} title={c.env || "prod"} />
                      <span className={cn("min-w-0 flex-1 truncate text-sm", active && "font-medium text-primary")}>{c.name}</span>
                      {c.shortName && <span className="shrink-0 rounded bg-muted px-1 text-[10px] text-muted-foreground">{c.shortName}</span>}
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5 shrink-0 text-muted-foreground opacity-0 transition-opacity hover:text-destructive group-hover:opacity-100"
                        title="删除连接"
                        onClick={(e) => {
                          e.stopPropagation()
                          doDelete(c.id, c.name)
                        }}
                      >
                        <Trash2 className="h-3 w-3" />
                      </Button>
                    </div>
                    <div className="mt-0.5 flex items-center gap-1 truncate pl-3.5 text-[11px] text-muted-foreground">
                      <span className="font-mono uppercase">{c.conn.Type}</span>
                      {c.env && <span className="rounded-sm border px-1 text-[9px] uppercase tracking-wide">{c.env}</span>}
                      <span className="truncate">{c.conn.Host}:{c.conn.Port}</span>
                    </div>
                  </div>
                )
              })}
            </ScrollArea>
            <div className="border-t bg-background p-2">
              <Button variant="outline" size="sm" className="w-full" onClick={resetForm}>
                <Plus className="mr-1 h-4 w-4" /> 新建连接
              </Button>
            </div>
          </div>

          {/* 连接配置表单 */}
          <div className="flex min-h-[460px] flex-col">
            {/* 表头：当前模式（新建/编辑）+ 环境标识 */}
            <div className="flex items-center justify-between border-b pb-2">
              <div className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">{isNew ? "新建连接" : "编辑连接"}</span>
                {!isNew && (
                  <span className="font-mono text-xs text-muted-foreground">· {loadedName}</span>
                )}
              </div>
              {(env || isNew) && (
                <span className={cn("inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-[10px] font-medium uppercase", envTone(env))}>
                  {env || "prod"}
                </span>
              )}
            </div>

            {/* 字段区：滚动以避免对话框过高 */}
            <div className="scrollbar-thin min-h-0 flex-1 space-y-4 overflow-y-auto pr-1 pt-3">
              {/* 基础（第一行）：名称 + 短名 + 环境 — 留足宽度避免标签换行/控件截断 */}
              <div className="grid grid-cols-12 gap-3">
                <div className="col-span-5 space-y-1.5">
                  <Label htmlFor="conn-name" className="whitespace-nowrap">连接名称</Label>
                  <Input id="conn-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="如：MySQL-生产" />
                </div>
                <div className="col-span-3 space-y-1.5">
                  <Label htmlFor="conn-short-name" className="whitespace-nowrap">
                    短名
                    <span className="ml-1 text-xs font-normal text-muted-foreground" title="命令行快速引用，仅允许字母数字连字符下划线">可选</span>
                  </Label>
                  <Input
                    id="conn-short-name"
                    value={shortName}
                    onChange={(e) => setShortName(e.target.value.replace(/[^a-zA-Z0-9_-]/g, ""))}
                    placeholder="如 prod"
                    className="font-mono text-sm"
                  />
                </div>
                <div className="col-span-4 space-y-1.5">
                  <Label className="whitespace-nowrap">环境</Label>
                  <Select value={env} onValueChange={setEnv}>
                    <SelectTrigger><SelectValue placeholder="选择环境..." /></SelectTrigger>
                    <SelectContent>
                      {ENV_OPTIONS.map((e) => (
                        <SelectItem
                          key={e.value}
                          value={e.value}
                          // 环境项左侧已经有色点，不需要再预留 Check 图标位置
                          className="pl-2"
                        >
                          <span className="flex items-center gap-2">
                            <span className={cn("inline-block h-2 w-2 rounded-full", e.tone.split(" ")[0])} />
                            {e.label}
                          </span>
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              {/* 基础（第二行）：数据库类型 + 可选产品 — 独占一行避免 Select 截断 */}
              <div className="grid grid-cols-12 gap-3">
                <div className={cn("space-y-1.5", subTypes.length > 1 ? "col-span-6" : "col-span-12")}>
                  <Label className="whitespace-nowrap">数据库类型</Label>
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
                  <div className="col-span-6 space-y-1.5">
                    <Label className="whitespace-nowrap">产品</Label>
                    <Select value={conn.SubType || ""} onValueChange={(v) => set({ SubType: v })}>
                      <SelectTrigger><SelectValue placeholder="选择兼容数据库产品" /></SelectTrigger>
                      <SelectContent>
                        {subTypes.map((s) => (
                          <SelectItem key={s} value={s}>{DB_SUBTYPE_LABEL[s] || s}</SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                )}
              </div>

              <Separator />

              {/* 网络：主机 + 端口 */}
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

              {/* 凭据：用户名 + 密码 */}
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

              <Separator />

              {/* 目标库：默认库可空（留空时使用连接默认库） */}
              <div className="grid grid-cols-12 gap-3">
                <div className="col-span-12 space-y-1.5">
                  {isOracle ? (
                    <>
                      <Label>Service Name</Label>
                      <Input value={conn.Service || ""} onChange={(e) => set({ Service: e.target.value })} placeholder="ORCL" />
                    </>
                  ) : (
                    <>
                      <Label>
                        数据库
                        <span className="ml-1 text-xs font-normal text-muted-foreground">（可选）</span>
                      </Label>
                      <Input
                        value={conn.DBName || ""}
                        onChange={(e) => set({ DBName: e.target.value })}
                        placeholder="库名，留空时按库选择"
                      />
                    </>
                  )}
                </div>
              </div>

              {/* Schema + SSL：始终渲染以保持高度一致，非 PG 时 invisible 占位避免模态框跳动 */}
              <div className={cn("grid grid-cols-12 gap-3", conn.Type !== "postgresql" && "invisible")}>
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
            </div>
          </div>
        </div>

        {/* 底部操作区 */}
        <div className="flex items-center justify-between border-t pt-3">
          <div className="flex items-center gap-2 text-xs text-muted-foreground">
            <PlugZap className="h-3.5 w-3.5" />
            <span>支持 MySQL / PostgreSQL / Oracle</span>
          </div>
          <div className="flex gap-2">
            <Button variant="ghost" onClick={closeDrawer}>取消</Button>
            <Button variant="outline" onClick={doTest} disabled={testing}>
              {testing ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <PlugZap className="mr-1 h-4 w-4" />}
              {testing ? "测试中..." : "测试连接"}
            </Button>
            <Button onClick={doSave} disabled={saving || !name.trim()}>
              {saving ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Check className="mr-1 h-4 w-4" />}
              {saving ? "保存中..." : isNew ? "新建" : "保存"}
            </Button>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
