---
id: 33-immersive-stage
title: 沉浸式舞台与 Live2D 交互
source_chapter: docs/球球全套资料/4.产品设计/33-沉浸式舞台与Live2D交互设计.md
status_summary: { implemented: 10, partial: 1, planned: 0, concept: 2 }
---

# 沉浸式舞台与 Live2D 交互

现实中的"舞台"是一个 392 行的 Flutter 控件：WebView 里跑 PIXI + Cubism，永远不接收指针事件，台词字幕和比分控件叠在它上面。旧文档设想的五层空间结构、四种可见性模式、动作预算体系都没有被实现——实现走了一条更窄但更可控的路：舞台是纯背景，词汇来自白名单，失败时显示重试按钮而不是表演。

## 系统实际怎么工作

**舞台是不可触碰的背景。** `Live2dView` 用 `InAppWebView` 加载 Cubism 渲染页，整个视图包在 `IgnorePointer` 里 [live2d_view.dart:156-160](../../../client/lib/widgets/live2d_view.dart)；web 端是同源 iframe，桥接层反复把 `pointerEvents` 置为 none，把指针让给上层控件 [live2d_bridge_web.dart:50-58](../../../client/lib/widgets/live2d_bridge_web.dart)。模型本身 `autoInteract:false`（live2d_view.dart:340），点它、长按它、盯它都没有任何反馈——旧文档用一整章禁止的"互动手势、凝视、索取回应"，在实现里是被物理删除而非被规则约束。

**事实和控制永远在舞台上面。** Stack 第一层是 Live2dView [match_screen.dart:2305-2310](../../../client/lib/screens/match_screen.dart)，之上依次是：顶部栏（返回、设置、连接状态、比分头）[match_screen.dart:1185-1218](../../../client/lib/screens/match_screen.dart)、比赛状态轮播 [match_screen.dart:2326-2338](../../../client/lib/screens/match_screen.dart)、底部可滚动字幕卡 [match_screen.dart:2372-2382](../../../client/lib/screens/match_screen.dart)。进球庆祝也是独立覆盖层：Lottie goal-burst 一次播放，同样 IgnorePointer [match_screen.dart:2311-2325](../../../client/lib/screens/match_screen.dart)。

**舞台词汇只有一条合法来源。** 服务端在比赛反应产生 `PresentationPlan` 且 Expression 非空时，推送 `type:"presentation"` 消息 [main.go:591-599](../../../backend/cmd/server/main.go)；客户端经白名单+别名校验 [presentation_state.dart:2-51](../../../client/lib/services/presentation_state.dart)，不合格整包丢弃 [presentation_state.dart:88-115](../../../client/lib/services/presentation_state.dart)。还有一条 legacy `expression` 通道，同样必须先过 `normalizeExpression` [match_screen.dart:363-373](../../../client/lib/screens/match_screen.dart)。表情/动作最终经静态映射表落到 Cubism：13 表情→7 个 expression 索引（live2d_view.dart:324），7 动作名→模型动作组，cheer 实际播放模型的 celebrate（live2d_view.dart:358-368）。

**表演有限时长，只回落不升级。** holdMs 钳制 0..10000ms，Timer 到期按 returnMode 回到 listening/idle/focus resting 态 [presentation_state.dart:118-129](../../../client/lib/services/presentation_state.dart) [match_screen.dart:941-949](../../../client/lib/screens/match_screen.dart)。客户端不生成任何自发动作——"可见性不等于主动性"在这里的实现形式是：客户端根本没有主动能力。

**失败是控件，不是表演。** native 端轮询 `window.modelReady` 30 次失败后显示"重新请球球入场"按钮，`onModelError` 上报错误（live2d_view.dart:102-122、230-236、249-283）；web 端 8 秒超时直接放行避免卡死（live2d_view.dart:63-68）。模型资产走 `qiuqiu://asset` 自定义 scheme，拒绝含 `..` 的路径（live2d_view.dart:185-214）。WebSocket 断线时右上角给"重新连接"按钮，字幕照常工作 [match_screen.dart:2362-2371](../../../client/lib/screens/match_screen.dart)——没有任何"角色累了"式拟人化失败文案。

**口型是随机近似。** `setSpeaking` 只切换布尔值，50ms 定时器在说话时给 `ParamJawOpen` 随机 0.25..1.0 幅度，停止时衰减回 0 [live2d_view.dart:370-385](../../../client/lib/widgets/live2d_view.dart)。没有音素级口型同步。

## 与旧设计的差异

| 旧设计（33 章） | 现实 | 锚点 |
| --- | --- | --- |
| 五层舞台结构（安全控制/事实/任务/角色舞台/环境氛围） | 三层：非交互舞台背景 → 事实/控制覆盖层 → 字幕层；无"氛围层" | [live2d_view.dart:156-160](../../../client/lib/widgets/live2d_view.dart)、[match_screen.dart:2326-2382](../../../client/lib/screens/match_screen.dart) |
| 四种可见性模式（完整/紧凑/静态/隐藏）可切换 | 无任何可见性模式；舞台常驻但零交互，控制靠音频/字幕偏好 | [preferences_service.dart:76-82](../../../client/lib/services/preferences_service.dart) |
| 动作强度四级、闲置动作预算、动作目的审查表 | 无闲置动作库、无强度分级；动作只来自后端 PresentationPlan 白名单 | [presentation_state.dart:2-51](../../../client/lib/services/presentation_state.dart) |
| 口型围绕当前回应、语音语调与动作同层确定性 | 口型为随机幅度近似；确定性由后端 ContentPolicy/白名单保证，不在渲染层 | [live2d_view.dart:370-385](../../../client/lib/widgets/live2d_view.dart) |
| 素材失败要"诚实显示已降级"状态 | 已实现且更朴素：进度提示 + 重试按钮 + 错误上报 | [live2d_view.dart:249-283](../../../client/lib/widgets/live2d_view.dart) |
| 用户点击角色触发已授权解释/设置 | 未实现：角色区域完全不接收事件 | [live2d_bridge_web.dart:50-58](../../../client/lib/widgets/live2d_bridge_web.dart) |

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 舞台为非交互背景层（IgnorePointer + pointerEvents none） | implemented-at | [live2d_view.dart:156-160](../../../client/lib/widgets/live2d_view.dart) | code |
| 事实与控制叠于舞台之上、独立可达 | implemented-at | [match_screen.dart:1185-1218](../../../client/lib/screens/match_screen.dart) | code |
| presentation/legacy expression 双通道且都过白名单 | implemented-at | [match_screen.dart:346-373](../../../client/lib/screens/match_screen.dart) | code |
| 13 表情→7 索引、7 动作→模型动作组静态映射 | implemented-at | [live2d_view.dart:324-368](../../../client/lib/widgets/live2d_view.dart) | code |
| 口型为随机幅度近似（50ms JawOpen） | implemented-at | [live2d_view.dart:370-385](../../../client/lib/widgets/live2d_view.dart) | code |
| 加载失败→进度/重试按钮，非表演化 | implemented-at | [live2d_view.dart:249-283](../../../client/lib/widgets/live2d_view.dart) | code |
| 资产自定义 scheme 服务 + 路径穿越防护 | implemented-at | [live2d_view.dart:185-214](../../../client/lib/widgets/live2d_view.dart) | code |
| 进球庆祝为非交互 Lottie 覆盖层 | implemented-at | [match_screen.dart:2311-2325](../../../client/lib/screens/match_screen.dart) | code |
| 舞台故障不阻塞重连与字幕 | implemented-at | [match_screen.dart:2362-2382](../../../client/lib/screens/match_screen.dart) | code |
| 表演只由后端计划驱动、到期回落 | implemented-at | [presentation_state.dart:118-129](../../../client/lib/services/presentation_state.dart) | code |
| 舞台专属即时控制（收起/静态/减少动效） | partial | [preferences_service.dart:76-82](../../../client/lib/services/preferences_service.dart) | code |
| 五层结构、四种可见性模式、强度四级 | concept | 旧 33 章 63-97 行 | doc |
| 闲置动作库与触碰互动手势 | concept | [live2d_view.dart:156-160](../../../client/lib/widgets/live2d_view.dart)（证伪锚） | code |
