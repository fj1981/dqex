import { useCallback, useEffect, useState } from "react"
import { ChevronRight, Folder, FolderOpen, Home, Loader2, RefreshCw, RotateCcw } from "lucide-react"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { browseDirs } from "@/api"
import type { DirBrowseResult } from "@/types"

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  /** 弹出时的起始路径；为空则从用户主目录开始 */
  initialPath?: string
  /** 确认选择目录 */
  onSelect: (path: string) => void
}

/**
 * 目录选择器：浏览本机目录并选择（仅目录，范围限制在用户主目录内）。
 * 点击子目录行即进入下级；"选择此文件夹"确认当前路径。
 */
export default function DirectoryPicker({ open, onOpenChange, initialPath, onSelect }: Props) {
  const [data, setData] = useState<DirBrowseResult | null>(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState("")

  const load = useCallback(async (path?: string) => {
    setLoading(true)
    setError("")
    try {
      setData(await browseDirs(path))
    } catch (e) {
      setError(e instanceof Error ? e.message : "加载目录失败")
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    if (open) load(initialPath)
  }, [open, initialPath, load])

  const enter = (path: string) => load(path)

  // 主目录显示为 ~（如 ~/project），避免暴露 /Users/xxx 原生路径
  const display = (p?: string) => {
    if (!p || !data?.root) return p
    if (p === data.root) return "~"
    const prefix = data.root.endsWith("/") ? data.root : `${data.root}/`
    return p.startsWith(prefix) ? `~/${p.slice(prefix.length)}` : p
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <FolderOpen className="h-4 w-4" />
            选择文件夹
          </DialogTitle>
        </DialogHeader>

        {/* 当前路径 + 操作 */}
        <div className="flex items-center gap-1.5">
          <Button variant="outline" size="sm" className="h-7 shrink-0 px-2" title="回到主目录" onClick={() => load(data?.root)}>
            <Home className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="outline"
            size="sm"
            className="h-7 shrink-0 px-2"
            title="上一级"
            disabled={!data?.parent || loading}
            onClick={() => data && load(data.parent)}
          >
            <RotateCcw className="h-3.5 w-3.5" />
          </Button>
          <Button variant="ghost" size="sm" className="h-7 shrink-0 px-2" title="刷新" onClick={() => load(data?.path)}>
            <RefreshCw className={`h-3.5 w-3.5 ${loading ? "animate-spin" : ""}`} />
          </Button>
          <div className="min-w-0 flex-1 truncate rounded-md bg-muted px-2 py-1 font-mono text-xs" title={data?.path}>
            {display(data?.path) || "加载中…"}
          </div>
        </div>

        {data && (
          <p className="text-[11px] text-muted-foreground">
            仅限浏览主目录
            <span className="mx-1">·</span>
            共 {data.entries.length} 个子目录
          </p>
        )}

        {/* 目录列表 */}
        <div className="h-56 overflow-y-auto rounded-md border">
          {error ? (
            <div className="flex h-full flex-col items-center justify-center gap-2 p-4 text-center">
              <p className="text-xs text-destructive">{error}</p>
              <Button variant="outline" size="sm" onClick={() => load(data?.path)}>
                重试
              </Button>
            </div>
          ) : data?.entries.length === 0 ? (
            <div className="flex h-full items-center justify-center p-4 text-xs text-muted-foreground">
              此文件夹下没有子目录
            </div>
          ) : (
            <ul className="p-1">
              {data?.entries.map((e) => (
                <li key={e.path}>
                  <button
                    type="button"
                    className="flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-xs hover:bg-accent"
                    onClick={() => enter(e.path)}
                    title={e.path}
                  >
                    <Folder className="h-3.5 w-3.5 shrink-0 text-amber-500" />
                    <span className="min-w-0 flex-1 truncate">{e.name}</span>
                    <ChevronRight className="h-3 w-3 shrink-0 text-muted-foreground" />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </div>

        <DialogFooter className="gap-2 sm:justify-between">
          <div className="min-w-0 flex-1 truncate font-mono text-[11px] text-muted-foreground" title={data?.path}>
            选择：{display(data?.path)}
          </div>
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => onOpenChange(false)}>
              取消
            </Button>
            <Button disabled={!data || loading} onClick={() => data && onSelect(data.path)}>
              {loading ? <Loader2 className="mr-1 h-4 w-4 animate-spin" /> : <FolderOpen className="mr-1 h-4 w-4" />}
              选择此文件夹
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
