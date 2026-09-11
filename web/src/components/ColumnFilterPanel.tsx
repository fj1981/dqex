import { useEffect, useRef, useState, type ReactNode } from "react"
import { createPortal } from "react-dom"
import { X } from "lucide-react"
import { useClickOutside } from "@/lib/useClickOutside"

const PANEL_W = 224 // 与原 w-56 一致
const EDGE = 8 // 视口安全边距
const GAP = 4 // 与漏斗按钮的间距（对应原 mt-1）

interface Props {
  // 锚点元素：漏斗按钮（点击打开时传 e.currentTarget）或列头 th（右键菜单打开时兜底查询）
  anchor: HTMLElement | null
  column: string
  onClose: () => void
  children: ReactNode
}

// 列过滤面板：Portal 到 body + fixed 定位。
// 原实现为 th 内 absolute 面板，受表格滚动容器 overflow 裁剪——列较窄时面板向左
// 溢出容器，左半被遮挡。改为按锚点矩形计算 fixed 坐标：默认右对齐漏斗（与原
// right-0 一致），左缘放不下改为左对齐向右展开，最后 clamp 在视口内；表格滚动
// （sticky 表头随内容水平移动）与窗口尺寸变化时实时重算对位。
export default function ColumnFilterPanel({ anchor, column, onClose, children }: Props) {
  const ref = useRef<HTMLDivElement>(null)
  const [pos, setPos] = useState<{ top: number; left: number } | null>(null)
  useClickOutside(ref, onClose)

  useEffect(() => {
    if (!anchor) return
    const place = () => {
      const r = anchor.getBoundingClientRect()
      // 垂直：默认锚点正下方；底部放不下翻到上方（首帧未挂载用估算高度）
      const h = ref.current?.offsetHeight ?? 170
      let top = r.bottom + GAP
      if (top + h > window.innerHeight - EDGE) top = Math.max(EDGE, r.top - h - GAP)
      // 水平：默认右对齐锚点；左缘溢出改为左对齐向右展开；最后 clamp 视口右缘
      let left = r.right - PANEL_W
      if (left < EDGE) left = r.left
      left = Math.max(EDGE, Math.min(left, window.innerWidth - PANEL_W - EDGE))
      setPos({ top, left })
    }
    place()
    // 面板挂载后实测高度再校正一次（首帧用估算高度，避免翻转判断偏差）
    const raf = requestAnimationFrame(place)
    // capture 捕获任意祖先/表格容器滚动；仅锚点仍在文档中时重算
    const onScroll = (e: Event) => {
      if (e.target instanceof Node && e.target.contains(anchor)) place()
    }
    document.addEventListener("scroll", onScroll, true)
    window.addEventListener("resize", place)
    return () => {
      cancelAnimationFrame(raf)
      document.removeEventListener("scroll", onScroll, true)
      window.removeEventListener("resize", place)
    }
  }, [anchor])

  if (!anchor || !pos) return null
  return createPortal(
    <div
      ref={ref}
      style={{ position: "fixed", top: pos.top, left: pos.left, width: PANEL_W }}
      className="z-50 rounded-md border bg-popover p-2 shadow-md"
      onClick={(e) => e.stopPropagation()}
    >
      <div className="mb-1.5 flex items-center justify-between">
        <span className="truncate text-xs font-semibold text-foreground">{column}</span>
        <button type="button" className="text-muted-foreground hover:text-foreground" onClick={onClose}>
          <X className="h-3.5 w-3.5" />
        </button>
      </div>
      {children}
    </div>,
    document.body
  )
}
