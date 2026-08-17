# 数据网格功能：优化与修复清单

> 审查日期：2026-08-14
> 基线：`git main`（含当前未提交改动）
> 范围：Web 数据表格已实现功能的系统审查（查询终端、复制导出、列操作、筛选排序分页、单元格编辑、大字段懒加载/Excel、历史/审计/工作区持久化）

## 一、总体结论

核心链路（API 契约、冻结列偏移与兜底、筛选排序算子对齐、大字段懒加载时序、viewLayout 持久化覆盖语义、SSE 增量映射、写路径安全）经审查**无功能性错误**。

待处理项按严重程度分三级：

| 级别 | 编号 | 问题 | 影响 | 状态 |
| --- | --- | --- | --- | --- |
| P0 待实测 | H1 | BIGINT 主键经 JSON number 传输可能丢精度 | 大整数主键（>2^53）可能定位/更新错行 | 待实测 |
| P1 | M1 | 写路径 4 处硬编码 `BuildMySQL()` | PostgreSQL/Oracle 连接下 UPDATE/DELETE/INSERT/大字段取值全部失败 | ✅ 已修复（2026-08-14） |
| P1 | M2 | Excel 导出复用列表查询的「大字段省略」逻辑 | 导出文件中大字段列全为空白 | ✅ 已修复（2026-08-14） |
| P2 体验 | L1–L7 | 导出含隐藏列 / 无防抖 / NULL 过滤语义 / 持久化注释误导等 | 体验与健壮性小问题 | ✅ 已修复（2026-08-14） |

## 二、P1 中等缺陷

### M1 写路径方言硬编码（PG / Oracle 不可用）— ✅ 已修复

**位置**：`internal/engine/sqlquery.go`

- 586 行 `RunParamUpdate`：`sql, _, err := q.BuildMySQL()`
- 628 行 `RunParamDelete`：`sql, _, err := q.BuildMySQL()`
- 670 行 `RunParamInsert`：`sql, _, err := q.BuildMySQL()`
- 709 行 `GetCellValue`：`sql, _, err := q.BuildMySQL()`

**根因**：读路径 `QueryTablePage`（438 行）正确使用 `q.QueryPagedResult(cli)`——cydb 会按 `cli.DBType()` 自动选择 Flavor；但写路径全部显式调用 `BuildMySQL()`，绕过了方言感知机制。生成的 SQL 使用反引号标识符（`UPDATE \`t\` SET \`c\` = :set_val ...`），在 PostgreSQL/Oracle 连接下执行必然报错。`meta.go` 已支持 `mysql/postgresql/oracle` 三种连接类型，故影响范围明确。

**修复方案**：改用 `BuildSQL(BuildOptions{Flavor: ...})`，Flavor 从 executor 派生（与 cydb 内部 `FlavorForDatabase(cli.DBType())` / `FlavorFromExecutor(cli)` 一致）：

```go
// 以 RunParamUpdate 为例（586 行）
var q def.SQLStmt = ss.Q().Update(p.Table).Set(ss.AssignParam(p.SetColumn, "set_val"))
q = q.Where(cydb.AND(pkConds...))
sql, _, err := q.BuildSQL(ss.BuildOptions{Flavor: ss.FlavorForDatabase(cli.DBType())})
if err != nil {
    return 0, fmt.Errorf("构建 SQL 失败: %w", err)
}
```

其余三处（628 / 670 / 709）同样替换。若 cydb 提供接收 executor 的 `BuildSQL(cli, opts)` 便捷方法，优先使用。

**回归验证**：分别用 MySQL、PostgreSQL 连接执行单元格编辑、删除行、新增行、点击大字段取值；`cli.DBType()` 覆盖三种方言后 SQL 引用符正确。

### M2 Excel 导出丢大字段 — ✅ 已修复

**位置**：

- `internal/engine/sqlquery.go` 489 行 `ExportTableExcel(ctx, cli, table, sortSpecs, excludeColumns, filters, maxRows)` → 493 行透传 `excludeColumns` 给 `QueryTablePage`
- `internal/engine/sqlquery.go` 458–470 行：`QueryTablePage` 将 exclude 列在结果中**置 NULL**
- `web/src/components/TableBrowser.tsx` 1253–1268 行：导出时传 `excludeColumns: Array.from(bigFieldCols)`

**根因**：导出复用列表查询语义。列表查询置 NULL 是为了省流量 + 前端渲染「点击加载」占位；但导出是离线完整数据交付，置 NULL 后大字段（BLOB / 超长文本）列导出的全是空白，用户无法在 Excel 中拿到真实内容。

**修复方案（推荐）**：导出路径不传 `excludeColumns`（导出本意即全量数据），一行改动：

```go
// internal/engine/sqlquery.go:493
res, err := QueryTablePage(ctx, cli, table, 1, maxRows, sortSpecs, nil, filters)
```

同时删除 `ExportTableExcel` 签名中的 `excludeColumns` 参数，并同步前端调用（`TableBrowser.tsx` 1253 行附近）不再传该参数。

**备选方案**：若需保留「导出也省流」的意图（导出大字段仍可能超限），则给 `QueryTablePage` 增加 `omitBigFields bool` 参数区分「列表展示」与「导出/取值」两条路径——但导出默认取全量更符合直觉，推荐前者。

**回归验证**：对含 BLOB/TEXT 列的表导出 Excel，确认大字段列有真实内容；列表展示仍保持「点击加载」占位。

## 三、P0 待实测高风险项

### H1 BIGINT 主键经 JSON 传输可能丢精度

**位置**：前后端行数据传输链路。

- 后端结果 `rows`（`[][]any`）经 JSON 序列化；主键为大整数（如雪花 ID，> 2^53）时，JSON number 在 JS 侧 `Number` 会丢失低位精度
- 前端用主键值构造「行选中 key / 编辑定位 / 删除定位」（`TableBrowser.tsx` 280–288、381–397 行），主键值经 `JSON.stringify` 往返
- 若主键实际值超过 2^53，可能定位到错误行或更新失败

**验证步骤**：

1. 造一张主键为 `BIGINT` 且含 > 2^53 值的表（如 `9223372036854775807`）
2. 前端列表展示 → 点击单元格编辑 → 保存，观察 SQL 中 WHERE 主键值是否仍为原值
3. 打开浏览器 Network，核对请求 payload 与响应中该主键的字符串表示

**修复预案（若实测命中）**：后端对大整数列以字符串形式序列化（或前端主键取值统一按字符串处理），行 key / 主键值在前后端间全程以 string 传递，仅在生成 SQL 时由参数绑定转为数字。注意同时覆盖：行选中 key、单元格编辑 `pkValues`、删除 `rows`、大字段懒加载 `GetCellValue` 的 `pkValues`（`web/src/api/sql.ts` 与 `internal/web/sql.go` 的对应 DTO）。

## 四、P2 轻微问题（体验 / 健壮性）

### L1 查询结果 CSV 导出包含隐藏列 — ✅ 已修复

**位置**：`web/src/components/ResultGrid.tsx` 543 行（`TableBrowser.tsx` 1244 行同类问题一并修复）

```tsx
onClick={() => downloadText(`query-result-${Date.now()}.csv`, rowsToCSV(columns, filteredRows))}
```

**问题**：`rowsToCSV` 使用全量 `columns`，用户隐藏的列仍会出现在 CSV 中，与所见不一致。

**修复**：改用 `visibleCols.map(c => c.name)`（172–175 行已有可见列集合）传给 `rowsToCSV`，并按可见列索引取值。

### L2 SQL 输入无防抖，每击键持久化 — ✅ 已修复

**位置**：`web/src/stores/queryStore.ts` 295–300 行 `updateTabSql`

**问题**：`updateTabSql` 在输入过程中每击键触发一次 `persistCurrent`（fire-and-forget 写后端 SQLite），长 SQL 输入会产生大量写请求。

**修复**：对 `updateTabSql` 引入 300–500ms 防抖（组件层 debounce 后调 store），或在 store 层对同 tab 的连续写合并。

### L3 NULL 单元格右键「等于此值」生成错误语义 — ✅ 已修复

**位置**：`web/src/components/ResultGrid.tsx` 150–158 行 `quickFilter`

```tsx
const value = cell === null || cell === undefined ? "" : String(cell)
next.push({ column: col, op, value })
```

**问题**：对 NULL 单元格右键「等于此值」会生成 `col = ''`（等空串），而非 SQL 语义的 `col IS NULL`；「不等于此值」同理。表头右键菜单虽有「只看空值/只看非空」（371–375 行 `isNull/isNotNull`），但单元格右键路径语义错误。

**修复**：`quickFilter` 中当 `cell == null` 时，将 `eq → isNull`、`neq → isNotNull`，并清空 value。

### L4 查询结果集不持久化（设计取舍，建议确认）— ✅ 已加 UI 提示

**位置**：`web/src/stores/queryStore.ts` 99–103 行注释 + 106–128 行 `toDTO`

**问题**：工作区只持久化「可重跑上下文」（sql/db/mode/seq/title），结果集 `results` 与瞬时状态 `running/error/activeResult` 均不落库，刷新/重开连接后结果丢失、需重新执行。这是**有意的设计**（避免大结果集撑爆存储），但用户感知可能为「结果丢了」。

**建议**：保持现状，但在 UI 上对「刷新后结果需重跑」给出提示；或在设置中提供「结果集持久化」开关（限行数，如 500 行）。

### L5 注释「结果持久化」名不副实 — ✅ 已修复

**位置**：`web/src/stores/queryStore.ts` 394 行

```tsx
// 结果持久化：刷新/切换连接后恢复上次查询结果
persistCurrent(connId, next, get().activeId)
```

**问题**：该注释暗示结果会被持久化，实际 `toDTO` 已剥离 `results`，落库的只是 tab 元数据。注释与行为不符，易误导后续维护。

**修复**：改为「持久化查询上下文（结果不落库，刷新后需重跑）」之类准确描述。

### L6 列结构加载失败静默降级 — ✅ 已修复

**位置**：`web/src/components/TableBrowser.tsx` 191–193 行

```tsx
} catch {
  // 列结构加载失败不阻塞数据展示，仅禁用编辑/大字段省略
}
```

**问题**：`getTableColumns` 失败时无任何提示，用户看到数据却不知道「编辑、删除、新增、大字段懒加载」已全部不可用（无主键定位）。

**修复**：catch 中记录错误状态，在工具栏/编辑入口处提示「列结构加载失败，编辑功能不可用」，并保留重试入口。

### L7 工作区持久化失败静默丢弃 — ✅ 已修复

**位置**：`web/src/stores/queryStore.ts` 170–176 行 `persistCurrent`

```tsx
saveWorkspace(connId, state).catch(() => {
  // 忽略持久化失败，不影响主流程
})
```

**问题**：持久化失败（后端不可写/网络抖动）被静默吞掉，用户切换连接或关闭标签页后可能丢上下文，且无任何痕迹。

**修复**：轻量方案——失败时 console.warn 并标记一次「上次保存失败」；重量方案——失败重试 1 次 + 状态栏提示。

## 五、修复方案汇总与排期建议

| 优先级 | 项 | 改动量 | 状态 |
| --- | --- | --- | --- |
| P0 | H1 | 待实测后再定 | 待实测：先造大整数主键表验证；命中后改字符串链路 |
| P1 | M1 | 4 行替换（sqlquery.go） | ✅ 已修复：`BuildSQL(ss.BuildOptions{Flavor: ss.FlavorForDatabase(cli.DBType())})` |
| P1 | M2 | ~3 行（sqlquery.go + TableBrowser.tsx） | ✅ 已修复：导出路径不传 `excludeColumns`，导出取全量大字段 |
| P2 | L1–L7 | 各 1–10 行 | ✅ 已修复（2026-08-14） |

## 六、已验证无问题清单（无需改动）

以下项经逐点核对，实现正确：

1. **API 契约**：`internal/web/sql.go`、`internal/web/sse.go` 与 `web/src/api/sql.ts`、`web/src/types/index.ts` 的 14 个路由/类型完全对齐。
2. **冻结列**：`ResultGrid.tsx` 188–200 行偏移计算从 0 起（无 checkbox 列）、边界列隐藏即视为无冻结；`TableBrowser.tsx` 611–645 行默认冻结自动回退到「从右往左最后一个可见主键列」，取消固定（`frozenUntil:null` 显式清旧值）即撤销自定义回默认，`frozenTouched` ref 正确区分用户自定义与默认态。
3. **筛选/排序算子对齐**：前端 `FILTER_OPS` 枚举与后端 `buildFilterWheres` 白名单一致，值全部参数化绑定，`LIKE` 自动转义；排序列/过滤列均经 `GetTableInfo` 白名单校验（`sqlquery.go` 399–410 行）。
4. **大字段懒加载时序**：`TableBrowser.tsx` `loadData`（160–233 行）先拉 struct 再查数据，`cols = sc.columns` 赋值避免竞态串表；无主键时自动退化为完整查询；点击单元格按主键取真实值。
5. **viewLayout 持久化覆盖语义**：单一写入点（583–593 行），后端整体替换 JSON，无合并脏写。
6. **SSE 增量映射**：`internal/web/sse.go` 事件类型与 `web/src` 消费端一一对应，无遗漏。
7. **写路径安全**：主键白名单（无主键拒绝编辑/删除）、命名参数绑定、标识符结构化引用，未发现 SQL 拼接注入面。
