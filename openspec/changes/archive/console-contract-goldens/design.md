# Design: Console Contract Goldens

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | golden 以 Go handler 为 oracle：consoleHarness（in-memory stores + httptest）构造固定输入，序列化响应与 testdata golden 文件逐字节对比（归一化后）。 |
| 2 | 非确定字段（时间戳、随机 id）归一化规则集中在测试辅助里，golden 文件保持可读 JSON。 |
| 3 | 快照更新流程显式化：regenerate 标志/命令重写 golden，review 时 diff 即形状变更清单。 |
| 4 | CI 接入：evals.yml 在 go test 之后增加 console 步骤（setup-node 已有 24.x，npm ci + build）；pr 档即拦。 |
| 5 | 既有 TestConsoleOverviewShape（内联精确 JSON 断言）是雏形——迁移为 golden 文件对比并扩展到全部 console 端点。 |

## Seam

- seam 位置：Go handler 的 HTTP 响应（console 将来消费的同一形状）。golden 锁的是 wire 形状而非 Go 结构体——跨语言锁定必须在这一层。

## Testing decisions

- 好的 golden 测试锁外部形状（JSON wire format），不锁 Go 内部结构体字段顺序之外的东西；归一化只处理真正非确定的字段。
- 先例：console_api_test.go 的精确 JSON 断言、check-director-event-model.mjs 的 byte-shape 对比。
- 覆盖清单：/api/matches/:id/{events,clock,config,state} + /api/console/{overview,threads,matches(用户聚合),operators,delivery-interruptions} + trace 行（含 router 列）。

## Further notes

- 退役 operator.html 时：parity 测试锚点从 operator-live-state.js 迁到 golden（届时任务），本变更先把 golden 建好，退役随时可行。
