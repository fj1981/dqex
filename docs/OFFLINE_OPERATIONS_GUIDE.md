# dqex 离线运维工具包使用指南

## 🎯 适用场景

本指南针对**完全隔离网络环境**下的数据库运维场景,例如:
- 银行/政府数据中心 (无互联网访问)
- 工厂车间服务器 (无桌面环境)
- 客户现场紧急故障排查 (时间紧迫,资源有限)

---

## 📦 工具包组成

```
dqex-offline-kit/
├── dqex                    # 主程序 (<50MB, 单二进制文件)
├── README_OFFLINE.md       # 本文档
└── templates/              # 自定义模板目录 (可选)
    └── custom.json         # 用户自定义SQL模板
```

### 核心特性

✅ **真正零依赖**
- 无需 JVM / Python / Node.js
- 无需 WebView / Docker / K8s
- U盘随身携带,双击即运行

✅ **双模式支持**
- CLI 模式: 适用于纯命令行环境 (SSH/Linux Server Core)
- Web UI 模式: 有浏览器时可视化操作 (`./dqex` 启动本地服务)

✅ **离线 AI 能力**
- Level 1: React Agent (需联网 + API Key)
- Level 2: 规则引擎 + 模板库 (完全离线) ← **本场景重点**
- Level 3: 传统 CLI (Tab补全 + 元命令)

---

## 🚀 快速开始

### 1. 准备工具包

```bash
# 从公司内网服务器下载 (或U盘拷贝)
cp /mnt/shared/dqex-offline-kit.zip /tmp/
cd /tmp && unzip dqex-offline-kit.zip
cd dqex-offline-kit
```

### 2. 启动服务

#### 方式A: CLI 模式 (推荐用于 SSH/无GUI环境)

```bash
# 交互式 SQL 终端
./dqex sql -c 生产库

# 或直接执行单次查询
./dqex sql -c 生产库 -e "SELECT COUNT(*) FROM users"
```

#### 方式B: Web UI 模式 (有浏览器时)

```bash
# 启动本地 Web 服务
./dqex

# 浏览器访问
# http://127.0.0.1:8181?token=<自动生成的令牌>

# 查看访问链接
./dqex url
```

---

## 💡 离线 AI 功能详解

### 三层降级策略

```
┌─────────────────────────────────────────────────────┐
│              AI 能力层级                              │
├──────────────┬──────────────┬───────────────────────┤
│   Level 1    │   Level 2    │      Level 3          │
│ React Agent  │  规则引擎     │     传统 CLI           │
│ (在线)       │  (离线)      │    (完全离线)          │
├──────────────┼──────────────┼───────────────────────┤
│ • 动态探索   │ • 50+模板库  │ • Tab 自动补全         │
│ • 自然语言   │ • 智能推荐   │ • 元命令 (\dt, \d)    │
│ • 多轮对话   │ • 语法检查   │ • 历史记录 (Ctrl+R)   │
│ • 需要API    │ • 无需联网   │ • 危险SQL拦截          │
└──────────────┴──────────────┴───────────────────────┘
```

### Level 2: 离线模板系统 (核心功能)

#### 查看所有可用模板

```sql
-- 在 dqex sql 终端中执行
\templates

-- 按分类查看
\templates query        -- 基础查询类
\templates aggregation  -- 聚合统计类
\templates filter       -- 条件过滤类
\templates join         -- 关联查询类
```

输出示例:
```
📚 可用SQL模板:

🔍 基础查询:
  select_all           全表查询
    示例: \template select_all users 100
  select_columns       指定列查询
    示例: \template select_columns "id,name,email" users 100

📊 聚合统计:
  count_rows           行数统计
    示例: \template count_rows users
  top_n                Top N 查询
    示例: \template top_n amount orders 10
  group_by_count       分组计数
    示例: \template group_by_count status orders

🎯 条件过滤:
  recent_days          最近N天数据
    示例: \template recent_days orders created_at 30
  where_equal          等值过滤
    示例: \template where_equal users status active 100

🔗 关联查询:
  join_two_tables      两表JOIN查询
    示例: \template join_two_tables orders users user_id id 100
```

#### 使用模板生成 SQL

```sql
-- 示例1: 查询销量Top 10的商品
\template top_n amount orders 10

-- 输出:
✓ 生成的SQL:
SELECT * FROM orders ORDER BY amount DESC LIMIT 10
提示: 使用 \g 执行，或 \e 编辑后执行

-- 示例2: 查询最近30天的订单
\template recent_days orders created_at 30

-- 输出:
✓ 生成的SQL:
SELECT * FROM orders WHERE created_at >= DATE_SUB(NOW(), INTERVAL 30 DAY) ORDER BY created_at DESC

-- 示例3: 检查重复邮箱
\template check_duplicate users email

-- 输出:
✓ 生成的SQL:
SELECT email, COUNT(*) AS cnt FROM users GROUP BY email HAVING cnt > 1 ORDER BY cnt DESC
```

#### 智能模板推荐

根据关键词自动推荐相关模板:

```sql
-- 输入包含"top" → 推荐 top_n
\ai "查询销量最高的商品"  -- 如果AI未配置,自动降级为模板推荐

-- 输入包含"最近" → 推荐 recent_days, this_month
\templates  -- 手动查看时也会显示常用模板
```

---

## 🛠️ 典型运维场景实战

### 场景1: 数据字典导出 (合规审计)

**需求**: 客户要求导出所有表结构为 Excel 格式,用于合规审计

```bash
# CLI 方式
./dqex dict camunda -s 生产库 -o data_dict.xlsx

# Web UI 方式
# 1. ./dqex 启动服务
# 2. 浏览器访问 http://127.0.0.1:8181
# 3. 点击"数据字典" → 选择数据库和表 → 生成Excel
```

**输出**: 
- `data_dict.xlsx` (包含概览Sheet + 分库详情Sheet)
- 自动列宽调整 + 表头冻结
- 符合金融/政府审计要求

---

### 场景2: 快照对比 (故障排查)

**需求**: 昨晚系统正常,今天早上发现数据异常,需要对比两个时间点的数据差异

```bash
# Step 1: 创建昨晚的快照 (假设已有)
./dqex snapshot create -c 生产库 -n 昨晚_20260820

# Step 2: 创建当前快照
./dqex snapshot create -c 生产库 -n 今早_20260821

# Step 3: 对比两个快照
./dqex snapshot compare -c 生产库 --a 昨晚_20260820 --b 今早_20260821

# Web UI 方式 (更直观)
# 1. 快照页面查看历史快照列表
# 2. 选择两个快照点击"对比"
# 3. 可视化差异报告:
#    - 结构差异: +新增列 -删除列 ±修改列
#    - 数据差异: 行数变化/样本数据对比
```

**输出示例**:
```
差异明细:
库 camunda ↔ camunda
  − act_ru_task (仅快照有)
  ✗ act_hi_procinst 结构差异 (+2 -1 ±3)
  ✗ act_ru_execution 数据差异 (快照1000 → 当前950)
```

---

### 场景3: 跨方言迁移 (MySQL → PostgreSQL)

**需求**: 客户从 MySQL 迁移到 PostgreSQL,需要自动转换SQL方言

```bash
# CLI 方式
./dqex migrate \
  --source-conn MySQL生产库 \
  --target-conn PG测试库 \
  --source-database camunda \
  --target-database camunda \
  --reset drop-and-create \
  --backup true

# Web UI 方式
# 1. 迁移页面选择源/目标连接
# 2. 勾选要迁移的表和对象
# 3. 选择重置模式 (保留数据/清空/重建)
# 4. 点击"开始迁移"
# 5. 实时进度条 + 详细日志
```

**自动处理**:
- ✅ MySQL `AUTO_INCREMENT` → PostgreSQL `SERIAL`
- ✅ MySQL `DATETIME` → PostgreSQL `TIMESTAMP`
- ✅ MySQL 反引号 `` `table` `` → PostgreSQL 双引号 `"table"`
- ✅ 函数差异: `NOW()` → `CURRENT_TIMESTAMP`

---

### 场景4: 性能问题诊断

**需求**: 某查询执行缓慢,需要分析执行计划并优化

```sql
-- Step 1: 使用模板生成 EXPLAIN 查询
\template explain_query orders user_id 12345

-- 输出:
EXPLAIN SELECT * FROM orders WHERE user_id = '12345'

-- Step 2: 执行并查看结果
\g

-- Step 3: 根据执行计划建议索引
\template suggest_index orders user_id

-- 输出:
-- 建议在 orders.user_id 上创建索引
CREATE INDEX idx_orders_user_id ON orders(user_id);

-- Step 4: 执行索引创建 (需确认)
\g
```

---

## 📋 完整命令速查表

### CLI 元命令

| 命令 | 说明 | 示例 |
|------|------|------|
| `\dt` | 列出所有表 | `\dt` |
| `\d <表名>` | 查看表结构 | `\d users` |
| `\d+ <表名>` | 查看表结构+索引 | `\d+ users` |
| `\l` | 列出所有数据库 | `\l` |
| `\c <库名>` | 切换数据库 | `\c test_db` |
| `\timing` | 开关执行计时 | `\timing` |
| `\x [on\|off\|auto]` | 扩展显示模式 | `\x on` |
| `\g` | 执行缓冲区SQL | `\g` |
| `\G` | 垂直显示执行结果 | `\G` |
| `\e` | 编辑缓冲区SQL | `\e` |
| `\copy <文件>` | 导出结果为CSV | `\copy result.csv` |
| `\i <文件>` | 执行SQL文件 | `\i script.sql` |
| `\template <ID>` | 应用离线模板 | `\template top_n amount orders 10` |
| `\templates` | 列出所有模板 | `\templates aggregation` |
| `\ai <需求>` | AI生成SQL (需联网) | `\ai 查询最近30天订单` |
| `\h` | 显示帮助 | `\h` |
| `\q` | 退出 | `\q` |

### 快捷键

| 快捷键 | 功能 |
|--------|------|
| `Enter` (带`;`) | 执行SQL |
| `Enter` (无`;`) | 多行续写 |
| `Ctrl+R` | 搜索历史 |
| `Tab` | 自动补全 (表名/字段名/关键字) |
| `Ctrl+C` | 取消输入 |
| `Ctrl+D` | 退出 |

---

## 🔧 高级配置

### 自定义SQL模板

创建 `templates/custom.json`:

```json
[
  {
    "id": "my_custom_query",
    "name": "我的自定义查询",
    "description": "查询特定业务的订单",
    "pattern": "SELECT * FROM orders WHERE business_type = '{type}' AND status = '{status}' LIMIT {n}",
    "example": "\\template my_custom_query ecommerce active 100",
    "category": "filter"
  }
]
```

加载自定义模板:

```bash
# 启动时指定模板目录
./dqex sql -c 生产库 --template-dir ./templates
```

### 环境变量配置

```bash
# 指定配置文件路径
export DQEX_CONFIG=/opt/dqex/config.yaml

# 指定数据目录
export DQEX_DATA_DIR=/opt/dqex/data

# 禁用颜色输出 (适用于日志重定向)
export NO_COLOR=1
```

---

## ⚠️ 注意事项

### 安全提醒

1. **危险SQL拦截**: dqex 会自动拦截以下操作并要求确认:
   - `DROP TABLE` / `TRUNCATE`
   - `DELETE` 无 `WHERE` 子句
   - `UPDATE` 无 `WHERE` 子句

2. **备份优先**: 执行写操作前,建议先创建快照:
   ```bash
   ./dqex snapshot create -c 生产库 -n 操作前备份
   ```

3. **权限最小化**: 建议使用只读账号进行查询,写操作使用专用账号

### 性能优化

1. **大表查询**: 默认自动添加 `LIMIT 1000`,可通过 `\x auto` 启用超宽自动降级

2. **批量导出**: 大数据量导出时使用 gzip 压缩:
   ```bash
   ./dqex exp camunda -s 生产库 -o backup.sql.gz --gzip
   ```

3. **并发控制**: 避免同时执行多个重型查询,防止数据库负载过高

---

## 🆘 常见问题

### Q1: 如何在不联网的情况下使用AI功能?

**A**: 使用 Level 2 离线模板系统:
```sql
\templates              -- 查看所有模板
\template top_n amount orders 10  -- 应用模板
```

### Q2: 如何在无桌面的 Linux Server 上使用?

**A**: 使用 CLI 模式:
```bash
./dqex sql -c 生产库
```

### Q3: 导出的Excel文件在哪里?

**A**: 默认在当前目录,或通过 `-o` 参数指定:
```bash
./dqex dict camunda -s 生产库 -o /tmp/data_dict.xlsx
```

### Q4: 如何查看历史执行记录?

**A**: 
```bash
# CLI 方式
./dqex history list

# Web UI 方式
# 右侧面板 → 历史记录 Tab
```

### Q5: 忘记密码怎么办?

**A**: 配置文件中的密码已加密存储,无法直接查看。需要重新配置连接:
```bash
./dqex conn add --name 生产库 --type mysql --host 10.20.16.170 --port 3317 --un root --pw '新密码'
```

---

## 📞 技术支持

- **内部Wiki**: http://wiki.company.com/dqex
- **邮件支持**: db-support@company.com
- **紧急联系**: +86-XXX-XXXX-XXXX (7×24小时)

---

## 📝 更新日志

### v0.6.0 (2026-08-21)
- ✅ 新增离线SQL模板系统 (50+预置模板)
- ✅ 新增 `\template` 和 `\templates` 元命令
- ✅ 优化 Tab 自动补全性能
- ✅ 修复快照对比在多库场景下的显示问题

### v0.5.0 (2026-08-20)
- ✅ 端口占用检测与自动恢复
- ✅ `dqex stop` 命令支持

---

**最后更新**: 2026-08-21  
**维护团队**: 数据库平台组
