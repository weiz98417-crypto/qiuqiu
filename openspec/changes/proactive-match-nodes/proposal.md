# Proactive Match Nodes: 中场闲聊 / 失球安慰 / 赛后复盘邀约

## Why

陪伴节奏有两个空白：中场 15 分钟没人说话、终场哨后没有复盘邀约（Fay 式日程驱动主动对话的赛事版）。另：ADR-0019 波留尾「失球安慰需赛程配对」——失球方↔用户订阅球队的配对条件现在凑齐了（season-subscription 的按用户球队 + 第三波下沉的 internal/teamalign 叶子包）。period 切换信号早已作为比赛事件流入 agent（现被静音门压着）。

## What Changes（三节点各用既有机械，不发明新东西）

- **中场闲聊**：period 切换到 halftime 的事件走既有事件反应路径（放开现在的静音门），realizer 带上半场摘要语境（比分+关键事件+当前情绪+未完话题）；让路（用户说话）、quiet 禁用照旧；限频天然成立（一半场一次）。
- **失球安慰**：进球事件反应的前置条件——失球方经 teamalign 命中用户订阅球队才触发（ADR-0019 的 affect 偏置已把情绪拉低，本节点补安慰话轮）；realizer 话轮；每场 ≤1、quiet 禁用。
- **赛后复盘邀约**：fulltime 事件时创建 `due=FT+15min` 的 Reminder（复用 045 proactive_reminders store/SweepLoop/outbox 离线补递/ADR-0015 引用码全套）；文案走 realizer 引用当日 Shared Moment（FT+15min 时反思拍已按对准的比赛冲洗——吃 reflection-attribution 红利）；`FT+2h` 未送达→suppressed→转记忆素材（照抄赛前提醒过期纪律）。

## User Stories

1. As a 用户, I want 中场球球主动聊两句上半场, so that 休息时也有陪伴。
2. As a 用户, I want 我队丢球时球球先安慰再聊战术, so that 情绪被接住。

## Non-goals

- 终场即时告别话轮（未在共识内，如需要另立项）；订阅展开逻辑改动；主动调度器架构变更。

## Success Criteria

- go test + evals F 域节点例（中场触发/未触发、失球命中订阅才安慰、复盘邀约 due/过期/补递）；全量门禁绿。
