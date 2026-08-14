# 前端功能规划与可执行计划（类似 Navicat 的数据库管理终端）

> 文档状态：计划（待评审 / 可执行）
> 生成日期：2026-08-12
> 目标：在现有 Web 前端基础上，补齐"类 Navicat"的核心数据管理能力，并通过**先进性、易用性、安全性**三维审核，产出可落地的执行清单。

---

## 0. 现状基线（已调研）

### 0.1 前端实际技术栈（已核实 package.json）
- 框架：React 18 + TypeScript 5 + Vite 5 + react-router-dom 6。
- 状态：**Zustand 5**（已用）。
- UI：**Radix UI + Tailwind CSS + lucide-react + sonner**（**非 Ant Design**，原稿误写已更正）。
- SQL 解析：`node-sql-parser` 已引入（可用于前端语句分类/危险词检测）。
- ⚠️ **待引入依赖**：Monaco Editor（或 CodeMirror）、虚拟滚动库（如 `@tanstack/react-virtual`）当前 `package.json` 均未包含，需在执行阶段显式 `npm i`。

| 模块 | 入口 | 状态 |
|------|------|------|
| 连接管理 | `ConnectionForm` / `ConnectionList` | 已有（`api/connections`） |
| 数据导出 | `ExportPage` | 已有 |
| 数据导入 | `ImportPage` | 已有 |
| 数据迁移 | `MigrationPage` | 已有 |
| 结构对比 | `ComparePage` | 已有 |
| 快照 | `SnapshotPage` / `SnapshotComparison` | 已有 |
| 数据字典 | `DictionaryPage` | 已有（结构/数据双模式） |

目录结构清晰：`pages/`（业务页）、`components/`（复用组件）、`api/`（请求封装）、`stores/`（Zustand）、`types/`（TS 类型）。

### 0.2 后端（Go）已具备（已核实 conn.go / handler.go）
- `internal/engine`：连接类型 `DBConnInfo`（含 `DBConnection{Host,Port,Un,Pd,DBName,SubType}`、`Type`、`SubType`）。
- 连接池：`Connect`（短生命周期，调用方 Close）/`ConnectPooled`（进程级 `cydb.DBMgr` 池，`GetOrCreateCli` 复用，禁止 Close）。
- 方言安全：`EscapeTable(dbType, subType, name)` / `EscapeColumn(dbType, subType, name)` 已提供，标识符必须走此转义，禁止字符串拼接。
- `internal/cli/sqlcmd`：会话式 SQL 引擎（`session` 含查询、历史、交互式执行），但**目前仅 CLI 可用**。
- `internal/web`：`Connection` 保存、导出/导入/迁移/对比/快照/字典的 Web API **已全部接入**；**唯独缺 SQL 执行与对象树 API**（本计划补齐）。

### 0.3 关键缺口（本计划聚焦）
1. **没有类 Navicat 的"SQL 查询终端 / 数据浏览器"**（核心缺失）。
2. **后端 Web 端没有 SQL 执行 API**：`sqlcmd` 引擎目前只能走 CLI，前端无法调用。
3. 已有连接管理但未与"对象树 / 查询"联动成统一工作区。

> 结论：前端骨架已就绪，本次规划以**"新增查询终端 + 数据浏览器"**为主线，复用现有连接管理、API 封装模式与后端 `sqlcmd` 引擎，而非推倒重来。

---

## 1. 功能规划（类 Navicat 能力矩阵）

按 Navicat 心智模型，规划 4 个新增核心能力（P0/P1 分级）：

### P0 — 查询终端（SQL Console）
- 多 Tab 编辑器（Monaco Editor，SQL 语法高亮 + 自动补全表名/列名）。
- 选中即执行 / 整段执行，结果以虚拟化表格展示（支持大数据量滚动）。
- 执行计划信息（影响行数、耗时）、错误提示。
- 历史记录（复用后端 `sqlcmd` history）。

### P0 — 数据浏览器（Object Browser）
- 左侧对象树：库 → 表/视图/函数，懒加载展开。
- 双击表打开"数据视图"：分页浏览、列过滤、按主键排序。
- 表结构（DDL）查看（复用 `DictionaryPage` 的能力）。

### P1 — 结构/数据编辑（类 Navicat 网格编辑）
- 数据视图内联编辑单元格、新增/删除行，批量提交事务。
- 风险护栏：写操作二次确认 + 仅对明确主键表开放。

### P1 — 连接工作区（Workspace）
- 将"连接列表"升级为侧边栏对象浏览器，统一入口。
- 连接健康检测（Ping）、超时自动断开。

### 跨场景覆盖（补齐使用场景盲区）
规划需显式覆盖以下场景，否则不视为完备：
- **多方言差异**：MySQL / PostgreSQL / Oracle 在分页（`LIMIT` vs `OFFSET FETCH` vs `ROWNUM`）、语句超时（`max_execution_time` vs `statement_timeout` vs `/*+ */`）、DDL 语法、对象树元数据查询上均不同；后端须按 `SubType` 路由，前端补全元数据由后端统一返回，前端不做方言判断。
- **只读账号 / 只读库**：连接若仅有 `SELECT` 权限，P1 网格编辑与写操作入口**自动隐藏**，仅保留查询/浏览。
- **连接失败 / 会话过期**：对象树加载失败、会话超时回收时，前端需有降级态（重试按钮 + 明确错误文案），不允许白屏；多 Tab 执行遇断连自动提示重连。
- **响应式 / 桌面优先**：明确桌面优先（Navicat 类工具不以移动端为目标），但侧边栏在窄屏需可折叠（`< lg` 抽屉式），保证基本可用。
- **结果集导出**：查询结果支持导出 CSV/Excel，导出同样受 LIMIT/脱敏护栏约束；大数据导出走流式/分批，不一次性全量入内存。
- **并发与事务**：同一连接多 Tab 并行查询互不阻塞（读）；P1 写操作为显式"开启事务"模式，避免隐式长事务锁表。
- **空结果 / 超长字段 / 二进制**：NULL、超长文本、BLOB 需有占位与展开策略，不撑破布局。

---

## 2. 三维审核

### 2.1 先进性（技术前瞻性）
| 维度 | 评估 | 规划决策 |
|------|------|----------|
| 编辑器 | Monaco 体量大；**当前 package.json 未含**，需引入 | ⚠️ 待引入。首选 `@monaco-editor/react`（SQL 高亮+补全强）；若 bundle 超限改用 CodeMirror 6（`@codemirror/lang-sql`，更轻量）。二者均满足需求 |
| 大数据渲染 | 结果集可能很大 | ✅ 虚拟滚动（待引入 `@tanstack/react-virtual`，当前未含）+ 后端分页续取 |
| 状态管理 | 已有 Zustand 5，契合 | ✅ 复用，新增 `queryStore` / `objectTreeStore` |
| 前后端协同 | SQL 引擎已存在 | ✅ 后端暴露 `/api/sql/*`，在 `sqlcmd` 之上抽薄封装复用 `engine`，前端不重写方言逻辑 |
| 协议 | 长结果集 | ✅ 采用 offset/limit 分页返回避免内存爆炸；后续可选 SSE 流 |
| 架构 | 单页工作区 | ✅ 路由采用 `/workspace/:connId` 嵌套布局 |

### 2.2 易用性（UX）
| 维度 | 痛点 | 规划决策 |
|------|------|----------|
| 上手成本 | 多工具分散 | ✅ 统一工作区：连接 → 对象树 → 查询/浏览 一步到位 |
| SQL 书写 | 手写易错 | ✅ 自动补全 + 表/列悬停提示 + 格式化和模板 |
| 结果查看 | 宽表滚动难 | ✅ 列宽自适应、冻结首列、JSON 单元格展开 |
| 反馈 | 执行无感知 | ✅ Loading / 耗时 / 行数 / 错误码即时反馈 |
| 多任务 | 单查询阻塞 | ✅ 多 Tab 并行查询，互不阻塞 |
| 可学习性 | 新手不会 SQL | ✅ 提供"可视化筛选"入口生成 SELECT |
| 响应式 | 窄屏不可用 | ✅ 桌面优先；侧边栏 `< lg` 折叠为抽屉，保证基本可用 |
| 可访问性 | 键盘/读屏缺失 | ✅ 编辑器与结果表支持键盘操作；错误用 sonner 提示而非仅色块 |

### 2.3 安全性（Security）—— 重点审核
| 风险 | 严重度 | 防护措施（必须落地） |
|------|--------|----------------------|
| SQL 注入 / 越权执行 | 高 | 后端仅做"转发执行"，**连接凭据只存服务端**；前端不发密码 |
| DDL/DML 误操作 | 高 | 写操作（INSERT/UPDATE/DELETE/DROP）二次确认 + 危险语句拦截清单 |
| 结果集过大拖垮服务 | 中 | 后端对 SELECT 强制 `LIMIT` 上限（如 1000 行预览），分页续取 |
| 敏感数据泄露 | 中 | 列值脱敏开关（手机号/身份证/密码字段）；导出受控 |
| 会话劫持 | 中 | 连接会话绑定用户态；超时回收；禁止跨连接复用 |
| 命令注入（库名/表名） | 中 | 所有标识符走方言 `EscapeTable/EscapeColumn`，禁字符串拼接 |
| 审计 | 低 | 记录 SQL 审计日志（谁/何时/何连接/语句），落 `sqlcmd` history |
| 资源耗尽 | 中 | 单连接并发上限、单语句超时（按方言：`max_execution_time`(MySQL) / `statement_timeout`(PG) / hint(ORACLE)，由后端 SubType 路由） |
| 越权写操作 | 高 | 前端用 `node-sql-parser` 做语句分类；**只读账号/只读库下隐藏全部写入口**，仅后端也二次校验 |
| 结果导出泄露 | 中 | 导出 CSV/Excel 同样受 LIMIT 与脱敏护栏约束，禁止绕过预览直接 dump 全库 |

> 安全红线：**前端永不持有数据库密码**；所有 SQL 经由后端受控执行；危险写操作默认禁用，需显式开启。

---

## 3. 可执行计划（分阶段任务清单）

### 阶段 A — 后端 SQL 执行 API（前置，P0）
- [ ] `internal/web/handler.go` 新增：
  - `POST /api/sql/query`：接收 `{connId, sql, limit, offset}` → 调用 `sqlcmd` 引擎执行 → 返回列定义 + 行数据 + 元数据（行数/耗时）。
  - `POST /api/sql/exec`：写操作执行（受护栏控制）。
  - `GET  /api/object-tree?connId=`：返回库/表/视图树（懒加载子节点）。
  - `GET  /api/sql/history?connId=`：返回历史。
- [ ] 在 `engine` 之上新增薄封装（勿污染 `sqlcmd` CLI 交互逻辑）：`RunQuery(cli *cydb.DBCli, dbType, subType, sql string, limit, offset int)`、`RunExec(cli, dbType, subType, sql string)`，由 `sqlcmd` 的 `session` 复用同一执行内核，Web handler 调用此封装。
- [ ] 连接复用：Web 侧用 `engine.ConnectPooled(info, dbName)` 建立按 `connId+dbName` 缓存的池化会话（`GetOrCreateCli` 已去重），调用方**禁止 Close**，由 `CloseAllCliPool` 统一释放。
- [ ] 安全护栏：`limit` 上限常量；DML/DDL 关键词白/黑名单；语句超时 context。
- [ ] 新增后端单测：`handler_test.go` 覆盖查询/拦截/分页。

### 阶段 B — 前端类型与 API 封装
- [ ] `types/index.ts` 新增：`SqlResult`、`SqlColumn`、`ObjectNode`、`QueryHistory`、`SqlExecRequest`、`SqlQueryRequest`。
- [ ] `api/sql.ts` 新增：`querySql`、`execSql`、`fetchObjectTree`、`fetchSqlHistory`（沿用 `request` 封装与错误格式）。

### 阶段 C — 前端状态与布局
- [ ] `stores/queryStore.ts`：多 Tab 编辑器状态、结果缓存、执行中状态。
- [ ] `stores/objectTreeStore.ts`：树懒加载、展开状态、选中节点。
- [ ] 新增工作区布局 `components/WorkspaceLayout.tsx`：左侧对象树 + 右侧 Tab 区 + 底部结果面板。
- [ ] 路由：新增 `/workspace/:connId`，接入 `App.tsx` 路由表。

### 阶段 D — 查询终端 UI
- [ ] 安装依赖：`npm i @monaco-editor/react`（或 CodeMirror 备选）与 `@tanstack/react-virtual`。
- [ ] `components/SqlEditor.tsx`：集成编辑器，SQL 语言、表/列补全（从 objectTree 取元数据）。
- [ ] `components/ResultGrid.tsx`：虚拟滚动表格、列宽、JSON 展开、复制单元格。
- [ ] `pages/QueryPage.tsx`：多 Tab、执行按钮、历史侧栏、错误提示条。

### 阶段 E — 数据浏览器 UI
- [ ] `components/ObjectTree.tsx`：库/表/视图层级，懒加载，双击打开数据视图。
- [ ] `pages/DataBrowserPage.tsx`：分页浏览 + 列过滤 + 排序 + 表结构(DDL) Tab。
- [ ] （P1）网格内联编辑 + 批量提交事务（带护栏确认）。

### 阶段 F — 安全与体验收尾
- [ ] 写操作二次确认弹窗 + 危险语句拦截提示。
- [ ] 结果集脱敏开关（配置化字段名匹配）。
- [ ] SQL 审计日志落盘。
- [ ] 连接健康检测（Ping）与超时回收。

---

## 4. 验收标准（Definition of Done）
1. 用户可在 Web 端选择已有连接 → 打开 SQL 终端 → 编写并执行 SELECT → 以表格查看结果（含耗时/行数）。
2. 用户可通过左侧对象树浏览库表，双击查看表数据与结构。
3. 任何写操作（DML/DDL）均触发二次确认；危险语句被拦截并提示。
4. SELECT 结果受 `LIMIT` 上限保护，支持分页续取，不拖垮后端。
5. 数据库密码不出现在前端网络请求或本地存储。
6. 新增后端单测通过；前端 `tsc` 无类型错误；`npm run build` 成功。
7. 非功能项达标：① 万行结果集滚动流畅（虚拟滚动，无卡顿）；② 同连接 3 个并行查询互不阻塞；③ 越权用例（前端伪装写 SQL 但后端只读账号）被后端拦截；④ MySQL/PostgreSQL/Oracle 三方言对象树与分页均可加载。
8. 降级场景：断连/会话过期时前端有重试入口而非白屏；只读账号下写入口不可见。

---

## 5. 风险与权衡
- **后端改动风险**：`sqlcmd` 当前面向 CLI，需抽离纯执行逻辑供 Web 复用；建议在 `engine` 层新增薄封装，避免污染 CLI 交互逻辑。
- **结果集内存**：浏览器与后端都要对大结果集做分页/虚拟滚动，禁止一次性全量拉取。
- **安全优先级最高**：查询终端直接暴露数据库执行能力，护栏（limit/拦截/审计）必须与功能同步上线，不可后置。
- **范围控制**：P1（网格编辑）可在 P0 稳定后再做，先交付"查"能力，再交付"改"能力，降低一次性复杂度。

> 建议执行顺序：A（后端 API）→ B/C（类型/状态/布局）→ D（查询终端）→ E（数据浏览器）→ F（安全收尾）。每阶段可独立提测。
