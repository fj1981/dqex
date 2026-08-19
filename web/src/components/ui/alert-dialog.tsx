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
import { Input } from "@/components/ui/input"

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

// ---- 命令式 prompt：带输入框，返回 Promise<string | null> ----
// 用户点确认 resolve(输入值 trim 后)；取消/关闭 resolve(null)。
// 默认值通过 defaultValue 预填，打开时自动全选便于直接覆盖或回车保存。
interface PromptOptions {
  title?: string
  description?: React.ReactNode
  defaultValue?: string
  placeholder?: string
  confirmText?: string
  cancelText?: string
  // 输入为空（trim 后）时拒绝确认并提示；留空表示允许空值
  required?: React.ReactNode
}

interface PromptState {
  title: string
  description: React.ReactNode
  defaultValue: string
  placeholder: string
  confirmText: string
  cancelText: string
  required: React.ReactNode
  resolve: (v: string | null) => void
}

let promptHandler: ((opts: PromptOptions) => Promise<string | null>) | null = null

// prompt 供业务代码调用（需先挂载 PromptProvider；未挂载时回退原生 prompt）
export function prompt(opts: PromptOptions = {}): Promise<string | null> {
  if (!promptHandler) {
    return Promise.resolve(window.prompt(typeof opts.description === "string" ? opts.description : "") ?? null)
  }
  return promptHandler(opts)
}

// ConfirmProvider 挂载在应用根，同时注册全局 confirm 与 prompt 实现。
export function ConfirmProvider({ children }: { children: React.ReactNode }) {
  const [state, setState] = React.useState<ConfirmState | null>(null)
  const [promptState, setPromptState] = React.useState<PromptState | null>(null)
  const [promptValue, setPromptValue] = React.useState("")
  const promptInputRef = React.useRef<HTMLInputElement | null>(null)

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
    promptHandler = (opts: PromptOptions) =>
      new Promise<string | null>((resolve) => {
        setPromptValue(opts.defaultValue ?? "")
        setPromptState({
          title: opts.title ?? "输入",
          description: opts.description ?? "",
          defaultValue: opts.defaultValue ?? "",
          placeholder: opts.placeholder ?? "",
          confirmText: opts.confirmText ?? "确认",
          cancelText: opts.cancelText ?? "取消",
          required: opts.required,
          resolve,
        })
      })
    return () => {
      confirmHandler = null
      promptHandler = null
    }
  }, [])

  const close = (result: boolean) => {
    state?.resolve(result)
    setState(null)
  }

  const closePrompt = (result: string | null) => {
    promptState?.resolve(result)
    setPromptState(null)
    setPromptValue("")
  }

  const submitPrompt = () => {
    if (!promptState) return
    const v = promptValue.trim()
    if (promptState.required && v === "") {
      // 不关闭，仅给输入框聚焦提示
      promptInputRef.current?.focus()
      return
    }
    closePrompt(v)
  }

  // 弹窗打开后自动聚焦并全选默认值，便于直接覆盖输入或回车保存
  React.useEffect(() => {
    if (promptState && promptInputRef.current) {
      const el = promptInputRef.current
      el.focus()
      el.select()
    }
  }, [promptState])

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

      <Dialog open={promptState !== null} onOpenChange={(open) => !open && closePrompt(null)}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>{promptState?.title}</DialogTitle>
            {promptState?.description != null && promptState.description !== "" && (
              <DialogDescription className="whitespace-pre-wrap break-words text-left">
                {promptState.description}
              </DialogDescription>
            )}
          </DialogHeader>
          <div className="py-1">
            <Input
              ref={promptInputRef}
              value={promptValue}
              placeholder={promptState?.placeholder}
              onChange={(e) => setPromptValue(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  e.preventDefault()
                  submitPrompt()
                }
                if (e.key === "Escape") {
                  e.preventDefault()
                  closePrompt(null)
                }
              }}
            />
            {promptState?.required && promptValue.trim() === "" && (
              <div className="mt-1 text-[11px] text-destructive">{promptState.required}</div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => closePrompt(null)}>
              {promptState?.cancelText}
            </Button>
            <Button size="sm" onClick={submitPrompt}>
              {promptState?.confirmText}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}
