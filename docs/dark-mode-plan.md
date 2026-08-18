# 夜间模式实现方案

> 状态:✅ 已实现(2026-08-18)
> 涉及范围:`web/`(React + Vite + Tailwind + shadcn/ui)

## 1. 背景与目标

为 dbimpex Web 端增加夜间模式(深色主题),支持「浅色 / 深色 / 跟随系统」三种模式,默认跟随系统。要求:

1. 所有页面、数据网格、编辑器、浮层在暗色下可读;
2. 刷新不闪白、主题持久化;
3. 不破坏现有功能(数据网格选中/斑马纹/冻结列/右键聚焦、Monaco 编辑、AI diff 高亮、JSON 预览等)。

## 2. 现状盘点(审核结论)

### 2.1 基础设施

| 项 | 状态 | 说明 |
|---|---|---|
| Tailwind `darkMode: ["class"]` | ✅ 已配置 | 无需改动 |
| `index.css` `.dark` CSS 变量 | ✅ 已有 | 偏纯黑(`222.2 84% 4.9%`),需微调为柔和深蓝灰 |
| `next-themes@0.4.6` | ✅ 已安装 | **未接入**(无 ThemeProvider、无防闪烁脚本) |
| `color-scheme` | ❌ 缺失 | 需在 `:root`/`.dark` 补充,适配原生滚动条/控件 |
| sonner Toaster | ✅ 已接入 | `useTheme()` 驱动,无需改动 |

### 2.2 需改造的硬编码颜色(审核盘点)

**数据网格内联色(JS 注入,暗色下必失效):**

| 文件 | 位置 | 现值 |
|---|---|---|
| `ResultGrid.tsx` | 单元格 `backgroundColor` | `(start+ri)%2===1 ? "#f8fafc" : "#ffffff"` |
| `TableBrowser.tsx` | 单元格 `backgroundColor` | `selected ? "#dbeafe" : ri%2===1 ? "#f8fafc" : "#ffffff"` |
| `index.css` | `.row-context-focused > td` | `#dbeafe !important`(右键聚焦行) |
| `index.css` | `.data-grid-table td[style*="background"]:hover` | `#eef2f7 !important`(hover) |

**浅色状态徽标 / 提示框(Tailwind 浅色系,暗色下反差过大):**

| 文件 | 位置 | 现值 |
|---|---|---|
| `App.tsx` | `STATUS_META` 状态胶囊 | `bg-green-50 text-green-700`、`bg-blue-50 text-blue-600`、`bg-red-50 text-destructive` 等 |
| `TaskView.tsx` | `TYPE_ICON` 图标底色 | `bg-blue-50`、`bg-green-50`、`bg-purple-50`、`bg-orange-50` |
| `SnapshotView.tsx` | 任务完成状态条 | `border-green-200 bg-green-50 text-green-800` |
| `CompareView.tsx` | 任务完成状态条 | 同 SnapshotView |
| `CompareReport.tsx` | 状态徽章 + 提示框 | `bg-green-50 text-green-700`、`border-blue-200 bg-blue-50` 等 |
| `Hint.tsx` | 提示条 | `border-blue-200 bg-blue-50/60 text-blue-700`、`border-amber-200 bg-amber-50` |
| `WorkspaceLayout.tsx` | AI 预览条 | `bg-emerald-50/80 text-emerald-600/700` |
| `TablePicker.tsx` | 提示与徽章 | `bg-amber-50 text-amber-600`、`border-amber-200 bg-amber-50` 等 |
| `ProgressView.tsx` | 状态条 + 错误日志 | `border-green-200 bg-green-50`、`bg-red-50/50 text-red-900` |

**编辑器主题(硬编码浅色):**

| 文件 | 位置 | 现值 |
|---|---|---|
| `SqlEditor.tsx` | `theme="vs"` | 需联动 `vs` / `vs-dark` |
| `CellEditor.tsx` | 3 处 Monaco `Editor` | 未设 theme(默认 vs 浅色) |
| `CellEditor.tsx` | 2 处 `JsonView` | 默认浅色主题,暗色不可读 |

### 2.3 无需改动(审核确认)

- 日志深色终端区(`bg-slate-950 text-slate-200`):`SnapshotView` / `CompareView` / `ProgressView` 的 SQL/日志输出区,浅色模式本身就是深色,夜间模式保持即可;
- 所有 shadcn 语义类(`bg-background` / `text-muted-foreground` / `bg-card` 等):自动跟随;
- 冻结列、右键菜单、Dialog / Dropdown / Popover 等浮层:均基于语义类,自动跟随;
- sonner Toaster:已由 `useTheme` 驱动。

## 3. 技术方案

### 3.1 基础设施接入 next-themes

- `index.html`:head 内联防闪烁脚本(读 `localStorage.theme` 与系统偏好,预置 `<html class="dark">` 与 `color-scheme`),避免刷新闪白;
- `main.tsx`:`<ThemeProvider attribute="class" defaultTheme="system" enableSystem>` 包裹 App(HashRouter 外层);
- `index.css`:
  - `:root { color-scheme: light }`、`.dark { color-scheme: dark }`;
  - `.dark` 背景色由纯黑微调为柔和深蓝灰(与浅色「柔和深灰」设计语言一致);
  - 新增数据网格语义变量:`--grid-row-base` / `--grid-row-zebra` / `--grid-row-selected` / `--grid-row-hover`(深浅各一组);
  - `.row-context-focused > td` 与网格 hover 规则改用 `hsl(var(--grid-row-*))`;
  - `.dark .w-rjv { ... }` 覆盖 `@uiw/react-json-view` 的 `--w-rjv-*` CSS 变量,使 JSON 预览暗色可读(无需改组件)。

### 3.2 数据网格取色(核心难点)

网格单元格内联色由 JS 注入,无法用 Tailwind class,方案:

- 新建 `web/src/lib/theme.ts`:
  - `useGridColors()`:基于 `useTheme().resolvedTheme` 的 `useMemo`,通过 `getComputedStyle` 读取 `--grid-row-*` 变量拼出 `hsl(...)`,返回 `{ base, zebra, selected }`;
  - 主题切换时 `resolvedTheme` 变化 → 自动重算,无需监听 DOM。
- `ResultGrid.tsx` / `TableBrowser.tsx` 替换内联 `backgroundColor`。

### 3.3 状态徽标语义化

统一改为「透明度底色 + 语义文字」模式,深浅色通吃:

- 状态胶囊:`bg-green-50 text-green-700` → `bg-green-500/10 text-green-700 dark:text-green-500`(其余色系同理);
- 提示框:`border-green-200 bg-green-50` → `border-green-500/30 bg-green-500/10 text-green-700 dark:text-green-400`;
- 图标底色:`bg-blue-50 text-blue-500` → `bg-blue-500/10 text-blue-600 dark:text-blue-400`;
- 错误日志:`bg-red-50/50 text-red-900` → `bg-red-500/10 text-red-800 dark:text-red-400`。

### 3.4 编辑器主题联动

- `SqlEditor.tsx`:`theme={resolvedTheme === "dark" ? "vs-dark" : "vs"}`;
- `CellEditor.tsx`:3 处 Monaco `Editor` 增加同款 theme;
- AI diff 高亮(rgba 半透明色)在深色下天然可用,无需改动。

### 3.5 切换入口

- **Header(tab 栏右侧操作组)**:主题切换 DropdownMenu(浅色 / 深色 / 跟随系统),图标 `Sun` / `Moon` / `Monitor`;
- **设置页「通用设置」**:主题下拉,与 Header 同源(`useTheme`)。

## 4. 实施清单

| 步骤 | 文件 | 改动 |
|---|---|---|
| 1 | `index.css` | 变量、`color-scheme`、网格规则、`.w-rjv` 暗色 |
| 2 | `index.html` | 防闪烁脚本 |
| 3 | `main.tsx` | `ThemeProvider` |
| 4 | `web/src/lib/theme.ts`(新建) | `useGridColors()` |
| 5 | `ResultGrid.tsx` / `TableBrowser.tsx` | 内联色 → hook |
| 6 | `SqlEditor.tsx` / `CellEditor.tsx` | Monaco theme 联动 |
| 7 | `WorkspaceLayout.tsx` | 主题按钮 + emerald-50 徽标 |
| 8 | `SettingsView.tsx` | 主题下拉 |
| 9 | `App.tsx` / `TaskView.tsx` / `Hint.tsx` | 徽标语义化 |
| 10 | `SnapshotView.tsx` / `CompareView.tsx` / `CompareReport.tsx` / `TablePicker.tsx` / `ProgressView.tsx` | 徽标语义化 |

## 5. 实施记录(2026-08-18 完成)

| 文件 | 改动 |
|---|---|
| `src/index.css` | `.dark` 背景微调柔和深蓝灰 + `color-scheme` + `--grid-row-*` 变量 + 网格规则语义化 + `.dark .w-rjv-inner` JSON 预览暗色变量 |
| `index.html` | 内联防闪烁脚本(读 `localStorage.theme` + 系统偏好) |
| `src/main.tsx` | 接入 `ThemeProvider attribute="class" defaultTheme="system" enableSystem` |
| `src/lib/theme.ts`(新建) | `useGridColors()` / `useIsDark()` |
| `ResultGrid.tsx` / `TableBrowser.tsx` | 内联 `#hex` → `grid.base/zebra/selected`;列信息表斑马 `bg-slate-50` → `bg-muted/40` |
| `SqlEditor.tsx` / `CellEditor.tsx` | Monaco `theme` 按 `useIsDark()` 返回 `vs` / `vs-dark`(3 处) |
| `WorkspaceLayout.tsx` | tab 栏主题切换 DropdownMenu(浅色/深色/跟随系统)+ AI 预览条徽标语义化 |
| `SettingsView.tsx` | 通用设置新增「外观」主题下拉 |
| `App.tsx` / `TaskView.tsx` / `Hint.tsx` / `SnapshotView.tsx` / `CompareView.tsx` / `CompareReport.tsx` / `TablePicker.tsx` / `ProgressView.tsx` / `AIPanel.tsx` | 状态胶囊 / 提示框 / 图标底色统一为 `*-500/10` 透明度语义色 + `dark:` 文字变体 |

### 5.1 实际执行中的补充发现

- JsonView 根容器类名为 `w-rjv-inner`(非 `w-rjv`),暗色变量选择器已用实际类名;
- `ProgressView`「查看详情」按钮原 `bg-white/60` → `bg-background`(避免暗色下白块);
- `TableBrowser` 列信息表斑马纹 `bg-slate-50` 一并语义化;
- `AIPanel` 两处 `bg-violet-50` 统一为 `/10` 风格;
- sonner Toaster 已由 `useTheme` 驱动,未改动。

## 6. 风险与规避

| 风险 | 规避 |
|---|---|
| 网格内联色不随主题刷新 | `resolvedTheme` 驱动 `useMemo` 重算 |
| `.row-context-focused` 需覆盖 inline 斑马色 | 保留 `!important`,改用 CSS 变量值 |
| JsonView 暗色不可读 | `.dark` 下覆盖 `--w-rjv-*` 变量(官方 darkTheme 色值) |
| 刷新闪白 | 内联防闪烁脚本 |
| 系统跟随 | `defaultTheme="system"` + `enableSystem` |

## 7. 验收清单

- [ ] 7 个功能页 + 右侧面板暗色可读;
- [ ] 数据网格:斑马纹 / 选中行 / 冻结列 / 右键聚焦行(hover)正常;
- [ ] Monaco SQL 编辑器语法高亮正常,AI diff 高亮可见;
- [ ] 单元 JSON 预览暗色可读;
- [ ] Dialog / Dropdown / Popover 等浮层跟随主题;
- [ ] 三种模式切换即时生效,刷新后保持;
- [ ] 跟随系统模式随 OS 切换;
- [ ] 浅色模式视觉与改造前一致(回归)。
