# Design: Intent Registry

## 形状

`IntentRegistry`（companion 包内单例）两段式：

1. **分类管道** `classifyPipeline`：有序 `classifierStep{Intent, Name, Match}`，逐字对应 Classify() 现有判定顺序（含两处强制 Unknown 守卫与各意图交错的优先级——顺序本身是行为，必须原样保留）。`Classify()` 改为遍历管道；谓词函数体留在 classify.go 不动。
2. **意图规格** `intentSpecs`：每意图一条 `IntentSpec{Intent, RouterIntent, RouterPromptLine, IsFact, ReplyEligible, ConfidenceGated, Handle}`，routable 条目的声明顺序即 router 枚举顺序。match_reaction（主动回合专属）以别名表表达 `match_reaction → emotion_reaction`，不占规格。

handler 统一为 `func(*Agent, *userTurn) (intentHandling, error)`：`userTurn{ctx, req, requestTraceID, trace}` 承载原 switch 闭包变量；`intentHandling{reply, requiredAnchors, scheduleLookup, allowRealize, deterministicReason, claimPersisted}` 承载原共享局部变量（默认 `allowRealize=true, deterministicReason="policy"`）。case 内 `break` 一律改早返回——含 MatchStatus 的完整性守卫分支与 Schedule 的五个早退。

## 校验式边界（Q4 落定）

- router 包：`routeSystemPrompt`、`routableIntents`、schema **一个字节不动**；只加 `SystemPrompt() string` 只读访问器。
- 注册表持 prompt 行镜像；`ValidateIntentRegistry()` 断言：枚举列表与 router 完全一致（含顺序）、每条 prompt 行在 SystemPrompt 中出现且索引严格递增、管道步骤引用的意图都有规格、每个用户回合规格都有 handler、RouterIntent 无重复且不含 match_reaction。
- 校验两处生效：`cmd/server` 启动时 fail-fast；`intent_vocabulary_test.go` 作为恒跑的 CI 锁。

## 删除测试

- routedTurnIntent / confidenceGatedIntent / routerReplyEligibleIntent / isFactIntent 保留为查表薄封装（调用方与测试不破），函数体不再持有映射数据。
- 意图大 switch 整块删除，复杂度收进注册表与 handler 方法；不新增间接层。
