# Design: Console Write Transport

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | 传输策略内聚在 `api()`：写方法自动携带幂等键；inflight Map 以 `method:path:body` 为键去重并发同请求；5xx 且首次尝试时重试一次；409 抛冲突专用错误（消息取 JSON 响应体）；读取 `Idempotency-Replayed` 响应头并暴露给调用方。 |
| 2 | 语义以旧 operator.html 的已验证行为为基准逐条对齐，不发明新策略。 |
| 3 | `GET /api/matches/:id/events` 双封装合一，类型取 director 的 `DirectorEventRow[]`（信息超集）。 |
| 4 | console 不新增测试框架依赖：沿用仓库已验证的无框架模式（esbuild bundle + node:test，先例 `scripts/check-director-event-model.mjs` 与根 `tests/unit/*.mjs`）。 |

## Seam

- seam 位置：`api()` transport 函数。测试通过注入的 fetch stub（或本地 stub server）驱动，只断言外部可见行为：同请求并发只发一次、5xx 后重试一次、409 错误消息内容、replay 标记可见。不测内部 Map 结构。

## Testing decisions

- 好的测试只测 transport interface 的外部行为，不碰实现细节。
- 新增 transport 测试（node:test）：双击去重、5xx 单次重试、409 冲突文案、replay 头识别、GET 不带幂等键。
- 回归门：`tsc && vite build` 绿；Playwright console-director evals 绿。
- 先例：esbuild 无框架 harness（event-model byte-shape 对比）、node:test 单元目录。

## Further notes

- 旧页的对应实现在迁移完成前保持不动（它是行为基准与 28 条 evals 的宿主）。
