# SQL 收藏夹功能设计文档

> 状态：待评审（设计稿）
> 关联：查询浏览页 `/query` 右侧面板 `SQLHistoryPanel`（现有「执行历史 / 审计」两个 Tab）
> 目标：让用户在 SQL 历史区域中「把常用 SQL 永久收藏」，独立于会被环形上限冲掉的执行历史。

---

## 1. 背景与动机

现有 SQL 留存能力：

| 能力 | 现状 | 局限 |
|---|---|---|
| `SQLHistoryItem` 执行历史 | 每连接环形保留最近 200 条，可回填重跑 | 自动累积、不区分重要性，常用 SQL 会被新执行冲掉 |
| `WorkspaceTab` 工作区 | 查询 tab 按连接持久化（含 SQL/db/mode） | 是「当前编辑器状态」，关闭 tab 即丢失，非有意收藏 |
| `TaskConfig` 任务配置 | 导出/导入/迁移任务可保存 | 面向迁移类任务，非通用 SQL 片段 |

**缺口**：缺少「用户主动、跨会话、长期保留、可重命名」的常用 SQL 集合。收藏夹用于填补该缺口。

---

## 2. 方案概述

在查询页右侧面板 `SQLHistoryPanel` 中新增第三个 Tab：**执行历史 / 收藏 / 审计**。

- 收藏为**独立数据表**，不受执行历史 200 条环形上限影响。
- 收藏**全局共享**（不按连接隔离）：同一条收藏在任一连接下都可见，适合多环境（测试/预发/生产）复用同一类 SQL。`conn_id` / `db` 仅作来源标记，用于跨连接回填时的不一致提示。
- **回填方式历史/收藏/AI 面板三处一致化**：均复用 `WorkspaceLayout` 已有的 4 种回填动作（`replace_all` / `insert_cursor` / `append` / `replace_selection`），不再使用「点击即整篇覆盖」的 `applySQL` 直接覆盖逻辑。降低学习成本，且避免误吞当前编辑器内容。
- 提供两个「加入收藏」入口：
  1. **执行历史条目 hover 出 ☆ 按钮**：把已执行过的 SQL「捞回来」常驻。
  2. **查询编辑器工具栏 ☆ 收藏按钮**：写 SQL 时顺手保存当前全文。

---

## 3. 数据模型

### 3.1 行模型（参考 `sqlHistoryRow` 的 `cydb` tag 约定）

文件：`internal/store/models.go`

```go
// sqlFavoriteRow SQL 收藏行。
type sqlFavoriteRow struct {
	ID        string `cydb:"column:id;type:varchar;size:64;primary_key"`
	ConnID    string `cydb:"column:conn_id;type:varchar;size:32;index"`
	Title     string `cydb:"column:title;type:varchar;size:256"` // 用户可重命名，默认取 SQL 去注释后首行前 40 字符
	CreatedAt int64  `cydb:"column:created_at;type:bigint;index"`
	BodyJSON  string `cydb:"column:body_json;type:text"` // 完整 SQLFavorite 序列化
}

func (sqlFavoriteRow) TableName() string { return tableSQLFav }
```

### 3.2 值对象

文件：`internal/store/types.go`

```go
// SQLFavorite 收藏的 SQL（字段对齐 SQLHistoryItem，便于回填）。
type SQLFavorite struct {
	ID        string     `json:"id"`
	ConnID    string     `json:"connId"`
	Title     string     `json:"title"`
	DB        string     `json:"db"`     // 执行上下文：目标库
	Mode      SQLExecMode `json:"mode"`   // 执行模式（query/write）
	SQL       string     `json:"sql"`
	CreatedAt int64      `json:"createdAt"`
}
```

> 说明：`ConnID` 用于按连接隔离；`DB`/`Mode`/`SQL` 复用执行历史的回填语义。

### 3.3 建表常量

`tableSQLFav = "sql_favorites"`（在 `models.go` 顶部常量区与 `sqlHistoryRow` 的 `tableSQLHist` 并列）。

---

## 4. 后端接口

### 4.1 Store 层

文件：`internal/store/store.go`（接口） + `internal/store/sqlite.go`（实现）

```go
// 在 Store 接口中新增：
AddFavorite(f *SQLFavorite) error
ListFavorites(connID string) ([]*SQLFavorite, error)
DeleteFavorite(connID, id string) error
RenameFavorite(connID, id, title string) error
```

实现要点（`sqlite.go`）：

- `AddFavorite`：生成 `ID`（参考 `genToken`/`rand`），写入 `sqlFavoriteRow`。
- `ListFavorites`：`WHERE conn_id = ? ORDER BY created_at DESC`。
- `DeleteFavorite` / `RenameFavorite`：按 `conn_id + id` 定位，防止越权删除他人连接的收藏。
- 在 `AutoMigrate`（或初始化处）注册 `sqlFavoriteRow`，自动建表/补列（沿用现有 `cydb` 约定）。

### 4.2 Service 层

文件：`internal/service/service.go`（接口） + 实现文件

- 复用现有 service 结构，新增与上述 Store 方法一一对应的方法。
- 入参校验：SQL 非空、ConnID 合法（与现有 `SaveSQLHistory` 校验保持一致）。

### 4.3 Web 层（API）

文件：`internal/web/handler.go` + `internal/web/server.go`

新增 4 个 REST 端点，全部复用现有 `tokenAuth` 令牌中间件：

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/api/sql/favorites` | 新增收藏（body: SQLFavorite 子集） |
| GET  | `/api/sql/favorites?connId=xxx` | 列出该连接收藏 |
| DELETE | `/api/sql/favorites?connId=xxx&id=yyy` | 删除收藏 |
| PATCH | `/api/sql/favorites` | 重命名（body: connId/id/title） |

> 鉴权沿用现有 `tokenAuth`（`server.go` 第 70 行），无需新机制；令牌常量时间比较、IP 限速已具备。

---

## 5. 前端实现

### 5.1 类型与 API

- `web/src/types/index.ts`：新增 `SQLFavorite` 类型（对齐后端 JSON）。
- `web/src/api/sql.ts`：新增 `listFavorites / addFavorite / deleteFavorite / renameFavorite`。

### 5.2 状态管理

新增 `web/src/stores/favoriteStore.ts`（结构参考 `sqlHistoryStore.ts`）：

- `items: SQLFavorite[]`
- `load(connId)`：进入查询页/切换连接时调用
- `add / remove / rename`：乐观更新 + 后端同步

### 5.3 UI 改动

文件：`web/src/App.tsx` 的 `SQLHistoryPanel`：

1. `Tabs` 新增第三个 `TabsTrigger value="favorite"`（第 384-394 行区域）。
2. 收藏 Tab 内容：复用执行历史的卡片样式（`line-clamp-2` 展示 SQL）。
   - **点击收藏项**：弹出与 AI 面板一致的「回填方式」菜单（`全部替换 / 插入光标处 / 追加末尾 / 替换所选`）。
   - **回填上下文语义区分**：仅当选择 `replace_all`（全部替换，整条替换语义）时，才一并还原 `db`/`mode` 上下文；`insert_cursor`/`append`/`replace_selection` 仅插入文本，**不切换当前库/模式**（避免把收藏的库误切到正在编辑的连接上）。
   - **空态文案**：若当前连接无收藏，明确展示「当前连接暂无收藏」，避免用户误以为数据丢失。
   - hover 出「✎ 重命名 / 🗑 删除」；**删除走 `confirm` 确认弹窗**（对齐执行历史删除，且误删不可恢复，比历史删除更需防护）。
3. 执行历史 Tab 的 hover 增加「☆ 收藏」按钮，点击调用 `favoriteStore.add`。

文件：`web/src/components/SqlEditor.tsx`：

4. 工具栏在「解释/优化」按钮旁新增「☆ 收藏」，点击把当前 `queryActive.sql + db + mode + connId` 存入收藏。

> 回填不再使用 `applySQL` 整篇覆盖，改为复用 `WorkspaceLayout` 的 `applyByAction`（第 867-884 行），与 AI 面板共用同一底层实现（`replace_all`/`insert_cursor`/`append`/`replace_selection`），三处交互一致、零新交互成本。

---

## 6. 交互流程

1. 用户在查询页写一条 SQL → 点工具栏「☆ 收藏」→ 出现在「收藏」Tab。
2. 用户翻执行历史发现一条好 SQL → hover 点「☆」→ 加入收藏。
3. 日后在任一查询会话，点「收藏」Tab 中任一条 → 选回填方式：
   - 选「全部替换」→ 整篇替换并还原该收藏的 db/mode，可直接执行；
   - 选「插入光标处 / 追加末尾 / 替换所选」→ 仅插入 SQL 文本，不切换当前库/模式，便于当片段拼进正在写的查询。
4. 重命名：双击或「✎」编辑标题；删除：hover「🗑」→ `confirm` 确认后删除（不可恢复，需二次确认）。

---

## 7. 评审自查（完整性 / 易用性 / 安全性 / 性能 / 可实现性）

### 7.1 完整性 ✅（基本覆盖，1 处待定）

- 写入路径：工具栏收藏 + 历史条目收藏 → **两条写入路径均已规划**。
- 读取/展示：收藏 Tab 列表 + 回填 → **覆盖**。
- 更新：重命名 → **覆盖**。
- 删除：收藏 Tab 删除 → **覆盖**。
- ⚠️ **已决议**：收藏按 `connId` 隔离（与执行历史一致），初期不做跨连接共享，符合直觉。

### 7.2 易用性 ✅

- 入口就近：工具栏 + 历史条目 hover，符合「写完顺手存 / 翻到顺手存」心智。
- 回填零跳转：复用 `WorkspaceLayout.applyByAction`，与 AI 面板同一套「全部替换 / 插入光标处 / 追加末尾 / 替换所选」菜单，**三处交互完全一致，用户只学一次，学习成本最低**。
- 与现有 Tab 视觉一致，无新学习成本。
- ⚠️ 建议：收藏项支持「按标题搜索/排序」若收藏量大时有价值，初期可不做（见性能节）。

### 7.3 安全性 ✅（复用现有机制，风险低）

- **鉴权**：全部端点走现有 `tokenAuth` 中间件（令牌 + IP 限速 + 常量时间比较），无新攻击面。
- **越权**：`DeleteFavorite`/`RenameFavorite` 均按 `conn_id + id` 双重定位，防止删除/篡改其他连接收藏。
- **注入**：SQL 存为 `BodyJSON` TEXT 字段，参数化存储，不拼接到 SQL 语句，**无 SQL 注入风险**。
- **XSS**：前端渲染 SQL 用 React 文本节点（非 `dangerouslySetInnerHTML`），自动转义。
- ⚠️ **输入校验**：后端需限制 `SQL` 长度（如 ≤ 64KB，对齐 `BodyJSON` TEXT 容量）与 `Title` 长度（≤ 256），拒绝空 SQL，避免存储滥用。

### 7.4 性能 ✅

- **建表**：`sql_favorites` 独立表，`conn_id` 索引，列表查询 `ORDER BY created_at DESC` 走索引。
- **体量**：收藏为用户主动行为，数量远小于执行历史（200 条环形），单连接收藏通常数十~数百条，前端一次性渲染无压力。
- **加载时机**：复用 `loadSQLHistory` 的触发点（进入查询页 / 切换连接），不新增额外请求频次。
- ⚠️ 若未来收藏量极大（上千），可加虚拟滚动或分页，初期不需要。

### 7.5 可实现性 ✅（高度可行）

- **模式完全对齐现有代码**：`sqlFavoriteRow` 复用 `sqlHistoryRow` 的 `cydb` tag + `BodyJSON` 序列化范式，建表/补列零新机制。
- **Store/Service/Web/前端分层**与现有 SQL 历史一一对应，改动面可控、无架构冲突。
- **回填逻辑** `applySQLByAction`（queryStore）已新增，与 AI 面板共用四动作（`replace_all`/`insert_cursor`/`append`/`replace_selection`），三处交互一致。
- **前端 Tab 结构** `SQLHistoryPanel` 已用 `Tabs`，加一个 `TabsTrigger` 即插即用。
- 依赖：无新增第三方库（前端图标用现有 `lucide-react`）。

---

## 8. 已决议事项

1. **收藏隔离粒度**：**全局共享**（不按连接隔离），多环境复用同一类 SQL；`conn_id`/`db` 仅作来源标记。
2. **重命名 UI 形态**：双击标题行内编辑（轻量，不弹窗）。
3. **收藏上限**：初期不设上限（或单连接 1000 软上限，超出时提示）。
4. **回填一致化**：历史 / 收藏 / AI 面板三处共用 `replace_all / insert_cursor / append / replace_selection` 四种回填动作（来自 `queryStore.applySQLByAction`），仅 `replace_all` 还原 db/mode 上下文，其余动作只插文本。
5. **跨连接/跨库提示**：收藏全局可见，当收藏来源 `conn_id`/`db` 与当前连接/库不一致时，卡片显示来源标记（连接 X / 库 Y）与 ⚠ 提示；点击回填（尤其「全部替换」会切库切模式）时弹出 `toast.warning` 告知用户确认目标正确。删除仍需 `confirm`（不可恢复）。
5. **删除确认**：收藏删除走 `confirm` 弹窗（误删不可恢复）。
6. **默认标题**：SQL 去注释后首行前 40 字符；空 SQL 拒绝收藏。
7. **空态文案**：当前连接无收藏时显示「当前连接暂无收藏」。

## 9. 实施步骤（全链路，按现有分层约定）

1. `store/types.go` + `store/models.go`：加 `SQLFavorite` / `sqlFavoriteRow` / `tableSQLFav`，注册 AutoMigrate。
2. `store/store.go` + `store/sqlite.go`：接口 + 4 个方法实现。
3. `service/service.go` + 实现：接口 + 4 个方法（含入参校验）。
4. `web/handler.go` + `web/server.go`：4 个端点 + 路由注册（复用 `tokenAuth`）。
5. `types/index.ts` + `api/sql.ts`：类型 + API 调用。
6. `stores/favoriteStore.ts`：状态管理。
7. `App.tsx`：新增收藏 Tab + 历史条目 ☆ 按钮。
8. `SqlEditor.tsx`：工具栏 ☆ 收藏按钮。
9. `read_lints` 轻量检查（按项目约定不单独跑 build）。

---

## 10. 实施状态（已落地）

全链路已实现并通过 `read_lints` 轻量检查（0 错误）：

- 后端：`store/types.go`（SQLFavorite）、`store/models.go`（sqlFavoriteRow + AutoMigrate）、`store/store.go`（接口）、`store/sqlite.go`（建表+全局 4 方法）、`service/sql.go` + `persist.go`（含入参校验：SQL 非空/≤64KB、标题≤256、默认标题取首行前 40 字符）、`web/sql.go`（4 端点，全局共享）、`web/server.go`（路由，复用 tokenAuth）。
- 前端：`types/index.ts`、`api/sql.ts`、`stores/favoriteStore.ts`、`lib/editorRef.ts`（共享编辑器实例）、`stores/queryStore.ts`（applySQLByAction）、`App.tsx`（SQLHistoryPanel 三 Tab + 回填菜单 + 收藏列表 + ☆/删除/重命名）、`WorkspaceLayout.tsx`（工具栏 ☆ 收藏 + onReady 注册编辑器）。

**回填一致化已闭环**：历史、收藏、AI 面板三处共用同一「全部替换/插入光标处/追加末尾/替换所选」菜单；仅「全部替换」还原 db/mode 上下文。
