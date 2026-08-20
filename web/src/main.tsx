import React from "react"
import ReactDOM from "react-dom/client"
import { ThemeProvider } from "next-themes"
import App from "./App"
import { ConfirmProvider } from "@/components/ui/alert-dialog"
import "@/lib/i18n" // 多语言初始化（必须在渲染前完成，避免首帧闪默认语言）
import "./index.css"

ReactDOM.createRoot(document.getElementById("root")!).render(
  <React.StrictMode>
    <ThemeProvider attribute="class" defaultTheme="system" enableSystem>
      <ConfirmProvider>
        <App />
      </ConfirmProvider>
    </ThemeProvider>
  </React.StrictMode>,
)
