# Agent / 脚本集成指南 / Agent Integration Guide

> dqex 如何接入自动化流程与 AI 智能体。
> 面向对象：需要在 CI 流水线、定时任务或 AI Agent 中操作数据库的工程师。

English below. 中文在前，因为命令与参数以实际实现为准。

---

## 一、先看清能力边界

这一点最重要，别踩坑：

| 命令 | `--json` 输出 | 说明 |
|---|---|---|
| `dqex sql` | ✅ | **唯一原生支持 JSON 的命令** |
| `dqex export` / `import` | ❌ | 结果输出到文件与 stdout 文本 |
| `dqex migrate` / `compare` | ❌ | 文本摘要 |
| `dqex snapshot` / `dictionary` | ❌ | 文本摘要 |
| `dqex conn` / `task` / `history` | ❌ | 文本列表 |

**结论**：让 Agent 消费结构化结果时，走 `dqex sql --json`；
其他命令若需被 Agent 解析，建议包一层 `jq` 或在自己的脚本里转成结构化输出。

所有子命令均支持 `--help`，参数以实际实现为准：
`dqex <command> --help`。

---

## 二、`dqex sql --json`：Agent 的主要入口

### 基本用法

```sh
dqex sql -c prod --json "SELECT id, name FROM users LIMIT 10"
```

- `--json` 输出结构化结果，便于 Agent 或脚本解析
- `--max-rows` 控制返回行数（默认 1000），避免一次拉爆上下文
- `--no-color` 关闭 ANSI 颜色（管道/日志场景建议加）

### 写操作默认被拦截

```sh
dqex sql -c prod --json "UPDATE orders SET status='paid' WHERE id=42"
```

在 `--json` 模式下，写操作需要显式加 `--allow-write`：

```sh
dqex sql -c prod --json --allow-write "UPDATE orders SET status='paid' WHERE id=42"
```

> ⚠️ **`--allow-write` 只应出现在你完全确定查询范围的脚本里。**
> 让 Agent 自动拼接写操作时，务必先用只读查询确认影响行数，再决定是否放行。

### 推荐的 Agent 提示词约束

在给 Agent 的系统提示里明确写死这条规则，效果比依赖默认行为可靠得多：

```text
使用 dqex 执行数据库操作时遵守以下规则：
1. 只读查询（SELECT / SHOW / DESC）可直接执行。
2. 任何写操作（INSERT / UPDATE / DELETE / DDL）必须：
   a. 先用同条件 SELECT COUNT(*) 确认影响行数；
   b. 把 SQL 与预期影响行数一并展示给我；
   c. 得到确认后才加 --allow-write 执行。
3. 每次查询前先用 list_tables / get_schema 确认真实表结构，
   不要凭记忆假设字段名。
4. 所有命令加 --max-rows 限制返回量。
```

---

## 三、脚本化：把 dqex 放进 CI

### 典型流水线场景

```sh
#!/usr/bin/env bash
set -euo pipefail

export DBX_LANG=zh          # CLI 输出语言（zh / en）
DQEX="dqex"

# 1. 建快照（部署前基线）
$DQEX snapshot create -c prod -n "before-deploy-${GIT_SHA::8}"

# 2. 备份
$DQEX exp orders -s prod -o "/backup/${GIT_SHA::8}.sql.gz" \
    --table-cond "orders:created_at >= '2026-01-01'"

# 3. 数据字典（合规审计交付物）
$DQEX dict orders -s prod -o "/audit/data_dict_${DATE}.xlsx"
```

### 连接复用

连接保存一次即可随处引用（完整参数见 `dqex conn add --help`）：

```sh
dqex conn add --name prod --type mysql --host 10.0.0.5 --port 3306 \
              --un app --pw "$DB_PASSWORD" --env prod

# 之后用 -c prod 引用，无需重复输入凭据
dqex sql -c prod --json "SELECT 1"
```

> 密码建议走环境变量或密钥管理，不要写进 shell 历史。

### 端口冲突怎么办

默认端口 8181 被占用时：

```sh
dqex --port 8282        # 换端口启动 Web 界面
dqex stop               # 终止正在运行的 dqex 进程
```

更多见 [FAQ](FAQ.md)。

---

## 四、隔离网环境下的接入

dqex 本身不依赖网络，但 AI 功能需要能访问你的模型服务。
在完全隔离的环境中，AI 入口默认不渲染，其他功能全部照常工作：

- 离线 SQL 模板库：交互式终端内 `\template top_n orders amount 10`
- AI 启用需三项齐全：`config.yaml` 的 `ai` 段中 `base_url` 与 `api_key` 非空，
  且 `enabled` 不为 `false`（`api_key` 可由环境变量 `DBX_AI_API_KEY` 提供）

详见 [OFFLINE_OPERATIONS_GUIDE.md](OFFLINE_OPERATIONS_GUIDE.md) 与 [FAQ](FAQ.md)。

---

## 五、给库/Agent 封装时的小建议

- **优先用短名**：`conn add` 时加 `--short-name`，命令行里 `-c` 可用短名，减少拼写错误
- **任务复用**：高频流程用 `dqex task save` 存成配置，之后 `--task <ID>` 一键执行
- **留痕**：`dqex history list` 是执行记录，故障排查与审计都靠它
- **结构化优先**：能让 Agent 消费的一律加 `--json` + `--max-rows`，减少人工解读

---

## 六、English

### Capability boundary (read this first)

`dqex sql` is the **only** command with native JSON output. All other commands
(`export`, `import`, `migrate`, `compare`, `snapshot`, `dictionary`, `conn`, `task`,
`history`) print human-readable text — wrap them with `jq` or your own parsing layer
if an agent needs structured results.

### JSON output

```sh
dqex sql -c prod --json "SELECT id, name FROM users LIMIT 10"
```

- `--json` — structured output for agents and scripts
- `--max-rows` — cap returned rows (default 1000)
- `--no-color` — disable ANSI colors (recommended when piping or logging)
- `--allow-write` — required to execute writes in `--json` mode

⚠️ Only pass `--allow-write` in scripts where you fully control the query.
When an Agent composes write statements, first confirm the blast radius with a
read-only `COUNT(*)`, then get explicit confirmation before allowing the write.

### Scripting

```sh
dqex snapshot create -c prod -n "before-deploy"
dqex exp orders -s prod -o backup.sql.gz --table-cond "orders:created_at >= '2026-01-01'"
dqex dict orders -s prod -o data_dict.xlsx
dqex sql -c prod --json "SELECT 1"
```

Connections are saved once and referenced with `-c <name>`:

```sh
dqex conn add --name prod --type mysql --host 10.0.0.5 --port 3306 \
              --un app --pw "$DB_PASSWORD" --env prod
```

Port conflict: `dqex --port 8282`, or stop a running instance with `dqex stop`.

See [FAQ](FAQ.md) for more.