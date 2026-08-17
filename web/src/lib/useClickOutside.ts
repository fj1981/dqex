import { useEffect, type RefObject } from "react"

// useClickOutside：当点击发生在 ref 元素外部时触发回调。
// 用于自定义浮层（列管理面板、过滤面板等）的「点击外部关闭」交互。
// 使用 mousedown 事件（比 click 更早，且与拖拽/右键不冲突）。
export function useClickOutside(ref: RefObject<HTMLElement | null>, onOutside: () => void, enabled = true) {
  useEffect(() => {
    if (!enabled) return
    const onMouseDown = (e: MouseEvent) => {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        onOutside()
      }
    }
    document.addEventListener("mousedown", onMouseDown)
    return () => document.removeEventListener("mousedown", onMouseDown)
  }, [ref, onOutside, enabled])
}
