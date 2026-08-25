# dqex 使用手册

CLI 与 Web 共享同一份数据（连接配置、任务配置、执行历史），默认存放于 `~/.dqex`（Linux/macOS）或 `%USERPROFILE%\.dqex`（Windows）。

## 快速开始

解压后：

**Linux / macOS：**
```bash
./install.sh                    # 安装到 /usr/local/bin
dqex                             # 启动 Web 服务
# 或
./start.sh                      # 前台运行（Ctrl+C 停止）
./start.sh -d                   # 后台运行（关终端不中断）
./stop.sh                       # 停止后台服务
./url.sh                        # 查看 Web 访问链接
```

**Windows（双击或 cmd 执行）：**
```bat
install.bat                     :: 安装到 %%LOCALAPPDATA%%\dqex 并加入 PATH
dqex                             :: 新开终端后启动 Web 服务
:: 或不安装直接使用：
start.bat                       :: 前台运行（关窗口即停）
start.bat -d                    :: 后台运行（关窗口不中断）
stop.bat                        :: 停止后台服务
```

---

## 全局说明

### Web 服务

不带子命令直接运行时启动 Web 界面，默认仅监听本机；本机回环（127.0.0.1/localhost）免认证，外部来源需要令牌认证：

```bash
dqex                      # 启动 Web 服务（默认 127.0.0.1:8181，自动打开浏览器）
dqex version              # 查看版本号（简写 dqex v）
dqex url                  # 输出带 token 的 Web 访问链接
dqex url --token-only     # 仅输出 token，便于 curl/脚本调试
```

| flag | 默认值 | 说明 |
|---|---|---|
| `--host` | `127.0.0.1` | 监听地址，对外暴露用 `0.0.0.0`（外部来源强制令牌认证） |
| `--port` | `8181` | 监听端口 |
| `--allow` | 无 | 访问来源白名单（IP/CIDR/域名，逗号分隔）；**对外暴露（非回环监听）时必填**，留空则外部来源一律拒绝、仅本机可访问；本机回环始终放行 |
| `--no-browser` | `false` | 启动时不自动打开浏览器 |
| `--no-auth` | `false` | 完全禁用令牌认证 |
| `--data-dir` | `~/.dqex` | 数据根目录 |
| `--config-file` | `~/.dqex/config.yaml` | 全局配置文件路径 |

> **安全说明**：令牌有效期 24 小时，未过期重启复用，超期后 API 返回 401 提示重启；`dqex url` 可随时取回访问链接。本机回环访问免认证；对外暴露（`--host 0.0.0.0`）时**必须配置 `--allow` 来源白名单**（未配置则外部来源一律拒绝、仅本机可访问），白名单内来源强制令牌认证，并支持暴力破解防护（1 分钟 10 次后锁定 5 分钟）；`--no-auth` 时白名单内免认证，仅建议在可信内网使用。
>
> ⚠️ 根命令 `--port` 是 **Web 服务端口**；子命令中的 `--port` 是数据库端口（mysqldump 风格别名），两者作用域不同。

### CLI 命令

所有执行类命令（export/import/migrate/compare/dictionary）都支持三种参数来源：

| 来源 | 说明 |
|---|---|
| `--config xxx.yaml` | 配置文件，适合固化到脚本 |
| `--task <ID>` | 复用已保存的任务配置 |
| `--xxx` 命令行参数 | 一次性覆盖，仅显式给出的参数生效 |

> 连接名支持中文 / 空格（如 `170 生产`），shell 中使用时注意加引号：`--source-conn "170 生产"`。

### 快捷输入

**命令别名：**

| 全名 | 别名 | 全名 | 别名 |
|---|---|---|---|
| `export` | `exp` | `conn` | `cn` |
| `import` | `imp` | `task` | `tk` |
| `migrate` | `mig` | `history` | `his` |
| `compare` | `cmp` | `config` | `cfg` |
| `dictionary` | `dict` | `sql` | — |
| `snapshot` | `snap` | `list` | `ls` |
| `delete` | `del` | | |

**常用短参：**

| 短参 | 等价长参 | 适用命令 |
|---|---|---|
| `-s` / `-t` | `--source-conn` / `--target-conn` | export/migrate/compare/dictionary 的 `-s`；import 的 `-t` |
| `-T` | `--tables` | export/migrate/compare/dictionary |
| `-o` / `-i` | `--output` / `--input` | export/dictionary 的 `-o`；import 的 `-i` |
| `-h` `-P` `-u` `-p` | `--host` `--port` `--user` `--password` | export/import/dictionary |

> ⚠️ export/import/dictionary 的 `-h` 是**主机**（mysqldump 习惯），这些命令的帮助改用 `--help`。

### Shell 补全

```bash
# zsh
mkdir -p ~/.zsh/completions && dqex completion zsh > ~/.zsh/completions/_dqex
echo 'fpath=(~/.zsh/completions $fpath)' >> ~/.zshrc && echo 'autoload -Uz compinit && compinit' >> ~/.zshrc

# bash
dqex completion bash >> ~/.bashrc
```

---

## 运维场景

### 连接管理

```bash
dqex conn add --name "生产库" --type mysql --host 10.20.16.170 --port 3317 --un root --pw 'xxx'
dqex conn list
dqex conn test --conn "生产库"
dqex conn delete --conn "生产库"
```

### 定时备份（cron）

```yaml
# daily.yaml
source_ref: "生产库"
output: /data/backup/daily.zip
databases:
  - camunda
compress: true
```

```bash
dqex export --config daily.yaml
# crontab -e
0 2 * * * dqex export --config daily.yaml >> /var/log/dqex-backup.log 2>&1
```

### 灾备恢复

```bash
dqex import --target-conn "测试库" --input daily.zip --reset truncate
```

### 跨类型同步

```bash
dqex migrate --source-conn "生产库" --target-conn "测试库" \
  --source-database camunda --target-database camunda --reset truncate
```

### 对比验证

```bash
dqex compare --source-conn "生产库" --target-conn "测试库" \
  --source-database camunda --target-database camunda --scope both
```

### 数据字典

```bash
dqex dictionary camunda -s "170 生产" -o ./dict.zip
```

### 故障排查

```bash
dqex history list                  # 查看执行记录
dqex history show --id <ID>        # 查看详情
```

---

## SQL 终端（dqex sql）

交互式 REPL / 单次执行 / 智能体友好 JSON 输出。SQL 按**目标数据库方言原生执行**：不提供 MySQL→PG/Oracle 自动语法翻译，跨方言场景请直接写目标方言（PG 用 `LIMIT ... OFFSET ...`、Oracle 用 `FETCH FIRST ... ROWS ONLY`）；DDL 与写入 SQL 的方言转换由迁移/导入引擎负责，与终端无关。

### 交互式终端

```bash
dqex sql -c "生产库"          # 连接名称
dqex sql -c prod             # 短名
dqex sql -c conn_a1b2c3      # 连接 ID

# 进入后提示符自动显示类型/地址/库名：
# dqex (mysql  @ 10.0.0.1:3306/mydb) >
# dqex (pg     @ 10.0.0.2:5432/mydb) >
```

终端能力（已落地）：

- **补全**：Tab 补全关键字 / 表名 / 列名 / 数据库名；`FROM users u` 后 `u.` 可补 `users` 列（别名感知）；列名补全带数据类型、表名补全带行数估计。
- **历史**：`Ctrl+R` 增量搜索；敏感 SQL（含密码）自动过滤，不落历史。
- **安全护栏**：写操作（INSERT/UPDATE/DELETE）默认需确认；`SLEEP(`/`BENCHMARK(` 触发警告，`LOAD_FILE(`/`INTO OUTFILE` 直接拦截；审计日志轮转落盘。
- **大数据量保护**：默认 `max-rows=1000` 行上限；非 TTY 自动降级为纯文本表格（便于管道 / grep）。
- **智能体模式**：`--json` 输出 NDJSON，供脚本 / Agent 消费。

**常用元命令：**

| 命令 | 说明 |
|---|---|
| `\q` / `\quit` | 退出终端 |
| `\dt` / `\tables [pat]` | 列出表（支持通配符 * ?） |
| `\d` / `\d+ <表名>` | 查看表结构（`\d+` 含索引/约束） |
| `\l` / `\c <库名>` | 列出数据库 / 切换数据库 |
| `\g` / `\G` | 再次执行上一条 SQL：表格 / 垂直显示（每行一个字段，字段多时推荐） |
| `\x [on\|off\|auto]` | 扩展显示开关（psql 风格）：on=垂直 off=表格 auto=表格超宽自动垂直（默认）；切换后写入 config.yaml `cli.display_mode` 持久化 |
| `\p` / `\r` | 打印缓冲区 / 清空缓冲区 |
| `\e` / `\edit` | 用外部编辑器编辑上一条 SQL |
| `\copy <文件>` | 导出上一条查询结果到文件（CSV） |
| `\i <文件>` | 执行文件中的 SQL |
| `\h` | 显示帮助 |

> **结果展示**：`auto`（默认）模式下，表格宽度超过终端时会自动切换垂直显示（行数 ≤ 30），行数较多时提示改用 `\G`；`\x off` 可强制表格，`\x on` 强制垂直。
>
> **便捷写法**：SQL 行尾加 `\G`（如 `SELECT * FROM t \G`）单次垂直显示；`/` 前缀与 `\` 等价（`/ai`、`/dt` 同 `\ai`、`\dt`）。

### 单次执行与管道

```bash
dqex sql -c prod -e "SELECT * FROM users LIMIT 5"          # 表格输出
dqex sql -c prod --json "SELECT id,name FROM users"        # JSON 输出
echo "SELECT COUNT(*) FROM orders" | dqex sql -c prod --json   # 从 stdin
dqex sql -c prod -f query.sql                              # 从文件执行

# 管道友好
dqex sql -c prod -e "SELECT * FROM users" | grep "admin"
result=$(dqex sql -c prod --json "SELECT COUNT(*) FROM users")
echo "$result" | jq '.rows[0][0]'
```

### 常用 flag

| flag | 短参 | 默认 | 说明 |
|---|---|---|---|
| `--conn` | `-c` | — | 已保存连接（ID/名称/短名） |
| `--execute` | `-e` | — | 执行 SQL 后退出 |
| `--file` | `-f` | — | 从文件读取 SQL |
| `--json` | — | false | JSON 格式输出 |
| `--timeout` | — | 30s | 查询超时 |
| `--max-rows` | — | 1000 | 最大返回行数 |
| `--allow-write` | — | false | `--json`/`-f` 模式允许写操作 |
| `--ssl-ca` | — | — | TLS CA 证书路径 |
| `--no-color` | — | false | 禁用颜色 |

> 内联连接：`dqex sql --type mysql --host 10.0.0.1 --port 3306 --un root --pw '${DB_PASSWORD}' --db mydb`

---

## 快照（dqex snapshot）

对库结构 / 数据打快照并对比差异，CLI + Web 全链路已落地。子命令：`create / list / show / delete / compare`。

```bash
dqex snapshot create -c 生产库 -n 早盘          # 打快照
dqex snapshot list -c 生产库                    # 列出快照
dqex snapshot show  -c 生产库 --id <ID>         # 查看快照内容
dqex snapshot delete -c 生产库 --id <ID>        # 删除
dqex snapshot compare -c 生产库 --a 早盘 --b 午盘   # 对比两个快照结构/数据差异
```

典型用途：每次变更前打快照，出问题时 `compare` 快速定位结构或数据漂移。

---

## AI 辅助写 SQL（可选模块）

**配置齐全（BaseURL / API Key / Model 三项非空）才启用；未配置时入口完全隐藏，不影响任何既有功能。**

- **生成 ≠ 执行**：AI 只产出 SQL 文本，需经写操作确认与危险语句拦截后才能执行。
- **只读探索**：生成前自动查询真实库表结构（控制台显示 `⟳` 进度），无 SQL 写执行能力。
- **密钥不出本机**：API Key 存本地，界面仅显示掩码后的端点与模型名。
- **注入防护**：AI 生成的 SQL 会经危险语句校验，阻断恶意注入。

```bash
# 在交互终端内启用（需先配置 AI）
dqex sql -c 生产库
dqex (mysql @ ...)> \ai 帮我统计昨日新增用户数
```

**`\ai` 子命令一览**（在 `dqex sql` 交互终端内使用）：

| 命令 | 说明 |
|---|---|
| `\ai <需求>` | 生成 SQL 到缓冲区，可 `\e` 编辑、`\g` 执行 |
| `\ai explain [SQL]` | 解释 SQL，缺省用缓冲区中的 SQL |
| `\ai fix [报错信息]` | 修复缓冲区中的 SQL；缺省自动附带最近一次执行报错（`\g` 执行失败后可直接 `\ai fix`） |
| `\ai continue <补充>` | 基于上文继续补充生成 |
| `\ai copy` | 复制缓冲区 SQL 到系统剪贴板 |
| `\ai status` | 查看配置状态、上下文消息数与 token 统计 |
| `\ai config` | 引导式修改 AI 配置（写回 config.yaml，Web 端下次启动读取） |
| `\ai clear` | 重置当前会话（清空上下文与 token 统计） |
| `\ai help` | 显示以上帮助 |

```bash
# 示例：生成 → 修复 → 执行（执行报错后可直接 \ai fix，自动携带报错信息）
dqex (mysql @ ...)> \ai 统计每个部门的人数
dqex (mysql @ ...)> \g
[query] error=> Error 1054 ... Unknown column 'dept' ...
提示: 输入 \ai fix 可让 AI 根据该报错自动修复 SQL
dqex (mysql @ ...)> \ai fix
dqex (mysql @ ...)> \g
```

> 提示：需求中写明表名（如 `\ai 查询 robotics_user 表的必要字段`）可让 AI 先确认真实表结构，避免凭想象生成字段。

---

## 命令详解

### export 导出

```bash
dqex export [database [table...]] [flags]
```

| flag | 说明 |
|---|---|
| `--config` / `--gen-config` / `--task` | 配置文件 / 生成模板 / 任务 ID |
| `--source-type` / `--source-host` / `--source-port` / `--source-un` / `--source-pw` / `--source-db` / `--source-subtype` | 内联连接 |
| `--host` / `--port` / `--user` / `--password` / `--database` | mysqldump 风格别名 |
| `--source-conn` | 已保存连接 |
| `--output` / `-o` | 输出目录或 `.zip` 路径 |
| `--name` | 任务名 |
| `--databases` | 指定库，逗号分隔 |
| `--tables` / `-T` | 指定表，逗号分隔 |
| `--objects` | 指定对象 `_views/名称`，逗号分隔 |
| `--table-cond` | 过滤条件 `表名:完整SELECT`，可重复 |
| `--schema-only` / `--no-data` | 仅结构 |
| `--data-only` / `--no-create-info` | 仅数据 |
| `--compress` | zip 打包，默认 true |
| `--gzip` | SQL 文件 gzip 压缩为 `.sql.gz` |
| `--single-transaction` | 一致性快照导出，默认 true（仅 MySQL/PG） |
| `--batch-size` | 批量大小，默认 500 |

```bash
# 示例
dqex export --gen-config > export.yaml
dqex export --config export.yaml
dqex export --source-conn "170 生产" --databases camunda -o ./camunda.zip
dqex export camunda act_ge_property -s 本地 --schema-only --compress=false -o ./schema
```

### import 导入

```bash
dqex import [flags]
```

| flag | 说明 |
|---|---|
| `--config` / `--gen-config` / `--task` | 同上 |
| `--target-type` / `--target-host` / `--target-port` / `--target-un` / `--target-pw` / `--target-db` / `--target-subtype` | 内联目标连接 |
| `--host` / `--port` / `--user` / `--password` / `--database` | mysqldump 风格别名 |
| `--target-conn` / `-t` | 已保存连接 |
| `--input` / `-i` | 导入文件（.sql / .sql.gz / .zip），必填 |
| `--reset` | `truncate`（清空表）/ `drop`（删除重建） |
| `--backup` | 重置前备份，默认 true |
| `--batch-size` | 批量大小，默认 500 |

```bash
dqex import -t 本地 -i ./camunda.zip --reset truncate
dqex import --config import.yaml
```

> ⚠️ `--reset` 非空且 `--backup=false` 时目标数据不可恢复。

### migrate 迁移

```bash
dqex migrate [flags]
```

| flag | 说明 |
|---|---|
| `--source-conn` / `--target-conn` | 已保存连接 |
| `--source-*` / `--target-*` | 内联连接 |
| `--tables` / `-T` | 指定表，逗号分隔 |
| `--objects` | 指定对象（仅同类型迁移生效） |
| `--table-cond` | 过滤条件，可重复 |
| `--schema-only` / `--data-only` | 仅结构 / 仅数据 |
| `--reset` / `--backup` / `--batch-size` | 同 import |

```bash
dqex migrate --config migrate.yaml
dqex migrate -s "170 生产" -t 本地 --tables act_ge_property --reset truncate
```

### compare 对比

```bash
dqex compare [flags]
```

| flag | 说明 |
|---|---|
| `--config` / `--gen-config` / `--task` | 同上 |
| `--source-conn` / `--target-conn` | 已保存连接 |
| `--source-*` / `--target-*` | 内联连接 |
| `--tables` / `-T` | 指定表，逗号分隔 |
| `--alias` | 表别名配对 `源表=目标表`，可重复 |
| `--scope` | `both` / `structure` / `data` |
| `--threshold` | 逐行比较阈值，默认 1000 |
| `--ignore-columns` | 忽略列，逗号分隔 |
| `--force-data` | 结构不一致仍强制对比数据 |
| `--output` | 报告 JSON 额外保存路径 |
| `--all` | 输出全部表（默认仅输出有差异的表） |

```bash
dqex compare --config compare.yaml
dqex cmp -s "170 生产" -t 本地 -T act_ge_property --scope both -o report.json
```

**compare show 查看差异明细：**

```bash
dqex cmp show -i <记录ID>            # 全部表差异摘要
dqex cmp show -i <记录ID> <表名>      # 单表差异明细
```

### dictionary 数据字典

```bash
dqex dictionary [database [table...]] [flags]
dqex dict                    # 简写
```

| flag | 说明 |
|---|---|
| `--config` / `--gen-config` / `--task` | 同上 |
| `--source-*` / `--host` / `--port` / `--user` / `--password` / `--database` | 源连接 |
| `--source-conn` / `-s` | 已保存连接 |
| `--output` / `-o` | 输出目录或 `.zip` 路径 |
| `--name` | 任务名 |
| `--databases` | 指定库，逗号分隔 |
| `--tables` / `-T` | 指定表，逗号分隔 |
| `--compress` | zip 打包，默认 true |

```bash
dqex dictionary --gen-config > dictionary.yaml
dqex dictionary --config dictionary.yaml
dqex dict camunda -s "170 生产" -o ./camunda-dict.zip
```

### config 全局配置

```bash
dqex config                     # 查看当前配置
dqex config --gen               # 输出配置模板
```

### conn 连接管理

```bash
dqex conn list
dqex conn add --name 生产库 --type mysql --host 10.20.16.170 --port 3317 --un root --pw 'xxx'
dqex conn add --id <ID> ...     # 更新已有连接
dqex conn test --conn "生产库"
dqex conn delete --conn "生产库"
```

### task 任务配置

```bash
dqex task list                          # --type 过滤
dqex task show --id <ID>
dqex task run --id <ID>                 # 同步执行
dqex task save --name 每日对比 --config compare.yaml
dqex task delete --id <ID>
```

### history 执行历史

```bash
dqex history list                       # --type 过滤
dqex history show --id <ID>
dqex history delete --id <ID>
```

---

## 配置文件

### 全局配置（~/.dqex/config.yaml）

```yaml
dirs:
  data: ""        # 默认 ~/.dqex
  tmp: ""         # 默认 <data>/tmp
  uploads: ""     # 默认 <data>/uploads
  exports: ""     # 默认 <data>/exports
cli:
  display_mode: ""   # SQL 终端结果默认显示：空/auto=表格超宽自动垂直 | table=强制表格 | vertical=强制垂直（\x 命令切换并写回）
```

配置文件查找顺序：`--config-file` > 环境变量 `dqex_CONFIG` > `~/.dqex/config.yaml`

### 任务配置文件

连接段的两种写法：

```yaml
# 内联
source:
  type: mysql
  host: 127.0.0.1
  port: 3306
  user: root
  password: ""

# 引用已保存连接
source_ref: "170 生产"
```

**dictionary.yaml 示例：**

```yaml
source_ref: "170 生产"
output: ./dictionary.zip
name: mydict
databases:
  - db1
tables:                      # 留空 = 全部表
  - table_a
compress: true
```

**export.yaml 示例：**

```yaml
source_ref: "170 生产"
output: ./export.zip
databases:
  - db1
tables:
  - table_a
conditions:
  - "table_a: SELECT * FROM table_a WHERE status = 1"
compress: true
batch_size: 500
```

**compare.yaml 示例：**

```yaml
source_ref: "170 生产"
target_ref: "本地"
source_database: camunda
target_database: camunda
tables:
  - act_ge_property
scope: both
threshold: 1000
ignore_columns:
  - created_at
  - updated_at
```

**import.yaml 示例：**

```yaml
target_ref: "本地"
input: ./export.zip
reset: ""                    # "" | truncate | drop
backup: true
batch_size: 500
```

**migrate.yaml 示例：**

```yaml
source_ref: "170 生产"
target_ref: "本地"
source_database: camunda
target_database: camunda
tables:
  - table_a
reset: ""
backup: true
batch_size: 500
```

---

## 参数优先级

**`--config` 配置文件 → `--task` 任务配置 → 命令行 flag**（命令行优先级最高）。命令行仅显式给出的 flag 才会覆盖，未给出的保留配置取值。

```bash
# 以配置文件为基础，临时改导出其中两张表
dqex export --config daily.yaml --tables orders,users
```

---

## 终端输出

- **进度条**：TTY 下单行刷新；管道/重定向时退化为每 20% 一条里程碑行
- **颜色**：仅 TTY 启用；`NO_COLOR=1` 可强制关闭
- **错误**：失败时输出中文错误信息，退出码为 1

---

## 注意事项

1. **连接带库名**：`compare` / `migrate` 要求连接明确到库。内联连接写 `database`；引用已保存连接时用 `--source-database` / `--target-database` 覆盖
2. **密码安全**：内联密码明文存于配置文件，注意文件权限，不要提交到 git；推荐用 `conn add` 保存连接后以 `source_ref` 引用
3. **重置即破坏**：`--reset truncate/drop` 会清空或删除目标表；默认 `--backup=true` 先建备份表，成功后自动清理，失败时保留可回滚
4. **导出文件格式**：dqex 自有结构（`库名.sql` + `.desc`），与 mysqldump 不互通，请用 `dqex import` 导入
5. **大表对比**：超过 `threshold`（默认 1000）仅比行数，需逐行确认时调大阈值
6. **task run 同步执行**：长任务占用终端，Ctrl+C 终止（历史保留已执行部分）
7. **同名参数**：根命令 `--port` 是 Web 端口；子命令 `--port` 是数据库端口
