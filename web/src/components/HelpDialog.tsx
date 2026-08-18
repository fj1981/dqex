import { Terminal } from "lucide-react"
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
  { cmd: "\\g", desc: "再次执行上一条 SQL" },
  { cmd: "\\G", desc: "垂直显示（每行一个字段）" },
  { cmd: "\\timing", desc: "切换耗时显示" },
  { cmd: "\\e / \\edit", desc: "用外部编辑器编辑上一条 SQL" },
  { cmd: "\\copy / \\w <文件>", desc: "导出上一条查询结果到文件（CSV）" },
  { cmd: "\\i <文件>", desc: "执行文件中的 SQL" },
]

// 智能体直接执行 SQL 模式（JSON 输出，脚本 / 智能体友好）
const AGENT_EXAMPLES = [
  { title: "JSON 输出（推荐智能体调用）", code: 'dbx sql -c "生产库" --json "SELECT * FROM users LIMIT 10"' },
  { title: "单次执行（表格输出）", code: 'dbx sql -c "生产库" -e "SELECT COUNT(*) FROM users"' },
  { title: "从文件执行", code: 'dbx sql -c "生产库" -f query.sql' },
  { title: "JSON 模式执行写操作", code: 'dbx sql -c "生产库" --json --allow-write "UPDATE users SET status=1"' },
]

export default function HelpDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (o: boolean) => void }) {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-[640px]">
        <DialogHeader>
          <DialogTitle>使用说明</DialogTitle>
          <DialogDescription>
            命令行（CLI）与 Web 界面共享同一份连接、任务配置与执行历史
          </DialogDescription>
        </DialogHeader>

        <div className="max-h-[65vh] space-y-5 overflow-y-auto pr-1">
          {/* 命令速查 */}
          <section className="space-y-2">
            <h3 className="flex items-center gap-1.5 text-sm font-semibold">
              <Terminal className="h-4 w-4 text-primary" />
              命令速查
            </h3>
            <div className="overflow-hidden rounded-md border">
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
          <section className="space-y-2">
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
          <section className="space-y-2">
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
          <section className="space-y-2">
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
            <div className="overflow-hidden rounded-md border">
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

          {/* 智能体直接执行 SQL */}
          <section className="space-y-2">
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
          <section className="space-y-2">
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
      </DialogContent>
    </Dialog>
  )
}
