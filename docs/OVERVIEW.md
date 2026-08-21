# dqex 项目概览

> **The AI-Native, Offline-First Database Workbench**  
> 从办公室到离线数据中心，从桌面到服务器核心 —— 一个工具，零依赖，全场景覆盖。

---

## 🎯 一句话定位

**dqex 是能在多种环境下运行的 AI 原生数据库工作台** —— 无需安装、支持离线使用、支持无桌面环境，单二进制文件（约 100MB）提供导入/导出/迁移/对比/AI 辅助能力。

---

## 📊 核心价值与典型场景

**核心价值**: 一个工具，覆盖从办公室到离线数据中心，从桌面到服务器核心的所有场景。

### **典型场景速查表**

| 场景 | 核心需求 | dqex 解决方案 | 关键命令 |
|------|---------|--------------|----------|
| **🏦 离线数据中心** | 无网络/无GUI/零依赖 | CLI 模式 + 离线模板 | `./dqex sql -c 生产库` |
| **🏠 家庭测试环境** | 数据导出还原 | 条件导出 + Gzip 压缩 | `./dqex exp --table-cond ... --gzip` |
| **🔍 故障排查** | 对比两个时间点差异 | 快照创建 + 对比报告 | `./dqex snapshot compare --a ... --b ...` |
| **📋 合规审计** | Excel 格式数据字典 | 原生 Excel 生成 | `./dqex dict -o data_dict.xlsx` |
| **🚀 跨方言迁移** | MySQL → PostgreSQL | 自动方言转换 | `./dqex migrate --source-conn ... --target-conn ...` |
| **💻 日常开发** | AI 辅助写 SQL | AI 智能体动态探索 | `\ai 查询最近30天订单` |
| **🎯 性能优化** | 执行计划分析 | 模板系统 + 索引建议 | `\template explain_query users id 123` |
| **📤 数据导出** | 部分数据 + 压缩 | 条件过滤 + Gzip | `--table-cond "created_at >= '2026-01-01'" --gzip` |

### **适用人群**

无论你是:
- 在银行数据中心排查故障的运维工程师
- 在咖啡馆写代码的独立开发者
- 在客户现场做咨询的技术顾问
- 还是需要满足等保审计的合规官
- 或是需要把生产数据带回家调试的开发者

**dqex 都为你而生。**

---

## 🌍 目标场景与用户画像

### **核心场景矩阵**

```
                    有网络 ──────────────→ 无网络
                  ┌──────────┬──────────────────────┐
    有桌面环境     │ 办公室    │ 客户现场 (临时办公)   │
                  │ - 日常开发│ - 顾问出差            │
                  │ - 团队协作│ - 演示环境            │
                  │ - 🏠 家庭测试│                     │
                  ├──────────┼──────────────────────┤
    无桌面环境     │ 云服务器  │ 🔥 离线数据中心       │
                  │ - CI/CD  │ - 银行/政府机房       │
                  │ - 自动化  │ - 工厂车间服务器      │
                  └──────────┴──────────────────────┘
                        ↑
                   dqex 覆盖多场景
```

### **典型用户画像**

#### **Persona 1: 金融运维工程师 (陈工)**
```yaml
年龄: 35岁
职业: 银行数据中心 DBA
工作环境:
  - 完全隔离网络 (等保三级要求)
  - 禁止安装软件 (安全策略)
  - Windows Server Core / Linux Minimal
  - 仅允许 U 盘拷贝工具
  
痛点:
  - DBeaver 需要 JVM + GUI (在受限环境中难以使用)
  - dbx 需要 WebView (在某些离线环境可能缺失)
  - mysql/psql 功能相对基础
  - 客户要求 Excel 格式数据字典 (合规审计)
  
为什么选择 dqex:
  ✅ U 盘拷贝即用 (单文件，zip 压缩后约 25MB)
  ✅ 离线 SQL 模板系统 (内置常用查询模板)
  ✅ 生成 Excel 格式数据字典
  ✅ 快照对比功能
```

#### **Persona 2: 独立开发者 (张三)**
```yaml
年龄: 28岁
职业: 全栈开发者 (远程工作)
工作环境:
  - 频繁切换 MySQL/PostgreSQL 项目
  - 讨厌重型工具启动慢
  - 需要 AI 辅助写复杂 SQL
  - 经常在家加班调试生产问题
  
痛点:
  - DBeaver 启动 30 秒+ (浪费时间)
  - TablePlus 收费 (成本高)
  - 手写复杂 SQL 可能出错
  - 生产数据不能直接拷贝回家 (敏感信息)
  - 家里网络无法连接公司数据库
  
为什么选择 dqex:
  ✅ 启动速度快 (实测 <1 秒)
  ✅ AI 辅助写 SQL (智能体架构)
  ✅ Diff 预览后插入编辑器
  ✅ 开源免费 (MIT 协议)
  ✅ 支持条件导出 + Gzip 压缩
```

#### **Persona 3: 咨询公司顾问 (李四)**
```yaml
年龄: 40岁
职业: IT 咨询合伙人
工作环境:
  - 每周拜访不同客户 (3-5 家)
  - 每个客户环境不同 (MySQL/PG/Oracle)
  - 需要快速诊断问题并出具报告
  
痛点:
  - 可能需要使用多个工具
  - 客户环境各异 (Windows/Mac/Linux)
  - 需要生成专业报告 (PPT/Excel)
  
为什么选择 dqex:
  ✅ 支持多种数据库 (MySQL/PG/Oracle)
  ✅ 跨平台一致体验 (单二进制文件)
  ✅ 提供对比报告功能
  ✅ 便于携带 (单文件)
```

---

## 💡 核心设计哲学

### **哲学 1: Anywhere First (无处不在优先)**

> **"如果工具不能在客户机房的离线服务器上运行，它就不够好。"**

**设计决策**:
- ✅ **零依赖部署**: 纯静态编译，前端嵌入二进制，无任何外部依赖
- ✅ **双模式支持**: CLI (SSH/Server Core) + Web UI (有浏览器时)
- ✅ **真正离线可用**: 内置 SQL 模板库，无需联网即可生成查询
- ❌ **拒绝 Electron/Tauri**: 避免 WebView 依赖，确保离线环境可用
- ❌ **拒绝重型运行时**: 避免 JVM/WebView 等依赖，确保低配机器流畅



### **哲学 2: AI-Native Architecture (AI 原生架构)**

> **"AI 不是插件，而是核心架构。"**

**设计决策**:
- ✅ **智能体模式**: 动态探索 schema，而非静态注入全量 DDL
- ✅ **三层降级策略**: 
  - Level 1: AI 智能体 (在线，需 API Key)
  - Level 2: 规则引擎 + 模板库 (离线，内置 50+ 模板)
  - Level 3: 传统 CLI (Tab 补全 + 元命令)
- ✅ **安全优先**: 危险 SQL 拦截 + 写操作二次确认
- ✅ **Diff 预览**: 生成 SQL 不直接覆盖，先侧并排对比再确认

**工作流程示例**:
```
用户输入: "查询最近 30 天销量 Top 10 的商品"
  ↓
Agent: 调用 list_tables() → 发现 orders, products 表
  ↓
Agent: 调用 get_schema("orders") → 获取字段定义
  ↓
Agent: 生成 SQL: SELECT * FROM orders WHERE created_at >= ... ORDER BY amount DESC LIMIT 10
  ↓
前端: Diff 预览 (生成结果 vs 当前编辑器)
  ↓
用户: 确认插入 → 执行
```



### **哲学 3: Developer Experience Obsession (开发者体验极致追求)**

> **"每一次点击、每一秒等待、每一个错误提示，都值得优化。"**

**设计决策**:
- ✅ **启动速度**: <1 秒 (实测)
- ✅ **内存占用**: 运行时约 50-60MB
- ✅ **智能补全**: Tab 触发，上下文感知
- ✅ **错误友好**: 统一错误卡片展示
- ✅ **深色模式**: 支持
- ✅ **快捷键**: `\g` 执行、`\e` 编辑、`Ctrl+R` 搜索历史

---

### **哲学 4: Compliance-Ready by Design (合规就绪设计)**

> **"金融/政府客户的合规需求，不是事后补丁，而是原生能力。"**

**设计决策**:
- ✅ **数据字典 Excel 导出**: 
  - 概览 Sheet + 分表详情 Sheet
  - 自动列宽调整 + 表头冻结 + 样式
- ✅ **快照对比报告**:
  - 结构差异: 新增/删除/修改列
  - 数据差异: 行数变化
  - JSON 格式输出
- ✅ **审计日志**:
  - SQL 执行记录
  - 可导出为 CSV/JSON

**典型场景**:
```bash
# 等保三级审计: 导出所有表结构
./dqex dict gis_db -s 政务库 -o 数据字典_2026Q3.xlsx

# 故障追溯: 对比两个时间点的数据差异
./dqex snapshot compare \
  --a 正常_20260819 \
  --b 故障前_20260820 \
  --output diff_report.json

# 审计日志导出
./dqex history list --type export --json > audit_log.json
```

---

## 🏆 核心功能优势

### **1. 真正的零依赖部署**

```bash
# 下载 → 解压 → 运行 (3 步完成)
wget https://github.com/yourname/dqex/releases/download/v0.6.0/dqex-0.6.0-linux-amd64.zip
unzip dqex-0.6.0-linux-amd64.zip
./dqex  # 启动 Web 服务 (:8181)

# 或 CLI 模式
./dqex sql -c 生产库
```

**技术实现**:
- 纯静态编译，无动态库依赖
- 前端构建产物嵌入二进制
- 内置数据库实现 (用于本地配置存储)



### **2. AI 原生架构 (智能体)**

**工作流程**:
```
用户输入: "查询最近 30 天销量 Top 10 的商品"
  ↓
Step 1: Agent 调用 list_tables()
  → 返回: ["orders", "products", "users"]
  ↓
Step 2: Agent 推断需要 orders 表
  → 调用 get_schema("orders")
  → 返回: 字段定义 (id, amount, created_at, ...)
  ↓
Step 3: Agent 生成 SQL
  → SELECT * FROM orders 
    WHERE created_at >= DATE_SUB(NOW(), INTERVAL 30 DAY) 
    ORDER BY amount DESC LIMIT 10
  ↓
Step 4: 前端 Diff 预览
  → 侧并排显示 (生成结果 vs 当前编辑器)
  → 用户确认: [替换选中] [插入光标] [追加末尾]
  ↓
Step 5: 执行并显示结果
  → 表格渲染 / 垂直显示 (\G)
  → 可导出 CSV/JSON
```

**技术亮点**:
- **会话独立 ChatModel**: 避免工具绑定竞争
- **历史按组裁剪**: 兼容工具轮次，保留关键 SQL
- **流式 SSE**: 逐 token 转发，打字机效果
- **进程级 Token 累计**: `\ai status` 查看消耗

**对比传统 AI 插件**:
| 维度 | 传统方式 | dqex 智能体 |
|------|---------|-------------|
| Schema 注入 | 启动时全量加载 (可能超 Token) | **工具调用按需查询** |
| 新表识别 | 需手动刷新 | **自动探索发现** |
| 跨库查询 | 困难 (上下文混乱) | **多轮工具调用自然支持** |
| Token 效率 | 低 (注入无关表结构) | **高 (仅查询相关表)** |

---

### **3. 离线 AI 能力 (50+ SQL 模板库)**

**适用场景**: 完全隔离网络环境 (银行/政府机房)

**模板分类**:
```
📚 可用 SQL 模板:

🔍 基础查询:
  select_all           全表查询
  select_columns       指定列查询
  
📊 聚合统计:
  count_rows           行数统计
  top_n                Top N 查询
  group_by_count       分组计数
  avg_value            平均值统计
  
🎯 条件过滤:
  recent_days          最近 N 天数据
  where_equal          等值过滤
  where_like           模糊查询
  check_null           空值检查
  check_duplicate      重复值检查
  
🔗 关联查询:
  join_two_tables      两表 JOIN
  left_join            左连接
```

**使用示例**:
```sql
-- 查询销量 Top 10 的商品
\template top_n amount orders 10

-- 输出:
✓ 生成的 SQL:
SELECT * FROM orders ORDER BY amount DESC LIMIT 10
提示: 使用 \g 执行，或 \e 编辑后执行

-- 查询最近 30 天的订单
\template recent_days orders created_at 30

-- 输出:
✓ 生成的 SQL:
SELECT * FROM orders WHERE created_at >= DATE_SUB(NOW(), INTERVAL 30 DAY) ORDER BY created_at DESC
```

**智能推荐**:
```sql
-- 根据关键词自动推荐
\templates  -- 查看所有模板
\templates aggregation  -- 只看聚合类
```

---

### **4. 快照对比 (杀手级特性)**

**场景**: 故障排查、变更验证、合规审计

**工作流程**:
```bash
# Step 1: 创建快照 (正常状态)
./dqex snapshot create -c 生产库 -n 正常_20260819

# Step 2: 系统发生变更 (部署新版本/数据异常)

# Step 3: 创建新快照
./dqex snapshot create -c 生产库 -n 异常_20260820

# Step 4: 对比两个快照
./dqex snapshot compare \
  --a 正常_20260819 \
  --b 异常_20260820

# Step 5: 查看可视化报告
```

**报告内容**:
```json
{
  "databases": [
    {
      "sourceDB": "camunda",
      "targetDB": "camunda",
      "tables": [
        {
          "name": "act_ru_task",
          "status": "source_only",  // 仅源库存在 (被删除)
          "columns": null,
          "data": null
        },
        {
          "name": "act_hi_procinst",
          "status": "both",
          "columns": {
            "matched": false,
            "sourceOnly": ["new_column"],  // + 新增列
            "targetOnly": ["old_column"],  // - 删除列
            "different": [                 // ± 修改列
              {
                "name": "status",
                "sourceType": "VARCHAR(50)",
                "targetType": "VARCHAR(100)"
              }
            ]
          },
          "data": {
            "mode": "count",
            "sourceRows": 1000,
            "targetRows": 950,
            "equal": false,
            "skippedReason": "行数变化: 快照 1000 → 当前 950"
          }
        }
      ]
    }
  ]
}
```

**Web UI 可视化**:
- 结构差异: 红色 `-` (删除)、绿色 `+` (新增)、黄色 `±` (修改)
- 数据差异: 行数对比柱状图 + 样本数据表格
- 过滤功能: 仅显示有差异的表 / 显示全部



### **5. 数据导出还原 (家庭测试环境)**

**场景**: 开发者需要将生产环境数据导出，带回家在个人电脑上还原到测试环境进行调试

**痛点**:
- ❌ 生产数据包含敏感信息 (用户手机号/身份证/邮箱)，不能直接拷贝
- ❌ 数据量巨大 (GB/TB 级)，全量导出不现实
- ❌ 家里网络不稳定，无法远程连接公司数据库
- ❌ 需要保持数据关联性 (外键约束)，否则测试无效
- ❌ 传统工具操作繁琐 (mysqldump → 手动脱敏 → scp → mysql import)

**dqex 解决方案**:

#### **Step 1: 选择性导出 + Gzip 压缩**

```bash
# 导出指定表的部分数据 (带条件过滤)
./dqex exp camunda \
  -s 生产库 \
  --tables "users,orders,order_items" \
  --table-cond "users:created_at >= '2026-01-01'" \
  --table-cond "orders:status = 'completed'" \
  --gzip true \
  -o ~/Downloads/prod_data_20260821.sql.gz

# 输出:
# >> 导出中: 1 个库, 3 个表 → /Users/name/Downloads/prod_data_20260821.sql.gz
# >> users: 已应用条件 created_at >= '2026-01-01'
# >> orders: 已应用条件 status = 'completed'
# >> order_items: 全量导出
# >> ✓ 导出完成: 3 个表 → prod_data_20260821.sql.gz
```

**关键特性**:
- ✅ **条件过滤**: `--table-cond` 支持 SQL WHERE 子句，只导出需要的数据
- ✅ **Gzip 压缩**: 减小文件体积
- ✅ **单事务一致性**: `--single-transaction` 保证导出数据一致性
- ✅ **进度显示**: 每表导出行数可见

#### **Step 2: U 盘拷贝回家**

```bash
# 文件大小减小，便于拷贝或发送
cp ~/Downloads/prod_data_20260821.sql.gz /Volumes/USB_DRIVE/

# 或通过加密邮件发送 (敏感数据建议加密)
gpg --encrypt --recipient your@email.com prod_data_20260821.sql.gz
```

#### **Step 3: 家庭测试环境还原**

```bash
# 在家里的 Mac/Windows/Linux 上
./dqex imp \
  -t 本地测试库 \
  -i prod_data_20260821.sql.gz \
  --reset drop-and-create \
  --backup false \
  --batch-size 1000

# 输出:
# >> 导入中...
# >> 删除并重建表: users
# >> 删除并重建表: orders
# >> 删除并重建表: order_items
# >> 导入 users
# >> 导入 orders
# >> 导入 order_items
# >> ✓ 导入完成
```

**关键特性**:
- ✅ **自动建表**: `--reset drop-and-create` 自动删除旧表并重建
- ✅ **批量插入**: `--batch-size 1000` 优化导入性能
- ✅ **支持压缩格式**: 直接读取 `.sql.gz` / `.zip` 文件
- ✅ **跨平台一致**: 支持 Mac/Windows/Linux

#### **Step 4: 验证数据完整性**

```bash
# CLI 方式验证
./dqex sql -c 本地测试库 -e "SELECT COUNT(*) FROM users"

./dqex sql -c 本地测试库 -e "SELECT COUNT(*) FROM orders"

# Web UI 方式验证
# 1. ./dqex 启动服务
# 2. 浏览器访问 http://127.0.0.1:8181
# 3. 对象树查看表结构
# 4. 表浏览器查看数据样本
```

---

**数据脱敏建议**:

如果生产数据包含敏感信息，建议在导出前通过 SQL 视图进行脱敏处理:

```sql
-- 在生产库创建脱敏视图
CREATE VIEW users_masked AS
SELECT 
  id,
  CONCAT('user_', id) AS username,  -- 用户名脱敏
  CONCAT(SUBSTRING(phone, 1, 3), '****', SUBSTRING(phone, 8)) AS phone,  -- 手机号脱敏
  CONCAT(SUBSTRING(email, 1, 3), '***@', SUBSTRING_INDEX(email, '@', -1)) AS email,  -- 邮箱脱敏
  created_at,
  status
FROM users;

-- 导出视图而非原表
./dqex exp camunda \
  -s 生产库 \
  --tables "users_masked,orders,order_items" \
  -o masked_data.sql.gz

# 回家后导入，数据已脱敏
./dqex imp -t 本地测试库 -i masked_data.sql.gz
```

**脱敏效果示例**:
```
原始数据:
  phone: 13812345678
  email: zhangsan@gmail.com

脱敏后:
  phone: 138****5678
  email: zha***@gmail.com
```

> **注意**: 脱敏逻辑需要用户根据实际业务需求自行实现，dqex 提供导出视图的能力。

---

**完整工作流示例**:

```bash
# === 公司办公室 (周五下午) ===

# 1. 导出最近 3 个月的订单数据 (用于周末在家调试性能问题)
./dqex exp ecommerce \
  -s 生产库 \
  --tables "orders,order_items,products" \
  --table-cond "orders:created_at >= DATE_SUB(NOW(), INTERVAL 3 MONTH)" \
  --gzip true \
  -o ~/Desktop/weekend_debug_data.sql.gz

# 2. 拷贝到 U 盘
cp ~/Desktop/weekend_debug_data.sql.gz /Volumes/SANDISK/

# === 家中 (周六上午) ===

# 3. 从 U 盘复制到本地
cp /Volumes/SANDISK/weekend_debug_data.sql.gz ~/Downloads/

# 4. 导入到本地 MySQL
./dqex imp \
  -t localhost_test \
  -i ~/Downloads/weekend_debug_data.sql.gz \
  --reset drop-and-create

# 5. 开始调试
./dqex sql -c localhost_test
> SELECT o.id, o.total_amount, COUNT(oi.id) AS item_count
  FROM orders o
  JOIN order_items oi ON o.id = oi.order_id
  WHERE o.created_at >= '2026-05-01'
  GROUP BY o.id
  ORDER BY o.total_amount DESC
  LIMIT 10;

# 6. 发现问题，修复代码，周日继续...
```

---

**价值总结**:

| 维度 | 传统方案 | dqex 方案 | 提升 |
|------|---------|----------|------|
| **操作步骤** | 多步 (mysqldump → 脱敏 → scp → mysql) | 2 步 (exp → imp) | **简化** |
| **文件大小** | 原始大小 | Gzip 压缩后减小 | **缩小** |
| **导出时间** | 较长 | 条件过滤 + 压缩 | **更快** |
| **数据一致性** | ⚠️ 需手动加 `--single-transaction` | ✅ 默认启用 | **更安全** |
| **跨平台兼容** | ❌ Windows↔Linux 编码问题 | ✅ 完全兼容 | **零问题** |
| **学习成本** | 需熟悉 mysqldump/mysql 参数 | ✅ 简单命令 | **易上手** |

---

### **6. 合规数据字典 (Excel 专业格式)**

**场景**: 等保三级审计、 GDPR 合规、项目验收

**生成命令**:
```bash
./dqex dict camunda -s 生产库 -o 数据字典_2026Q3.xlsx
```

**输出结构**:
```
数据字典_2026Q3.xlsx
├── Sheet 1: 概览
│   ├── 数据库名称: camunda
│   ├── 生成时间: 2026-08-21 14:30
│   ├── 表总数: 50
│   └── 总行数: 1,234,567
│
├── Sheet 2: act_ru_task
│   ├── 表注释: 运行时任务表
│   ├── 列信息:
│   │   ├── ID_ (varchar, 主键, 非空) - 任务 ID
│   │   ├── NAME_ (varchar, 可空) - 任务名称
│   │   └── CREATE_TIME_ (datetime, 非空) - 创建时间
│   └── 索引信息:
│       ├── PRIMARY (ID_)
│       └── IDX_TASK_CREATE (CREATE_TIME_)
│
├── Sheet 3: act_hi_procinst
│   └── ... (同上格式)
│
└── Sheet 50: act_ge_property
    └── ... (同上格式)
```

**技术亮点**:
- 原生实现，无外部依赖
- 自动样式: 表头加粗 + 背景色、交替行颜色、自动列宽
- 冻结窗格: 首行表头固定，滚动时始终可见
- 超链接: 概览 Sheet 点击表名跳转到详情 Sheet



## 🛠️ 技术架构

### **核心设计**

```yaml
部署特性:
  - 单二进制文件，无外部依赖
  - 前端嵌入二进制，无需单独部署
  - 内置数据库 (配置/历史记录)
  - 原生 Excel 生成 (数据字典)

AI 集成:
  - LLM 框架集成
  - 自研智能体: 工具调用模式

安全:
  - 密码加密存储
  - Token 认证 (24 小时有效期)
  - 危险 SQL 拦截 (DROP/TRUNCATE/DELETE 无 WHERE)
```

### **前端特性**

```yaml
用户体验:
  - SQL 编辑器 (语法高亮/智能补全)
  - AI 生成预览 (侧并排对比)
  - 虚拟滚动 (万行数据流畅)
  - 深色模式支持
  - 国际化 (中/英双语)
```

### **部署架构**

```
┌─────────────────────────────────────┐
│         dqex (单二进制)              │
├─────────────────────────────────────┤
│                                     │
│  ┌──────────┐    ┌───────────────┐  │
│  │ Web Server│    │  CLI Engine   │  │
│  │ (Gin :8181)│   │  (Cobra)      │  │
│  └─────┬────┘    └───────┬───────┘  │
│        │                 │          │
│  ┌─────▼─────────────────▼───────┐  │
│  │     Service Layer             │  │
│  │  - Export/Import/Migrate      │  │
│  │  - Snapshot/Compare           │  │
│  │  - Dictionary/AI              │  │
│  └─────────────┬─────────────────┘  │
│                │                     │
│  ┌─────────────▼─────────────────┐  │
│  │     Engine Layer              │  │
│  │  - cydb (方言适配)             │  │
│  │  - SQLite (本地存储)           │  │
│  │  - excelize (Excel 生成)       │  │
│  └───────────────────────────────┘  │
│                                     │
│  ┌───────────────────────────────┐  │
│  │  Embedded Frontend (web/dist) │  │
│  │  - React + TypeScript         │  │
│  │  - Monaco Editor              │  │
│  │  - shadcn/ui Components       │  │
│  └───────────────────────────────┘  │
└─────────────────────────────────────┘
```

---



## 🚀 快速开始

### **30 秒体验**

```bash
# 1. 下载 (以 Linux amd64 为例)
wget https://github.com/yourname/dqex/releases/download/v0.6.0/dqex-0.6.0-linux-amd64.zip
unzip dqex-0.6.0-linux-amd64.zip
cd dqex-0.6.0-linux-amd64

# 2. 启动 Web 服务
./dqex

# 3. 浏览器访问
# http://127.0.0.1:8181?token=<自动生成的令牌>

# 4. 添加数据库连接
# 点击左上角 "+" → 填写连接信息 → 测试连接 → 保存

# 5. 开始使用
# - SQL 查询终端
# - 表数据浏览
# - AI 辅助写 SQL
# - 导出/导入/迁移
# - 快照对比
# - 数据字典生成
```

### **CLI 模式 (离线环境)**

```bash
# 交互式 SQL 终端
./dqex sql -c 生产库

# 常用元命令
> \dt                    # 列出所有表
> \d users               # 查看表结构
> \template top_n amount orders 10  # 应用离线模板
> \g                     # 执行缓冲区 SQL
> \copy result.csv       # 导出结果为 CSV
> \q                     # 退出

# 导出数据字典
./dqex dict camunda -s 生产库 -o data_dict.xlsx

# 创建快照
./dqex snapshot create -c 生产库 -n 备份_20260821

# 对比快照
./dqex snapshot compare \
  --a 备份_20260820 \
  --b 备份_20260821
```

---



## 🤝 贡献指南

### **我们欢迎的贡献**
- ✅ Bug 修复 (优先处理)
- ✅ 文档改进 (中英文皆可)
- ✅ 新 SQL 模板 (丰富离线库)
- ✅ 数据库驱动支持 (扩展兼容性)
- ✅ UI/UX 优化 (提升开发者体验)

### **贡献流程**
1. Fork 仓库
2. 创建分支 (`git checkout -b feature/your-feature`)
3. 提交更改 (`git commit -am 'Add some feature'`)
4. 推送到分支 (`git push origin feature/your-feature`)
5. 创建 Pull Request

### **代码规范**
- Go: `gofmt` + `golint`
- TypeScript: ESLint + Prettier
- Commit Message: Conventional Commits 规范

详见 [docs/conventions.md](./conventions.md)

---