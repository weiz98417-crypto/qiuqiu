# Reflection Attribution: 赛后反思对准到人

## Why

`runReflectionBeat`（cmd/server/main.go）的 post_match 拍用 `ended[0]` 把**第一场终场**挂给**所有活跃用户**——没看那场的用户也被赛后反思；同 tick 多场终场时 `TakeMatchEnds()` 全取但只消费第一场，其余场次的终场信号被丢给 idle 拍兜底。反思骨架本身（冲洗→Memobase 合成→画像→带引用审计）已在且正确，只差对准。CONTEXT.md「Reflection」词条已随 2026-09-23 评审对齐现实（被引用的是画像，审计非记忆条目）。

## What Changes

- `memory.Queue` 增 per-user 终场归因：用户近期实际互动过的比赛（Ledger/最近信号来源可查）与终场集合求交，逐用户以其看过的比赛触发 post_match 反思；没看任何终场比赛的用户不触发。
- 同 tick 多场终场全部消费（按用户分组触发，不再只吃 `ended[0]`）。
- 无互动记录可查时保守降级：维持现状（全体活跃用户 + 第一场）还是跳过——按实施时数据可得性裁剪并记录。

## User Stories

1. As a 用户, I want 看完球当晚球球消化的是我看的那场, so that 第二天聊起来不串场。

## Non-goals

- 反思合成机制（Memobase 内部，不自研）；ReflectionAudit 结构；idle 拍节奏。

## Success Criteria

- go test：多场终场+不同用户观看不同比赛的归因断言；单场单用户行为不变。
