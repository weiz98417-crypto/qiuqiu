# Design: Server Residual Polish

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | WS 拆分为纯搬移：闭包体内的逻辑按相提成 handleWatchConnection 内的私有闭包/函数，捕获面仍走 deps——不改任何消息处理语义。 |
| 2 | writeMatchStateError 是唯一错误映射点（含 directordraft 的 404/409 变体入参）；9 处调用点替换。 |
| 3 | 语音链：保留 handleVoiceSession 与 ...WithSignalIDOptions 两层，删中间两层；测试直连。 |
| 4 | submittedUserSignals：main() 构造后经 watchDeps / voice 调用链显式传参；包级 var 删除。 |
| 5 | Recoveries：错误记入日志 + 该轮投递状态标记 recovery_source_error（新 trace/状态值，调用方可观察）。 |

## Seam

- 纯搬移不新增 seam；writeMatchStateError 与 Recoveries 状态是两个小的新测试面。

## Testing decisions

- 等价证明：go test 全量 + 28 条 operator-control evals 全绿。
- 新增：writeMatchStateError 表驱动（NotFound/Conflict/其他）；Recoveries 错误路径一条单测。