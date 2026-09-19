# Server Residual Polish: cmd/server 拆分后的残留打磨

## Why

main.go 拆分后仍有明确残留：watchconnection.go 的 handleWatchConnection 是一个 744 行零辅助函数；错误映射（ErrNotFound→404 / ErrConflict→409，含 directordraft 变体）在 match_operator_api.go 重复约 9 处；语音 wrapper 链 4 层里两层是纯转发；submittedUserSignals 仍是包级 global；conversation.WatchSession.Recoveries 静默丢弃 recovery 源错误（坏源与「无可恢复」不可区分）。

## What Changes

- handleWatchConnection 按 upgrade / identify-and-subscribe / message-pump / teardown 四相拆辅助函数（纯搬移，deps 不变）。
- 提取 writeMatchStateError(w, err)（含 directordraft 变体），收敛全部错误映射点。
- 语音 wrapper 链塌缩：删两个纯转发 shim，测试调用点直连 Options 变体。
- submittedUserSignals 构造注入（voice/deps 传参），包级 global 删除。
- Recoveries：recovery 源错误不再 continue 吞掉，升为一次投递状态记录（可观察）。

## User Stories

1. As a WS 引擎维护者, I want 744 行函数按相拆分, so that 定位一段逻辑不再靠滚动。
2. As a 比赛 API 维护者, I want 错误映射一处定义, so that 新端点不会映射错。
3. As a 调试者, I want recovery 源坏了能看见, so that 「没恢复」与「恢复器坏了」可区分。

## Non-goals

- 不改任何路由行为与请求形状（28 evals 冻结基线）。
- trace.go 22 位置参数（写一次读多次，最低优先级，不在本轮）。