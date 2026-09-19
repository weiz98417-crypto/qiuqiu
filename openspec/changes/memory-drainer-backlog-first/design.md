# Design: Memory Drainer Backlog-first

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | process() 内联尝试 1 次（首试）失败即 PutBacklog；DefaultBackoff 保留给 backlog 通道的重放退避（30s→10m 封顶、10 次后 parked），不再用于 drainer 内联睡眠。 |
| 2 | ForgetPortrait：适配器错误与 List 失败如实返回错误；调用方（portrait_api）privacy 5xx 映射承接——「部分删除」必须以错误示人。 |
| 3 | userCache 上限 512（超限淘汰最旧）；citations/recentMatchEnds 上限 256。 |
| 4 | backlog 为 nil（无 Postgres dev）语义不变：审计 + 丢弃。 |

## Seam

- Queue 的公开方法即测试面：吞错修复与上限用 Fake adapter 表驱动直测；drainer 移交时机用注入 clock/fake adapter 断言「1 次失败即入 backlog」。

## Testing decisions

- 新增：单次失败即入积压；ForgetPortrait 失败上抛（两种失败源）；缓存上限淘汰。
- 回归：go test 全量 + memory 包既有测试（含 postgres 集成自跳过）。