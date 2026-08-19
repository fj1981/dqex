# dbx sql — SQL 交互终端设计文档

## 1. 概述

`dbx sql` 是一个现代化的数据库 SQL 交互终端，内置于 `dbx` CLI 中。用户可以通过交互式 REPL 或单次执行模式连接 MySQL/PostgreSQL/Oracle 数据库并执行 SQL，支持人性化的表格渲染和智能体友好的 JSON 输出。

**核心理念**：让熟悉 MySQL 的用户可以用 MySQL 语法操作 PostgreSQL/Oracle，底层由 `cydb` 的 `preProcess` 自动完成方言翻译。

> ⚠️ **实施偏差（2026-08-19 核实）**：本文档为设计稿，其中关于 `cydb.preProcess` 自动方言翻译的描述**未在实现中落地**——SQL 终端按目标数据库方言原生执行（自动补 LIMIT 为字符串拼接，Oracle 需手写 `FETCH FIRST`），不提供 MySQL→PG/Oracle 语法翻译。实际能力以 [CLI.md](../CLI.md) 为准。

---

## 2. 使用方式

### 2.1 交互式终端（REPL）

```bash
# 用已保存连接（支持 ID、名称或短名）
dbx sql -c "生产库"         # 连接名称
dbx sql -c prod              # 短名
dbx sql -c conn_a1b2c3      # 连接 ID

# 连接 PostgreSQL
dbx sql -c pg-prod

# 连接 Oracle
dbx sql -c oracle-dev
```

进入后显示提示符：

```
dbx (mysql  @ 10.0.0.1:3306/mydb) >
dbx (pg     @ 10.0.0.2:5432/mydb) >
dbx (oracle @ 10.0.0.3:1521/XE)   >
```

> 提示符自动显示连接数据库的类型、地址和库名，MySQL 语法在 PG/Oracle 上自动翻译，用户无需感知差异。

### 2.2 单次执行模式

```bash
# 执行单条 SQL（表格输出）
dbx sql -c prod -e "SELECT * FROM users LIMIT 5"

# JSON 输出（智能体/脚本消费）
dbx sql -c prod --json "SELECT id, name FROM users"

# 从 stdin 读取
echo "SELECT COUNT(*) FROM orders" | dbx sql -c prod --json

# 从文件执行
dbx sql -c prod -f query.sql
```

### 2.3 管道友好（非 TTY 自动降级）

```bash
# 管道场景自动使用纯文本表格（无 ANSI 颜色）
dbx sql -c prod -e "SELECT * FROM users" | grep "admin"

# 脚本中获取 JSON 结果
result=$(dbx sql -c prod --json "SELECT COUNT(*) FROM users")
echo "$result" | jq '.rows[0][0]'
```

### 2.4 内联连接（无需预先保存）

```bash
dbx sql --type mysql --host 10.0.0.1 --port 3306 --un root --pw '${DB_PASSWORD}' --db mydb
```

---

## 3. 命令参数

| flag | 短参 | 说明 | 默认值 |
|---|---|---|---|
| `--conn` | `-c` | 已保存连接（支持 ID、名称或短名） | — |
| `--type` | — | 数据库类型 (mysql/postgresql/oracle) | — |
| `--host` | `-h` | 主机 | — |
| `--port` | `-P` | 端口 | — |
| `--un` | `-u` | 用户名 | — |
| `--pw` | `-p` | 密码 | — |
| `--db` | `-D` | 数据库名 | — |
| `--subtype` | — | 数据库产品 (oceanbase/gaussdb/dameng) | — |
| `--execute` | `-e` | 执行 SQL 后退出 | — |
| `--file` | `-f` | 从文件读取 SQL 执行 | — |
| `--json` | — | JSON 格式输出 | false |
| `--timeout` | — | 查询超时 | 30s |
| `--max-rows` | — | 最大返回行数 | 1000 |
| `--allow-write` | — | `--json` / `-f` 模式下允许执行写操作 | false |
| `--ssl-ca` | — | TLS CA 证书路径 | — |
| `--no-color` | — | 禁用颜色输出（等价于 `NO_COLOR=1`） | false |

---

## 4. 交互式终端特性

### 4.1 终端 UI 设计

```
┌─ dbx (pg @ 10.0.0.2:5432/mydb) ──────────────────────────────┐
│                                                                │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │  id │ name     │ email              │ created_at         │  │
│  ├─────┼──────────┼────────────────────┼────────────────────┤  │
│  │   1 │ 张三     │ zhangsan@test.com  │ 2024-01-15 10:30   │  │
│  │   2 │ 李四     │ lisi@test.com      │ 2024-02-20 14:22   │  │
│  │   3 │ 王五     │ (NULL)             │ 2024-03-10 09:15   │  │
│  └──────────────────────────────────────────────────────────┘  │
│  3 rows in set (0.003 sec)                                     │
│                                                                │
│  dbx > SELECT id, name, email FROM users LIMIT 3;              │
│        ^ 光标位置                                               │
└────────────────────────────────────────────────────────────────┘
```

**关键交互元素**：

| 元素 | 说明 |
|---|---|
| **顶栏** | 显示连接类型 + 地址 + 当前库名，不可编辑 |
| **结果区** | 表格渲染、消息、耗时统计，可滚动查看 |
| **提示符** | `dbx >` 简洁提示符，支持多行编辑 |
| **分页** | 结果超过终端高度时暂停，按 `q` 退出分页、`Space` 翻页 |

### 4.2 表格渲染

**设计目标**：在终端中用 Unicode 字符渲染出类 DataGrip 的现代化表格体验。

```
┌──────┬────────────────────┬──────────┐
│ id   │ email              │ status   │
├──────┼────────────────────┼──────────┤
│    1 │ admin@example.com  │ active   │
│    2 │ user@test.com      │ inactive │
│ 1234 │ verylongemail@exam │ pending  │
└──────┴────────────────────┴──────────┘
```

**渲染规则**：

| 规则 | 说明 |
|---|---|
| **列宽自适应** | 根据表头和数据的最大宽度计算，受终端宽度约束 |
| **超宽截断** | 列宽超过 40 字符时截断并显示 `…`，完整内容可通过详情模式查看 |
| **数字右对齐** | 整数/浮点数列自动右对齐，更直观 |
| **NULL 渲染** | 显示为灰色 `(NULL)`（TTY 模式），非 TTY 显示为 `NULL` |
| **空字符串** | 显示为 `""` |
| **布尔值** | 渲染为 `true` / `false`（绿色/红色） |
| **时间戳** | 自动格式化为 `YYYY-MM-DD HH:MM:SS` |
| **JSON/长文本** | 截断显示前 30 字符 + `…`，`\e` 命令可展开查看 |
| **BLOB/二进制** | 不渲染原始值，显示 `<BLOB 1.2MB>`，`\e` 可查看 hex 前 256 字节 |
| **超长文本 (>500字符)** | 截断为 200 字符 + `…(共 N 字符)` |
| **表头样式** | 粗体白色文字 + 深色背景分隔线 |

**垂直显示（`\G` 模式）**：

当结果包含大量列或单行数据很长时，用 `\G` 代替 `;` 结尾，每条记录以垂直键值对形式展示：

```
*************************** 1. row ***************************
        id: 1
      name: 张三
     email: zhangsan@test.com
   created: 2024-01-15 10:30:00
   status: active
*************************** 2. row ***************************
        id: 2
      name: 李四
     email: lisi@test.com
   created: 2024-02-20 14:22:00
   status: inactive
```

**颜色方案**：复用项目已有 `root.go` 中的 `green()`/`red()`/`yellow()`/`bold()`/`dim()` 函数，通过 `colorOn` 变量控制开关（TTY + 无 `NO_COLOR` 环境变量时启用）。

### 4.3 SQL 输入

**行编辑能力**（使用 `github.com/peterh/liner`）：

| 功能 | 快捷键 | 说明 |
|---|---|---|
| 执行 SQL | `Enter`（以 `;` 结尾） | 检测到分号结尾时立即执行 |
| 多行编辑 | `Enter`（无分号） | SQL 未结束时自动续行，提示符变为 `→` |
| 光标移动 | `←` `→` `Home` `End` | 标准行编辑 |
| 按词跳转 | `Ctrl+←` `Ctrl+→` | 按单词跳转光标 |
| 删除词 | `Ctrl+W` | 删除前一个词 |
| 清空行 | `Ctrl+U` | 清空当前行 |
| 历史向上 | `↑` / `Ctrl+P` | 上一条历史 |
| 历史向下 | `↓` / `Ctrl+N` | 下一条历史 |
| 搜索历史 | `Ctrl+R` | 增量搜索历史命令 |
| 自动补全 | `Tab` | 补全关键字/表名/列名 |
| 取消输入 | `Ctrl+C` | 取消当前输入；若正在确认写操作则取消执行；空行时退出 REPL |
| 退出 | `Ctrl+D` / `\q` | 退出终端 |

### 4.4 元命令

支持反斜杠命令，类似 `psql` 和 MySQL 客户端的混合体验：

| 命令 | 别名 | 说明 | 示例 |
|---|---|---|---|
| `\q` | `\quit` `exit` | 退出终端 | `\q` |
| `\dt` | `\tables` | 列出当前库所有表 | `\dt` |
| `\dt users*` | — | 按通配符过滤表名 | `\dt user*` |
| `\d` | `\desc` | 查看表结构 | `\d users` |
| `\d+` | `\descd` | 查看表结构（含索引/约束），显示索引列表及 PRIMARY/UNIQUE 标记 | `\d+ users` |
| `\l` | `\list` `\databases` | 列出所有数据库 | `\l` |
| `\c` | `\use` `\connect` | 切换数据库（底层重建连接池，保证所有连接一致） | `\c mydb2` |
| `\c` | — | 查看当前连接信息（含库名、地址、TLS 状态） | `\c` |
| `\e` | `\edit` | 用外部编辑器打开上一条 SQL | `\e` |
| `\p` | `\print` | 打印当前缓冲区内容 | `\p` |
| `\r` | `\reset` | 清空输入缓冲区 | `\r` |
| `\h` | `\help` | 显示帮助 | `\h` |
| `\timing` | — | 切换耗时显示 | `\timing` |
| `\g` | — | 执行缓冲区中的 SQL | `\g` |
| `\G` | — | 垂直显示（每行一个字段，类似 `mysql -E`） | `\G` |
| `\w file` | `\write` | 将上一条查询结果写入文件 | `\w result.csv` |
| `\i file` | `\include` | 执行文件中的 SQL | `\i script.sql` |
| `\copy` | — | 导出上一条查询结果到文件（CSV），复用 \w 逻辑 | `\copy result.csv` |

### 4.5 自动补全

补全引擎在后台异步加载元数据，首次连接时缓存表名和列名。

**补全策略**：

| 上下文 | 补全内容 | 示例 |
|---|---|---|
| 任意位置 | 关键字 + 表名（始终可用） | `cy_` → `cy_user` |
| `FROM` / `JOIN` 后 | 表名 | `FROM u` → `FROM users` |
| `SELECT` / `WHERE` / `ORDER BY` 后 | 列名（带表前缀感知） | `WHERE e` → `WHERE email` |
| `ON` 后 | 表名.列名 | `u.` → `u.id, u.name, u.email` |
| `\` 开头 | 元命令 | `\d` → `\dt, \d, \d+, \databases` |

**智能补全行为**：

- 表名补全时同时显示表的行数估计（如果可用）
- 列名补全时显示数据类型
- 已输入的别名自动映射，`FROM users u` 之后 `u.` 补全 `users` 的列
- 关键字补全大小写跟随用户输入风格

**大库保护**：

- 表名补全上限 500 个候选，超限时仅匹配用户已输入前缀的前 200 个
- 元数据（表名、列名）首次连接时异步加载并缓存，后续补全从缓存读取，不重复查询
- 缓存按连接标识分片，切换库时缓存失效重建
- 补全查询失败时静默降级：仅提供关键字补全，不阻塞用户输入，不崩溃 REPL
- 执行查询时若检测到连接断开（`driver.ErrBadConn`），自动重连一次并重试；重连失败则提示用户 `\c` 重新连接

### 4.6 分页器

当结果超过终端高度时，自动通过 `less -SR` 管道分页（`-S` 截断长行、`-R` 保留 ANSI 颜色）。不重复造轮子，直接复用系统 `less` 的全部能力（搜索/跳转/行号/水平滚动）。

交互模式下通过 `render.go` 检测结果行数，超过终端高度时自动将表格内容写入临时 buffer 并通过 `less` 显示。系统无 `less` 时降级为 `more`，非 TTY 环境（管道/重定向）自动跳过。

### 4.7 大数据量处理

#### 4.7.1 行数分级控制

查询结果行数按三级策略处理，防止内存溢出：

```
行数 < 1000     → 全量加载，正常表格渲染
行数 1000~10000 → 全量加载 + 黄色警告 "已返回 N 行"
行数 > 10000    → 自动追加 LIMIT + 提示缩小范围
```

**自动 LIMIT 机制**：交互模式下，若用户 SQL 未显式包含 `LIMIT` 子句，引擎自动追加 `LIMIT 1000`。

```
dbx > SELECT * FROM orders;
⚠ 自动追加 LIMIT 1000（使用 --max-rows 调整）
┌───┬──────────┬───────┐
│...│  ...     │ ...   │
└───┴──────────┴───────┘
1000 rows in set (0.523 sec)

dbx > SELECT * FROM orders LIMIT 5000;
⚠ 返回 5000 行可能较多，确定继续？[y/N] y
┌───┬──────────┬───────┐
│...│  ...     │ ...   │
└───┴──────────┴───────┘
5000 rows in set (2.103 sec)
```

**JSON 模式**：`--json` 输出不自动追加 LIMIT（智能体消费场景需要完整结果），但仍受 `--max-rows` 控制。超过 10000 行时采用**流式输出**——逐行写入 stdout，不在内存中构建完整 JSON 数组，避免 OOM。

#### 4.7.2 大字段处理

针对单行中可能存在的超大数据类型，按类型分别处理：

| 数据类型 | 检测方式 | 渲染策略 | 展开方式 |
|---|---|---|---|
| **BLOB / BINARY / VARBINARY** | Go `[]byte` 类型 | 不渲染原始值，显示 `<BLOB 1.2MB>` | `\e` 查看 hex dump 前 256 字节 |
| **TEXT / LONGTEXT / CLOB** | `string` 长度 > 500 | 截断为 200 字符 + `…(共 N 字符)` | `\e` 查看完整内容 |
| **JSON / JSONB** | 列名或类型推断 | 截断为 200 字符 + `…` | `\e` 格式化查看 |
| **GEOMETRY / 空间类型** | 类型推断 | 显示 `<GEOMETRY POINT(…)>` | `\e` 查看 WKT/WKB |

**BLOB hex 预览效果**（`\e` 展开后）：

```
\e 展开列 "avatar" (BLOB 1.2MB):
  00000000: 8950 4e47 0d0a 1a0a 0000 000d 4948 4452  .PNG........IHDR
  00000010: 0000 0100 0000 0100 0806 0000 005c 72a8  .............\r.
  ...
  (显示前 256 字节，共 1,258,432 字节)
```

#### 4.7.3 内存保护

| 保护机制 | 说明 |
|---|---|
| **结果集上限** | 硬限制单次查询最多返回 50000 行，超限截断并警告 |
| **行大小限制** | 单行序列化后超过 10MB 时，超限列标记为 `<TRUNCATED>` |
| **渲染内存预算** | 表格渲染缓冲区上限 100MB，超限降级为 CSV 流式输出 |
| **连接池释放** | 查询结束后立即归还连接到池，不持有结果集引用 |

### 4.8 查询历史

- 持久化到 `~/.dbimpex/query_history`（纯文本文件，最多保存 10000 条）
- 跨会话保留
- `Ctrl+R` 增量搜索历史
- 历史命令去重（连续相同的不重复保存）
- 敏感 SQL（含密码等）不保存（通过关键词检测过滤）

### 4.9 执行计划可视化

对 `EXPLAIN` 查询结果做结构化渲染，让执行计划一目了然。

**EXPLAIN 表格增强**：自动识别 EXPLAIN 输出的关键列（`type`、`key`、`rows`、`Extra`）并着色标记。

```
dbx > EXPLAIN SELECT * FROM users WHERE email = 'test@example.com';

┌────┬────────────┬──────┬───────┬──────────────┬──────┬──────┬──────┐
│ id │ table      │ type │ key   │ possible_keys│ rows │ filt │ Extra│
├────┼────────────┼──────┼───────┼──────────────┼──────┼──────┼──────┤
│  1 │ users      │ ref  │ idx_e│ idx_email    │    1 │ 100% │ NULL │
└────┴────────────┴──────┴───────┴──────────────┴──────┴──────┴──────┘
```

**关键列着色**：

| 列 | 颜色规则 |
|---|---|
| `type` | `ALL`=红色(全表扫描), `ref`/`eq_ref`=绿色, `index`/`range`=黄色, `const`/`system`=绿色 |
| `key` | 有值=绿色, `NULL`=红色(未使用索引) |
| `rows` | >10000=红色, 1000~10000=黄色, <1000=绿色 |
| `Extra` | 含 `Using filesort`/`Using temporary`=红色, `Using index`=绿色 |

**EXPLAIN ANALYZE 树形渲染**：

```
dbx > EXPLAIN ANALYZE SELECT u.name, o.total FROM users u JOIN orders o ON u.id=o.user_id;

 Nested Loop Join  (cost=1.05..58.31 rows=45 width=36)
 ├─ Seq Scan on orders o  (cost=0.00..1.65 rows=65 width=8)
 │   actual time=0.012..0.056 rows=65 loops=1
 └─ Index Scan using users_pkey on users u  (cost=0.05..0.87 rows=1 width=32)
     actual time=0.003..0.005 rows=1 loops=65
     Index Cond: (id = o.user_id)

 Planning Time: 0.185 ms
 Execution Time: 1.234 ms
```

用户直接输入 `EXPLAIN SELECT ...` 即可触发执行计划分析，无需额外元命令。

---

## 5. JSON 输出模式

### 5.1 输出格式

```json
{
  "columns": ["id", "name", "email", "created_at"],
  "rows": [
    [1, "张三", "zhangsan@test.com", "2024-01-15T10:30:00Z"],
    [2, "李四", "lisi@test.com", "2024-02-20T14:22:00Z"],
    [3, "王五", null, "2024-03-10T09:15:00Z"]
  ],
  "rowCount": 3,
  "elapsed": "3.2ms",
  "sql": "SELECT id, name, email, created_at FROM users LIMIT 5"
}
```

| 字段 | 类型 | 说明 |
|---|---|---|
| `columns` | `string[]` | 列名列表，保持原始顺序 |
| `rows` | `any[][]` | 数据行，每行是对应列的值数组 |
| `rowCount` | `int` | 实际返回行数 |
| `elapsed` | `string` | 查询耗时，人类可读格式 |
| `sql` | `string` | 实际执行的 SQL（翻译后） |

> **流式输出**：当 `--max-rows` > 10000 时，自动切换为 NDJSON（Newline Delimited JSON）格式——每行数据独立一行 JSON，最后输出一个汇总行。这样消费者可以逐行解析，不必等待全部结果加载到内存。
>
> ```json
> {"type":"header","columns":["id","name"],"sql":"SELECT ..."}
> {"type":"row","data":[1,"张三"]}
> {"type":"row","data":[2,"李四"]}
> {"type":"summary","rowCount":2,"elapsed":"3ms"}
> ```

**数据类型映射（JSON 模式）**：

| 数据库类型 | JSON 类型 | 说明 |
|---|---|---|
| 整数/浮点/定点 | `number` | 直接序列化 |
| 字符串/TEXT/CHAR | `string` | 直接序列化 |
| 布尔 | `boolean` | 直接序列化 |
| NULL | `null` | 直接序列化 |
| 时间/日期/时间戳 | `string` | ISO 8601 格式：`"2024-01-15T10:30:00Z"` |
| BLOB/BINARY/RAW | `string` | Base64 编码：`"iVBORw0KGgo..."` |
| JSON/JSONB | `object`/`array` | 直接嵌入 JSON 结构 |
| 其他（空间类型等） | `string` | WKT/WKB 文本 |

### 5.2 错误格式

```json
{
  "error": "Table 'mydb.nonexist' doesn't exist",
  "elapsed": "1.2ms",
  "sql": "SELECT * FROM nonexist"
}
```

### 5.3 非查询语句格式

```json
{
  "columns": [],
  "rows": [],
  "rowCount": 0,
  "affectedRows": 5,
  "elapsed": "2.1ms",
  "sql": "UPDATE users SET status='active' WHERE id=1"
}
```

---

## 6. 架构设计

### 6.1 文件结构

```
internal/cli/
├── compare.go
├── export.go
├── config.go
├── ...
└── sqlcmd/                # sql 子命令独立子包
    ├── sqlcmd.go          # 子命令注册入口
    ├── interactive.go     # REPL 主循环 + liner + 元命令
    ├── render.go          # 表格渲染 + 执行计划着色
    ├── engine.go          # SQL 执行 + 自动补全
    └── history.go         # 历史持久化
```

### 6.2 核心数据流

```
用户输入 SQL
    │
    ▼
┌──────────────┐
│  元命令？     │──是──▶ interactive.go 处理 ──▶ 显示结果
└──────┬───────┘
       │ 否
       ▼
┌──────────────┐
│  SQL 分类     │  engine.go
│  DQL / DML   │  ParseMySQL() → AST → stmt.Type 判定
│  / DDL / TCL │  SELECT/SHOW/DESC → DQL（QueryFast）
└──────┬───────┘  INSERT/UPDATE/DELETE → DML（Execute）
       │
       ▼
┌──────────────────────────────────┐
│  cydb.DBCli 执行                  │
│  DQL → DirectQueryFast(sql)      │  ← preProcess 自动翻译 MySQL→PG/Oracle
│  DML/DDL → DirectExecute(sql)    │
└──────────────┬───────────────────┘
               │
       ┌───────┴───────┐
       ▼               ▼
┌──────────────┐  ┌──────────────┐
│  表格渲染     │  │  JSON 输出    │
│  render.go   │  │  sqlcmd.go   │
└──────────────┘  └──────────────┘
```

### 6.3 MySQL 语法翻译（由 cydb 层自动处理）

```go
// cydb 的 preProcess 已完整实现：
// db_cli_preprocess.go
func (d *DBCli) preProcess(sql string) (string, error) {
    if d.dbtype == "mysql" {
        return sql, nil  // MySQL 原样
    }
    // PostgreSQL/Oracle: GoSQLX 解析 MySQL 语法 → AST → 目标方言重建
    builder, _ := ParseMySQL(sql)     // GoSQLX 解析
    flavor, _ := d.getFlavor()        // PG/Oracle 方言
    sqlStr, args, _ := builder.BuildSQL(ss.BuildOptions{
        Flavor: flavor, InlineLiterals: true,
    })
    return flavor.Interpolate(sqlStr, args)
}
```

**覆盖的翻译场景**：

| MySQL | PostgreSQL | Oracle |
|---|---|---|
| `` `table` `` | `"table"` | `"table"` |
| `LIMIT m, n` | `LIMIT n OFFSET m` | `OFFSET m ROWS FETCH NEXT n ROWS ONLY` |
| `REPLACE INTO` | `INSERT ... ON CONFLICT DO UPDATE` | `MERGE INTO` |
| `ON DUPLICATE KEY UPDATE` | `ON CONFLICT DO UPDATE` | `MERGE INTO` |
| `!=` | `!=` | `<>` |
| `NOW()` | `NOW()` | `SYSDATE` |
| `IFNULL(a,b)` | `COALESCE(a,b)` | `NVL(a,b)` |
| `CONCAT(a,b)` | `a \|\| b` | `a \|\| b` |
| `AUTO_INCREMENT` | `SERIAL` | `GENERATED AS IDENTITY` |
| `SHOW TABLES` | `\dt` 元命令处理 | `\dt` 元命令处理 |
| `DESC table` | `\d` 元命令处理 | `\d` 元命令处理 |

> **注意**：`SHOW` / `DESC` / `DESCRIBE` 等 MySQL 特有的管理命令不由 `preProcess` 处理。这些命令在 `engine.go` 的 SQL 分类阶段被识别并拦截，统一转为对应数据库的系统表查询：
>
> - `SHOW TABLES` → MySQL 原样；PG → `SELECT tablename FROM pg_catalog.pg_tables WHERE schemaname='public'`；Oracle → `SELECT table_name FROM user_tables`
> - `SHOW DATABASES` → MySQL 原样；PG → `SELECT datname FROM pg_database WHERE datistemplate=false`；Oracle → `SELECT username FROM all_users`
> - `DESC table` → MySQL 原样；PG → 查询 `information_schema.columns`；Oracle → `DESC table`（Oracle 原生支持）
> - `SHOW CREATE TABLE t` → MySQL 原样；PG → `pg_get_tabledef` 拼接；Oracle → `DBMS_METADATA.GET_DDL`

---

## 7. 安全设计

### 7.1 写操作保护

| 安全点 | 实现 |
|---|---|
| **只读确认** | INSERT/UPDATE/DELETE/DROP/TRUNCATE/ALTER/REPLACE 在交互模式下弹出确认提示 `确认执行写操作? [y/N]`，默认 N |
| **JSON 模式拒绝写** | `--json` 模式下默认拒绝 DML/DDL，除非显式加 `--allow-write` |
| **批量执行保护** | `-f` 文件执行模式下，检测到文件含写语句时需 `--allow-write` 或交互确认 |

### 7.2 查询安全

| 安全点 | 实现 |
|---|---|
| **查询超时** | 默认 30s，通过 `context.WithTimeout` 控制，超时自动取消并释放连接 |
| **行数限制** | 非交互模式自动追加 `LIMIT 1000`（若 SQL 未含 LIMIT）；交互模式超 10000 行时警告建议加 LIMIT |
| **多语句限制** | 单次执行模式禁止多条语句（防止 SQL 注入拼接）；交互模式允许多语句但逐条确认 |
| **危险函数拦截** | 检测 `SLEEP()`/`BENCHMARK()` 等 DoS 函数时警告；`LOAD_FILE()`/`INTO OUTFILE` 等文件操作默认拒绝 |

> **实现策略**：SQL 分类（DQL/DML/DDL 识别）、LIMIT 检测、危险函数检测均优先使用 `cydb.ParseMySQL()` 解析 AST 后判断（`stmt.Type`、`stmt.LimitClause`），比字符串匹配更精确——不会误判注释、字符串字面量中的同名文本。解析失败时采用**保守拒绝**策略：无法确定 SQL 类型时，按最严格的安全规则处理（要求写操作确认、追加 LIMIT），防止未知语句绕过安全检查。

### 7.3 凭证安全

| 安全点 | 实现 |
|---|---|
| **密码掩码** | 提示符中不显示密码；`\c` 查看连接信息时密码显示为 `****` |
| **历史过滤** | 查询历史持久化时过滤含 `IDENTIFIED BY`/`PASSWORD`/`SET PASSWORD`/`CREATE USER` 的 SQL |
| **环境变量优先** | 密码优先从环境变量 `DBX_PASSWORD` 读取，避免命令行明文传递 |
| **连接释放** | 单次执行模式（`-e`/`--json`）：查询结束立即 `Close()` 释放连接；交互模式：参见 7.6 节会话管理，REPL 期间维持连接池，退出时释放 |

### 7.4 传输安全

| 安全点 | 实现 |
|---|---|
| **TLS 提示** | 连接非本地数据库时，若未启用 TLS 则给出黄色警告 `⚠ 未使用加密连接` |
| **CA 证书** | 支持 `--ssl-ca` 指定 CA 证书路径 |

### 7.5 审计追踪

| 安全点 | 实现 |
|---|---|
| **操作日志** | 写操作（INSERT/UPDATE/DELETE/DDL）记录到 `~/.dbimpex/audit.log`，含时间戳、SQL（截断至 500 字符）、影响行数、耗时 |
| **日志轮转** | 单文件上限 10MB，自动轮转保留最近 5 个文件（`audit.log`、`audit.1.log`...`audit.4.log`） |
| **只读操作不记录** | SELECT/SHOW/DESC 等只读查询不产生审计日志，避免日志膨胀 |

### 7.6 交互模式会话管理

`cydb` 底层使用 `sqlx.DB` 连接池（默认 MaxOpenConns=10），连接池中的连接可被不同查询复用。如果直接用 `USE dbname` SQL 切换库，只有当前拿到的那个连接会切换，池中其他连接仍然指向旧库，导致后续查询可能拿到不一致的连接。

**设计决策**：交互模式在整个 REPL 会话期间维持一个 `*cydb.DBCli` 实例（即一个连接池），切换库时**关闭旧连接池、重建新连接池**，确保所有连接一致。

```
REPL 会话生命周期
═══════════════════════════════════════════════════

启动 → TryConnect(mydb) → 连接池 [10 个连接，全部指向 mydb]
  │
  ├─ SELECT ... → 从池中取连接 → 执行 → 归还
  ├─ SELECT ... → 从池中取连接 → 执行 → 归还
  │
  ├─ \c mydb2 或 USE mydb2
  │   └─ cli.Close() → 销毁旧连接池
  │   └─ TryConnect(mydb2) → 新连接池 [10 个连接，全部指向 mydb2]
  │
  ├─ SELECT ... → 从新池中取连接 → 执行 ✅ 一致
  │
  └─ \q → cli.Close() → 销毁连接池
```

**切换库的处理策略**：

| 用户输入 | 数据库 | 处理方式 |
|---|---|---|
| `USE mydb2;` | MySQL | 拦截，不直接执行；关闭旧池 → 重建新池到 `mydb2` |
| `\c mydb2` | 全部 | 关闭旧池 → 重建新池到 `mydb2` |
| `\c`（无参数） | 全部 | 显示当前连接信息：库名、地址、TLS 状态 |

**提示符实时更新**：切换库后提示符同步更新。

```
dbx (mysql @ 10.0.0.1:3306/mydb)  > USE mydb2;
dbx (mysql @ 10.0.0.1:3306/mydb2) >   ← 提示符自动更新
```

---

## 8. 依赖与兼容性

### 8.1 新增 Go 依赖

```go
require (
    github.com/peterh/liner v1.2.2   // 行编辑、历史、补全
)
```

**选择 `liner` 的理由**：

- 纯 Go 实现，无 CGO 依赖，跨平台一致（Windows/macOS/Linux）
- 支持多行编辑、历史持久化、Tab 补全
- 比 `readline` 更轻量，API 更简单
- 已在众多 Go CLI 工具中广泛使用（如 `influx`、`cockroach sql`）

### 8.2 系统依赖与降级

| 依赖 | 用途 | Windows | macOS/Linux |
|---|---|---|---|
| `less` | 结果分页 | ❌ 不可用 → 降级为 `more` → 仍不可用则直接输出 | ✅ 默认可用 |
| `$EDITOR` | `\e` 外部编辑器 | 检测 `EDITOR` 环境变量，默认降级为 `notepad` | 默认 `vim` |
| 终端宽度 | 列宽自适应 | `golang.org/x/term.GetSize()` 全平台 | `golang.org/x/term.GetSize()` 全平台 |

### 8.3 Windows 兼容性

| 组件 | 状态 | 说明 |
|---|---|---|
| `liner` 行编辑 | ✅ | 纯 Go，Windows 原生支持 |
| `cydb` 连接池 | ✅ | `sqlx` 纯 Go，驱动层全平台 |
| ANSI 颜色 | ✅ | Windows Terminal（Win10+ 默认）完整支持；旧 cmd.exe 自动降级为纯文本 |
| Unicode 表格边框 | ✅ | Windows Terminal 完整支持 Unicode |
| `~/.dbimpex/` 数据目录 | ✅ | 项目已使用 `os.UserHomeDir()`，Windows 下自动映射到 `%USERPROFILE%` |
| `Ctrl+C` / SIGINT | ✅ | `liner` 已处理 Windows 平台差异 |
| 分页器降级 | ✅ | `less` → `more` → 直接输出，三级降级 |

---

## 9. 实施计划

| 阶段 | 模块 | 说明 | 预估 |
|---|---|---|---|
| **Phase 1** | `sqlcmd.go` + `engine.go` | 子命令注册、SQL 分类执行、JSON 输出、单次执行模式 | 核心骨架 |
| **Phase 2** | `render.go` | 终端表格渲染（Unicode 边框、列宽自适应、NULL/数字/布尔着色）、执行计划着色 | 核心体验 |
| **Phase 3** | `interactive.go` | REPL 主循环、liner 集成、全部元命令（`\q` `\dt` `\d` `\l` `\c` `\e` `\timing` `\G` `\g` `\p` `\r` `\h` `\w` `\i`） | 交互核心 |
| **Phase 4** | `history.go` | 查询历史持久化、`Ctrl+R` 搜索、敏感 SQL 过滤 | 体验增强 |
| **Phase 5** | 注册到 `root.go` + 测试 | 集成测试、边界情况、性能调优 | 交付 |

### 优先级矩阵

```
影响大 │  Phase 2 表格渲染         │  Phase 3 REPL 循环
       │  Phase 1 核心骨架         │
       │                          │
影响小 │  Phase 4 历史搜索         │  Phase 5 测试
       │                          │
       └──────────────────────────┴──────────────
         工作量小                   工作量大
```

---

## 10. 与其他数据库 CLI 工具的对比

| 特性 | mysql CLI | psql | mycli | dbx sql |
|---|---|---|---|---|
| Unicode 表格边框 | ❌ ASCII | ❌ ASCII | ❌ ASCII | ✅ Unicode |
| 语法着色 | ❌ | ❌ | ✅ AST 级着色 | ✅ 关键字/值/表名/列名着色 |
| NULL 视觉区分 | ❌ | ❌ | ✅ 灰色 | ✅ 灰色 `(NULL)` |
| 自动补全 | ✅ 基础 | ✅ 丰富 | ✅ 模糊匹配+类型提示 | ✅ 表名+列名+类型+别名感知 |
| 查询历史搜索 | ❌ | ❌ | ❌ | ✅ `Ctrl+R` 增量搜索 |
| 执行计划可视化 | ❌ | ❌ | ❌ | ✅ type/key/rows 着色 + 树形渲染 |
| 结果分页 | ❌ | ✅ less | ❌ | ✅ less 管道 + 降级 more |
| JSON 输出 | ❌ | ❌ | ❌ | ✅ `--json` 结构化输出 |
| 智能体友好 | ❌ | ❌ | ❌ | ✅ 专为 AI/脚本设计 |
| MySQL 语法跨库 | ❌ | ❌ | ❌ | ✅ 自动翻译 PG/Oracle |
| 写操作确认 | ❌ | ❌ | ✅ 有确认 | ✅ 交互 + 批量 + JSON 三层确认 |
| 密码掩码/不记历史 | ❌ | ❌ | ❌ | ✅ 掩码 + 过滤 + 环境变量 |
| 操作审计日志 | ❌ | ❌ | ❌ | ✅ 写操作审计 |
| 危险函数拦截 | ❌ | ❌ | ❌ | ✅ DoS/文件操作拦截 |
| BLOB/大字段处理 | ❌ 刷屏 | ❌ 刷屏 | ❌ 刷屏 | ✅ 类型化渲染 + hex 预览 |
| 大数据量保护 | ❌ OOM | ❌ OOM | ❌ OOM | ✅ 行数分级 + 内存预算 + 自动 LIMIT |
| 对现有功能零侵入 | — | — | — | ✅ 仅新增子包，不改任何现有文件 |

---

## 11. 对现有功能的影响（零侵入）

**零侵入**。`dbx sql` 子命令以独立子包 `internal/cli/sqlcmd/` 实现，对现有代码的改动仅一行：

```go
// root.go 新增一行
rootCmd.AddCommand(sqlcmd.Command())
```

**复用现有函数（不动其源码）**：
- 连接解析：复用 `newCliService()` + `overrideConnDB()`（`root.go` 已导出）
- 颜色输出：复用 `colorOn` / `green()` / `red()` / `yellow()` / `bold()` / `dim()`（`root.go` 已导出）
- 终端检测：复用 `stdoutTTY`（`root.go` 已导出）
- 连接 flags：复用 `registerConnFlags()` + `connFlags` + `registerConnAliases()`（`root.go` 已导出）
- 连接补全：复用 `completeConnNames()`（`root.go` 已导出）

**不改动任何现有文件**：`root.go`、`conn.go`、`config.go`、`compare.go`、`export.go` 等全部保持不变。

---

## 12. 附录：关键数据结构

### QueryResult（内部）

```go
type QueryResult struct {
    Columns      []string
    Rows         [][]any
    RowCount     int
    AffectedRows int64
    Elapsed      time.Duration
    SQL          string  // 实际执行的 SQL（翻译后）
    IsQuery      bool    // true=SELECT 类，false=DML/DDL
}
```

### RenderConfig（渲染配置）

```go
type RenderConfig struct {
    MaxColWidth    int           // 单列最大宽度，默认 40
    TerminalWidth  int           // 终端宽度，自动检测
    TerminalHeight int           // 终端高度
    UseColor       bool          // 是否启用颜色
    PagerEnabled   bool          // 是否启用分页
    NullString     string        // NULL 显示文本，默认 "(NULL)"
    TimeFormat     string        // 时间格式，默认 "2006-01-02 15:04:05"
}
```

### HistoryEntry（历史记录）

```go
type HistoryEntry struct {
    SQL       string
    Timestamp time.Time
    Database  string
    Elapsed   time.Duration
    RowCount  int
    Error     string  // 非空表示执行失败
}
```

---

> **文档版本**：v3.0  
> **最后更新**：2026-08-12  
> **状态**：已实现，持续完善


---

## 13. 实现状态

| 功能 | 状态 |
|---|---|
| 交互式 REPL + 单次执行 + JSON 输出 | ✅ |
| Unicode 表格渲染（tablewriter） | ✅ |
| NULL/BLOB/布尔着色 | ✅ |
| SQL 分类（cydb.ParseMySQL AST + 降级） | ✅ |
| 写操作确认 + 危险函数拦截 | ✅ |
| 元命令（\q/\dt/\d/\d+/\l/\c/\e/\g/\G/\p/
/\h/\w/\copy/\i） | ✅ |
| 上下文感知自动补全（关键字+表名+数据库名+SHOW子命令） | ✅ |
| 查询历史持久化 + 敏感 SQL 过滤 | ✅ |
| 分页器（less -SRX） | ✅ |
| 审计日志（10MB 轮转） | ✅ |
| 会话连接池 + 断线重连 | ✅ |
| MySQL 语法跨库翻译（cydb preProcess） | ❌ 未接入（按目标方言原生执行，设计稿预留） |
| 流式 JSON（NDJSON，>10000 行自动切换） | ✅ |
| 外部编辑器 \e（\ 降级 notepad） | ✅ |
| 垂直显示 \G（和 mysql CLI 规则一致） | ✅ |
| 基础库 GetDatabases / GetIndexes | ✅ |
| 数字右对齐 | ⏳ 待实现 |

### 基础库新增 API

| 方法 | 说明 |
|---|---|
| `DBCli.GetDatabases()` | 返回数据库列表（跨方言） |
| `DBCli.GetIndexes(tableName)` | 返回表索引列表（跨方言） |
