# Tasks: agent 内功

- [ ] 3.1 A2 回合经济:fact 织写移决策后 + per-turn recall 缓存;evals:沉默回合零织写零召回/非沉默恰一次/措辞不变全量对照。
- [ ] 3.2 A4 词表出膛:companion_vocabulary.go 收编文案/违禁词/球员白名单/副词;agent.go 目标 <1,200 行。
- [ ] 3.3 A4 管线命名化:HandleBoundaryRequest 拆 classify→policy→realize→guard→落账 阶段函数,可单测。
- [ ] 3.4 A5 Queue 拆分:RecallFusion/PortraitMaintainer/ReflectionEngine(公开签名不动);锁拆分审查。
- [ ] 3.5 A5 持久化:citations/userMatches/claims 落 PG(migration);重启存活测试。
- [ ] 3.5b A5 goroutine 治理:observeVector 并发上限+统一取消。
- [ ] 3.6 A6 注册表注入:prompt 行/enum 生成式单源 + 字节级等价测试。
- [ ] 3.7 A7 evals(本 change 分摊):Response Delivery 套件(重复投递/中断/TTS fallback)+ memory 队列套件(三腿融合排序/衰减)。
- [ ] 3.8 门禁:go 全量 + evals 全量 + pr tier。

## Sequencing

波2 首个。3.1 独立可先行;3.2/3.3 同文件顺序做;3.4-3.5b 一族;3.6 独立。与 policy-bits 的交叉:3.3 管线命名化先落,policy-bits 的赛点档接在命名阶段上更干净(soft 依赖,不阻塞)。与 memory-surfacing(波1A)的交叉:A1 账本统一动 thread_observers——本 change 不碰 threads 域,冲突面为零,但 3.4 拆 ReflectionEngine 时以统一后的 Thread 为准(若波1A 未完,以现 Thread 为准,接口不变)。
