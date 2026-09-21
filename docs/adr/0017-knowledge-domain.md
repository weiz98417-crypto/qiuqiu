# 0017 · 知识域：策展条目作为第二事实域

日期：2026-09-22（第二波，openspec/changes/knowledge-rag）

## 背景

球球的确定性事实面只覆盖本场的 Match Facts（ADR-0002 账本）：问"越位是什么""英超几个队"——路由得了、答不了。裸 LLM 生成知识答案被反编造纪律挡死，而知识广度是市面体育助手与交互数字人的默认预期（RAG 知识库是交互数字人管线标配段）。

## 决定

1. **知识域是第二事实域**：与 Match Fact 账本（ADR-0002）并列而非扩权。条目 = 知识事实单元，带 source（可溯源出处）、confidence（确信度）、effective_at（生效时间）——把「事实优先文化」平移到知识域。
2. **条目策展于 repo**（`backend/knowledge/rules/*.yaml`，git 版本化）：answer 原文即答案锚，策展时就写成球球口吻的口语；回答 = 逐字拼装，**realizer 不碰、LLM 不生成**（capability-wave-2 grilling Q13）。structured.Extract 的知识合成后置。
3. **检索双路**：topics 关键词（双向 contains）∥ embedding 余弦（本地 bge-m3，复用 semantic-memory 通道），关键词命中优先，向量补换说法（阈值 0.55）；confidence < 0.6 的条目不出答案——「不知道」好过「不太对」。
4. **意图独立**：`knowledge_question` 走意图注册表（置信门控、非闲聊回复资格），trace 记 `knowledge.answer` ToolCall（条目 id + confidence）。
5. **纪律不变式**：ForbiddenClaims 对知识域同样生效——球球不得把知识条目断言成本场比赛事实；知识答案永远不写进 Match Fact 账本。

## 被否决的替代方案

- **LLM 现场生成 + 自我核查**：核查者与生成者同源，纪律形同虚设。
- **抓取外部 wiki**：版权、时效与来源稳定性都不归我们控制。
- **球队/球员静态档案进 MVP**：无权威源前必然过期（等 data-provider-lite-bridge）。

## 后果

- KNOWLEDGE_DIR 配置留空即停用实质回答（意图如实回"还没接上"）。
- 知识条目错误 = 策展错误，走 git 修正而非运行时补丁；条目格式变更需过本 ADR。
- CONTEXT.md 新增「知识条目 Knowledge Entry」词条。
