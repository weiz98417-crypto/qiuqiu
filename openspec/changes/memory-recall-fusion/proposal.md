# Memory Recall Fusion: 召回第三腿 + 时序衰减

## Why

`Queue.Recall` 双路合并（semantic-memory 波）有两个盲区：①向量路纯余弦排序，`embedding_moments.occurred_at` 有字段有索引却不参与——「上周那场」全凭余弦运气（LongMemEval-V2 的时序推理维度）；②稀有实体（裁判名、错别字球员名）余弦不可靠，而 adapter 的 contains 路只管 Memobase 聊天记忆、管不到这张 Observe 瞬间表。不需要 BM25/Postgres FTS（中文分词深坑）——表里已有结构化字段没被用。

## What Changes

- `VectorMomentStore` 增 `SearchByContent(ctx, userID, pattern, limit)`（`content ILIKE '%pattern%'`，pattern 来自 `query.Focus`——本就是实体词，无新抽取）。
- `Recall` 三路合并：adapter 路配额不变（≥limit/2 保底），向量路+精确腿共享其余量、按 content 去重；任一路弃权语义照旧（弃权=现状，配额不得回退给故障路）。
- 向量路召回在合并前按 `cosine × exp(-age/τ)` 重排（τ 初值 14 天，恒定衰减——不做意图层时间窗，等真实查询日志证明需要再上）。

## User Stories

1. As a 用户, I want 问「上周那场逆转」时球球真能想起, so that 被记住的感觉是真的。

## Non-goals

- BM25/FTS/zhparser；意图层时间窗解析；Memobase 画像写入策略（ADD-only 评估另行）；D 之后的新召回通道。

## Success Criteria

- go test：实体精确命中（向量路漏召回时第三腿兜住）、recency 重排顺序断言、三路去重与弃权语义；vector_dual_test 扩展。
