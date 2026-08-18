# AI 辅助写 SQL 功能计划（CLI + Web）

> 文档状态：**实施中**（Phase 1 完成，Phase 2 完成，Phase 3 完成）
> 生成日期：2026-08-17
> 目标：为 CLI（`dbx sql`）与 Web（查询终端）新增"大模型动态辅助写 SQL"能力。**AI 是完全可选的增量功能：未配置大模型时入口自动隐藏，全链路零侵入，行为与现状完全一致。**
>
> **实施进度（2026-08-17）**：依赖已装（Eino v0.9.14 + openai 组件 v0.1.13 + atotto/clipboard）；`internal/llm`（2 文件）✓；`AppConfig.AI` + 掩码/保留密钥 + `Service.AIEnabled()` ✓；`service/ai.go` 会话/元数据缓存/流式编排/token 累计/进程级累计 ✓；CLI `\ai`（生成/run/explain/fix/optimize/continue/again/copy/status/config/clear/help）✓；Web `/api/ai/*`（status/usage/sessions/chat/SSE）✓；`AIPanel` 抽屉（流式 + diff 预览替换/追加 + 进程 token 展示）+ Settings AI 配置区（保存即热生效）✓；设置页左侧导航四区（通用/安全/AI/兼容）分区独立保存 ✓；未配置时入口隐藏 ✓。
> **与计划的实现差异**：① Web diff 预览用 Monaco `DiffEditor` 侧并排对比，提供「替换编辑器内容 / 追加到末尾」两种确认（计划 §6.3）；② CLI `\ai config` 为逐项引导式写入 `config.yaml`（回车保持原值、`.` 退出），`\ai copy` 用 atotto/clipboard 跨平台复制缓冲区 SQL；③ 设置页四区导航实现（通用/安全/AI/兼容），AI 区独立「保存并立即生效」，其余区「保存后重启生效」；④ 进程级 token 累计：后端 `GET /api/ai/usage`，Web 面板底部展示「进程累计」，CLI `\ai status` 展示进程级消耗；⑤ schema 注入采用「表 + 字段定义」摘要格式（非完整 DDL）：透传表/列注释（`engine.GetTableMeta`）、PII 列脱敏、单表列数上限 80、单条注释截断 60 字、按 rune 安全裁剪不产生乱码（**仅 CLI 全量注入路径保留，Web 端已改为统一工具探索，见 ⑥**）；⑥ **React Agent 统一工具探索（Phase 4）**：Web 侧所有会话统一走 agent 模式——system prompt 只注入轻量「库+表名录」+ 工具使用约束（不再注入全量字段摘要，也不按表数路由），模型通过三个只读工具（`list_databases`/`list_tables`/`get_schema`，复用 engine 元数据，无 SQL 执行能力）自主探索后生成 SQL；agent 使用**会话独立 ChatModel 实例**避免工具绑定竞争，历史含工具轮次按「组」裁剪，SSE 新增 `tool` 事件供前端展示中间态；**CLI 侧保持全量注入（不启用 agent 模式）**。

---

## 0. 现状基线（已调研）

### 0.1 可复用能力
- **CLI**：`internal/cli/sqlcmd` 交互终端，已有元命令分派（`handleMeta`）、Tab 补全、危险 SQL 检测（`checkDangerous`）、写操作确认、历史记录。
- **Web**：`WorkspaceLayout` 工作区含 SQL 编辑器 + 结果表（`QueryView`），`/api/sql/*` 已有 run/history/audit/ddl/workspace 接口。
- **元数据**：`engine.GetTables` / `GetTableInfo`（列白名单）/ `GetObjectDDL` 可直接作为 LLM 上下文；CLI 侧 `sess.tableCache` / `refreshMetadata` 已有。
- **配置**：`config.yaml`（`AppConfig`）+ `SettingsView` 页面 + GET/PUT `/api/config`。
- **安全链路**：SQL 执行统一走 `classifySQL` + `checkDangerous` + 写操作二次确认，生成 SQL 必须复用，不得绕过。

### 0.2 关键缺口
- 项目目前**无任何 LLM 集成**，需从零引入框架与配置段。
- 后端无 AI 相关 API；前端无 AI 面板；CLI 无 AI 元命令。

---

## 1. 总体架构

```
┌─────────────┐   ┌──────────────────────────────┐   ┌────────────────┐
│ CLI (sqlcmd)│   │ Web 前端 (QueryView AI 面板)   │   │ SettingsView    │
│ \ai 命令     │   │ 生成/插入/解释/修复 (SSE 流式)  │   │ AI 配置表单      │
└──────┬──────┘   └──────────────┬───────────────┘   └───────┬────────┘
       │                         │                            │
       ▼                         ▼                            ▼
│ Service 层（internal/service/ai.go：AIEnabled 判定 / 会话管理 / 上下文构建 / 调用编排）│
│  └── internal/llm 包：封装 Eino OpenAI 兼容 ChatModel（Generate / Stream）          │
│  └── engine 元数据（GetTables / GetTableInfo / GetObjectDDL）                       │
│  └── config.yaml 新增 ai: 段（或环境变量 DBX_AI_API_KEY）                            │
└──────────────────────────────────────────────────────────────────────────────┘
```

---

## 2. 技术选型：Eino 框架（组件优先最新版本）

### 2.1 选型结论
采用 **字节跳动开源的 Go LLM 框架 Eino**，原因：
1. **国内模型厂商级适配**：内置/生态提供 OpenAI 兼容 provider 组件，通义、DeepSeek、Kimi、智谱、豆包、Ollama 等统一接入，各家协议差异（SSE 流式、消息格式）由框架封装，避免自写细节错误。
2. **官方维护**：`cloudwego` 组织维护，对标 LangChain，活跃更新。
3. **按需取用**：本项目只使用 `ChatModel` + 多轮消息 + 流式/非流式，不引入 Workflow / Agent 编排，控制依赖重量。

### 2.2 依赖与版本（写文档时最新，安装时以 `go get` 解析的最新为准）
| 模块 | 最新版本（2026-08 确认） | 用途 |
|---|---|---|
| `github.com/cloudwego/eino` | **v0.9.14** | 核心：`schema.Message`、`ChatModel` 接口 |
| `github.com/cloudwego/eino-ext/components/model/openai` | **v0.1.13** | OpenAI 兼容 ChatModel 实现（`openai.NewChatModel`） |

> 约定：`go.mod` 锁定安装时解析到的最新稳定版；后续升级走 `go get -u`。

### 2.3 关键 API 用法（计划采用，字段以安装版 pkg.go.dev 为准）
```go
import (
    "github.com/cloudwego/eino-ext/components/model/openai"
    "github.com/cloudwego/eino/schema"
)

cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
    BaseURL:    "https://api.deepseek.com/v1", // 可切换 qwen/kimi/ollama 等
    APIKey:     cfg.APIKey,
    Model:      cfg.Model,       // deepseek-chat / qwen-plus 等
    Temperature: ptr(0.2),
    MaxTokens:  ptr(int64(2048)),
    Timeout:    ptr(60 * time.Second),
})

// 非流式（CLI 用）
resp, err := cm.Generate(ctx, msgs)

// 流式（Web 用，SSE 逐 token 转发）
sr, err := cm.Stream(ctx, msgs)
for { m, err := sr.Recv(); if errors.Is(err, io.EOF) { break }; /* 逐段写前端 */ }

// 消息构造（多轮会话）
msgs := []*schema.Message{
    schema.SystemMessage(systemPrompt),
    schema.UserMessage(userPrompt),
    // ...历史轮次...
}
```

---

## 3. 配置设计（AppConfig 新增 `ai:` 段）

```yaml
ai:
  enabled: false            # 显式开关（默认 false，配合下方自动判定）
  provider: openai          # 标识服务商，仅用于展示/文档
  base_url: https://api.deepseek.com/v1   # OpenAI 兼容端点
  api_key: ""               # 支持环境变量 DBX_AI_API_KEY 覆盖；Web 回显掩码
  model: deepseek-chat
  temperature: 0.2          # 生成 SQL 建议偏低，保证确定性
  max_tokens: 2048
  timeout: 60s
  # 提示词（可在 SettingsView 编辑）：
  system_prompt: ""         # 可选，留空 = 用内置默认模板；填写 = 覆盖默认
```

### 提示词配置（可通过 Settings 修改）

**原则：默认零配置可用，但全部提示词都允许用户覆盖。**

| 配置项 | 默认行为 | 用户覆盖方式（SettingsView） |
|---|---|---|
| `ai.system_prompt` | 内置默认模板（含方言声明 + 安全约束 + 任务指令骨架） | 文本框编辑，**支持占位符**，提供「恢复默认」按钮 |
| 占位符 | — | `{dialect}`（数据库方言）、`{schema}`（表结构上下文，按需注入）由 service 渲染后发往模型 |

**占位符机制（关键）**：`system_prompt` 中出现的 `{dialect}`、`{schema}` 由后端 `llm/prompt.go` 渲染成实际值再发送。好处：
- 用户可自定义角色设定、安全措辞、输出格式要求，而上下文注入位置固定可控；
- 用户自定义模板缺失占位符时，后端仍按需追加 `schema` 段，保证表结构上下文不丢（**占位符仅用于控制位置，不用于开关**）；
- 内置默认模板同样使用占位符，与用户模板渲染路径完全一致。

内置默认模板（`llm/prompt.go` 常量，用户留空时使用）：
```
你是一个资深 {dialect} DBA 与 SQL 专家。你的任务是根据用户需求生成/解释/修复 SQL。
约束：
1. 只输出 SQL，不输出多余解释（除特别要求外）。
2. 禁止生成 DROP TABLE、TRUNCATE、DELETE 无 WHERE 等危险语句；用户提出此类需求必须拒绝并说明。
3. 仅使用下方提供的表结构（schema）中的表与列，不得臆造不存在的对象。
4. 字段名/表名用反引号或正确方言标识符，字符串字面量用单引号。
5. 不确定时给出说明，不编造。

{schema}
```

> 任务级指令（"生成一条查询……" / "解释以下 SQL……" / "修复报错……"）由 service 拼接为用户消息，不属于 system_prompt，避免用户误改破坏功能。

### 判定规则（唯一事实来源，`Service.AIEnabled()`）
`enabled=true`（或 `enabled` 未显式设置时按自动判定）**且** `base_url` 非空 **且** `api_key` 非空（含环境变量 `DBX_AI_API_KEY` 兜底）。

> 自动判定：`enabled` 缺省（`*bool` 为 nil）时，以 `base_url + api_key` 是否齐全为准；显式 `enabled: false` 强制关闭。保存时由前端/CLI 配置表单统一写全字段。

---

## 4. 后端设计

### 4.1 `internal/llm` 包（对 Eino 的薄封装，全端共用，**共 2 个文件，不依赖 engine**）
| 文件 | 职责 |
|---|---|
| `llm.go` | `NewClient(cfg AIConfig)` 创建 Eino ChatModel；`Chat(msgs) (content, Usage)` 非流式（Usage 取自响应 `ResponseMeta.Usage`）；`ChatStream(msgs, cb)` 流式回调，结束回调携带流尾 Usage（无则记 0，不自数 token）；错误归一化（超时/网络/401/限流） |
| `prompt.go` | 内置默认 system prompt（含 `{dialect}`/`{schema}` 占位符）；`RenderSystemPrompt(custom, dialect, schema)` 渲染用户自定义或默认模板（缺失 `{schema}` 时追加 schema 段）；`BuildSchemaText(tables)` 表/列转义 + 敏感列排除 + token 裁剪；任务级指令拼接；`ExtractSQL` 剥代码围栏/注释 |

> 依赖方向：`cli → service/ai.go → {llm, engine}`，`internal/llm` 纯客户端层不依赖 `engine`。元数据拉取（`GetObjectDDL`/`GetTableColumns`）在 `service/ai.go`，格式化（`BuildSchemaText`）在 `llm/prompt.go`。

### 4.2 `internal/service/ai.go`（编排与状态）
| 能力 | 说明 |
|---|---|
| `AIEnabled() bool` | 读配置 + 环境变量判定（见 §3） |
| `AIConfig() (masked, raw)` | Web 回显用掩码版；CLI/调用用 raw |
| 会话管理 | 内存会话表：`sessionID → {connId, db, []*schema.Message, Usage}`，**会话绑定 (connId, db)**，切换连接/库自动开新会话，避免 schema 上下文错乱；`sync.Mutex` 保护；LRU 上限（默认 20 会话 × 50 轮，超限丢最老消息保留 system+最近 N 轮） |
| Token 统计 | 会话结构内 `Usage` 累计字段：`{prompt, completion, total}` 逐轮累加；CLI `\ai status` 显示「会话已消耗 X / 进程累计 Y」，Web 抽屉底部小字显示；**不落盘、不核算成本金额、不自数流式 token**（以流尾 usage 为准，无则记 0） |
| 元数据缓存 | `(connId, db) → {tables, DDL}` 按库缓存，TTL 60s；避免每轮对话重复 `GetObjectDDL` introspect；连接断开时失效 |
| 上下文构建 | 入参：connId、db、表清单（可选，空=自动取当前库全部表 DDL 并裁剪）、问题 → 组装 messages；方言从 engine 连接元数据获取注入 `{dialect}` |
| 输出后处理 | 剥除代码围栏（```sql ... ```）、非 SQL 噪声；**先剥离 SQL 注释再交给 checkDangerous**；仍走现有执行安全链路 |

### 4.3 Web API（`internal/web`，统一 `/api/ai/*` 命名空间）
| 接口 | 类型 | 说明 |
|---|---|---|
| `GET /api/ai/status` | JSON | `{ enabled, provider, model, reason? }`；未配置时 `enabled=false` + 原因（前端据此隐藏入口）；**不含 api_key** |
| `POST /api/ai/generate` | **SSE 流式** | 参数：`connId, db, prompt, tables[], sessionID?, action(generate\|explain\|fix\|optimize)`；逐 token 下发 `data: {delta}`，结束发 `[DONE]`，错误 `event: error` |
| `POST /api/ai/explain` | JSON 非流式 | 参数：`sql, connId, db` → 返回意图 + 风险提示 |
| `POST /api/ai/fix` | JSON 非流式 | 参数：`sql, error, connId, db` → 返回修复后 SQL 与说明 |
| `POST /api/ai/optimize` | JSON 非流式 | 参数：`sql, connId, db` → 返回改写 SQL + 索引/性能建议（对应"优化"能力） |
| `DELETE /api/ai/session` | JSON | 参数：`sessionID?`（空=全部）；清空会话 |

> SSE 响应：`Content-Type: text/event-stream`，`X-Accel-Buffering: no`；**逐 token 小批量缓冲转发**（≈50ms 或 16 字符，减少 HTTP 包数，保持打字机观感）；客户端断开 `ctx` 取消即终止上游调用（不浪费 token）。
> 请求体限制：`generate/explain/fix/optimize` 均限制 body ≤ 1MB，`prompt` 单字段 ≤ 64KB，防滥用。

---

## 5. CLI 设计（`dbx sql` 内新增元命令，非流式整块输出）

### 生成后进入缓冲区，不弹多选菜单

`\ai <自然语言>` 生成 SQL 后**写入待执行缓冲区**（显示为可编辑预览，同现有"写操作确认"交互），用户可编辑后回车执行，或使用轻量子命令。**避免 `[E]执行 [Y]复制 [R]重新生成 [C]继续 [q]退出` 这种心智负担重的多选菜单**——动作拆成独立子命令，各司其职：

| 命令 | 行为 |
|---|---|
| `\ai <自然语言>` | 携带当前连接+库表结构上下文调 LLM，整块输出 SQL 并写入缓冲区（可编辑后 `\g` 执行）；写操作仍需确认 |
| `\ai continue <追问>` / `\ai c` | 基于上一轮会话继续，结果追加进缓冲区 |
| `\ai explain [SQL]` | 解释当前缓冲区/`lastSQL` 或指定 SQL 的意图/风险 |
| `\ai fix <报错>` | 带上失败 SQL + 报错让模型修复，结果进缓冲区 |
| `\ai copy` | 复制缓冲区 SQL 到剪贴板 |
| `\ai clear` / `\ai status` | 清空会话 / 查看配置模型、可用状态、**进程累计 tokens** |
| `\ai config` | 引导式设置 base_url / api_key / model，写入 config.yaml |
| `\ai help` | AI 子命令帮助（仅当 AI 可用时显示） |

> 命令精简说明（参考 GitHub Copilot CLI / OpenAI Codex / Claude Code 的命令集）：
> 移除 `\ai run`（与 `\ai` + `\g` 执行链路冗余）、`\ai again`（直接重输需求即可）、`\ai optimize`（低频，可用 `\ai continue 请优化这段 SQL` 覆盖），并收敛别名（`\ai clear`、`\ai c`）。

实现要点：复用 `handleMeta` 分派 + `sess.tableCache`；缓冲区复用现有写操作确认的编辑链路；生成结果必须过 `classifySQL` + `checkDangerous` + 写操作确认，不绕过安全链路。

---

## 6. Web 前端设计（交互体验重点）

### 6.1 布局：编辑器右侧 AI 抽屉（非底部面板）
- **入口**：`QueryView` 编辑器**右侧可折叠 AI 抽屉**（宽度 ~380px，可拖拽）；工具栏 AI 按钮仅当 `GET /api/ai/status` 返回 `enabled=true` 时渲染。
- 选右侧抽屉而非底部面板：生成 SQL 要"插入编辑器"，用户在编辑器与面板间视线平行移动更自然；底部面板会挤压结果表高度。
- **快捷键**：`Cmd/Ctrl+Enter` 发送 prompt、`Esc` 停止生成、`Cmd/Ctrl+K` 聚焦 AI 输入框。

### 6.2 会话与上下文
- 顶部会话栏：「新建会话」+「清空」按钮；会话自动跟随当前连接/库（切换连接自动新会话）。
- 上下文控件："包含表结构"开关 + 表多选（默认收起，展开可勾选，最多选 10 张）；空状态显示引导文案 + 示例 prompt 占位符（如"近 30 天销量 Top10 商品"），降低使用门槛。

### 6.3 生成交互：插入前 diff 预览（杀手级体验）
- ① 生成 SQL 流式显示在抽屉中（打字机效果 + **「停止生成」按钮**，`AbortController` 中止并提示已用 token 情况）。
- ② 「插入编辑器」→ 打开 **diff 预览**（生成结果 vs 当前编辑器内容，Ace 内置 diff 或并排高亮增删），用户确认后替换选中/追加；**不直接覆盖**，避免盲改。
- ③ 「执行」→ 直接把生成 SQL 交给现有 `POST /api/sql/run`（写操作仍需二次确认），**不绕过安全链路**。
- ④ 「解释」/「修复」/「优化」按钮作用于当前编辑器 SQL，结果展示在抽屉中，「解释」可复制。

### 6.4 流式与状态
- `fetch` + `ReadableStream` 解析 SSE，打字机效果；错误（`event: error`）**面板内联显示**，不用全局 toast 打断。
- `useAIStatus()` hook 统一拉取；`enabled=false` 时 AI 抽屉代码不 mount（惰性加载）。

### 6.5 其他
- 新增 `web/src/api/ai.ts` + `types` 同步。
- **设置页分区重构**：`SettingsView` 由单一纵向滚动改为**左侧导航 + 右侧内容**（仿 Navicat/VSCode），四区：**通用**（数据目录）、**数据库**（兼容选项等）、**安全**（访问白名单，预留令牌认证）、**AI 助手**（模型配置 + 提示词）。**分区独立保存**：通用/数据库/安全区保存提示"需重启生效"，AI 区保存提示"立即生效"（热生效），避免"一个大保存按钮 + 全局重启提示"在 AI 加入后产生误导。

---

## 7. 安全设计（关键）

1. **生成 ≠ 执行**：AI 只起草，执行一律走 `handleSQLRun` transform + `checkDangerous` + 写操作二次确认。
2. **API Key 不出本机**：仅写本地 `config.yaml`（0600），Web 回显掩码，支持环境变量注入；`status` 接口不回传 api_key。
3. **Prompt 硬约束**：system prompt 明确"只输出 SQL、禁 DROP/TRUNCATE/危险语句、无权限必须拒绝"，输出端二次过滤（代码围栏剥离 + 危险词兜底）。
4. **间接 Prompt 注入防护**：表名/列名/注释可能被恶意构造（如 `CREATE TABLE "drop users --"`）——注入 schema 时对表名/列名统一加正确方言标识符引用；**生成 SQL 先剥离注释（`--`、`/* */`）再过 `checkDangerous`**，防注释绕过危险检测。
5. **上下文最小化 + 可选脱敏**：默认仅注入表清单 + 涉及表 DDL；采样列值需显式开启；敏感列名（`password/token/secret/credit_card/phone/id_card/email` 等 PII 关键词）默认排除。
6. **请求防护**：SSE/JSON 接口 body ≤ 1MB、`prompt` ≤ 64KB；会话 LRU 上限防内存膨胀；`base_url` 属管理员配置（单机工具），多用户部署时标注 SSRF 风险。
7. **审计**：AI 生成/执行记录可写入现有 SQL 审计日志（不含 prompt 全文与 api_key，避免敏感信息落盘）。

---

## 8. 未配置时的降级机制（贯穿全链路）

| 场景 | 行为 |
|---|---|
| Web 面板/工具栏 | `enabled=false` 时**完全不渲染**，无任何 AI 痕迹 |
| `SettingsView` 配置区块 | **始终可见**（属于设置范畴，否则无法开启功能） |
| `/api/sql/ai/*` 接口 | 返回明确错误 `AI 功能未配置`，防绕过前端直接调用 |
| CLI `\h` 帮助 | AI 元命令条目**不显示** |
| CLI 输入 `\ai xxx` | 单行提示如何配置，不打断现有流程 |
| 运行时 | 不初始化 Eino 客户端、不发任何请求、无后台 goroutine；现有功能零改动 |
| 配置热生效 | `SettingsView` 保存后状态实时翻转，刷新即生效，无需重启（AI 配置做成运行时生效） |

---

## 9. 落地步骤

### Phase 1（基础可用：CLI 先行打通）
1. `go get` Eino + openai 组件；`internal/llm` 包（client/prompt/context）。
2. `AppConfig.AI` + 配置加载/掩码 + `Service.AIEnabled()`。
3. CLI `\ai` 生成/执行 + `\ai status/config`（非流式）。
4. `GET /api/ai/status` 接口。

### Phase 2（Web 接入）
5. `/api/sql/ai/generate`（SSE）+ `explain` + `fix` + `DELETE session`。
6. 前端 `types` + `api/ai.ts` + `useAIStatus`。
7. `QueryView` AI 面板（生成/插入/解释/修复，流式）。

### Phase 3（打磨）
8. **设置页分区重构**：`SettingsView` 改为左侧导航 + 右侧内容四区（通用/数据库/安全/AI 助手），**分区独立保存**（通用/数据库/安全区"需重启生效"，AI 区"立即生效"）。
9. **AI 配置表单**：AI 助手区内实现（含 `system_prompt` 提示词编辑，textarea + 占位符说明 + 「恢复默认」）+ 运行时热生效。字段：`provider`（展示）、`base_url`、`api_key`（掩码/留空表示不变）、`model`、`temperature`、`max_tokens`、`timeout`、`system_prompt`；仅当 `ai.enabled=true` 且配置齐全时显示提示词编辑区，未启用时隐藏避免误导。
10. 自动表结构上下文 + 会话记忆（多轮）+ LRU 上限。
11. Prompt 调优、模型切换、审计完善。

### Phase 4（React Agent 统一工具探索）
12. `internal/llm/agent.go`：Eino `react.Agent` 封装——`NewReactAgent`（会话独立 ChatModel + 幂等 MessageModifier + MaxStep 16）、`Stream`（用 `GetMessageStreams` 主动 drain 流式消息收集历史 + usage 累加 + tool 事件回调）。**注意**：流式模式必须用 `GetMessageStreams`（而非 `GetMessages`）并逐流 drain，否则工具结果流（`Copy(2)` 扇出）无人消费会导致父流背压阻塞、agent 卡死（前端一直转圈）。
13. `service/ai.go`：`AISession` 加 `Agent/Sys/ToolSink`；**所有会话统一走 agent 模式**——`AINewSession` 只注入轻量「库+表名录」+ 工具使用约束（不再注入全量字段摘要，也不按表数路由）；新增三个只读工具 `list_databases`/`list_tables`/`get_schema`（复用 engine 元数据，纯只读）；`aiAgentChat` 统一对话入口（工具事件回调 + 历史持久化 + usage 累计）；`trimMessages` 按「组」裁剪兼容工具轮次。
14. Web：SSE 新增 `tool` 事件；`aiChatStream` 加 `onTool`；`AIPanel` 展示「正在查询 xxx 表结构…」中间态。
15. 说明：**CLI `\ai` 不启用 agent 模式**（保持全量注入）；模型不支持工具调用时报错提示，后续可加 `ai.agent_enabled` 开关兜底。
16. **会话失效透明重建**：后端 `AIChatStreamWithFallback` 检测「会话不存在」时，用请求携带的 `connId/db/history` 透明重建并回放历史（保留多轮上下文），SSE `session` 事件回传新会话 ID 供前端复用；前端无需感知会话生命周期（职责后移，消除前端正则匹配错误类型的脆弱逻辑）。
17. **历史压缩机制（分层，第 1 层已落地）**：`trimMessages` 分两级——① 字符预算 `aiMaxHistoryChars`（约 6K token）超限时按轮次裁剪，**优先丢不含 SQL 的旧轮次、保留含 SQL 的关键轮次**；② 条数上限 `aiMaxMessages` 兜底。**预留演进**：第 2 层为「大模型摘要压缩」（历史过长时用一次 LLM 调用把旧历史压缩成摘要，摘要 prompt 定制保留表名/字段/业务约束/SQL 片段）；第 3 层为「滚动摘要 + 滑动窗口」混合（业界主流终态）。当前未遇到膨胀问题，仅落地零成本的软裁剪作为地基。

---

## 10. 审核（风险与对策）

### 10.1 技术风险
| 风险 | 等级 | 对策 |
|---|---|---|
| Eino v0.9 仍处 0.x，API 可能变动 | 低 | 实际为 `go.mod` **indirect 依赖且已固定 v0.9.14**，AI 为可选模块（未配置不初始化）；升级仅影响 AI 链路，不波及导入/导出/迁移/查询核心。隔离在 `internal/llm` 单点封装，版本锁定 go.mod，升级集中评估 |
| openai 组件对个别国内服务 SSE 差异 | 低 | 厂商级适配已覆盖主流（deepseek/qwen/kimi/ollama）；异常回落非流式 |
| 流式长 SQL 客户端断开浪费 token | 中 | 服务端 `ctx` 取消传播到上游调用，断开即终止 |
| 配置错误（baseURL 不可达/key 无效） | 低 | `status` 返回 reason；`\ai status` 提供诊断信息；超时兜底 |
| 会话内存增长 | 低 | LRU 上限 20 会话 × 50 轮，超限自动裁剪 |
| API Key 泄露 | 高 | config.yaml 0600 + Web 掩码 + 环境变量注入；审计不落 key |
| Prompt 注入/危险 SQL | 高 | system prompt 硬约束 + 输出端二次过滤 + 执行端不绕过现有安全链路 |

### 10.2 业务与体验风险
| 风险 | 对策 |
|---|---|
| 未配置时误显示入口 | `useAIStatus` 统一 gate + 后端接口兜底报错 |
| 生成 SQL 质量差/方言错 | temperature 低 + 注入「表 + 字段定义」摘要（含注释）+ 支持 `\ai continue` 多轮修正 |
| 库表众多注入不全/跨库多表关联 | Web 统一走 React Agent：只注入「库+表名录」，模型经只读工具按需探索后生成；可多轮并发调用工具跨库探索 |
| 模型不支持工具调用 | agent 模式错误提示明确（含排查建议）；后续加 `ai.agent_enabled` 开关强制回退全量注入 |
| CLI 与 Web 体验不一致 | 共用 `internal/llm` + `service/ai.go` 编排，前端仅做展示层差异 |

### 10.3 审核结论
**通过，可执行**。规模适中（1 个新依赖包 + 1 个 service 文件 + 少量 API/前端改动），风险集中在 Eino 0.x 稳定性与安全两条线，均已单点隔离 + 配置门控。建议按 Phase 1 → 2 → 3 顺序落地，Phase 1 完成后先在小范围验证 prompt 效果再继续。

### 10.4 交互体验要点（评审新增，实现时必须满足）
- Web：右侧 AI 抽屉 + **diff 预览后插入** + 停止生成按钮 + 快捷键 + 面板内联错误，不直接覆盖编辑器
- CLI：生成 SQL 进缓冲区可编辑执行，子命令拆分动作（`run/copy/again/continue`），**不做多选菜单**
- 未配置时入口零痕迹，配置后入口即时出现（热生效）
