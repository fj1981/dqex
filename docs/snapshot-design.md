# 快照功能设计文档

> **文档状态**: 已落地（cli/snapshot.go + service/snapshot.go + engine/snapshot.go + engine/snapshot_compare.go，Web `/snapshots` API 已挂载）  
> **最后更新**: 2026-08-18  （设计稿初版 2025-08-11） 
> **设计原则**: 最大程度复用现有对比引擎和对比报告组件，快照作为独立一级功能，与实时对比互补。

---

## 1. 概述

### 1.1 定位

快照 (Snapshot) 是对某个数据库在某个时间点的"冻结副本"，记录库内所有表的结构定义（列名/类型/可空/主键）和数据摘要（行数/内容哈希/采样）。用户可以随时将数据库当前状态与任意历史快照进行对比，快速发现变化。

### 1.2 与现有实时对比的关系

| | 实时对比 (CompareView) | 快照对比 (SnapshotView) |
|---|---|---|
| 源端 | 在线连接，实时查询 | 历史快照（JSON 文件），离线读取 |
| 目标端 | 在线连接 | 在线连接 |
| 使用模式 | 一次性四步向导 | 持续性管理：创建→复用→对比 |
| 核心场景 | 两个在线库之间的差异检查 | 版本基线对比：上线前/后检查、日常巡检 |

两者共享底层对比引擎 (`engine/compare.go`) 和前端对比报告组件 (`CompareReport`)。

---

## 2. 数据模型

### 2.1 快照存储模型（engine/snapshot.go 新增）

所有快照模型定义在 `engine` 包，通过 `service` 包的类型别名对外暴露，与现有架构保持一致。

```go
// Snapshot 快照完整数据（单个快照文件内容）
type Snapshot struct {
    ID          string          `json:"id"`
    Name        string          `json:"name"`
    Description string          `json:"description,omitempty"`
    ConnID      string          `json:"connId"`      // 来源连接 ID（允许连接被删除后快照仍可读）
    ConnLabel   string          `json:"connLabel"`   // 冗余连接名称
    DBName      string          `json:"dbName"`
    DBType      string          `json:"dbType"`
    CreatedAt   time.Time       `json:"createdAt"`
    TableCount  int             `json:"tableCount"`
    TotalRows   int64           `json:"totalRows"`
    Tables      []SnapshotTable `json:"tables"`
}

// SnapshotTable 单表快照
type SnapshotTable struct {
    Name       string           `json:"name"`
    RowCount   int64            `json:"rowCount"`
    Columns    []SnapshotColumn `json:"columns"`
    PrimaryKey []string         `json:"primaryKey,omitempty"`
    RowSamples []map[string]any `json:"rowSamples,omitempty"` // 前 N 行采样（创建时可选）
}

// SnapshotColumn 列快照
type SnapshotColumn struct {
    Name       string `json:"name"`
    DataType   string `json:"dataType"`
    Nullable   bool   `json:"nullable"`
    PrimaryKey bool   `json:"primaryKey"`
    Position   int    `json:"position"`
}

// SnapshotInfo 快照摘要（索引文件条目，不包含表详情以减少列表加载开销）
type SnapshotInfo struct {
    ID          string    `json:"id"`
    Name        string    `json:"name"`
    Description string    `json:"description,omitempty"`
    ConnID      string    `json:"connId"`
    ConnLabel   string    `json:"connLabel"`
    DBName      string    `json:"dbName"`
    DBType      string    `json:"dbType"`
    TableCount  int       `json:"tableCount"`
    TotalRows   int64     `json:"totalRows"`
    CreatedAt   time.Time `json:"createdAt"`
}

// SnapshotCompareOptions 快照对比选项
type SnapshotCompareOptions struct {
    SnapshotID    string      `json:"snapshotId"`
    TargetConn    string      `json:"targetConn"`
    Target        *DBConnInfo `json:"target,omitempty"`
    Tables        []string    `json:"tables,omitempty"`        // 限定对比的表，nil=全部
    Threshold     int         `json:"threshold"`
    IgnoreColumns []string    `json:"ignoreColumns,omitempty"`
    ForceData     bool        `json:"forceData,omitempty"`
}
```

### 2.2 对比结果复用

快照对比结果**完全复用**现有 `CompareResult` 结构，无需新增结果类型。快照侧数据转换为 `CompareOptions` 中的源端后，直接调用 `engine.RunCompare()` 的离线变体 `engine.RunSnapshotCompare()`。

---

## 3. 存储方案

### 3.1 目录结构

```
~/.dbimpex/
  snapshots/
    index.json              # 快照索引（[]SnapshotInfo），按创建时间倒序
    <snapshot-id>.json      # 单个快照完整数据（Snapshot）
```

**设计理由**：
- 索引与详情分离：列表加载时只读 `index.json`（几 KB），不加载所有表定义
- 单个快照文件独立：方便删除、导出、备份
- 不使用加密：快照不含密码，仅含表结构（列名/类型）和行数统计，敏感度低

### 3.2 持久化路径

在 `PersistMgr` 中新增 `snapshotDir`：

```go
const SnapshotDirName = "snapshots"

func (p *PersistMgr) SnapshotDir() string { return p.snapshotDir }

func (p *PersistMgr) snapshotIndexPath() string {
    return filepath.Join(p.snapshotDir, "index.json")
}
func (p *PersistMgr) snapshotDataPath(id string) string {
    return filepath.Join(p.snapshotDir, id+".json")
}
```

---

## 4. 后端设计

### 4.1 引擎层 (`engine/snapshot.go`)

核心函数：

```go
// CreateSnapshot 创建快照：连接数据库，读取所有表结构 + 行数 + 可选采样
func CreateSnapshot(ctx context.Context, conn *DBConnInfo, dbName string, opts CreateSnapshotOptions, cb ProgressFunc) (*Snapshot, error)

// LoadSnapshot 从文件加载快照完整数据
func LoadSnapshot(path string) (*Snapshot, error)

// RunSnapshotCompare 快照 vs 在线库对比
// 内部将快照表结构转为 CompareOptions 的源端描述，然后复用现有 compareColumns/compareTableData 逻辑
func RunSnapshotCompare(ctx context.Context, snapshot *Snapshot, targetCli *cydb.DBCli, opts SnapshotCompareOptions, cb ProgressFunc) (*CompareResult, error)
```

**关键设计决策**：`RunSnapshotCompare` 不重新实现对比逻辑，而是：
1. 将快照的表结构注入为"源端虚拟连接"
2. 结构对比直接对比快照列定义 vs 在线库列定义
3. 数据对比：快照侧数据不可得（快照不存全量数据），走在线库全量查询 + 快照侧行数/采样辅助判断

**数据对比策略**：
- 快照不存全量数据，所以无法做逐行对比
- 走"结构对比 + 行数对比 + 采样差异"模式
- 对比时先从在线库加载数据，以快照结构为基准判断列变化，以快照行数为基准判断数据量变化
- 如果用户需要逐行数据对比，应使用实时对比（两个在线库）

### 4.2 服务层 (`service/snapshot.go`)

```go
// CreateSnapshot 创建快照（同步，CLI 用）
func (s *Service) CreateSnapshot(ctx context.Context, connID, dbName, name, description string, includeSamples bool, cb ProgressFunc) (*Snapshot, error)

// ListSnapshots 列出所有快照摘要
func (s *Service) ListSnapshots() []SnapshotInfo

// GetSnapshot 获取单个快照完整数据
func (s *Service) GetSnapshot(id string) (*Snapshot, error)

// DeleteSnapshot 删除快照
func (s *Service) DeleteSnapshot(id string) error

// StartSnapshotCompare 异步启动快照对比任务
func (s *Service) StartSnapshotCompare(opts SnapshotCompareOptions, taskConfigID string) (string, error)

// GetSnapshotCompareResult 读取快照对比结果
func (s *Service) GetSnapshotCompareResult(taskID string) (*CompareResult, error)
```

**异步任务执行历史**：
- 快照对比结果落盘到 `compares/snapshot-compare-<taskID>.json`
- `taskType` 使用 `"snapshot_compare"`，与现有 `"compare"` 区分
- 执行历史摘要格式：`快照对比: <快照名> vs <连接名> · <库名>`

### 4.3 Web API 层 (`handler.go` 新增)

| 方法 | 路径 | Handler | 说明 |
|---|---|---|---|
| POST | `/api/snapshots` | `handleCreateSnapshot` | 创建快照 |
| GET | `/api/snapshots` | `handleListSnapshots` | 快照列表 |
| GET | `/api/snapshots/:id` | `handleGetSnapshot` | 快照详情 |
| DELETE | `/api/snapshots/:id` | `handleDeleteSnapshot` | 删除快照 |
| POST | `/api/snapshots/compare` | `handleSnapshotCompare` | 启动快照对比 |
| GET | `/api/snapshots/compare/result` | `handleSnapshotCompareResult` | 查询快照对比结果 |

请求/响应结构遵循现有 `cygin.Handle` 规范：

```go
// 创建快照
type CreateSnapshotReq struct {
    ConnID         string `json:"connId" binding:"required"`
    DBName         string `json:"dbName" binding:"required"`
    Name           string `json:"name" binding:"required"`
    Description    string `json:"description"`
    IncludeSamples bool   `json:"includeSamples"`
}

// 快照对比
type SnapshotCompareReq struct {
    SnapshotID    string              `json:"snapshotId" binding:"required"`
    TargetConn    string              `json:"targetConn" binding:"required"`
    Target        *service.DBConnInfo `json:"target,omitempty"`
    Tables        []string            `json:"tables,omitempty"`
    Threshold     int                 `json:"threshold"`
    IgnoreColumns []string            `json:"ignoreColumns,omitempty"`
    ForceData     bool                `json:"forceData,omitempty"`
    TaskConfigID  string              `json:"taskConfigId"`
}

// 快照对比结果查询
type SnapshotCompareResultReq struct {
    TaskID string `query:"taskID" binding:"required"`
}
```

### 4.4 安全设计

1. **连接验证**：创建快照前必须验证连接可用（复用 `TestConnection`）
2. **文件安全**：
   - 快照文件仅通过 API 读取，不直接暴露路径
   - 快照 ID 使用 xid 生成（不可预测）
   - 删除快照时确认文件存在再删除，避免路径遍历
3. **超时保护**：快照创建涉及全表扫描（读结构），需设置 context 超时（默认 5 分钟）
4. **表数量限制**：单库超过 500 张表时提示用户确认（避免误操作大库）

---

## 5. CLI 设计 (`cli/snapshot.go`)

```bash
# 快照管理
dbx snapshot create   -c <连接名> -d <数据库> -n <名称> [-desc <备注>] [--samples]
dbx snapshot list     [-c <连接名>] [-t <类型>]
dbx snapshot show     -i <快照ID>
dbx snapshot delete   -i <快照ID>

# 快照对比
dbx snapshot compare  -i <快照ID> -c <连接名> [-d <数据库>] [--threshold N]
dbx snapshot diff     -i <快照A> -j <快照B>   # 两个快照之间的结构对比（Phase 2）
```

CLI 命令遵循现有模式：使用 cobra，通过 `service` 包的类型别名调用业务逻辑。

---

## 6. 前端设计

### 6.1 路由与导航

`App.tsx` 中新增：

```typescript
// NAV 数组
{ path: "/snapshots", label: "快照", desc: "快照 ↔ 对比", icon: Camera },

// Routes
<Route path="/snapshots" element={<SnapshotView />} />

// TYPE_BY_PATH（右侧面板历史过滤）
"/snapshots": "snapshot_compare",
```

### 6.2 页面布局：SnapshotView.tsx

**三区域布局**：左侧快照列表 + 右侧主操作区（详情/对比/报告）：

```
┌──────────────────────────────────────────────────────────────────────┐
│  📸 快照管理                                            [+ 新建快照]  │
│  冻结数据库快照，随时与当前状态对比变化                                 │
├──────────────┬───────────────────────────────────────────────────────┤
│              │                                                       │
│  快照列表     │  主操作区（状态驱动）                                    │
│  (限高内滚)   │                                                       │
│              │  ┌─ 空闲态 ──────────────────────────────────────┐    │
│ ┌──────────┐ │  │  📸 选择一个快照查看详情                        │    │
│ │ 上线前检查 │ │  │  或点击「+ 新建快照」创建第一个快照               │    │
│ │ mydb      │ │  └────────────────────────────────────────────┘    │
│ │ 32表 1.2M │ │                                                     │
│ │ 08-10     │ │  ┌─ 选中快照详情态 ────────────────────────────┐    │
│ └──────────┘ │  │  快照: 上线前检查                              │    │
│ ┌──────────┐ │  │  连接: 生产库 (MySQL)  ·  创建: 08-10 14:30    │    │
│ │ 周检基线  │ │  │  表: 32  ·  行: 1.2M                           │    │
│ │ testdb    │ │  │                                               │    │
│ │ 15表 50K  │ │  │  [📊 对比当前库]  [🗑 删除快照]               │    │
│ │ 08-09     │ │  │                                               │    │
│ └──────────┘ │  │  包含的表 (可展开预览列结构):                    │    │
│              │  │  users (12列) · orders (8列) · products (15列)  │    │
│              │  └───────────────────────────────────────────────┘    │
│              │                                                       │
│              │  ┌─ 对比进行中态 ────────────────────────────────┐    │
│              │  │  ProgressView（复用现有组件）                    │    │
│              │  └───────────────────────────────────────────────┘    │
│              │                                                       │
│              │  ┌─ 对比完成态 ────────────────────────────────┐     │
│              │  │  CompareReport（复用现有组件）                  │    │
│              │  │  source: "上线前检查 (快照 08-10)"              │    │
│              │  │  target: "生产库 (当前)"                       │    │
│              │  └───────────────────────────────────────────────┘    │
└──────────────┴───────────────────────────────────────────────────────┘
```

### 6.3 状态管理

`SnapshotView` 使用局部状态（不放入 Zustand store，因为快照数据不与全局共享）：

```typescript
// 页面状态机
type SnapshotPhase = "idle" | "detail" | "comparing" | "done"

// 核心状态
const [snapshots, setSnapshots] = useState<SnapshotInfo[]>([])
const [selectedId, setSelectedId] = useState<string | null>(null)
const [selectedDetail, setSelectedDetail] = useState<SnapshotDetail | null>(null)
const [phase, setPhase] = useState<SnapshotPhase>("idle")
const [runningTaskID, setRunningTaskID] = useState("")
const [report, setReport] = useState<CompareResult | null>(null)
const [createOpen, setCreateOpen] = useState(false)
const [compareOpen, setCompareOpen] = useState(false)
```

### 6.4 组件清单

| 组件 | 文件 | 行数估算 | 说明 |
|---|---|---|---|
| `SnapshotView` | `pages/SnapshotView.tsx` | ~400 | 主页面：列表+操作区+状态机 |
| `CreateSnapshotDialog` | `components/CreateSnapshotDialog.tsx` | ~200 | 新建快照对话框 |
| `SnapshotCompareDialog` | `components/SnapshotCompareDialog.tsx` | ~180 | 快照对比目标选择对话框 |
| `SnapshotDetail` | `components/SnapshotDetail.tsx` | ~120 | 快照详情卡片（内嵌在主操作区） |

**完全复用的现有组件**（零改动）：
- `CompareReport` — 对比报告展示
- `ProgressView` — 进度展示
- `ConnectionSelect` — 连接选择
- `PageHeader` — 页面标题
- `StepWizard` — 不适用（快照不用向导）

### 6.5 交互流程

```
┌──────────┐    点击[+ 新建快照]     ┌──────────────────────┐
│  空闲态   │ ───────────────────→  │  CreateSnapshotDialog │
│  idle     │                        │  选连接→选库→命名     │
└──────────┘                        └───────┬──────────────┘
     │                                      │ 创建成功
     │ 点击快照列表项                          ↓
     ↓                              ┌──────────────────┐
┌──────────┐                        │  详情态 detail    │
│  空闲态   │ ←──────────────────── │  显示快照信息      │
│  idle     │    关闭详情            │  [对比当前库]     │
└──────────┘                        └────┬─────────────┘
     ↑                                   │ 点击[对比当前库]
     │                                   ↓
     │                          ┌────────────────────────┐
     │                          │ SnapshotCompareDialog  │
     │                          │  选择目标连接+库+选项   │
     │                          └────────┬───────────────┘
     │                                   │ 开始对比
     │                                   ↓
     │                          ┌──────────────────┐
     │                          │  对比中 comparing │
     │                          │  ProgressView    │
     │                          └────────┬─────────┘
     │                                   │ 完成
     │                                   ↓
     │                          ┌──────────────────┐
     └──────────────────────────│  完成态 done      │
          [重新开始] 或 [返回]    │  CompareReport   │
                                └──────────────────┘
```

### 6.6 类型定义 (`types/index.ts` 新增)

```typescript
// 快照摘要
export interface SnapshotInfo {
  id: string
  name: string
  description: string
  connId: string
  connLabel: string
  dbName: string
  dbType: string
  tableCount: number
  totalRows: number
  createdAt: number
}

// 快照详情
export interface SnapshotDetail extends SnapshotInfo {
  tables: SnapshotTableInfo[]
}

export interface SnapshotTableInfo {
  name: string
  rowCount: number
  columns: SnapshotColumnInfo[]
  primaryKey: string[]
  rowSamples?: Record<string, unknown>[]
}

export interface SnapshotColumnInfo {
  name: string
  dataType: string
  nullable: boolean
  primaryKey: boolean
  position: number
}

// 快照对比选项
export interface SnapshotCompareOptions {
  snapshotId: string
  targetConn: string
  target?: DBConn | null
  tables?: string[]
  threshold: number
  ignoreColumns?: string[]
  forceData?: boolean
}
```

### 6.7 API 函数 (`api/index.ts` 新增)

遵循现有模式：`post<T>()` 辅助函数 + 类型化返回值。

```typescript
// 快照管理
export const listSnapshots = () => request<SnapshotInfo[]>("/api/snapshots")
export const getSnapshot = (id: string) => request<SnapshotDetail>(`/api/snapshots/${encodeURIComponent(id)}`)
export const createSnapshot = (opts: { connId: string; dbName: string; name: string; description?: string; includeSamples?: boolean }) =>
  post<{ id: string }>("/api/snapshots", opts)
export const deleteSnapshot = (id: string) =>
  request<{ ok: boolean }>(`/api/snapshots/${encodeURIComponent(id)}`, { method: "DELETE" })

// 快照对比
export const startSnapshotCompare = (opts: SnapshotCompareOptions, taskConfigId?: string) =>
  post<{ taskID: string }>("/api/snapshots/compare", { options: opts, taskConfigId })
export const getSnapshotCompareResult = (taskID: string) =>
  request<CompareResult>(`/api/snapshots/compare/result?taskID=${encodeURIComponent(taskID)}`)
```

---

## 7. 实施计划

### Phase 1: 后端引擎（3 个文件）

| 文件 | 内容 | 说明 |
|---|---|---|
| `engine/snapshot.go` | `CreateSnapshot`, `LoadSnapshot` | 快照创建/加载核心逻辑 |
| `engine/snapshot_compare.go` | `RunSnapshotCompare` | 快照对比逻辑 |
| `engine/model.go` | 新增 Snapshot 相关类型 | 数据模型定义 |

### Phase 2: 后端服务 + API（4 个文件改动）

| 文件 | 内容 |
|---|---|
| `service/snapshot.go`（新增） | 快照 CRUD + 异步任务编排 |
| `service/persist.go` | 新增 `snapshotDir` + 快照文件读写 |
| `web/handler.go` | 新增 6 个 API handler |
| `web/server.go` | 注册 `/api/snapshots` 路由组 |

### Phase 3: CLI（1 个文件）

| 文件 | 内容 |
|---|---|
| `cli/snapshot.go`（新增） | `snapshot create/list/show/delete/compare` 子命令 |

### Phase 4: 前端（5 个文件）

| 文件 | 内容 |
|---|---|
| `types/index.ts` | 新增快照类型定义 |
| `api/index.ts` | 新增 6 个 API 函数 |
| `pages/SnapshotView.tsx`（新增） | 主页面 |
| `components/CreateSnapshotDialog.tsx`（新增） | 新建快照对话框 |
| `components/SnapshotCompareDialog.tsx`（新增） | 快照对比对话框 |
| `App.tsx` | 新增路由和导航项 |

---

## 8. 代码规范检查清单

### Go 规范

- [x] 模型定义在 `engine` 包，`service` 包通过类型别名暴露
- [x] 引擎函数签名：`func Xxx(ctx context.Context, opts XxxOptions, cb ProgressFunc) (*XxxResult, error)`
- [x] Service 函数：同步路径用 `Run*`，异步路径用 `Start*`
- [x] 错误处理：service 层用 `cygin.NewError`/`cygin.WrapError`，engine 层用 `fmt.Errorf`
- [x] 持久化：使用 `PersistMgr` 统一管理，`0600` 权限落盘
- [x] 异步任务：通过 `TaskRunner.Start()` 注册，支持取消和 SSE 进度推送
- [x] 执行历史：落盘 `ExecutionRecord`，summary 格式与现有风格一致

### TypeScript 规范

- [x] 接口命名：PascalCase，字段 camelCase，与后端 JSON tag 对应
- [x] API 函数：使用 `request<T>()` / `post<T>()` 封装
- [x] 组件：函数组件 + hooks，无 class 组件
- [x] 样式：Tailwind CSS + shadcn/ui 组件
- [x] 状态管理：页面级状态用 `useState`，全局共享用 Zustand
- [x] 路由：HashRouter + React Router v6

### 安全规范

- [x] 快照不含密码，存储不加密
- [x] 快照 ID 使用 xid 生成（不可预测）
- [x] 创建快照前验证连接可用性
- [x] 文件路径使用 `filepath.Join` 防路径遍历
- [x] 超时保护：context 5 分钟超时

---

## 9. 附录：快照对比 vs 实时对比的数据对比能力对比

| 对比维度 | 实时对比 | 快照对比 |
|---|---|---|
| 结构差异（列名/类型/可空/主键） | ✅ 完整支持 | ✅ 完整支持（快照结构 vs 在线结构） |
| 数据逐行差异（≤1000行） | ✅ PK模式/整行模式 | ⚠️ 仅结构+行数对比 |
| 数据行数差异（>1000行） | ✅ COUNT 对比 | ✅ COUNT 对比 |
| 主键级有无判断 | ✅ 支持 | ❌ 快照不存全量数据 |
| 变化行检测（PK匹配但内容不同） | ✅ 支持 | ❌ 快照不存全量数据 |
| 差异采样展示 | ✅ 支持 | ⚠️ 仅行数差异摘要 |

**设计原则**：快照对比定位为"结构级+行数级"对比，满足大多数日常巡检场景。如需逐行数据差异分析，引导用户使用实时对比。
