# Client Resilience Quickfixes: 重连后过期数据与 dev 兜底进生产

## Why

两处客户端韧性缺口：SocketStatus.connected 分支只跑自动进入逻辑，比赛概览（比分/时钟）只在进房时拉取一次——长断线重连后用户盯着过期比分直到下一个后端事件；matchId 默认 'test'、WS 地址默认模拟器回环 ws://10.0.2.2:8080 的 dev 兜底会进 release 包——无 dart-define 的正式构建静默加入死主机上的 test 比赛。另有 shouldAutoEnterMatch 恒 false 的死标志。

## What Changes

- connected 分支追加一次比赛概览拉取（服务幂等、非轮询——WS 快照仍是主通道，这次拉取只是重连后的再同步）。
- matchId / WS 地址：debug 构建保留现默认（开发便利）；release 构建缺失时进入明确错误空态（不静默连错地方）。
- shouldAutoEnterMatch 死标志内联删除。

## User Stories

1. As a 用户, I want 断线重连后看到的是最新比分, so that 我不会拿旧比分当真。
2. As a 发布者, I want release 包不会静默连向模拟器地址, so that 构建配置错误在开发期就暴露。

## Non-goals

- 不做概览轮询（WS 快照是主通道）。
- 不改 debug 构建的默认便利性。