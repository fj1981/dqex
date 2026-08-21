import type { ReactNode } from "react"
import { MoveRight } from "lucide-react"
import ConnectionSelect from "@/components/ConnectionSelect"

// 数据源卡片统一宽度基准（px）：
// CONN_SINGLE_W 供单卡页（导出/导入）容器使用；
// CONN_PAIR_W = 380×2 + 箭头列 36 + gap 3×8 = 820，供双卡布局使用，
// 保证所有任务页的数据源卡片尺寸完全一致
export const CONN_SINGLE_W = "max-w-[380px]"
export const CONN_PAIR_W = "max-w-[820px]"

interface ConnProps {
  title: string
  subtitle?: string
  value: string
  onChange: (id: string) => void
}

interface Props {
  source: ConnProps
  target: ConnProps
  /** 卡片下方内容（提示条/底部导航），与卡片同宽居中 */
  children?: ReactNode
}

// 数据源成对选择布局（源 ↔ 目标）：迁移/对比页共用；
// 默认 stretch + 卡片 h-full，两侧卡片等宽等高
export default function ConnectionPair({ source, target, children }: Props) {
  return (
    <div className={`mx-auto w-full space-y-4 ${CONN_PAIR_W}`}>
      {/* 窗口宽度不足时双卡区域横向滚动，避免目标卡片被裁剪；下方提示/按钮不随动 */}
      <div className="scrollbar-thin overflow-x-auto pb-1">
        <div className="grid min-w-[600px] grid-cols-[1fr_auto_1fr] gap-3">
          <ConnectionSelect {...source} fill />
          <div className="flex h-11 items-center justify-center self-center">
            <span className="flex h-9 w-9 items-center justify-center rounded-full border bg-background text-muted-foreground shadow-sm">
              <MoveRight className="h-5 w-5" />
            </span>
          </div>
          <ConnectionSelect {...target} fill />
        </div>
      </div>
      {children}
    </div>
  )
}
