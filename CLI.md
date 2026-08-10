# dbx CLI 使用手册

dbx 是数据库导入 / 导出 / 迁移 / 对比工具，提供两种使用方式：

- **Web 界面**：不带子命令直接运行，启动 Web 服务（默认端口 8181）
- **命令行（CLI）**：`export / import / migrate / compare / config / conn / task / history` 子命令，
  在无法开启 Web 的服务器上可独立完成全部操作

CLI 与 Web 共享同一份数据（连接配置、任务配置、执行历史），默认存放于 `~/.dbimpex`，
四类数据目录可通过[全局配置文件](#全局配置configyaml)独立指定。

---

## 目录

- [构建](#构建)
- [全局说明](#全局说明)
  - [快捷输入：命令别名 / 短参 / Shell 补全](#快捷输入命令别名--短参--shell-补全)
- [运维实战场景](#运维实战场景)
- [命令详解](#命令详解)
  - [export 导出](#export-导出)
  - [import 导入](#import-导入)
  - [migrate 迁移](#migrate-迁移)
  - [compare 对比](#compare-对比)
    - [compare show 查看差异明细](#compare-show-查看差异明细)
  - [config 全局配置](#config-全局配置)
  - [conn 连接管理](#conn-连接管理)
  - [task 任务配置](#task-任务配置)
  - [history 执行历史](#history-执行历史)
- [目录规划](#目录规划)
- [全局配置（config.yaml）](#全局配置configyaml)
- [任务配置文件](#任务配置文件)
- [参数优先级](#参数优先级)
- [终端输出与降级](#终端输出与降级)
- [注意事项](#注意事项)

---

## 构建

```bash
make build            # 内嵌前端产物的单二进制 → ./dbx
make install          # 构建并安装到本机 /usr/local/bin（无权限自动 sudo）
make install PREFIX=$HOME/bin   # 安装到自定义目录
make uninstall        # 卸载本机安装
make release          # 跨平台 zip 打包 → release/dbx-<版本>-{os}-{arch}.zip
make release VERSION=v1.2.3   # 手动指定版本号（默认取 git tag/短哈希）
# 或仅构建后端：
go build -o dbx ./cmd
```

产物为**单二进制**，直接拷到目标服务器即可运行，无其他依赖。

**服务器部署（zip 包内为干净的 `dbx` + 安装脚本，解压即用）**：

```bash
# Linux / macOS
unzip dbx-<版本>-linux-amd64.zip -d dbx && cd dbx
./install.sh                  # 安装到 /usr/local/bin（无权限时自动 sudo）
./install.sh ~/bin            # 或安装到自定义目录
./dbx                     # 也可以不安装直接运行
```

```bat
:: Windows（install.bat 双击或 cmd 执行均可，无执行策略限制）
install.bat                   :: 安装到 %LOCALAPPDATA%\dbx 并加入用户 PATH
install.bat D:\dbx        :: 或安装到自定义目录
:: 安装后新开终端即可使用 dbx 命令；不安装也可直接 .\dbx.exe 运行
```

## 全局说明

```bash
dbx                      # 启动 Web 服务（默认端口 8181）
dbx version              # 查看版本号
dbx --port 9000          # 指定 Web 端口
dbx --data-dir /data/x   # 指定数据根目录（优先于全局配置）
dbx --config-file /etc/dbx.yaml   # 指定全局配置文件
dbx <命令> --help        # 查看任意命令帮助
```

> ⚠️ Web 模式下的 `--port` 是 **Web 服务端口**；`export` / `import` 子命令中的
> `--port` 是 **数据库端口**（mysqldump 风格别名），两者作用域不同。

所有执行类命令（export/import/migrate/compare）都支持三种参数来源：

| 来源 | 说明 |
|---|---|
| `--config xxx.yaml` | 独立配置文件，适合固化到脚本 / 版本库 |
| `--task <ID>` | 复用 Web 或 `task save` 保存的任务配置 |
| `--xxx` 命令行参数 | 一次性覆盖，仅显式给出的参数生效 |

> 连接名称支持中文 / 空格（如 `170 生产`），shell 中使用时注意加引号：
> `--source-conn "170 生产"`。

### 快捷输入：命令别名 / 短参 / Shell 补全

**命令别名**（与全名完全等价）：

| 全名 | 别名 | 全名 | 别名 |
|---|---|---|---|
| `export` | `exp` | `conn` | `cn` |
| `import` | `imp` | `task` | `tk` |
| `migrate` | `mig` | `history` | `his` |
| `compare` | `cmp` | `config` | `cfg` |
| 各命令的 `list` | `ls` | `delete` | `del`（原有） |

**常用短参：**

| 短参 | 等价长参 | 适用命令 |
|---|---|---|
| `-s` / `-t` | `--source-conn` / `--target-conn`（import 的 `-t` 为 `--target-conn`） | migrate / compare / export 的 `-s` / import 的 `-t` |
| `-T` | `--tables` | export / migrate / compare |
| `-o` / `-i` | `--output` / `--input` | export / import；`compare show` 的 `-i` 为 `--id`、`-t` 为 `--table` |
| `-h` `-P` `-u` `-p` | `--host` `--port` `--user` `--password`（mysqldump 风格，支持 `-p123456` 连写） | export / import |
| `-c` / `-n` | `--conn` / `--name` | conn test、delete / conn add |

> ⚠️ export / import 的 `-h` 是**主机**（mysqldump 习惯），这两个命令的帮助改用 `--help`；
> 其他命令 `-h` 仍为帮助。

```bash
# 短参示例：等价于 compare --source-conn 本地 --target-conn 本地 --tables ...
dbx cmp -s 本地 -t 本地 --source-database camunda --target-database camunda -T act_ge_property
dbx exp -s 生产库 --databases camunda -T t_user -o backup.zip
```

**Shell 自动补全**（一次配置，永久生效）：

```bash
# zsh（macOS 默认）
mkdir -p ~/.zsh/completions && dbx completion zsh > ~/.zsh/completions/_dbx
echo 'fpath=(~/.zsh/completions $fpath)' >> ~/.zshrc && echo 'autoload -Uz compinit && compinit' >> ~/.zshrc

# bash
dbx completion bash >> ~/.bashrc
```

配置后除子命令/flag 补全外，还支持**动态补全**：

- `--source-conn / --target-conn / --conn` 后 Tab → 列出已保存连接名
- `task run/show/delete --id` 后 Tab → 列出任务 ID；`--task` 按命令类型过滤
- `history show/delete --id`、`compare show --id` 后 Tab → 列出执行记录 ID
- `--scope / --type / --reset` 等枚举 flag 后 Tab → 列出可选值

---

## 运维实战场景

以下是从零部署到日常运维的完整操作序列，可直接参考使用。

### 场景 1：首次部署与初始化

```bash
# 1. 拷入二进制并安装（zip 解压后单文件，无依赖）
scp dbx-*-linux-amd64.zip ops@server:/opt/
ssh ops@server 'cd /opt && unzip dbx-*-linux-amd64.zip -d dbx && cd dbx && ./install.sh'

# 2. 登记常用连接（一次登记，后续所有命令复用；密码加密落盘）
dbx conn add --name "生产库" --type mysql --subtype "8.0" \
  --host 10.20.16.170 --port 3317 --un backup_user --pw 'xxx'
dbx conn add --name "测试库" --type mysql --host 10.20.16.20 --port 3306 --un root --pw 'xxx'

# 3. 验证连通性
dbx conn test --conn "生产库"

# 4.（可选）产物目录指到大容量数据盘
dbx config --gen > ~/.dbimpex/config.yaml
vi ~/.dbimpex/config.yaml        # 只改一行：exports: /data/dbx-exports
dbx config                   # 确认解析结果
```

### 场景 2：每日定时备份（cron）

```bash
# 1. 固化备份配置：生成模板后填写（引用已保存连接，不写密码）
dbx export --gen-config > /opt/dbx/daily-backup.yaml
```

```yaml
# /opt/dbx/daily-backup.yaml
source_ref: "生产库"
output: /data/backup/dbx/daily.zip   # 固定路径，便于清理脚本定位
databases:
  - camunda
compress: true
```

```bash
# 2. 先手动跑一次验证
dbx export --config /opt/dbx/daily-backup.yaml

# 3. 加入 crontab（凌晨2点；非交互环境自动降级为里程碑日志）
# crontab -e：
0 2 * * * /opt/dbx/dbx export --config /opt/dbx/daily-backup.yaml >> /var/log/dbx-backup.log 2>&1

# 4. 事后检查是否成功（失败记录会带错误信息）
dbx history list --type export
```

### 场景 3：备份验证（导入后自动对比）

```bash
# 向测试库导入数据后，逐行对比源与目标是否一致
# （保存的连接未配库时，用 --source-database/--target-database 指定）
dbx compare --source-conn "生产库" --target-conn "测试库" \
  --source-database camunda --target-database camunda \
  --tables act_ge_property,act_id_user --scope both
# 输出「（无差异）」即验证通过；有差异时列出具体表与缺失/多出行数
```

### 场景 4：灾备恢复 / 导入数据

```bash
# 清空重建（默认先自动备份原表；若导入失败，备份表保留可用于回滚）
dbx import --target-conn "测试库" --input /data/backup/dbx/daily.zip --reset truncate

# 确认风险后不备份直接覆盖（会打印黄色警告）
dbx import --target-conn "测试库" --input daily.zip --reset truncate --backup=false
```

> 备份表命名为 `__dbimpex_bak_{表名}`；任务**成功后自动清理**，
> 仅在失败时保留，可用 SQL 手动改名回滚。

### 场景 5：生产 → 测试环境同步（含跨库类型）

```bash
# 同名库直连迁移，先清空目标表（自动备份）
dbx migrate --source-conn "生产库" --target-conn "测试库" \
  --source-database camunda --target-database camunda \
  --tables orders,users --reset truncate

# 跨类型（mysql → postgresql）用配置文件更清晰：
dbx migrate --gen-config > sync-pg.yaml   # 填 source_ref/target_ref + 两侧库名
dbx migrate --config sync-pg.yaml
```

### 场景 6：版本发布前结构核对

```bash
# 只比结构（字段/索引差异），快速确认目标库是否需要 DDL
dbx compare --source-conn "生产库" --target-conn "测试库" \
  --source-database camunda --target-database camunda --scope structure

# 表名不一致时配对对比（改名后的表）
dbx compare --config release.yaml   # aliases: { old_orders: orders_v2 }
```

### 场景 7：故障排查

```bash
dbx history list                      # 找到失败记录（状态列为「失败」并附错误摘要）
dbx history show --id <ID>            # 看完整错误、执行进度与日志
dbx task show --id <任务配置ID>      # 复核当时使用的参数（yaml 全量输出）
```

### 场景 8：临时换一台机器 / 换目录运行

```bash
# 不改配置文件，单次指定数据根目录（连接/任务/历史/产物全套隔离）
dbx --data-dir /data/dbx-staging conn list
dbx --data-dir /data/dbx-staging export --config daily-backup.yaml
```

---

## 命令详解

### export 导出

将数据库结构与数据导出为 SQL 文件（每库一个 `库名.sql`，附 `.desc` 描述文件），可选 zip 打包。

```bash
dbx export [database [table...]] [flags]
```

**mysqldump 风格快速上手**（习惯 mysqldump 的用户可直接这样用）：

```bash
# 位置参数 = 库名 [表名...]；--no-data = 仅结构；--no-create-info = 仅数据
dbx export camunda \
  --source-type mysql --host 10.20.16.170 --port 3317 \
  --user root --password 'xxx' \
  --no-data --output ./camunda.zip
```

**flags：**

| flag | 说明 |
|---|---|
| `--config` | 配置文件（yaml），`--gen-config` 可生成模板 |
| `--gen-config` | 输出配置模板到标准输出 |
| `--task` | 已保存任务配置 ID |
| `--source-type` | 数据库类型：mysql / postgresql / oracle（内联连接必填） |
| `--source-host` / `--source-port` / `--source-un` / `--source-pw` / `--source-db` / `--source-subtype` | 内联源连接 |
| `--host` / `--port` / `--user` / `--password` / `--database` | mysqldump 风格别名，等价于上面 source-* |
| `--source-conn` | 使用已保存连接（ID 或名称），与 `--source-*` 同时给出时后者优先 |
| `--output` | 输出目录或 `.zip` 文件路径（以 .zip 结尾自动打包并重命名） |
| `--name` | 任务名（用于生成文件名） |
| `--databases` | 指定库，逗号分隔 |
| `--tables` | 指定表，逗号分隔 |
| `--objects` | 指定对象，格式 `_views/名称`，逗号分隔 |
| `--table-cond` | 表过滤条件 `表名:完整SELECT`，可重复（兼容旧格式 `表名:WHERE片段`） |
| `--schema-only` / `--no-data` | 仅导出结构（不导数据） |
| `--data-only` / `--no-create-info` | 仅导出数据（不导建表语句） |
| `--compress` | 打包为 zip，默认 true |
| `--batch-size` | 批量大小，默认 500 |

**示例：**

```bash
dbx export --gen-config > export.yaml          # 生成配置模板
dbx export --config export.yaml                # 按配置执行
dbx export --source-conn "170 生产" --databases camunda --output ./camunda.zip
dbx export camunda act_ge_property act_id_user \
  --source-conn 本地 --schema-only --compress=false --output ./schema
```

---

### import 导入

将 `.sql` 或 `.zip` 导入目标数据库。`.sql` 单文件直接导入目标库；
`.zip` 内每个根目录 `.sql` 文件对应一个库（文件名即库名）。

```bash
dbx import [flags]
```

**flags：**

| flag | 说明 |
|---|---|
| `--config` / `--gen-config` / `--task` | 同上 |
| `--target-type` / `--target-host` / `--target-port` / `--target-un` / `--target-pw` / `--target-db` / `--target-subtype` | 内联目标连接 |
| `--host` / `--port` / `--user` / `--password` / `--database` | mysqldump 风格别名，等价于上面 target-* |
| `--target-conn` | 使用已保存连接（ID 或名称） |
| `--input` | 导入文件（.sql 或 .zip），必填 |
| `--reset` | 重置模式：`truncate`（清空表）/ `drop`（删除重建），默认不重置 |
| `--backup` | 重置前创建备份表，默认 true |
| `--batch-size` | 批量大小，默认 500 |

**示例：**

```bash
dbx import --target-conn 本地 --input ./camunda.zip --reset truncate
dbx import --config import.yaml
```

> ⚠️ `--reset` 非空且 `--backup=false` 时，目标表现有数据将无法恢复，命令会打印黄色警告。

---

### migrate 迁移

源库 → 目标库直接迁移，**支持跨数据库类型**（如 mysql → postgresql）。

```bash
dbx migrate [flags]
```

**flags：** 在 import 的基础上增加源侧：

| flag | 说明 |
|---|---|
| `--source-*` / `--target-*` | 内联源 / 目标连接 |
| `--source-user` / `--source-password` / `--source-database` | mysqldump 风格别名（target- 同名系列同理）；其中 `--source-database` 还可给已保存连接指定库名 |
| `--source-conn` / `--target-conn` | 引用已保存连接 |
| `--tables` | 指定表，逗号分隔 |
| `--objects` | 指定对象（仅同类型迁移生效） |
| `--table-cond` | 表过滤条件 `表名:完整SELECT`，可重复 |
| `--schema-only` / `--data-only` | 仅结构 / 仅数据 |
| `--reset` / `--backup` / `--batch-size` | 同 import |

**示例：**

```bash
dbx migrate --config migrate.yaml
dbx migrate --source-conn "170 生产" --target-conn 本地 \
  --tables act_ge_property --reset truncate
```

---

### compare 对比

对比两个数据库的结构与数据差异，终端输出对齐着色的差异报告。

```bash
dbx compare [flags]
```

**flags：**

| flag | 说明 |
|---|---|
| `--config` / `--gen-config` / `--task` | 同上 |
| `--source-*` / `--target-*`、`--source-user` 等别名、`--source-conn` / `--target-conn` | 同 migrate |
| `--tables` | 指定对比的表，逗号分隔（默认全部） |
| `--alias` | 表别名配对 `源表=目标表`，可重复（不同名表对比） |
| `--scope` | 对比范围：`both` / `structure` / `data` |
| `--structure-only` / `--data-only` | 等价 `--scope structure` / `--scope data` |
| `--threshold` | 逐行比较阈值，默认 1000；单表行数超过阈值仅对比行数 |
| `--ignore-columns` | 数据内容对比忽略的列，逗号分隔（如 `created_at,updated_at`，列名大小写不敏感） |
| `--force-data` | 表结构不一致时仍强制对比数据（默认结构有差异则跳过数据对比） |
| `--output` | 报告 JSON 额外保存路径（历史记录已自动落盘一份，见下） |
| `--all` | 输出全部表（默认仅输出有差异的表） |

**示例：**

```bash
dbx compare --config compare.yaml
dbx compare --source-conn "170 生产" --target-conn 本地 \
  --tables act_ge_property,act_id_membership --scope both --output report.json
```

**报告输出说明：**

- 汇总行：总项数、一致、仅源有、仅目标有、结构差异、数据差异（彩色标注）
- 明细表：表名 / 状态 / 差异说明
  - 结构差异形如 `结构: +1 -2 ±3`（源独有列、目标独有列、定义不同列）
  - 数据差异形如 `缺失1行` / `多出2行` / `变化1行` / `行数 100 vs 98`（超阈值时）
- 默认只显示有差异的表，`--all` 显示全部

**结构对比语义：**

- 列类型对比前会归一化**整数类型的显示宽度**：`BIGINT(20)` ≡ `BIGINT`、`INT(11)` ≡ `INT` 等
  （MySQL 8.0.17 起废弃显示宽度，5.7↔8.0 跨版本写法不同但存储等价，不判为差异）；
  归一化复用 cydb 方言的 `NormalizeColumnType`（与自动迁移同一实现）；
  `ZEROFILL` 列的宽度有实际意义不归一化，`VARCHAR(64)`/`DECIMAL(10,2)` 等长度/精度属存储语义照常对比

**数据对比语义：**

- **结构不一致时默认跳过数据对比**（列定义都不同，数据对比意义有限），明细标注跳过原因；
  需要时可加 `--force-data` 强制对比（配置文件中 `force_data: true`）
- **有主键的表（含复合主键）**：按主键判断有无——源有目标无为「缺失」，目标有源无为「多出」；
  主键匹配但内容不同为「变化」（能精确定位到哪一行哪一列变了）
- **无主键的表**：回退为整行内容对比，无法区分「变化」与「缺失+多出」
- `created_at`/`updated_at` 等易变列可用 `--ignore-columns` 排除，避免噪声差异
  （忽略列仅影响数据内容对比，不影响结构对比；主键列不可忽略）

**对比完成后会输出记录 ID**，报告自动落盘到 `exports/compare-<ID>.json`，
可用 [compare show](#compare-show-查看差异明细) 随时回看差异明细。

#### compare show 查看差异明细

```bash
dbx cmp show -i <记录ID>                  # 全部表的差异摘要
dbx cmp show -i <记录ID> <表名>            # 单表差异明细（表名也可用 --table/-t 指定）
```

单表明细包含：**列级结构对照**（`+` 仅源有 / `-` 仅目标有 / `±` 定义不一致，附两侧完整定义）
与**数据差异**（行数、缺失/多出行数及样例行内容）：

```
表: act_id_membership

── 结构 ──
结构一致

── 数据 ──
行数: 源 1 / 目标 0
缺失 1 行（源有目标无）
缺失行样例（1）:
  1. user_id_=demo  group_id_=camunda-admin
```

> 记录 ID 来源：compare 执行后终端输出；或 `history list --type compare` 查找。
> CLI 与 Web 产生的记录都可回看。超阈值仅比行数的表，需要逐行明细时可调大
> `--threshold` 重新对比。

---

### config 全局配置

```bash
dbx config                     # 查看配置文件路径与四类目录解析结果
dbx config --gen               # 输出配置模板
dbx config --config-file /etc/dbx.yaml   # 指定配置文件查看
```

---

### conn 连接管理

管理已保存的数据库连接（Web 界面共享）。

```bash
dbx conn list                        # 列出连接（ID/名称/类型/地址）
dbx conn add --name 生产库 \
  --type mysql --subtype "8.0" \
  --host 10.20.16.170 --port 3317 --un root --pw 'xxx'
dbx conn add --id <ID> ...           # 带 --id 为更新已有连接
dbx conn test --conn "生产库"        # 测试连接可用性（接受 ID 或名称）
dbx conn delete --conn "生产库"      # 删除（别名 del）
```

| flag（add） | 说明 |
|---|---|
| `--name` | 连接名称（必填） |
| `--id` | 按 ID 更新已有连接 |
| `--type` | mysql / postgresql / oracle（必填） |
| `--host` / `--port` / `--un` / `--pw` / `--db` / `--subtype` | 连接信息 |

---

### task 任务配置

管理任务配置（与 Web 保存的任务互通）。

```bash
dbx task list                        # 列出（--type export|import|migrate|compare 过滤）
dbx task show --id <ID>              # 查看详情（yaml）
dbx task run --id <ID>               # 同步执行
dbx task save --name 每日对比 --config compare.yaml
dbx task save --name 导出 --config x.yaml --type export   # 手动指定类型
dbx task delete --id <ID>            # 删除（别名 del）
```

- `task save` 会自动识别配置文件的类型（compare/export/import/migrate），
  识别不准时可用 `--type` 强制指定
- `task run` 为**同步执行**，进度实时输出到终端

---

### history 执行历史

CLI 与 Web 的执行都会留下历史记录。

```bash
dbx history list                     # 列出（--type 过滤）
dbx history show --id <ID>           # 详情：进度/耗时/输出文件/错误/日志
dbx history delete --id <ID>         # 删除（别名 del；运行中的记录不允许删除）
```

---

## 目录规划

所有数据统一在数据根目录（默认 `~/.dbimpex`，可用 `--data-dir` 或全局配置覆盖）下：

```
~/.dbimpex/
├── connections.json / tasks.json / history.json  # ① 配置存储（根目录）
├── uploads/     # ③ Web 上传文件临时目录
├── tmp/         # ② 任务处理临时目录（zip 解压等，任务结束自动清理）
└── exports/     # ④ 最终生成产物目录（导出 zip/目录、对比报告 JSON）
```

四类目录均可通过全局配置文件独立指定（如把产物目录放到数据盘），见下节。

> 旧版本曾将导出产物放在二进制同级的 `.dbimpex-exports/`，现已统一到
> `exports/`；历史导出文件仍在原处，历史记录中的路径不受影响。

## 全局配置（config.yaml）

独立的全局配置文件，用于统一管理四类数据目录，无需每次传参：

```bash
dbx config --gen > ~/.dbimpex/config.yaml   # 生成模板
dbx config                                  # 查看当前解析结果
```

```yaml
# ~/.dbimpex/config.yaml
dirs:
  data: ""        # ① 配置保存目录，默认 ~/.dbimpex
  tmp: ""         # ② 任务处理临时目录，默认 <data>/tmp
  uploads: ""     # ③ 上传临时目录，默认 <data>/uploads
  exports: ""     # ④ 最终产物目录，默认 <data>/exports
```

**配置文件查找顺序**：`--config-file` 参数 > 环境变量 `DBIMPEX_CONFIG` > `~/.dbimpex/config.yaml`

**目录优先级**：`--data-dir` 参数 > 配置文件 `dirs.data` > 默认 `~/.dbimpex`；
其余三类目录：配置文件显式值 > 由 data 目录派生。留空的项自动派生，
因此只需填写想改的目录（典型场景：只把 `exports` 指到大容量数据盘）。

---

## 任务配置文件

四个执行命令都支持独立 YAML 配置文件，模板可由 `--gen-config` 生成：

```bash
dbx compare --gen-config > compare.yaml
```

### 连接段的两种写法

```yaml
# 写法一：内联连接
source:
  type: mysql
  subtype: "8.0"      # 可选
  host: 127.0.0.1
  port: 3306
  user: root
  password: ""
  database: ""        # 留空 = 由 databases 参数或库内全部库决定

# 写法二：引用已保存连接（dbx conn add 保存的 ID 或名称）
source_ref: "170 生产"
```

### compare.yaml 示例

```yaml
source_ref: "170 生产"
target_ref: "本地"
source_database: camunda     # 覆盖源库名（引用连接未配库时必填）
target_database: camunda
tables:                      # 留空 = 全部表
  - act_ge_property
  - act_id_membership
aliases:                     # 不同名表配对（源表: 目标表）
  old_table: new_table
scope: both                  # both | structure | data
threshold: 1000              # 超过阈值仅比行数
ignore_columns:              # 内容对比忽略的列（可选）
  - created_at
  - updated_at
force_data: false            # 结构不一致时仍强制对比数据（默认跳过）
output: report.json          # 可选
```

### export.yaml 示例

```yaml
source:
  type: mysql
  host: 127.0.0.1
  port: 3306
  user: root
  password: ""
output: ./export.zip         # .zip 路径自动打包；目录则不打包
name: myexport
databases:
  - db1
tables:
  - table_a
conditions:                  # 表级过滤条件 "表名:完整SELECT"
  - "table_a: SELECT * FROM table_a WHERE status = 1"
schema_only: false
data_only: false
compress: true
batch_size: 500
```

### import.yaml 示例

```yaml
target:
  type: mysql
  host: 127.0.0.1
  port: 3306
  user: root
  password: ""
input: ./export.zip
reset: ""                    # "" 直接追加 | truncate 清空表 | drop 删除重建
backup: true                 # 重置前创建备份表
batch_size: 500
```

### migrate.yaml 示例

```yaml
source_ref: "170 生产"
target_ref: "本地"
source_database: camunda
target_database: camunda
tables:
  - table_a
schema_only: false
data_only: false
reset: ""
backup: true
batch_size: 500
```

> 旧版 Web 风格的嵌套 camelCase 配置（`export:` / `compare:` 顶层段）仍可自动识别读取，
> 但新脚本建议使用上述独立格式。

## 参数优先级

同时给出多个来源时，后加载者整体生效：**`--config` 配置文件 → `--task` 任务配置 → 命令行 flag**（命令行优先级最高）。
命令行仅**显式给出**（Changed）的 flag 才会覆盖，未给出的保留配置取值。

以 export 为例：

```bash
# 以配置文件为基础，临时改导出其中两张表（其余项保持配置不变）
dbx export --config daily.yaml --tables orders,users
```

## 终端输出与降级

- **进度条**：TTY 下单行原地刷新（`[████░░] 62.3% 50/81 项 · 1.2万行 · 当前表`）；
  管道 / 重定向（非 TTY）时自动退化为每 20% 一条里程碑行，避免日志刷屏
- **颜色**：仅 TTY 启用；设置环境变量 `NO_COLOR=1` 可强制关闭，便于脚本消费输出
- **错误**：失败时输出中文错误信息与详情，进程退出码为 1

## 注意事项

1. **连接必须带库名**：`compare` / `migrate` 要求连接明确到库（引擎限制）。
   内联连接写 `database`；引用已保存连接时用 `--source-database` / `--target-database`
   （或配置文件 `source_database` / `target_database`）覆盖；
   否则报「请先选择对比的库」。export 不限：连接带库时导出该库，
   不带库时用 `--databases` 指定（可多库）。
2. **密码安全**：内联连接的密码以明文存于配置文件，注意文件权限，
   不要把含密码的配置提交到 git；推荐用 `conn add` 保存连接后以 `source_ref` 引用。
3. **重置即破坏**：`--reset truncate/drop`（import/migrate）会清空或删除目标表；
   默认 `--backup=true` 会先建备份表 `__dbimpex_bak_{表名}`，**任务成功后自动清理**，
   失败时保留可回滚；关闭备份前请确认风险。
4. **导出文件格式**：输出为 dbx 自有结构（`库名.sql` + `.desc` 描述文件），
   建表段与数据段分离；请用 `dbx import` 导入。flag 命名参考了 mysqldump
   习惯（--host/--user/--password/--no-data 等），但**文件格式不与 mysqldump 互通**。
5. **导入文件格式**：仅支持 dbx 导出格式。`.sql` 单文件导入目标库；
   `.zip` 内每个根目录 `.sql` 对应一个库（文件名即库名）。
6. **大表数据对比**：单表行数超过 `threshold`（默认 1000）时仅对比行数，
   需要逐行确认时可调大阈值（注意耗时）。
7. **task run 同步执行**：长任务期间终端被占用；CLI 不支持中途取消，
   用 Ctrl+C 终止（历史记录会保留已执行部分）。
8. **同名参数作用域**：根命令 `--port` 是 Web 端口；`export`/`import` 的
   `--port` 是数据库端口别名；`conn add` 的 `--host/--port` 等无前缀。
