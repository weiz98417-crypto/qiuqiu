# Character Settings: 人格参数的三入口一状态

## Why

人格参数只有话痨档有正经面：InitiativeMode/AnalysisAppetite/调侃许可只能靠中文触发词改（用户不可发现、运营不可见、不可组合）。市面人格自定义已是标配，但球球的差异化是"不捏人设"——设置面调的是互动规范，不是人格。

## What Changes

- `internal/relationship` 增 CharacterSettings 服务：统一持四参数（话痨档 + InitiativeMode + AnalysisAppetite + 调侃许可；Profanity 留运营台，Q10）与不变式（**只调互动规范、永不触碰 Character Stance 价值观**）。
- **三入口一状态**（Q19）：cue 词（现有触发词，原样）+ WS `set_character` 消息（即改即 ack，对齐 set_talkativeness 模式）+ HTTP `/api/me/character`（portrait_api 同款会话鉴权）；按用户独立持久化（migration 048 user_character_settings）——relationship_states 按 (user, match) 存互动规范，人格设置需要跨场稳定的一份状态。
- 变更进 Interaction Ledger（`character.updated`，含入口与前后值）；运营台控制台只读展示。

## User Stories

1. As a 用户, I want 在设置接口里明确调"少分析多点情绪", so that 不用猜触发词。
2. As a 运营员, I want 在干预台看到用户当前互动规范, so that 干预有依据。

## Non-goals

- 客户端设置 UI（后置）；Profanity 用户面；Character Stance/价值观可调（永不）。
- cue 词入口迁移到新状态（如实收缩：cue 词仍写 relationship_state 旧路径，收敛为后续任务）；互动规范接入 policy 运行时读取（后续任务）。

## Success Criteria

- 全量 go test + eval 绿；三入口写同一 state 的一致性测试；Ledger 记账断言。
