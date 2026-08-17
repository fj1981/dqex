"use client"

import * as React from "react"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"

interface ConfirmOptions {
  title?: string
  description?: React.ReactNode
  confirmText?: string
  cancelText?: string
  danger?: boolean // 危险操作：确认按钮用 destructive 样式
}

// 命令式 confirm：返回 Promise<boolean>，用户点确认 resolve(true)，取消/关闭 resolve(false)。
// 通过 ConfirmProvider 挂载的全局单例弹窗实现，替代原生 window.confirm。
interface ConfirmState extends Required<Omit<ConfirmOptions, "description">> {
  description: React.ReactNode
  resolve: (v: boolean) => void
}

let confirmHandler: ((opts: ConfirmOptions) => Promise<boolean>) | null = null

// confirm 供业务代码调用（先 await 挂载完成，一般由 ConfirmProvider 在应用启动时注册）
export function confirm(opts: ConfirmOptions = {}): Promise<boolean> {
  if (!confirmHandler) {
    // 兜底：ConfirmProvider 未挂载时回退原生 confirm
    return Promise.resolve(window.confirm(typeof opts.description === "string" ? opts.description : ""))
  }
  return confirmHandler(opts)
}

// ConfirmProvider 挂载在应用根，注册全局 confirm 实现。
export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = React.useState<ConfirmState | null>(null)

  React.useEffect(() => {
    confirmHandler = (opts: ConfirmOptions) =>
      new Promise<boolean>((resolve) => {
        setState({
          title: opts.title ?? "确认操作",
          description: opts.description ?? "",
          confirmText: opts.confirmText ?? "确认",
          cancelText: opts.cancelText ?? "取消",
          danger: opts.danger ?? false,
          resolve,
        })
      })
    return () => {
      confirmHandler = null
    }
  }, [])

  const close = (result: boolean) => {
    state?.resolve(result)
    setState(null)
  }

  return (
    <>
      {children}
      <Dialog open={state !== null} onOpenChange={(open) => !open && close(false)}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{state?.title}</DialogTitle>
            {state?.description != null && state.description !== "" && (
              <DialogDescription className="whitespace-pre-wrap break-words text-left">
                {state.description}
              </DialogDescription>
            )}
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => close(false)}>
              {state?.cancelText}
            </Button>
            <Button
              size="sm"
              variant={state?.danger ? "destructive" : "default"}
              onClick={() => close(true)}
            >
              {state?.confirmText}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
