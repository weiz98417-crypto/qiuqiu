# Tasks: Intent Registry

- [x] 1.1 intent_registry.go：分类管道 + IntentSpec 表 + 查表/校验 + intentHandling/userTurn 类型。
- [x] 1.2 classify.go：Classify() 改走注册表管道；内联词表判定补成命名谓词（行为等价）。
- [x] 1.3 agent.go：大 switch 拆 11 个 handler 方法，dispatch 走注册表；isFactIntent 改查表。
- [x] 1.4 intent_router.go：routedTurnIntent / confidenceGatedIntent / routerReplyEligibleIntent 改查表；router.go 加 SystemPrompt() 访问器。
- [x] 1.5 main.go 启动校验接线 + intent_vocabulary_test.go 升级为注册表不变式锁。
- [x] 1.6 验证：go test ./... 全绿 + 100 eval case 全绿（零漂移）。

## Sequencing

能力波第 1 个：纯机械重构、零行为变化，为后续三个 change 降摩擦（新意图只改一处）。2/3/4 依赖本 change 的注册表。
