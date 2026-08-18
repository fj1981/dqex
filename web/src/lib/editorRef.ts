import type { SqlEditorInstance } from "@/components/SqlEditor"

// 共享的编辑器实例 getter：由 SqlEditor 的 onReady 注册，供跨组件（如右侧收藏/历史面板）
// 在「插入光标处/替换所选」回填时读取实时光标/选中偏移（权威来源）。
let editor: SqlEditorInstance | null = null

export function setSqlEditor(ed: SqlEditorInstance | null) {
  editor = ed
}

export function getSqlEditor(): SqlEditorInstance | null {
  return editor
}
