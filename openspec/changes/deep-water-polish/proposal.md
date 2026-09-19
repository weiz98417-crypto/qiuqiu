# Deep Water Polish: 四项记录在案的深水区收尾

## Why

前几轮记录在案的四项深水区：relationship policy 的中文触发词表散在 5 个分类器函数（inferUserCues / interactionFeedback / isProfanityBoundary / isTacticalQuestion / containsPersonalInsult），而仓库已有词汇表先例（presentation_vocabulary.go）；ResponseDeliveryService.Deliver 手工配对锁跨约 6 个早退（新增早退即死锁/双解锁风险）；WatchSession.Recoveries 静默吞 recovery 源错误；directordraft/service.go 706 行混合转写启发式分类与草稿装配。

## What Changes

- relationship：触发词收敛为 policy_vocabulary.go 单表，5 个分类器改为查表（行为等价，词集原样）。
- conversation：Deliver 拆「加锁规划半 + 无锁 I/O 半」，用 scoped lock 辅助消除手工配对。
- directordraft：transcript 启发式（isPlausibleMatchTranscript / matchHints）与草稿装配（enrichExtraction / normalize）分离为两个文件（同包）。
- Recoveries 吞错已在 server-residual-polish 顺带完成，此处只做回归确认。

## User Stories

1. As a 策略维护者, I want 触发词一张表, so that 调语气不找五个函数。
2. As a 投递维护者, I want 锁的作用域结构化, so that 新早退不会死锁。
3. As a 语音草稿维护者, I want 启发式与装配分开, so that 两类变更互不踩。

## Non-goals

- 不改任何触发词内容与策略行为。
- 不动 scheduler / applyPolicy（已是深 module）。