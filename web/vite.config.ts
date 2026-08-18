import { existsSync, readFileSync } from "fs"
import os from "os"
import path from "path"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// dev 模式：后端默认启用 token 认证，而 vite 页面 URL 没有 ?token=。
// 从数据目录 web-access.json 读令牌（该文件由后端启动时生成，作为 vite 的令牌桥接），
// 由代理层自动注入 /api 请求，使 http://localhost:5281 开箱即用（与生产行为一致：真实校验 token）
function devToken(): string {
  try {
    const file = path.join(os.homedir(), ".dbimpex", "web-access.json")
    if (!existsSync(file)) return ""
    const info = JSON.parse(readFileSync(file, "utf-8")) as { token?: string }
    return info.token || ""
  } catch {
    return ""
  }
}

export default defineConfig({
  plugins: [react()],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    port: 5281,
    // dev 默认打开 5281（若未打开则由 Vite 自动唤起浏览器）
    open: true,
    proxy: {
      "/api": {
        target: "http://localhost:8181",
        changeOrigin: true,
        configure: (proxy) => {
          proxy.on("proxyReq", (proxyReq) => {
            const token = devToken()
            if (!token) return
            // 请求头：前端已带则不覆盖
            if (!proxyReq.getHeader("X-Auth-Token") && !proxyReq.getHeader("Authorization")) {
              proxyReq.setHeader("X-Auth-Token", token)
            }
            // 查询参数：SSE/下载走 ?token=，前端未携带时补上
            const u = new URL(proxyReq.path, "http://placeholder")
            if (!u.searchParams.has("token")) {
              u.searchParams.set("token", token)
              proxyReq.path = u.pathname + u.search
            }
          })
        },
      },
    },
  },
  build: {
    outDir: "dist",
    // Monaco 本体 minified 后约 3~4MB，属本地工具可接受范围，调高阈值避免误报
    chunkSizeWarningLimit: 4000,
    rollupOptions: {
      output: {
        // 大体积第三方库拆独立 chunk：互不阻塞加载、便于浏览器长期缓存
        manualChunks: {
          monaco: ["monaco-editor", "@monaco-editor/react"],
          markdown: ["react-markdown", "remark-gfm"],
        },
      },
    },
  },
})
