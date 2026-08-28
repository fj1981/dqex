// 嵌入模式 postMessage 协议单一封装（docs/library-api-design.md 6.5.2）。
// 仅在嵌入模式（hash 以 #/embed/ 开头）下激活：普通模式不监听、不发送，零副作用。

// 嵌入入口约定：#/embed/<view>
const EMBED_HASH_PREFIX = "#/embed/"

// 判定当前是否处于嵌入模式（iframe 内以 #/embed/ 入口打开）
export function isEmbedMode(): boolean {
  return typeof window !== "undefined" && window.location.hash.startsWith(EMBED_HASH_PREFIX)
}

// 协议消息类型（data.type 字段；dqex: 前缀避免与宿主自身消息混淆）
export const EmbedMsgType = {
  ready: "dqex:ready", // iframe 加载完成 → 宿主
  init: "dqex:init", // 宿主 → iframe，注入初始上下文 { token?, lang?, theme?, config? }
  state: "dqex:state", // iframe → 宿主，状态变化 { key, value }
  action: "dqex:action", // iframe → 宿主，请求宿主动作 { type, payload }
  resize: "dqex:resize", // iframe → 宿主，高度自适应 { height }
  command: "dqex:command", // 宿主 → iframe，宿主下发指令 { type, payload }
  tokenExpired: "dqex:tokenExpired", // iframe → 宿主，API 401 时（宿主重新握手或刷新 src）
} as const

export type EmbedMsgName = (typeof EmbedMsgType)[keyof typeof EmbedMsgType]

// dqex:init 携带的初始上下文（token 走握手不落 URL，见设计文档 6.5.3 方案 b）
export interface EmbedInitPayload {
  token?: string
  lang?: string
  theme?: string
  config?: Record<string, unknown>
}

// ---- 宿主 origin 白名单 ----
// 从 URL query ?origin= 读取逗号分隔的宿主 origin 列表
// （如 ?origin=https://a.example.com,https://b.example.com#/embed/query）。
// 未提供时放行任意 origin：内网/同源部署（形态 B/C）的默认策略，
// 对外/跨域场景务必显式配置白名单（设计文档 6.5.4 安全约束）。
let originWhitelist: string[] | null = null
function allowedOrigins(): string[] {
  if (originWhitelist === null) {
    const raw = new URLSearchParams(window.location.search).get("origin") ?? ""
    originWhitelist = raw
      .split(",")
      .map((s) => s.trim())
      .filter(Boolean)
  }
  return originWhitelist
}

function originAllowed(origin: string): boolean {
  const list = allowedOrigins()
  // 未配置白名单：放行任意 origin（内网默认，见上）
  if (list.length === 0) return true
  return list.includes(origin)
}

// 首次校验通过的宿主 origin：记录后收紧后续发送的 targetOrigin（避免长期用 "*"）
let verifiedHostOrigin = ""

// 发送 targetOrigin：优先用已校验的宿主 origin；
// 尚未收到过宿主消息时，有白名单用首项，无白名单退回 "*"（内网默认）
function targetOrigin(): string {
  if (verifiedHostOrigin) return verifiedHostOrigin
  const list = allowedOrigins()
  return list.length > 0 ? list[0] : "*"
}

// ---- 收发封装 ----

// 向宿主发送消息；非嵌入模式直接忽略（普通模式零副作用）
export function send(type: EmbedMsgName, payload?: unknown): void {
  if (!isEmbedMode() || window.parent === window) return
  try {
    window.parent.postMessage({ type, payload }, targetOrigin())
  } catch {
    // 宿主窗口不可达（如 iframe 已被移除）时静默忽略
  }
}

type EmbedListener = (payload: unknown, event: MessageEvent) => void

const listeners = new Map<string, Set<EmbedListener>>()
let listening = false

// 挂载全局 message 监听（仅嵌入模式；首次订阅时执行一次）
function ensureListening(): void {
  if (listening || !isEmbedMode()) return
  listening = true
  window.addEventListener("message", (event: MessageEvent) => {
    // 仅接受直接父窗口的消息，忽略同层 iframe / window 自身
    if (event.source !== window.parent) return
    // origin 校验：不在白名单内的一律丢弃
    if (!originAllowed(event.origin)) return
    const data = event.data as { type?: unknown; payload?: unknown } | null
    if (!data || typeof data !== "object") return
    const type = data.type
    if (typeof type !== "string" || !type.startsWith("dqex:")) return
    // 校验通过：记录宿主 origin，后续发送使用精确 targetOrigin；
    // origin 为 "null"（sandbox/隐私模式等）时不记录，维持原策略
    if (event.origin && event.origin !== "null") verifiedHostOrigin = event.origin
    const set = listeners.get(type)
    if (!set) return
    for (const fn of set) fn(data.payload, event)
  })
}

// 订阅宿主消息（仅嵌入模式生效），返回取消订阅函数
export function onMessage(type: EmbedMsgName, listener: EmbedListener): () => void {
  ensureListening()
  let set = listeners.get(type)
  if (!set) {
    set = new Set()
    listeners.set(type, set)
  }
  set.add(listener)
  const owned = set
  return () => {
    owned.delete(listener)
  }
}

// ---- 便捷方法（均仅在嵌入模式下实际发送） ----

// iframe 就绪通知（EmbedShell 挂载后调用，宿主收到后发起 dqex:init 握手）
export const sendReady = (): void => send(EmbedMsgType.ready)

// 高度自适应通知（宿主据此调整 iframe 高度）
export const sendResize = (height: number): void => send(EmbedMsgType.resize, { height })

// 状态变化上报（如选表结果）
export const sendState = (key: string, value: unknown): void => send(EmbedMsgType.state, { key, value })

// 请求宿主执行动作（如关闭弹层）
export const sendAction = (actionType: string, payload?: unknown): void =>
  send(EmbedMsgType.action, { type: actionType, payload })

// 令牌过期上报：API 统一 401 处理点调用（api/index.ts），非嵌入模式自动忽略
export const notifyTokenExpired = (): void => send(EmbedMsgType.tokenExpired)
