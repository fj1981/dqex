import { useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { Bot, Flag, Info, Keyboard, List, Sparkles, Terminal } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Separator } from "@/components/ui/separator"
import { tKey } from "@/lib/i18n"
import i18n from "@/lib/i18n"

// 使用说明弹窗：以 CLI 命令为主，帮助用户快速掌握命令行用法。
// 与「关于」弹窗（版本/构建信息）职责分离：这里只讲「怎么用」。
// 左侧为章节书签，点击可平滑定位；滚动内容时自动高亮当前章节。

// 命令速查表：命令 / 别名 / 用途（desc 为 i18n key）
const COMMANDS = [
  { cmd: "dqex", alias: "", desc: "help.cmdDbx" },
  { cmd: "dqex conn add", alias: "cn", desc: "help.cmdConnAdd" },
  { cmd: "dqex conn list", alias: "", desc: "help.cmdConnList" },
  { cmd: "dqex sql", alias: "", desc: "help.cmdSql" },
  { cmd: "dqex export", alias: "exp", desc: "help.cmdExport" },
  { cmd: "dqex import", alias: "imp", desc: "help.cmdImport" },
  { cmd: "dqex migrate", alias: "mig", desc: "help.cmdMigrate" },
  { cmd: "dqex compare", alias: "cmp", desc: "help.cmdCompare" },
  { cmd: "dqex dictionary", alias: "dict", desc: "help.cmdDictionary" },
  { cmd: "dqex snapshot", alias: "shot", desc: "help.cmdSnapshot" },
  { cmd: "dqex task", alias: "tk", desc: "help.cmdTask" },
  { cmd: "dqex history", alias: "his", desc: "help.cmdHistory" },
  { cmd: "dqex config", alias: "cfg", desc: "help.cmdConfig" },
]

// 常用短参（desc 为 i18n key）
const FLAGS = [
  { flag: "-s / -t", desc: "help.flagSrcTgt" },
  { flag: "-T", desc: "help.flagTables" },
  { flag: "-o / -i", desc: "help.flagIO" },
  { flag: "-h -P -u -p", desc: "help.flagMyDump" },
]

// 典型示例（title/code 均为 i18n key，code 含插值变量见 exVars）
const EXAMPLES = [
  { title: "help.exAddConn", code: "help.exConnAddCode" },
  { title: "help.exExport", code: "help.exExportCode" },
  { title: "help.exImport", code: "help.exImportCode" },
  { title: "help.exMigrate", code: "help.exMigrateCode" },
  { title: "help.exCompare", code: "help.exCompareCode" },
  { title: "help.exDict", code: "help.exDictCode" },
  { title: "help.exSnapshot", code: "help.exSnapshotCode" },
]

// 交互模式（dqex sql REPL）常用元命令（desc 为 i18n key）
const META_COMMANDS = [
  { cmd: "\\q / \\quit", desc: "help.metaQuit" },
  { cmd: "\\dt / \\tables [pat]", desc: "help.metaTables" },
  { cmd: "\\d / \\desc <表名>", desc: "help.metaDesc" },
  { cmd: "\\d+ <表名>", desc: "help.metaDescPlus" },
  { cmd: "\\l / \\list", desc: "help.metaList" },
  { cmd: "\\c / \\use <库名>", desc: "help.metaUse" },
  { cmd: "\\g", desc: "help.metaG" },
  { cmd: "\\G", desc: "help.metaGUpper" },
  { cmd: "\\x [on|off|auto]", desc: "help.metaX" },
  { cmd: "\\timing", desc: "help.metaTiming" },
  { cmd: "\\e / \\edit", desc: "help.metaEdit" },
  { cmd: "\\copy / \\w <文件>", desc: "help.metaCopy" },
  { cmd: "\\i <文件>", desc: "help.metaInclude" },
]

// AI 辅助写 SQL（\ai）子命令（desc 为 i18n key）
const AI_COMMANDS = [
  { cmd: "\\ai <需求>", desc: "help.aiGen" },
  { cmd: "\\ai explain [SQL]", desc: "help.aiExplain" },
  { cmd: "\\ai fix [报错信息]", desc: "help.aiFix" },
  { cmd: "\\ai continue <补充>", desc: "help.aiContinue" },
  { cmd: "\\ai copy", desc: "help.aiCopy" },
  { cmd: "\\ai status", desc: "help.aiStatus" },
  { cmd: "\\ai config", desc: "help.aiConfig" },
  { cmd: "\\ai clear", desc: "help.aiClear" },
]

// 智能体直接执行 SQL 模式（JSON 输出，脚本 / 智能体友好）；title/code 为 i18n key
const AGENT_EXAMPLES = [
  { title: "help.agJson", code: "help.agJsonCode" },
  { title: "help.agOnce", code: "help.agOnceCode" },
  { title: "help.agFromFile", code: "help.agFromFileCode" },
  { title: "help.agWrite", code: "help.agWriteCode" },
]

// 左侧书签（目录）：id 对应右侧章节的锚点；label 为 i18n key
const SECTIONS = [
  { id: "commands", label: "help.secCommands", icon: Terminal },
  { id: "flags", label: "help.secFlags", icon: Flag },
  { id: "examples", label: "help.secExamples", icon: List },
  { id: "repl", label: "help.secRepl", icon: Keyboard },
  { id: "ai", label: "help.secAi", icon: Sparkles },
  { id: "agent", label: "help.secAgent", icon: Bot },
  { id: "notes", label: "help.secNotes", icon: Info },
]

export default function HelpDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  const { t } = useTranslation()
  // 示例命令插值变量：连接名 / 快照名等示例值随 UI 语言翻译
  const exVars: Record<string, Record<string, string>> = {
    "help.exConnAddCode": { conn: t("help.sampleConnProd") },
    "help.exExportCode": { conn: t("help.sampleConnProd") },
    "help.exImportCode": { conn: t("help.sampleConnTest") },
    "help.exMigrateCode": { connSrc: t("help.sampleConnProdQuoted"), connTgt: t("help.sampleConnLocal") },
    "help.exCompareCode": { connSrc: t("help.sampleConnProd"), connTgt: t("help.sampleConnTest") },
    "help.exDictCode": { connSrc: t("help.sampleConnProdQuoted") },
    "help.exSnapshotCode": { conn: t("help.sampleConnProd"), snapA: t("help.sampleSnapMorning"), snapB: t("help.sampleSnapNoon") },
    "help.agJsonCode": { conn: t("help.sampleConnProd") },
    "help.agOnceCode": { conn: t("help.sampleConnProd") },
    "help.agFromFileCode": { conn: t("help.sampleConnProd") },
    "help.agWriteCode": { conn: t("help.sampleConnProd") },
  }
  const scrollRef = useRef<HTMLDivElement>(null)
  const sectionRefs = useRef<Record<string, HTMLElement | null>>({})
  const [active, setActive] = useState(SECTIONS[0].id)

  // 每次打开时回到顶部并重置高亮
  useEffect(() => {
    if (open) {
      scrollRef.current?.scrollTo({ top: 0 })
      setActive(SECTIONS[0].id)
    }
  }, [open])

  const scrollTo = (id: string) => {
    setActive(id)
    sectionRefs.current[id]?.scrollIntoView({ behavior: "smooth", block: "start" })
  }

  // 滚动监听：根据各章节相对容器顶部的位置，高亮当前章节
  const onScroll = () => {
    const container = scrollRef.current
    if (!container) return
    const cTop = container.getBoundingClientRect().top
    let current = SECTIONS[0].id
    for (const s of SECTIONS) {
      const el = sectionRefs.current[s.id]
      if (el && el.getBoundingClientRect().top - cTop <= 96) current = s.id
    }
    if (current !== active) setActive(current)
  }

  const setSectionRef = (id: string) => (el: HTMLElement | null) => {
    sectionRefs.current[id] = el
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="flex max-h-[85vh] flex-col overflow-hidden sm:max-w-[820px]">
        <DialogHeader className="shrink-0">
          <DialogTitle>{t("help.title")}</DialogTitle>
          <DialogDescription>
            {t("help.desc")}
          </DialogDescription>
        </DialogHeader>

        <div className="flex min-h-0 flex-1 gap-4">
          {/* 左侧书签栏 */}
          <nav className="hidden w-36 shrink-0 flex-col gap-1 self-start overflow-y-auto py-1 sm:flex">
            {SECTIONS.map((s) => (
              <button
                key={s.id}
                onClick={() => scrollTo(s.id)}
                title={tKey(s.label)}
                className={
                  "flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs font-medium transition-colors " +
                  (active === s.id
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground")
                }
              >
                <s.icon className="h-3.5 w-3.5 shrink-0" />
                {tKey(s.label)}
              </button>
            ))}
            <Separator className="my-1" />
            <span className="px-2 text-[11px] text-muted-foreground/70">{t("help.navHint")}</span>
          </nav>

          {/* 右侧滚动内容 */}
          <div ref={scrollRef} onScroll={onScroll} className="min-w-0 flex-1 space-y-5 overflow-y-auto pr-1">
            {/* 命令速查 */}
            <section ref={setSectionRef("commands")} className="scroll-mt-2 space-y-2">
              <h3 className="flex items-center gap-1.5 text-sm font-semibold">
                <Terminal className="h-4 w-4 text-primary" />
                {t("help.secCommands")}
              </h3>
              <div className="overflow-x-auto rounded-md border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-muted text-xs text-muted-foreground">
                    <tr>
                      <th className="px-3 py-1.5 font-medium">{t("help.colCmd")}</th>
                      <th className="px-3 py-1.5 font-medium">{t("help.colAlias")}</th>
                      <th className="px-3 py-1.5 font-medium">{t("help.colUsage")}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {COMMANDS.map((c) => (
                      <tr key={c.cmd}>
                        <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-foreground">{c.cmd}</td>
                        <td className="px-3 py-1.5 font-mono text-xs text-muted-foreground">{c.alias || "—"}</td>
                        <td className="px-3 py-1.5 text-xs text-muted-foreground">{tKey(c.desc)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>

            <Separator />

            {/* 常用短参 */}
            <section ref={setSectionRef("flags")} className="scroll-mt-2 space-y-2">
              <h3 className="text-sm font-semibold">{t("help.secFlags")}</h3>
              <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                {FLAGS.map((f) => (
                  <div key={f.flag} className="flex items-baseline gap-2 rounded-md border px-2.5 py-1.5">
                    <code className="shrink-0 font-mono text-xs font-medium text-primary">{f.flag}</code>
                    <span className="text-xs text-muted-foreground">{tKey(f.desc)}</span>
                  </div>
                ))}
              </div>
            </section>

            <Separator />

            {/* 典型示例 */}
            <section ref={setSectionRef("examples")} className="scroll-mt-2 space-y-2">
              <h3 className="text-sm font-semibold">{t("help.secExamples")}</h3>
              <div className="space-y-2">
                {EXAMPLES.map((e) => (
                  <div key={e.title} className="space-y-1">
                    <div className="text-xs font-medium text-muted-foreground">{tKey(e.title)}</div>
                    <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs text-foreground">
                      {i18n.t(e.code as never, exVars[e.code])}
                    </pre>
                  </div>
                ))}
              </div>
            </section>

            <Separator />

            {/* 交互模式 */}
            <section ref={setSectionRef("repl")} className="scroll-mt-2 space-y-2">
              <h3 className="flex items-center gap-1.5 text-sm font-semibold">
                <Terminal className="h-4 w-4 text-primary" />
                {t("help.secRepl")}
              </h3>
              <p className="text-xs text-muted-foreground">
                {t("help.replIntro")}
              </p>
              <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs text-foreground">
                dqex sql -c &lt;{t("help.replConn")}&gt;
              </pre>
              <div className="overflow-x-auto rounded-md border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-muted text-xs text-muted-foreground">
                    <tr>
                      <th className="px-3 py-1.5 font-medium">{t("help.colMeta")}</th>
                      <th className="px-3 py-1.5 font-medium">{t("help.colDesc")}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {META_COMMANDS.map((m) => (
                      <tr key={m.cmd}>
                        <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-foreground">{m.cmd}</td>
                        <td className="px-3 py-1.5 text-xs text-muted-foreground">{tKey(m.desc)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="text-xs text-muted-foreground">
                {t("help.replShortcuts1")}
                <code className="font-mono">Ctrl+R</code>
                {t("help.replShortcuts2")}
                <code className="font-mono">Tab</code>
                {t("help.replShortcuts3")}
                <code className="font-mono">Ctrl+D</code>
                {t("help.replShortcuts4")}
                <code className="font-mono">\G</code>
                {t("help.replShortcuts5")}
                <code className="font-mono">SELECT * FROM t \G</code>
                {t("help.replShortcuts6")}
                <code className="font-mono">/</code>
                {t("help.replShortcuts7")}
                <code className="font-mono">\</code>
                {t("help.replShortcuts8")}
                <code className="font-mono">/ai</code>
                {t("help.replShortcuts9")}
                <code className="font-mono">/dt</code>
                {t("help.replShortcuts10")}
              </p>
            </section>

            <Separator />

            {/* AI 辅助写 SQL */}
            <section ref={setSectionRef("ai")} className="scroll-mt-2 space-y-2">
              <h3 className="flex items-center gap-1.5 text-sm font-semibold">
                <Sparkles className="h-4 w-4 text-primary" />
                {t("help.secAi")}
              </h3>
              <p className="text-xs text-muted-foreground">
                {t("help.aiIntro1")}<code className="font-mono">\e</code>{t("help.aiIntro2")}<code className="font-mono">\g</code>{t("help.aiIntro3")}<code className="font-mono">\ai fix</code>{t("help.aiIntro4")}
              </p>
              <div className="overflow-x-auto rounded-md border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-muted text-xs text-muted-foreground">
                    <tr>
                      <th className="px-3 py-1.5 font-medium">{t("help.colCmd")}</th>
                      <th className="px-3 py-1.5 font-medium">{t("help.colDesc")}</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {AI_COMMANDS.map((m) => (
                      <tr key={m.cmd}>
                        <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-foreground">{m.cmd}</td>
                        <td className="px-3 py-1.5 text-xs text-muted-foreground">{tKey(m.desc)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs text-foreground">
                dqex sql -c {t("help.aiDemoConn")}
                {"\n"}
                dqex (mysql @ ...)&gt; \ai {t("help.aiDemoPrompt")}
              </pre>
            </section>

            <Separator />

            {/* 智能体直接执行 SQL */}
            <section ref={setSectionRef("agent")} className="scroll-mt-2 space-y-2">
              <h3 className="flex items-center gap-1.5 text-sm font-semibold">
                <Terminal className="h-4 w-4 text-primary" />
                {t("help.secAgent")}
              </h3>
              <p className="text-xs text-muted-foreground">
                {t("help.agentIntro1")}<code className="font-mono">--json</code>{t("help.agentIntro2")}<code className="font-mono">--allow-write</code>{t("help.agentIntro3")}
              </p>
              <div className="space-y-2">
                {AGENT_EXAMPLES.map((e) => (
                  <div key={e.title} className="space-y-1">
                    <div className="text-xs font-medium text-muted-foreground">{tKey(e.title)}</div>
                    <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs text-foreground">
                      {i18n.t(e.code as never, exVars[e.code])}
                    </pre>
                  </div>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                {t("help.agentParams1")}<code className="font-mono">--max-rows</code>{t("help.agentParams2")}<code className="font-mono">--timeout</code>{t("help.agentParams3")}<code className="font-mono">--no-color</code>{t("help.agentParams4")}
              </p>
            </section>

            <Separator />

            {/* 说明 */}
            <section ref={setSectionRef("notes")} className="scroll-mt-2 space-y-2">
              <h3 className="text-sm font-semibold">{t("help.secNotes")}</h3>
              <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
                <li>{t("help.note1a")}<code className="font-mono text-xs">--config 文件</code>{t("help.note1b")}<code className="font-mono text-xs">--task 任务ID</code>{t("help.note1c")}</li>
                <li>{t("help.note2a")}<code className="font-mono text-xs">"170 生产"</code>{t("help.note2b")}</li>
                <li>{t("help.note3a")}<code className="font-mono text-xs">--reset truncate/drop</code>{t("help.note3b")}</li>
                <li>{t("help.note4a")}<code className="font-mono text-xs">dqex import</code>{t("help.note4b")}</li>
                <li>{t("help.note5a")}<code className="font-mono text-xs">--port</code>{t("help.note5b")}<code className="font-mono text-xs">--port</code>{t("help.note5c")}</li>
              </ul>
            </section>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
