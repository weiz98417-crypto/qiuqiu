# Delivery Outcome Uplink: 投递结果实报闭环

## Why

CONTEXT.md 对 Delivery Outcome 的定义是「用户实际收到了什么」，但投递七态（backend/internal/conversation/delivery.go:12-20：planned/text_delivered/audio_started/completed/interrupted/skipped/failed）中 completed/interrupted 由服务端播放调度推断（cmd/server/watchconnection.go 的 observeReplyOutcome），客户端上行只有 `reply_displayed`——互动账本记的是二手事实，延迟与打断诊断没有 ground truth。同时 voice-duplex 1.4（自打断检测）需要客户端实报 interrupted 作依据，本 change 是其前置通道。

## What Changes

- 客户端上行 WS 消息 `playback_result {deliveryKey, state, reason}`：state 实报 completed / interrupted / skipped——映射 flutter_soloud 播放器既有事件（播完=completed；pause() 停播=interrupted；被新播放顶替/静音跳过=skipped）。deliveryKey 由下发音频消息携带，客户端透传回执。
- 服务端：收到实报→写投递账本（复用既有 `playback_result` Kind 与七态），Source 标 client；服务端推断降为「下发后超时无回执时兜底」，Source 标 server_inferred。迟到回执（回合已终态）不覆盖、记账为 client_late。
- `reply_displayed` 上行**不动**（已验证链路不碰，不做合并迁移）。
- 卫生顺手项：`evals/schema/eval-case.schema.json` 同步到实有 12 目录——`suite.enum` 与 `id.pattern` 的三-suite 前缀锁**两处都要改**（已核实无脚本依赖该 schema，纯同步）。

## User Stories

1. As a 球球（记忆消费者）, I want 账本记用户实际播完/被打断的内容, so that 后续回合知道用户真听到了什么。
2. As a 运营/调试者, I want 投递结果有 ground truth, so that 打断与延迟问题可诊断、可归因。
3. As a 抢断实现者（voice-duplex）, I want interrupted 实报通道先就位, so that 1.4 自打断检测有数据依据。

## Non-goals

- `reply_displayed` 合并或迁移。
- 投递七态枚举本身的改动。
- 播放进度/心跳流——只报终态，不做流式进度。
- 修复历史上的服务端推断逻辑缺陷（只降级其权威性，不重写）。

## Success Criteria

- 确定性 eval：实报路由（有回执→账本记实报值；无回执→兜底推断值；迟到回执→client_late 不覆盖）。
- 同一 deliveryKey 的实报与推断在账本可区分（Source 字段）。
- 删除测试：删上行处理则账本回退纯推断——实报路径独立成缝，主链路不塌。
- pr tier 绿。
