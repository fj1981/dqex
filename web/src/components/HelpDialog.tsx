import { useEffect, useRef, useState } from "react"
import { Bot, Flag, Info, Keyboard, List, Sparkles, Terminal } from "lucide-react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Separator } from "@/components/ui/separator"

// 使用说明弹窗：以 CLI 命令为主，帮助用户快速掌握命令行用法。
// 与「关于」弹窗（版本/构建信息）职责分离：这里只讲「怎么用」。
// 左侧为章节书签，点击可平滑定位；滚动内容时自动高亮当前章节。

// 命令速查表：命令 / 别名 / 用途
const COMMANDS = [
  { cmd: "dbx", alias: "", desc: "启动 Web 服务（默认 127.0.0.1:8181）" },
  { cmd: "dbx conn add", alias: "cn", desc: "新增 / 更新数据库连接" },
  { cmd: "dbx conn list", alias: "", desc: "列出已保存连接" },
  { cmd: "dbx sql", alias: "", desc: "交互式 SQL 终端 / 执行 SQL 查询" },
  { cmd: "dbx export", alias: "exp", desc: "库结构与数据 → SQL 文件（zip/gzip）" },
  { cmd: "dbx import", alias: "imp", desc: "SQL/zip 文件 → 数据库" },
  { cmd: "dbx migrate", alias: "mig", desc: "数据库 → 数据库（跨类型）" },
  { cmd: "dbx compare", alias: "cmp", desc: "两个库的结构与数据差异" },
  { cmd: "dbx dictionary", alias: "dict", desc: "表结构 + 注释 → Excel（.xlsx）" },
  { cmd: "dbx snapshot", alias: "shot", desc: "库快照：create / list / show / delete / compare" },
  { cmd: "dbx task", alias: "tk", desc: "任务配置：保存 / 运行 / 删除" },
  { cmd: "dbx history", alias: "his", desc: "执行历史：查看 / 删除" },
  { cmd: "dbx config", alias: "cfg", desc: "查看 / 生成全局配置" },
]

// 常用短参
const FLAGS = [
  { flag: "-s / -t", desc: "--source-conn / --target-conn（源 / 目标连接）" },
  { flag: "-T", desc: "--tables（指定表，逗号分隔）" },
  { flag: "-o / -i", desc: "--output / --input（输出 / 输入文件）" },
  { flag: "-h -P -u -p", desc: "--host --port --user --password（mysqldump 风格）" },
]

// 典型示例
const EXAMPLES = [
  { title: "添加连接", code: 'dbx conn add --name 生产库 --type mysql --host 10.20.16.170 --port 3317 --un root --pw \'xxx\'' },
  { title: "导出", code: "dbx exp camunda -s 生产库 -o backup.zip" },
  { title: "导入", code: "dbx import -t 测试库 -i backup.zip --reset truncate" },
  { title: "迁移", code: 'dbx migrate -s "170 生产" -t 本地 --tables act_ge_property --reset truncate' },
  { title: "对比", code: "dbx cmp -s 生产库 -t 测试库 --scope both" },
  { title: "数据字典", code: 'dbx dict camunda -s "170 生产" -o dict.zip' },
  { title: "快照与对比", code: 'dbx shot create -c 生产库 -n 早盘\n dbx shot compare -c 生产库 --a 早盘 --b 午盘' },
]

// 交互模式（dbx sql REPL）常用元命令
const META_COMMANDS = [
  { cmd: "\\q / \\quit", desc: "退出终端" },
  { cmd: "\\dt / \\tables [pat]", desc: "列出表（支持通配符 * ?）" },
  { cmd: "\\d / \\desc <表名>", desc: "查看表结构" },
  { cmd: "\\d+ <表名>", desc: "查看表结构（含索引 / 约束）" },
  { cmd: "\\l / \\list", desc: "列出数据库" },
  { cmd: "\\c / \\use <库名>", desc: "切换数据库" },
  { cmd: "\\g", desc: "再次执行上一条 SQL（表格）" },
  { cmd: "\\G", desc: "执行上一条 SQL 并垂直显示（每行一个字段）" },
  { cmd: "\\x [on|off|auto]", desc: "扩展显示：on=垂直 off=表格 auto=超宽自动（默认，写入 config.yaml）" },
  { cmd: "\\timing", desc: "切换耗时显示" },
  { cmd: "\\e / \\edit", desc: "用外部编辑器编辑上一条 SQL" },
  { cmd: "\\copy / \\w <文件>", desc: "导出上一条查询结果到文件（CSV）" },
  { cmd: "\\i <文件>", desc: "执行文件中的 SQL" },
]

// AI 辅助写 SQL（\ai）子命令
const AI_COMMANDS = [
  { cmd: "\\ai <需求>", desc: "生成 SQL 到缓冲区，可 \\e 编辑、\\g 执行" },
  { cmd: "\\ai explain [SQL]", desc: "解释 SQL（缺省用缓冲区）" },
  { cmd: "\\ai fix [报错信息]", desc: "修复缓冲区 SQL（缺省自动附带上次执行报错）" },
  { cmd: "\\ai continue <补充>", desc: "基于上文继续补充生成" },
  { cmd: "\\ai copy", desc: "复制缓冲区 SQL 到系统剪贴板" },
  { cmd: "\\ai status", desc: "查看配置状态与 token 统计" },
  { cmd: "\\ai config", desc: "引导式修改 AI 配置（写回 config.yaml）" },
  { cmd: "\\ai clear", desc: "重置当前会话（清空上下文与 token 统计）" },
]

// 智能体直接执行 SQL 模式（JSON 输出，脚本 / 智能体友好）
const AGENT_EXAMPLES = [
  { title: "JSON 输出（推荐智能体调用）", code: 'dbx sql -c "生产库" --json "SELECT * FROM users LIMIT 10"' },
  { title: "单次执行（表格输出）", code: 'dbx sql -c "生产库" -e "SELECT COUNT(*) FROM users"' },
  { title: "从文件执行", code: 'dbx sql -c "生产库" -f query.sql' },
  { title: "JSON 模式执行写操作", code: 'dbx sql -c "生产库" --json --allow-write "UPDATE users SET status=1"' },
]

// 左侧书签（目录）：id 对应右侧章节的锚点
const SECTIONS = [
  { id: "commands", label: "命令速查", icon: Terminal },
  { id: "flags", label: "常用短参", icon: Flag },
  { id: "examples", label: "典型示例", icon: List },
  { id: "repl", label: "SQL 终端", icon: Keyboard },
  { id: "ai", label: "AI 写 SQL", icon: Sparkles },
  { id: "agent", label: "脚本直连", icon: Bot },
  { id: "notes", label: "说明", icon: Info },
]

export default function HelpDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
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
          <DialogTitle>使用说明</DialogTitle>
          <DialogDescription>
            命令行（CLI）与 Web 界面共享同一份连接、任务配置与执行历史
          </DialogDescription>
        </DialogHeader>

        <div className="flex min-h-0 flex-1 gap-4">
          {/* 左侧书签栏 */}
          <nav className="hidden w-36 shrink-0 flex-col gap-1 self-start overflow-y-auto py-1 sm:flex">
            {SECTIONS.map((s) => (
              <button
                key={s.id}
                onClick={() => scrollTo(s.id)}
                title={s.label}
                className={
                  "flex items-center gap-2 rounded-md px-2 py-1.5 text-left text-xs font-medium transition-colors " +
                  (active === s.id
                    ? "bg-primary/10 text-primary"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground")
                }
              >
                <s.icon className="h-3.5 w-3.5 shrink-0" />
                {s.label}
              </button>
            ))}
            <Separator className="my-1" />
            <span className="px-2 text-[11px] text-muted-foreground/70">点击定位到对应章节</span>
          </nav>

          {/* 右侧滚动内容 */}
          <div ref={scrollRef} onScroll={onScroll} className="min-w-0 flex-1 space-y-5 overflow-y-auto pr-1">
            {/* 命令速查 */}
            <section ref={setSectionRef("commands")} className="scroll-mt-2 space-y-2">
              <h3 className="flex items-center gap-1.5 text-sm font-semibold">
                <Terminal className="h-4 w-4 text-primary" />
                命令速查
              </h3>
              <div className="overflow-x-auto rounded-md border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-muted text-xs text-muted-foreground">
                    <tr>
                      <th className="px-3 py-1.5 font-medium">命令</th>
                      <th className="px-3 py-1.5 font-medium">别名</th>
                      <th className="px-3 py-1.5 font-medium">用途</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {COMMANDS.map((c) => (
                      <tr key={c.cmd}>
                        <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-foreground">{c.cmd}</td>
                        <td className="px-3 py-1.5 font-mono text-xs text-muted-foreground">{c.alias || "—"}</td>
                        <td className="px-3 py-1.5 text-xs text-muted-foreground">{c.desc}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </section>

            <Separator />

            {/* 常用短参 */}
            <section ref={setSectionRef("flags")} className="scroll-mt-2 space-y-2">
              <h3 className="text-sm font-semibold">常用短参</h3>
              <div className="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
                {FLAGS.map((f) => (
                  <div key={f.flag} className="flex items-baseline gap-2 rounded-md border px-2.5 py-1.5">
                    <code className="shrink-0 font-mono text-xs font-medium text-primary">{f.flag}</code>
                    <span className="text-xs text-muted-foreground">{f.desc}</span>
                  </div>
                ))}
              </div>
            </section>

            <Separator />

            {/* 典型示例 */}
            <section ref={setSectionRef("examples")} className="scroll-mt-2 space-y-2">
              <h3 className="text-sm font-semibold">典型示例</h3>
              <div className="space-y-2">
                {EXAMPLES.map((e) => (
                  <div key={e.title} className="space-y-1">
                    <div className="text-xs font-medium text-muted-foreground">{e.title}</div>
                    <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs text-foreground">
                      {e.code}
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
                交互模式（SQL 终端）
              </h3>
              <p className="text-xs text-muted-foreground">
                启动交互式 REPL 终端，支持表格渲染、语法高亮、Tab 补全与历史搜索。
              </p>
              <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs text-foreground">
                dbx sql -c &lt;连接&gt;
              </pre>
              <div className="overflow-x-auto rounded-md border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-muted text-xs text-muted-foreground">
                    <tr>
                      <th className="px-3 py-1.5 font-medium">元命令</th>
                      <th className="px-3 py-1.5 font-medium">说明</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {META_COMMANDS.map((m) => (
                      <tr key={m.cmd}>
                        <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-foreground">{m.cmd}</td>
                        <td className="px-3 py-1.5 text-xs text-muted-foreground">{m.desc}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <p className="text-xs text-muted-foreground">
                快捷操作：分号结尾回车即执行，无分号则多行续写；<code className="font-mono">Ctrl+R</code> 搜索历史、<code className="font-mono">Tab</code> 补全、<code className="font-mono">Ctrl+D</code> 退出。
              </p>
            </section>

            <Separator />

            {/* AI 辅助写 SQL */}
            <section ref={setSectionRef("ai")} className="scroll-mt-2 space-y-2">
              <h3 className="flex items-center gap-1.5 text-sm font-semibold">
                <Sparkles className="h-4 w-4 text-primary" />
                AI 辅助写 SQL（可选模块）
              </h3>
              <p className="text-xs text-muted-foreground">
                在 SQL 终端内用自然语言生成 SQL；配置齐全（BaseURL / Model / Key）才启用，未配置时无入口。生成结果仅写入缓冲区，需 <code className="font-mono">\e</code> 编辑、<code className="font-mono">\g</code> 执行，复用安全链路。生成时自动调用 list_databases / list_tables / get_schema 查询真实表结构；执行报错后直接 <code className="font-mono">\ai fix</code> 即可自动携带报错修复。
              </p>
              <div className="overflow-x-auto rounded-md border">
                <table className="w-full text-left text-sm">
                  <thead className="bg-muted text-xs text-muted-foreground">
                    <tr>
                      <th className="px-3 py-1.5 font-medium">命令</th>
                      <th className="px-3 py-1.5 font-medium">说明</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y">
                    {AI_COMMANDS.map((m) => (
                      <tr key={m.cmd}>
                        <td className="whitespace-nowrap px-3 py-1.5 font-mono text-xs text-foreground">{m.cmd}</td>
                        <td className="px-3 py-1.5 text-xs text-muted-foreground">{m.desc}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
              <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs text-foreground">
                dbx sql -c 生产库
                {"\n"}
                dbx (mysql @ ...)&gt; \ai 帮我统计昨日新增用户数
              </pre>
            </section>

            <Separator />

            {/* 智能体直接执行 SQL */}
            <section ref={setSectionRef("agent")} className="scroll-mt-2 space-y-2">
              <h3 className="flex items-center gap-1.5 text-sm font-semibold">
                <Terminal className="h-4 w-4 text-primary" />
                智能体 / 脚本直接执行 SQL
              </h3>
              <p className="text-xs text-muted-foreground">
                以 <code className="font-mono">--json</code> 输出结构化结果（columns / rows / rowCount / elapsed），供智能体、脚本或程序化调用解析；大数据量自动切换 NDJSON 流式输出。写操作需显式 <code className="font-mono">--allow-write</code> 确认。
              </p>
              <div className="space-y-2">
                {AGENT_EXAMPLES.map((e) => (
                  <div key={e.title} className="space-y-1">
                    <div className="text-xs font-medium text-muted-foreground">{e.title}</div>
                    <pre className="overflow-x-auto rounded-md bg-muted px-3 py-2 font-mono text-xs text-foreground">
                      {e.code}
                    </pre>
                  </div>
                ))}
              </div>
              <p className="text-xs text-muted-foreground">
                常用参数：<code className="font-mono">--max-rows</code>（默认 1000 最大行数）、<code className="font-mono">--timeout</code>（默认 30s）、<code className="font-mono">--no-color</code>（纯文本输出，便于解析）。
              </p>
            </section>

            <Separator />

            {/* 说明 */}
            <section ref={setSectionRef("notes")} className="scroll-mt-2 space-y-2">
              <h3 className="text-sm font-semibold">说明</h3>
              <ul className="list-disc space-y-1 pl-5 text-sm text-muted-foreground">
                <li>所有执行类命令支持三种参数来源：<code className="font-mono text-xs">--config 文件</code>、<code className="font-mono text-xs">--task 任务ID</code>、命令行参数，命令行优先级最高。</li>
                <li>连接名支持中文 / 空格（如 <code className="font-mono text-xs">"170 生产"</code>），shell 中需加引号。</li>
                <li>重置类操作（<code className="font-mono text-xs">--reset truncate/drop</code>）默认先备份，失败时可回滚。</li>
                <li>导出文件为 dbx 自有结构，请用 <code className="font-mono text-xs">dbx import</code> 还原。</li>
                <li>根命令 <code className="font-mono text-xs">--port</code> 是 Web 服务端口；子命令 <code className="font-mono text-xs">--port</code> 是数据库端口。</li>
              </ul>
            </section>
          </div>
        </div>
      </DialogContent>
    </Dialog>
  )
}
