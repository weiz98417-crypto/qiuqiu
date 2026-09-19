# Design: Agent Engine Extraction

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | 提取顺序：类型区 → 关键词分类器 → 事实引擎 → 观察协调搬家；每步独立可编译、全测试绿。 |
| 2 | 同包文件优先（companion 包内 classify.go / matchclaim.go / types.go）；仅当目标包不回导 companion 类型时升包。决策标准是导入环，不是审美。 |
| 3 | 意图词汇表锁：测试通过注册函数/反射断言三处全集一致（Intent 常量、router routeTurnParameters 的 enum 列表、routedTurnIntent 的映射覆盖）。 |
| 4 | 纯搬移：函数体逐字搬，不改签名除非去 agent 字段依赖（引擎函数本来就纯——它们只读传入的快照/事件）。 |
| 5 | intent_router.go 的 claimPersistedHoldReply / activeMatchingObservation / claimMatchesObservation 移到观察协调文件（thread_observers / memory_observers 旁），router 文件只剩路由。 |

## Seam

- 引擎函数的 interface 即测试面：快照/事件/主张进，判定出。无需 fake、无需脚手架。
- 词汇表锁是一个新测试 seam（三处全集的一致性），位置在 companion 测试内。

## Testing decisions

- 表驱动直测：比分主张（口语变体已有先例 TestIsEventClaimColloquialVariants）、快照完整性、事件应答。
- 等价证明：conversation_regression_test、intent_router_test、planner_test 全绿；不加新行为断言（行为未变）。
- 词汇表锁测试是唯一新增测试（它锁的是「未来变更的纪律」而非现有行为）。

## Further notes

- 执行前提：router-trace-durability 已合并（guard 统一后再搬，避免搬两遍）。
- agent.go 目标形态：类型 → Agent 构造 → Plan → HandleBoundaryRequest（编排）→ 主动路径 → 罐头库 → realize/护栏入口 → 工具函数；引擎与分类器在外围文件。
