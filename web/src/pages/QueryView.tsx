import WorkspaceLayout from "@/components/WorkspaceLayout"

// 查询浏览（终端 + 数据浏览合并）：类 Navicat 工作区（对象浏览 tab + 查询 tab + 表数据 tab）
// 连接选择以内联紧凑下拉形式置于顶部连接栏，不再使用外层 ConnectionSelect 大卡片
export default function QueryView() {
  return (
    // 用负 margin 抵消外层 main 的 p-6 内边距，让工作区贴满整个内容区
    <div className="-m-6 flex h-[calc(100%+3rem)] min-h-0 flex-col">
      <div className="min-h-0 flex-1 overflow-hidden bg-background">
        <WorkspaceLayout />
      </div>
    </div>
  )
}