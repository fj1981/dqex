import { useEffect, useMemo } from "react"
import { useParams } from "react-router-dom"
import { useTranslation } from "react-i18next"
import { useTheme } from "next-themes"
import type { ComponentType } from "react"

import { setAuthToken } from "@/api"
import { Toaster } from "@/components/ui/sonner"
import QueryView from "@/pages/QueryView"
import CompareView from "@/pages/CompareView"
import ExportView from "@/pages/ExportView"
import ImportView from "@/pages/ImportView"
import MigrateView from "@/pages/MigrateView"
import DictionaryView from "@/pages/DictionaryView"
import SnapshotView from "@/pages/SnapshotView"
import TaskView from "@/pages/TaskView"
import { useAppStore } from "@/stores/app"
import { useQueryStore } from "@/stores/queryStore"
import { changeUILang } from "@/lib/i18n"
import {
  EmbedMsgType,
  isEmbedMode,
  onMessage,
  sendReady,
  sendResize,
  type EmbedInitPayload,
} from "@/lib/embedBus"

// 嵌入视图白名单（docs/library-api-design.md 6.5.4.2 视图降权）：
// 仅开放数据操作视图，不暴露设置页/连接管理入口；白名单外的 view 渲染提示文案
const EMBED_VIEWS: Record<string, ComponentType> = {
  query: QueryView,
  compare: CompareView,
  export: ExportView,
  import: ImportView,
  migrate: MigrateView,
  dictionary: DictionaryView,
  snapshot: SnapshotView,
  task: TaskView,
  // 旧数据浏览入口映射到工作台（与主路由 /browser → /query 重定向保持一致）
  browser: QueryView,
}

// 轮询参数：解析连接/等待工作区恢复的最长等待时间（250ms × 40 ≈ 10s，超时放弃注入）
const INJECT_INTERVAL_MS = 250
const INJECT_MAX_TRIES = 40

// 嵌入模式精简壳：无侧边栏/页头/右侧面板/连接抽屉/About/Help，仅渲染白名单内的 View 组件
// （docs/library-api-design.md 6.5.2；宿主以 iframe 嵌入 #/embed/<view>）
export default function EmbedShell() {
  const { view } = useParams<{ view: string }>()
  const { t } = useTranslation()
  const { setTheme } = useTheme()
  const loadDBTypes = useAppStore((s) => s.loadDBTypes)
  const loadConnections = useAppStore((s) => s.loadConnections)

  // view 归一化：统一小写；browser 已在白名单中映射为工作台
  const normalized = (view ?? "").toLowerCase()
  const View = EMBED_VIEWS[normalized]

  // URL query 初始上下文（hash 之外的 ?... 部分，如 ?embed=1&conn=prod-mysql&db=orders#/embed/query）
  const connParam = useMemo(() => new URLSearchParams(window.location.search).get("conn") ?? "", [])
  const dbParam = useMemo(() => new URLSearchParams(window.location.search).get("db") ?? "", [])

  // 全局数据初始化：各 View 依赖 dbTypes / connections（与普通模式 App 的初始化一致）
  useEffect(() => {
    loadDBTypes()
    loadConnections()
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  // 通知宿主 iframe 已就绪（宿主收到 dqex:ready 后可发起 dqex:init 握手）
  useEffect(() => {
    sendReady()
  }, [])

  // 处理宿主 dqex:init：注入 token / 切换语言 / 切换主题
  useEffect(() => {
    return onMessage(EmbedMsgType.init, (payload) => {
      const p = (payload ?? {}) as EmbedInitPayload
      // token：注入 API 客户端，后续请求携带 X-Auth-Token（不落 sessionStorage/URL）
      if (p.token) setAuthToken(p.token)
      // lang：切换界面语言并持久化（与顶栏语言切换同源）
      if (p.lang) changeUILang(p.lang)
      // theme：跟随 next-themes（light/dark/system，持久化到 localStorage）
      if (p.theme === "light" || p.theme === "dark" || p.theme === "system") setTheme(p.theme)
      if (p.config) {
        // TODO: 复杂嵌入配置（如预置任务参数）按约定分发到对应视图，出现实际需求时再扩展
      }
    })
  }, [setTheme])

  // 高度自适应：内容尺寸变化时上报宿主（宿主据此 auto-resize iframe，设计文档 6.5.2）
  useEffect(() => {
    if (!isEmbedMode() || typeof ResizeObserver === "undefined") return
    let last = -1
    let raf = 0
    const report = () => {
      const h = Math.ceil(document.documentElement.getBoundingClientRect().height)
      if (h !== last) {
        last = h
        sendResize(h)
      }
    }
    // rAF 合帧：布局连续变化（如面板展开动画）时每帧最多上报一次
    const ro = new ResizeObserver(() => {
      cancelAnimationFrame(raf)
      raf = requestAnimationFrame(report)
    })
    ro.observe(document.body)
    report()
    return () => {
      ro.disconnect()
      cancelAnimationFrame(raf)
    }
  }, [])

  // URL query 初始上下文注入（仅工作台视图：其余视图的连接由各自任务配置选择，注入语义不明确）
  // conn 兼容连接 id / 名称 / 简称；db 写入恢复完成后的激活查询 tab（无 tab 时新建一个）
  useEffect(() => {
    if (normalized !== "query" || (!connParam && !dbParam)) return
    let tries = 0
    let connId = ""
    const timer = setInterval(() => {
      tries++
      const app = useAppStore.getState()
      const qs = useQueryStore.getState()
      // 第一步：解析连接（id 精确匹配 > name/shortName），需等 connections 加载完成
      if (!connId) {
        if (connParam) {
          const hit =
            app.connections.find((c) => c.id === connParam) ??
            app.connections.find((c) => c.name === connParam || c.shortName === connParam)
          if (hit) {
            connId = hit.id
            if (qs.connId !== connId) qs.setConnId(connId)
          } else if (tries >= INJECT_MAX_TRIES) {
            // 连接不存在/列表加载失败：放弃注入（工作台会回落到自动选择逻辑）
            clearInterval(timer)
            return
          }
        } else {
          // 仅 db 无 conn：沿用当前记住的连接；无连接（列表已加载仍为空）时放弃
          connId = qs.connId
          if (!connId && (app.connections.length > 0 || tries >= 8)) {
            clearInterval(timer)
            return
          }
        }
        if (!connId) return
      }
      // 第二步：等该连接的工作区异步恢复完成，把 db 写入激活查询 tab
      const s = useQueryStore.getState()
      if (s.connId !== connId) return
      if (!dbParam) {
        // 只注入连接：store 生效即完成
        clearInterval(timer)
        return
      }
      const active = s.tabs.find((x) => x.id === s.activeId)
      if (active && active.kind === "query") {
        if (active.db !== dbParam) s.updateTabDb(active.id, dbParam)
        clearInterval(timer)
        return
      }
      if (s.tabs.length === 0 && tries >= INJECT_MAX_TRIES) {
        // 恢复完成但无任何 tab：新建一个指向 db 的查询 tab
        s.addTab(dbParam)
        clearInterval(timer)
        return
      }
      if (tries >= INJECT_MAX_TRIES) clearInterval(timer)
    }, INJECT_INTERVAL_MS)
    return () => clearInterval(timer)
  }, [connParam, dbParam, normalized])

  // 白名单外的 view：简单提示（不渲染任何数据视图）
  if (!View) {
    return (
      <div className="flex h-screen items-center justify-center bg-background p-6">
        <div className="text-center text-sm text-muted-foreground">
          {t("embed.invalidView", { view: view ?? "" })}
        </div>
        <Toaster position="top-center" richColors />
      </div>
    )
  }

  // 仅渲染 View 本体：外层容器与普通模式 Layout 的 main 保持一致
  // （bg-muted/20 + p-6：QueryView 等用 -m-6 抵消内边距贴满内容区）
  return (
    <main className="scrollbar-thin h-screen w-full overflow-y-auto bg-muted/20 p-6">
      <View />
      <Toaster position="top-center" richColors />
    </main>
  )
}
