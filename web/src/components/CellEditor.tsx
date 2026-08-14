import { useMemo, useState } from "react"
import Editor from "@monaco-editor/react"
import JsonView from "@uiw/react-json-view"
import { Button } from "@/components/ui/button"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { cn } from "@/lib/utils"

interface Props {
  column: string // 列名
  dataType: string // 列数据类型（用于辅助判断）
  value: unknown // 当前值
  nullable: boolean
  onSave: (newValue: unknown) => void
  onCancel: () => void
  saving?: boolean
  readonly?: boolean // 只读模式：仅「查看」渲染展示，不提供编辑与保存（用于查询结果）
}

// 内容格式类型：用于选择展示/编辑组件
type ContentKind = "json" | "sql" | "xml" | "text" | "null" | "number" | "boolean"

// 尝试解析 JSON；成功返回解析结果，失败返回 undefined
function tryParseJSON(s: string): unknown | undefined {
  const t = s.trim()
  if (!t.startsWith("{") && !t.startsWith("[")) return undefined
  try {
    return JSON.parse(t)
  } catch {
    return undefined
  }
}

// 自动判断内容格式（结合列数据类型 + 值形态）
function detectKind(value: unknown, dataType: string): ContentKind {
  if (value === null || value === undefined) return "null"
  const t = typeof value
  if (t === "number" || t === "bigint") return "number"
  if (t === "boolean") return "boolean"

  const s = String(value)
  // JSON：对象/数组字符串，或 dataType 含 json
  if (/json/i.test(dataType) || (s.trim().startsWith("{") && tryParseJSON(s) !== undefined) || (s.trim().startsWith("[") && tryParseJSON(s) !== undefined)) {
    return "json"
  }
  // SQL：常见关键字开头
  if (/^(SELECT|INSERT|UPDATE|DELETE|CREATE|ALTER|DROP|WITH|EXPLAIN)\b/i.test(s.trim())) return "sql"
  // XML：标签开头
  if (/^\s*</.test(s) && /<\w+[\s>]/.test(s)) return "xml"
  // 长文本：超过阈值
  if (s.length > 200 || s.includes("\n")) return "text"
  return "text"
}

// 判断是否为 JSON 类型的值（对象/数组）
function isJSONValue(v: unknown): v is object {
  return typeof v === "object" && v !== null
}

export default function CellEditor({ column, dataType, value, nullable, onSave, onCancel, saving, readonly }: Props) {
  const kind = useMemo(() => detectKind(value, dataType), [value, dataType])

  // 编辑态文本（切到「编辑」tab 时才展示，但状态始终保留）
  const [text, setText] = useState<string>(value === null || value === undefined ? "" : String(value))
  const [jsonError, setJsonError] = useState("")

  // 展示态：JSON 树预览所需解析结果
  const parsedJSON = useMemo(() => {
    if (kind !== "json") return undefined
    if (isJSONValue(value)) return value // 已是对象/数组
    return tryParseJSON(String(value))
  }, [kind, value])

  const handleSave = () => {
    // NULL：空文本 + 可空 → 存 null
    if (nullable && text.trim() === "") {
      onSave(null)
      return
    }
    // JSON：尝试解析
    if (kind === "json") {
      const parsed = tryParseJSON(text)
      if (parsed === undefined) {
        setJsonError("JSON 格式错误，无法保存")
        return
      }
      onSave(text.trim()) // 保存规范化后的 JSON 字符串
      return
    }
    // number：转数字
    if (kind === "number") {
      const n = Number(text)
      if (Number.isNaN(n)) {
        setJsonError("请输入有效数字")
        return
      }
      onSave(n)
      return
    }
    onSave(text)
  }

  return (
    <div className="flex min-h-0 flex-col gap-2">
      {/* 头部：列名 + 类型 */}
      <div className="flex items-center gap-2">
        <span className="font-medium">{column}</span>
        <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">{dataType || "未知类型"}</span>
        <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[11px] text-primary">{kind.toUpperCase()}</span>
      </div>

      {/* 只读模式：仅「查看」，直接渲染内容，不显示 tab 标题（节省可视区） */}
      {readonly ? (
        <div className="scrollbar-thin max-h-[40vh] overflow-auto rounded-md border bg-muted/20 p-2">
          {kind === "json" && parsedJSON !== undefined ? (
            <JsonView value={parsedJSON as object} collapsed={2} displayDataTypes={false} enableClipboard={false} />
          ) : kind === "json" ? (
            <div className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-xs text-destructive">JSON 格式无效</div>
          ) : kind === "null" ? (
            <div className="font-mono text-[12px] italic text-muted-foreground">NULL</div>
          ) : (
            <pre className="whitespace-pre-wrap break-words font-mono text-[12px] leading-relaxed">{String(value ?? "")}</pre>
          )}
        </div>
      ) : (
        /* 可编辑模式：顶部 Tab 查看 / 编辑 */
        <Tabs defaultValue="view">
          <TabsList className="w-fit">
            <TabsTrigger value="view">查看</TabsTrigger>
            <TabsTrigger value="edit">编辑</TabsTrigger>
          </TabsList>

          {/* 查看：只读渲染，按格式选最佳展示（min-h 与编辑区对齐，避免切换抖动） */}
          <TabsContent value="view" className="min-h-[240px]">
            {kind === "json" && parsedJSON !== undefined ? (
              <div className="scrollbar-thin max-h-[40vh] min-h-[240px] overflow-auto rounded-md border bg-muted/20 p-2">
                <JsonView value={parsedJSON as object} collapsed={2} displayDataTypes={false} enableClipboard={false} />
              </div>
            ) : kind === "json" ? (
              <div className="min-h-[240px] rounded-md border border-destructive/40 bg-destructive/5 p-3 text-xs text-destructive">
                JSON 格式无效，请切到「编辑」修正
              </div>
            ) : kind === "sql" || kind === "xml" ? (
              <div className="overflow-hidden rounded-md border">
                <Editor
                  height="240px"
                  language={kind === "sql" ? "sql" : "xml"}
                  value={String(value ?? "")}
                  options={{ minimap: { enabled: false }, fontSize: 12, wordWrap: "on", scrollBeyondLastLine: false, readOnly: true }}
                />
              </div>
            ) : kind === "null" ? (
              <div className="min-h-[240px] rounded-md border bg-muted/20 p-3 font-mono text-[12px] italic text-muted-foreground">NULL</div>
            ) : (
              <pre className="scrollbar-thin max-h-[40vh] min-h-[240px] overflow-auto whitespace-pre-wrap break-words rounded-md border bg-muted/20 p-3 font-mono text-[12px] leading-relaxed">
                {String(value ?? "")}
              </pre>
            )}
          </TabsContent>

          {/* 编辑：可修改，按格式选组件（统一 min-h 与查看区一致） */}
          <TabsContent value="edit" className="min-h-[240px]">
            {kind === "sql" || kind === "xml" || (kind === "text" && text.length > 200) ? (
              <div className="overflow-hidden rounded-md border">
                <Editor
                  height="240px"
                  language={kind === "sql" ? "sql" : kind === "xml" ? "xml" : "plaintext"}
                  value={text}
                  onChange={(v) => {
                    setText(v ?? "")
                    setJsonError("")
                  }}
                  options={{ minimap: { enabled: false }, fontSize: 12, wordWrap: "on", scrollBeyondLastLine: false }}
                />
              </div>
            ) : (
              <textarea
                className={cn(
                  "min-h-[240px] w-full resize-y rounded-md border bg-background px-3 py-2 font-mono text-[12px] outline-none focus:border-primary",
                  jsonError && "border-destructive",
                )}
                value={text}
                onChange={(e) => {
                  setText(e.target.value)
                  setJsonError("")
                }}
                placeholder={nullable ? "留空保存 NULL" : ""}
              />
            )}
          </TabsContent>
        </Tabs>
      )}

      {/* 错误提示 */}
      {jsonError && <div className="text-xs text-destructive">{jsonError}</div>}

      {/* 底部操作 */}
      <div className="flex shrink-0 items-center justify-end gap-2">
        <Button variant="outline" size="sm" onClick={onCancel} disabled={saving}>
          {readonly ? "关闭" : "取消"}
        </Button>
        {!readonly && (
          <Button size="sm" onClick={handleSave} disabled={saving}>
            {saving ? "保存中..." : "保存"}
          </Button>
        )}
      </div>
    </div>
  )
}
