# Intent Registry: 校验式意图注册表

## Why

新增一个意图要人肉同步 7+ 处：classify.go 词表管道、router.go 的 routableIntents 枚举与 system prompt（意图定义硬编码中文）、router tool schema、intent_router.go 的 routedTurnIntent 映射与两个意图分类函数（置信门/回复资格）、agent.go 的 400 行意图大 switch 与 isFactIntent、intent_vocabulary_test.go 词汇锁。没有任何一处「意图 = 处理函数」的中心定义，漂移风险随意图数量线性涨——而能力波后续每个 change（排程提醒意图、工具注册表）都要加意图。

## What Changes

- 新增 `internal/companion/intent_registry.go`：每个意图一处声明五件套——有序关键词管道步骤（Classify 按registry顺序执行，谓词函数体不动）、router 枚举字符串、router prompt 意图定义行（字节镜像）、意图标记（fact / 置信门 / 回复资格）、确定性 handler。
- agent.go 的意图大 switch 拆为 11 个 handler 方法，dispatch 走注册表；isFactIntent、routedTurnIntent、confidenceGatedIntent、routerReplyEligibleIntent 全部改为注册表查表。
- 校验式落法（ADR-0009 prompt 字节不动）：router prompt 与枚举保持 router 包内硬编码，注册表持镜像；`ValidateIntentRegistry()` 在启动时断言镜像一致（枚举顺序 1:1、prompt 行按序出现、管道步骤引用已知意图），漂移即 fail-fast。router 只加一个只读 `SystemPrompt()` 访问器。
- intent_vocabulary_test.go 升级为注册表不变式锁（枚举顺序、prompt 行序、管道覆盖、flag 值集、match_reaction 别名）。

## User Stories

1. As a 意图维护者, I want 一个意图在一处声明全部五件套, so that 新增/修改意图不再跨 7 文件人肉同步。
2. As a 路由行为守门人, I want router prompt 与枚举字节不动且有启动期漂移断言, so that 100 个 eval 的行为零漂移可被机器保证。

## Non-goals

- 本波不加任何新意图（排程提醒意图留给 proactive-scheduler）。
- 不改 router prompt 一个字节、不迁 router 到结构化接缝（structured-tool-seam 单独立项）。
- 不动 ClassifyScheduleIntent 槽位分类与各谓词函数体。

## Success Criteria

- `go test ./...` 全绿，100 个 eval case 全绿（baseline/boundary/regression 零漂移）。
- 删除测试：意图五件套中任何一处单独改动且未同步其余，会在启动校验或词汇锁测试即红。
