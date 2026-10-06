# Tasks: 记忆打分收编

- [x] 8.1 三因子收编:adapter 腿打分升级——`recallWeight = relevance × importance × recency`(memobase.go):relevance(focus 命中 1.0/未命中 0.35/空 focus=1.0)、importance(类目查表 entryImportance,favorite_*=0.75——Memobase 合成条目不带 per-entry importance,ADR-0006 合成不重写 moment 分,故类目近似)、recency(exp(-age/τ),τ 与向量腿同源 Query.DecayDays,<=0 关)。Recall.Importance 字段归正(装类目分,不再装排序分;排序分落 Score)。**两腿同代后排序 evals**:TestRecallWeightDirectionalOrdering 三方向断言(空 focus 类目序/τ 窗内新旧序+关衰减回归现状/命中恒压未命中)。
- [x] 8.2 反思触发:Queue 新增 pendingImportance 账本(Observe 累计/ReflectNow 取出即清/未配置 adapter 不消费（账不清）；beat 跑了但失败时账已清不回账（有界：该用户沉到 fallback 轮 ≤75min，审查修正措辞）);常量 ReflectImportanceThreshold=1.5(两三条有价值 moment 即达标)。main.go idle 分支过滤:常规 idle 轮只反映「有新料」用户,每 reflectFallbackRounds=5 轮(≤75min)强制全量一轮——低活跃定时兜底保留(spec 原文);post_match 不受门(事件驱动)。**evals**:TestPendingImportanceAccruesAndClearsOnReflect(累计/阈值/per-user 隔离/未配置不消费/beat 消费清零)——「同数据下触发次数不升」由结构保证(达标用户频次=现状 idle 节奏,未达标用户更少),零新洞察白跑消除。
- [x] 8.3 写回 dry-run:PortraitWritebackReviewer 接口+Consolidate 插入点(privacy 门后/判定器前)——一次轻量 LLM 自查(与画像矛盾/噪声闲聊/越权记比赛事实=宪法红线),不过审记审计(op=NOOP reason=writeback_rejected:…)跳过;生产实现 LLMWritebackReviewer(structured seam,与判定器同形)。**审查修正**:比分形态硬门(claimScoreRedline,连字符/比字形态,三段阵型豁免;冒号与时间戳局部不可分留 LLM 提示层)——红线不依赖 reviewer 在场,确定性执行 ADR-0006。**纪律**:reviewer 是质量闸不是可用性闸——缺席=直落(现状字节级,缺席测试),审查故障也直落(不阻断画像维护)。装配:MiMo key 在场时与判定器共用 structured client;无 key 环境(CI/evals)缺席。**evals**:TestWritebackDryRunGatesAndDegrades 三分支(拒绝不落库/故障直落/缺席现状)。
- [x] 8.4 Memobase 核对:docs/research/memobase-version-audit.md——适配器端点清单 vs 0.0.36/37/40 逐版对照:**适配器未用上任何新能力**;裁决=context API 值得接但只 Portrait 腿(Recall 腿保本地三因子,排序可复现优先)、gist 搜索不接(向量腿已覆盖)、workflow 重写随镜像自动享受(升级主收益)。镜像升级+回归+token 对照=用户侧(Docker 不可用于开发机),执行清单在档。
- [x] 8.5 门禁:go 全量 33 包绿(含 memory 全量与既有 portrait_ops/evals runner 调用点适配)。

## Sequencing

波3。依赖 agent-internals(波2)的 3.4 Queue 拆分先落(RecallFusion/ReflectionEngine 模块化后本 change 改动面才干净)。Memobase 升级(8.4)独立可先行,但 token 对照在 8.1-8.3 落地后测才有意义。

> 3.4 拆分状态备注(2026-10-06):A5-2 三模块拆分已在 agent-internals 轮评估后缩编(RecallFusion 已是函数族),本 change 直接在函数族上改,改动面干净——依赖满足。
