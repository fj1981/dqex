import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { Calendar, ChevronDown, Clock, Database, Loader2 } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogTitle,
} from "@/components/ui/dialog"
import { Separator } from "@/components/ui/separator"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import * as api from "@/api"
import type { VersionInfo } from "@/types"

// 解析 CHANGELOG markdown，按 ## [version] 拆分为版本段落
interface ChangelogVersion {
  version: string
  date: string
  content: string
}

function parseChangelog(md: string): ChangelogVersion[] {
  const regex = /^## \[(.+?)\](?:\s*-\s*(.+))?$/gm
  const matches: { version: string; date: string; index: number; headerEnd: number }[] = []
  let m: RegExpExecArray | null
  while ((m = regex.exec(md)) !== null) {
    matches.push({
      version: m[1].trim(),
      date: (m[2] ?? "").trim(),
      index: m.index,
      headerEnd: m.index + m[0].length,
    })
  }
  return matches.map((item, i) => {
    const nextStart = i + 1 < matches.length ? matches[i + 1].index : md.length
    const content = md.slice(item.headerEnd, nextStart).trim()
    return { version: item.version, date: item.date, content }
  })
}

// 关于弹窗：品牌展示 + 版本信息 + 更新日志（手风琴）
export default function AboutDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t, i18n } = useTranslation()
  const [version, setVersion] = useState<VersionInfo | null>(null)
  const [changelog, setChangelog] = useState<ChangelogVersion[]>([])
  const [changelogLoading, setChangelogLoading] = useState(false)
  const [changelogError, setChangelogError] = useState(false)
  const [expandedIdx, setExpandedIdx] = useState<number | null>(null)
  // 已加载语言：切换语言后清空缓存，重新按新语言拉取更新日志
  const [changelogLang, setChangelogLang] = useState(i18n.language)

  useEffect(() => {
    if (i18n.language === changelogLang) return
    setChangelogLang(i18n.language)
    setChangelog([])
    setChangelogError(false)
    setExpandedIdx(null)
  }, [i18n.language, changelogLang])

  useEffect(() => {
    if (!open) return
    let cancelled = false
    api.getVersion()
      .then((v) => { if (!cancelled) setVersion(v) })
      .catch(() => {})
    return () => { cancelled = true }
  }, [open])

  const loadChangelog = () => {
    if (changelog.length > 0 || changelogLoading) return
    setChangelogLoading(true)
    setChangelogError(false)
    api.getChangelog()
      .then((res) => {
        const versions = parseChangelog(res.content)
        setChangelog(versions)
        // 默认展开最新版本
        setExpandedIdx(0)
      })
      .catch(() => setChangelogError(true))
      .finally(() => setChangelogLoading(false))
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[460px] h-[420px]">
        <DialogTitle className="sr-only">{t("about.title")}</DialogTitle>

        <Tabs defaultValue="about" className="w-full">
          <TabsList className="mx-auto w-auto shrink-0">
            <TabsTrigger value="about" className="px-5">{t("about.title")}</TabsTrigger>
            <TabsTrigger value="changelog" className="px-5" onClick={loadChangelog}>{t("about.changelog")}</TabsTrigger>
          </TabsList>

          {/* 关于 Tab：品牌区居上居中，版本信息贴底 */}
          <TabsContent value="about" className="mt-3">
            <div className="flex h-[320px] flex-col">
              {/* 品牌区：上方视觉中心 */}
              <div className="flex flex-1 flex-col items-center justify-center gap-2">
                <span className="flex h-14 w-14 shrink-0 items-center justify-center rounded-xl bg-primary text-primary-foreground">
                  <Database className="h-7 w-7" />
                </span>
                <div className="text-base font-medium">{t("about.brand")}</div>
                <div className="text-xs text-muted-foreground">{t("about.tagline")}</div>
              </div>

              <Separator className="mb-4" />

              {/* 版本信息：底部 */}
              {version ? (
                <div className="space-y-3 text-sm">
                  <InfoRow icon={<Database className="h-4 w-4" />} label={t("about.version")}>
                    <span className="font-mono font-medium">dbx {version.version}</span>
                  </InfoRow>
                  {version.buildTime && (
                    <InfoRow icon={<Calendar className="h-4 w-4" />} label={t("about.buildTime")}>
                      <span className="font-mono text-muted-foreground">{version.buildTime}</span>
                    </InfoRow>
                  )}
                  <InfoRow icon={<Clock className="h-4 w-4" />} label={t("about.dbTypes")}>
                    <span className="font-mono text-muted-foreground">{version.dbTypes.join(" / ")}</span>
                  </InfoRow>
                </div>
              ) : (
                <div className="flex items-center justify-center pb-4">
                  <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                </div>
              )}
            </div>
          </TabsContent>

          {/* 更新日志 Tab：固定高度内滚动 */}
          <TabsContent value="changelog" className="mt-3">
            <div className="h-[320px] overflow-y-auto">
              {changelogLoading ? (
                <div className="flex items-center justify-center py-8">
                  <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
                </div>
              ) : changelogError ? (
                <div className="py-8 text-center text-sm text-muted-foreground">
                  {t("about.changelogLoadFailed")}
                </div>
              ) : changelog.length === 0 ? null : (
                <div className="space-y-1">
                  {changelog.map((item, idx) => (
                    <div key={item.version} className="rounded-md border">
                      <button
                        className="flex w-full items-center justify-between px-3 py-2 text-left text-sm transition-colors hover:bg-muted/50"
                        onClick={() => setExpandedIdx(expandedIdx === idx ? null : idx)}
                      >
                        <span className="font-medium">
                          v{item.version}
                          {item.date && (
                            <span className="ml-2 font-normal text-muted-foreground">{item.date}</span>
                          )}
                          {idx === 0 && (
                            <span className="ml-2 rounded bg-primary/10 px-1.5 py-px text-[10px] font-medium text-primary">Latest</span>
                          )}
                        </span>
                        <ChevronDown
                          className={`h-4 w-4 shrink-0 text-muted-foreground transition-transform ${expandedIdx === idx ? "rotate-180" : ""}`}
                        />
                      </button>
                      {expandedIdx === idx && (
                        <div className="border-t px-3 py-2">
                          <div className="markdown-body">
                            <ReactMarkdown remarkPlugins={[remarkGfm]}>
                              {item.content}
                            </ReactMarkdown>
                          </div>
                        </div>
                      )}
                    </div>
                  ))}
                </div>
              )}
            </div>
          </TabsContent>
        </Tabs>
      </DialogContent>
    </Dialog>
  )
}

// 版本信息行：左侧图标 + 标签，右侧值
function InfoRow({ icon, label, children }: { icon: React.ReactNode; label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="flex items-center gap-2 text-muted-foreground">
        {icon}
        {label}
      </span>
      {children}
    </div>
  )
}
