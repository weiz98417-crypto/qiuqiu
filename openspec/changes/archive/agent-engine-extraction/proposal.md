# Agent Engine Extraction: 从 agent.go 释放两个纯引擎

## Why

agent.go 3,254 行是 companion 包的 monolith：~800 行最该单测的纯逻辑埋在里面——比赛事实引擎（快照完整性、事件应答、比分主张评估，约 400 行）与关键词快速通道分类器（~15 个纯谓词、77 处 containsAny 调用）。意图词汇表散在 4 处（Intent 枚举、Classify、router tool schema 枚举、routedTurnIntent 映射开关）靠人肉保持 1:1；「新增一个意图」要动 4-5 个文件约 7 处。test surface 被迫是整个 Agent 的注入面。另外 intent_router.go 里 ~80 行 C2 观察协调逻辑（claim 持久化匹配）放错了家。

## What Changes

- 比赛事实引擎与关键词分类器各自提取为独立编译单元（先同包拆文件；若与 companion 类型无导入环则升独立包——不为包而包）。
- 类型区（意图枚举、Fact Claim、Trace 等约 330 行）拆 types 文件。
- 意图词汇表 1:1 锁：测试断言 Intent 枚举全集 == router tool schema 枚举 == routedTurnIntent 覆盖集，漂移即红。
- intent_router.go 中的观察协调逻辑移到观察内聚的家。
- 目标：新增意图的编辑点从 ~7 处降到 ~3 处（枚举、分发、router schema/prompt）；HandleBoundaryRequest 收缩为可读的编排。

## User Stories

1. As a agent 维护者, I want 新增一个意图只改三处（枚举、分发、router schema）, so that 意图扩展从半个下午变成半小时。
2. As a agent 维护者, I want router 枚举与 Intent 枚举漂移时 CI 变红, so that 1:1 不再靠人肉纪律。
3. As a 事实引擎维护者, I want 快照断言与比分主张评估可表驱动直测, so that 不需要装配整个 Agent 才能测一条主张。
4. As a 关键词表维护者, I want 谓词函数独立可测, so that 加一个口语变体立即验证。
5. As a agent.go 读者, I want 文件按编排顺序组织（类型→规划→回合管线→引擎→护栏）, so that 定位一段逻辑不再靠搜索。
6. As a 观察机制维护者, I want claim 持久化匹配住在观察逻辑旁边, so that 改匹配窗口不用进路由文件。
7. As a 陪看回合回归作者, I want 提取是纯搬移, so that conversation_regression_test 全绿就是等价证明。

## Non-goals

- 不改任何回合行为、护栏判定或路由策略（等价重构）。
- 不拆 HandleBoundaryRequest 的管线结构本身（那是编排，留在 Agent）。
- 不强制独立包（导入环不干净就同包文件）。
