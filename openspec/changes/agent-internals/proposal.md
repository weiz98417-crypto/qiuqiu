# agent 内功:回合经济/god file 出膛/Queue 拆分/注册表注入

## Why

四项存量重构(调研 A 组 Strong/Worth exploring),互相独立、同属 companion+memory 热区:

1. **A2 回合内浪费调用**:fact 补充语织写在 policy 决策**之前**发起(companion/agent.go:721 → memory_realization.go:83-119),policy 判 chosen_silence 时清空 reply(agent.go:734-737)——这次 LLM+Recall 白做;`recallMemoryBlock` 一回合可执行 2 次(agent.go:721 与 realize 路径 :1582-1583),每次独立 Memobase HTTP+pgvector。
2. **A4 god file 内容出膛**:agent.go 2,093 行,canned 文案(:1012-1075)、guard 违禁词表(:1760-1795)、knownPlayerNames 白名单(:1919)、insistenceAdverbs(:1558)等「数据」混在编排逻辑里;HandleBoundaryRequest 单管线 180 行(:632-811)。
3. **A5 memory.Queue 九状态一个 struct**(queue.go:99-148,1,306 行):写路/召回融合/threads/portrait overlay/claims/citations/userMatches/health tail 共享锁与时序;citations/userMatches/claims 纯内存重启即失(:133-139,reflection 审计断档);observeVector 裸 goroutine 无并发上限(:373-396)。
4. **A6 意图 5 处人肉同步**:加一个意图要动 classify 谓词、registry spec、router prompt 硬编码行、jsonschema enum 串、漂移锁测试(intent_registry.go:90-220 ↔ router/router.go:118, 166-186)——注册表持逐字镜像靠 Validate() 红灯兜底,双份中文文案永远存在漂移风险。

## What Changes

- **A2 回合经济**:fact 补充语织写移到 policy 决策之后(与 realizer 同批;沉默回合零织写);per-turn recall 缓存——一回合至多一次 Memobase+pgvector 召回,fact 织写与 realize 共享结果。先例:router 建议被 guard 采纳时跳过 realizer(agent.go:744-748)已证明省调用安全。
- **A4 出膛**:新建 companion_vocabulary.go(仿 policy_vocabulary.go 先例)收编全部内容数据(文案/违禁词/球员白名单/副词);HandleBoundaryRequest 拆命名阶段函数(classify→policy→realize→guard→落账)。**纯实现内重排,接口面不动**。
- **A5 Queue 拆分**:按已有半成品切面拆 RecallFusion / PortraitMaintainer / ReflectionEngine;citations/userMatches/claims 落 PG(重启不断档);observeVector 加并发上限与统一生命周期。ADR-0006 缝(Memories: Observe/Recall/Portrait/Threads)不变。
- **A6 注册表注入**:router 的 system prompt 中文行与 enum 串由注册表构造期注入生成——**产出字节与现状逐字节一致**(ADR-0009 字节锁不修订,消灭的是双份来源);加意图从 5 处到 2 处(谓词+注册表一条)。

## Non-goals

- agent.go 整体搬移或包结构重组(只做内容出膛+管线命名化)。
- ADR-0009 契约任何语义变化(单次 function call/置信门/router 永不写事实全不动)。
- memory 对外接口变化(拆分是缝内重排)。
- knowledge 检索(knowledge-retrieval 独立 change)、policy 层新能力(policy-bits 独立 change)。

## Success Criteria

- A2:chosen_silence 回合零织写零召回(eval 断言);非沉默回合 recall 恰一次;全量 evals 249+ 绿(措辞不变)。
- A4:agent.go 行数显著下降(目标 <1,200);词表单文件;阶段函数可单测。
- A5:三模块各自可测;citations 重启存活;goroutine 上限生效。
- A6:注入产出与现状 diff 为零(字节级测试);新增一个试验意图只需 2 处改动走通。
- evals 新增:Response Delivery(重复投递/中断/TTS fallback)+ memory 队列(三腿融合排序/时序衰减)套件(A7 分摊)。
