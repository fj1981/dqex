import { useCallback, useEffect, useState } from "react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { Bot, FolderCog, FolderOpen, Globe, KeyRound, Languages, Loader2, Monitor, Moon, Plus, RefreshCw, Save, ShieldCheck, Sun, Trash2 } from "lucide-react"
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
import { changeUILang, tKey } from "@/lib/i18n"
import { SUPPORTED_LANGS } from "@/locales"
import type { AIConfig, AppConfig, ConfigInfo, DirConfig } from "@/types"

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
                  <div className="space-y-1 sm:col-span-2">
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
                  <div className="space-y-1">
                    <label className="text-sm font-medium">{t("settings.aiModel")}</label>
                    <Input
                      value={config.ai.model}
                      onChange={(e) => updateAI({ model: e.target.value })}
                      placeholder="gpt-4o-mini / deepseek-chat"
                      className="font-mono text-xs"
                    />
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
                    <label className="text-sm font-medium">{t("settings.aiMaxSchemaTables")}</label>
                    <Input
                      type="number"
                      min={1}
                      value={config.ai.maxSchemaTables}
                      onChange={(e) => updateAI({ maxSchemaTables: Number(e.target.value) })}
                      placeholder={t("settings.placeholderDefault", { n: "30" })}
                      className="text-xs"
                    />
                    <div className="text-xs text-muted-foreground">
                      {t("settings.aiMaxSchemaTablesHint")}
                    </div>
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
    </div>
  )
}
