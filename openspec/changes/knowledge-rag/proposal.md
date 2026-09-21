# Knowledge RAG: 策展知识库与 knowledge_question 意图

## Why

出了 Match Fact 账本球球就"失明"：不知道越位规则、联赛赛制。裸 LLM 生成被反编造纪律挡死——缺的是把「有据可答」延伸到知识域的通道。这是十项对标里唯一的 🔴大×低风险差距（structured 缝已就位）。

## What Changes

- repo 策展知识库 `backend/knowledge/rules/*.yaml`：MVP 只做**规则解释 + 联赛赛制**（Q4）；条目字段 = id/主题关键词/answer 原文（口语化中文即锚点）/source/confidence/effective_at。
- 新意图 `knowledge_question`（注册表一处声明，置信门控）：问"越位是什么/英超几个队"类。
- 检索 = 关键词 + 向量双匹配（复用候选 1 的 embedding 通道）；命中 → **确定性拼装回答**（条目 answer 即最终答案，realizer 不碰，Q13）；多条命中取最高分，无命中如实说不知道。
- 新 ADR-0017：知识域 = 第二事实域（来源、确信度、可更正；绝不裸生成）。

## User Stories

1. As a 新球迷, I want 问"越位是什么"得到靠谱解释, so that 球球在比赛之外也有话可说。
2. As a 信任守门人, I want 知识答案必须来自策展条目, so that 反编造纪律延伸而非放宽。

## Non-goals

- 球队/球员静态档案（等 data-provider-lite-bridge 提供权威源）。
- 运营台知识编辑页（v1 走 repo 文件 + 发版，Q14）。
- LLM 现场合成知识答案（违背 Q13 落定）。

## Success Criteria

- 全量 go test + eval 绿；knowledge 检索单测（关键词/向量双路、无命中如实回话）。
