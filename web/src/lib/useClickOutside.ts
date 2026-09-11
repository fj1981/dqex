import { useEffect, type RefObject } from "react"

// useClickOutside：当点击发生在 ref 元素外部时触发回调。
// 用于自定义浮层（列管理面板、过滤面板等）的「点击外部关闭」交互。
// 使用 mousedown 事件（比 click 更早，且与拖拽/右键不冲突）。
export function useClickOutside(ref: RefObject<HTMLElement | null>, onOutside: () => void, enabled = true) {
  useEffect(() => {
    if (!enabled) return
    const onMouseDown = (e: MouseEvent) => {
      const target = e.target
      if (!(target instanceof Element)) return
      // Radix Select/DropdownMenu/Popover 的浮层内容通过 Portal 渲染到 body 下
      // （data-radix-popper-content-wrapper），DOM 上不在面板内；点击其选项
      // 不应被视为「点击外部」，否则面板会在下拉选择完成前被关闭。
      if (target.closest("[data-radix-popper-content-wrapper]")) return
      if (ref.current && !ref.current.contains(target)) {
        onOutside()
      }
    }
    // 必须用捕获阶段：点击 Radix 选项时，mousedown 冒泡到 document 之前
    // React 可能已同步提交选择并卸载 Portal 浮层（iframe 内焦点干涉会放大该窗口），
    // 此时 target 已 detach，closest 查不到 wrapper、contains 也为 false，
    // 冒泡阶段判定会误判为「点击外部」。捕获阶段在一切 React/DOM 变更前执行，
    // target 与浮层必然完好，判定可靠。
    document.addEventListener("mousedown", onMouseDown, true)
    return () => document.removeEventListener("mousedown", onMouseDown, true)
  }, [ref, onOutside, enabled])
}
