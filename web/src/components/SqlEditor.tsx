import Editor, { type OnMount } from "@monaco-editor/react"
import { useEffect, useMemo, useRef } from "react"
import { cn } from "@/lib/utils"
import { useIsDark } from "@/lib/theme"
import { monaco } from "@/lib/monaco"
import { formatEditorSQL } from "@/lib/sqlFormat"

interface Props {
  value: string
  onChange: (sql: string) => void
  onRun: (selection?: string) => void
  disabled?: boolean
  // 保留 placeholder 参数以兼容调用方；Monaco 原生不内置 placeholder，
  // 空状态引导文案由下方结果区展示，不在编辑器内部叠加。
  placeholder?: string
  className?: string
  // diffBase：AI 采纳预览的基准文本（原 SQL）。非空时编辑器对「基准 → 当前 value」
  // 做行级 diff 高亮：新增行绿底、删除行红底，用于「应用到编辑器」的确认交互。
  diffBase?: string
  // onReady：编辑器挂载后回调，暴露编辑器实例（父组件用于读取光标/选中，实现 AI 插入定位）
  onReady?: (ed: Parameters<OnMount>[0]) => void
  // onSelectionChange：光标/选中变化时回调，上报当前选中状态（供 AI 插入菜单默认项判断）
  onSelectionChange?: (info: { hasSelection: boolean; selectionText: string; cursorOffset: number; selectionOffset: number; selectionLength: number }) => void
}

// 编辑器实例类型（简化）：仅需 getModel / getSelection / getPosition / getValue 等常用方法
export type SqlEditorInstance = Parameters<OnMount>[0]

// 行级 diff：返回 base 中新增（相对 target 缺失）的行号集合
// 简化实现：以 target 的每一行为锚，在 base 中顺序匹配，未匹配到的 base 行视为「删除行」。
// 足够用于 SQL 采纳预览的视觉提示，无需完整 LCS。
function computeDiff(base: string, target: string): { added: number[]; removed: number[] } {
  const bl = base.split("\n")
  const tl = target.split("\n")
  const removed = new Set<number>()
  const added = new Set<number>()
  // base 中的行：若在 target 中找不到（按顺序贪心），标记为删除
  let ti = 0
  for (let bi = 0; bi < bl.length; bi++) {
    const line = bl[bi].trim()
    // 在 target 中从 ti 开始找同内容行
    let found = -1
    for (let j = ti; j < tl.length; j++) {
      if (tl[j].trim() === line) {
        found = j
        break
      }
    }
    if (found >= 0) {
      ti = found + 1
    } else if (line !== "") {
      removed.add(bi + 1) // Monaco 行号从 1 开始
    }
  }
  // target 中的行：未在 base 中匹配到的（按顺序）标记为新增
  let bi = 0
  for (let i = 0; i < tl.length; i++) {
    const line = tl[i].trim()
    let found = -1
    for (let j = bi; j < bl.length; j++) {
      if (bl[j].trim() === line) {
        found = j
        break
      }
    }
    if (found >= 0) {
      bi = found + 1
    } else if (line !== "") {
      added.add(i + 1)
    }
  }
  return { added: [...added], removed: [...removed] }
}

// SQL 编辑器：Monaco（VS Code 内核）。语言注册 + worker 本地化见 @/lib/monaco。
// 保留 Cmd/Ctrl+Enter 执行、Tab 插入空格、行号、语法高亮与自动补全。
// 选中执行（Navicat 式）：有选中文本时只执行选中部分，无选中时执行整个编辑器内容。
// diffBase 非空时启用行级 diff 高亮（绿=新增，红=删除），供 AI 采纳预览。
export default function SqlEditor({ value, onChange, onRun, disabled, className, diffBase, onReady, onSelectionChange }: Props) {
  const { KeyCode, KeyMod } = monaco
  const isDark = useIsDark()
  const decoRef = useRef<string[]>([])
  const editorRef = useRef<Parameters<OnMount>[0] | null>(null)

  // 应用 diff 高亮：依据 diffBase/value 计算行级差异并更新 decorations。
  // 注意：只高亮「新增行」（绿），「删除行」在新文本（value）中不存在，
  // 其行号会越界导致 Monaco 抛异常，故不渲染整行高亮。
  const applyDiffTo = (ed: Parameters<OnMount>[0]) => {
    const model = ed.getModel()
    if (!model) return
    if (!diffBase) {
      decoRef.current = ed.deltaDecorations(decoRef.current, [])
      return
    }
    const { added } = computeDiff(diffBase, value)
    const totalLines = model.getLineCount()
    const decos: monaco.editor.IModelDeltaDecoration[] = []
    for (const ln of added) {
      if (ln < 1 || ln > totalLines) continue // 防御：越界行号跳过
      decos.push({
        range: new monaco.Range(ln, 1, ln, 1),
        options: { isWholeLine: true, className: "ai-diff-added", linesDecorationsClassName: "ai-diff-added-gutter" },
      })
    }
    decoRef.current = ed.deltaDecorations(decoRef.current, decos)
  }

  // 编辑器挂载后：注入 Cmd/Ctrl+Enter 执行快捷键，初始化 diff 高亮，并暴露实例/监听选中。
  // 关键：所有「操作编辑器内部状态」的动作统一延后到下一帧，避免在 Monaco 的 layout effect
  // 挂载阶段同步调用 deltaDecorations/getModel 等，否则会抛异常导致整个编辑器崩溃。
  const handleMount: OnMount = (ed) => {
    editorRef.current = ed
    ed.addCommand(KeyMod.CtrlCmd | KeyCode.Enter, () => {
      const sel = ed.getSelection()
      const model = ed.getModel()
      if (sel && model && !sel.isEmpty()) {
        onRun(model.getValueInRange(sel))
      } else {
        onRun()
      }
    })

    // Shift+Alt+F：SQL 格式化（有选中格式化选中部分，无选中格式化全文）
    ed.addCommand(KeyMod.Shift | KeyMod.Alt | KeyCode.KeyF, () => {
      formatEditorSQL(ed)
    })

    // 光标/选中变化时上报（供 AI 插入菜单默认项判断）
    const report = () => {
      const sel = ed.getSelection()
      const model = ed.getModel()
      if (!sel || !model) return
      const hasSelection = !sel.isEmpty()
      const selectionText = hasSelection ? model.getValueInRange(sel) : ""
      const selectionOffset = hasSelection ? model.getOffsetAt(sel.getStartPosition()) : -1
      const selectionLength = hasSelection ? selectionText.length : 0
      const cursorOffset = model.getOffsetAt(sel.getPosition())
      const info = { hasSelection, selectionText, cursorOffset, selectionOffset, selectionLength }
      queueMicrotask(() => onSelectionChange?.(info))
    }
    ed.onDidChangeCursorSelection(report)

    // 立即暴露实例 + 上报一次光标：父组件（AI 插入定位）依赖 sqlEditorRef.current，
    // 若延后到下一帧，AI 面板开合导致编辑器 remount 时，用户在这一帧窗口内点击「插入」
    // 会拿到 null 实例，进而光标定位失败。onReady 仅赋值 ref，不触碰 Monaco 内部状态，
    // 同步调用是安全的。真正需延后的 deltaDecorations/getModel（diff 高亮）仍在下一帧执行。
    onReady?.(ed)
    report()

    // 挂载后的初始化动作延后到下一帧，确保 Monaco 完全就绪
    requestAnimationFrame(() => {
      ed.focus()
      applyDiffTo(ed)
      report()
    })
  }

  // diffBase / value 变化时重算 decorations
  useEffect(() => {
    const ed = editorRef.current
    if (ed) applyDiffTo(ed)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [diffBase, value])

  // options 用 useMemo 稳定引用：@monaco-editor/react 的 <Editor> 以 options 引用作为
  // effect 依赖（updateOptions），内联对象会导致每次父组件重渲染都触发 updateOptions，
  // 在编辑器未就绪的窗口期可能访问 undefined 抛错。
  const editorOptions = useMemo(
    () => ({
      readOnly: disabled,
      fontSize: 13,
      lineHeight: 20,
      fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
      minimap: { enabled: false },
      lineNumbers: "on" as const,
      wordWrap: "off" as const,
      scrollBeyondLastLine: false,
      automaticLayout: true,
      tabSize: 4,
      insertSpaces: true,
      renderWhitespace: "none" as const,
      folding: true,
      padding: { top: 8, bottom: 8 },
      suggest: { showKeywords: true, showSnippets: true },
    }),
    [disabled],
  )

  return (
    <div className={cn("min-h-0 flex-1 overflow-hidden", className)}>
      <Editor
        language="sql"
        value={value}
        theme={isDark ? "vs-dark" : "vs"}
        onChange={(v) => onChange(v ?? "")}
        onMount={handleMount}
        options={editorOptions}
        loading={<div className="p-3 text-xs text-muted-foreground">加载编辑器…</div>}
      />
    </div>
  )
}
