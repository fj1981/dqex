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
  maximized?: boolean // 最大化模式：内容区撑满可用高度（配合 DialogContent 拉高）
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
  // JSON：dataType 含 json 或 字符串是合法 JSON
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

export default function CellEditor({ column, dataType, value, nullable, onSave, onCancel, saving, readonly, maximized }: Props) {
  const kind = useMemo(() => detectKind(value, dataType), [value, dataType])
  // 内容区高度：最大化时撑满剩余高度，否则用固定 min-h（240px）与 max-h（40vh）
  const boxH = maximized ? "min-h-0 flex-1" : "min-h-[240px] max-h-[40vh]"

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
    <div className={cn("flex min-h-0 min-w-0 flex-col gap-2", maximized && "flex-1")}>
      {/* 头部：列名 + 类型 */}
      <div className="flex items-center gap-2">
        <span className="font-medium">{column}</span>
        <span className="rounded bg-muted px-1.5 py-0.5 text-[11px] text-muted-foreground">{dataType || "未知类型"}</span>
        <span className="rounded bg-primary/10 px-1.5 py-0.5 text-[11px] text-primary">{kind.toUpperCase()}</span>
      </div>

      {/* 只读模式：仅「查看」，直接渲染内容，不显示 tab 标题（节省可视区） */}
      {readonly ? (
        kind === "json" && parsedJSON !== undefined ? (
          <div className={cn("scrollbar-thin overflow-auto rounded-md border bg-muted/20 p-2", boxH)}>
            <JsonView value={parsedJSON as object} collapsed={2} displayDataTypes={false} enableClipboard={false} />
          </div>
        ) : kind === "json" ? (
          <div className={cn("rounded-md border border-destructive/40 bg-destructive/5 p-3 text-xs text-destructive", boxH)}>JSON 格式无效</div>
        ) : kind === "sql" || kind === "xml" ? (
          <div className={cn("overflow-hidden rounded-md border", boxH)}>
            <Editor
              height={maximized ? "100%" : "240px"}
              language={kind === "sql" ? "sql" : "xml"}
              value={String(value ?? "")}
              options={{
                minimap: { enabled: false },
                fontSize: 12,
                wordWrap: "bounded",
                wrappingIndent: "deepIndent",
                scrollBeyondLastLine: false,
                readOnly: true,
              }}
            />
          </div>
        ) : kind === "null" ? (
          <div className={cn("rounded-md border bg-muted/20 p-3 font-mono text-[12px] italic text-muted-foreground", boxH)}>NULL</div>
        ) : (
          <pre className={cn("scrollbar-thin overflow-auto whitespace-pre-wrap break-all rounded-md border bg-muted/20 p-3 font-mono text-[12px] leading-relaxed", boxH)}>{String(value ?? "")}</pre>
        )
      ) : (
        /* 可编辑模式：顶部 Tab 查看 / 编辑 */
        <Tabs defaultValue="view" className={cn("flex min-h-0 flex-col", maximized && "flex-1")}>
          <TabsList className="w-fit">
            <TabsTrigger value="view">查看</TabsTrigger>
            <TabsTrigger value="edit">编辑</TabsTrigger>
          </TabsList>

          {/* 查看：只读渲染，按格式选最佳展示（min-h 与编辑区对齐，避免切换抖动） */}
          <TabsContent value="view" className={cn("min-h-[240px]", maximized && "min-h-0 flex-1")}>
            {kind === "json" && parsedJSON !== undefined ? (
              <div className={cn("scrollbar-thin overflow-auto rounded-md border bg-muted/20 p-2", boxH)}>
                <JsonView value={parsedJSON as object} collapsed={2} displayDataTypes={false} enableClipboard={false} />
              </div>
            ) : kind === "json" ? (
              <div className={cn("rounded-md border border-destructive/40 bg-destructive/5 p-3 text-xs text-destructive", boxH)}>
                JSON 格式无效，请切到「编辑」修正
              </div>
            ) : kind === "sql" || kind === "xml" ? (
              <div className={cn("overflow-hidden rounded-md border", boxH)}>
                <Editor
                  height={maximized ? "100%" : "240px"}
                  language={kind === "sql" ? "sql" : "xml"}
                  value={String(value ?? "")}
                  options={{
                    minimap: { enabled: false },
                    fontSize: 12,
                    // wordWrap "bounded" 会在视区宽度内强制换行，避免超长单行撑出容器（"on" 模式仍可能让容器被内容撑大）。
                    wordWrap: "bounded",
                    wrappingIndent: "deepIndent",
                    scrollBeyondLastLine: false,
                    readOnly: true,
                  }}
                />
              </div>
            ) : kind === "null" ? (
              <div className={cn("rounded-md border bg-muted/20 p-3 font-mono text-[12px] italic text-muted-foreground", boxH)}>NULL</div>
            ) : (
              <pre className={cn("scrollbar-thin overflow-auto whitespace-pre-wrap break-all rounded-md border bg-muted/20 p-3 font-mono text-[12px] leading-relaxed", boxH)}>
                {String(value ?? "")}
              </pre>
            )}
          </TabsContent>

          {/* 编辑：可修改，按格式选组件（统一 min-h 与查看区一致） */}
          <TabsContent value="edit" className={cn("min-h-[240px]", maximized && "min-h-0 flex-1")}>
            {kind === "sql" || kind === "xml" || (kind === "text" && text.length > 200) ? (
              <div className={cn("overflow-hidden rounded-md border", boxH)}>
                <Editor
                  height={maximized ? "100%" : "240px"}
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
                  "w-full resize-y rounded-md border bg-background px-3 py-2 font-mono text-[12px] outline-none focus:border-primary",
                  maximized ? "min-h-0 flex-1" : "min-h-[240px]",
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
