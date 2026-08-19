// SQL 格式化：基于 sql-formatter，支持多方言、选中/全文格式化。
// 动态 import 懒加载，不计入首屏 bundle。

import type { SqlEditorInstance } from "@/components/SqlEditor"
import { useAppStore } from "@/stores/app"
import { useQueryStore } from "@/stores/queryStore"
import { toast } from "sonner"

// 连接类型 → sql-formatter 方言模块名映射
const DIALECT_MAP: Record<string, string> = {
  mysql: "mysql",
  postgresql: "postgresql",
  oracle: "plsql",
  sqlite: "sqlite",
  tidb: "tidb",
}

// 获取当前连接对应的 sql-formatter 方言名
function currentDialect(): string {
  const connId = useQueryStore.getState().connId
  const { connections } = useAppStore.getState()
  const conn = connections.find((c) => c.id === connId)
  if (conn) {
    const mapped = DIALECT_MAP[conn.conn.Type.toLowerCase()]
    if (mapped) return mapped
  }
  return "sql"
}

// 格式化 SQL 文本（懒加载 sql-formatter）
export async function formatSQL(sql: string): Promise<string> {
  const { format } = await import("sql-formatter")
  const dialect = currentDialect()
  // language 传字符串，format 内部自动映射到对应方言对象
  return format(sql, { language: dialect as "mysql", tabWidth: 4 })
}

// 格式化编辑器内容（选中 → 仅格式化选中部分；无选中 → 格式化全文）
// 供 SqlEditor 快捷键和 WorkspaceLayout 工具栏按钮共用
export async function formatEditorSQL(ed: SqlEditorInstance): Promise<void> {
  const model = ed.getModel()
  if (!model) return
  const selection = ed.getSelection()
  const hasSelection = selection && !selection.isEmpty()
  const rawSQL = hasSelection ? model.getValueInRange(selection!) : model.getValue()
  if (!rawSQL.trim()) {
    toast.info("编辑器中没有可格式化的 SQL")
    return
  }
  try {
    const formatted = await formatSQL(rawSQL)
    if (hasSelection) {
      // 替换选中范围
      ed.executeEdits("format", [{
        range: selection!,
        text: formatted,
        forceMoveMarkers: true,
      }])
    } else {
      // 替换全文：保留光标大致位置（格式化前行号 → 格式化后同位置）
      const pos = ed.getPosition()
      const offsetBefore = pos ? model.getOffsetAt(pos) : 0
      model.setValue(formatted)
      // 恢复光标：不超过新文本长度
      const newLen = model.getValue().length
      const safeOffset = Math.min(offsetBefore, newLen)
      const newPos = model.getPositionAt(safeOffset)
      ed.setPosition(newPos)
    }
  } catch (e) {
    toast.error(`SQL 格式化失败: ${(e as Error).message}`)
  }
}
