# Settings In Policy: 让人格设置真正生效

## Why

character-settings 存了三个互动规范槽位，但 policy.go 不读它们——用户改"分析胃口 deep"后球球行为不变。设置面当前是摆设。这是功能债：改它 = policy 决策时读设置覆盖默认值 = 改球球的行为，需要行为 eval 而非纯重构，故独立立项（打磨轮保持零行为变化）。

## What Changes

- relationship.Director.Apply 前把 user_character_settings 三槽位合并进回合的偏好输入：initiative → InitiativeMode、analysis_appetite → AnalysisAppetite、banter_level → 调侃许可；**设置值显式指定时优先于 cue 推断**，未设置（空串）维持现状推断。
- 映射细节（枚举对齐、幅度、冲突时 cue 与设置谁赢）在实施前经一轮小 grilling 定稿并以 ADR 记录（确定性决策内核第一次消费外部设置——需要可解释性 reason code 与行为 eval 方案）。
- 新行为 eval：设置后对应行为维度变化的断言（scripted settings fixture，离线）。

## User Stories

1. As a 用户, I want 在设置面改"分析胃口 deep"后球球真的讲得更深, so that 设置不是摆设。
2. As a 可解释性守门人, I want 设置生效时 trace 留 reason code, so that "球球为什么这样答"始终有账可查。

## Non-goals

- 不动 Portrait/Recall 进决策（memory-in-policy 另立）。
- 不做客户端 UI；不改 cue 词与 WS/HTTP 入口形状。

## Success Criteria

- 全量 go test + eval 绿；新行为 eval 断言设置生效与未设置时行为不变（双向）。
