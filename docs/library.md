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

其余业务码（2012~2026，Web/AI 相关）库模式一般不涉及，见 `internal/service/errors.go`。

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
| 105 | 能力不可用 | `ErrNotImplemented`、`ErrStoreUnavailable` |
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
