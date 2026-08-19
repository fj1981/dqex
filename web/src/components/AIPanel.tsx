import { isValidElement, useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { useVirtualizer } from "@tanstack/react-virtual"
import { toast } from "sonner"
import {
  BookOpen,
  Bot,
  BrainCircuit,
  Check,
  ChevronDown,
  ChevronUp,
  Copy,
  CornerDownLeft,
  Eraser,
  Gauge,
  Loader2,
  RotateCcw,
  Square,
  Sparkles,
  Wrench,
  X,
} from "lucide-react"
import ReactMarkdown from "react-markdown"
import remarkGfm from "remark-gfm"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
} from "@/components/ui/dropdown-menu"
import { Separator } from "@/components/ui/separator"
import { Textarea } from "@/components/ui/textarea"
import * as api from "@/api"
import type { AIUsage } from "@/types"

type AiMode = "generate" | "explain" | "fix" | "optimize"

// 动作 → 对话气泡标签（用于快捷触发「解释/优化/修复」时在 user 气泡上标识语义，避免纯 SQL 无法区分）
const ACTION_LABEL: Record<AiMode, string> = {
  generate: "生成",
  explain: "解释 SQL",
  optimize: "优化 SQL",
  fix: "修复 SQL",
}

interface AiMessage {
  role: "user" | "assistant"
  content: string
  error?: boolean
  // action：本条 user 消息对应的动作类型（解释/优化/修复/生成），
  // 仅 user 消息携带，用于气泡上展示语义标签；assistant 消息无。
  action?: AiMode
  // schemaVerified：本轮对话是否调用过 get_schema（真实表结构已验证）。
  // 用于 SQL 代码块上展示可靠度图标（绿✓已验证 / 灰?未验证），不阻断。
  schemaVerified?: boolean
}

interface AIPanelProps {
  connId: string
  db?: string
  // 所属 query tab（按 tab 隔离对话：不同 tab 各自维护独立会话与历史）
  tabId?: string
  // 请求将生成的 SQL 应用到编辑器（编辑器内 diff 高亮 + 应用/取消确认）。
  // action 指定插入方式：replace_all 全部替换 / replace_selection 替换所选 / insert_cursor 插入光标处 / append 追加末尾。
  onPreviewSql: (sql: string, action: "replace_all" | "replace_selection" | "insert_cursor" | "append") => void
  // 主编辑器当前是否有选中内容（用于插入菜单默认项高亮）
  hasSelection?: boolean
  // 外部快捷触发请求（编辑器工具栏「解释/优化」、报错卡片「AI 修复」）：
  // action 指定动作，text 为拼装好的任务文本，AIPanel 收到后立即发送。
  quickRequest?: { action: "explain" | "optimize" | "fix"; text: string } | null
  // 快捷触发请求已被消费（AIPanel 完成发送准备），父组件据此清除 quickRequest
  onQuickConsumed?: () => void
  onClose: () => void
}

// 工具调用提示文案：args 为 JSON 时用 fmt 生成带参数的提示，解析失败用 fallback
function fmtToolHint(args: string, fmt: (a: Record<string, string>) => string, fallback: string): string {
  try {
    const a = JSON.parse(args || "{}") as Record<string, string>
    return fmt(a)
  } catch {
    return fallback
  }
}

// 分离模型输出的思考段。不同模型用不同标签，需兼容常见变体：
// <thinking> / <think> / <thought> / <reasoning>（成对或未闭合的流式尾段都归为思考）。
function parseThinking(text: string): { thinking: string; answer: string } {
  const tag = "(?:thinking|think|thought|reasoning)"
  const re = new RegExp(`<${tag}>([\\s\\S]*?)(?:</${tag}>|$)`, "g")
  const thinking: string[] = []
  let m: RegExpExecArray | null
  while ((m = re.exec(text)) !== null) thinking.push(m[1])
  if (thinking.length === 0) return { thinking: "", answer: text }
  const answer = text.replace(new RegExp(`<${tag}>[\\s\\S]*?(?:</${tag}>|$)`, "g"), "").trim()
  return { thinking: thinking.join("\n"), answer }
}

// 可折叠思考段：默认折叠，流式生成中自动展开便于观察进度
// StreamingStatus AI 流式执行过程中的统一状态指示器：
//   - 工具调用中：显示工具名 + 三点跳动动画（处理节奏感）
//   - 思考中（无工具、无文字输出）：显示「正在思考…」+ 呼吸圆点
//   - 已有文字输出（answer 已流式）：显示极简的呼吸圆点（提示仍在持续生成）
// 统一替代原先消息头部 + 工具提示两处割裂的 Loader2 旋转图标。
function StreamingStatus({ toolHint, hasAnswer }: { toolHint: string; hasAnswer: boolean }) {
  if (toolHint) {
    return (
      <div className="mb-1.5 inline-flex items-center gap-2 rounded-full border border-violet-500/30 bg-violet-500/10 px-2.5 py-1 text-[11px] font-medium text-violet-700 dark:text-violet-300">
        <span className="flex items-center gap-0.5">
          <span className="ai-bounce-dot h-1 w-1 rounded-full bg-violet-500" />
          <span className="ai-bounce-dot h-1 w-1 rounded-full bg-violet-500" />
          <span className="ai-bounce-dot h-1 w-1 rounded-full bg-violet-500" />
        </span>
        <span className="truncate">{toolHint}</span>
      </div>
    )
  }
  // 无工具调用：思考中（无输出）或生成中（已有输出，用呼吸圆点表达「仍在持续」）
  const label = hasAnswer ? "正在生成…" : "正在思考…"
  return (
    <div className="mb-1.5 inline-flex items-center gap-1.5 text-[11px] text-muted-foreground">
      <span className="ai-status-dot h-1.5 w-1.5 rounded-full bg-violet-500" />
      <span>{label}</span>
    </div>
  )
}

// CollapsibleContent 长内容折叠容器：内容超过 maxHeight 时收起，底部渐隐 + 「展开」按钮；
// 展开后完整显示并可「收起」。内容未超限时不做任何处理（无按钮、无遮罩）。
// live=true 表示流式输出中：此时始终展开（不折叠、不显示按钮），避免输出过程中高度抖动；
// 流式结束后（live 变 false）才按实际高度启用折叠。
function CollapsibleContent({ children, maxHeight = 320, live = false }: { children: ReactNode; maxHeight?: number; live?: boolean }) {
  const innerRef = useRef<HTMLDivElement>(null)
  const [collapsed, setCollapsed] = useState(true)
  const [overflow, setOverflow] = useState(false)

  useEffect(() => {
    if (live) {
      setOverflow(false)
      setCollapsed(true)
      return
    }
    const el = innerRef.current
    if (!el) return
    // 内容高度超过阈值才启用折叠；用 2px 容差避免临界抖动
    setOverflow(el.scrollHeight > maxHeight + 2)
  }, [children, maxHeight, live])

  // 折叠态：定位到「首个 SQL 代码块」位置，使末尾的 SQL（用户核心诉求）优先可见，
  // 前面的解释文字折叠到上方。无代码块时回到顶部。
  // 仅在进入折叠态时定位一次，不持续纠偏：否则用户向上滚动查看解释文字时会被反复拉回 SQL 处，导致无法滚动
  useEffect(() => {
    const el = innerRef.current
    if (!el || !overflow || !collapsed) return
    const codeBlock = el.querySelector<HTMLElement>("pre")
    el.scrollTop = codeBlock ? Math.max(0, codeBlock.offsetTop - 8) : 0
  }, [overflow, collapsed, children])

  if (!overflow) {
    return <div ref={innerRef}>{children}</div>
  }

  return (
    <div className="relative">
      <div
        ref={innerRef}
        style={collapsed ? { maxHeight, overflowY: "auto" } : undefined}
        className={collapsed ? "overflow-y-auto" : undefined}
      >
        {children}
      </div>
      {collapsed && (
        <div className="pointer-events-none absolute inset-x-0 bottom-0 h-8 bg-gradient-to-t from-background to-transparent" />
      )}
      <button
        type="button"
        onClick={() => setCollapsed((v) => !v)}
        className="mt-1 flex w-full items-center justify-center gap-1 rounded-md border py-1 text-[11px] text-muted-foreground transition-colors hover:bg-accent"
      >
        {collapsed ? (
          <>
            展开完整内容
            <ChevronDown className="h-3 w-3" />
          </>
        ) : (
          <>
            收起
            <ChevronUp className="h-3 w-3" />
          </>
        )}
      </button>
    </div>
  )
}

function ThinkingBlock({ text, defaultOpen }: { text: string; defaultOpen: boolean }) {
  const [open, setOpen] = useState(defaultOpen)
  useEffect(() => {
    setOpen(defaultOpen)
  }, [defaultOpen])
  return (
    <div className="mb-2 overflow-hidden rounded-md border bg-muted/20">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex w-full items-center gap-1.5 px-2 py-1 text-[10px] text-muted-foreground transition-colors hover:bg-accent"
      >
        <BrainCircuit className="h-3 w-3" />
        <span>{open ? "思考过程" : "思考过程（点击展开）"}</span>
        {open ? (
          <ChevronUp className="ml-auto h-3 w-3" />
        ) : (
          <ChevronDown className="ml-auto h-3 w-3" />
        )}
      </button>
      {open && (
        <pre className="max-h-40 overflow-y-auto whitespace-pre-wrap border-t px-2 py-1.5 font-sans text-[11px] leading-relaxed text-muted-foreground">
          {text}
        </pre>
      )}
    </div>
  )
}

// 代码块：顶部工具栏（语言标签 + 可靠度图标 + 复制按钮；SQL 代码块额外带「插入编辑器」下拉）。
// 流式输出过程中代码块可能尚未闭合，仍按块级渲染，避免内容跳动。
function CodeBlock({ text, lang, isSql, hasSelection, schemaVerified, onApplySql }: {
  text: string
  lang: string
  isSql: boolean
  hasSelection: boolean
  schemaVerified: boolean
  onApplySql: (sql: string, action: "replace_all" | "replace_selection" | "insert_cursor" | "append") => void
}) {
  const [copied, setCopied] = useState(false)
  const copyTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  
  const copy = async () => {
    // 清除之前的定时器（防止快速点击导致多个定时器叠加）
    if (copyTimerRef.current) {
      clearTimeout(copyTimerRef.current)
      copyTimerRef.current = null
    }
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      copyTimerRef.current = setTimeout(() => {
        setCopied(false)
        copyTimerRef.current = null
      }, 1500)
    } catch {
      toast.error("复制失败，请手动复制")
    }
  }
  return (
    <div className="my-1.5 overflow-hidden rounded-md border bg-muted/30">
      <div className="flex items-center gap-1 border-b bg-muted/50 px-2 py-1">
        <span className="text-[10px] font-mono uppercase text-muted-foreground">{lang || "text"}</span>
        {/* 可靠度图标：仅 SQL 代码块展示（绿✓=本轮查过真实表结构；灰?=未验证，字段可能臆造） */}
        {isSql && (
          <span
            className="flex items-center gap-0.5 text-[10px]"
            title={schemaVerified ? "本轮已查询真实表结构，字段可靠" : "本轮未查询真实表结构，字段可能不准确，请人工确认"}
          >
            {schemaVerified ? (
              <Check className="h-3 w-3 text-emerald-500" />
            ) : (
              <span className="text-muted-foreground">?</span>
            )}
            <span className={schemaVerified ? "text-emerald-600" : "text-muted-foreground"}>
              {schemaVerified ? "已验证" : "未验证"}
            </span>
          </span>
        )}
        <div className="ml-auto flex items-center gap-0.5">
          {isSql && (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button size="sm" variant="ghost" className="h-5 gap-1 px-1.5 text-[10px] text-muted-foreground hover:text-violet-600">
                  <CornerDownLeft className="h-3 w-3" /> 插入
                  <ChevronDown className="h-3 w-3" />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-44">
                {hasSelection && (
                  <DropdownMenuItem onClick={() => onApplySql(text, "replace_selection")}>
                    <CornerDownLeft className="mr-2 h-3.5 w-3.5 text-violet-500" />
                    替换所选内容
                  </DropdownMenuItem>
                )}
                <DropdownMenuItem onClick={() => onApplySql(text, "replace_all")}>
                  <CornerDownLeft className="mr-2 h-3.5 w-3.5 text-violet-500" />
                  全部替换
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => onApplySql(text, "insert_cursor")}>
                  <CornerDownLeft className="mr-2 h-3.5 w-3.5 text-violet-500" />
                  插入当前光标处
                </DropdownMenuItem>
                <DropdownMenuItem onClick={() => onApplySql(text, "append")}>
                  <CornerDownLeft className="mr-2 h-3.5 w-3.5 text-violet-500" />
                  追加到末尾
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          )}
          <Button size="sm" variant="ghost" className="h-5 gap-1 px-1.5 text-[10px] text-muted-foreground hover:text-foreground" onClick={copy} title="复制">
            {copied ? <Check className="h-3 w-3 text-emerald-500" /> : <Copy className="h-3 w-3" />}
            {copied ? "已复制" : "复制"}
          </Button>
        </div>
      </div>
      <pre className="max-h-[260px] overflow-auto px-3 py-2 font-mono text-[11px] leading-relaxed">
        <code>{text}</code>
      </pre>
    </div>
  )
}

// Markdown 渲染：AI 回答用 markdown 呈现（标题/列表/表格/代码块/粗体等），
// 代码块额外提供复制 + SQL 代码块「插入编辑器」操作。
function MarkdownContent({ content, hasSelection, schemaVerified, onApplySql }: {
  content: string
  hasSelection: boolean
  schemaVerified: boolean
  onApplySql: (sql: string, action: "replace_all" | "replace_selection" | "insert_cursor" | "append") => void
}) {
  const isSqlLang = (lang: string) => /^(sql|sqlite|mysql|mariadb|postgresql|pgsql|plsql|oracle|sqlserver|tsql)$/i.test(lang)
  return (
    <div className="ai-markdown">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        components={{
          // 块级代码块：拦截 <pre>，从内部 <code> 提取语言与代码，渲染带复制/插入工具栏的 CodeBlock。
          // 注意：不能在 <code> 组件里直接返回自定义块（会破坏 <pre> 结构导致嵌套无效 HTML），
          // 必须替换外层 <pre>。
          pre(props) {
            const { children } = props
            // react-markdown 块级代码的 pre 只包一个 code 元素；用 isValidElement 安全提取
            let lang = ""
            let raw = ""
            if (isValidElement(children)) {
              const childProps = children.props as { className?: string; children?: unknown }
              const m = /language-(\w+)/.exec(childProps.className || "")
              lang = m?.[1] ?? ""
              raw = String(childProps.children ?? "").replace(/\n$/, "")
            } else {
              raw = String(children ?? "").replace(/\n$/, "")
            }
            return <CodeBlock text={raw} lang={lang} isSql={isSqlLang(lang)} hasSelection={hasSelection} schemaVerified={schemaVerified} onApplySql={onApplySql} />
          },
          // 行内代码
          code(props) {
            const { children } = props
            return <code className="rounded bg-muted px-1 py-0.5 font-mono text-[11px]">{children}</code>
          },
          // 表格：加边框样式，窄面板下可横向滚动
          table(props) {
            const { children } = props
            return (
              <div className="my-1.5 overflow-x-auto">
                <table className="border-collapse text-[11px]">{children}</table>
              </div>
            )
          },
          th(props) {
            const { children } = props
            return <th className="border border-border bg-muted/50 px-2 py-1 text-left font-medium">{children}</th>
          },
          td(props) {
            const { children } = props
            return <td className="border border-border px-2 py-1">{children}</td>
          },
          // 链接：新窗口打开
          a(props) {
            const { href, children } = props
            return (
              <a href={href} target="_blank" rel="noreferrer" className="text-violet-600 underline">
                {children}
              </a>
            )
          },
        }}
      >
        {content}
      </ReactMarkdown>
    </div>
  )
}

export function AIPanel({ connId, db, tabId, onPreviewSql, hasSelection, quickRequest, onQuickConsumed, onClose }: AIPanelProps) {
  const [messages, setMessages] = useState<AiMessage[]>([])
  const [input, setInput] = useState("")
  const [streaming, setStreaming] = useState(false)
  const [sessionID, setSessionID] = useState<string>("")
  const [usage, setUsage] = useState<AIUsage | null>(null)
  const [procUsage, setProcUsage] = useState<AIUsage | null>(null)
  const [creating, setCreating] = useState(false)
  // agent 模式工具调用中间态提示（如"正在查询 sys_user 表结构…"）
  const [toolHint, setToolHint] = useState("")
  const stopRef = useRef<(() => void) | null>(null)
  const scrollRef = useRef<HTMLDivElement>(null)
  // 内联 diff 预览：展开预览的消息索引（-1 表示不展开）

  // 虚拟滚动器：只渲染可视区域内的消息，支持大量消息流畅滚动
  const virtualizer = useVirtualizer({
    count: messages.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => 120, // 估算每条消息高度（会根据实际渲染调整）
    overscan: 3, // 预渲染上下各 3 条，避免滚动白屏
    // 使用动态尺寸：根据实际渲染高度自动调整
    measureElement: (el) => el.getBoundingClientRect().height,
  })

  // 跟踪用户是否在底部（用于智能跟随滚动）
  const isAtBottomRef = useRef(true) // 默认认为在底部

  // 监听用户滚动行为：实时更新「是否在底部」状态
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return

    const threshold = 80 // 距离底部 80px 以内视为「在底部」
    
    const checkBottom = () => {
      const isNearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < threshold
      isAtBottomRef.current = isNearBottom
    }

    el.addEventListener('scroll', checkBottom, { passive: true })
    // 初始化时检查一次
    checkBottom()
    return () => el.removeEventListener('scroll', checkBottom)
  }, [])

  // 当消息内容变化时：如果用户之前在底部，则自动跟随滚动到底部
  useEffect(() => {
    const el = scrollRef.current
    if (!el || !isAtBottomRef.current) return

    // 用 rAF + setTimeout 双重确保 DOM 完全渲染后再滚动
    let cancelled = false
    const raf = requestAnimationFrame(() => {
      const timer = setTimeout(() => {
        if (!cancelled && isAtBottomRef.current) {
          // 虚拟滚动：滚动到最后一个虚拟项的底部
          const virtualItems = virtualizer.getVirtualItems()
          const lastItem = virtualItems[virtualItems.length - 1]
          if (lastItem) {
            el.scrollTop = lastItem.start + lastItem.size - el.clientHeight
          } else {
            el.scrollTop = el.scrollHeight
          }
        }
      }, 50) // 给 DOM 渲染留一点时间
      return () => clearTimeout(timer)
    })
    return () => {
      cancelled = true
      cancelAnimationFrame(raf)
    }
  }, [messages, virtualizer])

  // 拉取进程级 token 累计（每次 done 后也刷新）
  const refreshProcUsage = useCallback(() => {
    void api.getAIProcessUsage().then((r) => setProcUsage(r.processUsage)).catch(() => {})
  }, [])

  useEffect(() => {
    refreshProcUsage()
  }, [refreshProcUsage])

  // 关闭时停止流式（会话已持久化，保留不删除，重开可恢复历史）
  useEffect(() => {
    return () => {
      stopRef.current?.()
    }
  }, [])

  // 切换 tab / 连接 / 库时，重置本地会话状态（会话按 tab 隔离，需切换到对应 tab 的会话）。
  useEffect(() => {
    stopRef.current?.()
    setStreaming(false)
    setToolHint("")
    setSessionID("")
    setMessages([])
    setUsage(null)
  }, [connId, db, tabId])

  // 挂载 / 切换 tab 时恢复该 tab 的会话（按 connId + tabId 隔离）：
  // 拉取该 tab 的历史填充 UI，复用原 sessionID 继续多轮上下文（后端已持久化，刷新/重开不丢对话）。
  useEffect(() => {
    if (!connId || !tabId) return
    let cancelled = false
    void (async () => {
      try {
        const { sessions } = await api.listAISessions(connId, tabId)
        if (cancelled || sessions.length === 0) return
        const latest = sessions[0]
        const { session } = await api.getAISessionHistory(latest.id)
        if (cancelled) return
        // 过滤 system 消息，映射为 UI 消息（仅 user/assistant）。
        // user 消息优先用 extra.raw（原始纯 SQL / 需求文本）显示，并还原 action 标签，
        // 避免后端 ActionPrompt 拼装的指令长文本直接暴露给用户。
        const restored: AiMessage[] = (session.messages ?? [])
          .filter((m) => m.role === "user" || m.role === "assistant")
          .filter((m) => {
            // 过滤空消息：user 消息必须有 content 或 extra.raw；assistant 消息必须有 content
            if (m.role === "user") return !!(m.content || m.extra?.raw)
            return !!m.content
          })
          .map((m) => {
            if (m.role === "user") {
              const raw = m.extra?.raw || m.content
              const action = (m.extra?.action || "generate") as AiMode
              return { role: "user", content: raw, action }
            }
            return { role: "assistant", content: m.content }
          })
        if (restored.length === 0) return
        setSessionID(latest.id)
        setMessages(restored)
        if (session.usage) setUsage(session.usage)
      } catch {
        // 恢复失败不阻塞（如后端无会话记录），静默
      }
    })()
    return () => {
      cancelled = true
    }
  }, [connId, db, tabId])

  // 懒创建会话（首次发送时）
  const ensureSession = useCallback(async (): Promise<string> => {
    if (sessionID) return sessionID
    setCreating(true)
    try {
      const ses = await api.createAISession(connId, db, undefined, tabId)
      setSessionID(ses.sessionID)
      return ses.sessionID
    } finally {
      setCreating(false)
    }
  }, [sessionID, connId, db, tabId])

  // 把失败信息写进消息流：流式中的空回复直接填充，否则追加一条错误气泡
  const appendError = useCallback((msg: string) => {
    setMessages((m) => {
      const next = [...m]
      const last = next[next.length - 1]
      if (last && last.role === "assistant" && last.content === "" && !last.error) {
        next[next.length - 1] = { role: "assistant", content: msg, error: true }
      } else {
        next.push({ role: "assistant", content: msg, error: true })
      }
      return next
    })
  }, [])

  // 最新 messages 快照（供 doSend 内读取 history，避免 useCallback 闭包捕获过期值）
  const messagesRef = useRef(messages)
  messagesRef.current = messages

  // 跟踪是否正在流式输出（用于防止重复发送）
  const streamingRef = useRef(false)
  useEffect(() => {
    streamingRef.current = streaming
  }, [streaming])

  // 核心发送逻辑：text 为任务文本，action 为本次对话动作（generate/explain/fix/optimize）。
  // 与输入框状态解耦：既可被「发送」按钮调用，也可被外部一键修复触发。
  const doSend = useCallback(
    async (text: string, action: AiMode) => {
      const task = text.trim()
      if (!task || streamingRef.current) return
      setInput("")
      setMessages((m) => [...m, { role: "user", content: task, action }])
      setStreaming(true)
      setToolHint("")
      try {
        const sid = await ensureSession()
        setMessages((m) => [...m, { role: "assistant", content: "" }])
        // 会话失效时后端透明重建：带上 connId/db 和当前历史（不含刚追加的 user/空 assistant 占位）
        const history = messagesRef.current
          .filter((x) => x.content && !x.error)
          .map((x) => ({ role: x.role, content: x.content }))
        const stop = api.aiChatStream(
          sid,
          task,
          {
            onDelta: (delta) =>
              setMessages((m) => {
                const next = [...m]
                const last = next[next.length - 1]
                if (last && last.role === "assistant") next[next.length - 1] = { ...last, content: last.content + delta }
                return next
              }),
            onTool: (name, args) => {
              let hint = ""
              if (name === "list_databases") {
                hint = "正在列出可用的数据库…"
              } else if (name === "list_tables") {
                hint = fmtToolHint(args, (a) => `正在查询 ${a.db ?? ""} 的表列表…`, "正在查询表列表…")
              } else if (name === "get_schema") {
                hint = fmtToolHint(args, (a) => `正在查询 ${a.table ?? ""} 的表结构…`, "正在查询表结构…")
              } else {
                hint = `正在调用工具 ${name}…`
              }
              setToolHint(hint)
            },
            onDone: (u, schemaVerified) => {
              // 把「本轮已验证表结构」标记写入当前 assistant 消息，供 SQL 代码块可靠度图标展示
              setMessages((m) => {
                const next = [...m]
                const last = next[next.length - 1]
                if (last && last.role === "assistant") next[next.length - 1] = { ...last, schemaVerified }
                return next
              })
              setUsage(u)
              setStreaming(false)
              setToolHint("")
              stopRef.current = null
              refreshProcUsage()
            },
            onError: (msg) => {
              appendError(msg)
              toast.error(msg)
              setStreaming(false)
              setToolHint("")
              stopRef.current = null
            },
          },
          { connId, db, tabId, history },
          action,
        )
        stopRef.current = stop
      } catch (e) {
        const msg = (e as Error).message || "创建 AI 会话失败"
        appendError(msg)
        toast.error(msg)
        setStreaming(false)
        setToolHint("")
      }
    },
    [ensureSession, appendError, connId, db, tabId, refreshProcUsage],
  )

  // 错误气泡「重试」：回溯到错误前最后一条 user 消息，截断其后的失败对话并重新发送
  const retrySend = useCallback(
    (errIndex: number) => {
      if (streamingRef.current) return
      const msgs = messagesRef.current
      let userIdx = -1
      for (let i = errIndex - 1; i >= 0; i--) {
        if (msgs[i]?.role === "user") {
          userIdx = i
          break
        }
      }
      if (userIdx < 0) return
      const { content, action } = msgs[userIdx]
      // 截断该 user 消息之后的所有内容（含错误气泡），doSend 会重新追加 user/assistant；
      // 同步刷新 ref，避免 doSend 读取到含失败轮次的过期历史
      messagesRef.current = msgs.slice(0, userIdx)
      setMessages((m) => m.slice(0, userIdx))
      void doSend(content, action ?? "generate")
    },
    [doSend],
  )

  // 「发送」按钮：读取输入框文本 + 当前 action；解释/修复/优化自动带上当前编辑器 SQL
  const send = useCallback(async () => {
    const text = input.trim()
    if (!text || streaming) return
    // 输入框固定为「生成」场景：描述需求生成 SQL
    await doSend(text, "generate")
  }, [input, streaming, doSend])

  // 外部快捷触发（解释/优化/修复）：收到请求立即发送，无需输入框
  useEffect(() => {
    if (!quickRequest || !quickRequest.text.trim()) return
    // 流式中忽略外部请求，避免打断当前对话（父组件已清空，安全）
    if (streaming) return
    onQuickConsumed?.()
    void doSend(quickRequest.text, quickRequest.action)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [quickRequest])

  const stop = () => {
    stopRef.current?.()
    stopRef.current = null
    setStreaming(false)
    setToolHint("")
    // 模型尚未输出任何内容就停止时，移除空占位，避免留下空白 AI 卡片
    setMessages((m) => {
      const next = [...m]
      const last = next[next.length - 1]
      if (last && last.role === "assistant" && last.content === "" && !last.error) next.pop()
      return next
    })
  }

  // 请求应用到编辑器：交给父组件做编辑器内 diff 高亮 + 应用/取消确认
  const requestApply = (sql: string, action: "replace_all" | "replace_selection" | "insert_cursor" | "append") => onPreviewSql(sql, action)

  const reset = async () => {
    stopRef.current?.()
    setStreaming(false)
    if (sessionID) {
      try {
        await api.resetAISession(sessionID)
      } catch {
        /* 忽略 */
      }
    }
    setMessages([])
    setUsage(null)
    toast.success("AI 会话已重置")
  }

  return (
    <div className="flex h-full w-full flex-col border-l bg-background">
      {/* 头部：窄面板下不折行，标题/db/按钮按优先级收缩（按钮固定、标题不换、db 截断） */}
      <div className="flex items-center gap-1 border-b px-3 py-2">
        <div className="flex min-w-0 flex-1 items-center gap-1.5 overflow-hidden">
          <Sparkles className="h-4 w-4 shrink-0 text-violet-500" />
          <span className="shrink-0 text-sm font-semibold whitespace-nowrap">AI 助手</span>
          {db ? (
            <Badge variant="secondary" className="min-w-0 max-w-[100px] truncate font-mono text-[10px]" title={db}>
              {db}
            </Badge>
          ) : null}
        </div>
        <div className="flex shrink-0 items-center gap-0.5">
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={reset} title="重置会话">
            <RotateCcw className="h-3.5 w-3.5" />
          </Button>
          <Button variant="ghost" size="icon" className="h-7 w-7" onClick={onClose} title="关闭">
            <X className="h-4 w-4" />
          </Button>
        </div>
      </div>

      {/* 消息区 */}
      <div ref={scrollRef} className="scrollbar-thin flex-1 overflow-y-auto relative">
        {messages.length === 0 ? (
          <div className="flex flex-col items-center gap-2 pt-8 text-center text-xs text-muted-foreground">
            <Bot className="h-8 w-8 text-muted-foreground/50" />
            <p>描述需求生成 SQL，或咨询表结构 / 字段 / 关联关系等信息</p>
            <p className="text-[11px]">解释 / 优化 / 修复可在编辑器工具栏或报错卡片一键触发，生成的 SQL 请人工审核后再执行</p>
          </div>
        ) : (
          // 虚拟滚动容器：用总高度撑开滚动条
          <div
            style={{
              height: `${virtualizer.getTotalSize()}px`,
              width: "100%",
              position: "relative",
            }}
          >
            {/* 只渲染可视区域内的消息 */}
            {virtualizer.getVirtualItems().map((virtualItem) => {
              const m = messages[virtualItem.index]
              const i = virtualItem.index
              
              return (
                <div
                  key={m.role + i}
                  ref={virtualizer.measureElement} // 用于动态测量高度
                  data-index={virtualItem.index} // react-virtual 需要这个属性
                  style={{
                    position: "absolute",
                    top: 0,
                    left: 0,
                    width: "100%",
                    transform: `translateY(${virtualItem.start}px)`,
                  }}
                  className="p-3"
                >
                  {m.role === "user" ? (
                    <div className="flex justify-end">
                      <div className="max-w-[85%]">
                        {m.action && m.action !== "generate" ? (
                          <div className="mb-1 flex justify-end">
                            <span className="inline-flex items-center gap-1 rounded-full border border-violet-500/30 bg-violet-500/10 px-2 py-0.5 text-[10px] font-medium text-violet-700 dark:text-violet-300">
                              {m.action === "explain" ? (
                                <BookOpen className="h-3 w-3" />
                              ) : m.action === "optimize" ? (
                                <Gauge className="h-3 w-3" />
                              ) : (
                                <Wrench className="h-3 w-3" />
                              )}
                              {ACTION_LABEL[m.action]}
                            </span>
                          </div>
                        ) : null}
                        <div className="rounded-lg bg-primary/10 px-3 py-2 text-xs">
                          <CollapsibleContent maxHeight={240}>
                            <div className="whitespace-pre-wrap">{m.content}</div>
                          </CollapsibleContent>
                        </div>
                      </div>
                    </div>
                  ) : m.content || m.error || (streaming && i === messages.length - 1) ? (
                    <div className="flex justify-start">
                      <div
                        className={`max-w-[92%] rounded-lg border px-3 py-2 text-xs ${
                          m.error ? "border-destructive/40 bg-destructive/10 text-destructive" : "bg-muted/40"
                        }`}
                      >
                        <div className="mb-1 flex items-center gap-1.5 text-[10px] text-muted-foreground">
                          <Sparkles className={`h-3 w-3 ${m.error ? "text-destructive" : "text-violet-500"}`} />
                          AI
                        </div>
                        {m.error && (
                          <div className="mb-1.5 flex justify-start">
                            <Button
                              size="sm"
                              variant="outline"
                              className="h-6 gap-1 border-destructive/40 px-2 text-[11px] text-destructive hover:bg-destructive/10 hover:text-destructive"
                              disabled={streaming}
                              onClick={() => retrySend(i)}
                            >
                              <RotateCcw className="h-3 w-3" /> 重试
                            </Button>
                          </div>
                        )}
                        {(() => {
                          const parsed = parseThinking(m.content)
                          const isStreamingLast = streaming && i === messages.length - 1
                          return (
                            <>
                              {isStreamingLast ? <StreamingStatus toolHint={toolHint} hasAnswer={!!parsed.answer} /> : null}
                              {parsed.thinking ? (
                                <ThinkingBlock text={parsed.thinking} defaultOpen={isStreamingLast} />
                              ) : null}
                              {parsed.answer ? (
                                <CollapsibleContent live={isStreamingLast}>
                                  <MarkdownContent
                                    content={parsed.answer}
                                    hasSelection={!!hasSelection}
                                    schemaVerified={m.schemaVerified === true}
                                    onApplySql={requestApply}
                                  />
                                </CollapsibleContent>
                              ) : null}
                            </>
                          )
                        })()}
                      </div>
                    </div>
                  ) : null}
                </div>
              )
            })}
          </div>
        )}
      </div>

      <Separator />

      {/* 输入区 */}
      <div className="space-y-2 p-3">
        <Textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" && !e.shiftKey && !e.nativeEvent.isComposing) {
              e.preventDefault()
              void send()
            }
          }}
          rows={3}
          placeholder="描述需求生成 SQL，或咨询表结构。例：查询最近 30 天订单量 / sys_user 表有哪些字段"
          className="resize-none text-xs"
          disabled={creating}
        />
        <div className="flex items-center justify-between">
          <span className="text-[10px] text-muted-foreground">
            Enter 发送 / Shift+Enter 换行
          </span>
          <div className="flex items-center gap-1">
            <Button variant="ghost" size="icon" className="h-7 w-7" onClick={reset} title="清空会话">
              <Eraser className="h-3.5 w-3.5" />
            </Button>
            {streaming ? (
              <Button size="sm" variant="destructive" className="h-7 gap-1 text-xs" onClick={stop}>
                <Square className="h-3 w-3" /> 停止
              </Button>
            ) : (
              <Button size="sm" className="h-7 gap-1 text-xs" onClick={() => void send()} disabled={!input.trim() || creating}>
                {creating ? <Loader2 className="h-3 w-3 animate-spin" /> : <Bot className="h-3 w-3" />}
                发送
              </Button>
            )}
          </div>
        </div>
        {(usage && usage.total_tokens > 0) || (procUsage && procUsage.total_tokens > 0) ? (
          <div className="flex items-center gap-2 text-[10px] text-muted-foreground">
            {usage && usage.total_tokens > 0 ? (
              <span className="font-mono" title="本轮会话累计">本会话 {usage.total_tokens} token</span>
            ) : null}
            {procUsage && procUsage.total_tokens > 0 ? (
              <span className="font-mono text-violet-500" title="服务启动以来所有会话累计">
                进程累计 {procUsage.total_tokens}
              </span>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  )
}
