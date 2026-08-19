import Editor, { DiffEditor, type OnMount } from "@monaco-editor/react"
import { useMemo, useRef } from "react"
import { Check, X } from "lucide-react"
import { cn } from "@/lib/utils"
import { useIsDark } from "@/lib/theme"
import { monaco } from "@/lib/monaco"
import { formatEditorSQL } from "@/lib/sqlFormat"

interface Props {
  value: string
  onChange: (sql: string) => void
  onRun: (selection?: string) => void
  disabled?: boolean
  placeholder?: string
  className?: string
  // diffBase：AI 采纳预览的基准文本（原 SQL）。非空时编辑器切换为 DiffEditor 对比模式，
  // 右上角悬浮「应用/取消」按钮；应用=确认替换，取消=还原为 diffBase。
  diffBase?: string
  onReady?: (ed: Parameters<OnMount>[0]) => void
  onSelectionChange?: (info: { hasSelection: boolean; selectionText: string; cursorOffset: number; selectionOffset: number; selectionLength: number }) => void
  // onApply：用户点击「应用」，确认当前内容（父组件清除 diffBase 退出对比模式）
  onApply?: () => void
  // onCancel：用户点击「取消」，父组件将编辑器内容还原为 diffBase
  onCancel?: () => void
}

export type SqlEditorInstance = Parameters<OnMount>[0]

const FONT = "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace"

// SQL 编辑器：Monaco（VS Code 内核）。语言注册 + worker 本地化见 @/lib/monaco。
// 保留 Cmd/Ctrl+Enter 执行、Tab 插入空格、行号、语法高亮与自动补全。
// 选中执行（Navicat 式）：有选中文本时只执行选中部分，无选中时执行整个编辑器内容。
// diffBase 非空时切换为 DiffEditor inline 对比模式 + 右下角悬浮「应用/取消」。
export default function SqlEditor({ value, onChange, onRun, disabled, className, diffBase, onReady, onSelectionChange, onApply, onCancel }: Props) {
  const { KeyCode, KeyMod } = monaco
  const isDark = useIsDark()
  const decoRef = useRef<string[]>([])
  const editorRef = useRef<Parameters<OnMount>[0] | null>(null)

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
    // 同步调用是安全的。
    onReady?.(ed)
    report()

    // 挂载后的初始化动作延后到下一帧，确保 Monaco 完全就绪
    requestAnimationFrame(() => {
      ed.focus()
      report()
    })
  }

  // options 用 useMemo 稳定引用：@monaco-editor/react 的 <Editor> 以 options 引用作为
  // effect 依赖（updateOptions），内联对象会导致每次父组件重渲染都触发 updateOptions，
  // 在编辑器未就绪的窗口期可能访问 undefined 抛错。
  const editorOptions = useMemo(
    () => ({
      readOnly: disabled,
      fontSize: 13,
      lineHeight: 20,
      fontFamily: FONT,
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

  // DiffEditor options 同样用 useMemo 稳定引用
  const diffOptions = useMemo(
    () => ({
      readOnly: true, // 对比模式只读，确认靠右上角按钮
      renderSideBySide: false, // inline 模式：单列显示，红=删除 绿=新增
      ignoreTrimWhitespace: true,
      enableSplitViewResizing: false,
      automaticLayout: true,
      scrollBeyondLastLine: false,
      fontSize: 13,
      lineHeight: 20,
      fontFamily: FONT,
      minimap: { enabled: false },
      renderOverviewRuler: false,
    }),
    [],
  )

  // 对比模式：DiffEditor + 右下角悬浮「应用/取消」
  if (diffBase !== undefined && diffBase !== "") {
    return (
      <div className={cn("relative min-h-0 flex-1 overflow-hidden", className)}>
        <div className="absolute bottom-3 right-3 z-20 flex items-center gap-1.5 rounded-lg border bg-background/95 px-2 py-1.5 shadow-md backdrop-blur">
          <button
            type="button"
            onClick={() => onCancel?.()}
            className="flex items-center gap-1 rounded px-2 py-1 text-[11px] font-medium text-muted-foreground transition-colors hover:bg-muted"
            title="放弃修改，还原为原内容"
          >
            <X className="h-3 w-3" />
            取消
          </button>
          <div className="h-4 w-px bg-border" />
          <button
            type="button"
            onClick={() => onApply?.()}
            className="flex items-center gap-1 rounded bg-green-600 px-2 py-1 text-[11px] font-medium text-white transition-colors hover:bg-green-700"
            title="确认应用修改"
          >
            <Check className="h-3 w-3" />
            应用
          </button>
        </div>
        <DiffEditor
          original={diffBase}
          modified={value}
          language="sql"
          theme={isDark ? "vs-dark" : "vs"}
          options={diffOptions}
          className="sql-diff-inline"
          loading={<div className="p-3 text-xs text-muted-foreground">加载对比视图…</div>}
        />
      </div>
    )
  }

  // 普通编辑模式
  return (
    <div className={cn("relative min-h-0 flex-1 overflow-hidden", className)}>
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
