import type * as monacoNS from "monaco-editor"
import { useObjectTreeStore } from "@/stores/objectTreeStore"
import { useQueryStore } from "@/stores/queryStore"
import type { ObjectNode } from "@/types"

// SQL 自动补全：关键字 + 库名 + schema 名 + 表名，依据光标所在上下文判断补什么。
// 元数据复用对象树 store（getTableTree 的结果），无需额外接口。
// PG 系对象树为 库 → schema → 分组 → 对象 分层：表/视图叶子名带 "schema." 前缀（限定名），
// 遍历需递归深入 schema 节点；MySQL/Oracle 无 schema 层，行为与历史一致（裸表名）。

const SQL_KEYWORDS = [
  "SELECT", "FROM", "WHERE", "JOIN", "LEFT JOIN", "RIGHT JOIN", "INNER JOIN", "OUTER JOIN",
  "ON", "GROUP BY", "ORDER BY", "HAVING", "LIMIT", "OFFSET", "INSERT INTO", "VALUES",
  "UPDATE", "SET", "DELETE FROM", "CREATE TABLE", "ALTER TABLE", "DROP TABLE", "CREATE INDEX",
  "DROP INDEX", "CREATE VIEW", "DROP VIEW", "CREATE DATABASE", "USE", "AND", "OR", "NOT",
  "NULL", "AS", "DISTINCT", "UNION", "UNION ALL", "CASE", "WHEN", "THEN", "ELSE", "END",
  "BETWEEN", "IN", "IS", "LIKE", "ASC", "DESC", "COUNT", "SUM", "AVG", "MIN", "MAX",
  "SHOW", "EXPLAIN", "DESCRIBE", "TRUNCATE", "BEGIN", "COMMIT", "ROLLBACK", "PRIMARY KEY",
  "FOREIGN KEY", "REFERENCES", "UNIQUE", "NOT NULL", "DEFAULT", "AUTO_INCREMENT",
]

// 关键字后应补「表名」的锚点；USE 后补「库名」
const TABLE_ANCHORS = new Set([
  "FROM", "JOIN", "INNER", "LEFT", "RIGHT", "OUTER", "INTO", "UPDATE", "TABLE", "INDEX",
  "TRUNCATE", "ALTER", "DROP", "DESCRIBE", "EXPLAIN",
])
const DB_ANCHORS = new Set(["USE", "DATABASE"])

interface CompletionContext {
  // 光标前最近一个非空白 token（可能为空字符串）
  lastWord: string
  // 光标前最近一个关键字（大写）
  lastKeyword: string
  // 是否存在 `库名.` 前缀，如 "db1." 的 db1
  dbPrefix: string | null
}

// 从模型文本中分析光标上下文（启发式，覆盖常见场景）
function analyzeContext(
  model: monacoNS.editor.ITextModel,
  position: monacoNS.Position,
): CompletionContext {
  const text = model.getValueInRange({
    startLineNumber: 1,
    startColumn: 1,
    endLineNumber: position.lineNumber,
    endColumn: position.column,
  })

  // 1. 检测 `库名.` 前缀（光标紧跟点号后）或 `库名.表名前缀`（点号后已输入部分表名）
  const dbPrefixMatch = text.match(/(?:^|[^\w.])`?(\w+)`?\.([\w$]*)$/)
  const dbPrefix = dbPrefixMatch ? dbPrefixMatch[1] : null
  const tablePrefixAfterDot = dbPrefixMatch ? dbPrefixMatch[2] : ""

  // 2. 光标前最后一个单词（正在输入的部分）；若有 dbPrefix，则它是点号后的部分
  const lastWord = dbPrefix ? tablePrefixAfterDot : (text.match(/([A-Za-z_][\w$]*)$/)?.[1] ?? "")

  // 3. 光标前最近一个关键字：取 lastWord 之前的上一个 token
  const beforeWord = text.slice(0, text.length - lastWord.length)
  const keywordMatch = beforeWord.match(/([A-Za-z_][\w$]*)\s*$/)
  const lastKeyword = keywordMatch ? keywordMatch[1].toUpperCase() : ""

  return { lastWord, lastKeyword, dbPrefix }
}

export function registerSQLCompletion(monaco: typeof monacoNS) {
  monaco.languages.registerCompletionItemProvider("sql", {
    triggerCharacters: [" ", "."],
    provideCompletionItems: (model, position) => {
      const ctx = analyzeContext(model, position)
      const word = model.getWordUntilPosition(position)
      // dbPrefix 场景：range 覆盖点号后的已输入部分（不含库名和点号）
      const range: monacoNS.IRange = ctx.dbPrefix
        ? {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: position.column - ctx.lastWord.length,
            endColumn: position.column,
          }
        : {
            startLineNumber: position.lineNumber,
            endLineNumber: position.lineNumber,
            startColumn: word.startColumn,
            endColumn: word.endColumn,
          }
      const prefix = ctx.lastWord.toUpperCase()

      const suggestions: monacoNS.languages.CompletionItem[] = []

      // 1) `库名.` 或 `schema.` 之后：
      //    - PG：`库名.` 后补该库的 schema 名；`schema.` 后补该 schema 的表/视图（裸名，
      //      前缀已在编辑器中，避免生成非法三级引用 库.schema.表）
      //    - MySQL/Oracle：`库名.` 后补该库的表/视图（裸名，原行为）
      if (ctx.dbPrefix) {
        const schemas = getSchemasOfDB(ctx.dbPrefix)
        if (schemas.length > 0) {
          for (const s of schemas) {
            if (!s.toUpperCase().startsWith(prefix)) continue
            suggestions.push({
              label: s,
              kind: monaco.languages.CompletionItemKind.Module,
              insertText: s,
              range,
              sortText: "1" + s,
            })
          }
          return { suggestions }
        }
        const schemaTables = getTablesOfSchema(ctx.dbPrefix)
        if (schemaTables.length > 0) {
          for (const t of schemaTables) {
            if (!t.toUpperCase().startsWith(prefix)) continue
            suggestions.push({
              label: t,
              kind: monaco.languages.CompletionItemKind.Struct,
              insertText: t,
              range,
              sortText: "1" + t,
            })
          }
          return { suggestions }
        }
        const tables = getTablesOfDB(ctx.dbPrefix)
        for (const t of tables) {
          if (!t.toUpperCase().startsWith(prefix)) continue
          suggestions.push({
            label: t,
            kind: monaco.languages.CompletionItemKind.Struct,
            insertText: t,
            range,
            sortText: "1" + t,
          })
        }
        return { suggestions }
      }

      // 2) USE/DATABASE 之后 → 补库名
      if (DB_ANCHORS.has(ctx.lastKeyword)) {
        for (const db of getDatabases()) {
          if (!db.toUpperCase().startsWith(prefix)) continue
          suggestions.push({
            label: db,
            kind: monaco.languages.CompletionItemKind.Module,
            insertText: db,
            range,
            sortText: "1" + db,
          })
        }
        return { suggestions }
      }

      // 3) FROM/JOIN/INTO 等之后 → 补表名。
      //    跟随当前查询 Tab 的库选择：选了库只补该库的表，未选则补所有库的表。
      //    PG 表名带 "schema." 前缀（限定名），非 public schema 的表必须限定名才能引用。
      if (TABLE_ANCHORS.has(ctx.lastKeyword)) {
        const tables = getTablesForCurrentScope()
        for (const t of tables) {
          if (!t.toUpperCase().startsWith(prefix)) continue
          suggestions.push({
            label: t,
            kind: monaco.languages.CompletionItemKind.Struct,
            insertText: t,
            range,
            sortText: "1" + t,
          })
        }
        return { suggestions }
      }

      // 4) 默认 → 关键字补全
      for (const k of SQL_KEYWORDS) {
        if (!k.startsWith(prefix)) continue
        suggestions.push({
          label: k,
          kind: monaco.languages.CompletionItemKind.Keyword,
          insertText: k,
          range,
          sortText: "0" + k,
        })
      }
      return { suggestions }
    },
  })
}

// ---- 元数据读取（复用对象树 store） ----

function getDatabases(): string[] {
  const { nodes } = useObjectTreeStore.getState()
  return nodes.filter((n) => n.type === "db").map((n) => n.name)
}

// collectTableNames 递归收集节点子树中的表/视图名：
// - MySQL/Oracle 无 schema 层：直接收集叶子（裸名）
// - PG 有 schema 层：深入 schema 节点收集（叶子名带 "schema." 前缀的限定名）
// 跨库/跨 schema 去重由调用方传入 seen（可选）
function collectTableNames(children: ObjectNode[] | undefined, seen?: Set<string>): string[] {
  const result: string[] = []
  for (const c of children ?? []) {
    if (c.type === "table" || c.type === "view") {
      if (seen) {
        if (seen.has(c.name)) continue
        seen.add(c.name)
      }
      result.push(c.name)
    } else if (c.type === "schema" || c.type === "db") {
      result.push(...collectTableNames(c.children, seen))
    }
  }
  return result
}

// getSchemasOfDB 返回库下的 schema 名（PG 系有 schema 层；MySQL/Oracle 为空）
function getSchemasOfDB(db: string): string[] {
  const { nodes } = useObjectTreeStore.getState()
  const dbNode = nodes.find((n) => n.type === "db" && n.name.toLowerCase() === db.toLowerCase())
  if (!dbNode) return []
  return (dbNode.children ?? []).filter((c) => c.type === "schema").map((c) => c.name)
}

function getTablesOfDB(db: string): string[] {
  const { nodes } = useObjectTreeStore.getState()
  const dbNode = nodes.find((n) => n.type === "db" && n.name.toLowerCase() === db.toLowerCase())
  if (!dbNode) return []
  return collectTableNames(dbNode.children)
}

// getTablesOfSchema 全树查找名为 schema 的节点（PG `库名.schema.` / `schema.` 前缀场景），
// 返回其下的裸表名（不带 schema 前缀：前缀已留在编辑器中，避免重复）
function getTablesOfSchema(schema: string): string[] {
  const { nodes } = useObjectTreeStore.getState()
  for (const n of nodes) {
    if (n.type !== "db") continue
    const sc = (n.children ?? []).find((c) => c.type === "schema" && c.name.toLowerCase() === schema.toLowerCase())
    if (sc) return collectTableNames(sc.children)
  }
  return []
}

// 当前查询 Tab 的库选择：空 = 未选库（默认库）
function currentScopeDB(): string {
  const { activeId, tabs } = useQueryStore.getState()
  const active = tabs.find((t) => t.id === activeId)
  if (active && active.kind === "query") return active.db
  return ""
}

// 按当前查询 Tab 的库选择返回表名列表：
// - 选了库 → 该库的表
// - 未选库 → 所有库的表（去重）
function getTablesForCurrentScope(): string[] {
  const db = currentScopeDB()
  if (db) return getTablesOfDB(db)

  const { nodes } = useObjectTreeStore.getState()
  const seen = new Set<string>()
  const result: string[] = []
  for (const n of nodes) {
    if (n.type !== "db") continue
    result.push(...collectTableNames(n.children, seen))
  }
  return result
}
