import { useMemo } from "react"
import { useTheme } from "next-themes"

export interface GridColors {
  /** 单元格默认底色 */
  base: string
  /** 斑马纹行底色 */
  zebra: string
  /** checkbox 选中行 / 右键聚焦行底色 */
  selected: string
}

/**
 * 响应式获取数据网格的三套底色。
 * 直接使用 CSS 变量字符串，由浏览器根据 <html class="dark"> 自动解析，
 * 避免 next-themes 初始水合时 resolvedTheme 尚未确定导致读到错误颜色。
 */
export function useGridColors(): GridColors {
  return useMemo(
    () => ({
      base: "hsl(var(--grid-row-base))",
      zebra: "hsl(var(--grid-row-zebra))",
      selected: "hsl(var(--grid-row-selected))",
    }),
    [],
  )
}

/** 当前是否暗色主题(供 Monaco、JsonView 等非 Tailwind 场景使用) */
export function useIsDark(): boolean {
  const { resolvedTheme } = useTheme()
  return resolvedTheme === "dark"
}
