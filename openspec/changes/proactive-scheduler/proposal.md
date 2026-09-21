# Proactive Scheduler: 服务端主动调度与赛前提醒

## Why

主动回合的触发完全挂在 WS 连接生命周期上（ProactiveGate 是 watchConnection 私有字段、Scheduler 按会话持有）——没人连接就没有任何主动回合，时间维度的主动性物理上不可能发生。市面证据（Replika 主动 check-in、MLS Sidekick）表明主动关怀是陪伴产品标配；体育场景里"开球前叫我"是用户感知最强的 agent 行为。

## What Changes

- 新增 `internal/proactive`（主动调度 Proactive Scheduling）：提醒簿（Store 双实现：Postgres migration 045 / 内存降级）、时序推导（开球前 30 分钟叫、开球后 30 分钟过期）、过期清扫节拍（SweepLoop：翻 suppressed + 转记忆素材）。
- 新意图 `reminder_request`（能力波 #1 注册表首批用户）：pre_match 且赛程源有 kickoff 才落簿；关键词词表 + router 枚举/prompt 增行 + 置信门控 + 确定性回复。
- C2 引用码第三钥匙 `reminder:<id>`（ADR-0015）：用户显式请求；gate 语义不变。
- 投递：连接时补递 + 连接内 30s 低频检查（020 outbox 模式，送达翻 delivered）；错过静默过期并转记忆素材（Q13）。
- 客户端零新 UI；订阅式提醒后置。

## User Stories

1. As a 球友, I want 说一句"开球前叫我"就能在开球前被叫, so that 我不会错过我关心的比赛。
2. As a 信任守门人, I want 每个主动回合仍带引用码, so that 主动性永远是"有据可引"的。
3. As a 被提醒过但错过的用户, I want 错过不再被补发、而在之后的聊天里自然提起, so that 球球不迟到但记得。

## Non-goals

- 订阅式提醒（关注球队每轮自动）后置为独立技能。
- 真推送（厂商 push 通道）后置；本轮投递面即 App 内。
- 画像口味暂不升级为独立引用码钥匙（ADR-0015 后果条款）。

## Success Criteria

- 全量 go test 绿 + 100 eval 绿（新意图在 evals 中走 scripted router 路径，行为锁定）。
- 提醒簿时序/清扫/意图落簿三面有单元锁。
