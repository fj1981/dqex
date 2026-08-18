import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { Bot, FolderCog, FolderOpen, Globe, KeyRound, Loader2, Monitor, Moon, Plus, RefreshCw, Save, ShieldCheck, Sun, Trash2 } from "lucide-react"
import { useTheme } from "next-themes"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import * as api from "@/api"
import PageHeader from "@/components/PageHeader"
import { Section } from "@/components/Section"
import DirectoryPicker from "@/components/DirectoryPicker"
import type { AIConfig, AppConfig, ConfigInfo, DirConfig } from "@/types"

// 六类目录的中文说明与占位符
const DIR_FIELDS: { key: keyof DirConfig; label: string; desc: string }[] = [
  { key: "data", label: "配置保存目录", desc: "连接 / 任务 / 历史（SQLite 数据库）" },
  { key: "tmp", label: "任务临时目录", desc: "任务处理临时文件（zip 解压等，任务结束自动清理）" },
  { key: "uploads", label: "上传临时目录", desc: "Web 导入文件上传目录" },
  { key: "exports", label: "导出产物目录", desc: "导出 zip / 目录" },
  { key: "compares", label: "对比报告目录", desc: "对比报告 JSON" },
  { key: "snapshots", label: "快照目录", desc: "数据库快照数据" },
]

// 左侧导航分区：通用 / 安全 / AI / 兼容
type SettingsTab = "general" | "security" | "ai" | "compat"
const TABS: { key: SettingsTab; label: string; desc: string; icon: typeof FolderCog }[] = [
  { key: "general", label: "通用设置", desc: "数据目录", icon: FolderCog },
  { key: "security", label: "安全访问", desc: "访问来源白名单", icon: ShieldCheck },
  { key: "ai", label: "AI 助手", desc: "大模型与提示词", icon: Bot },
  { key: "compat", label: "兼容选项", desc: "排序规则兼容", icon: Globe },
]

export default function SettingsView() {
  // 主题：浅色 / 深色 / 跟随系统（与 Header 按钮同源，next-themes 持久化）
  const { theme, setTheme } = useTheme()
  const [info, setInfo] = useState<ConfigInfo | null>(null)
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [tab, setTab] = useState<SettingsTab>("general")
  // 编辑中的配置副本
  const [config, setConfig] = useState<AppConfig | null>(null)
  // 目录选择器：pickerDir 为当前正在浏览的目录 key，null = 未打开
  const [pickerDir, setPickerDir] = useState<keyof DirConfig | null>(null)
  // 白名单新增输入框
  const [newAllow, setNewAllow] = useState("")

  const load = useCallback(async () => {
    try {
      const data = await api.getConfig()
      setInfo(data)
      const c: AppConfig = JSON.parse(JSON.stringify(data.config))
      // 补齐 AI 默认值：字段缺失时用默认值，用户显式填 0 仍保留
      c.ai = {
        baseUrl: c.ai?.baseUrl ?? "",
        apiKey: c.ai?.apiKey ?? "",
        model: c.ai?.model ?? "",
        temperature: c.ai?.temperature ?? 0.2,
        maxTokens: c.ai?.maxTokens ?? 2048,
        timeoutSec: c.ai?.timeoutSec ?? 60,
        maxSchemaTables: c.ai?.maxSchemaTables ?? 30,
        maxSchemaChars: c.ai?.maxSchemaChars ?? 20000,
        systemPrompt: c.ai?.systemPrompt ?? "",
      }
      // 全局 debug 默认值
      c.debug = c.debug ?? false
      // 深拷贝编辑副本，避免直接改动返回对象
      setConfig(c)
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

  const updateAI = (patch: Partial<AIConfig>) => {
    setConfig((c) => (c ? { ...c, ai: { ...c.ai, ...patch } } : c))
  }

  const save = async (msg: string) => {
    setSaving(true)
    try {
      await api.saveConfig(config)
      toast.success(msg)
      await load()
    } catch (e) {
      toast.error(`保存失败: ${(e as Error).message}`)
    } finally {
      setSaving(false)
    }
  }

  const saveGeneral = () => save("目录配置已保存")
  const saveSecurity = () => save("白名单已保存")
  const saveAI = () => save("AI 配置已保存")
  const saveCompat = () => save("兼容选项已保存")

  const TAB_ACTIONS: Record<SettingsTab, { save: () => void }> = {
    general: { save: saveGeneral },
    security: { save: saveSecurity },
    ai: { save: saveAI },
    compat: { save: saveCompat },
  }
  const currentAction = TAB_ACTIONS[tab]

  return (
    <div className="mx-auto max-w-4xl space-y-4">
      <PageHeader title="设置" description="全局配置（config.yaml）" />

      <div className="flex flex-col gap-4 md:flex-row">
        {/* 左侧导航 */}
        <nav className="flex shrink-0 gap-1 md:w-44 md:flex-col">
          {TABS.map(({ key, label, desc, icon: Icon }) => (
            <button
              key={key}
              onClick={() => setTab(key)}
              className={`flex items-center gap-2.5 rounded-lg px-3 py-2 text-left text-sm transition-colors ${
                tab === key ? "bg-primary text-primary-foreground" : "text-muted-foreground hover:bg-muted"
              }`}
            >
              <Icon className="h-4 w-4 shrink-0" />
              <span className="min-w-0">
                <span className="block truncate font-medium">{label}</span>
                <span className={`block truncate text-xs ${tab === key ? "text-primary-foreground/70" : ""}`}>{desc}</span>
              </span>
            </button>
          ))}
        </nav>

        {/* 右侧分区内容 */}
        <div className="min-w-0 flex-1 space-y-4">
          {/* 右上角固定保存栏 */}
          <div className="sticky top-0 z-10 flex justify-end border-b bg-background/95 py-2 backdrop-blur">
            <Button onClick={currentAction.save} disabled={saving}>
              {saving ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <Save className="mr-1 h-4 w-4" />}
              保存设置
            </Button>
          </div>

          {tab === "general" && (
            <div className="space-y-4">
            <Section title="外观" description="界面主题，即时生效">
              <div className="flex items-center justify-between gap-3 rounded-lg border p-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    {theme === "dark" ? <Moon className="h-4 w-4" /> : theme === "light" ? <Sun className="h-4 w-4" /> : <Monitor className="h-4 w-4" />}
                    主题
                  </div>
                  <div className="text-xs text-muted-foreground">
                    深色模式降低夜间强光刺激；「跟随系统」随操作系统外观自动切换
                  </div>
                </div>
                <Select value={theme} onValueChange={setTheme}>
                  <SelectTrigger className="h-8 w-32 shrink-0 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="light">浅色</SelectItem>
                    <SelectItem value="dark">深色</SelectItem>
                    <SelectItem value="system">跟随系统</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </Section>

            <Section title="数据目录" description="六类数据存储目录；留空 = 由数据目录自动派生">
              <div className="space-y-3">
                {DIR_FIELDS.map(({ key, label, desc }) => {
                  const resolved = info.resolved[key]
                  const isData = key === "data"
                  return (
                    <div key={key} className="space-y-1">
                      <div className="flex items-center justify-between">
                        <label className="flex items-center gap-2 text-sm font-medium">
                          {label}
                          {isData && (
                            <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-normal text-muted-foreground">
                              运行时固定
                            </span>
                          )}
                        </label>
                        <span className="text-xs text-muted-foreground">{desc}</span>
                      </div>
                      <div className="flex items-center gap-1.5">
                        <Input
                          value={isData ? resolved : config.dirs[key]}
                          onChange={isData ? undefined : (e) => updateDir(key, e.target.value)}
                          placeholder={resolved || "留空 = 自动派生"}
                          disabled={isData}
                          readOnly={isData}
                          title={resolved}
                          className="font-mono text-xs"
                        />
                        {!isData && (
                          <Button
                            type="button"
                            variant="outline"
                            size="icon"
                            className="h-8 w-8 shrink-0"
                            title="浏览选择文件夹"
                            onClick={() => setPickerDir(key)}
                          >
                            <FolderOpen className="h-3.5 w-3.5" />
                          </Button>
                        )}
                      </div>
                    </div>
                  )
                })}
              </div>
            </Section>

            <Section title="日志" description="全局日志级别（debug 及以上）">
              <div className="flex items-center justify-between gap-3 rounded-lg border p-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    调试日志（Debug）
                    {config.debug && (
                      <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-600 dark:text-amber-400">
                        开启
                      </span>
                    )}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    输出 debug 及以上级别的日志（含 AI 请求 / 首字节耗时 / token），便于排查问题；修改后重启服务生效
                  </div>
                </div>
                <Switch
                  checked={config.debug}
                  onCheckedChange={(v) => setConfig((c) => (c ? { ...c, debug: v } : c))}
                />
              </div>
            </Section>
            </div>
          )}

          {/* 目录选择器（本机目录快捷选择，范围限制在主目录内） */}
          <DirectoryPicker
            open={pickerDir !== null}
            onOpenChange={(open) => !open && setPickerDir(null)}
            initialPath={pickerDir ? (config?.dirs[pickerDir] || undefined) : undefined}
            onSelect={(path) => {
              if (pickerDir) updateDir(pickerDir, path)
              setPickerDir(null)
              toast.success("目录已选择，点击右上角保存生效")
            }}
          />

          {tab === "security" && (
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
          )}

          {tab === "ai" && (
            <Section title="AI 辅助 SQL" description="OpenAI 兼容协议（可对接 OpenAI / DeepSeek / 通义等）">
              <div className="space-y-4">
                <div className="flex items-start gap-2 rounded-md border bg-muted/40 p-3 text-xs text-muted-foreground">
                  <Bot className="mt-0.5 h-4 w-4 shrink-0" />
                  <div>
                    AI 助手根据当前连接的表结构生成 / 解释 / 修复 / 优化 SQL，生成结果需人工审核后执行。
                    未填写以下四项必填时，SQL 终端的 AI 助手入口自动隐藏。
                  </div>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  <div className="space-y-1 sm:col-span-2">
                    <label className="text-sm font-medium">Base URL（OpenAI 兼容端点）</label>
                    <Input
                      value={config.ai.baseUrl}
                      onChange={(e) => updateAI({ baseUrl: e.target.value })}
                      placeholder="https://api.openai.com/v1"
                      className="font-mono text-xs"
                    />
                    <div className="text-xs text-muted-foreground">
                      填到版本前缀为止（如 https://api.deepseek.com/v1），勿带 /chat/completions 等具体端点
                    </div>
                  </div>
                  <div className="space-y-1">
                    <label className="flex items-center gap-1 text-sm font-medium">
                      <KeyRound className="h-3.5 w-3.5" /> API Key
                    </label>
                    <Input
                      type="password"
                      value={config.ai.apiKey}
                      onChange={(e) => updateAI({ apiKey: e.target.value })}
                      placeholder="sk-..."
                      className="font-mono text-xs"
                    />
                    <div className="text-xs text-muted-foreground">回显为掩码；未修改时保存不会覆盖原密钥</div>
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">模型</label>
                    <Input
                      value={config.ai.model}
                      onChange={(e) => updateAI({ model: e.target.value })}
                      placeholder="gpt-4o-mini / deepseek-chat"
                      className="font-mono text-xs"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">温度（Temperature）</label>
                    <Input
                      type="number"
                      min={0}
                      max={2}
                      step={0.1}
                      value={config.ai.temperature}
                      onChange={(e) => updateAI({ temperature: Number(e.target.value) })}
                      placeholder="默认 0.2"
                      className="text-xs"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">最大 Token（单次回复）</label>
                    <Input
                      type="number"
                      min={64}
                      step={64}
                      value={config.ai.maxTokens}
                      onChange={(e) => updateAI({ maxTokens: Number(e.target.value) })}
                      placeholder="默认 2048"
                      className="text-xs"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">请求超时（秒）</label>
                    <Input
                      type="number"
                      min={5}
                      value={config.ai.timeoutSec}
                      onChange={(e) => updateAI({ timeoutSec: Number(e.target.value) })}
                      placeholder="默认 60"
                      className="text-xs"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">表结构注入上限（张）</label>
                    <Input
                      type="number"
                      min={1}
                      value={config.ai.maxSchemaTables}
                      onChange={(e) => updateAI({ maxSchemaTables: Number(e.target.value) })}
                      placeholder="默认 30"
                      className="text-xs"
                    />
                    <div className="text-xs text-muted-foreground">
                      注入 AI 上下文的表数量上限；表多时调大，避免模型"看不到"所需表
                    </div>
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">表结构文本上限（字符）</label>
                    <Input
                      type="number"
                      min={1000}
                      step={1000}
                      value={config.ai.maxSchemaChars}
                      onChange={(e) => updateAI({ maxSchemaChars: Number(e.target.value) })}
                      placeholder="默认 20000"
                      className="text-xs"
                    />
                    <div className="text-xs text-muted-foreground">
                      表结构注入文本字符上限（约 5K tokens）；过大可能超出模型上下文窗口
                    </div>
                  </div>
                </div>
                <div className="space-y-1">
                  <label className="text-sm font-medium">System Prompt 模板（可选）</label>
                  <textarea
                    value={config.ai.systemPrompt}
                    onChange={(e) => updateAI({ systemPrompt: e.target.value })}
                    rows={5}
                    placeholder="支持 {dialect}（数据库方言）与 {schema}（表结构）占位符；留空使用内置默认模板"
                    className="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
                  />
                </div>
              </div>
            </Section>
          )}

          {tab === "compat" && (
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
          )}

          {info.dataDirOverride && (
            <Card className="flex items-start gap-2 p-3 text-xs text-amber-600">
              <RefreshCw className="mt-0.5 h-3.5 w-3.5 shrink-0" />
              <div>当前以 --data-dir 启动，data 目录以启动参数为准，config.yaml 中的 dirs.data 不会被使用。</div>
            </Card>
          )}
        </div>
      </div>
    </div>
  )
}
