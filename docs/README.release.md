# dqex 数据库工作台

dqex 是一款跨平台数据库工作台：单个静态二进制交付，无需安装任何依赖，支持**导出、导入、迁移、对比、快照、数据字典、SQL 终端与 AI 辅助写 SQL**，同时提供 Web 界面与命令行（CLI）两种使用方式。

- 支持数据库：MySQL、PostgreSQL、Oracle（跨类型迁移自动做方言转换）
- 支持平台：macOS、Linux、Windows（纯静态二进制，可离线使用）

---

## 系统要求

- 操作系统：macOS（Intel / Apple Silicon）、Linux（x86_64 / arm64）、Windows 10 及以上 64 位
- 无需安装任何运行时或依赖（不含 JVM、Node.js 等），可完全离线使用
- 磁盘占用约 90 MB（解压后），内存占用约 50–60 MB

---

## 快速开始

解压 zip 后即可直接使用，无需安装。

### 方式一：直接运行（推荐）

**Linux / macOS：**

```bash
./start.sh              # 前台运行（Ctrl+C 停止）
./start.sh -d           # 后台运行（关终端不中断）
./stop.sh               # 停止后台服务
./url.sh                # 查看 Web 访问链接
```

**Windows（双击或 cmd 执行）：**

```bat
start.bat               :: 前台运行（关窗口即停）
start.bat -d            :: 后台运行（关窗口不中断）
stop.bat                :: 停止后台服务
```

启动后浏览器自动打开 Web 界面（默认 http://127.0.0.1:8181，仅本机访问，本机免认证）。

### 方式二：安装到系统（可选）

**Linux / macOS：**

```bash
./install.sh            # 安装到 /usr/local/bin（无写权限时自动 sudo）
dqex                    # 任意目录直接运行
```

**Windows：**

```bat
install.bat             :: 安装到 %LOCALAPPDATA%\dqex 并加入 PATH
dqex                    :: 新开终端后运行
```

**卸载：** 删除安装目录中的 dqex 可执行文件即可（如 `rm /usr/local/bin/dqex`，Windows 为删除 `%LOCALAPPDATA%\dqex`）；连接、任务、历史等数据保留在数据目录（默认 `~/.dqex`），如需彻底清除一并删除该目录。

**升级：** 解压新版本 zip 后重新执行安装脚本，或直接覆盖原有 dqex 可执行文件；数据与配置不受影响。

---

## 使用入门

### Web 界面

运行 `dqex` 后浏览器自动打开，在界面中：

1. 先到「连接」页添加数据库连接
2. 再到「查询」页执行 SQL（支持表浏览器、AI 辅助）
3. 导出 / 导入 / 迁移 / 对比 / 快照 / 数据字典等功能均在左侧菜单中

### CLI 30 秒上手

```bash
# 保存连接（一次保存，随处复用）
dqex conn add --name 生产库 --type mysql --host 10.20.16.170 --port 3317 --un root --pw 'xxx'

# 导出（结构 + 数据，支持条件过滤与 gzip 压缩）
dqex export --source-conn 生产库 --databases camunda -o ./camunda.zip

# 导入
dqex import --target-conn 本地测试库 --input ./camunda.zip --reset truncate

# 迁移（支持跨数据库类型，如 MySQL → PostgreSQL）
dqex migrate --source-conn 生产库 --target-conn 测试库 \
  --source-database camunda --target-database camunda

# 对比（结构与数据差异）
dqex compare --source-conn 生产库 --target-conn 测试库 \
  --source-database camunda --target-database camunda --scope both

# 数据字典（表结构 + 注释 → Excel）
dqex dictionary camunda -s 生产库 -o ./数据字典.zip

# 快照与对比（变更前后对比，快速定位漂移）
dqex snapshot create -c 生产库 -n 基线
dqex snapshot compare -c 生产库 --a 基线 --b 部署后

# 交互式 SQL 终端（按目标库方言原生执行）
dqex sql -c 生产库
```

### AI 辅助写 SQL（可选）

配置 BaseURL / API Key / Model 三项后自动启用，未配置时入口隐藏、不影响其他功能：

- **终端内**：`dqex sql` 进入后输入 `\ai 你的需求`，如 `\ai 统计每个部门的人数`
- **Web 界面**：查询页 AI 面板，支持生成 / 解释 / 优化 / 修复

AI 只生成 SQL 文本，写操作仍需确认，危险语句会被拦截；API Key 只保存在本机，不会上传。

---

## 局域网使用

默认仅本机可访问（127.0.0.1:8181）。如需让同网段的其他电脑通过浏览器访问 Web 界面：

### 1. 启动时监听所有网卡并配置白名单（必填）

```bash
dqex --host 0.0.0.0 --allow 192.168.1.0/24            # 前台运行
./start.sh -d --host 0.0.0.0 --allow 192.168.1.0/24   # 后台运行（Windows: start.bat -d --host 0.0.0.0 --allow 192.168.1.0/24）
```

> **白名单必填**：对外暴露（监听非 127.0.0.1）时必须配置 `--allow`，否则外部来源一律被拒绝（仅本机可访问）。白名单支持 IP / 网段（CIDR）/ 域名，逗号分隔，把需要访问的机器 IP 或所在网段加进去即可；本机回环始终放行，不受白名单约束。

### 2. 分发访问链接

启动后执行 `dqex url`（或 `./url.sh`），输出带令牌的局域网访问链接，把链接发给白名单内的电脑，在浏览器打开即可：

```
http://192.168.1.100:8181/?token=xxxxxxxx
```

> 令牌有效期 24 小时，过期后重新执行 `dqex url` 获取新链接；服务重启令牌也会刷新。

### 3. 防火墙放行端口

- **Linux**：`firewall-cmd --add-port=8181/tcp --permanent`（或 `ufw allow 8181`）
- **Windows**：首次启动防火墙弹窗时勾选「允许访问」，或手动添加入站规则放行 TCP 8181
- **macOS**：系统设置 → 网络 → 防火墙 → 允许 dqex 接受传入连接

### 安全说明

- **访问控制**：对外暴露（非 127.0.0.1 监听）**必须配置 `--allow` 来源白名单**（IP / CIDR / 域名）；未配置时外部来源一律拒绝（HTTP 403），仅本机可访问
- **默认认证模式**（不带 `--no-auth`）：白名单内的外部来源仍需**令牌认证**；连续 1 分钟失败 10 次，该来源将被锁定 5 分钟
- **免认证模式**（`--no-auth`）：白名单内免认证直连，仅建议在可信内网使用，如 `--no-auth --allow 192.168.1.0/24`

---

## 完整使用手册

所有命令、参数与详细说明见同目录下的 [CLI.md](CLI.md)，涵盖以下内容：

| 章节 | 内容 |
|---|---|
| 全局说明 | Web 服务启动参数与安全说明、CLI 命令的三种参数来源、命令别名与快捷输入、Shell 补全 |
| 运维场景 | 连接管理、定时备份（cron）、灾备恢复、跨类型同步、对比验证、数据字典、故障排查 |
| SQL 终端 | 交互式 REPL、元命令（`\dt` `\d` `\e` `\copy` 等）、单次执行与管道、JSON 输出、常用 flag |
| 快照 | `snapshot` 创建 / 列表 / 查看 / 删除 / 对比，变更前后快速定位结构或数据漂移 |
| AI 辅助 | `\ai` 生成 / 解释 / 优化 / 修复 / 配置，密钥仅存本机 |
| 命令详解 | export / import / migrate / compare / dictionary / config / conn / task / history 全部参数与示例 |
| 配置文件 | 全局配置（config.yaml）与任务配置文件示例 |
| 注意事项 | 参数优先级、密码安全、重置备份、导出格式等 |

---

## 常见问题

- **端口被占用**：先执行 `./stop.sh`（Windows 为 `stop.bat`），或 `dqex --port <端口>` 指定其他端口
- **忘记 Web 访问地址**：执行 `dqex url` 查看带令牌的访问链接
- **数据存放位置**：连接、任务、执行历史等数据默认存于 `~/.dqex`（Windows 为 `%USERPROFILE%\.dqex`）
- **导出文件格式**：dqex 自有结构（`库名.sql` + `.desc`），请用 `dqex import` 导入，与其他工具不互通
- **重置即破坏**：`--reset truncate/drop` 会清空或删除目标表，默认先自动备份，确认后再使用
