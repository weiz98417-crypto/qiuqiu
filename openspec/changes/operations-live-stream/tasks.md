# Tasks: Operations Live Stream

- [x] 3.1 `observingLedger` 装饰器（非阻塞广播/饱和丢弃计数）+ 单测（含无正文断言）。
- [x] 3.2 `/ws/ops` 端点：Claims 提升 + TraceRead 守卫 + 单一全局主题 + 心跳清理复用 hub。
- [x] 3.3 直播监听页：双栏 + kind/user 过滤 + trace 关联视图（复用 TurnReplay 数据面）；组件级测试为主（console spec 的 mock server 是纯 HTTP、无 WS——直播页 e2e 走 runtime-e2e 扩一条 ops 流真后端用例，不塞 mock）。
- [x] 3.4 路由迁移：console/match 双 API 面手写 switch → ServeMux pattern；未知路径 404/405 行为比对回归。
- [x] 3.5 `withScope`/`withAudit` 装饰器收敛（~20 处 authorize + 3 处审计三段式）；错误输出保持原样。
- [x] 3.6 验证：golden + console evals + pr tier 全绿；go 全量 + `-race` 抽验（server 包含 goroutine 广播）。

## Sequencing

波 3（最后：依赖波 1 的 TurnReplay 数据面；E 的路由收敛由 3.2 新端点顺路逼出，不单开纯重构波）。

## 触发型留尾（Q3 标准格式）

- 触发：出现「auditor 不该看某比赛」或按比赛隔离运营的真实需求；动作：ops 主题按 match 粒度订阅 + 新 scope 裂变。
- 触发：ops 流稳定运行一段时间；动作：活跃会话数入观测面板（operations-metrics-stack 留尾合流）。
- 触发：错误响应格式统一提上日程；动作：在装饰器层统一 writeJSON（届时作为行为变化单独过 golden/evals）。
