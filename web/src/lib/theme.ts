import { useMemo } from "react"
import { useTheme } from "next-themes"

/**
 * 读取 CSS 变量的 HSL 三元组并拼成可用的颜色字符串。
 * 数据网格单元格的内联背景色无法使用 Tailwind 类,需经此函数读取主题变量。
 */
function readGridColor(varName: string): string {
  const raw = getComputedStyle(document.documentElement).getPropertyValue(varName).trim()
  return raw ? `hsl(${raw})` : "transparent"
}

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
 * 依赖 useTheme().resolvedTheme:主题切换时重新计算,无需监听 DOM。
 */
export function useGridColors(): GridColors {
  const { resolvedTheme } = useTheme()
  return useMemo(
    () => ({
      base: readGridColor("--grid-row-base"),
      zebra: readGridColor("--grid-row-zebra"),
      selected: readGridColor("--grid-row-selected"),
    }),
    [resolvedTheme],
  )
}

/** 当前是否暗色主题(供 Monaco、JsonView 等非 Tailwind 场景使用) */
export function useIsDark(): boolean {
  const { resolvedTheme } = useTheme()
  return resolvedTheme === "dark"
}
