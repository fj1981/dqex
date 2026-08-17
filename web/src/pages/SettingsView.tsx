import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { FolderCog, Globe, Loader2, Plus, RefreshCw, Save, ShieldCheck, Trash2 } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Separator } from "@/components/ui/separator"
import { Switch } from "@/components/ui/switch"
import * as api from "@/api"
import PageHeader from "@/components/PageHeader"
import { Section } from "@/components/Section"
import type { AppConfig, ConfigInfo, DirConfig } from "@/types"

// 六类目录的中文说明与占位符
const DIR_FIELDS: { key: keyof DirConfig; label: string; desc: string }[] = [
  { key: "data", label: "配置保存目录", desc: "连接 / 任务 / 历史（SQLite 数据库）" },
  { key: "tmp", label: "任务临时目录", desc: "任务处理临时文件（zip 解压等，任务结束自动清理）" },
  { key: "uploads", label: "上传临时目录", desc: "Web 导入文件上传目录" },
  { key: "exports", label: "导出产物目录", desc: "导出 zip / 目录" },
  { key: "compares", label: "对比报告目录", desc: "对比报告 JSON" },
  { key: "snapshots", label: "快照目录", desc: "数据库快照数据" },
]

export default function SettingsView() {
  const [info, setInfo] = useState<ConfigInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  // 编辑中的配置副本
  const [config, setConfig] = useState<AppConfig | null>(null)
  // 白名单新增输入框
  const [newAllow, setNewAllow] = useState("")

  const load = useCallback(async () => {
    try {
      const data = await api.getConfig()
      setInfo(data)
      // 深拷贝编辑副本，避免直接改动返回对象
      setConfig(JSON.parse(JSON.stringify(data.config)))
    } catch (e) {
      toast.error((e as Error).message)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    load()
  }, [load])

  if (loading || !info || !config) {
    return (
      <div className="flex h-full items-center justify-center">
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    )
  }

  const updateDir = (key: keyof DirConfig, value: string) => {
    setConfig((c) => (c ? { ...c, dirs: { ...c.dirs, [key]: value } } : c))
  }

  const addAllow = () => {
    const v = newAllow.trim()
    if (!v) return
    if (config.web.allow.includes(v)) {
      toast.error("该来源已在白名单中")
      return
    }
    setConfig((c) => (c ? { ...c, web: { ...c.web, allow: [...c.web.allow, v] } } : c))
    setNewAllow("")
  }

  const removeAllow = (item: string) => {
    setConfig((c) => (c ? { ...c, web: { ...c.web, allow: c.web.allow.filter((x) => x !== item) } } : c))
  }

  const save = async () => {
    setSaving(true)
    try {
      await api.saveConfig(config)
      toast.success("配置已保存，重启服务后生效")
      await load()
    } catch (e) {
      toast.error(`保存失败: ${(e as Error).message}`)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="mx-auto max-w-3xl space-y-4">
      <PageHeader
        title="设置"
        description="全局配置（config.yaml），保存后需重启服务生效"
        actions={
          <Button onClick={save} disabled={saving}>
            {saving ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Save className="mr-1 h-4 w-4" />}
            保存配置
          </Button>
        }
      />

      {/* 配置文件位置提示 */}
      <Card className="flex items-center gap-2 p-3 text-sm text-muted-foreground">
        <FolderCog className="h-4 w-4 shrink-0" />
        {info.configFile ? (
          <span className="min-w-0 truncate" title={info.configFile}>
            配置文件：{info.configFile}
          </span>
        ) : (
          <span>未发现配置文件，保存后将写入默认位置 ~/.dbimpex/config.yaml</span>
        )}
      </Card>

      {/* 数据目录 */}
      <Section title="数据目录" description="六类数据存储目录；留空 = 由数据目录自动派生">
        <div className="space-y-3">
          {DIR_FIELDS.map(({ key, label, desc }) => {
            const resolved = info.resolved[key]
            return (
              <div key={key} className="space-y-1">
                <div className="flex items-center justify-between">
                  <label className="text-sm font-medium">{label}</label>
                  <span className="text-xs text-muted-foreground">{desc}</span>
                </div>
                <Input
                  value={config.dirs[key]}
                  onChange={(e) => updateDir(key, e.target.value)}
                  placeholder="留空 = 自动派生"
                  disabled={key === "data" && info.dataDirOverride}
                  className="font-mono text-xs"
                />
                <div className="text-xs text-muted-foreground">
                  实际生效：
                  <span className="font-mono">{resolved || "（派生）"}</span>
                  {key === "data" && info.dataDirOverride && (
                    <span className="ml-1 text-amber-600">（由 --data-dir 启动参数覆盖，此处修改不生效）</span>
                  )}
                </div>
              </div>
            )
          })}
        </div>
      </Section>

      <Separator />

      {/* 访问白名单 */}
      <Section title="访问来源白名单" description="允许访问 Web 的来源（IP / CIDR / 域名）；留空 = 不限制来源，本机回环始终放行">
        <div className="space-y-2">
          {config.web.allow.length === 0 ? (
            <div className="flex items-center gap-2 text-sm text-muted-foreground">
              <Globe className="h-4 w-4" />
              未配置白名单，当前不限制访问来源
            </div>
          ) : (
            <div className="space-y-1.5">
              {config.web.allow.map((item) => (
                <div key={item} className="flex items-center justify-between rounded-md border bg-background px-3 py-1.5">
                  <span className="flex items-center gap-2 font-mono text-sm">
                    <ShieldCheck className="h-3.5 w-3.5 text-green-600" />
                    {item}
                  </span>
                  <Button variant="ghost" size="icon" className="h-6 w-6 text-muted-foreground hover:text-destructive" onClick={() => removeAllow(item)}>
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              ))}
            </div>
          )}
          <div className="flex gap-2">
            <Input
              value={newAllow}
              onChange={(e) => setNewAllow(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && addAllow()}
              placeholder="如 192.168.1.0/24 或 dbx.example.com"
              className="font-mono text-xs"
            />
            <Button variant="outline" onClick={addAllow} disabled={!newAllow.trim()}>
              <Plus className="mr-1 h-4 w-4" /> 添加
            </Button>
          </div>
        </div>
      </Section>

      <Separator />

      {/* 兼容选项 */}
      <Section title="兼容选项" description="MySQL 8.0 特有排序规则（如 utf8mb4_0900_ai_ci）自动替换为 5.7 兼容版本">
        <div className="flex items-center justify-between rounded-md border bg-background px-3 py-2.5">
          <div>
            <div className="text-sm font-medium">兼容排序规则</div>
            <div className="text-xs text-muted-foreground">
              全局默认；可在单个导出 / 导入 / 迁移任务中覆盖
            </div>
          </div>
          <Switch
            checked={config.compatCollation}
            onCheckedChange={(v) => setConfig((c) => (c ? { ...c, compatCollation: v } : c))}
          />
        </div>
      </Section>

      {/* 提示 */}
      <Card className="flex items-start gap-2 p-3 text-xs text-muted-foreground">
        <RefreshCw className="mt-0.5 h-3.5 w-3.5 shrink-0" />
        <div>
          目录与白名单在服务启动时读取，保存后需<b>重启 dbx 服务</b>才会真正生效。
          保存操作仅写入 config.yaml，不影响当前正在运行的服务。
          {info.dataDirOverride && (
            <div className="mt-1 text-amber-600">
              当前以 --data-dir 启动，data 目录以启动参数为准，config.yaml 中的 dirs.data 不会被使用。
            </div>
          )}
        </div>
      </Card>
    </div>
  )
}
