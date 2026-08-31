# tl-env 集成 dqex 方案

> 版本 v1（2026-08-28）。配套实现：dqex 贡献者扩展点已落地（见第 4 节），
> tl-env 侧改造按第 5 节分阶段执行。

## 1. 背景与目标

tl-env（环境管理平台）现有三块数据能力自研维护：

| tl-env 现状 | 位置 | 问题 |
|---|---|---|
| SQL 执行台（单条执行/EXPLAIN/回滚生成） | `internal/service/dbconsole.go`、`internal/controller/dbconsole.go` | 前端无对象树/补全，方言处理与 dqex 重复 |
| 业务数据导入（zip：SQL 模式 / JSON 模式） | `pkg/database.go` ImportWithSqlMode / ImportWithJsonMode | 与 dqex importer 逻辑重复且较弱 |
| 导出（DB 全量 + flow/panel/datatable/rule 业务对象） | `internal/service/export.go`、`pkg/database.go` ExportDB / ExportFlow 等 | 打包/进度/任务调度自研，无依赖排序、无视图/函数/存储过程覆盖 |

目标：**数据底座统一收敛到 dqex**，tl-env 保留业务语义层；通过
**配置层 + 代理层**（Contributor 扩展点）把 tl-env 特有的业务对象导出/导入
代理回宿主，而不是把业务代码搬进 dqex。

## 2. 总体架构

```
┌───────────────────────────── tl-env ─────────────────────────────┐
│  gin Engine                                                      │
│  ├── tl-env 自有路由（/tool/api/...，业务对象管理、环境管理等）      │
│  ├── dqexweb.Mount(r, client, MountOptions{Prefix: "/dqex"})     │
│  │     ├── /dqex/api/...   API（鉴权外置 → tl-env 现有中间件）      │
│  │     └── /dqex/...       前端静态资源（iframe 嵌入 #/embed/...）  │
│  └── Contributor 注册（代理层）：flow/panel/datatable/rule 回调     │
│                                                                  │
│  dqex.Client（StoreNone 库模式）                                  │
│  ├── WithConnProvider：envId+dbName → DBConnInfo 映射              │
│  ├── WithConnHooks：审计日志桥接 logSvc                            │
│  └── WithContributors：业务对象取数/回写代理                        │
└──────────────────────────────────────────────────────────────────┘
```

连接解析映射：tl-env 的 `getDBCli(envId, dbName)` 语义由 `ConnProvider.GetConn`
承接，connKey 约定为 `env:<envId>/db:<dbName>`，GetConn 内部读 tl-env
环境配置组装 `dqex.DBConnInfo`（Pw 字段，用 `dqex.NewConn()` 构造）。

**连接作用域约定**：dqex 连接模型是全局扁平列表，tl-env 是 env 级作用域，
集成时必须遵守——①嵌入视图一律 `?conn=<key>` 显式指定连接，不依赖全局连接
下拉；②`ConnProvider.ListConns` 仅返回"当前默认 env"的库（或 tl-env 侧按
前端会话上下文过滤），避免把所有 env 的库混入 UI；③跨 env 操作由任务
connKey 显式表达，不做隐式选择。

## 3. 能力映射与处置

| tl-env 能力 | dqex 承接 | 处置 |
|---|---|---|
| dbconsole `/execute` | `client.RunSQLScript`（多语句批量） | 阶段一替换底层执行 |
| dbconsole `/explain` `/validate` `/generate-rollback` `/preview-impact` | 无对应 | **保留 tl-env 自研**（业务增强，不下沉） |
| ExportDB（库全量 SQL 导出） | `client.RunExport`（更强：外键排序、视图/函数/存储过程、一致性快照） | 阶段二替换 |
| ExportFlow / ExportPanel / ExportDataTable / ExportRule | Contributor 代理（见第 4 节） | 取数逻辑留在 tl-env，编排上移 dqex |
| ImportWithSqlMode | `client.RunImport`（zip / .sql） | 阶段二替换 |
| ImportWithJsonMode（data_entity/camunda JSON） | Contributor 代理（import 回调） | 同上 |
| ExportScheduler 任务调度、上传下载记录 | ProgressFunc + ArtifactRef；上传记录留 tl-env | 任务调度收敛，产物上传留宿主 |
| zip 打包、进度计算、目录管理 | dqex 引擎内建 | 删除 tl-env 自研 |

## 4. Contributor 扩展点（本次已落地）

### 4.1 两层设计

- **配置层（可序列化）**：`ExportOptions.Contributors` / `ImportOptions.Contributors`
  只填 `Type`（业务对象类型，同时是 zip 内目录名）+ `IDs`（对象 ID 列表），
  纯数据、可进 yaml 任务配置/前端表单/CLI 参数。
- **代理层（回调）**：宿主在 `dqex.New` 时用 `WithContributors` 注册
  `dqex.Contributor` 模板（Type + Export/Import 回调）。服务层按 Type 匹配补齐回调
  （同 Type 多条目自动合并 IDs 并集）；未注册返回 `ErrCtbUnknown`（2031）。
- **适用范围**：该扩展点仅库模式（`dqex.New`）可用；CLI/Web 模式无注册来源，
  任务配置引用 Contributors 将报 `ErrCtbUnknown`。

职责划分：**dqex 负责编排序**（任务目录、进度单元、`<Type>/` 目录约定、zip 打包、
导入顺序），**宿主只写"取数/回写"**。

### 4.2 包内目录约定（zip 布局）

```
task_20260828_150405.zip
├── mydb.sql            # dqex 引擎导出（建表/数据/视图/函数/存储过程，外键排序）
├── flow/               # 贡献者：流程对象（宿主自定义内部格式）
│   └── flow_xxx.json
├── panel/              # 贡献者：面板
├── datatable/          # 贡献者：数据表业务数据
└── rule/               # 贡献者：规则
```

导入时：引擎先导入全部 `*.sql`（根级），再对存在的 `<Type>/` 目录回调
`Contributor.Import`；包内无该目录则跳过（前后版本兼容）。

### 4.3 API 签名（dqex 根包）

```go
type Contributor struct {
    Type   string // zip 内目录名，如 "flow"
    Title  string // 进度/日志展示名
    Export func(ctx context.Context, req ContributorRequest) (*ContributorResult, error)
    Import func(ctx context.Context, req ContributorImportRequest) error // nil = 仅导出
    IDs    []string // 任务配置层携带；注册模板留空
}

type ContributorRequest struct {
    Key  string      // 任务连接 connKey（如 env:prod/db:biz），宿主据此路由 env 级业务上下文
    Conn *DBConnInfo // 导出源连接
    DB   string      // 业务库名（连接配置库）
    IDs  []string
    Dir  string      // 写入目录（任务目录下 <Type>/，已存在）
}

type ContributorResult struct {
    Files []string // 生成文件清单（相对 Dir）
    Count int      // 对象数（进度展示）
}

type ContributorImportRequest struct {
    Key  string      // 任务目标连接 connKey
    Conn *DBConnInfo // 导入目标连接
    DB   string
    Dir  string      // 解压后 <Type>/ 目录
}
```

回调契约（与 ProgressFunc 一致）：运行在任务执行 goroutine 上，阻塞会暂停进度
推送；取消由 ctx 控制；回调内不得再回调 Client 方法。

> **Key 取值说明**：`Key` 来自任务选项的 `SourceConn`/`TargetConn`（connKey）。
> 若宿主以 `Source: &DBConnInfo{...}` 内联直连方式发起任务（不经 ConnProvider），
> `Key` 为空——多 env 宿主应**始终以 connKey 引用连接**，否则回调无法路由
> env 级业务上下文。

### 4.4 tl-env 侧注册示例（代理层桥接现有代码）

```go
import (
    "github.com/fj1981/dqex"
    "gitlab.mycyclone.com/rpa-platform/tl-env/pkg/datatable" // tl-env 自有包，非 infrakit
)

// flow 贡献者：复用 tl-env 现有 NewFlowData.ExportByIds，只做"写到 Dir"的桥接。
// 业务策略是 env 级的：按 req.Key 解析环境，再取该 env 的规则配置。
func flowContributor(getDBCli func(envID, db string) (*cydb.DBCli, error)) dqex.Contributor {
    return dqex.Contributor{
        Type:  "flow",
        Title: "流程",
        Export: func(ctx context.Context, req dqex.ContributorRequest) (*dqex.ContributorResult, error) {
            envID, dbName, err := parseConnKey(req.Key) // "env:prod/db:biz" → ("prod","biz")
            if err != nil {
                return nil, err
            }
            cli, err := getDBCli(envID, dbName)
            if err != nil {
                return nil, err
            }
            ruleCfg, err := envRuleCfg(envID) // env 级业务配置
            if err != nil {
                return nil, err
            }
            fd, err := datatable.NewFlowData(cli, ruleCfg, sqlMode(envID))
            if err != nil {
                return nil, err
            }
            // ExportByIds 改造点：目标目录改为 req.Dir（原为任务目录硬编码）
            if err := fd.ExportByIds(ruleCfg, req.Dir, req.IDs...); err != nil {
                return nil, err
            }
            return &dqex.ContributorResult{Count: len(req.IDs)}, nil
        },
        // Import 回调同理桥接 NewFlowData 的导入方法（req.Key 路由 + req.Dir 读回）
    }
}

func newDQEXClient() (*dqex.Client, error) {
    return dqex.New(
        dqex.WithConnProvider(connProviderFromTlEnv), // env:<envId>/db:<dbName> → DBConnInfo
        dqex.WithConnHooks(dqex.ConnHooks{
            OnConnect: func(ctx context.Context, conn *dqex.DBConnInfo, err error) {
                // 桥接 logSvc 审计（注意遮蔽 Pwd）
            },
        }),
        dqex.WithContributors(
            flowContributor(...), panelContributor(...),
            datatableContributor(...), ruleContributor(...),
        ),
    )
}
```

### 4.5 任务调用示例（配置层驱动）

```go
ref, err := client.RunExport(ctx, dqex.ExportOptions{
    SourceConn:     "env:prod/db:biz",       // ConnProvider 解析
    TaskName:       "release_2.3.1",
    OutputDir:      "/data/dl",              // tl-env 下载目录
    Compress:       true,
    SingleTransaction: true,                 // 一致性快照（原 ExportDB 不具备）
    Contributors: []dqex.Contributor{        // 配置层：仅 Type + IDs
        {Type: "flow", IDs: []string{"f-1", "f-2"}},
        {Type: "panel", IDs: []string{"p-7"}},
        {Type: "rule", IDs: []string{"r-3"}},
    },
}, func(p dqex.ProgressInfo) { /* 进度推送（WebSocket/SSE 桥接） */ })
// ref.Ref = zip 路径 → tl-env 上传并登记下载记录（UploadMeta）
```

导入：

```go
err := client.RunImport(ctx, dqex.ImportOptions{
    TargetConn: "env:uat/db:biz",
    InputPath:  zipPath,
    ResetMode:  dqex.ResetDrop,
    Contributors: []dqex.Contributor{ // 仅声明参与回读的类型
        {Type: "flow"}, {Type: "panel"}, {Type: "rule"},
    },
}, nil)
```

> JSON 模式（ImportWithJsonMode 的 data_entity/camunda）的**通用引擎部分将下沉为
> dqex 原生能力**（数据格式导出 + 精确回滚，见第 7 节）；tl-env 的业务版本合并
> 策略（Camunda/FormDefinition 等 PreProcess）经 `PreProcessors` 回调注册，仍留宿主。

## 5. 分阶段实施（tl-env 侧）

### 阶段一：SQL 执行底座替换（低风险，先行验证）

1. `go.mod` 引入 `github.com/fj1981/dqex`（私有仓库用 replace 指向内网 Git）。
2. 构建 Client：`WithConnProvider` 映射 env/db → DBConnInfo；
   `WithQueryHooks` 桥接写操作审计（原 ExecuteSQL 的 sqlType 判别 + logSvc 记录
   语义由 QueryHooks 承接，不得遗漏——SQL 审计是安全合规项）。
   **前置**：QueryHooks 目前为设计预留接口位（library-api-design 4.4），需在
   dqex v0.2 先实现 `OnQuery` 钩子与 `WithQueryHooks` 选项（见 7.4 步骤 0）。
3. `dbConsoleService.ExecuteSQL` 底层改调 `RunSQLScript`（保留 explain/
   validate/rollback 等增强接口不动）。
4. 回归：dbconsole `/execute` + 内部脚本调用路径。

### 阶段二：导入/导出切换 + 贡献者接入

1. `ExportDB` → `RunExport`；`ImportWithSqlMode` → `RunImport`。
2. 注册 4 个业务贡献者（改造点：`ExportByIds`/导入方法的目标目录参数化）。
3. `ExportTask.DoTask` 中的打包/进度/目录管理删除，保留任务创建 + 上传登记。
4. 存量包兼容（见 6.1）。

### 阶段三：前端 iframe 嵌入

1. 菜单/按钮跳转 `/dqex/#/embed/query?conn=<key>`（对象树 + SQL 编辑器 + 表数据）。
2. `/dqex/#/embed/export|import` 接导出导入视图；`postMessage` 协议接收任务完成事件。
3. 鉴权：宿主 Cookie `SameSite=None; Secure`（或 token 注入方案，见
   library-api-design 6.5.3）。
4. 前置安全配置：`MountOptions` 配置 `frameAncestors` CSP（作用于全站，
   需评估对 tl-env 自身页面无副作用）；嵌入页 postMessage 收发必须带
   `?origin=` 白名单参数，禁止 `*`。

## 6. 兼容性与风险

### 6.1 zip 布局差异（重点）

| | tl-env 存量包 | dqex 包 |
|---|---|---|
| 结构 | `<库名>/<文件>.sql`（嵌套目录，含 `._` 前缀过滤） | 根级 `<库名>.sql` + `<Type>/` 目录 |

处置：
- **新导出统一 dqex 布局**（贡献者目录即业务对象的落位，取代旧的
  `RuleCfg.GetDatabaseDir` 子目录约定）。
- **存量包导入**：切换期保留 tl-env 旧导入通道（仅 ImportWithSqlMode，
  代码量小），按包内是否存在 `ExportDesc`/根级 `*.sql` 自动判别走向；稳定后下线。

### 6.2 其他风险

| 风险 | 缓解 |
|---|---|
| 导入事务/错误定位差异（tl-env 整文件事务 + `[起始行,结束行]` 定位） | dqex importer 按 SQL 块执行并报块级错误；若需整文件事务语义，阶段二在宿主侧评估，必要时给 dqex 提需求 |
| 方言覆盖（达梦等国产库） | 上线前跑 dqex 支持矩阵核对 tl-env 实际 DB 类型清单 |
| JSON 模式导入依赖 `datatable.NewDataWriteTool` | 通用引擎下沉 dqex（第 7 节，v0.2 演进项）；过渡期走 Contributor 代理 |
| **产物敏感度**：回滚 SQL 含导入前整行旧值；json 数据产物含全量业务数据明文 | 上传文件服务/下载记录须有权限控制与保留期策略；产物路径不得落在匿名可读位置 |
| **审计完整性**：替换 ExecuteSQL 后写操作审计经 `WithQueryHooks` 承接（阶段一步骤 2） | 上线前核对审计日志覆盖率与原 logSvc 记录一致 |
| 产物上传（文件服务）依赖任务结果 | RunExport 返回 `ArtifactRef`（local 路径），tl-env 拿路径上传并登记；WithArtifactStore 仍为触发式，不阻塞 |
| 服务层任务调度（Web UI 异步任务）与库模式同步 API 差异 | 库模式同步 + 宿主自起 goroutine；进度经 ProgressFunc 推送 |

## 7. 演进：数据格式导出与精确回滚（dqex 原生，v0.2）

> 述求来源：dqex 自身即需要"结构化数据格式导出"与"导入后精确回滚"，
> tl-env JSON 模式是第二使用者——通用引擎下沉条件已成立。

### 7.1 能力一：数据导出格式（DataPackage JSON）

`ExportOptions.Format` 增加数据格式选项（默认 `"sql"` 不变，落地时常量化为
`FormatSQL` / `FormatJSON`，避免裸字符串扩散）：

```go
Format string // FormatSQL（默认，现行行为） | FormatJSON（DataPackage 数据格式，每库一个 .json）
```

数据格式契约（JSON 结构与 tl-env `DataHolder` 兼容；dqex 侧命名终稿为
`DataPackage`/`DataEntry`，进入 library.md 契约后冻结）：

```json
{
  "db": "mydb",
  "datas": [
    { "type": 0, "table": "t1", "sql": "CREATE TABLE ..." },
    { "type": 1, "table": "t1", "pk": ["id"], "data": [{ "id": "1", "...": "..." }] }
  ],
  "index": { "t1": 0 }
}
```

要点：
- `type`: 0=建表 / 1=按 PK 数据 / 2=DDL 变更（含回滚 SQL 对）
- 数据按 PK 组织，是"精确回滚"与"幂等 upsert 导入"的前提；纯 SQL 导出无行级语义，两者互为补充
- 引擎复用现有导出编排（库/表选择、外键排序、进度、zip、Contributor），仅"写文件层"换为 DataPackage 累积
- **无主键表策略（v0.2 定义）**：json 导出/导入跳过该表数据并在任务日志告警
  （结构仍导出）；后续版本评估唯一键回退。精确回滚不承诺覆盖无 PK 表。
- `SchemaOnly`/`DataOnly`/`Conditions` 与 Format 组合语义：SchemaOnly → 仅
  type=0 条目；DataOnly → 仅 type=1/2；Conditions 过滤对 type=1 数据生效。

### 7.2 能力二：导入精确回滚

```go
// ImportOptions 新增
Rollback bool   // true 时生成行级回滚 SQL 产物
// ⚠️ 破坏性变更（v0.2，v1 冻结前允许）：RunImport 返回值由 error 改为
// (*ImportResult, error)，ImportResult.RollbackRef 为回滚产物 ArtifactRef。
// v1 冻结前统一公布破坏性变更清单，宿主一次性迁移。
```

| 导入类型 | 失败回滚 | 成功后精确回滚 |
|---|---|---|
| `.json` 数据导入 | 整体事务自动回滚 | ✅ ApplyDataPackage 引擎先查旧值生成行级回滚（旧值 REPLACE + 建表 DROP） |
| `.sql` 导入 | 可选单事务模式（失败自动回滚）；**边界**：事务粒度为 per-db per-file（多库 zip 跨文件/跨连接不可能整包单事务） | ❌ 盲执行无行级语义，需要精确回滚请用 `.json` 格式 |

> **时点语义警告**：回滚 SQL 基于导入时刻的旧值快照。若导入后其他写入者
> 修改了同一行，再执行回滚会覆盖其修改——精确回滚仅适用于"紧随导入的撤销"
> 场景，不适用于长期 revert。该约束须写入 library.md 回滚能力说明。

### 7.3 引擎下沉与业务策略外置

从 tl-env `pkg/datatable` 下沉（约 300 行，依赖仅 cydb/cyutil）：

| 下沉 dqex | 留在宿主（回调化） |
|---|---|
| `DataPackage/DataEntry`（格式契约，兼容 tl-env `DataHolder/DataStruc`） | `CamundaPreProcess`（流程版本合并） |
| `ApplyDataPackage`（事务 + 幂等 upsert + 回滚 SQL 生成，原 `UpdateDB`） | `FormDefinitionPreProcess` / `JsApiPreProcess`（版本/发布状态） |
| `LoadDataPackage`/`collectTableRows` 通用读写（去硬编码库名清单） | `TablePreProcess`（be_field_def 元数据驱动 DDL） |
| `stripSQLDBPrefix`（库名前缀清理，清单=包内库名+目标库名，替代 tl-env `stripDatabasePrefix` 硬编码） | tl-env 业务错误码、`newID` 业务 ID 格式 |
| `tryColumnRollback`（ALTER 列变更空回滚修复：执行前读原列定义生成还原 SQL；不支持方言记 `Unrollback` 告警） | `.json` 导入编排（并入 RunImport：格式判别 → 引擎 → 回滚产物） |

回调接口（与 Contributor 同构，命名终稿 `DataPreparer`）：

```go
// DataPreparer 数据前置处理器：按目标库名注册，导入前对 DataPackage 做版本合并/冲突处理，
// 可修改包内容（返回修改后的包）
type DataPreparer func(ctx context.Context, req DataPrepareRequest) (context.Context, *DataPackage, error)

// Client 选项
dqex.WithDataPreparers(map[string]DataPreparer{ "camunda": camundaPreparer, "data_entity": dataEntityPreparer })
```

### 7.4 落地顺序

0. dqex v0.2（前置）：`QueryHooks`/`WithQueryHooks` 落地（接口位已预留，
   `OnQuery(ctx, connKey, stmtDigest, costMs, rowsAffected)`），阶段一审计桥接依赖它。
1. dqex v0.2：下沉 `DataHolder/UpdateDB` → `DataPackage`/`ApplyDataPackage`（PreProcess 回调化为 DataPreparer、去 errcode/硬编码依赖、ModifyField 空回滚缺陷经 `tryColumnRollback` 修复、`stripDatabasePrefix` 硬编码经 `stripSQLDBPrefix` 通用化）→ `.json` 导入 + `Rollback` 选项。**已落地**。
2. dqex v0.2：`ExportOptions.Format="json"` 数据格式导出。
3. tl-env：`ImportWithJsonMode` 替换为 `RunImport(.json)` + `WithDataPreparers` 注册两个业务策略；`NewDataWriteTool` 下线。
4. 格式契约（DataPackage JSON，兼容 tl-env DataHolder 结构）写入 docs/library.md 后冻结，作为两个产品共用的数据交换格式。

## 8. 验收标准

1. 阶段一后：dbconsole 全部用例回归通过，SQL 执行路径无 tl-env 自研方言代码。
2. 阶段二后：
   - 同一 env 导出包可在任意 env 导入，业务对象（flow/panel/datatable/rule）完整往返；
   - 存量 tl-env 包仍可导入（兼容通道）；
   - 进度上报、错误码（`ErrCtbUnknown` 等）在 tl-env 日志/响应中可见。
3. 阶段三后：`#/embed/query` 内对象树、SQL 编辑、自动补全可用；iframe 跨域
   鉴权与 postMessage 事件联调通过。
4. examples/ 中贡献者示例可作为 CI 集成测试样例（后续补充 `examples/contributor`）。
5. v0.2 数据格式/回滚能力后（第 7 节）：`Format="json"` 导出 → `RunImport(.json, Rollback=true)`
   往返一致；回滚 SQL 在目标库执行后数据恢复至导入前状态（行级比对验证）；
   tl-env `NewDataWriteTool` 下线，业务 PreProcess 经回调注册后行为与原 ImportWithJsonMode 等价。
