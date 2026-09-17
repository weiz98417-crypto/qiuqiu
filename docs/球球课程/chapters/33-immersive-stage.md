---
id: 33-immersive-stage
title: 沉浸式舞台与 Live2D 交互
source_chapter: docs/球球全套资料/4.产品设计/33-沉浸式舞台与Live2D交互设计.md
status_summary: { implemented: 12, partial: 1, planned: 0, concept: 2 }
---

# 沉浸式舞台与 Live2D 交互

现实中的"舞台"是一个 642 行的 Flutter 控件：WebView 里跑 PIXI + Cubism，永远不接收指针事件，台词字幕和比分控件叠在它上面。旧文档设想的五层空间结构、四种可见性模式、动作预算体系都没有被实现——实现走了一条更窄但更可控的路：舞台是纯背景，词汇来自单一映射表（ADR-0007），失败时显示重试按钮而不是表演。

## 系统实际怎么工作

**舞台是不可触碰的背景。** `Live2dView` 用 `InAppWebView` 加载 Cubism 渲染页，整个视图包在 `IgnorePointer` 里 [live2d_view.dart:188-200](../../../client/lib/widgets/live2d_view.dart)；web 端是同源 iframe，桥接层反复把 `pointerEvents` 置为 none，把指针让给上层控件 [live2d_bridge_web.dart:5-33](../../../client/lib/widgets/live2d_bridge_web.dart)。模型本身 `autoInteract:false`（live2d_view.dart:549），点它、长按它、盯它都没有任何反馈——旧文档用一整章禁止的"互动手势、凝视、索取回应"，在实现里是被物理删除而非被规则约束。

**事实和控制永远在舞台上面。** 舞台 Stack 第一层是 Live2dView [match_screen.dart:2377-2385](../../../client/lib/screens/match_screen.dart)，之上依次是：比分（大字号模式的顶部 `_HorizontalScoreBar` / 常规模式的左侧 `_ScoreRail`，后者带陪看设置入口）[match_screen.dart:2250-2304](../../../client/lib/screens/match_screen.dart)、舞台内顶部行（比赛状态轮播、连接标记、切换/设置/离开菜单）[match_screen.dart:2401-2426](../../../client/lib/screens/match_screen.dart)、底部可滚动字幕卡 [match_screen.dart:2447-2457](../../../client/lib/screens/match_screen.dart)。进球庆祝也是独立覆盖层：Lottie goal-burst 一次播放，同样 IgnorePointer [match_screen.dart:2386-2400](../../../client/lib/screens/match_screen.dart)。

**舞台词汇只有一条合法来源。** 服务端在比赛反应产生 `PresentationPlan` 且 Expression 非空时，推送 `type:"presentation"` 消息 [main.go:719-728](../../../backend/cmd/server/main.go)；客户端经白名单+别名校验 [presentation_state.dart:19-40](../../../client/lib/services/presentation_state.dart)，不合格整包丢弃 [presentation_state.dart:209-241](../../../client/lib/services/presentation_state.dart)。还有一条 legacy `expression` 通道，同样必须先过 `normalizeExpression` [match_screen.dart:377-387](../../../client/lib/screens/match_screen.dart)。表情/动作最终落到 Cubism 的映射单一源自 presentation-map.json：13 表情→7 个 expression 文件索引、动作名→（组，变体）；web 页与内嵌页运行时 fetch 它，Dart 常量是被契约测试锁定的同步镜像 [presentation-map.json:2-35](../../../client/assets/live2d/models/qiuqiu/presentation-map.json) [live2d_view.dart:472-533](../../../client/lib/widgets/live2d_view.dart)——旧的三份互不一致的内联 exprMap 已删除，`thinking` 不再指向空参数文件（暂定绑到 3）。

**回合相位与闲置档位驱动身体。** 相位表：user_speaking→listening/listen_01、understanding→thinking/think、qiuqiu_speaking→chat/speak_01、session_open→happy/hello，fulltime 触发一次性 happy/wave 欢送 [match_session_controller.dart:190-224](../../../client/lib/services/match_session_controller.dart)。闲置时 IdleTierPicker 把最近表演的 affect 映射到三档（deflated/calm/energetic），各取 idle_01/02/03 [idle_tier_picker.dart:5-48](../../../client/lib/services/idle_tier_picker.dart)；JS idle 调度器只在相位为 idle 且无持中表演时补位 [live2d.html:793-806](../../../client/assets/live2d/live2d.html)。

**表演有限时长，hold 窗口内后端权威。** holdMs 钳制 0..10000ms，Timer 到期按 `presentationReturnState` 的四值回落到真实目标：watching/decay_to_focus→focus、decay_to_listening→listening/listen_01、decay_to_idle→idle 档位 [presentation_state.dart:250-268](../../../client/lib/services/presentation_state.dart) [match_screen.dart:996-1004](../../../client/lib/screens/match_screen.dart)。hold 窗口内 web 渲染面的 idle 调度器与静音复位都让位（`armPresentationHold`）[live2d.html:807-820](../../../client/assets/live2d/live2d.html)——"可见性不等于主动性"的实现形式是：客户端能上身的身体只有映射表里有的行。

**失败是控件，不是表演。** native 端轮询 `window.modelReady` 30 次失败后显示"重新请球球入场"按钮，`onModelError` 上报错误（live2d_view.dart:134-154、265-271、308-318）；web 端 8 秒超时直接放行避免卡死（live2d_view.dart:93-101）。模型资产走 `qiuqiu://asset` 自定义 scheme，拒绝含 `..` 的路径（live2d_view.dart:217-249）。WebSocket 断线时右上角给"重新连接"按钮，字幕照常工作 [match_screen.dart:2437-2446](../../../client/lib/screens/match_screen.dart)——没有任何"角色累了"式拟人化失败文案。

**口型由 TTS 音频驱动。** wLipSync（WASM MFCC→viseme）分析平台播放器同源的那份音频，得出五参数嘴型，50ms 定时器平滑应用 [live2d_view.dart:330-441](../../../client/lib/widgets/live2d_view.dart) [live2d_view.dart:598-637](../../../client/lib/widgets/live2d_view.dart)；分析器不可用时退化为音频包络，再退化为随机开合。仍是近似，不是音素级同步。

## 与旧设计的差异

| 旧设计（33 章） | 现实 | 锚点 |
| --- | --- | --- |
| 五层舞台结构（安全控制/事实/任务/角色舞台/环境氛围） | 三层：非交互舞台背景 → 事实/控制覆盖层 → 字幕层；无"氛围层" | [live2d_view.dart:188-200](../../../client/lib/widgets/live2d_view.dart)、[match_screen.dart:2401-2457](../../../client/lib/screens/match_screen.dart) |
| 四种可见性模式（完整/紧凑/静态/隐藏）可切换 | 无任何可见性模式；舞台常驻但零交互，控制靠音频/字幕偏好 | [preferences_service.dart:76-82](../../../client/lib/services/preferences_service.dart) |
| 动作强度四级、闲置动作预算、动作目的审查表 | 无强度分级与预算；动作只来自 presentation-map.json 的表行：后端路由 + 相位表 + idle 三档 | [presentation-map.json:2-61](../../../client/assets/live2d/models/qiuqiu/presentation-map.json)、[idle_tier_picker.dart:5-48](../../../client/lib/services/idle_tier_picker.dart) |
| 口型围绕当前回应、语音语调与动作同层确定性 | 口型由 TTS 音频 viseme 近似驱动；确定性由后端 ContentPolicy/白名单保证，不在渲染层 | [live2d_view.dart:330-441](../../../client/lib/widgets/live2d_view.dart) |
| 素材失败要"诚实显示已降级"状态 | 已实现且更朴素：进度提示 + 重试按钮 + 错误上报 | [live2d_view.dart:308-318](../../../client/lib/widgets/live2d_view.dart) |
| 用户点击角色触发已授权解释/设置 | 未实现：角色区域完全不接收事件 | [live2d_bridge_web.dart:5-33](../../../client/lib/widgets/live2d_bridge_web.dart) |

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 舞台为非交互背景层（IgnorePointer + pointerEvents none） | implemented-at | [live2d_view.dart:188-200](../../../client/lib/widgets/live2d_view.dart) | code |
| 事实与控制叠于舞台之上、独立可达 | implemented-at | [match_screen.dart:2377-2457](../../../client/lib/screens/match_screen.dart) | code |
| presentation/legacy expression 双通道且都过白名单 | implemented-at | [match_screen.dart:356-387](../../../client/lib/screens/match_screen.dart) | code |
| 表情/动作映射单源 presentation-map.json（三面派生+契约测试） | implemented-at | [presentation-map.json:2-35](../../../client/assets/live2d/models/qiuqiu/presentation-map.json) | code |
| 口型由 TTS 音频 viseme 驱动（包络/随机退化） | implemented-at | [live2d_view.dart:330-441](../../../client/lib/widgets/live2d_view.dart) | code |
| 加载失败→进度/重试按钮，非表演化 | implemented-at | [live2d_view.dart:308-318](../../../client/lib/widgets/live2d_view.dart) | code |
| 资产自定义 scheme 服务 + 路径穿越防护 | implemented-at | [live2d_view.dart:217-249](../../../client/lib/widgets/live2d_view.dart) | code |
| 进球庆祝为非交互 Lottie 覆盖层 | implemented-at | [match_screen.dart:2386-2400](../../../client/lib/screens/match_screen.dart) | code |
| 舞台故障不阻塞重连与字幕 | implemented-at | [match_screen.dart:2437-2457](../../../client/lib/screens/match_screen.dart) | code |
| hold 窗口所有权 + ReturnMode 四值真实回落 | implemented-at | [live2d.html:793-820](../../../client/assets/live2d/live2d.html)、[presentation_state.dart:250-268](../../../client/lib/services/presentation_state.dart) | code |
| 回合相位驱动身体（phases 表 + fulltime wave） | implemented-at | [match_session_controller.dart:190-224](../../../client/lib/services/match_session_controller.dart) | code |
| 闲置动作以 idle 三档受限存在 | implemented-at | [idle_tier_picker.dart:5-48](../../../client/lib/services/idle_tier_picker.dart) | code |
| 舞台专属即时控制（收起/静态/减少动效） | partial | [preferences_service.dart:76-82](../../../client/lib/services/preferences_service.dart) | code |
| 五层结构、四种可见性模式、强度四级 | concept | 旧 33 章 63-97 行 | doc |
| 注视镜头与触碰互动手势 | concept | [live2d_view.dart:188-200](../../../client/lib/widgets/live2d_view.dart)（证伪锚） | code |
