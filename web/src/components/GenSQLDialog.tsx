// 快速生成 SQL 的预览弹窗：展示后端生成的 SQL 文本，提供复制 / 发送到查询页 / 关闭。
// 高度遵循「最大高度限制」原则（对齐单元格编辑弹窗）：弹窗整体限高，
// 内容区独立滚动——弹窗高度不与 SQL 长度绑定；标题栏可最大化查看超长 SQL。
import { useState } from "react"
import { Copy, Loader2, Maximize2, Minimize2, Send } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { cn } from "@/lib/utils"

interface Props {
  open: boolean
  title: string // 弹窗标题，如「生成 INSERT · sys_user」
  sql: string
  loading: boolean
  error: string
  onCopy: () => void
  onSendToQuery: () => void
  onClose: () => void
}

export default function GenSQLDialog({ open, title, sql, loading, error, onCopy, onSendToQuery, onClose }: Props) {
  const [maximized, setMaximized] = useState(false)
  const canSend = !loading && !error && sql.trim() !== ""

  return (
    <Dialog open={open} onOpenChange={(open) => { if (!open) { setMaximized(false); onClose() } }}>
      <DialogContent className={cn("max-w-[720px]", maximized && "h-[92vh] max-w-[96vw] flex flex-col")}>
        <DialogHeader className="shrink-0 pr-8">
          <DialogTitle className="flex items-center gap-2">
            <span className="flex-1 truncate">{title}</span>
            <Button
              variant="ghost"
              size="sm"
              className="h-6 w-6 p-0"
              title={maximized ? "还原" : "最大化"}
              onClick={() => setMaximized((m) => !m)}
            >
              {maximized ? <Minimize2 className="h-3.5 w-3.5" /> : <Maximize2 className="h-3.5 w-3.5" />}
            </Button>
          </DialogTitle>
        </DialogHeader>

        {/* SQL 内容区：最大高度限制 + 独立滚动（横向超宽不换行截断） */}
        <div className={cn("scrollbar-thin max-h-[60vh] overflow-auto rounded-md border bg-muted/20", maximized && "flex-1")}>
          {loading ? (
            <div className="flex items-center justify-center gap-1.5 py-8 text-xs text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 animate-spin" /> 生成中...
            </div>
          ) : error ? (
            <div className="m-2 flex items-start gap-1.5 rounded-md border border-destructive/40 bg-destructive/5 px-2 py-1.5 text-xs text-destructive">
              <span className="break-words">{error}</span>
            </div>
          ) : (
            <pre className="whitespace-pre p-2 font-mono text-xs leading-relaxed text-foreground">{sql}</pre>
          )}
        </div>

        <div className="mt-2 flex shrink-0 justify-end gap-2">
          <Button variant="ghost" size="sm" onClick={onClose}>关闭</Button>
          <Button variant="outline" size="sm" disabled={!canSend} onClick={onCopy}>
            <Copy className="mr-1 h-3.5 w-3.5" /> 复制
          </Button>
          <Button size="sm" disabled={!canSend} onClick={onSendToQuery}>
            <Send className="mr-1 h-3.5 w-3.5" /> 发送到查询页
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  )
}
