"use client"

import * as React from "react"
import * as ContextMenuPrimitive from "@radix-ui/react-context-menu"

import { cn } from "@/lib/utils"

const ContextMenu = ContextMenuPrimitive.Root

const ContextMenuTrigger = ContextMenuPrimitive.Trigger

// onPlaced 运行时由 radix 透传给底层 PopperContent（定位完成回调），
// 但 radix 的 ContextMenuContent 类型定义里已将其 Omit 掉，此处显式补回类型，
// 用于「定位完成前保持透明，避免菜单从 Portal 初始位置（视口左上）闪现」。
type ContextMenuContentProps = React.ComponentPropsWithoutRef<typeof ContextMenuPrimitive.Content> & {
  onPlaced?: () => void
}

const ContextMenuContent = React.forwardRef<
  React.ElementRef<typeof ContextMenuPrimitive.Content>,
  ContextMenuContentProps
>(({ className, onPlaced, ...props }, ref) => {
  const [placed, setPlaced] = React.useState(false)
  return (
    <ContextMenuPrimitive.Portal>
      <ContextMenuPrimitive.Content
        ref={ref}
        // radix 类型缺 onPlaced，但运行时透传到底层 PopperContent；用断言保留原防闪现能力
        {...({ onPlaced: () => {
          setPlaced(true)
          onPlaced?.()
        } } as React.ComponentPropsWithoutRef<typeof ContextMenuPrimitive.Content>)}
        // floating-ui 定位完成前保持透明，避免菜单从 Portal 初始位置（视口左上）闪现后跳到鼠标点
        className={cn(
          "z-50 min-w-[8rem] overflow-hidden rounded-md border bg-popover p-1 text-popover-foreground shadow-md",
          placed ? "animate-in fade-in-0 zoom-in-95" : "opacity-0",
          className
        )}
        {...props}
      />
    </ContextMenuPrimitive.Portal>
  )
})
ContextMenuContent.displayName = ContextMenuPrimitive.Content.displayName

const ContextMenuItem = React.forwardRef<
  React.ElementRef<typeof ContextMenuPrimitive.Item>,
  React.ComponentPropsWithoutRef<typeof ContextMenuPrimitive.Item>
>(({ className, ...props }, ref) => (
  <ContextMenuPrimitive.Item
    ref={ref}
    className={cn(
      "relative flex cursor-default select-none items-center rounded-sm px-2 py-1.5 text-sm outline-none focus:bg-accent focus:text-accent-foreground data-[disabled]:pointer-events-none data-[disabled]:opacity-50",
      className
    )}
    {...props}
  />
))
ContextMenuItem.displayName = ContextMenuPrimitive.Item.displayName

const ContextMenuSeparator = React.forwardRef<
  React.ElementRef<typeof ContextMenuPrimitive.Separator>,
  React.ComponentPropsWithoutRef<typeof ContextMenuPrimitive.Separator>
>(({ className, ...props }, ref) => (
  <ContextMenuPrimitive.Separator
    ref={ref}
    className={cn("-mx-1 my-1 h-px bg-border", className)}
    {...props}
  />
))
ContextMenuSeparator.displayName = ContextMenuPrimitive.Separator.displayName

export {
  ContextMenu,
  ContextMenuTrigger,
  ContextMenuContent,
  ContextMenuItem,
  ContextMenuSeparator,
}
