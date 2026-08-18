# dbx - 数据库工作台

跨平台数据库导入 / 导出 / 迁移 / 对比 / 数据字典工具，支持 MySQL、PostgreSQL、Oracle。

- **Web 界面**：可视化操作，实时进度，任务配置管理
- **CLI 命令行**：脚本化 / 定时任务 / 无图形界面服务器

## 快速开始

### 下载

从 [Releases](../../releases) 下载对应平台 zip 包并解压。

### Linux / macOS

```bash
./install.sh                    # 安装到 /usr/local/bin
dbx                             # 启动 Web 服务（127.0.0.1:8181）
# 或直接使用
./start.sh                      # 前台运行（Ctrl+C 停止）
./start.sh -d                   # 后台运行
./stop.sh                       # 停止后台服务
```

### Windows

双击或用 cmd 执行：

```bat
install.bat                     :: 安装到 %LOCALAPPDATA%\dbx 并加入 PATH
dbx                             :: 新开终端后启动 Web 服务
:: 或不安装直接使用：
start.bat                       :: 前台运行
start.bat -d                    :: 后台运行
stop.bat                        :: 停止后台服务
```

## 功能

| 功能 | Web | CLI 简写 | 说明 |
|---|---|---|---|
| 导出 | export | `exp` | 数据库结构与数据 → SQL 文件（zip/gzip） |
| 导入 | import | `imp` | SQL/zip 文件 → 数据库 |
| 迁移 | migrate | `mig` | 数据库 → 数据库（支持跨类型） |
| 对比 | compare | `cmp` | 两个数据库结构与数据差异 |
| 数据字典 | dictionary | `dict` | 表结构 + 注释 → Excel（.xlsx） |
| 快照 | snapshot | `shot` | 库结构/数据快照，支持 create/list/show/delete/compare（CLI + Web 全链路） |
| SQL 终端 | sql | — | 交互式 REPL / 单次执行 / JSON 输出，MySQL 语法自动翻译到 PG/Oracle |
| AI 辅助写 SQL | — | — | 可选模块：自然语言生成 SQL，复用安全链路，未配置无入口 |

```bash
# CLI 示例
dbx conn add --name 生产库 --type mysql --host 10.20.16.170 --port 3317 --un root --pw 'xxx'
dbx exp camunda -s 生产库 -o backup.zip
dbx dict camunda -s 生产库 -o dict.xlsx
dbx cmp -s 生产库 -t 测试库 --source-database camunda --target-database camunda --scope both

# SQL 终端（用 MySQL 语法操作 PG/Oracle）
dbx sql -c 生产库                 # 交互式 REPL
dbx sql -c 生产库 --json "SELECT id,name FROM users"   # 智能体友好 JSON 输出

# 快照
dbx snapshot create -c 生产库 -n 早盘
dbx snapshot compare -c 生产库 --a 早盘 --b 午盘
```

## 与同类工具的对比（基于真实实现）

> 定位说明：dbx 是**单机运维型**工具（数据存本地 `~/.dbimpex`，连接令牌 + 白名单）。
> 下列对比按「各自实际能力」客观呈现，不夸大也不贬低。

| 维度 | dbx | mycli / usql（CLI 系） | DBeaver（免费） | Navicat（商业） | DataGrip（商业） |
|---|---|---|---|---|---|
| 交互形态 | CLI + Web 双形态共用同一数据 | 仅 CLI | 仅 GUI | 仅 GUI | 仅 GUI |
| 跨方言语法翻译 | ✅ MySQL 语法自动翻译到 PG/Oracle（AST 级） | ❌ 各自独立 | ❌ 按库切换方言 | ❌ 按库切换方言 | ⚠️ 部分（自动识别但需手动） |
| 支持数据库 | MySQL / PG / Oracle（含 OceanBase/GaussDB/Dameng 子类型） | 按工具单库（mycli=MySQL, usql=多库但无翻译） | 上百种（JDBC） | 数十种 | 数十种 |
| 数据导入导出/迁移/对比 | ✅ 内置全链路 | ❌ 无 | ✅（GUI 向导） | ✅（强项） | ⚠️ 较弱 |
| 快照与结构对比 | ✅ CLI + Web 全链路 | ❌ 无 | ⚠️ 部分（ER/对比） | ✅ | ✅ |
| AI 辅助写 SQL | ✅ 可选模块，生成≠执行，只读探索 | ❌ 无 | ❌ 无 | ⚠️ 新版有（付费） | ⚠️ 新版 AI（订阅） |
| SQL 补全 | ✅ 关键字/表/列/别名感知（CLI） | ✅ 基础补全 | ✅ | ✅ | ✅ 神级补全 |
| 写操作安全护栏 | ✅ 三层确认 + 危险函数拦截 + 审计 | ⚠️ mycli 基础确认 | ⚠️ 有确认弹窗 | ⚠️ 有确认弹窗 | ⚠️ 有确认弹窗 |
| 多用户/协作/权限 | ❌ 单机定位 | ❌ | ❌（社区版） | ✅ 企业版 | ❌ |
| 价格 | 自托管（依赖内部 pk-infrakit-g 组件） | 开源免费 | 开源免费 | 商业付费 | 商业订阅 |

**结论（事实层面）**：
- 在「同定位单机工具」里，dbx 的**跨方言翻译 + 双形态 + 安全细节 + AI 增量**是真实差异化优势。
- 在「通用数据库管理」大盘里，dbx 的**数据库种类广度、多用户协作、生态插件**不及 DBeaver/Navicat/DataGrip，但核心运维场景的体验细节更优。
- dbx 当前**没有自身定位内的实打实缺陷**；此前常见的「不足」多源于拿商业全功能工具的尺子衡量单机工具，或对未核实功能的误判。

## 构建

```bash
# 开发
make dev               # Go :8181 + Vite :5281，支持调试

# 构建
make build             # 单二进制 ./dbx（内嵌前端）
make release           # 跨平台打包 → release/

# 安装
make install           # → /usr/local/bin
```

## 技术栈

- 后端：Go + [pk-infrakit-g](https://gitlab.mycyclone.com/rpa-platform/pk-infrakit-g)（数据库方言适配）
- 前端：React + TypeScript + Vite + Tailwind CSS + shadcn/ui
- Excel：excelize（纯 Go，无 CGO）

## 文档

- [CLI 使用手册](CLI.md)
- [开发约定](docs/conventions.md)（状态建模、全链路数据流等工程约定，改动前必读）
