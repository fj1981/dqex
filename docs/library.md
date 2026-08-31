# dqex 库使用文档（Go Library）

dqex 除独立 CLI/Web 应用外，也作为 **Go 库**交付：外部 Go 程序只 import `github.com/fj1981/dqex`
即可在进程内调用核心能力（迁移 / 导出 / 导入 / 对比 / 查询 / 元数据 / 快照），无需启动 dqex 进程
或经 HTTP 调用。典型场景：**安装工具**（安装/升级流程中执行 schema 迁移）、**环境管理产品嵌入**。

设计依据：[docs/library-api-design.md](library-api-design.md)（第 3 章为门面 API 契约）。

## 安装

```bash
go get github.com/fj1981/dqex
```

要求 Go >= 1.25。模块依赖走内部 Git + GOPROXY（见 CONTRIBUTING）。

## Quick Start（安装工具场景，进程内完成一次迁移）

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
            dqex.ConnInfo{ID: "src", Name: "源库", Conn: dqex.NewConn("mysql", "10.0.0.1", 3306, "root", "***", "app")},
            dqex.ConnInfo{ID: "tgt", Name: "目标库", Conn: dqex.NewConn("mysql", "10.0.0.2", 3306, "root", "***", "app_new")},
        ),
    )
    if err != nil {
        panic(err)
    }
    defer client.Close()

    err = client.RunMigrate(context.Background(), dqex.MigrateOptions{
        SourceConn: "src",
        TargetConn: "tgt",
    }, func(p dqex.ProgressInfo) {
        fmt.Printf("[%d/%d] %s\n", p.DoneUnits, p.TotalUnits, p.Message)
    })
    if err != nil {
        var se *dqex.SvcError
        if dqex.AsSvcErr(err, &se) {
            fmt.Println("code:", se.Code) // 按错误码分支处理，而非字符串匹配
        }
        panic(err)
    }
}
```

完整可运行示例见 `examples/`（migrate / export / compare / query / snapshot 各一个最小示例）。

## Options（client 生命周期配置）

| Option | 说明 |
|---|---|
| `WithDataDir(dir)` | 持久化根目录（快照/历史/连接库），非空即 StoreSQLite 便捷糖；空 = 纯内存 |
| `WithConfigFile(path)` | 显式 config.yaml；空 = 不加载全局配置 |
| `WithLang(lang)` | 错误消息语言（zh/en），默认 zh；决定所有 SvcError 渲染语言 |
| `WithInlineConns(...)` | 静态注入少量连接（内部为只读内存注册表） |
| `WithConnProvider(p)` | 连接提供者回调：连接列表完全由外部持有（配置中心/KMS 等，见下文契约） |
| `WithConnHooks(hooks)` | 连接生命周期回调（审计/监控） |
| `WithQueryHooks(hooks)` | SQL 审计钩子：任务与查询执行的每条语句回调（见"扩展点"） |
| `WithDataPreparers(map[db]fn)` | 数据前置处理器（key=目标库名）：数据包导入应用前回调宿主（见"扩展点"） |
| `WithContributors(ctbs...)` | 业务对象贡献者模板：业务对象取数/回写代理给宿主（见"扩展点"） |
| `WithStoreConn` / `WithStoreDB` / `WithCacheRedis` / `WithCacheClient` / `WithArtifactStore` / `WithMinio` / `WithS3` | **触发式能力**（4.4）：v0.x 门面即接受参数并校验，实现随多副本/对象存储场景确认后交付；当前注入会返回 `ErrNotImplemented` |

## 能力清单

门面方法与 `internal/service.Service` 一一对应（薄包装）。完整签名见 godoc。

| 分组 | 方法 |
|---|---|
| 生命周期 | `New(opts...)`、`Close()`（幂等）、`StoreMode()` |
| 连接管理 | `AddConnection`、`ListConnections`、`DeleteConnection`、`TestConnection`、`PingConnection` |
| 元数据 | `Databases`、`Schemas`、`Objects`、`TableColumns`、`ObjectDDL` |
| 查询执行 | `RunSQLScript`（ScriptParams）、`QueryTablePage`（PageParams）、`GenerateSQL`、`UpdateTableCell`、`GetCellValue`、`InsertTableRow`、`DeleteTableRows` |
| 任务型（同步） | `RunExport` / `RunDictionary`（返回 `*ArtifactRef`）、`RunImport`、`RunMigrate`、`RunCompare` |
| 快照 | `CreateSnapshot`（SnapshotParams）、`LoadSnapshot`（离线读文件）、`CompareSnapshot`（target 用完整连接信息） |

设计原则（3.3）：**同步优先**——只暴露 `Run*` 系列，不暴露 `Start* + taskID` 异步任务模型；
需要后台并发由调用方自己起 goroutine，取消统一走 `ctx`。

## StoreMode：库模式 vs 应用模式

| 模式 | 触发方式 | 行为 |
|---|---|---|
| `StoreNone`（默认） | 不传 `WithDataDir` | 纯内存：不建存储、不写历史/任务/连接库；`AddConnection`/`DeleteConnection` 仅内存生效；快照仅内存返回（调用方自行持久化） |
| `StoreSQLite` | `WithDataDir(dir)` | 与 CLI/Web 相同的持久化链路（数据目录 + SQLite） |
| `StoreExternal` | `WithStoreConn`/`WithStoreDB`（触发式） | 外部注入存储，实现随 4.4 交付 |

产物落位规则：`ExportOptions.OutputDir` / `DictionaryOptions.OutputDir` 显式指定则用之；
为空时回填持久化 export 目录。**StoreNone 且未设 `WithDataDir` 时无回填来源，
必须显式指定 OutputDir，否则返回 `ErrExpOutDir`。**

## 数据包导入与回滚产物（DataPackage 契约）

**数据格式**：`ExportOptions.Format="json"` 导出 DataPackage 数据包，JSON 结构如下
（字段冻结，向后兼容）。条目类型：`0` 建表（CREATE TABLE）、`1` 行数据（按 PK 幂等
upsert）、`2` 成对 SQL（每项 map 一个执行语句键，值为回滚语句字符串或经 `rollback`
键携带）。适用于业务配置类**中小数据量**（整包驻留内存；Oracle 单条 IN 上限
1000 行表达式，超限需拆分条目）。

```json
{
  "db": "biz",
  "datas": [
    {"type": 0, "table": "t1", "sql": "CREATE TABLE t1 (...)"},
    {"type": 1, "table": "t1", "pk": ["id"], "data": [{"id": "a1", "name": "x"}]},
    {"type": 2, "table": "",   "data": [{"ALTER TABLE t1 ADD c int": "ALTER TABLE t1 DROP COLUMN c"}]}
  ],
  "index": {"t1": 0}
}
```

- `datas`：条目数组，按顺序应用（先建表后写数据）；`index` 为表名→首个条目下标的
  兼容索引（`Add` 合并语义按 `(表,类型)` 复合键）。
- 数字值以 json.Number（原文）解码：雪花 ID 等超过 float64 精度的整数主键往返不失真。
- `datas` 数组中的 `null` 条目在加载时被丢弃，不产生错误。

**导入语义**（`RunImport` 导入 `.json`）：单库单事务应用（PG/Oracle 含 DDL 可整体
回滚；MySQL 系 DDL 隐式提交，事务仅覆盖数据语句）。导入后精确回滚需
`ImportOptions.Rollback=true`。

**回滚产物契约**：

- 命名：单库为 `<输入名>.rollback.sql`；多库 zip 按库分文件 `<输入名>.<库名>.rollback.sql`
  （回滚语句无库上下文，回放须连接对应库）。`ImportResult.RollbackPath` 为首个产物
  路径，多库其余产物经任务日志输出。
- 内容：基于**应用时点旧值快照**的回放语句（DELETE 本次导入行 → REPLACE 导入前旧行；
  建表条目回滚为 DROP TABLE）。仅适用于**紧随导入的撤销**，不适用于长期 revert。
- 权限：产物含旧行全量明文数据，落盘权限 **0600**，宿主需管控输入文件所在目录访问。
- 回放方式：按文件内语句顺序整体回放；MySQL DML 失败回滚后回放产物收敛到应用前状态，
  已建表残留由 `DROP TABLE IF EXISTS` 兜底。
- 静默丢失防护：产物写出失败逐个告警；一份都写不出时导入硬失败（`errImpRollback`），
  不允许静默丢失回滚能力。
- `DataApplyResult.Unrollback`：执行了但无法生成精确回滚的语句清单（含空回滚的成对
  SQL），宿主侧需甄别告警；`SkippedTables` 为无主键跳过表。

**MySQL sql_mode 假设**：PK 值转义按默认模式（反斜杠为转义符）处理；开启
`NO_BACKSLASH_ESCAPES` 时含反斜杠的 PK 值可能匹配不中（单引号转义不受影响，
注入防护始终有效）。

## 扩展点（宿主回调注册）

dqex 的核心设计是**编排归引擎、业务归宿主**：连接持有、SQL 审计、数据策略、业务对象
取数/回写均通过回调代理给宿主实现，引擎统一负责任务目录、进度、打包、事务与回滚。
所有回调共享同一契约基线：

- 回调运行在 dqex 的任务/调用 goroutine 上，**阻塞会暂停任务**（重活自行起 goroutine）；
- 取消统一由回调参数 `ctx` 控制；回调内不得再回调 Client 方法；
- 回调可能被并发调用，实现方必须线程安全。

### connKey 约定

连接引用一律使用 connKey 字符串（`ExportOptions.SourceConn`、`ImportOptions.TargetConn`、
`MigrateOptions.SourceConn/TargetConn`、查询 API 的 `connKey` 参数等）。宿主自行定义
key 语法并在 `ConnProvider.GetConn` 中解析；多环境宿主建议采用分层 key（如
`env:prod/db:biz`），因为业务级回调（Contributor/DataPreparer 的 `req.Key`）按
connKey 路由到对应环境。注意：任务参数若使用内联直连（直接填 `Source`/`Target`
连接信息而非 connKey），回调的 `Key` 为空字符串——**多环境宿主必须使用 connKey 引用连接**。

### 连接提供者（ConnProvider）与连接钩子（ConnHooks）

连接完全由外部持有时注入 `WithConnProvider`：连接可来自配置中心、密钥管理系统
（Vault/KMS）、宿主自身数据库等任何地方。dqex 不落盘、不缓存密码；每次真实建连都走
`GetConn` 取最新凭证（密码轮换天然支持）。`ListConns` 供列表/元数据遍历使用，可脱敏；
`GetConn` 必须返回可用明文密码，返回 `(nil, nil)` 表示不认识该 key（继续尝试后续来源）。

```go
client, _ := dqex.New(
    dqex.WithConnProvider(dqex.ConnProvider{
        ListConns: func(ctx context.Context) ([]dqex.ConnInfo, error) { return host.listConns(ctx) },
        GetConn:   func(ctx context.Context, key string) (*dqex.DBConnInfo, error) { return host.resolve(key) },
    }),
    dqex.WithConnHooks(dqex.ConnHooks{
        OnResolved: func(ctx context.Context, key string, src dqex.ConnSource) { /* 审计 */ },
        OnConnect:  func(ctx context.Context, c *dqex.DBConnInfo, err error) { /* 日志注意遮蔽 Pwd */ },
    }),
)
```

`DBConnInfo` 内嵌 cydb 的 `DBConnection`（组合字面量无法直接设置提升字段），请使用
`dqex.NewConn(dbType, host, port, user, password, dbname)` 构造。

### SQL 审计钩子（QueryHooks）

`WithQueryHooks` 注册后，任务（导出/导入/迁移）与 `RunSQLScript` 执行的**每条语句**
都会回调 `OnQuery`，宿主可接合规审计、慢查询采集：

```go
dqex.WithQueryHooks(dqex.QueryHooks{
    OnQuery: func(ctx context.Context, connKey, stmt string, costMs, rows int64) {
        audit.Log(connKey, stmt, costMs, rows) // 必须快速返回
    },
})
```

契约：同步调用，审计耗时直接计入语句响应时间；失败语句同样回调（`rows=-1`）；
超长语句截断至 4096 字节后回调（截断点回退至 UTF-8 字符边界）；DDL 与批量写语句
同样有埋点（批量写回调的是模板描述文本）。

### 数据前置处理器（DataPreparer，key=目标库名）

`.json` 数据包（DataPackage）导入应用前，按目标库名回调宿主执行业务策略（如业务
表单/流程的版本合并），宿主可直接修改包内容后返回：

```go
dqex.WithDataPreparers(map[string]dqex.DataPreparer{
    "biz": func(ctx context.Context, req dqex.DataPrepareRequest) (context.Context, *dqex.DataPackage, error) {
        // req.Key=目标连接 connKey，req.DB=目标库名，req.Package 可原地修改
        return nil, nil, nil // 返回 nil ctx 沿用原 ctx；nil 包沿用入参
    },
})
```

返回值：新 ctx（nil 沿用原 ctx，派生值/取消能力会传播到后续流程）、修改后的包
（nil 沿用入参）、错误。全局注册与逐调用注入合并时，**逐调用同键优先**。

### 业务对象贡献者（Contributor）

宿主业务对象（流程/面板/规则/数据表等）的"取数"与"回写"经 `WithContributors` 注册
回调代理，导出/导入的编排（任务目录、进度、zip 打包、清单）由 dqex 统一负责。
**配置层与代理层分离**：`ExportOptions.Contributors` / `ImportOptions.Contributors`
仅携带 `Type` + `IDs`（可序列化，可存任务配置）；回调本体由 Client 注册的模板按
`Type` 补齐（同 Type 多条目的 IDs 取并集），未注册的 Type 返回 `ErrCtbUnknown`（2031）。
Contributor 扩展点仅库模式可用（CLI/Web 无回调注册来源）。

```go
dqex.WithContributors(dqex.Contributor{
    Type:  "flow",                    // 同时是 zip 包内目录名（sanitizeName 处理）
    Title: "流程",                    // 进度/日志展示名
    Export: func(ctx context.Context, req dqex.ContributorRequest) (*dqex.ContributorResult, error) {
        // 把 req.IDs 指定的对象写入 req.Dir（任务目录下 flow/），格式任意
        // req.Key=连接 connKey（宿主据此路由环境）、req.Conn=源连接、req.DB=业务库名
        return &dqex.ContributorResult{Files: files, Count: n}, nil
    },
    Import: func(ctx context.Context, req dqex.ContributorImportRequest) error {
        // 从 req.Dir 读回业务对象（SQL 导入完成之后调用）；nil 表示仅导出
        return nil
    },
})
```

zip 包布局约定：根级 `<库名>.sql`（每库一个）+ 根级可选 `<库名>.json` 数据包 +
`<Type>/` 业务对象目录；导入时按目录存在性回调 Import。

## 并发模型与回调契约（3.6，必读）

1. `Client` 可被多个 goroutine 并发使用：并发调用任意能力方法（含同一方法）安全。
2. 连接池为**进程级**共享；`Close()` 幂等，之后调用能力方法返回 `ErrClientClosed`。
   注意：Close 会释放进程级连接池，同进程其他 Client 实例的池化连接一并释放。
3. 同进程多 Client 实例可用但共享元数据缓存与连接池（v0 语义，安全无冲突）。
4. `ProgressFunc`：回调在 dqex 执行 goroutine 上**同步调用，回调内阻塞会暂停任务**（重活自行转 goroutine）；
   回调返回不是取消机制——取消统一由 `ctx` 控制；不保证串行、不保证每阶段恰好触发一次，
   消费方按幂等展示设计。
5. `ConnProvider` 回调：`ListConns`/`GetConn` 可能被并发调用，实现方必须线程安全；
   `GetConn` 返回 `(nil, nil)` 表示不认识该 key（继续尝试后续解析来源）；
   `OnResolved`/`OnConnect` 参数含密码字段，日志注意遮蔽。

## 错误体系（契约）

错误是结构化的 `*dqex.SvcError`（Code/Key/Args/Cause + 按语言渲染），用 `dqex.AsSvcErr` 提取，
**按错误码分支处理，而不是字符串匹配**。错误码分为 cygin 系统码（1001~1012）与业务码（2001 起）。

### 系统错误码（cygin）

| 码 | 常量 | 含义 |
|---|---|---|
| 1001 | `ErrInternalServer` | 内部服务器错误 |
| 1002 | — | 数据库操作错误 |
| 1004 | — | 未授权 |
| 1005 | — | 禁止访问 |
| 1006 | — | 资源未找到 |
| 1007 | — | 操作超时 |
| 1011 | `ErrParamsInvalid` | 参数无效 |
| 1012 | — | 请求错误 |

### 业务错误码（库模式高频用加粗）

| 码 | 常量 | 含义 |
|---|---|---|
| 2001 | `ErrConnNotFound` | 连接配置不存在 |
| 2002 | `ErrUnsupportedType` | 不支持的数据库类型 |
| 2003 | `ErrTaskNotFound` | 任务/快照不存在 |
| 2004 | `ErrExecFailed` | 执行操作失败（导出/导入/迁移等） |
| 2005 | `ErrFileType` | 不支持的文件类型 |
| 2006 | `ErrNoArtifact` | 任务没有可下载产物 |
| 2008 | `ErrConnNotSpecified` | 未指定数据库连接 |
| 2009 | `ErrConnFailed` | 数据库连接/操作失败 |
| 2010 | `ErrTaskInvalid` | 任务配置无效 |
| 2011 | `ErrCryptoFailed` | 配置文件加解密失败 |
| **2027** | **`ErrExpOutDir`** | **库模式（StoreNone 且无 DataDir）下未显式指定产物输出目录** |
| **2028** | **`ErrClientClosed`** | **客户端已关闭（Close 后调用能力方法）** |
| 2029 | `ErrNotImplemented` | 触发式能力尚未实现（WithStoreConn 等，随 4.4 落地） |
| 2030 | **`ErrStoreUnavailable`** | 当前模式无持久化存储，该能力不可用（SQL 历史/任务配置/收藏等） |
| **2031** | **`ErrCtbUnknown`** | **业务对象贡献者未注册：任务引用的 Type 无导出/导入回调** |

其余业务码（2012~2026，Web/AI 相关）库模式一般不涉及，见 `internal/service/errors.go`。
引擎层错误（如回滚产物失败 `errImpRollback`）以 `MsgError`（含 Key 与底层原因）原样透出，
不经数字码包装。

### 错误码 → 进程退出码建议映射（安装工具场景）

安装工具通常以退出码判定流程分支，建议宿主按下表映射（`context` 取消/超时用标准判定）：

| 退出码 | 语义 | 触发条件 |
|---|---|---|
| 0 | 成功 | — |
| 2 | 通用失败 | 兜底：`ErrInternalServer` 及未识别错误 |
| 101 | 参数/配置错误 | `ErrParamsInvalid`、`ErrTaskInvalid`、`ErrConnNotSpecified`、`ErrUnsupportedType`、`ErrExpOutDir`、`ErrFileType` |
| 102 | 连接不可用 | `ErrConnNotFound`、`ErrConnFailed`、1002 |
| 103 | 目标不存在 | `ErrTaskNotFound`、`ErrNoArtifact`、1006 |
| 104 | 执行失败 | `ErrExecFailed` |
| 105 | 能力不可用 | `ErrNotImplemented`、`ErrStoreUnavailable`、`ErrCtbUnknown` |
| 106 | 客户端状态错误 | `ErrClientClosed` |
| 124 | 超时 | `context.DeadlineExceeded`、1007 |
| 130 | 用户取消 | `context.Canceled`（对应 shell SIGINT 语义） |

参考实现：

```go
func exitCode(err error) int {
    switch {
    case err == nil:
        return 0
    case errors.Is(err, context.Canceled):
        return 130
    case errors.Is(err, context.DeadlineExceeded):
        return 124
    }
    var se *dqex.SvcError
    if !dqex.AsSvcErr(err, &se) {
        return 2
    }
    switch se.Code {
    case dqex.ErrParamsInvalid, dqex.ErrTaskInvalid, dqex.ErrConnNotSpecified,
        dqex.ErrUnsupportedType, dqex.ErrExpOutDir, dqex.ErrFileType:
        return 101
    case dqex.ErrConnNotFound, dqex.ErrConnFailed:
        return 102
    case dqex.ErrTaskNotFound, dqex.ErrNoArtifact:
        return 103
    case dqex.ErrExecFailed:
        return 104
    case dqex.ErrNotImplemented, dqex.ErrStoreUnavailable:
        return 105
    case dqex.ErrClientClosed:
        return 106
    default:
        return 2
    }
}
```

## 库 vs CLI/Web 能力差异矩阵

| 能力 | 库（StoreNone） | 库（WithDataDir） | CLI/Web |
|---|---|---|---|
| 迁移/导入/导出/对比/字典 | ✓ | ✓ | ✓ |
| 查询/元数据/单元格编辑 | ✓ | ✓ | ✓ |
| 快照创建/对比 | ✓（仅内存） | ✓（落盘） | ✓（落盘 + 管理界面） |
| 连接 Add/Delete | ✓（仅内存） | ✓（落盘） | ✓ |
| SQL 历史 / 任务配置 / 收藏 / 工作区 | ✗（`ErrStoreUnavailable`） | ✓ | ✓ |
| 异步任务（Start* + taskID） | ✗（同步 API + 自起 goroutine） | ✗ | ✓ |
| AI 辅助 | 未配置时明确报错 | ✓ | ✓ |
| 触发式基础设施（外部存储/Redis/对象存储） | ErrNotImplemented（随 4.4） | 同左 | CLI/Web 现状 |

## i18n

`WithLang` 决定所有 `SvcError` 消息与引擎 `MsgError` 的语言（zh/en）。
库使用者也可用 `dqex.WithLangCtx(ctx, lang)` 为单次调用指定语言。

## 宿主 Web 集成：gin 挂载 + iframe 嵌入（形态 B，推荐）

除了进程内 Go API，dqex 的 **Web UI + 全套 HTTP API** 也可同进程挂载到宿主自己的 gin engine：
库调用与页面操作共享同一个 Service 实例（同一份连接/状态），同源 iframe 无 token/CORS/SameSite 问题。
这是设计文档 6.5.1 的**主形态 B**，也是两个内部使用方（环境管理/安装工具均为 Go 程序）的默认形态。

```bash
go get github.com/fj1981/dqex/dqexweb
```

```go
import (
    "github.com/gin-gonic/gin"

    dqex "github.com/fj1981/dqex"
    "github.com/fj1981/dqex/dqexweb"
)

r := gin.New()
r.Use(hostAuthMiddleware) // 鉴权外置：宿主登录态，作用于 /dqex 子树即可

client, _ := dqex.New(dqex.WithConnProvider(...))
dqexweb.Mount(r, client, dqexweb.MountOptions{
    Prefix:         "/dqex",                        // API=/dqex/api，UI=/dqex/
    FrameAncestors: []string{"https://host.example"}, // 允许 iframe 嵌入的宿主 origin（CSP frame-ancestors）
    // Fallback:     hostNotFoundHandler,           // 宿主自有 SPA/404 回退（可选）
})
r.Run(":8080")
```

前端嵌入（同源，HashRouter 深链直用）：

```html
<!-- 完整界面 -->
<iframe src="/dqex/#/query"></iframe>
<!-- 精简嵌入视图：无侧边栏/页头/连接管理入口，仅数据操作视图（6.5.2） -->
<iframe src="/dqex/?embed=1#/embed/query"></iframe>
```

**嵌入视图与 postMessage 协议**（`embedBus`，仅 `#/embed/` 前缀激活，普通模式零影响）：

| 消息 | 方向 | 说明 |
|---|---|---|
| `dqex:ready` | iframe → 宿主 | 加载完成，宿主可发起 init |
| `dqex:init` | 宿主 → iframe | `{ token?, lang?, theme?, config? }`；token 走握手则不落 URL |
| `dqex:state` | iframe → 宿主 | 状态变化（如选表结果）`{key, value}` |
| `dqex:action` | iframe → 宿主 | 请求宿主动作（如关闭弹层） |
| `dqex:resize` | iframe → 宿主 | `{height}` 高度自适应 |
| `dqex:command` | 宿主 → iframe | 宿主下发指令（刷新/销毁等） |
| `dqex:tokenExpired` | iframe → 宿主 | API 401，宿主重新握手或刷新 src |

安全约束（6.5.4）：
- `FrameAncestors` 配置后对本挂载子树全部响应生效（hash 不发送到服务端，CSP 只能按请求粒度设置）；生产环境建议显式配置。
- 嵌入视图白名单：仅 `query/export/import/migrate/compare/dictionary/snapshot/task`，设置页/连接管理不可经嵌入视图暴露。
- 宿主 origin 白名单经 iframe URL `?origin=a,b` 传入 `embedBus` 校验；未配置时放行任意 origin（内网默认）。

完整示例见 [examples/webmount](../examples/webmount/main.go)。

## 版本与兼容性承诺

- `Options` 结构体字段自 **v1 起冻结为公开契约**：新增字段只能加、不能改既有字段语义。
- 公开错误码常量、方法签名遵循语义化版本；破坏性变更仅出现在大版本。
- 触发式能力（4.4）落地不改变既有签名。
