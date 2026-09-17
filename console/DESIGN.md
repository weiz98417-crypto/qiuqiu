---
version: alpha
name: QiuQiu-operator-console-design-analysis
description: "QiuQiu 运营管理台的设计基底：夜空近黑画布（#070B12，带蓝调的深空底色），看台色面板（#111824）配边线（#253142）细线分层，米白墨色文字（#F4F1E8），球场橙（#FF6B35）作为唯一强调色（按压态深橙 #E83A24），天蓝（#55A8FF）作为次强调。结构取自 VoltAgent awesome-design-md 的 linear.app 分析（深色画布 + 表面阶梯 + 发丝线 + 单强调 + 负字距展示字体），色板替换为 app_theme.dart 实测的 QiuQiu 色板，并映射到 antd ConfigProvider 暗色 token。"

colors:
  primary: "#FF6B35"
  on-primary: "#070B12"
  primary-hover: "#FF8559"
  primary-active: "#E83A24"
  ink: "#F4F1E8"
  ink-muted: "#AAB4C0"
  ink-subtle: "#7C8794"
  canvas: "#070B12"
  surface-1: "#111824"
  surface-2: "#182234"
  surface-3: "#1E2A3E"
  border: "#253142"
  border-strong: "#384B66"
  accent-secondary: "#55A8FF"
  semantic-success: "#5FCB8B"
  semantic-warning: "#F2C94C"
  semantic-error: "#F05D5E"
  inverse-canvas: "#F4F1E8"
  inverse-ink: "#070B12"

typography:
  headline:
    fontFamily: QiuQiu Display
    fontSize: 24px
    fontWeight: 600
    lineHeight: 1.25
    letterSpacing: -0.5px
  card-title:
    fontFamily: QiuQiu Display
    fontSize: 18px
    fontWeight: 600
    lineHeight: 1.30
    letterSpacing: -0.3px
  subhead:
    fontFamily: QiuQiu Text
    fontSize: 16px
    fontWeight: 500
    lineHeight: 1.45
    letterSpacing: 0
  body:
    fontFamily: QiuQiu Text
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: 0
  caption:
    fontFamily: QiuQiu Text
    fontSize: 12px
    fontWeight: 400
    lineHeight: 1.45
    letterSpacing: 0
  button:
    fontFamily: QiuQiu Text
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.20
    letterSpacing: 0
  mono:
    fontFamily: JetBrains Mono
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.50
    letterSpacing: 0

rounded:
  xs: 4px
  sm: 6px
  md: 8px
  lg: 12px
  xl: 16px
  pill: 9999px
  full: 9999px

spacing:
  xxs: 4px
  xs: 8px
  sm: 12px
  md: 16px
  lg: 24px
  xl: 32px
  xxl: 48px

components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.on-primary}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    padding: 6px 16px
  button-primary-hover:
    backgroundColor: "{colors.primary-hover}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.md}"
  button-primary-active:
    backgroundColor: "{colors.primary-active}"
    textColor: "{colors.on-primary}"
    rounded: "{rounded.md}"
  button-secondary:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    border: 1px solid "{colors.border}"
    padding: 6px 16px
  data-card:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.lg}"
    border: 1px solid "{colors.border}"
    padding: 20px
  metric-cell:
    backgroundColor: "{colors.surface-1}"
    textColor: "{colors.ink}"
    metricTypography: "{typography.headline}"
    labelTypography: "{typography.caption}"
    rounded: "{rounded.lg}"
    border: 1px solid "{colors.border}"
    padding: 20px
  text-input:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink}"
    typography: "{typography.body}"
    rounded: "{rounded.md}"
    border: 1px solid "{colors.border}"
    padding: 6px 12px
  status-badge:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.ink-muted}"
    typography: "{typography.caption}"
    rounded: "{rounded.pill}"
    padding: 2px 10px
  side-nav:
    backgroundColor: "{colors.canvas}"
    textColor: "{colors.ink-muted}"
    activeTextColor: "{colors.primary}"
    activeBackground: "{colors.surface-1}"
    typography: "{typography.button}"
    width: 208px
  table-row-hover:
    backgroundColor: "{colors.surface-2}"

---

## Overview

QiuQiu 运营管理台的画布是 `{colors.canvas}` —— 带蓝调的夜空近黑（#070B12），不是纯黑。其上是看台色的表面阶梯：`{colors.surface-1}`（#111824）承载卡片、表格容器与侧栏激活项，`{colors.surface-2}`（#182234）承载输入框、悬浮行与次级按钮，`{colors.surface-3}` 用于下拉菜单等最深浮层。所有分层以 1px `{colors.border}`（#253142）发丝线勾勒，不依赖阴影。文字用米白墨色 `{colors.ink}`（#F4F1E8，带纸感的暖白，呼应"球场灯光"），次级文字 `{colors.ink-muted}`（#AAB4C0）。

唯一的强调色是**球场橙** `{colors.primary}`（#FF6B35）—— 主按钮、激活导航项、关键操作、链接强调。悬停提亮为 `{colors.primary-hover}`（#FF8559），按压深入为 `{colors.primary-active}`（#E83A24，深橙红）。**天蓝** `{colors.accent-secondary}`（#55A8FF）是次强调，只用于信息类标签、在线状态与可点击的跳转链接，不得与橙争夺主操作位。语义色：成功 `{colors.semantic-success}`（#5FCB8B）、警告 `{colors.semantic-warning}`（#F2C94C）、错误 `{colors.semantic-error}`（#F05D5E）——仅用于状态语义（健康/降级、在线/离线、过期），不作装饰。

**Key Characteristics:**
- **夜空画布运营台** —— `{colors.canvas}`（#070B12）带蓝调，从不出纯黑 #000000。
- **球场橙单强调**（#FF6B35）—— 主操作、激活态、审计重点；稀缺使用。
- **天蓝次强调**（#55A8FF）—— 链接、在线徽标、信息 Tag；与橙分工不混用。
- 三级表面阶梯（canvas → surface-1 → surface-2 → surface-3）+ 发丝线分层，无阴影。
- 卡片 `{rounded.lg}` 12px 圆角；按钮/输入 `{rounded.md}` 8px；状态徽标 pill。
- 数据表格是页面主角（运营台的"产品截图"就是数据本身）；行悬浮 = surface-2 抬升。
- 中文标签为主，数字/ID/traceId 用 `{typography.mono}`。

## Colors

> 色源：`client/lib/theme/app_theme.dart`（实测色板）。

### Brand & Accent
- **球场橙**（{colors.primary}）：主操作按钮、侧栏激活指示、比分/进球等高亮、审计页关键引用标记。
- **球场橙 · 悬停**（{colors.primary-hover}）：主按钮悬停态，比主色亮一档。
- **球场橙 · 按压**（{colors.primary-active}）：主按钮按压态，深橙红 #E83A24。
- **天蓝**（{colors.accent-secondary}）：次强调——超链接、在线状态点、信息类 Tag、用户层钻取链接。

### Surface
- **夜空画布**（{colors.canvas}）：页面底色 #070B12；侧栏与头部同色，靠发丝线分隔。
- **看台**（{colors.surface-1}）：卡片、表格容器、Metric 单元格、激活导航项背景。
- **看台抬升**（{colors.surface-2}）：输入框、悬浮行、次级按钮、status-badge。
- **深浮层**（{colors.surface-3}）：下拉菜单、Drawer 内嵌套面板。
- **边线**（{colors.border}）：1px 卡片边框与分割线 #253142。
- **边线强化**（{colors.border-strong}）：输入聚焦描边、表格表头底线。

### Text
- **墨色**（{colors.ink}）：标题与正文主色，暖米白 #F4F1E8。
- **弱墨**（{colors.ink-muted}）：次要文字、表格次要列、说明文字 #AAB4C0。
- **隐墨**（{colors.ink-subtle}）：占位符、禁用态、页脚注记 #7C8794。

### Semantic
- **成功绿**（{colors.semantic-success}）：memory 健康、话题已答、在线会话正常。
- **警告黄**（{colors.semantic-warning}）：backlog 积压、3 天以上未答话题。
- **错误红**（{colors.semantic-error}）：memory 降级、过期话题、吊销动作、投递打断。

## Typography

### Font Family

- **QiuQiu Display** —— 中文界面标题字体栈：`"PingFang SC", "Microsoft YaHei", "Noto Sans SC", system-ui, sans-serif`；承载 headline / card-title。
- **QiuQiu Text** —— 同栈正文；承载 body / caption / button。
- **Mono** —— `ui-monospace, "JetBrains Mono", "Cascadia Code", Consolas`；用于 matchId / userId / traceId / citation 前缀 / 令牌等标识符。

### Hierarchy

| Token | Size | Weight | Use |
|---|---|---|---|
| `{typography.headline}` | 24px | 600 | 页面标题（全局层/比赛层/用户层） |
| `{typography.card-title}` | 18px | 600 | 卡片标题、单元名（记忆健康 / 话题老化…） |
| `{typography.subhead}` | 16px | 500 | 区块副标题、Drawer 标题 |
| `{typography.body}` | 14px | 400 | 表格正文、表单、默认文本 |
| `{typography.caption}` | 12px | 400 | 时间戳、计数徽标、表头辅助说明 |
| `{typography.button}` | 14px | 500 | 按钮与导航标签 |
| `{typography.mono}` | 13px | 400 | ID、令牌、citation 前缀 |

### Principles

- 中文不做负字距（仅西文数字混排时保持 0）；标题层级靠字号与 600/400 权重对比。
- antd 组件字体一律走 ConfigProvider token（fontFamily / fontSize），不写散落 inline font。
- Mono 只用于标识符 —— 运营需要精确复制 matchId/traceId 去核查。

## Layout

### Spacing System

- **Base unit**: 4px。
- 卡片内边距 `{spacing.lg}` 20–24px；页面内容区 `{spacing.xl}` 24–32px；区块间距 `{spacing.lg}` 24px。
- 表格行高紧凑（antd size="middle"），单元格纵向 padding 12px —— 运营台密度优先于留白。

### Grid & Container

- 侧栏固定 208px（`{components.side-nav}`），头部 56px 与 Linear top-nav 同高。
- 内容区最大宽度不设限（运营表格需要横向空间），五宫格 Overview 用 antd Row/Col gutter 16px。
- 三层 IA：全局层 `/console` → 比赛层 `/console/match/:id` → 用户层 `/console/match/:id/user/:uid`，每层头部保留面包屑。

### Whitespace Philosophy

深色画布即留白。分层靠"抬升到看台色 + 发丝线"，不靠分隔线堆叠。同屏多单元（Overview 五格）时以卡片为分组单元，卡片之间 16px gutter。

## Elevation & Depth

| Level | Treatment | Use |
|---|---|---|
| 0 (flat) | 画布色无边框 | 页面标题区、说明文字 |
| 1 (看台抬升) | surface-1 背景 + 1px border | 卡片、表格容器、Metric 格 |
| 2 (悬浮抬升) | surface-2 背景 | 表格行 hover、Dropdown、Tooltip |
| 3 (浮层) | surface-3 背景 | Modal、Drawer、Select 下拉 |
| 4 (focus ring) | 2px primary 50% 透明描边 | 聚焦输入、聚焦按钮 |

不使用 drop shadow（antd 默认阴影通过 token 压平：boxShadow* 设为 none 或极弱）。

## Shapes

| Token | Value | Use |
|---|---|---|
| `{rounded.md}` | 8px | 按钮、输入框（antd borderRadius token） |
| `{rounded.lg}` | 12px | 卡片、Drawer（antd borderRadiusLG） |
| `{rounded.pill}` | 9999px | 状态徽标、Tag |
| `{rounded.sm}` | 6px | 表格内小按钮、行内操作 |

## Components

### Buttons

**`button-primary`** —— 球场橙 CTA：标记已答、创建运营员等写操作。按压态深橙 `{colors.primary-active}`。
**`button-secondary`** —— 看台色 + 边线：过滤、刷新、次级操作。
**危险动作**（吊销运营员、删除画像槽位、过期话题）用 antd `danger` —— 映射 `{colors.semantic-error}`，且一律带二次确认（Popconfirm/Modal confirm）。

### Cards & Cells

**`metric-cell`** —— Overview 五格中的计数格：大数字（headline 24px/600）+ caption 标签 + 钻取链接。背景 surface-1，12px 圆角，1px 边线。
**`data-card`** —— 表格容器（最近审计、最近主动引用、事件流）。

### Tables

- antd Table size="middle"，容器卡片化（surface-1 + 边线 + 12px 圆角）。
- 行 hover = surface-2；可钻取行整行可点击并带 hover 指针。
- 标识符列（matchId/userId/traceId）用 mono 字体 + 复制能力（可后续增强）。
- 空态用 antd Empty（中文文案），不用骨架屏占位超过一次轮询周期。

### Status & Badges

**`status-badge`** —— pill 徽标：在线（天蓝点 + 灰底）、已答（绿）、待答（黄）、过期（红）、memory 降级（红）。语义色只上点和文字，不上整行背景。

### Navigation

**`side-nav`** —— 深色 sider：画布色背景，默认文字 `{colors.ink-muted}`，激活项文字球场橙 + surface-1 背景 + 左侧 3px 橙色指示条。头部 56px：左侧页面上下文（面包屑），右侧运营员姓名 + 退出按钮。

## Do's and Don'ts

### Do

- 把 `{colors.canvas}`（#070B12）锚定为唯一页面底色 —— 蓝调夜空是有意的。
- 球场橙只用于：主操作、激活导航、关键审计强调。稀缺即高级。
- 天蓝只用于链接与在线/信息态，与橙形成"主操作 vs 跳转"的分工。
- 用表面阶梯 + 发丝线表达层级，避免跳级（canvas → surface-1 → surface-2）。
- 语义色仅表达状态语义（健康/积压/降级、在线/离线）。
- 所有写操作（PATCH threads、吊销、画像删除）带确认与 loading 态。

### Don't

- 不出浅色主题（运营台只有暗色）。
- 不把球场橙当区块背景或大面积填充 —— 橙是点缀不是底色。
- 不引入第二组强调色（紫、粉等）。
- 不用 #000000 纯黑。
- 不给 CTA 用 pill 圆角（pill 只属于状态徽标）。
- 不用阴影堆层级 —— 用边线。
- 不在表格里堆 emoji 图标 —— 用 antd 图标或纯文字徽标。

## Responsive Behavior

运营台为桌面优先（≥1280px 全量五格）；1024px 以下五格降两列；768px 以下单列、侧栏可折叠（antd Sider breakpoint="lg" collapsible）。表格横向滚动（scroll.x），不隐藏列。

## antd ConfigProvider 映射

```ts
theme={{
  algorithm: theme.darkAlgorithm,
  token: {
    colorPrimary: '#FF6B35',
    colorInfo: '#55A8FF',
    colorSuccess: '#5FCB8B',
    colorWarning: '#F2C94C',
    colorError: '#F05D5E',
    colorBgBase: '#070B12',
    colorBgLayout: '#070B12',
    colorBgContainer: '#111824',
    colorBgElevated: '#1E2A3E',
    colorText: '#F4F1E8',
    colorTextSecondary: '#AAB4C0',
    colorTextTertiary: '#7C8794',
    colorBorder: '#253142',
    colorBorderSecondary: '#253142',
    borderRadius: 8,
    borderRadiusLG: 12,
    fontFamily: '"PingFang SC", "Microsoft YaHei", "Noto Sans SC", system-ui, sans-serif',
    fontSize: 14,
  },
}}
```

## Iteration Guide

1. 一次只改一个组件，引用其 `components:` token 名。
2. 新区块先决定它落在哪一级表面（canvas / surface-1 / surface-2）。
3. 默认正文 `{typography.body}` 400 权重。
4. 新增变体作为独立组件条目。
5. 把球场橙当稀缺资源：主操作、激活态、审计重点，仅此三处。

## Known Gaps

- 色板取自 `app_theme.dart` 常量，未做 WCAG 全量对比度审计；ink-muted #AAB4C0 在 surface-1 上对比度约 6.5:1，正文安全。
- 移动端不是运营台目标形态，断点仅为兜底。
- antd v6 组件内部衍生色（hover/active 梯度）由算法从 colorPrimary 推导，个别衍生色与手写 hover/active token 有细微出入，以 ConfigProvider token 为准。
