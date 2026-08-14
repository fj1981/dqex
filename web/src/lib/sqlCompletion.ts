import type * as monacoNS from "monaco-editor"
import { useObjectTreeStore } from "@/stores/objectTreeStore"
import { useQueryStore } from "@/stores/queryStore"

// SQL 自动补全：关键字 + 库名 + 表名，依据光标所在上下文判断补什么。
// 元数据复用对象树 store（getTableTree 的结果），无需额外接口。

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

      // 1) `库名.` 之后 → 补该库的表名（裸表名）
      if (ctx.dbPrefix) {
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

      // 3) FROM/JOIN/INTO 等之后 → 补表名（裸名）。
      //    跟随当前查询 Tab 的库选择：选了库只补该库的表，未选则补所有库的表。
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

function getTablesOfDB(db: string): string[] {
  const { nodes } = useObjectTreeStore.getState()
  const dbNode = nodes.find((n) => n.type === "db" && n.name.toLowerCase() === db.toLowerCase())
  if (!dbNode) return []
  return (dbNode.children ?? [])
    .filter((c) => c.type === "table" || c.type === "view")
    .map((c) => c.name)
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
// - 未选库 → 所有库的表（去重，裸名）
function getTablesForCurrentScope(): string[] {
  const db = currentScopeDB()
  if (db) return getTablesOfDB(db)

  const { nodes } = useObjectTreeStore.getState()
  const seen = new Set<string>()
  const result: string[] = []
  for (const n of nodes) {
    if (n.type !== "db") continue
    for (const c of n.children ?? []) {
      if ((c.type === "table" || c.type === "view") && !seen.has(c.name)) {
        seen.add(c.name)
        result.push(c.name)
      }
    }
  }
  return result
}
