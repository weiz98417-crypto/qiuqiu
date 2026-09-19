# Tasks: Agent Engine Extraction

- [x] 1.1 类型区拆 types 文件（意图枚举、FactClaim、Trace、调度类型）。
- [x] 1.2 关键词分类器提取（Classify + 纯谓词），表驱动测试补齐口语变体。
- [x] 1.3 比赛事实引擎提取（快照完整性 / 事件应答 / 比分主张评估），表驱动直测。
- [x] 1.4 意图词汇表 1:1 锁测试（Intent ↔ router schema ↔ routedTurnIntent）。
- [x] 1.5 观察协调逻辑从 intent_router.go 移到观察内聚文件。
- [x] 1.6 验证：go test 全绿（等价证明）；「加一个意图」演练一次确认三处收敛。

## Sequencing

第六个执行：在 guard 统一（router-trace-durability）之后。
