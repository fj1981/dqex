import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { Bot, FolderCog, FolderOpen, Globe, KeyRound, Languages, Loader2, Monitor, Moon, Plus, RefreshCw, Save, ShieldCheck, Sun, Trash2 } from "lucide-react"
import { useTheme } from "next-themes"
import { Button } from "@/components/ui/button"
import { Card } from "@/components/ui/card"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Textarea } from "@/components/ui/textarea"
import * as api from "@/api"
import PageHeader from "@/components/PageHeader"
import { Section } from "@/components/Section"
import DirectoryPicker from "@/components/DirectoryPicker"
import { changeUILang, tKey } from "@/lib/i18n"
import { SUPPORTED_LANGS } from "@/locales"
import type { AIConfig, AIProvider, AppConfig, ConfigInfo, DirConfig } from "@/types"

// 六类目录的 i18n key（渲染时 t() 翻译）
const DIR_FIELDS: { key: keyof DirConfig; label: string; desc: string }[] = [
  { key: "data", label: "settings.dirData", desc: "settings.dirDataDesc" },
  { key: "tmp", label: "settings.dirTmp", desc: "settings.dirTmpDesc" },
  { key: "uploads", label: "settings.dirUploads", desc: "settings.dirUploadsDesc" },
  { key: "exports", label: "settings.dirExports", desc: "settings.dirExportsDesc" },
  { key: "compares", label: "settings.dirCompares", desc: "settings.dirComparesDesc" },
  { key: "snapshots", label: "settings.dirSnapshots", desc: "settings.dirSnapshotsDesc" },
]

// 左侧导航分区：通用 / 安全 / AI / 兼容
type SettingsTab = "general" | "security" | "ai" | "compat"
const TABS: { key: SettingsTab; label: string; desc: string; icon: typeof FolderCog }[] = [
  { key: "general", label: "settings.general", desc: "settings.generalDesc", icon: FolderCog },
  { key: "security", label: "settings.security", desc: "settings.securityDesc", icon: ShieldCheck },
  { key: "ai", label: "settings.ai", desc: "settings.aiDesc", icon: Bot },
  { key: "compat", label: "settings.compat", desc: "settings.compatDesc", icon: Globe },
]

// 根据 baseUrl 反推厂商 ID（从 API 返回的 providers 列表匹配，未匹配返回 "custom"）
function getProviderIdFromBaseUrl(baseUrl: string, providers: AIProvider[]): string {
  const trimmed = baseUrl.trim().replace(/\/$/, "")
  for (const p of providers) {
    if (p.id !== "custom" && p.baseUrl && p.baseUrl.replace(/\/$/, "") === trimmed) {
      return p.id
    }
  }
  return trimmed ? "custom" : ""
}

// 根据厂商 ID 获取对应的 BaseURL（从 API 返回的 providers 列表查找）
function getBaseUrlFromProviderId(id: string, providers: AIProvider[]): string {
  const p = providers.find((p) => p.id === id)
  return p?.baseUrl ?? ""
}

export default function SettingsView() {
  const { t, i18n } = useTranslation()
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
  // AI 厂商预设
  const [aiProviders, setAiProviders] = useState<AIProvider[]>([])
  // 显示管理厂商对话框
  const [showManageProviders, setShowManageProviders] = useState(false)
  // 管理对话框状态
  const [mgrList, setMgrList] = useState<AIProvider[]>([])
  const [mgrSelectedId, setMgrSelectedId] = useState<string>("")
  const [mgrForm, setMgrForm] = useState({ id: "", name: "", baseUrl: "", models: "" })

  // 根据 baseUrl 反推当前选中的厂商 ID（未匹配返回 "custom"）
  const detectProvider = useCallback((baseUrl: string): string => {
    const trimmed = baseUrl.trim().replace(/\/$/, "")
    for (const p of aiProviders) {
      if (p.id !== "custom" && p.id === getProviderIdFromBaseUrl(trimmed, aiProviders)) {
        return p.id
      }
    }
    return trimmed ? "custom" : ""
  }, [aiProviders])

  const load = useCallback(async () => {
    try {
      const [data, providers] = await Promise.all([
        api.getConfig(),
        api.getAIProviders(),
      ])
      setInfo(data)
      setAiProviders(providers)
      const c: AppConfig = JSON.parse(JSON.stringify(data.config))
      // 补齐 AI 默认值：字段缺失时用默认值，用户显式填 0 仍保留
      c.ai = {
        baseUrl: c.ai?.baseUrl ?? "",
        apiKey: c.ai?.apiKey ?? "",
        model: c.ai?.model ?? "",
        temperature: c.ai?.temperature ?? 0.2,
        maxTokens: c.ai?.maxTokens ?? 2048,
        timeoutSec: c.ai?.timeoutSec ?? 60,
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
      toast.error(t("settings.whitelistDuplicate"))
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
      toast.error(t("settings.saveFailed", { msg: (e as Error).message }))
    } finally {
      setSaving(false)
    }
  }

  const saveGeneral = () => save(t("settings.dirsSaved"))
  const saveSecurity = () => save(t("settings.whitelistSaved"))
  const saveAI = () => save(t("settings.aiSaved"))
  const saveCompat = () => save(t("settings.compatSaved"))

  // ---- 厂商管理对话框 ----
  const mgrModelsToStr = (models: AIProvider["models"]) =>
    (models ?? []).map((m) => (m.maxTokens ? `${m.name}:${m.maxTokens}` : m.name)).join(", ")
  const mgrStrToModels = (s: string) =>
    s.split(",").map((n) => n.trim()).filter(Boolean).map((raw) => {
      const parts = raw.split(":")
      const name = parts[0].trim()
      const maxTokens = parts[1] ? parseInt(parts[1], 10) || 0 : 0
      return { name, context: 0, maxTokens }
    })

  const openMgrDialog = () => {
    setMgrList(aiProviders.map((p) => ({ ...p })))
    const first = aiProviders[0]
    if (first) {
      setMgrSelectedId(first.id)
      setMgrForm({ id: first.id, name: first.name, baseUrl: first.baseUrl, models: mgrModelsToStr(first.models) })
    } else {
      setMgrSelectedId("")
      setMgrForm({ id: "", name: "", baseUrl: "", models: "" })
    }
    setShowManageProviders(true)
  }

  const mgrSelectProvider = (id: string) => {
    setMgrSelectedId(id)
    const p = mgrList.find((x) => x.id === id)
    if (p) setMgrForm({ id: p.id, name: p.name, baseUrl: p.baseUrl, models: mgrModelsToStr(p.models) })
  }

  const mgrApplyForm = () => {
    setMgrList((list) => list.map((p) => (p.id === mgrSelectedId ? { ...p, name: mgrForm.name, baseUrl: mgrForm.baseUrl, models: mgrStrToModels(mgrForm.models) } : p)))
  }

  const mgrAddProvider = () => {
    const id = mgrForm.id.trim()
    if (!id) return
    if (mgrList.some((p) => p.id === id)) {
      toast.error(t("settings.aiProviderIdExists"))
      return
    }
    const newP: AIProvider = { id, name: mgrForm.name || id, baseUrl: mgrForm.baseUrl, models: mgrStrToModels(mgrForm.models) }
    setMgrList((list) => [...list, newP])
    setMgrSelectedId(id)
  }

  const mgrDeleteProvider = (id: string) => {
    const newList = mgrList.filter((p) => p.id !== id)
    setMgrList(newList)
    if (mgrSelectedId === id) {
      const first = newList[0]
      if (first) {
        setMgrSelectedId(first.id)
        setMgrForm({ id: first.id, name: first.name, baseUrl: first.baseUrl, models: mgrModelsToStr(first.models) })
      } else {
        setMgrSelectedId("")
        setMgrForm({ id: "", name: "", baseUrl: "", models: "" })
      }
    }
  }

  const mgrSaveAll = async () => {
    try {
      const items = mgrList.map(({ id, name, baseUrl, models }) => ({ id, name, baseUrl, models: models.map((m) => m.name) }))
      await api.saveAIProviders(items as any)
      toast.success(t("settings.aiProvidersSaved"))
      const refreshed = await api.getAIProviders()
      setAiProviders(refreshed)
      setShowManageProviders(false)
    } catch (e) {
      toast.error((e as Error).message)
    }
  }

  const TAB_ACTIONS: Record<SettingsTab, { save: () => void }> = {
    general: { save: saveGeneral },
    security: { save: saveSecurity },
    ai: { save: saveAI },
    compat: { save: saveCompat },
  }
  const currentAction = TAB_ACTIONS[tab]

  return (
    <div className="mx-auto max-w-4xl space-y-4">
      <PageHeader title={t("settings.title")} description={t("settings.desc")} />

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
                <span className="block truncate font-medium">{tKey(label)}</span>
                <span className={`block truncate text-xs ${tab === key ? "text-primary-foreground/70" : ""}`}>{tKey(desc)}</span>
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
              {t("settings.saveBtn")}
            </Button>
          </div>

          {tab === "general" && (
            <div className="space-y-4">
            <Section title={t("settings.appearance")} description={t("settings.appearanceDesc")}>
              <div className="flex items-center justify-between gap-3 rounded-lg border p-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    {theme === "dark" ? <Moon className="h-4 w-4" /> : theme === "light" ? <Sun className="h-4 w-4" /> : <Monitor className="h-4 w-4" />}
                    {t("settings.themeLabel")}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {t("settings.themeDesc")}
                  </div>
                </div>
                <Select value={theme} onValueChange={setTheme}>
                  <SelectTrigger className="h-8 w-32 shrink-0 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="light">{t("app.light")}</SelectItem>
                    <SelectItem value="dark">{t("app.dark")}</SelectItem>
                    <SelectItem value="system">{t("app.system")}</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </Section>

            <Section title={t("settings.languageSection")} description={t("settings.languageDesc")}>
              <div className="flex items-center justify-between gap-3 rounded-lg border p-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    <Languages className="h-4 w-4" />
                    {t("settings.languageSection")}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {t("settings.languageDesc")}
                  </div>
                </div>
                <Select value={i18n.language} onValueChange={(v) => changeUILang(v)}>
                  <SelectTrigger className="h-8 w-32 shrink-0 text-xs">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {SUPPORTED_LANGS.map((l) => (
                      <SelectItem key={l.code} value={l.code}>
                        {l.label}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </Section>

            <Section title={t("settings.dirs")} description={t("settings.dirsDesc")}>
              <div className="space-y-3">
                {DIR_FIELDS.map(({ key, label, desc }) => {
                  const resolved = info.resolved[key]
                  const isData = key === "data"
                  return (
                    <div key={key} className="space-y-1">
                      <div className="flex items-center justify-between">
                        <label className="flex items-center gap-2 text-sm font-medium">
                          {tKey(label)}
                          {isData && (
                            <span className="rounded bg-muted px-1.5 py-0.5 text-[10px] font-normal text-muted-foreground">
                              {t("settings.dirFixed")}
                            </span>
                          )}
                        </label>
                        <span className="text-xs text-muted-foreground">{tKey(desc)}</span>
                      </div>
                      <div className="flex items-center gap-1.5">
                        <Input
                          value={isData ? resolved : config.dirs[key]}
                          onChange={isData ? undefined : (e) => updateDir(key, e.target.value)}
                          placeholder={resolved || t("settings.dirAutoDerive")}
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
                            title={t("settings.browseDir")}
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

            <Section title={t("settings.log")} description={t("settings.logDesc")}>
              <div className="flex items-center justify-between gap-3 rounded-lg border p-3">
                <div className="min-w-0">
                  <div className="flex items-center gap-2 text-sm font-medium">
                    {t("settings.debugLog")}
                    {config.debug && (
                      <span className="rounded bg-amber-500/15 px-1.5 py-0.5 text-[10px] font-semibold text-amber-600 dark:text-amber-400">
                        {t("settings.enabled")}
                      </span>
                    )}
                  </div>
                  <div className="text-xs text-muted-foreground">
                    {t("settings.debugLogDesc")}
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
              toast.success(t("settings.dirPicked"))
            }}
          />

          {tab === "security" && (
            <Section title={t("settings.whitelist")} description={t("settings.whitelistDesc")}>
              <div className="space-y-2">
                {config.web.allow.length === 0 ? (
                  <div className="flex items-center gap-2 text-sm text-muted-foreground">
                    <Globe className="h-4 w-4" />
                    {t("settings.whitelistEmpty")}
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
                    placeholder={t("settings.whitelistPlaceholder")}
                    className="font-mono text-xs"
                  />
                  <Button variant="outline" onClick={addAllow} disabled={!newAllow.trim()}>
                    <Plus className="mr-1 h-4 w-4" /> {t("settings.whitelistAdd")}
                  </Button>
                </div>
              </div>
            </Section>
          )}

          {tab === "ai" && (
            <Section title={t("settings.aiSection")} description={t("settings.aiSectionDesc")}>
              <div className="space-y-4">
                <div className="flex items-start gap-2 rounded-md border bg-muted/40 p-3 text-xs text-muted-foreground">
                  <Bot className="mt-0.5 h-4 w-4 shrink-0" />
                  <div>
                    {t("settings.aiHint")}
                  </div>
                </div>
                <div className="grid gap-3 sm:grid-cols-2">
                  {/* 厂商选择 */}
                  <div className="space-y-1">
                    <div className="flex items-center justify-between">
                      <label className="text-sm font-medium">{t("settings.aiProvider")}</label>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-6 px-2 text-xs"
                        onClick={openMgrDialog}
                      >
                        {t("settings.aiManageProviders")}
                      </Button>
                    </div>
                    <Select
                      value={detectProvider(config.ai.baseUrl)}
                      onValueChange={(providerId) => {
                        const baseUrl = getBaseUrlFromProviderId(providerId, aiProviders)
                        const provider = aiProviders.find((p) => p.id === providerId)
                        const firstModel = provider?.models?.[0]?.name ?? ""
                        updateAI({ baseUrl, model: firstModel })
                      }}
                    >
                      <SelectTrigger className="text-xs">
                        <SelectValue placeholder={t("settings.aiProviderPlaceholder")} />
                      </SelectTrigger>
                      <SelectContent>
                        {aiProviders.map((p) => (
                          <SelectItem key={p.id} value={p.id}>
                            {p.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                    <div className="text-xs text-muted-foreground">{t("settings.aiProviderHint")}</div>
                  </div>
                  {/* BaseURL（自定义厂商或回退时显示） */}
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiBaseUrl")}</label>
                    <Input
                      value={config.ai.baseUrl}
                      onChange={(e) => updateAI({ baseUrl: e.target.value })}
                      placeholder="https://api.openai.com/v1"
                      className="font-mono text-xs"
                    />
                    <div className="text-xs text-muted-foreground">
                      {t("settings.aiBaseUrlHint")}
                    </div>
                  </div>
                  {/* API Key */}
                  <div className="space-y-1">
                    <label className="flex items-center gap-1 text-sm font-medium">
                      <KeyRound className="h-3.5 w-3.5" /> {t("settings.aiApiKey")}
                    </label>
                    <Input
                      type="password"
                      value={config.ai.apiKey}
                      onChange={(e) => updateAI({ apiKey: e.target.value })}
                      placeholder="sk-..."
                      className="font-mono text-xs"
                    />
                    <div className="text-xs text-muted-foreground">{t("settings.aiApiKeyHint")}</div>
                  </div>
                  {/* 模型选择 */}
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiModel")}</label>
                    {(() => {
                      const currentProviderId = detectProvider(config.ai.baseUrl)
                      const currentProvider = aiProviders.find((p) => p.id === currentProviderId)
                      const models = currentProvider?.models ?? []
                      if (models.length > 0) {
                        return (
                          <>
                            <Select
                              value={config.ai.model}
                              onValueChange={(model) => {
                                const m = models.find((x) => x.name === model)
                                const patch: Partial<AIConfig> = { model }
                                if (m?.maxTokens) patch.maxTokens = m.maxTokens * 1024
                                updateAI(patch)
                              }}
                            >
                              <SelectTrigger className="font-mono text-xs">
                                <SelectValue placeholder={t("settings.aiModelPlaceholder")} />
                              </SelectTrigger>
                              <SelectContent>
                                {models.map((m) => (
                                  <SelectItem key={m.name} value={m.name}>
                                    {m.name}{m.context ? ` (${m.context}K)` : ""}{m.maxTokens ? ` / ${m.maxTokens}K` : ""}
                                  </SelectItem>
                                ))}
                              </SelectContent>
                            </Select>
                            <div className="text-xs text-muted-foreground">{t("settings.aiModelHint")}</div>
                          </>
                        )
                      }
                      return (
                        <>
                          <Input
                            value={config.ai.model}
                            onChange={(e) => updateAI({ model: e.target.value })}
                            placeholder="gpt-4o-mini / deepseek-chat"
                            className="font-mono text-xs"
                          />
                          <div className="text-xs text-muted-foreground">{t("settings.aiModelHint")}</div>
                        </>
                      )
                    })()}
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiTemperature")}</label>
                    <Input
                      type="number"
                      min={0}
                      max={2}
                      step={0.1}
                      value={config.ai.temperature}
                      onChange={(e) => updateAI({ temperature: Number(e.target.value) })}
                      placeholder={t("settings.placeholderDefault", { n: "0.2" })}
                      className="text-xs"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiMaxTokens")}</label>
                    <Input
                      type="number"
                      min={64}
                      step={64}
                      value={config.ai.maxTokens}
                      onChange={(e) => updateAI({ maxTokens: Number(e.target.value) })}
                      placeholder={t("settings.placeholderDefault", { n: "2048" })}
                      className="text-xs"
                    />
                    {(() => {
                      const currentProviderId = detectProvider(config.ai.baseUrl)
                      const currentProvider = aiProviders.find((p) => p.id === currentProviderId)
                      const selectedModel = currentProvider?.models?.find((m) => m.name === config.ai.model)
                      const modelInfo: string[] = []
                      if (selectedModel?.context) modelInfo.push(`上下文窗口 ${selectedModel.context}K（输入 + 输出共享）`)
                      if (selectedModel?.maxTokens) modelInfo.push(`单次回复上限 ${selectedModel.maxTokens}K`)
                      return (
                        <div className="text-xs text-muted-foreground">
                          {t("settings.aiMaxTokensHint")}
                          {modelInfo.length > 0 && <span className="ml-1">· {modelInfo.join(" · ")}</span>}
                        </div>
                      )
                    })()}
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiTimeout")}</label>
                    <Input
                      type="number"
                      min={5}
                      value={config.ai.timeoutSec}
                      onChange={(e) => updateAI({ timeoutSec: Number(e.target.value) })}
                      placeholder={t("settings.placeholderDefault", { n: "60" })}
                      className="text-xs"
                    />
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiMaxSchemaChars")}</label>
                    <Input
                      type="number"
                      min={1000}
                      step={1000}
                      value={config.ai.maxSchemaChars}
                      onChange={(e) => updateAI({ maxSchemaChars: Number(e.target.value) })}
                      placeholder={t("settings.placeholderDefault", { n: "20000" })}
                      className="text-xs"
                    />
                    <div className="text-xs text-muted-foreground">
                      {t("settings.aiMaxSchemaCharsHint")}
                    </div>
                  </div>
                </div>
                <div className="space-y-1">
                  <label className="text-sm font-medium">{t("settings.aiSystemPrompt")}</label>
                  <textarea
                    value={config.ai.systemPrompt}
                    onChange={(e) => updateAI({ systemPrompt: e.target.value })}
                    rows={5}
                    placeholder={t("settings.aiSystemPromptHint")}
                    className="w-full rounded-md border bg-background px-3 py-2 font-mono text-xs outline-none focus:ring-1 focus:ring-ring"
                  />
                </div>
              </div>
            </Section>
          )}

          {tab === "compat" && (
            <Section title={t("settings.compatSection")} description={t("settings.compatSectionDesc")}>
              <div className="flex items-center justify-between rounded-md border bg-background px-3 py-2.5">
                <div>
                  <div className="text-sm font-medium">{t("settings.compatCollation")}</div>
                  <div className="text-xs text-muted-foreground">
                    {t("settings.compatCollationDesc")}
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
              <div>{t("settings.dataDirOverride")}</div>
            </Card>
          )}
        </div>
      </div>

      {/* 管理厂商对话框 */}
      <Dialog open={showManageProviders} onOpenChange={setShowManageProviders}>
        <DialogContent className="sm:max-w-3xl">
          <DialogHeader>
            <DialogTitle>{t("settings.aiManageProviders")}</DialogTitle>
            <DialogDescription>{t("settings.aiManageProvidersDesc")}</DialogDescription>
          </DialogHeader>
          <div className="flex gap-4 py-2" style={{ minHeight: 360 }}>
            {/* 左侧：厂商列表 */}
            <div className="w-48 shrink-0 space-y-1 overflow-y-auto border-r pr-2" style={{ maxHeight: 400 }}>
              {mgrList.map((p) => (
                <div
                  key={p.id}
                  className={`flex cursor-pointer items-center justify-between rounded-md px-2.5 py-2 text-sm transition-colors ${
                    mgrSelectedId === p.id ? "bg-primary text-primary-foreground" : "hover:bg-muted"
                  }`}
                  onClick={() => mgrSelectProvider(p.id)}
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{p.name}</div>
                    <div className={`truncate text-xs ${mgrSelectedId === p.id ? "text-primary-foreground/70" : "text-muted-foreground"}`}>{p.id}</div>
                  </div>
                  {!p.builtin && (
                    <button
                      className={`ml-1 shrink-0 rounded p-0.5 transition-colors ${
                        mgrSelectedId === p.id ? "hover:bg-primary-foreground/20" : "hover:bg-destructive/10 hover:text-destructive"
                      }`}
                      onClick={(e) => { e.stopPropagation(); mgrDeleteProvider(p.id) }}
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  )}
                </div>
              ))}
              {/* 新增入口 */}
              <div className={`rounded-md border px-2.5 py-2 text-sm transition-colors ${
                mgrSelectedId === "__new__" ? "border-primary bg-primary/5" : "border-dashed"
              }`}>
                <button
                  className="flex w-full items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
                  onClick={() => { setMgrSelectedId("__new__"); setMgrForm({ id: "", name: "", baseUrl: "", models: "" }) }}
                >
                  <Plus className="h-3.5 w-3.5" />
                  {t("settings.aiProviderAdd")}
                </button>
              </div>
            </div>
            {/* 右侧：编辑表单 */}
            <div className="flex-1 space-y-3 overflow-y-auto" style={{ maxHeight: 400 }}>
              {mgrSelectedId && mgrSelectedId !== "__new__" && (() => {
                const sel = mgrList.find((p) => p.id === mgrSelectedId)
                return (
                  <>
                    {sel?.builtin && (
                      <div className="rounded-md bg-blue-50 px-3 py-1.5 text-xs text-blue-600 dark:bg-blue-950/30 dark:text-blue-400">
                        {t("settings.aiProviderBuiltin")}
                      </div>
                    )}
                    <div className="space-y-1">
                      <label className="text-sm font-medium">{t("settings.aiProviderName")}</label>
                      <Input value={mgrForm.name} onChange={(e) => setMgrForm((f) => ({ ...f, name: e.target.value }))} className="text-xs" />
                    </div>
                    <div className="space-y-1">
                      <label className="text-sm font-medium">{t("settings.aiBaseUrl")}</label>
                      <Input value={mgrForm.baseUrl} onChange={(e) => setMgrForm((f) => ({ ...f, baseUrl: e.target.value }))} placeholder="https://api.example.com/v1" className="font-mono text-xs" />
                    </div>
                    <div className="space-y-1">
                      <label className="text-sm font-medium">{t("settings.aiProviderModels")}</label>
                      <Textarea
                        value={mgrForm.models}
                        onChange={(e) => setMgrForm((f) => ({ ...f, models: e.target.value }))}
                        placeholder="model-a, model-b:8, model-c:16"
                        className="min-h-[72px] font-mono text-xs"
                        rows={3}
                      />
                      <div className="text-xs text-muted-foreground">{t("settings.aiProviderModelsHint")}</div>
                    </div>
                    <Button variant="outline" size="sm" className="mt-1" onClick={mgrApplyForm}>{t("settings.aiProviderApply")}</Button>
                  </>
                )
              })()}
              {mgrSelectedId === "__new__" && (
                <>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiProviderId")}</label>
                    <Input value={mgrForm.id} onChange={(e) => setMgrForm((f) => ({ ...f, id: e.target.value }))} placeholder="my-provider" className="font-mono text-xs" />
                    <div className="text-xs text-muted-foreground">{t("settings.aiProviderIdHint")}</div>
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiProviderName")}</label>
                    <Input value={mgrForm.name} onChange={(e) => setMgrForm((f) => ({ ...f, name: e.target.value }))} className="text-xs" />
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiBaseUrl")}</label>
                    <Input value={mgrForm.baseUrl} onChange={(e) => setMgrForm((f) => ({ ...f, baseUrl: e.target.value }))} placeholder="https://api.example.com/v1" className="font-mono text-xs" />
                  </div>
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiProviderModels")}</label>
                    <Textarea
                      value={mgrForm.models}
                      onChange={(e) => setMgrForm((f) => ({ ...f, models: e.target.value }))}
                      placeholder="model-a, model-b:8, model-c:16"
                      className="min-h-[72px] font-mono text-xs"
                      rows={3}
                    />
                    <div className="text-xs text-muted-foreground">{t("settings.aiProviderModelsHint")}</div>
                  </div>
                  <Button size="sm" className="mt-1" onClick={mgrAddProvider} disabled={!mgrForm.id.trim()}>
                    <Plus className="mr-1 h-3.5 w-3.5" />
                    {t("settings.aiProviderAdd")}
                  </Button>
                </>
              )}
              {!mgrSelectedId && (
                <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
                  {t("settings.aiProviderSelectHint")}
                </div>
              )}
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowManageProviders(false)}>{t("common.cancel")}</Button>
            <Button onClick={mgrSaveAll}><Save className="mr-1.5 h-3.5 w-3.5" />{t("settings.aiProvidersSave")}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
