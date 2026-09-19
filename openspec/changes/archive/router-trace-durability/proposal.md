# Router Trace Durability: 路由证据落库与护栏统一

## Why

ADR-0009 承诺「每次路由 ReasonCode 入 trace、运营台引用审计可查」，但实现只兑现了一半：PostgresTraceWriter 不序列化 RouterTrace、scanTrace 无法恢复——Postgres 部署（生产形态）下路由的置信度、槽位全部丢失，只剩 decision JSON 里嵌的 reason 码；意图路由器的审计链路只在内存储活着。同时两条回复路径的护栏调用已经分叉：router 路径的护栏拒绝是静默的（ReplyUsed=false 把「模型没给建议」和「被护栏拦截」混为一谈），且锚源比 realizer 路径松（漏掉 requiredAnchors）。trace.Reason 是 27 处裸字符串赋值、约 20 个字面量，词汇表只有 grep 能发现。

## What Changes

- Migration 044：`agent_traces` 增加 `router` JSONB 列；PostgresTraceWriter / scanTrace 完整读写 RouterTrace（镜像 fact_claim 的持久化模式）。
- RouterTrace 增加「拒绝原因」字段，区分空建议与被护栏拦截；被拦的路由回复获得与 realize_fallback_policy 同级的 trace 证据。
- 护栏统一：router 路径与 realizer 路径过同一个 guard 函数——同锚源（含 requiredAnchors，评审 Q2 已批准收紧）、同拒绝可见性。
- trace ReasonCode 收敛为常量词汇表（一处定义，赋值点引用常量）。
- TurnRouter seam 真实化：补上接口声明时承诺的脚本化 fake，agent 级测试从 httptest 信封耦合迁到 fake；保留一条 httptest 测 client 自身信封。

## User Stories

1. As a 运营员, I want 在 Postgres 部署的引用审计里看到路由意图、置信度与槽位, so that 「球球为什么这么回」可以完整回答（ADR-0009 的承诺兑现）。
2. As a 运营员, I want 被护栏拦截的路由回复有明确标记, so that 分得清模型没建议和护栏说不。
3. As a agent 维护者, I want reason 码从常量表取, so that 新增码不会拼错也无需 grep 全库。
4. As a 陪看回合维护者, I want 闲聊回复的锚定标准与 realizer 一致, so that 路由路径不成为证据标准的洼地。
5. As a agent 测试作者, I want 用 5 行 fake TurnRouter 编写路由行为用例, so that 加信封字段不再破坏所有脚本桩。
6. As a 回归分析者, I want 护栏收紧后的被拦率可从 trace 观察, so that 收紧是否过头由数据回答（Q2 的验证闭环）。

## Non-goals

- 不改 ADR-0009 的路由策略：单次调用、6s 超时、置信门原样。
- 不重写护栏规则本身（ForbiddenClaims、句数上限等策略不动，只统一调用路径）。
- 不做 Langfuse 式 trace UI（引用审计列展示即可）。
