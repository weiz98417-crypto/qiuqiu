# Semantic Memory: pgvector 双路召回 + router 迁 structured

## Why

召回仍是关键词 contains（换说法就漏），而主动回合/事实补充语的保守门依赖"recall 材料非空"——contains 落空 = 门恒关，能力波 #2 的实际触发率被检索质量卡死。F1 查证：Memobase adapter 不消费服务端语义检索（Recall=全量拉取+本地 contains 打分），翻开关不提升召回，**自建语义召回是唯一路径**。本地 Ollama bge-m3 已验收（1024 维，paraphrase 0.71 vs 无关 0.44）。

## What Changes

- `internal/embedding`：OpenAI 兼容 /embeddings 客户端（本地 Ollama，零外部依赖）。
- migration 046：`embedding_moments`（user_id/kind/content/importance/occurred_at/embedding vector(1024)/provenance），`CREATE EXTENSION IF NOT EXISTS vector`。
- `memory.Queue` 增加 `WithVectorRecall`：Observe 时异步嵌 Moment 落 pgvector（fail-soft，失败随重试一次后放弃）；Recall 时嵌查询（200ms 超时）走 pgvector 余弦 top-k，与 Memobase contains 路**各取一半配额合并去重**（Q2/Q11）；embedding 故障对用户不可见（该路弃权，行为=现状）。
- 顺路还债：**router 迁 structured.Extract**（留尾 3.6）——payload 家酿管道删除，schema 由 Result 类型反射生成（enum 用 jsonschema tag 锁定），单测锁 schema 形状与现枚举一致；boundary 路由 eval 全量重验。

## User Stories

1. As a 球友, I want 换个说法提过去的事球球也记得, so that 记忆不被关键词匹配卡死。
2. As a 策略守门人, I want embedding 故障时行为与现状逐字一致, so that 降级不可见。

## Non-goals

- 不动 Memobase 写路径与画像合成（contains 打分原样保留为双路之一）。
- 不做 embedding 的多语言/重排模型；单模型 bge-m3、1024 维钉死。
- relationship/memory 双词汇统一若触及 relationship 策略内核则如实收缩记录。

## Success Criteria

- 全量 go test 绿 + 100 eval 绿；向量路在 Ollama 在场时命中换说法样本（单测用本地服务，缺席时自动跳过该断言）。
- router schema 形状锁测试通过（枚举/必填/描述与迁移前一致）。
