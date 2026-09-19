# Memory Drainer Backlog-first: 队头阻塞收窄与隐私操作如实报错

## Why

memory 队列的持久积压机制（memory_backlog 表 + 重放）已经存在，但单 drainer 仍在内联重试最多 3 次（退避累计可达数秒）才移交积压——Memobase 一次抖动即让所有用户的共同瞬间写入排队。更严重的是 ForgetPortrait 两处吞错（适配器错误整体丢弃、部分删除后 List 失败 return nil）：隐私删除操作会把「删了一半」报成成功。另有 userCache 与 citations/recentMatchEnds 缓存按用户无限增长。

## What Changes

- 内联重试 3 → 1 次：失败立即 PutBacklog 移交持久积压通道（backlog 重放退避 30s→10m 既有机制不变）；单条最终写入延迟上限变大，换取不再队头阻塞。
- ForgetPortrait：queue.go 两处吞错改为如实上抛；portrait_api 的错误映射已有 5xx 语义承接。
- userCache 加容量上限（LRU 语义）；citations / recentMatchEnds 加条目上限。
- 无 Postgres 的开发场景（backlog 为 nil）行为保持：审计记录 + 丢弃（现状）。

## User Stories

1. As a 用户, I want 别人的网络抖动不影响我的瞬间被记住, so that 我的共同瞬间不因队头阻塞而丢。
2. As a 用户, I want 删除画像失败时得到明确错误, so that 我能确认隐私操作真的生效了。
3. As a 运维者, I want 缓存有界, so that 长时间运行不积累内存。

## Non-goals

- 不改 memory_backlog 表结构与重放策略（已合理）。
- 不改 Memobase adapter 协议。