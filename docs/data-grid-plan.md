# dqex — 数据表格功能规划文档

## 1. 概述

本文档规划 Web 端「数据表格」的后续增强功能。数据表格在当前代码库中有两个组件，二者能力不同、诉求不同，需分开规划：

| 组件 | 文件 | 数据来源 | 定位 | 当前能力 |
|---|---|---|---|---|
| `ResultGrid` | `web/src/components/ResultGrid.tsx` | SQL 查询结果（一次性返回，前端分页） | 查询结果只读展示 | 前端分页、全量排序、列宽自适应、单元格只读查看 |
| `TableBrowser` | `web/src/components/TableBrowser.tsx` | 表/视图数据（后端分页） | 表数据浏览与编辑 | 后端分页、全局排序、单元格编辑、行删除、大字段懒加载 |

> 本文档与 `docs/query-terminal-design.md`（CLI 终端）是两回事：后者是命令行终端的表格渲染，本文档专注 **Web 界面** 的 `<table>` 数据网格。

---

## 2. 现状盘点（已实现能力）

### 2.1 `ResultGrid`（查询结果表）

| 能力 | 说明 |
|---|---|
| 前端分页 | 结果一次性返回后内存切页，页大小 50/100/200/500/1000 |
| 全量排序 | 三态循环（无 → 升序 → 降序），数字/字符串，NULL 恒排最后 |
| 列宽自适应 | `computeColWidths` 按列名 + 采样前 100 行估算 |
| 单元格查看 | 点击弹只读弹层，JSON 树 / SQL / XML 高亮渲染 |
| 分页条 | 页码省略号、跳页输入、行数统计 |
| NULL 视觉 | 灰色斜体 |

### 2.2 `TableBrowser`（表数据浏览器）

| 能力 | 说明 |
|---|---|
| 后端分页 | page/pageSize + 全表总行数，跳页、页大小切换 |
| 全局排序 | 后端 `ORDER BY`，三态循环；大字段列禁排 |
| 单元格编辑 | 按主键 `UPDATE`（named bind），NULL 处理 |
| 大字段懒加载 | BLOB/长文本/JSON 列表省略，点击按主键取完整值 |
| 行选中 + 删除 | 按主键 key 跨页保留、全选、右键/批量删除（带确认） |
| 结构 / DDL | 表结构字段明细、DDL 展示两个 Tab |
| 编辑弹层 | JSON 树、SQL/XML 高亮、最大化 |

### 2.3 现状痛点（对标主流软件的差距）

| 痛点 | 说明 |
|---|---|
| NULL vs 空字符串混淆 | 现状 `renderCell` 把 `null` 和空串都渲染成「看似空」，且 NULL 用灰色斜体、空串却无标记，数据核对时极易误判 |
| 过滤缺「右键快速过滤」 | 主流工具（DataGrip/Navicat）右键单元格即可「按此值过滤」，我们是唯一没有快捷过滤入口的 |
| 列操作无聚合入口 | 排序/筛选/显隐散落各处，缺少列头右键聚合菜单（DBeaver/DataGrip 的交互骨架） |
| 主键列无标识 | 无法一眼识别定位列，编辑/删除定位靠猜 |

---

## 3. 待实现功能（需求池）

按使用频率与投入产出比分级。

### 3.1 列筛选 / 过滤（Filter）— 优先级 P0

**现状**：两个表格均只有排序，无按列值过滤能力，只能翻页 + 肉眼找。

**需求**：
- 表头加筛选入口（漏斗图标），支持按列值过滤。
- 过滤操作符**全量保留（9 种）**：

  | 操作符 | 文案 | 说明 |
  |---|---|---|
  | `eq` | 等于 | 精确匹配 |
  | `neq` | 不等于 | 精确不匹配 |
  | `contains` | 包含 | 模糊匹配（LIKE %v%） |
  | `notContains` | 不包含 | 模糊不匹配（NOT LIKE %v%） |
  | `startsWith` | 开头是 | LIKE v% |
  | `endsWith` | 结尾是 | LIKE %v |
  | `gt` / `gte` | 大于 / 大于等于 | 数值/日期比较 |
  | `lt` / `lte` | 小于 / 小于等于 | 数值/日期比较 |
  | `isNull` / `isNotNull` | 为空 / 非空 | IS NULL / IS NOT NULL，无需输入值 |

- 多列条件可叠加（AND）。
- `ResultGrid`：前端内存过滤，对已返回结果集生效。
- `TableBrowser`：后端过滤（`fetchTableData` 增加过滤条件参数），因为数据在库端分页，前端过滤只作用于当前页，语义不对。
- 过滤条件 UI 与状态清晰，支持一键清除全部过滤。
- **激活过滤需显式提示**：应用过滤后表头高亮 + 顶部提示「已应用 N 个过滤条件」，避免用户误以为数据丢失。
- **右键单元格快速过滤（超越点）**：右键某单元格 → 菜单直接提供「等于此值 / 不等于此值 / 包含此值」

### 3.2 单元格 / 行复制 — 优先级 P0

**现状**：`ResultGrid` 单元格只读但无复制入口；`TableBrowser` 右键菜单只有「删除行」。

**需求**：
- 右键菜单：复制单元格值 / 复制行（TSV）/ 复制列名 / 复制主键定位。
- 表格支持鼠标拖选区域 + `Ctrl/Cmd+C` 复制为 TSV/CSV。
- 复制长文本时避免 `title` 截断，直接取真实值。
- 复制 NULL 时按约定文案（如 `NULL`）。
- **NULL 与空字符串区分**：复制/展示时，NULL 显示为 `NULL`，空字符串显示为 `""` 或 `(空)`，二者绝不混淆（见 §2.3）。

### 3.3 结果导出 — 优先级 P1

**现状**：查询结果只能看，无法导出。

**需求**：
- 导出当前结果集为 CSV / Excel（.xlsx）。
- `ResultGrid`：导出前端已加载的全部结果（或应用当前排序/过滤后的结果）。
- `TableBrowser`：导出后端数据（可导出当前页或全表，全表需后端流式导出）。
- 复用项目已有的 `excelize`（后端）能力生成 .xlsx；CSV 可前端直接生成 blob 下载。

### 3.4 列显隐 / 冻结 — 优先级 P1

**现状**：无列显隐切换，无冻结列，宽表体验差。

**需求**：
- 列显示/隐藏切换（表头下拉勾选列）。
- 冻结首列 / 主键列（横向滚动时保持可见）。
- 隐藏大字段列或超宽列的快捷开关。

### 3.5 列数据统计 / 汇总 — 优先级 P1

**现状**：底部只有总行数，无聚合信息。

**需求**：
- 底部状态栏显示：当前页行数、总行数、选中行数、NULL 计数。
- 数值列可显示 min/max/sum/avg（`TableBrowser` 可由后端 `SELECT` 聚合返回，`ResultGrid` 前端计算）。
- 列头提供「只看 NULL」「只看非 NULL」快捷过滤。

### 3.6 新增行（INSERT）— 优先级 P2

**现状**：`TableBrowser` 支持编辑、删除，但无新增行。

**需求**：
- 表数据页提供「新增行」按钮，弹出空行编辑表单。
- 复用 `CellEditor` 与主键/自增列感知，自增列留空由库生成。
- 提交后刷新并定位到新行。

### 3.7 多列排序 — 优先级 P2

**现状**：排序仅单列三态。

**需求**：
- Shift + 点击列头叠加排序优先级（类似 DataGrip/表格库惯例）。
- 排序优先级在列头用序号角标显示。
- `TableBrowser` 后端支持多列 `ORDER BY`。

### 3.8 明确不实现（边界声明）

以下能力**刻意不做**，避免范围蔓延与语义歧义：

| 不实现项 | 理由 |
|---|---|
| `ResultGrid` 内联编辑 | 破坏「查询结果只读快照」的清晰语义，且后端需识别查询来源与主键，成本高、误改风险大 |
| 结果集并排对比 | 项目已有专门的「对比」功能（`dqex cmp` + Web 对比），重复造轮子 |
| 事务/延迟提交（deferred commit） | 现状单元格编辑为**即时 UPDATE 落库**（配合审计日志兜底）；改成「批量改后统一 commit」需引入事务预览与回滚 UI，改动大且与审计语义冲突，暂不引入 |
| 虚拟滚动（万行不卡） | `TableBrowser` 后端分页、`ResultGrid` 前端分页（默认 ≤1000 行）已规避性能问题，无需虚拟滚动 |

### 3.9 列头右键聚合菜单 — 优先级 P1

**现状**：排序/筛选/显隐等操作散落在各处，无统一入口。

**需求**（**易用性超越点**）：
- 右键列头弹出聚合菜单，集中提供：排序（升/降）、筛选、隐藏该列、冻结该列、复制列名、查看列类型。
- 菜单项随列类型动态禁用（如大字段列禁用排序/筛选）。
- 这是主流工具（DBeaver/DataGrip）的交互骨架，把它做得比主流更聚合、更顺手是关键。

### 3.10 主键列标识 + 列类型 tooltip — 优先级 P2

**现状**：`TableBrowser` 有行号列，但主键列无视觉标识；列头悬停无类型提示。

**需求**：
- 主键列在列头显示主键图标（🔑 或 Key 图标），便于识别定位列。
- 悬停列头显示列数据类型 tooltip（复用 `struct` 的 `TableColumn.dataType`）。
- `ResultGrid` 补充行号列（可选）。

---

## 4. 实施计划

| 阶段 | 模块 | 涉及组件 | 说明 | 预估 |
|---|---|---|---|---|
| Phase 1 | 列筛选（Filter）+ 右键快速过滤 | `ResultGrid` + `TableBrowser` + 后端 `fetchTableData` | 前端内存过滤 + 后端过滤参数（9 种操作符），多条件 AND、清除、激活提示、右键单元格过滤 | ✅ 已完成 |
| Phase 2 | 单元格/行复制 + NULL/空串区分 | `ResultGrid` + `TableBrowser` | 右键菜单 + 拖选复制 TSV + NULL/空串视觉区分 | ✅ 已完成 |
| Phase 3 | 列头右键聚合菜单（§3.9） | `ResultGrid` + `TableBrowser` | 排序/筛选/显隐/复制列名聚合入口 + 主键列标识 | ✅ 已完成 |
| Phase 4 | 结果导出 CSV/Excel | `ResultGrid` + `TableBrowser` + 后端 | 前端 CSV blob + 后端 xlsx 流式（复用 excelize） | ✅ 已完成 |
| Phase 5 | 列显隐 + 列统计 | `ResultGrid` + `TableBrowser` | 列勾选、底部聚合（仅当前页）、NULL 计数 | ✅ 已完成 |
| Phase 6 | 新增行 + 多列排序 | `TableBrowser`（+ 后端） | INSERT 表单、Shift 多列排序（`ss.Q` 多列 OrderBy 叠加） | ✅ 已完成 |
| Phase 7 | 布局持久化联动（§5.6） | `types` → `queryStore` → `TableBrowser` → 后端 → SQLite | 过滤/排序/列显隐/页大小随 object tab 持久化，`viewLayout` 全链路透传 | ✅ 已完成 |

> **持久化拆分原则**：Phase 1-6 先以「组件局部 state」实现功能本身（不持久化），跑通稳定后，Phase 7 再单独做 §5.6 的五层持久化联动，降低一次性改动风险。现已全部落地。

### 优先级矩阵

```
影响大 │  Phase 1 列筛选/快速过滤   │  Phase 4 结果导出
       │  Phase 2 复制             │  Phase 7 持久化联动
       │  Phase 3 列头聚合菜单      │  Phase 5 列显隐/统计
影响小 │                          │  Phase 6 新增行/多列排序
       │                          │
       └──────────────────────────┴──────────────
         工作量小                   工作量大
```

---

## 5. 设计约束与约定

以下约束须在实现时遵守（继承自 `docs/conventions.md`）：

### 5.1 数据来源语义必须分清（关键）

`ResultGrid` 是**前端分页**（结果已全量在内存），`TableBrowser` 是**后端分页**（数据在库端）。这决定了过滤/排序/统计的落点：

| 能力 | `ResultGrid` | `TableBrowser` |
|---|---|---|
| 过滤 | 前端内存过滤（对全量结果） | **必须后端过滤**，否则只作用于当前页，语义错误 |
| 排序 | 前端内存全量排序（现状） | 后端 `ORDER BY`（现状） |
| 统计 | 前端对全量结果计算 | 后端聚合（或接受「当前页统计」并明确标注） |
| 导出 | 前端导出全量已加载结果 | 后端导出全表 |

> 不要把 `TableBrowser` 的过滤做成前端过滤——那会只过滤当前页，产生「过滤后结果错误」的假象。

### 5.2 后端字段同步 + 安全红线（全链路）

新增后端过滤/导出能力时，遵循：
- 前端 TS 接口（`web/src/types/index.ts` 的 `TableDataRequest` 等）与后端请求解析字段对齐。
- `fetchTableData` 请求体增加过滤条件后，后端 `internal/web/sql.go` / `internal/service/sql.go` 同步解析与拼 SQL。

**过滤是唯一新增「用户输入进入 SQL」的路径，以下为硬性验收项（缺一不可）**：

| 防护项 | 要求 |
|---|---|
| **复用 `cydb` 条件构建器** | **不要手写 SQL 拼接**。过滤条件用 `cydb` 的 `EQ/NEQ/GT/GTE/LT/LTE`、`LIKEC/LIKEL/LIKER/NOT_LIKE*`、`ISNULL/ISNOTNULL`、`And/Or` 构建，值自动参数绑定（见 §5.2.1） |
| **列名白名单** | 过滤的 `column` 必须校验真实存在于表结构（复用 `getTableColumns` 结果做白名单），不能盲信前端 |
| **操作符白名单** | `op` 后端枚举校验，拒绝未知操作符（前端可被篡改） |
| **大字段列限制** | 二进制大字段列仅允许 `isNull`/`isNotNull`，其余操作符后端直接拒绝（见 §5.5） |
| **注入单测** | 覆盖 `' OR '1'='1`、`'; DROP TABLE` 等 payload 的单元测试，作为 CI 验收 |

> 现状 `QueryTablePage` 的 `table` 由上游校验（注释明示「调用方负责白名单/转义」），责任链分散。本次过滤改造应把**列名校验收敛到 engine 层**，避免重蹈「上游漏校验即注入」的隐患。

#### 5.2.1 复用 infrakit（cydb）能力清单（禁止重复造轮子）

后端 `infrakit`（`cydb`）**已经提供**过滤/分页/排序所需的一切，实现时**必须复用**，不得自行手写等价逻辑：

| 需求 | 复用 `cydb` 能力 | 说明 |
|---|---|---|
| 等于/不等于 | `cydb.EQ(field, v)` / `cydb.NEQ(field, v)` | 值自动参数绑定，防注入 |
| 大于/小于等比较 | `cydb.GT/GTE/LT/LTE(field, v)` | 同上 |
| 包含（LIKE %v%） | `cydb.LIKEC(field, v)` | **内部已含 `EscapeLikePattern` 转义**，`%`/`_` 自动处理，无需手动转义 |
| 开头/结尾是 | `cydb.LIKEL` / `cydb.LIKER` | 同上，自动转义 |
| 不包含/非开头/非结尾 | `cydb.NOT_LIKEC/NOT_LIKEL/NOT_LIKER` | 同上 |
| 为空/非空 | `cydb.ISNULL(field)` / `cydb.ISNOTNULL(field)` | — |
| 多条件 AND 叠加 | `cydb.And(conds...)` | 组合多个 `Where` |
| 排序 | `ss.OrderBy` / `WithOrderBy` | 按方言正确渲染 `ORDER BY` |
| 分页 | `ss.Limit` / `ss.Offset`（或 `QueryPagedResult`） | 跨方言（Oracle ROWNUM 等）由底层处理 |
| 计数 | `cli.Count(...)` / `SQLStmt.Count` | 自动剥离 SELECT/LIMIT/ORDER BY 做 COUNT |
| 标识符转义 | `ss.QuoteIdentifier(flavor, name)` | 表名/列名按连接方言引用 |
| 查询返回 `[][]any` | `cli.DirectQueryFastContext` / `QueryFast` | 与项目现有 `TablePageResult` 格式一致 |

**映射关系（前端 `FilterOp` → `cydb` 条件）**：

| 前端 `FilterOp` | `cydb` 条件 |
|---|---|
| `eq` | `cydb.EQ(col, v)` |
| `neq` | `cydb.NEQ(col, v)` |
| `contains` | `cydb.LIKEC(col, v)` |
| `notContains` | `cydb.NOT_LIKEC(col, v)` |
| `startsWith` | `cydb.LIKEL(col, v)` |
| `endsWith` | `cydb.LIKER(col, v)` |
| `gt` / `gte` / `lt` / `lte` | `cydb.GT/GTE/LT/LTE(col, v)` |
| `isNull` / `isNotNull` | `cydb.ISNULL(col)` / `cydb.ISNOTNULL(col)` |

> **结论**：此前设计的「named bind + 手动 `EscapeColumn` + 手动 LIKE 转义」是**重复造轮子**，已废弃。正确做法是：engine 层新增一个 `filters → []cydb.Where` 的纯转换函数，再用 `cydb` 的 Q 链式查询或 `ss` 构建器拼装，值绑定、转义、方言适配全部交给 `cydb`。

**写操作同样彻底复用 `cydb` 语句构建器**（不手写 `strings.Builder` 拼接）：

| 操作 | cydb 构建器写法 | 生成 SQL（命名参数） |
|---|---|---|
| 单元格 UPDATE | `ss.Q().Update(t).Set(ss.AssignParam(col, "set_val")).Where(cydb.AND(EQ(pk, Param("pk_N"))))` | `UPDATE \`t\` SET \`c\` = :set_val WHERE \`id\` = :pk_0` |
| 整行 DELETE | `ss.Q().Delete(t).Where(cydb.AND(EQ(pk, Param("pk_N"))))` | `DELETE FROM \`t\` WHERE \`id\` = :pk_0` |
| 新增行 INSERT | `ss.Q().Insert(t).Columns(cols...).Values(Param("v_N")...)` | `INSERT INTO \`t\` (\`a\`) VALUES (:v_0)` |
| 大字段按主键取值 | `ss.Q().Select(col).From(t).Where(cydb.AND(EQ(pk, Param("pk_N"))))` | `SELECT \`c\` FROM \`t\` WHERE \`id\` = :pk_0` |

关键点：值必须显式用 `ss.Param(":name")`（而非直接传 Go 值），否则 cydb 会把普通值渲染为 `LiteralExpr` 字面量内联（有注入风险）。生成的命名参数 `:name` 与 `cli.DirectNamedExecuteContext`（sqlx named bind）完全兼容。

#### 5.2.2 顺带重构 `QueryTablePage`（根治拼接隐患）

现状 `internal/engine/sqlquery.go` 的 `QueryTablePage` 用 `fmt.Sprintf("SELECT * FROM %s%s LIMIT %d OFFSET %d", ...)` 手写拼接，虽已 `EscapeColumn`/`EscapeTable` 转义，但仍存在：
- 分页 `LIMIT/OFFSET` 手写，**Oracle 方言不支持**（Oracle 需 ROWNUM/FETCH），跨库有隐患；
- 排序/分页字符串拼接，责任链分散；
- 计数 `countByStmt` 另走一条手写路径。

**决策**：本次过滤改造**顺带把 `QueryTablePage` 重构为 `cydb` Q 链式查询**，用 `ss.From` + `ss.Where(cydb.And(filters...))` + `ss.OrderBy` + `ss.Limit/Offset` + `SQLStmt.Count` 统一拼装，彻底消除手写 SQL 拼接，分页/排序/计数/方言适配全部交给 `cydb`。

重构后数据流：

```
QueryTablePage(ctx, cli, table, page, pageSize, sort, filters, exclude)
    │
    ├─ 列名白名单校验（getTableColumns 结果）→ 过滤/排序列合法
    ├─ filters → []cydb.Where（§5.2.1 映射表）
    ├─ q := ss.Q().From(quote(table)).
    │        Where(cydb.And(wheres...)).
    │        OrderBy(...).Limit(pageSize).Offset(...)
    ├─ rows := q 执行（返回 [][]any + 列名）
    ├─ total := q.Count(...)  ← 复用 cydb 剥离语义，废弃手写 countByStmt
    └─ 大字段 exclude 置 NULL（逻辑保留，不动）
```

### 5.3 性能约束

| 约束 | 要求 |
|---|---|
| **过滤输入防抖** | `TableBrowser` 后端过滤，输入过滤值时需**防抖（debounce）**，不能每敲一个字符就打库一次 |
| **列统计仅当前页** | `TableBrowser` 的 min/max/sum/avg 默认**只统计当前页**并在 UI 明确标注，避免全表聚合开销与误导 |
| **计数复用** | 过滤后总行数复用 `cydb` 的 `SQLStmt.Count`（自动剥离 SELECT/LIMIT/ORDER BY），废弃手写 `countByStmt`，避免二次全表扫描 |
| **`ResultGrid` 内存计算** | 前端过滤/排序/列宽均 `useMemo` 缓存，注意依赖数组，避免每次渲染全量重排 |
| **持久化体积** | 布局字段（过滤/排序/显隐）体积小，写 SQLite 工作区 JSON 无膨胀风险 |

---

### 5.4 状态建模

- 过滤条件、排序列、隐藏列各自独立 state，唯一写入点。
- 过滤条件默认「空」，用户操作后再填充，不预填。
- 排序/过滤/分页联动时，变更过滤或排序需重置到第 1 页（复用现状 `handleSort` 的 `setPage(1)` 模式）。

### 5.5 大字段列限制

- 大字段列（`BIG_FIELD_TYPES`）不支持排序（现状已禁）；过滤同样需谨慎，二进制列不支持文本过滤，仅支持 IS NULL / IS NOT NULL。

### 5.6 布局持久化联动

新功能要与项目既有的「工作区保存」机制联动，但**必须先分清存什么、存到哪**，不能一刀切全存或全不存。

#### 5.6.1 既有持久化机制回顾

| 机制 | 位置 | 内容 | 关键约定 |
|---|---|---|---|
| 工作区持久化 | 后端 SQLite，`queryStore.ts` 的 `persistCurrent`/`toDTO`/`fromDTO` | 按连接保存 tabs 的「可重跑」上下文 | 只存 sql/db/mode/seq/title + object 定位；**不存结果集与瞬时态** |
| 前端内存态 | zustand + 组件局部 `useState` | 分页/排序/页大小等视图状态 | 关 tab 即丢，不持久化 |

> 现状缺口：object tab 的 `toDTO` 只存 `{db, name, objType, subTab}`，**`page` 未存**（`fromDTO` 写死 `page:1`），`setObjectPage` 也未触发 `persistCurrent`。表浏览的视图布局持久化目前是半成品，本次一并补齐。

#### 5.6.2 各功能持久化归属

| 功能 | 是否持久化 | 存到哪 | 说明 |
|---|---|---|---|
| 列显隐 / 冻结（§3.4） | ✅ 持久化 | object tab DTO | 属「表视图布局」，调一次不想每次重调 |
| 列宽（手动拖拽后） | ✅ 持久化 | object tab DTO | 同上 |
| 排序状态（列 + 方向） | ✅ 持久化 | object tab DTO | 恢复表浏览时还原排序 |
| 过滤条件（§3.1） | ✅ 持久化 | object tab DTO | 恢复时重新带过滤条件请求后端 |
| 页大小（pageSize） | ✅ 持久化 | object tab DTO 或全局偏好 | 用户偏好 |
| 当前页码（page） | ❌ 不持久化 | — | 数据会变，恢复旧页码无意义（现状 `page:1` 写死是对的） |
| `ResultGrid` 的排序/过滤/结果集 | ❌ 不持久化 | — | 查询结果是瞬时数据，靠重新执行恢复，符合「结果集不持久化」约定 |
| `activeResult` 多结果集索引 | ❌ 不持久化 | — | 瞬时态 |

#### 5.6.3 持久化设计要点

1. **扩展 object tab DTO**：`WorkspaceTab`（后端）与 `ObjectTab`（前端内存）同步增加 `filters` / `sortColumn` / `sortOrder` / `hiddenColumns` / `pageSize` 等字段；`toDTO` 序列化、`fromDTO` 反序列化一一对应。
2. **恢复时重建视图**：`fromDTO` 还原后，`TableBrowser` 的 `loadData` 需在请求里带上持久化的 `filters`/`sortColumn`/`sortOrder`/`pageSize`，而非从默认值起步。
3. **写入时机**：所有视图状态变更（排序/过滤/显隐/页大小）都要像 `setObjectSubTab` 一样触发 `persistCurrent`，且保持「单一写入点」约定（见 §5.4）。
4. **`ResultGrid` 默认页大小**：若需「用户偏好的默认页大小」，用 `localStorage` 存全局偏好，与后端工作区无关，避免污染连接级状态。

#### 5.6.4 全链路同步清单（实现时逐项核对）

- 前端 `types/index.ts`：`WorkspaceTabDTO` 与 `TableDataRequest` 增加字段。
- 前端 `queryStore.ts`：`ObjectTab` 接口、`toDTO`/`fromDTO`、`setObject*` 系列方法同步。
- 前端 `TableBrowser.tsx`：视图状态从「组件局部 `useState`」上移到 store 或受控，读写唯一。
- 后端 `internal/web/*.go`：`WorkspaceTab` 序列化结构 + `fetchTableData` 请求解析同步。
- 后端存储：工作区 JSON 字段兼容旧数据（缺字段时取默认值，向前兼容）。

---

## 6. 易用性设计原则（超越主流软件）

目标不只是「对标主流」，而是在关键交互上**比 DataGrip/Navicat/DBeaver 更顺手**。以下为必须落实的超越点：

### 6.1 超越点清单

| 超越点 | 主流软件现状 | 我们的做法（更好） |
|---|---|---|
| **右键单元格快速过滤** | DataGrip/Navicat 有「按此值过滤」 | 提供「等于/不等于/包含」三合一，且**过滤后立即在表头高亮 + 顶部横幅提示**，避免「数据怎么变少了」的困惑 |
| **列头聚合菜单** | DBeaver 列头右键菜单项分散、层级深 | 菜单项**扁平聚合**：排序/筛选/隐藏/冻结/复制列名/类型一次性呈现，减少层级 |
| **NULL vs 空字符串** | 多数工具不区分（DataGrip 也常混） | 明确视觉区分：NULL=灰色斜体 `NULL`，空串=`""`（带引号）或 `(空)`，数据核对零误判 |
| **激活过滤的显式反馈** | 多数工具仅表头小图标，易被忽略 | 顶部横幅「已应用 N 个过滤条件」+ 一键清除，比主流更醒目 |
| **大字段懒加载** | 多数工具全量加载导致卡顿 | 已有按需加载（现状已领先），继续保留 |
| **过滤输入防抖** | 部分工具每敲一字符就打库 | 防抖 + 后端过滤，体验更顺滑 |

### 6.2 易用性验收标准（实现时逐项自查）

- [ ] 任何过滤操作后，用户能**一眼看到**「当前数据是被过滤过的」及其条件数量
- [ ] NULL 与空字符串在**任何视图**（列表/复制/导出/编辑弹层）都清晰区分
- [ ] 排序、筛选、显隐、冻结都可通过**列头右键**一次触达，无需翻找菜单
- [ ] 右键任意单元格可直接「等于此值」过滤，无需手输
- [ ] 主键列有明确视觉标识，编辑/删除定位不靠猜
- [ ] 大字段列不阻塞列表加载，点击才按需取完整值

---

## 7. 关键数据结构（新增草案）

```ts
// web/src/types/index.ts 新增

// 过滤操作符
export type FilterOp =
  | "eq" | "neq"          // 等于 / 不等于
  | "contains" | "notContains" // 包含 / 不包含
  | "startsWith" | "endsWith"
  | "gt" | "lt" | "gte" | "lte" // 数值/日期比较
  | "isNull" | "isNotNull"

// 单列过滤条件
export interface ColumnFilter {
  column: string
  op: FilterOp
  value?: unknown // isNull/isNotNull 时为空
}

// TableDataRequest 增加字段
export interface TableDataRequest {
  // ...现有字段
  filters?: ColumnFilter[] // 后端过滤条件（AND 叠加）
}

// 列显隐：隐藏列名列表；undefined/空 = 全部显示
export type HiddenColumns = string[]

// ---- 布局持久化（§5.6）----

// 表浏览视图布局（跟随 object tab 持久化）
export interface TableViewLayout {
  filters?: ColumnFilter[]      // 过滤条件
  sortColumn?: string           // 排序列
  sortOrder?: "asc" | "desc"    // 排序方向
  hiddenColumns?: HiddenColumns // 隐藏列
  pageSize?: number             // 页大小偏好
}

// WorkspaceTab（后端 DTO）扩展：object tab 增加 viewLayout
// 前端 ObjectTab（queryStore.ts）对应增加同名字段，toDTO/fromDTO 一一映射
```

---

> **文档版本**：v2.1
> **最后更新**：2026-08-14
> **状态**：全部 7 个 Phase 已实现并验证通过；写操作（UPDATE/DELETE/INSERT/大字段取值）也彻底复用 cydb 语句构建器，消除全部手写 SQL 字符串拼接。
