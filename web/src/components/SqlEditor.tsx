import Editor, { type OnMount } from "@monaco-editor/react"
import { cn } from "@/lib/utils"
import { monaco } from "@/lib/monaco"

interface Props {
  value: string
  onChange: (sql: string) => void
  onRun: (selection?: string) => void
  disabled?: boolean
  // 保留 placeholder 参数以兼容调用方；Monaco 原生不内置 placeholder，
  // 空状态引导文案由下方结果区展示，不在编辑器内部叠加。
  placeholder?: string
  className?: string
}

// SQL 编辑器：Monaco（VS Code 内核）。语言注册 + worker 本地化见 @/lib/monaco。
// 保留 Cmd/Ctrl+Enter 执行、Tab 插入空格、行号、语法高亮与自动补全。
// 选中执行（Navicat 式）：有选中文本时只执行选中部分，无选中时执行整个编辑器内容。
export default function SqlEditor({ value, onChange, onRun, disabled, className }: Props) {
  const { KeyCode, KeyMod } = monaco

  // 编辑器挂载后：注入 Cmd/Ctrl+Enter 执行快捷键（传出选中文本）
  const handleMount: OnMount = (ed) => {
    ed.addCommand(KeyMod.CtrlCmd | KeyCode.Enter, () => {
      const sel = ed.getSelection()
      const model = ed.getModel()
      if (sel && model && !sel.isEmpty()) {
        onRun(model.getValueInRange(sel))
      } else {
        onRun()
      }
    })
    ed.focus()
  }

  return (
    <div className={cn("min-h-0 flex-1 overflow-hidden", className)}>
      <Editor
        language="sql"
        value={value}
        theme="vs"
        onChange={(v) => onChange(v ?? "")}
        onMount={handleMount}
        options={{
          readOnly: disabled,
          fontSize: 13,
          lineHeight: 20,
          fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
          minimap: { enabled: false },
          lineNumbers: "on",
          wordWrap: "off",
          scrollBeyondLastLine: false,
          automaticLayout: true,
          tabSize: 4,
          insertSpaces: true,
          renderWhitespace: "none",
          folding: true,
          padding: { top: 8, bottom: 8 },
          suggest: { showKeywords: true, showSnippets: true },
        }}
        loading={<div className="p-3 text-xs text-muted-foreground">加载编辑器…</div>}
      />
    </div>
  )
}