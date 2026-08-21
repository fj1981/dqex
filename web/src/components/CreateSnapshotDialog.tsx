import { useEffect, useMemo, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { toast } from "sonner"
import { Camera, Database, Info, Loader2, Sparkles, X } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import DbTypeIcon from "@/components/DbTypeIcon"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { Separator } from "@/components/ui/separator"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import * as api from "@/api"
import { useAppStore } from "@/stores/app"
import { tKey } from "@/lib/i18n"

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  onCreated: (id: string) => void
}

// 常用快照用途 — 一键带入名称与备注，便于复用
const SCENE_PRESETS: { key: string; label: string; suffix: string }[] = [
  { key: "pre-release", label: "createSnapshot.scenePreRelease", suffix: "createSnapshot.scenePreRelease" },
  { key: "post-release", label: "createSnapshot.scenePostRelease", suffix: "createSnapshot.scenePostRelease" },
  { key: "daily", label: "createSnapshot.sceneDaily", suffix: "createSnapshot.sceneDaily" },
  { key: "incident", label: "createSnapshot.sceneIncident", suffix: "createSnapshot.sceneIncident" },
  { key: "backup", label: "createSnapshot.sceneBackup", suffix: "createSnapshot.sceneBackup" },
]

// 库名摘要：单库直接返回；多库用 `+` 连接，最多保留 N 个（避免过长）
function summarizeDBs(dbs: string[], max = 3): string {
  if (dbs.length === 0) return "default"
  if (dbs.length <= max) return dbs.join("+")
  return `${dbs.slice(0, max).join("+")}+${dbs.length - max}`
}

// 一日内递增的序号 — 同名重复时自动加序号避免冲突
function nextSeqOfToday(): string {
  const key = `dqex:snapshot:seq:${new Date().toISOString().slice(0, 10)}`
  let seq = 1
  try {
    const raw = localStorage.getItem(key)
    const v = raw ? parseInt(raw, 10) : 0
    seq = Number.isFinite(v) ? v + 1 : 1
    localStorage.setItem(key, String(seq))
  } catch {
    /* localStorage 不可用时退回 1 */
  }
  return seq.toString().padStart(2, "0")
}

// 名称生成器 — 规则：{env}-{库摘要}-{日期}-{序号}
// 例：prod-cy_cli_mgr-20260812-01  /  staging-account+cx-20260812-03
function generateSnapshotName(opts: {
  env: string
  dbs: string[]
  seq?: string
  now?: Date
}): string {
  const { env, dbs, seq, now = new Date() } = opts
  const d = now.toISOString().slice(0, 10).replace(/-/g, "")
  const dbTag = summarizeDBs(dbs)
  return `${env}-${dbTag}-${d}-${seq || "01"}`
}

// 新建快照对话框：选择连接 + 数据库 + 自动命名 + 可选采样
export default function CreateSnapshotDialog({ open, onOpenChange, onCreated }: Props) {
  const { t } = useTranslation()
  const { connections } = useAppStore()
  const [connId, setConnId] = useState("")
  const [selectedDBs, setSelectedDBs] = useState<string[]>([]) // 多选；空=使用连接默认库
  const [name, setName] = useState("")
  const [nameDirty, setNameDirty] = useState(false) // 用户是否手动改过名称（决定自动重新生成时机）
  const [description, setDescription] = useState("")
  const [includeSamples, setIncludeSamples] = useState(false)
  const [sampleRows, setSampleRows] = useState(10) // 采样行数（保留，可扩展）
  const [databases, setDatabases] = useState<string[]>([])
  const [loadingDBs, setLoadingDBs] = useState(false)
  const [submitting, setSubmitting] = useState(false)

  // 初始进入：自动选中第一个连接（历史优先 / 默选连接）
  const initializedRef = useRef(false)
  useEffect(() => {
    if (open && !initializedRef.current) {
      initializedRef.current = true
      if (!connId && connections.length > 0) {
        setConnId(connections[0].id)
      }
    }
    if (!open) {
      // 关闭时重置除"重置后保留选项"以外的状态（关闭=放弃本次输入）
      initializedRef.current = false
    }
  }, [open]) // eslint-disable-line react-hooks/exhaustive-deps

  // 连接变更时重置数据库并加载
  useEffect(() => {
    if (!connId) {
      setDatabases([])
      setSelectedDBs([])
      return
    }
    let cancelled = false
    setLoadingDBs(true)
    api
      .getTableTree(connId)
      .then((res) => {
        if (cancelled) return
        setDatabases(res.databases.map((d) => d.name))
      })
      .catch((e) => {
        if (!cancelled) toast.error(t("createSnapshot.loadDBsFailed", { err: (e as Error).message }))
      })
      .finally(() => {
        if (!cancelled) setLoadingDBs(false)
      })
    return () => {
      cancelled = true
    }
  }, [connId, t])

  const selectedConn = useMemo(() => connections.find((c) => c.id === connId), [connections, connId])
  // 环境标签直接从连接属性读取（用户在录入连接时手动选择），未设置则兜底 prod
  const env = selectedConn?.env || "prod"

  // 自动填充 / 重新生成名称：
  // - 选第一个库、连接变化、点击"重新生成"时触发
  // - 用户手动改过（nameDirty=true）则不覆盖，避免破坏输入
  const regenerateName = (bumpSeq = false) => {
    const seq = bumpSeq || !name ? nextSeqOfToday() : (name.match(/-(\d{2})$/)?.[1] || nextSeqOfToday())
    setName(
      generateSnapshotName({
        env,
        dbs: selectedDBs,
        seq,
      }),
    )
  }

  useEffect(() => {
    if (nameDirty) return
    if (!connId) return
    // 选完库 / 切换连接后自动生成
    if (databases.length === 0 && !loadingDBs) {
      // 还没有库数据：等加载完成
      return
    }
    regenerateName(!name)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedDBs, env, connId, databases.length, loadingDBs])

  const canSubmit = Boolean(connId) && Boolean(name.trim()) && !submitting

  const toggleDB = (db: string) => {
    setSelectedDBs((prev) =>
      prev.includes(db) ? prev.filter((d) => d !== db) : [...prev, db],
    )
  }

  const toggleAllDBs = () => {
    if (selectedDBs.length === databases.length) setSelectedDBs([])
    else setSelectedDBs(databases)
  }

  const onSelectScene = (key: string) => {
    const preset = SCENE_PRESETS.find((p) => p.key === key)
    if (!preset) return
    // 仅当用户还没自定义过名称或描述时填充（防止覆盖）
    if (!nameDirty) regenerateName()
    if (!description.trim()) setDescription(tKey(preset.suffix))
  }

  const doSubmit = async () => {
    if (!canSubmit) return
    setSubmitting(true)
    try {
      const { id } = await api.createSnapshot({
        connId,
        dbNames: selectedDBs,
        name: name.trim(),
        description: description.trim() || undefined,
        includeSamples,
        sampleLimit: includeSamples ? sampleRows : 0, // 未开启采样时显式传 0（走后端默认也行）
      })
      toast.success(t("createSnapshot.created"))
      onCreated(id)
      // 重置可复用输入（连接保留、库选择保留，便于连续创建）
      setNameDirty(false)
      setDescription("")
      setIncludeSamples(false)
      regenerateName(true)
      onOpenChange(false)
    } catch (e) {
      toast.error(t("createSnapshot.createFailed", { err: (e as Error).message }))
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <div className="flex items-center gap-2">
            <Camera className="h-4 w-4 text-primary" />
            <DialogTitle>{t("createSnapshot.title")}</DialogTitle>
            {selectedConn && (
              <DbTypeIcon type={selectedConn.conn.Type} />
            )}
          </div>
          <DialogDescription>
            {t("createSnapshot.desc")}
          </DialogDescription>
        </DialogHeader>

        <div className="grid grid-cols-1 gap-4 md:grid-cols-5">
          {/* 左：连接 + 数据库（约 2/5） */}
          <div className="space-y-3 md:col-span-2">
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">{t("createSnapshot.conn")}</label>
                {selectedConn && (
                  <Badge
                    variant={env === "prod" ? "destructive" : env === "staging" ? "default" : "secondary"}
                    className="px-1.5 py-0 text-[10px] font-normal uppercase"
                    title={t("createSnapshot.envHint", { host: selectedConn?.conn.Host, port: selectedConn?.conn.Port })}
                  >
                    {env}
                  </Badge>
                )}
              </div>
              <Select value={connId} onValueChange={setConnId}>
                <SelectTrigger className="h-9">
                  <SelectValue placeholder={t("createSnapshot.connPlaceholder")} />
                </SelectTrigger>
                <SelectContent>
                  {connections.length === 0 && (
                    <div className="px-2 py-4 text-center text-sm text-muted-foreground">{t("createSnapshot.noConnections")}</div>
                  )}
                  {connections.map((c) => (
                    <SelectItem key={c.id} value={c.id}>
                      <span className="flex items-center gap-2">
                        <DbTypeIcon type={c.conn.Type} />
                        <span className="truncate">{c.name}</span>
                      </span>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {selectedConn && (
                <p className="flex items-center gap-1 text-xs text-muted-foreground">
                  <Info className="h-3 w-3" />
                  {selectedConn.conn.Host}:{selectedConn.conn.Port}
                  {selectedConn.conn.DBName && t("createSnapshot.defaultDB", { db: selectedConn.conn.DBName })}
                  {!selectedConn.conn.DBName && !selectedConn.conn.Service && t("createSnapshot.noDefaultDB")}
                </p>
              )}
            </div>

            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">
                  {t("createSnapshot.databases")}<span className="ml-1 text-xs font-normal text-muted-foreground">{t("createSnapshot.multiSelect")}</span>
                </label>
                {databases.length > 1 && (
                  <Button type="button" variant="ghost" size="sm" className="h-6 px-2 text-xs" onClick={toggleAllDBs}>
                    {selectedDBs.length === databases.length ? t("createSnapshot.selectNone") : t("createSnapshot.selectAll")}
                  </Button>
                )}
              </div>
              <div className="scrollbar-thin max-h-52 space-y-0.5 overflow-y-auto rounded-md border p-1.5">
                {loadingDBs && (
                  <div className="flex items-center gap-2 py-3 text-sm text-muted-foreground">
                    <Loader2 className="h-3.5 w-3.5 animate-spin" /> {t("common.loading")}
                  </div>
                )}
                {!loadingDBs && databases.length === 0 && (
                  <p className="px-1 py-3 text-sm text-muted-foreground">
                    {connId ? t("createSnapshot.noDBs") : t("createSnapshot.needConn")}
                  </p>
                )}
                {databases.map((d) => (
                  <label
                    key={d}
                    className="flex cursor-pointer items-center gap-2 rounded px-2 py-1.5 text-sm hover:bg-accent"
                  >
                    <Checkbox checked={selectedDBs.includes(d)} onCheckedChange={() => toggleDB(d)} />
                    <Database className="h-3 w-3 text-muted-foreground" />
                    <span className="font-mono text-xs">{d}</span>
                  </label>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                {selectedDBs.length === 0
                  ? t("createSnapshot.useDefaultDB")
                  : t("createSnapshot.selectedCount", { n: selectedDBs.length, names: selectedDBs.slice(0, 3).join(", ") + (selectedDBs.length > 3 ? "…" : "") })}
              </p>
            </div>
          </div>

          {/* 右：名称 + 用途 + 选项（约 3/5） */}
          <div className="space-y-3 md:col-span-3">
            <div className="space-y-1.5">
              <div className="flex items-center justify-between">
                <label className="text-sm font-medium">
                  {t("createSnapshot.name")} <span className="text-destructive">*</span>
                </label>
                <Button
                  type="button"
                  variant="ghost"
                  size="sm"
                  className="h-6 px-2 text-xs"
                  onClick={() => {
                    setNameDirty(false)
                    regenerateName(true)
                  }}
                  title={t("createSnapshot.regenerateTitle")}
                >
                  <Sparkles className="mr-1 h-3 w-3" /> {t("createSnapshot.regenerate")}
                </Button>
              </div>
              <div className="relative">
                <Input
                  value={name}
                  onChange={(e) => {
                    setName(e.target.value)
                    setNameDirty(true)
                  }}
                  placeholder="prod-cy_cli_mgr-20260812-01"
                  className="pr-9 font-mono text-sm"
                />
                {name && (
                  <button
                    type="button"
                    onClick={() => {
                      setName("")
                      setNameDirty(false)
                      regenerateName(true)
                    }}
                    className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-muted-foreground hover:bg-accent hover:text-foreground"
                    aria-label={t("createSnapshot.clearName")}
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                )}
              </div>
              <p className="text-xs text-muted-foreground">
                {t("createSnapshot.genRulePre")}<code className="rounded bg-muted px-1 py-0.5 text-[11px]">{t("createSnapshot.genRuleCode")}</code>
                {t("createSnapshot.genRuleSuffix")}
              </p>
            </div>

            <div className="space-y-1.5">
              <label className="text-sm font-medium">{t("createSnapshot.descLabel")}<span className="ml-1 text-xs font-normal text-muted-foreground">{t("createSnapshot.optional")}</span></label>
              <div className="flex flex-wrap gap-1.5">
                {SCENE_PRESETS.map((p) => (
                  <Button
                    key={p.key}
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-7 px-2 text-xs"
                    onClick={() => onSelectScene(p.key)}
                  >
                    {tKey(p.label)}
                  </Button>
                ))}
              </div>
              <Input
                value={description}
                onChange={(e) => setDescription(e.target.value)}
                placeholder={t("createSnapshot.descPlaceholder")}
                className="text-sm"
              />
            </div>

            <Separator />

            {/* 高级 / 数据采样 */}
            <Card className="border-dashed bg-muted/30 p-3">
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0 flex-1 space-y-1">
                  <div className="flex items-center gap-1.5 text-sm font-medium">
                    <Info className="h-3.5 w-3.5 text-muted-foreground" />
                    {t("createSnapshot.sampling")}
                  </div>
                  <p className="text-xs text-muted-foreground">
                    {t("createSnapshot.samplingDescPre")}<span className="font-mono">{sampleRows}</span>{t("createSnapshot.samplingDescPost")}
                  </p>
                </div>
                <Switch
                  checked={includeSamples}
                  onCheckedChange={(v) => setIncludeSamples(v === true)}
                  aria-label={t("createSnapshot.samplingAria")}
                />
              </div>
              {includeSamples && (
                <div className="mt-2 flex items-center gap-2 border-t pt-2">
                  <label className="text-xs text-muted-foreground">{t("createSnapshot.sampleRows")}</label>
                  <Input
                    type="number"
                    min={1}
                    max={100}
                    value={sampleRows}
                    onChange={(e) => setSampleRows(Math.max(1, Math.min(100, parseInt(e.target.value || "10", 10))))}
                    className="h-7 w-20 text-xs"
                  />
                  <span className="text-xs text-muted-foreground">{t("createSnapshot.rowsPerTable")}</span>
                </div>
              )}
            </Card>
          </div>
        </div>

        <DialogFooter className="gap-2">
          <Button variant="outline" onClick={() => onOpenChange(false)} disabled={submitting}>
            {t("common.cancel")}
          </Button>
          <Button onClick={doSubmit} disabled={!canSubmit}>
            {submitting ? (
              <Loader2 className="mr-1 h-4 w-4 animate-spin" />
            ) : (
              <Camera className="mr-1 h-4 w-4" />
            )}
            {submitting ? t("createSnapshot.creating") : t("createSnapshot.create")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
