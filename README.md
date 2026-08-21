# dqex - 数据库工作台

跨平台数据库导入 / 导出 / 迁移 / 对比 / 快照 / 数据字典 / SQL 查询工具，支持 MySQL、PostgreSQL、Oracle。

- **Web 界面**：可视化操作（查询终端 / 表浏览器 / AI 面板 / 快照与对比报告），实时进度，任务配置管理
- **CLI 命令行**：脚本化 / 定时任务 / 无图形界面服务器（含 Shell 补全）

## 快速开始

### 下载

从 [Releases](../../releases) 下载对应平台 zip 包并解压。

### Linux / macOS

```bash
./install.sh                    # 安装到 /usr/local/bin
dqex                             # 启动 Web 服务（127.0.0.1:8181）
# 或直接使用
./start.sh                      # 前台运行（Ctrl+C 停止）
./start.sh -d                   # 后台运行
./stop.sh                       # 停止后台服务
```

### Windows

双击或用 cmd 执行：

```bat
install.bat                     :: 安装到 %LOCALAPPDATA%\dqex 并加入 PATH
dqex                             :: 新开终端后启动 Web 服务
:: 或不安装直接使用：
start.bat                       :: 前台运行
start.bat -d                    :: 后台运行
stop.bat                        :: 停止后台服务
```

## 功能

| 功能 | Web | CLI 命令 | CLI 简写 | 说明 |
|---|---|---|---|---|
| 导出 | ✅ | export | `exp` | 数据库结构与数据 → SQL 文件（zip/gzip） |
| 导入 | ✅ | import | `imp` | SQL/zip 文件 → 数据库 |
| 迁移 | ✅ | migrate | `mig` | 数据库 → 数据库（支持跨类型） |
| 对比 | ✅ | compare | `cmp` | 两个数据库结构与数据差异 |
| 数据字典 | ✅ | dictionary | `dict` | 表结构 + 注释 → Excel（.xlsx） |
| 快照 | ✅ | snapshot | `snap` | 库结构/数据快照，支持 create/list/show/delete/compare（CLI + Web 全链路） |
| SQL 查询 | ✅ | sql | — | CLI：交互式 REPL / 单次执行 / JSON 输出；Web：查询终端 + 表浏览器；按目标库方言原生执行 |
| AI 辅助写 SQL | ✅ | sql 内 `\ai` | — | 可选模块：自动查询真实库表结构后生成 SQL（按目标方言），写操作需确认，未配置无入口 |
| 连接管理 | ✅ | conn | `cn` | 保存/测试/删除数据库连接 |
| 任务配置 | ✅ | task | `tk` | 保存/复用任务配置（`--task <ID>` 或 Web 一键执行） |
| 执行历史 | ✅ | history | `his` | 查看/删除执行记录，失败排查 |
| 全局配置 | ✅ | config | `cfg` | 查看数据目录等全局配置 |
| 访问链接 | — | url | — | 输出带 token 的 Web 访问链接（供 curl / 脚本） |
| 版本 | — | version | `v` | 查看版本号 |
| Shell 补全 | — | completion | — | 生成 zsh / bash 补全脚本 |

**AI 辅助写 SQL（可选模块，CLI `\ai` 与 Web AI 面板功能一致）**：

- **真实表结构**：生成前自动查询库表结构，杜绝凭想象生成；Web 端实时显示查询进度
- **生成 ≠ 执行**：AI 只产出 SQL 文本，不直接执行；写操作需确认、危险语句被拦截，结果可 `\e` 编辑后执行
- **补全与应用**：`\ai continue` 基于上文续写；Web 端支持生成 / 解释 / 优化 / 修复，结果 diff 预览后一键应用到编辑器（替换选中 / 插入光标 / 追加）
- **密钥不出本机**：API Key 存本地，界面仅显示掩码后的端点与模型名
- **未配置无入口**：BaseURL / API Key / Model 三项配置齐全才启用，不影响任何既有功能

```bash
# CLI 示例
dqex conn add --name 生产库 --type mysql --host 10.20.16.170 --port 3317 --un root --pw 'xxx'
dqex exp camunda -s 生产库 -o backup.zip
dqex dict camunda -s 生产库 -o dict.xlsx
dqex cmp -s 生产库 -t 测试库 --source-database camunda --target-database camunda --scope both

# SQL 终端（按目标库方言原生执行）
dqex sql -c 生产库                 # 交互式 REPL
dqex sql -c 生产库 --json "SELECT id,name FROM users"   # 智能体友好 JSON 输出

# 快照
dqex snapshot create -c 生产库 -n 早盘
dqex snapshot compare -c 生产库 --a 早盘 --b 午盘
```

## 构建

```bash
# 开发
make dev               # Go :8181 + Vite :5281，支持调试

# 构建
make build             # 单二进制 ./dqex（内嵌前端）
make release           # 跨平台打包 → release/

# 安装
make install           # → /usr/local/bin
```

## 技术栈

- 后端：Go + [infrakit](https://github.com/fj1981/infrakit)（数据库方言适配）
- 前端：React + TypeScript + Vite + Tailwind CSS + shadcn/ui
- Excel：excelize（纯 Go，无 CGO）

## 文档

- [CLI 使用手册](CLI.md)
- [开发约定](docs/conventions.md)（状态建模、全链路数据流等工程约定，改动前必读）
