# 库化能力设计文档（Library API）

> **文档状态**: 定稿 v4（三轮架构/产品评审收敛完毕，无遗留 B 级问题；可按第 7 章启动阶段一）
> **最后更新**: 2026-08-28
> **设计原则**: 最大程度复用现有 engine/service 分层，通过公开门面包对外暴露能力；CLI/Web 与库共享同一套业务层，不复制实现。
> **目标使用方**: 内部环境管理产品、安装工具产品（详见 1.1），优先级由其真实场景倒排；触发式能力不进默认路线。

---

## 1. 概述

### 1.1 目标与目标使用者

让其它 Go 程序可以直接 `import` dqex 并调用其核心能力（迁移 / 导出 / 导入 / 对比 / 查询 / 元数据 / 快照），而不必启动 dqex 进程或通过 HTTP 调用。

**库化的目标使用方已锚定为两个内部产品**（design partner 模式，优先级由其真实场景倒排）：

| 使用方 | 集成形态 | 核心诉求 | 关键设计映射 |
|---|---|---|---|
| 环境管理产品 | Go 库 + 可选 iframe 嵌入查询/对比页（形态 B 同进程，见 6.5.1） | 环境连接统一由其持有维护（配置中心式）、环境间对比/快照、元数据浏览；多副本部署形态待确认 | ConnProvider（连接外部化，3.5）、Compare/Snapshot 能力、`dqexweb.Serve` 内嵌（5.1）、4.4 基础设施注入（触发式）、6.5 L3 嵌入（触发式） |
| 安装工具产品 | Go 库（一次性进程内调用，无 UI） | 安装/升级流程中执行 schema 迁移、数据导入；进度上报到安装器 UI；退出码/错误码可判定 | StoreNone 零依赖库模式（4.1）、WithInlineConns、Run* 同步 API + ProgressFunc、错误码契约（3.3） |

两个使用方的共同点：**连接与持久化都应由宿主持有，dqex 作为无状态能力库嵌入**——这正是 StoreNone 库模式 + ConnProvider 列为核心设计的原因。UI 嵌入（第 6 章）与基础设施注入（4.4）对两个使用方均为**可选项**，按其确认的部署形态触发，不进默认路线。

### 1.2 现状分析

分层已经清晰，是库化的良好基础：

| 层 | 位置 | 现状 | 库化适配度 |
|---|---|---|---|
| 引擎层 | `internal/engine` | 纯函数式核心：`RunMigrate` / `RunExport` / `RunImport` / `RunCompare` / `RunDictionary` / `RunSQLQuery` 等，签名统一为 `ctx + opts + ProgressFunc` | 高，天然适合做库 |
| 服务层 | `internal/service` | 编排层：连接管理、任务、历史、快照、AI，`Service` 结构 | 高，但耦合配置发现 / SQLite 存储 / 全局缓存 |
| 表现层 | `internal/cli`、`internal/web` | cobra CLI + gin Web | 无需暴露 |

**三个硬阻碍：**

1. 所有能力都在 `internal/` 下，外部模块**无法 import**（Go 编译器强制）。
2. `go.mod` 的 module 名是裸的 `dqex`，其它程序 `go get` 不到，也不是合法的远端模块路径。
3. `Service` 强依赖数据目录 + SQLite 持久化 + 全局状态（`metaCache`、`CloseAllCliPool`、`SetProvidersDataDir`），库使用者可能只想要"无状态跑一次迁移"。

### 1.3 非目标

- 不做 gRPC / FFI / 多语言 SDK：非 Go 程序走现有 `/dqex/api` HTTP 能力面（补文档即可）。
- 不改变 CLI / Web 的现有行为和用户体验。
- v1 之前不承诺公开 API 永久兼容（semver：v0.x 允许破坏性变更，v1 起承诺）。

---

## 2. 总体方案

### 2.1 公开门面（Facade）方案

新建顶层公开包（同模块内可合法引用 `internal/`），外部**只 import 门面包**：

```
github.com/fj1981/dqex/     ← 公开门面（新目录，仓库根）
├── client.go                Client 定义 + New(opts...) / Close()
├── options.go               WithDataDir / WithConfigFile / WithLang / WithInlineConns / WithConnProvider / WithConnHooks ...
├── connect.go               连接管理：Add / List / Delete / Test / Ping
├── meta.go                  元数据：Databases / Schemas / Objects / Columns / DDL
├── query.go                 SQL 执行：RunScript / TablePage / CellOps / GenSQL
├── tasks.go                 RunExport / RunImport / RunMigrate / RunCompare / RunDictionary
├── snapshot.go              快照：Create / Load / Compare
├── errors.go                SvcError / 错误码 re-export
└── types.go                 类型别名 re-export（Options / Result / ProgressFunc）
```

**为什么选 Facade 而不是直接把 engine/service 移出 internal：**

| | Facade（推荐） | 直接搬移 pkg/engine + pkg/service |
|---|---|---|
| 改动量 | 小（新增包装包 + module 改名） | 大（全仓 import 路径重写 ×2） |
| API 收敛 | 只暴露刻意挑选的能力面 | 全部类型/函数被动公开，收缩需二次破坏 |
| 稳定性承诺 | 门面即契约，内部随意重构 | 内部实现细节全部变成兼容负担 |
| 缺点 | 需要维护类型别名 | 早早期就背上长期兼容包袱 |

结论：先 Facade，等 API 面稳定后（v1 前）再评估是否下沉为 `pkg/`。

### 2.2 前置条件：module 路径修正（阶段一，破坏性）

- `module dqex` → **`module github.com/fj1981/dqex`（已定，评审结论）**：与 infrakit（`github.com/fj1981/infrakit`）同 owner、同路径方案，一致且默认可解析；repo 可保持私有，宿主经 `GOPRIVATE=github.com/fj1981/*` + 凭证或公司 GOPROXY 拉取，无需 replace 特判。阶段一全仓 import 重写一次到位，避免未来二次迁移。
- 全仓 import 路径一次性替换（`dqex/internal/...` → `github.com/fj1981/dqex/internal/...`），涉及 `cmd/`、`internal/`、`web/embed.go`、`web/src` 无关。
- 同步更新 `Makefile`、`.github/workflows/ci.yml`、安装脚本中的构建变量。
- 打 `v0.1.0` tag，此后每阶段独立发版，库使用者可锁版本。**tag 语义说明（已定，评审结论）**：延续现有 tag 方式，**库版本与二进制版本一致**（同一 commit 同一 tag，二进制经 ldflags 注入同一版本号）。理由：库的 `Run*` 与二进制的 `/dqex/api` 来自同一 engine 实现，本就不是两个独立演进的工件；单版本号让两个使用方的支持问答只有一个答案，避免"库 v0.2 + 二进制 v1.5"的排查矩阵。配套两条升版纪律：① **以库 API 兼容性为最严约束**——门面 API 破坏性变更时即使应用侧无感也必须升 minor（v0 期）/ major（v1 后），防止应用小版本夹带库破坏；② **changelog 强制三节分列**（库 API / CLI / Web），使用方按需阅读。

---

## 3. 门面 API 设计（阶段二）

### 3.1 Client 生命周期

```go
package dqex

// New 创建库客户端。零依赖即可用：不传任何 Option 时为"库模式"，
// 不发现全局配置、不建 SQLite 存储（见 4.1 StoreMode）。
func New(opts ...Option) (*Client, error)

// Close 释放连接池、缓存等资源。
func (c *Client) Close() error
```

Option 列表（`options.go`）：

```go
func WithDataDir(dir string) Option          // 持久化根目录（快照/历史/连接库），空 = 纯内存
func WithConfigFile(path string) Option      // 显式 config.yaml，空 = 不加载全局配置
func WithLang(lang string) Option            // 错误消息语言（zh/en），默认 zh
func WithInlineConns(conns ...ConnInfo) Option // 便捷方式：静态注入少量连接（内部仍是只读注册表）
func WithConnProvider(fn ConnProvider) Option  // 连接提供者回调（见 3.5）：连接列表完全由外部持有
func WithConnHooks(hooks ConnHooks) Option     // 连接生命周期回调（见 3.5），审计/监控
// —— 以下为触发式能力（见 4.4）：v0.2 门面即接受参数并校验，具体实现随 4.4 触发落地 ——
func WithStoreConn(conn def.DBConnection) Option // 内部存储（cydb 多数据库：sqlite/mysql/postgresql/oracle，见 4.4.1）；另有 WithStoreDB 实例注入
func WithCacheRedis(addr, pwd string) Option   // 元数据缓存走 Redis（见 4.4.2）；另有 WithCacheClient 实例注入
func WithArtifactStore(s cystore.Store, bucket string) Option // 产物存储（复用 infrakit cystore：本地/MinIO/S3/OBS/OSS，见 4.4.3）；另有 WithMinio / WithS3 参数便捷糖（触发式）
func WithLogger(...) Option                  // 注入调用方日志（可选，后期）
```

### 3.2 能力分组

门面方法与 `service.Service` 现有方法一一对应（薄包装，不新增逻辑）：

**连接管理（connect.go）**

```go
func (c *Client) AddConnection(ctx context.Context, rec ConnRecord) (ConnRecord, error)
func (c *Client) ListConnections() []ConnInfo
func (c *Client) DeleteConnection(key string) error
func (c *Client) TestConnection(ctx context.Context, conn DBConnInfo) error
func (c *Client) PingConnection(ctx context.Context, connKey string) (int64, error)
```

> 连接管理方法按模式分流：`StoreSQLite` 下 `AddConnection` / `DeleteConnection` 落盘持久化；`StoreNone` 库模式下**允许调用且仅内存生效**（内存注册表增删，适合安装工具动态注册连接）；`TestConnection` / `PingConnection` 在库模式下接受外部 key 或完整连接信息。与 4.1 的语义保持一致。

**元数据（meta.go）**

```go
func (c *Client) Databases(ctx context.Context, connKey string, force bool) ([]string, error)
func (c *Client) Schemas(ctx context.Context, connKey, db string, force bool) ([]SchemaSummary, error)
func (c *Client) Objects(ctx context.Context, connKey, db, schema string, force bool) (*DBSchema, error)
func (c *Client) TableColumns(ctx context.Context, connKey, db, table string) ([]TableColumnInfo, error)
func (c *Client) ObjectDDL(ctx context.Context, connKey, db, objType, name string) (*ObjectDDLResult, error)
```

**查询执行（query.go）**

```go
// ScriptParams 参数对象化：门面方法避免多裸参数（公开契约可读性/可演进性）
type ScriptParams struct {
    Limit  int
    Offset int
    Mode   string // 执行模式
}

func (c *Client) RunSQLScript(ctx context.Context, connKey, db, sql string, p ScriptParams) ([]*SQLQueryResult, error)
func (c *Client) QueryTablePage(ctx context.Context, connKey, db, table string, p PageParams) (*TablePageResult, error)
func (c *Client) GenerateSQL(ctx context.Context, connKey, db string, p GenSQLParams) (string, error)
// 单元格更新 / 插入 / 删除行 / 取值 同理公开
```

**任务型能力（tasks.go）**

```go
// ArtifactRef 产物引用（阶段二即引入，避免未来签名破坏）：
// 本地模式 Storage="local"、Ref=文件路径；对象存储模式 Ref=object key（存储实现见 4.4.3，触发式）
type ArtifactRef struct {
    Storage string // local / minio / s3 / obs / oss
    Ref     string
    Size    int64  // 字节；目录产物为 0
}

// 同步 API：ctx 控制取消，ProgressFunc 回调进度
func (c *Client) RunExport(ctx context.Context, opts ExportOptions, cb ProgressFunc) (*ArtifactRef, error)
func (c *Client) RunImport(ctx context.Context, opts ImportOptions, cb ProgressFunc) error
func (c *Client) RunMigrate(ctx context.Context, opts MigrateOptions, cb ProgressFunc) error
func (c *Client) RunDictionary(ctx context.Context, opts DictionaryOptions, cb ProgressFunc) (*ArtifactRef, error)
func (c *Client) RunCompare(ctx context.Context, opts CompareOptions, cb ProgressFunc) (*CompareResult, error)
```

产物落位规则：调用方在 `ExportOptions.OutputDir` / `DictionaryOptions.OutputDir` 显式指定则用之；为空时服务层回填持久化目录（export 子目录）。**库模式下若 `StoreNone` 且未设 `WithDataDir`，无回填来源，调用方必须显式指定 OutputDir，否则报错（公开错误码 `ErrExpOutDir`）。**

**快照（snapshot.go）**

```go
func (c *Client) CreateSnapshot(ctx context.Context, connKey string, dbs []string, opts SnapshotParams, cb ProgressFunc) (*Snapshot, error)
func (c *Client) LoadSnapshot(path string) (*Snapshot, error)   // 离线读文件，不需要连接
func (c *Client) CompareSnapshot(ctx context.Context, snap *Snapshot, target *DBConnInfo, opts SnapshotCompareOptions, cb ProgressFunc) (*CompareResult, error) // target 用完整连接信息而非 connKey：对比目标常为临时环境，未必已注册
```

### 3.3 API 设计原则

1. **同步优先**：库场景只暴露 `Run*` 系列；不暴露 `Start* + taskID` 的异步任务模型（那是 Web UI 的需求）。需要后台并发由调用方自己起 goroutine，取消统一走 `ctx`。
2. **类型别名 + 字段契约声明**：`types.go` 用别名 `type MigrateOptions = service.MigrateOptions` 对外公开。**必须明确承认的代价**：别名使 internal/service 的 Options/Result 结构体**字段**成为公开契约（v1 起冻结：新增字段只能加、不能改既有字段语义），2.1 所述"内部随意重构"仅覆盖函数签名与私有实现、不含这些结构体；该约束写入 CONTRIBUTING。此代价经评估可接受——独立公开类型 + 转换层是双份维护，v0 不做。`ProgressFunc`、`CompareResult`、`TablePageResult` 等一并公开。
3. **错误体系是契约**：公开 `SvcError`、`AsSvcErr`、错误码常量与 `WithLang`；库使用者按错误码分支处理，而不是字符串匹配。**命名规范**：公开错误码统一 `Err` 前缀导出常量（如 `ErrExpOutDir`、`ErrClientClosed`、`ErrConnNotFound`），internal 层小写命名（如 errExpOutDir）在门面层统一映射，错误码表进 `docs/library.md`。
4. **i18n 内聚**：`WithLang` 决定所有 `SvcError` 消息的语言；引擎层的 `MsgError` 同规则。

### 3.4 使用示例（目标形态：安装工具场景，进程内完成一次迁移）

```go
package main

import (
    "context"
    "fmt"

    dqex "github.com/fj1981/dqex"
)

func main() {
    client, err := dqex.New(
        dqex.WithLang("zh"),
        dqex.WithInlineConns(
            dqex.DBConnInfo{Type: "mysql", Host: "10.0.0.1", Port: 3306, Un: "root", Pwd: "***"},
            dqex.DBConnInfo{Type: "mysql", Host: "10.0.0.2", Port: 3306, Un: "root", Pwd: "***"},
        ),
    )
    if err != nil {
        panic(err)
    }
    defer client.Close()

    conns := client.ListConnections()
    err = client.RunMigrate(context.Background(), dqex.MigrateOptions{
        Source: conns[0], Target: conns[1],
        // ...
    }, func(p dqex.ProgressInfo) {
        fmt.Printf("[%s] %s\n", p.Stage, p.Message)
    })
    if err != nil {
        var se *dqex.SvcError
        if dqex.AsSvcErr(err, &se) {
            fmt.Println("code:", se.Code)
        }
        panic(err)
    }
}
```

### 3.5 连接外部化（ConnProvider + ConnHooks）

**定位：连接列表完全由外部持有，库内部不维护连接。** 库模式（StoreNone）下 `AddConnection` 不落盘（仅内存生效，见 3.2/4.1）；需要连接完全外部持有的宿主只用 ConnProvider、不调 Add 即可。宿主程序通过注入回调把外部定义好的数据库连接列表直接交给 dqex 使用——连接可以来自宿主的配置中心、密钥管理系统（Vault/KMS）、自身数据库等任何地方，dqex 不落盘、不缓存密码。

现有连接解析全部经由 `service.resolveConn` 单一漏斗，回调方案挂在这个插入点上，侵入性最小。

**ConnProvider（连接提供者回调）** —— 库模式的核心扩展点，两个回调构成完整的外部连接源：

```go
// ConnProvider 由宿主实现，提供外部维护的连接列表。
// 两个回调都可能被并发调用，实现方必须线程安全。
type ConnProvider struct {
    // ListConns 返回外部连接列表（供 ListConnections / 元数据遍历使用）。
    // 返回的密码字段宿主自行决定是否填充（List 场景可脱敏）。
    ListConns func(ctx context.Context) ([]ConnInfo, error)
    // GetConn 按 connKey 取一个连接用于实际建连，密码必须是可用的明文。
    // 返回 (nil, nil) 表示不认识该 key（dqex 返回 ErrConnNotFound）。
    GetConn func(ctx context.Context, connKey string) (*DBConnInfo, error)
}

func WithConnProvider(p ConnProvider) Option
```

解析优先级（resolveConn 链路扩展，先显式后动态）：

```
inline conn（任务参数直接带连接）
  → 内存注册表（WithInlineConns 静态列表 + StoreNone 下 AddConnection 的内存连接）
  → ConnProvider.GetConn（动态外部源）
  → 持久化存储（仅 StoreSQLite 应用模式）
```

配套语义：

- 门面 `ListConnections()` 在库模式下直接透传 `ConnProvider.ListConns` 的结果（不合并内部存储，因为库模式没有内部存储；若宿主同时用内存 AddConnection 注册了连接，两者合并返回）。
- `AddConnection` / `DeleteConnection` 在库模式下**允许且仅内存生效**（与 3.2/4.1 一致）；需要连接完全外部持有时宿主不调用它们即可。
- 密码轮换天然支持：每次真实建连都走 `GetConn` 取最新凭证。
- `WithInlineConns(conns...)` 是 `ConnProvider` 的便捷糖：静态列表内部转成只读注册表实现，少量固定连接时省去写回调。

**ConnHooks（生命周期回调）** —— 审计 / 监控用，宿主可选注册，失败只告警不影响主流程：

```go
type ConnHooks struct {
    OnResolved func(ctx context.Context, connKey string, source ConnSource) // 解析成功后（source: inline/provider/store）
    OnConnect  func(ctx context.Context, conn *DBConnInfo, err error)      // 每次真实建连后（含耗时），err=失败原因
    OnAdded    func(rec ConnRecord)                                        // AddConnection 成功后（任意模式，内存/落盘均触发）
    OnDeleted  func(key string)                                            // DeleteConnection 成功后（任意模式）
}

func WithConnHooks(hooks ConnHooks) Option
```

回调契约（写进公开文档的硬性约定）：

1. 回调运行在 dqex 的调用 goroutine 上，`OnConnect` 在关键路径上，**必须快速返回**（重活自己起 goroutine）。
2. 回调内不得再回调 dqex 的 Client 方法（避免自锁/重入）。
3. `GetConn` / `ListConns` 返回的连接结构由 dqex 持有副本，回调方不得复用修改。
4. 宿主自行谨慎处理凭证：`ListConns` 建议脱敏返回，`OnResolved`/`OnConnect` 参数中含 Pwd 字段，日志场景注意遮蔽。

### 3.6 并发模型与回调契约

**并发承诺**（实现后以并发冒烟测试固化，库使用者的必读契约）：

1. `Client` 可被多个 goroutine 并发使用：并发调用任意能力方法（含同一方法）安全。
2. 连接池为进程级共享（见 4.2）；`Close()` 幂等，之后调用能力方法返回明确错误码 `ErrClientClosed`。
3. 同进程多 `Client` 实例语义见 4.3（v0：可用但共享缓存/连接池）。

**ProgressFunc 契约**（与 3.5 连接回调契约对称）：

1. 回调在 dqex 执行 goroutine 上同步调用，**回调内阻塞会暂停任务**（进度上报天然背压）；重活自行转 goroutine。
2. 回调返回不是取消机制——取消统一由 `ctx` 控制（回调内可读 ctx 判断取消状态）。
3. 回调不保证串行、不保证每阶段恰好触发一次（重试/并发子任务可能交错），消费方按幂等展示设计。

**QueryHooks（SQL 审计钩子，可选、后期）**：`OnQuery(ctx, connKey, stmtDigest, costMs, rowsAffected)`。数据管理类宿主的合规审计（SQL 审计）诉求常见，成本极低；v0 不实现，但接口位预留，避免 v1 加钩子时破坏 `ConnHooks` 结构。

---

## 4. 服务层解耦（阶段三）

### 4.1 StoreMode：库模式 vs 应用模式

`NewServiceWith` 当前强依赖数据目录 + SQLite（`PersistMgr`）。持久化开关统一为三态（合并 4.4.1 的 cydb 注入路径，消除两套模型）：

```go
type StoreMode int
const (
    StoreNone     StoreMode = iota // 纯内存：不建存储、不写历史/任务/连接库（默认，库模式——安装工具/环境管理嵌入即此形态）
    StoreSQLite                    // 现有行为：数据目录 + SQLite（CLI/Web 用）；等价于 StoreExternal + sqlite 连接的便捷糖
    StoreExternal                  // 外部注入：WithStoreConn(cydb 多数据库) / WithStoreDB(实例)，见 4.4.1（触发式）
)
```

- `StoreNone` 时：连接信息仅内存保存（`WithInlineConns` 注入或运行期 `AddConnection` 不落盘）；快照仍可用（落盘目录由 `WithDataDir` 提供，未提供则仅内存）；SQL 历史 / 任务配置 / 收藏不可用（方法返回明确错误码）。
- CLI / Web 保持 `StoreSQLite`，行为不变。

### 4.2 全局状态收敛

| 全局点 | 位置 | 处理 |
|---|---|---|
| `metaCache`（cydist 全局缓存） | service/service.go | 短期：文档化说明（同进程共享、TTL 10 分钟）；中期：改为 Client 级字段注入 |
| `CloseAllCliPool`（进程级连接池） | engine/conn.go | 保持进程级（连接池本来就该复用），门面 `Close()` 中调用并在文档注明进程级语义 |
| `SetProvidersDataDir` / `AIProviders` | service/ai_providers.go | AI 依赖数据目录，`StoreNone` 且未配置 AI 时相关方法返回明确错误码 |
| `WithLang` ctx | service/errors_msg.go | 公开别名，库使用者全程透传 |

### 4.3 多实例语义

- v0 阶段明确承诺：同进程多 `Client` 实例**可用但共享**元数据缓存与连接池（安全，无冲突）。
- v1 前如需完全隔离，再把缓存/池下沉为 Client 字段（工作量集中在 engine/conn.go 与 service/service.go）。
- 多实例**跨进程**部署（多副本 Pod）需配合 4.4：内部存储进宿主数据库 + 缓存 Redis 后端，状态与缓存才具备一致性基础。

### 4.4 基础设施后端可插拔（cydb / Redis / cystore）

**动机**：集成到宿主环境时，dqex 内部使用的基础设施应能与宿主对齐——内部存储进宿主数据库（多实例共享状态）、元数据缓存走 Redis（多实例一致性）、产物（导出 zip / 数据字典）直接落 MinIO/S3（不依赖本地磁盘）。统一设计原则：**默认实现 = 现状（SQLite / 本地缓存 / 本地目录），零配置可用；后端优先复用 infrakit 既有设施（cydb / cydist / cystore），以接口注入，宿主可传参数也可传现成 client 实例。**

> **按需触发原则（评审结论）**：4.4 是"多副本/容器化部署"场景的配套能力，对已锚定的两个使用方均为**可选项**——安装工具（一次性进程内调用）明确不需要；环境管理产品仅当确认多副本部署形态后才启动 4.4.1（cydb 重写）与 4.4.2（Redis）。4.4 不进默认路线图，避免无需求牵引的"中"风险改造。

| 基础设施 | 现状 | 插入点 | 改动量级 |
|---|---|---|---|
| 内部存储 | SQLite（store 层已抽象 `Store` 接口，`OpenSQLite() (Store, error)`） | **基于 cydb 统一重写**：`AutoMigrate` + ss 构建器，一次获得 SQLite/MySQL/PostgreSQL/Oracle 支持 | 中 |
| 元数据缓存 | cydist `NewCacheWrapper` 纯本地 FreeCache；**原生支持 `WithRedisClient`，只是没传** | 透传 RedisClient 即可（go-redis 已在依赖树） | 小 |
| 产物存储 | 本地路径 `OutputPath`，Web 下载 handler 读本地文件 | **复用 infrakit `cystore.Store`**（已支持 MinIO/S3/OBS/OSS/本地）+ Web 下载改造为流式读取 | 中 |

#### 4.4.1 内部存储：基于 cydb 统一实现（多数据库）

**不自写方言分叉**——infrakit 的 `pkg/cydb` 已支持多数据库（`def.DBConnection{Type: mysql / postgresql / sqlite / oracle ...}`），且提供屏蔽差异的高级构建接口：`cli.AutoMigrate(&Model{})` 结构体迁移、`cydb/ss` 全套查询构建器（Select/Insert/Update/Delete/CreateTable/Union/CTE + `cydb.EQ` 等条件构建），方言差异由 cydb 处理。

```go
func WithStoreConn(conn def.DBConnection) Option  // 参数注入：mysql / postgresql / sqlite / oracle
func WithStoreDB(cli *cydb.DBCli) Option          // 实例注入：宿主复用已有 cydb client/连接池
```

- **实施路线**：将 store 层的裸 SQL（store/*.go 共 6 个文件的 CRUD/建表）迁到 cydb——建表走 `AutoMigrate`（结构体即模型），CRUD 走 ss 构建器。一次迁移，SQLite/MySQL/PostgreSQL/Oracle 全部获得支持，且后续新增存储引擎零方言成本。
- **默认行为**：未注入时 `WithDataDir(dataDir)` 即 StoreSQLite 便捷糖（内部等价于 `DBConnection{Type: "sqlite"}`，见 3.1/4.1），CLI/Web 现状行为不变。
- **迁移风险**：现有 store/migrate.go 的手写建表语句与 `AutoMigrate` 生成的 schema 需做等价性回归（类型映射、索引、默认值）；**另需核实 cydb 的 SQLite 驱动是否纯 Go**（现 store 用纯 Go 驱动保证交叉编译，若 cydb 走 cgo 驱动会破坏现有 CI 交叉编译能力——迁移前必须确认）。v0 不做 SQLite→其它库的数据迁移工具（库使用者是新集成，无存量）。
- 场景：宿主多副本部署共享连接库/历史/任务/快照索引，或运维上要求内部数据进已有数据库实例（不再限定 MySQL）。

#### 4.4.2 元数据缓存：Redis 后端

```go
func WithCacheRedis(addr, password string) Option   // 参数注入（dqex 内建 client）
func WithCacheClient(rc *cydist.RedisClient) Option // 实例注入（宿主复用已有 Redis）
```

- 注入后 `metaCache` 由"本地 FreeCache"升级为 jetcache-go 本地+远程两级（cydist 原生行为），多实例间元数据缓存一致、失效广播生效（连接变更 invalidate 全量键，见 service.go invalidateMetaCache）。
- 与 4.2 的"metaCache 收敛为 Client 级字段"合并实施，一次改完。
- 场景：多实例部署时避免各实例元数据缓存漂移；单机不用注入，行为不变。

#### 4.4.3 产物存储：复用 infrakit cystore（MinIO / S3 / OBS / OSS / 本地）

**不自造存储接口**——infrakit 的 `pkg/cystore` 已提供统一云存储抽象：`cystore.NewStore(*Config)` 工厂，Provider 覆盖 MinIO / S3 / 华为 OBS / 阿里 OSS / 本地文件 / NFS / SFTP，能力齐备（PutObject / GetObject / PresignedGetObject / GetObjectInfo / 桶管理 / 批量删除 / 复制重命名）。dqex 核心只依赖 `cystore.Store` 接口，各 provider 的 client SDK（minio-go 等）按需引入，核心无新增负担（infrakit 本就是直接依赖）。

```go
// 实例注入（主推）：宿主构造 cystore.Store（可用任意 provider 或自实现），dqex 内部使用
func WithArtifactStore(store cystore.Store, bucket string) Option

// 参数便捷糖：dqex 内部按 Config 构造（MinIO/S3 等常见后端）
func WithMinio(endpoint, accessKey, secretKey, bucket string) Option
func WithS3(region, bucket string) Option
```

- **产物引用**：`cystore` 语义即 `bucket + objectName`，产物引用 `{Bucket, Object, Size}`；本地 provider（ProviderLocal）下 object = 相对路径，天然兼容现状。
- **门面签名已前置**（阶段二，见 3.2）：`RunExport` / `RunDictionary` 返回 `*ArtifactRef`，本地模式 Ref=路径；**触发时仅引入存储实现，零签名破坏**。`OutputDir` 仅在本地 provider 下生效。
- **Web 下载**：`/dqex/api/export/download/:taskID` 改为经 `store.GetObject` 流式输出；对象存储 provider 下可选 302 到 `PresignedGetObject` 直链（大文件不走 dqex 进程带宽）。
- **默认实现**：未注入时用 `cystore.Config{Provider: ProviderLocal, Root: dataDir}` 包一层，行为与现状完全一致（产物落数据目录）。
- 场景：容器化/多实例部署无共享本地盘；产物集中管理生命周期（宿主的桶策略/生命周期规则接管清理）。

---

## 5. 配套与分发（阶段四）

- `examples/` 目录：`migrate/`、`export/`、`compare/`、`query/`、`snapshot/` 各一个最小可运行示例。
- `docs/library.md`：库使用文档（安装、Quick Start、能力清单、错误码表、**错误码→进程退出码建议映射表**（安装工具场景，如 ErrCancelled→130）、与 CLI/Web 的能力差异矩阵）。
- CI（`.github/workflows/ci.yml`）新增：examples 编译 + `go vet` 公开 API 检查；tag 时自动发布。
- 版本策略：v0.x 快速迭代允许破坏；进入 v1 后遵循 Go 1 兼容承诺，新增能力只加不减（弃用先标记后移除）。

### 5.1 其它形态接入路径（非 Go 程序）

| 形态 | 方式 | 成本 |
|---|---|---|
| Go 程序 | 本文档门面 API，`go get` 直接用 | 本文档范围 |
| Go 程序要完整 UI | 公开 `dqexweb.Serve(svc, opts)` 包装现有 `web.RunWeb`，把 dqex Web 整体内嵌进调用方进程 | 小（一层公开包装） |
| 非 Go 程序 | 现有 `/dqex/api` HTTP + token 认证已是完整能力面，补 API 文档即可 | 只补文档 |

---

## 6. 前端组件化能力（Frontend Integration）

### 6.1 目标与集成层级

除后端门面 API 外，同时把前端功能交互组件化，让外部 Web 应用直接集成 dqex 的功能交互（选表器、SQL 编辑器、结果表格、对比报告、迁移向导……）。按改造成本与集成深度分三级：

| 层级 | 形态 | 适用宿主 | 改造成本 |
|---|---|---|---|
| L1 组件级 | npm 包 `@dqex/ui`，`<DqexTablePicker />` 等按需引入 | React 应用 | 中（组件解耦） |
| L2 功能块级 | 预接线的能力部件：`<DqexQueryBuilder server={...} />`（编辑器+结果表+执行）、`<DqexCompareReport />`、迁移/导出向导步骤块 | React 应用，想少写胶水 | 中（L1 之上组合） |
| L3 页面级 | 整页 iframe 嵌入：`/dqex/#/embed/query` 等精简壳路由 + postMessage（详见 6.5，**优先落地方案**） | 任意框架（Vue/Angular/jQuery） | 低（几乎零改造） |

### 6.2 数据通道：统一走 HTTP API

公开组件**只认 HTTP `/dqex/api`**（见 6.6 前缀约定），不与 Go 库直接绑定（浏览器里也无法 import Go）：

```tsx
<DqexProvider server={{ baseUrl: "https://dqex.internal", token: () => getToken() }}
              lang="zh" theme="dark">
  <DqexTablePicker connKey="prod-mysql" db="orders" value={sel} onChange={setSel} />
</DqexProvider>
```

- 宿主是 Go 程序：用阶段四的 `dqexweb.Serve`（可关 UI 只留 API，或直接同进程同源），组件请求同源 `/dqex/api`，token 从宿主会话注入。
- 宿主非 Go：连独立部署的 dqex 服务，token 走宿主后端代理换取（不把长期凭证下发浏览器）。
- 这样 L1/L2/L3 三级共享同一个后端契约，组件版本与 HTTP API 版本对齐发布。

### 6.3 组件解耦改造（前置工作）

当前组件与"应用"耦合在三点，公开化前需解开（以 TablePicker 为样本审计）：

| 耦合点 | 现状 | 改造 |
|---|---|---|
| API 调用内联 | 组件直接 `import * as api from "@/api"` | 改为从 `DqexProvider` context 取注入的 client；应用内用法不变（Provider 默认实例） |
| 全局状态 | 部分组件直连 zustand store（queryStore / app 等） | 公开组件禁止直连 store：数据经 props 进出（受控/非受控双模式），store 只留在页面级容器 |
| i18n / 主题 | `useTranslation` 全局实例、theme 全局 | i18n 实例与 theme 随 Provider 注入，复用现有 `locales/zh|en` |

配套约束：

- 公开组件只依赖 `@/components/ui`（shadcn）、`@/lib`、`@/types`，不得反向依赖 `pages/`、`stores/`。
- 组件目录按公开与否分组：`web/src/components/` → 逐步迁移公开件到 `web/src/dqex-ui/`（未来拆包源），迁移一个解耦一个。
- Monaco 编辑器（SqlEditor）作为较重组件独立导出，宿主未用到时不引入。

### 6.4 构建与发布

- vite library mode 构建 `@dqex/ui`：`react` / `react-dom` / `react-i18next` 设为 peerDependencies；预构建 CSS 随包发布（Tailwind 产物 + `@dqex/ui` 自身样式），宿主引入一个 css 文件即可，无 Tailwind 接入要求。
- L3 iframe 嵌入：基于 HashRouter 的 `#/embed/<view>` 精简壳 + postMessage 协议，无需新增服务端路由（详见 6.5，L3 为优先落地方案）。
- npm 包版本与 Go 门面版本同步发版（同一 tag），包内 README 标注配套的后端最低版本。

### 6.5 L3 整页 iframe 嵌入（优先落地方案）

**结论先行：可行性极高，绝大部分基础设施已存在。** 现状盘点：

| 基础项 | 现状 | 结论 |
|---|---|---|
| Go 暴露前端文件 | `//go:embed all:dist`（web/embed.go）+ `cygin.WithEmbeddedFiles("/", DistFS, "dist")`（server.go:457），前端文件已作为虚拟文件系统经 HTTP 全量暴露 | 已具备，单二进制自带 UI |
| 磁盘目录模式 | cygin 另有 `WithStaticFiles(urlPath, dirPath)`，可从本地目录服务前端 | 已具备，换前端免重编 Go |
| 防 iframe 头 | dqex 自有 `securityHeaders()`（server.go:157）未设 `X-Frame-Options` / CSP frame-ancestors | 跨域 iframe 不会被浏览器拦，但需补白名单（见安全） |
| 前端路由 | HashRouter（App.tsx:1057），路由 `#/query` `#/compare` 等 | 深链即 `http://host/#/query`，无需 history fallback，反向代理任意子路径天然兼容 |
| 令牌传递 | `tokenAuth` 已支持 `?token=` 查询参数（为 SSE/下载设计）；本机回环免认证 | iframe src 可直接带 token |
| 泄漏防护 | `Referrer-Policy: no-referrer` 已全局设置 | URL 中的 token 不随跳转泄漏 |

#### 6.5.1 三种部署形态（Go 侧）

```
主形态 B：宿主 Go 程序内嵌 dqexweb.Serve（同进程）【内部集成默认形态】
  宿主把 dqex 挂在 /dqex 子树（或旁路端口）；同源，iframe 无跨域问题
  为何是主形态：两个内部使用方（环境管理/安装工具）均为 Go 程序——
    库 API 与 Web UI 同进程共享同一个 Service 实例（库调用与页面操作看到同一份状态）；
    同源一次性消灭 token 分发、CORS、SameSite 三大嵌入难题（6.5.3/6.5.4/6.7.1 大幅简化）；
    连接经 ConnProvider 由宿主持有（3.5），单部署物，版本天然一致（见 2.2）
  改动：公开 dqexweb.Serve 包装层（阶段四已规划）

辅助形态 A：独立进程 + iframe 直连（跨域）—— dqex 需独立部署、宿主无法同进程时
  宿主 Web ──iframe src="http://dqexhost:port/dqex/#/embed/query?token=..."──> dqex 服务
  改动：零（跑通握手与安全白名单即可）；注意跨域 Cookie 的 SameSite 约束（见 6.7.1）

辅助形态 C：宿主反向代理子路径（/dqex/* → dqex 服务）—— 网关聚合/统一域名场景
  前提：整站 /dqex 前缀（页面挂载 + API，见 6.6），宿主整段转发 /dqex/* 即可，零重写、零路径冲突；
  vite base: './' 统一构建配置，资产相对路径在任意挂载点均正确
  改动：/dqex 前缀（一次性）；dqex 侧无需网关重写
```

#### 6.5.2 嵌入壳设计（前端改动）

前端新增"嵌入模式"判定与精简壳，复用现有页面组件：

- **入口约定**：`#/embed/<view>`（如 `#/embed/query`、`#/embed/compare`）。App.tsx 检测 hash 前缀后渲染 EmbedShell：无侧边栏/页头/连接管理入口，仅渲染对应 View。`?embed=1` query 同时作为**服务端可感知**的 embed 标记（hash 不发送到服务端，安全头/日志按此识别，见 6.5.4）。
- **视图参数**：经 URL query 传入初始上下文（如 `?conn=prod-mysql&db=orders`），复杂配置走 postMessage。
- **postMessage 协议**（dqex 约定，写入公开文档）：

```
iframe 加载完成 ──dqex:ready──> 宿主
宿主 ──dqex:init { token?, lang?, theme?, config? }──> iframe   # token 走握手则不落 URL
iframe ──dqex:state { key, value }──> 宿主                      # 状态变化（如选表结果）
iframe ──dqex:action { type, payload }──> 宿主                  # 请求宿主动作（如关闭弹层）
iframe ──dqex:resize { height }──> 宿主                         # 高度自适应（auto-resize iframe）
宿主 ──dqex:command { type, payload }──> iframe                 # 宿主下发指令（如刷新、销毁）
```

  实现要点：单一 `embedBus.ts` 模块封装收发；`event.origin` 校验 + embed 模式下的宿主 origin 白名单；普通模式（非 embed）协议模块不激活，零影响。

#### 6.5.3 Token 分发（嵌入场景核心痛点）

现有 token 24h 有效、重启刷新，对长期嵌入的宿主是挑战。**注意：本节仅服务形态 A/C**——形态 B（同进程同源，主形态）下宿主登录态经 6.7.1 鉴权外置直接生效，token 分发问题整体不存在。三个方案按安全度递进：

| 方案 | 机制 | 适用 |
|---|---|---|
| a. URL 直传 | `?token=` 直接放 iframe src | 内网/低敏感场景，零改动（Referrer-Policy 已挡跳转泄漏，但会进浏览器历史与宿主访问日志） |
| b. postMessage 握手 | iframe ready 后宿主经 `dqex:init` 注入 token，不落 URL | 推荐默认方案，改动仅 embedBus |
| c. 宿主代理注入 | 宿主后端持有 dqex token（`dqex url` / web-access.json 读取），浏览器只持宿主会话，代理层补 X-Auth-Token 转发 | 对外/多租户场景，最安全且 token 轮换对前端透明 |

配套：token 过期后 iframe 内 API 全部 401，embedBus 捕获 401 → `dqex:tokenExpired` 事件 → 宿主重新握手或刷新 src。长期可增加短期机器 token 交换端点（开放问题 #9）。

> 若采用 6.7 的鉴权外置（形态 B 下宿主会话 Cookie 直接生效），方案 a/b 的 token 注入可整体省略——iframe 内请求由宿主登录态自动放行，仅方案 c（代理注入）仍需 dqex token。

#### 6.5.4 安全约束（必须补齐）

1. **frame-ancestors 白名单**：当前无 X-Frame-Options = 任意站点可 iframe dqex（点击劫持/信息暴露面）。补配置项 `web.frameAncestors: []string`（宿主 origin 列表）。**技术约束**：hash fragment 不会发送到服务端，安全头只能按 HTTP 请求粒度生效——因此 frameAncestors 配置后**整站生效**（有配置即对 HTML 响应设置 CSP frame-ancestors，未配置维持现状不发头）；embed 视图由前端按 hash 前缀识别，`?embed=1` query 作为服务端可感知的辅助标记（见 6.5.2）。
2. **embed 视图降权**：embed 模式下不暴露设置页/配置页/连接管理，仅开放数据操作视图（query/compare/export 等），白名单写死在路由映射表。
3. **CORS**：形态 A 跨域直连 `/dqex/api` 时需放开宿主 origin（现有 cors 中间件配置化）；形态 B/C 同源无需。
4. **白名单联动**：现有访问来源白名单（accessControl）与 frameAncestors 独立配置、各自生效。

#### 6.5.5 待补改动清单（L3 完整范围）

| # | 改动 | 位置 | 量级 |
|---|---|---|---|
| 0 | 整站 `/dqex` 前缀（页面双挂载 + API 前缀，见 6.6，宿主集成零冲突的前提） | server.go、web/src/api/*、vite.config.ts | 小 |
| 1 | EmbedShell + `#/embed/<view>` 路由映射（先覆盖 query/compare 两视图） | web/src/App.tsx、新增 EmbedShell.tsx | 小 |
| 2 | embedBus（postMessage 协议 + token 握手 + 401 上报） | web/src/lib/embedBus.ts | 小 |
| 3 | 嵌入模式样式隔离（隐藏壳、紧凑布局、主题/lang 由握手注入） | index.css / theme.ts | 小 |
| 4 | frameAncestors 配置 + 中间件 + 文档 | appconfig.go、server.go | 中 |
| 5 | vite `base: './'`（统一构建配置，资产相对路径，任意挂载点通用） | vite.config.ts | 小 |
| 6 | 宿主侧示例页（iframe + 握手 + 高度自适应 demo） | examples/ | 小 |
| 7 | 鉴权外置：`WithAuthenticator` 回调（Serve 选项）+ authWebhook（见 6.7） | server.go、appconfig.go | 中 |

### 6.6 API 与页面统一 `/dqex` 前缀

**动机**：宿主应用通常已有自己的 `/api` 和根路径页面。整站（API + 页面 + 资产）统一收进 `/dqex` 命名空间后：反代子路径（形态 C）下宿主只需整段转发 `/dqex/*` → dqex，与宿主自身路由**零冲突、零重写**；Go 库集成（形态 B）时宿主把 dqex 挂在 `/dqex` 子树，iframe 指向 `/dqex/#/embed/query`，不占用宿主的任何路径。

**路由分层结论**：

| 层 | 形态 | 是否加前缀 | 说明 |
|---|---|---|---|
| 页面挂载点 | `/dqex/`（index.html + assets） | **加** | 这是与宿主抢路径空间的部分 |
| API | `/dqex/api/...` | **加** | 同上，与页面同命名空间 |
| 前端 hash 路由 | `#/embed/query` 等 | **不加** | fragment 属于 iframe 文档内部，宿主页面（即使也用 hash 路由）与之互不可见，无冲突可能；改动反而破坏现有深链 |
| 健康检查 | cygin 默认 `/health` 保留 | 不强制 | 独立可用；集成场景由宿主代理层决定是否映射入 `/dqex` 子树 |

**Go 侧挂载方案**（cygin `WithEmbeddedFiles` 的 urlPath 本就是参数）：

```go
// 集成约定挂载点：/dqex 子树（页面 + API + 健康检查全在内）
cygin.WithEmbeddedFiles("/dqex", webui.DistFS, "dist")
// 独立使用兼容：双挂载，旧书签 http://host:port/#/query 依旧可用
cygin.WithEmbeddedFiles("/", webui.DistFS, "dist")
```

- **集成模式**（形态 B/C）：宿主只需处理 `/dqex/*` 一个前缀，`dqex url` 输出的集成地址形如 `http://host:port/dqex/#/embed/query?...`。
- **独立模式**：`/` 与 `/dqex` 同时可用（同一份 FS，双挂载成本为零），存量用户无感。
- **前端资产路径**：vite `base: './'`（相对路径），同一份 dist 在 `/` 与 `/dqex/` 下都能正确解析资产——无需按挂载点分别构建。

**API 前缀改动面（已核实，非常集中）：**

| 端 | 位置 | 改动 |
|---|---|---|
| Go 路由前缀 | server.go:338 `cygin.NewEndpointBuilder("/api", ...)` → `"/dqex/api"`；server.go:455 `AddRouteGroup` 同步 | 2 处 |
| Go 页面挂载 | server.go:457 `WithEmbeddedFiles("/", ...)` → 双挂载 `"/dqex"` + `"/"`（见上） | 1 处 |
| Go 中间件判断 | server.go:89（tokenAuth 放行判断）、:145、:162（Cache-Control 等按 `/api/` 前缀处理）`HasPrefix("/api/")` → `"/dqex/api/"` | 3 处 |
| 前端 API client | 所有 `/api/...` 端点字符串**集中**在 `web/src/api/index.ts` + `web/src/api/sql.ts`（组件/stores 均经 `@/api` 函数调用，无散落硬编码）：提取 `const API_BASE = "/dqex/api"`，端点改为 `${API_BASE}/...` | ~60 处机械替换（2 个文件） |
| vite dev 代理 | vite.config.ts 代理规则 `/api` → `/dqex/api` | 1 处 |
| SSE / 下载 URL | 前端 EventSource 与下载链接基于上述 client 构建，随 API_BASE 生效 | 0 处（跟随） |
| 健康检查 | cygin `WithHealthCheck` 默认 `/health` 保留；集成模式由宿主代理时可直接使用，或映射入 `/dqex` 子树 | 0 处 |

**语义保持**：

- 独立使用（形态 A）：`/` 与 `/dqex` 双挂载并存，页面、`dqex url` 输出、存量书签全部无感；API 统一走 `/dqex/api`。
- hash 路由（`#/query`、`#/embed/query`）不变，现有深链与 6.5 的 embed 入口约定不受影响。
- token 传递方式（header / `?token=`）不变；tokenAuth 逻辑只改前缀判断。
- 无存量外部调用方（库化前无公开 API），一次性切换无需兼容层，changelog 注明即可。**回归范围注明**：CLI `dqex url` 输出地址变为 `/dqex/...`；如存在内部脚本/自动化直接调用 `/api`，随 API 前缀同步调整。
- 约束写入公开文档：`/dqex`（页面）+ `/dqex/api`（API）是 dqex 的稳定集成契约，v1 起不变。

### 6.7 API 鉴权外置（宿主回调）

**动机**：集成场景下宿主应用已有自己的认证体系（Session/JWT/SSO），不应要求宿主在 dqex 的启动 token 体系之外再维护一套凭证。提供两级鉴权外置，现有 tokenAuth 保持为**默认兜底**（未注入回调时行为不变，本机回环豁免、外部校验 `?token=` / header）。

#### 6.7.1 进程内回调（形态 B：Go 库集成，推荐）

`dqexweb.Serve` 增加鉴权选项，宿主注入回调替换 tokenAuth 中间件：

```go
// Authenticator 对每个 API 请求做鉴权判定。
// 返回 nil 放行；返回错误时 dqex 统一按 401 渲染（错误消息支持 i18n）。
// 请求的登录态由宿主自行解析（Session cookie / JWT / SSO 票据均可）。
type Authenticator func(c *gin.Context) error

func WithAuthenticator(fn Authenticator) ServeOption
func WithoutAuth() ServeOption                    // 完全关闭鉴权（宿主自己在外层包中间件时用）
```

- 实现上就是把 `tokenAuth` 中间件替换为调用回调；限速器（authLimiter）、回环豁免逻辑保留可选复用。
- 宿主回调失败策略 **fail-closed**：回调 panic / 返回错误一律拒绝。
- iframe 场景：**同源（形态 B/C）时浏览器自动携带宿主会话 Cookie，宿主登录态直接生效**。形态 A 跨域时注意：默认 `SameSite=Lax` 的 Cookie **不会**随 iframe 内的跨站 API 请求发送——需宿主会话 Cookie 改 `SameSite=None; Secure`（扩大 CSRF 面，需宿主自行评估）或退回 6.5.3 的 token 方案 a/b/c，文档按此明示。

#### 6.7.2 进程间 Webhook（形态 A/C：独立进程部署）

跨进程无法传 Go 回调，提供 HTTP 反向回调（introspection）：

```yaml
# config.yaml
web:
  authWebhook: "https://hostapp.internal/auth/dqex-introspect"  # 空 = 使用默认 tokenAuth
  authWebhookSecret: "..."    # 请求签名共享密钥（HMAC-SHA256 请求头），防 webhook 端点被伪造调用
```

```
dqex 收到 /dqex/api 请求
  → 提取凭证（原样透传 Authorization / Cookie / X-Auth-Token）
  → POST webhook（带签名头 + 原始凭证 + 来源 IP 等元数据）
  → 2xx 放行；401/403 拒绝；超时(默认 2s)/5xx/网络错误 fail-closed 拒绝
```

性能约束（写进文档的硬性要求）：

- **判定缓存**：按凭证摘要（SHA-256，不落原文）缓存判定结果，TTL 默认 60s、上限条数可配——避免每个请求一跳；宿主登出后最迟 TTL 过期生效（文档明示该窗口）。
- 失败限速复用现有 authLimiter（按 IP）。
- webhook 端点必须校验签名头；凭证不写日志。

#### 6.7.3 选型对照

| 场景 | 方案 | 改动 |
|---|---|---|
| 形态 B（Go 库同进程） | `WithAuthenticator` 回调 / 宿主自包中间件 + `WithoutAuth` | 小（一个中间件替换点） |
| 形态 A/C（独立进程），宿主愿意改代理 | 6.5.3 方案 c：代理层注入 dqex token，鉴权仍在 dqex | 零（已有机制） |
| 形态 A/C，宿主要用**自己的**登录态直连 | 6.7.2 authWebhook | 中（webhook 客户端 + 缓存） |

### 6.8 分期建议

| 步骤 | 内容 | 启动条件 |
|---|---|---|
| F1（**嵌入线**，独立于库化阶段，可与阶段二并行） | **L3 优先**：整站 `/dqex` 前缀 + EmbedShell + embedBus + frameAncestors 白名单 + 鉴权外置（`WithAuthenticator`）+ 宿主示例页（按 6.5.5 清单，成本最低，任意框架可集成） | 环境管理产品确认需要嵌入查询/对比页；预研（前缀 + EmbedShell）可随时先行 |
| F2-F4（**触发式**，不进默认路线） | L1 解耦改造、vite library mode 打包、L2 功能块、npm 发布 | 首个 React 组件级集成场景确认（环境管理产品前端若为 React 则自然触发）；npm 走内部私服 |

> L3（iframe 整页嵌入）不依赖 Go 门面 API，与库化完全正交——这是本方案性价比最高的集成形态，独立成线避免被后端阶段阻塞。

---

## 7. 落地顺序与工作量拆解（双线 + 触发式）

**库化线（默认路线，服务两个使用方）：**

| 阶段 | 内容 | 主要改动 | 风险 |
|---|---|---|---|
| 一 | module 路径修正 + 打 tag | go.mod、全仓 import、Makefile、CI | 低（机械替换），破坏性一次性 |
| 二 | 门面包（先 tasks + query + meta + errors，含 3.6 并发冒烟测试） | 新增根包 ~6 个文件 | 低（薄包装），API 面评审是重点 |
| 三 | StoreMode 库模式（三态）+ 全局状态文档化 | service/service.go、persist.go、appconfig.go | 中（碰现有初始化链路，需回归 CLI/Web） |
| 四 | examples（兼作 CI 集成测试）/ 文档 / 首个使用方集成陪跑 | 新增文件为主 | 低 |

**嵌入线（独立于库化，与阶段二并行，见 6.8）：**

| 步骤 | 内容 | 启动条件 |
|---|---|---|
| F1 | 整站 /dqex 前缀 + L3 embed（EmbedShell/embedBus/frameAncestors）+ 鉴权外置（6.5.5 清单 0-7） | 环境管理产品确认嵌入诉求；前缀/EmbedShell 预研可先行。**依赖注意**：主形态 B 需要 `dqexweb.Serve` 包装层（原列于阶段四）——若 F1 先于阶段四启动，将该包装层（小改动）提前实现 |

**触发式（不进默认路线，避免无需求牵引的中风险改造）：**

| 项 | 触发条件 |
|---|---|
| 4.4.1 cydb 存储重写 / 4.4.2 Redis 缓存 | 环境管理产品确认多副本部署形态 |
| 4.4.3 产物落对象存储 | 任一使用方确认产物需集中存储或多副本无共享盘 |
| F2-F4（L1/L2 组件 + npm 私服发布） | 首个 React 组件级集成场景确认 |

**Exit criteria（阶段四验收，"库化成功"的定义）：**

1. 安装工具或环境管理产品之一，在测试环境以库方式跑通真实场景（安装器完成一次 schema 迁移并正确消费错误码/进度，或环境管理完成一次环境对比）。
2. examples 全部作为 CI 集成测试通过（编译 + smoke run，非纯演示）。
3. `docs/library.md` 完成：Quick Start、能力差异矩阵（库 vs CLI/Web）、错误码表、并发与回调契约（3.5/3.6）。

版本：v0.1.0（module 修正）→ v0.2.0（门面 API）→ v0.3.0（库模式）。库版本与二进制版本一致（见 2.2 tag 语义）；嵌入线（F1）与触发式项可独立合入、随最近一次 tag 发布，但不驱动库 API 版本语义（升版纪律见 2.2）。

---

## 8. 开放问题

1. ~~module 路径~~ **已定：`github.com/fj1981/dqex`**（与 infrakit 一致，repo 可私有，经 GOPRIVATE/公司 GOPROXY 拉取）。残留子项：是否由个人账号迁至 GitHub org（权限管理考量，不影响路径决策与阶段一执行）。
2. 门面是否需要异步任务 API（`Start*` 系列）——当前判断不需要，等两个使用方的真实反馈（长任务进度持久化/断点诉求）再加。
3. AI 能力是否纳入门面——依赖 eino/LLM 配置较重，建议 v0 不暴露，按需跟进。
4. 快照 `LoadSnapshot` 是否允许读任意路径（安全边界）——建议门面版限制在 `WithDataDir` 之下。
5. 触发式组件线（F2-F4）启动时的 npm 私服坐标（`@<org>/dqex-ui` 命名）——随触发条件一并确定。
6. embed 路由的跨域策略（外部宿主直连时需要 CORS / 反向代理方案）——F1 阶段定。
7. 产物引用切换到 cystore（bucket + object）后，历史记录表的引用字段结构，以及快照文件走对象存储后与 `WithDataDir` 本地快照目录的关系（互斥还是并存）——随 4.4.3 触发时细化。
8. 环境管理产品的部署形态（单副本/多副本）——决定 4.4.1/4.4.2 是否触发，尽早确认。
9. 短期机器 token 交换端点（长期 embed 场景的 token 轮换与自动续期）——随 F1 上线后按使用方反馈评估。
