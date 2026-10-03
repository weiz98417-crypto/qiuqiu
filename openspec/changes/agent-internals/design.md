# Design: agent 内功

## D1 · A2 的时序权衡:省的是净延迟

重排直觉风险是「决策后织写会不会更晚出声」——不会:沉默回合原本的织写在决策前白跑(纯浪费);非沉默回合 fact 织写与 realize 同批并发发起,关键路径取 max 而非 sum,且 recall 合并后少一次 Memobase HTTP(预算 ~300ms)+一次 pgvector。净效果:沉默回合省 1 LLM+1 recall,非沉默回合省 1 recall。eval 断言锁「措辞不变」——重排不改任何输出内容,只改调用时序与次数。

## D2 · A5 拆分保接口

Queue 的公开方法签名全部不动(调用方零迁移),内部委托三模块:RecallFusion(三腿+衰减,读路径)、PortraitMaintainer(overlay/tombstone/claims——claims 归它,与 portrait 权威层同生命周期)、ReflectionEngine(flush/归因/写回/引用)。citations/userMatches 落 PG 新表(reflection 引用序列审计的重启存活面),migrations 既有惯例。锁拆分:三模块各自 mutex,Queue 只做编排不再共享一把大锁——review 时重点看锁序。

## D3 · A6 字节锁的生成式单源

注入方案:registry 构造期把每条 spec 的中文描述行渲染进 router 的 system prompt 模板、把 intent 名渲染进 enum 串。验收门是**字节级等价测试**:注入产物 vs 现硬编码字符串逐字节 diff 为零——这既是 ADR-0009「字节不动」的满足(产出不变,只是来源单一),也是回归网。若渲染中发现现状 prompt 与 registry 描述已有漂移(Validate 竟绿的边角),以现状字节为准修 registry,先等价后收编。
