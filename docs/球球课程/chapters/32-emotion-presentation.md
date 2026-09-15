---
id: 32-emotion-presentation
title: 情绪动力学与 Live2D 表演编排
source_chapter: docs/球球全套资料/4.产品设计/32-情绪动力学与Live2D表演编排设计.md
status_summary: { implemented: 12, partial: 1, planned: 0, concept: 1 }
---

# 情绪动力学与 Live2D 表演编排

球球的"情绪"在后端是五个浮点数（`AffectState`），在客户端是一个带白名单的表演包（`PresentationPlan`），中间没有任何状态机。旧设计文档描述的 expression 引擎（连续维度引擎、七舞台状态、防抖衰减）从未上线，已作为死代码删除（ADR-0005）；本章只讲真实在跑的这条链路。

## 系统实际怎么工作

**情绪是五个字段，不是引擎。** `AffectState` 只有 valence/arousal/tension/confidence/engagement 五个浮点值 [types.go:342-349](../../../backend/internal/relationship/types.go)。比赛事件按类型叠加增量：goal 给愉快和激活各 +0.8，var_check 加紧张 +0.6、减确信 -0.5，goal_cancelled 减愉快 -1.2 [affect.go:9-40](../../../backend/internal/relationship/affect.go)，由 `Director.Apply` 在比赛事件信号进入时调用 [policy.go:71-72](../../../backend/internal/relationship/policy.go)。衰减是指数回基线：arousal 90 秒回到 0.2，tension/confidence 2 分钟，valence/engagement 10 分钟 [affect.go:53-67](../../../backend/internal/relationship/affect.go)——这是状态衰减，不是旧文档那种"表演冷却引擎"。

**表演包是唯一合同。** 后端产出的不是"表情指令"，而是一份 `PresentationPlan`：Affect + Expression + Motion + VoiceStyle + VoiceEnergy + VoiceSpeed + HoldMS + ReturnMode [types.go:351-360](../../../backend/internal/relationship/types.go)。它内嵌在 `Decision.Presentation` 里，由 `presentationFor` 按"谁触发了这回合"分支组装 [affect.go:69-123](../../../backend/internal/relationship/affect.go)：进球→excited/cheer/2600ms，VAR→tense/hold/2200ms，进球被吹→deflated/settle/low_disappointed/2800ms，普通用户回合→chat/speak。用户观察被核实走独立模板：确认→excited/cheer/energy 0.9，被驳倒→deflated/slump/energy 0.45 [agent.go:491-500](../../../backend/internal/companion/agent.go)。

**客户端只认白名单。** 13 个表情、7 个动作、8 个语音风格、4 个回落模式是硬编码集合，`low→sad`、`tense→nervous`、`deflated→sad`、`slump→idle`、`hold→focus`、`settle→idle` 是别名表 [presentation_state.dart:2-51](../../../client/lib/services/presentation_state.dart)。别名表存在的原因很直接：后端 `presentationFor` 和 `observationPresentation` 至今仍发旧词汇，客户端负责翻译。任何一个字段越界，整个表演包返回 null 丢弃 [presentation_state.dart:88-115](../../../client/lib/services/presentation_state.dart)——宁可不表演，不猜语义。

**表演有限时长且会回落。** holdMs 钳制在 0..10000ms，客户端起 Timer，到期后按 `returnMode` 映射回 resting 态（decay_to_listening/idle/focus）[presentation_state.dart:105-129](../../../client/lib/services/presentation_state.dart) [match_screen.dart:941-949](../../../client/lib/screens/match_screen.dart)，且 `returnPresentation` 用 `identical` 校验，旧定时器不会覆盖新表演 [match_session_controller.dart:376-385](../../../client/lib/services/match_session_controller.dart)。

**激活度门控说话方式。** arousal≥0.7 且用户许可才给 mild 冒犯语，≥0.9 且关系阶段够深才给 strong [policy.go:412-418](../../../backend/internal/relationship/policy.go)；arousal≥0.7 时直接抑制"追问偏好"的 ask 动作 [policy.go:179-181](../../../backend/internal/relationship/policy.go)。语音能量/语速也从 arousal 线性推导 [affect.go:74-76](../../../backend/internal/relationship/affect.go)。用户说"不想分析/缓会儿/先别说话"时，表演压到 low/quiet/0.2/3200ms，决策层同时给 ActSilence [affect.go:80-87](../../../backend/internal/relationship/affect.go) [policy.go:117-118](../../../backend/internal/relationship/policy.go)。

## 与旧设计的差异

| 旧设计（32 章） | 现实 | 锚点 |
| --- | --- | --- |
| expression 引擎：10 表情态、防抖、自动衰减 | **该引擎从未接线，已作为死代码删除**；表演走 PresentationPlan 单一合同 | [ADR-0005:3-13](../../adr/0005-presentation-single-contract.md) |
| 五连续维度 + 七舞台状态双层模型，mermaid 状态机 | 七个舞台状态、状态机转场无任何代码；五维度以 `AffectState` 字段存活，仅喂给策略门控 | [types.go:342-349](../../../backend/internal/relationship/types.go) |
| 衰减/转场最小单元、冷却与重复抑制的编排规则 | 真实机制是指数状态衰减 + 固定 HoldMS + returnMode 回落，无冷却编排层 | [affect.go:53-67](../../../backend/internal/relationship/affect.go)、[presentation_state.dart:105-129](../../../client/lib/services/presentation_state.dart) |
| 用户控制三档密度（安静/只回应/有限提示）、低动态开关 | 无三档模式；有客户端偏好项 + 文本即时指令；talkativeness 上送后端但无人消费 | [preferences_service.dart:76-82](../../../client/lib/services/preferences_service.dart)、[agent.go:1797-1801](../../../backend/internal/companion/agent.go) |
| 表达决策卡、多模态一致性规则表 | 真实形态是"决策（Actions/ContentPolicy）与表达（realize.go）分离 + 白名单整包校验" | [realize.go:24-42](../../../backend/internal/companion/realize.go)、[presentation_state.dart:88-115](../../../client/lib/services/presentation_state.dart) |

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 五维 AffectState 连续情绪 | implemented-at | [types.go:342-349](../../../backend/internal/relationship/types.go) | code |
| 事件驱动情绪增量（goal/VAR/被吹/错失） | implemented-at | [affect.go:9-40](../../../backend/internal/relationship/affect.go) | code |
| 指数衰减回基线（90s/2min/10min） | implemented-at | [affect.go:53-67](../../../backend/internal/relationship/affect.go) | code |
| PresentationPlan 唯一合同（8 字段） | implemented-at | [types.go:351-360](../../../backend/internal/relationship/types.go) | code |
| 事件→表演映射（excited/tense/deflated） | implemented-at | [affect.go:69-123](../../../backend/internal/relationship/affect.go) | code |
| 观察消解表演模板（0.9/0.45 能量） | implemented-at | [agent.go:491-500](../../../backend/internal/companion/agent.go) | code |
| 安静请求→低强度表演 + ActSilence | implemented-at | [affect.go:80-87](../../../backend/internal/relationship/affect.go) | code |
| 客户端白名单 + 别名表，越界整包丢弃 | implemented-at | [presentation_state.dart:2-51](../../../client/lib/services/presentation_state.dart) | code |
| hold 限时 + returnMode 回落 | implemented-at | [match_screen.dart:941-949](../../../client/lib/screens/match_screen.dart) | code |
| arousal 门控冒犯语与追问 | implemented-at | [policy.go:412-418](../../../backend/internal/relationship/policy.go) | code |
| 语音能量/语速随 arousal 推导 | implemented-at | [affect.go:74-76](../../../backend/internal/relationship/affect.go) | code |
| 决策/表达分离 + 438 字节人设提示 | implemented-at | [realize.go:24-42](../../../backend/internal/companion/realize.go) | code |
| 三档密度控制与低动态开关 | partial | [preferences_service.dart:76-82](../../../client/lib/services/preferences_service.dart) | code |
| 双层模型、七舞台状态机、表演冷却引擎 | concept | [ADR-0005:3-13](../../adr/0005-presentation-single-contract.md) | doc |
