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

```bash
# CLI 示例
dbx conn add --name 生产库 --type mysql --host 10.20.16.170 --port 3317 --un root --pw 'xxx'
dbx exp camunda -s 生产库 -o backup.zip
dbx dict camunda -s 生产库 -o dict.xlsx
dbx cmp -s 生产库 -t 测试库 --source-database camunda --target-database camunda --scope both
```

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
