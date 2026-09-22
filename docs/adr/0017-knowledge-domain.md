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
- 2026-09 补充（knowledge-players）：球队/球员档案条目的快变字段（现属俱乐部、队长等）自带 `effective_at` 保质期标签，并建立**转会窗复查制度**——每年 7 月、1 月两个转会窗关闭后对 players 目录集中复查一轮；data-provider-lite-bridge 权威源落地后由源数据替换策展。原料经 AnySearch 检索 + 整页抽取、人工策展后落 repo（检索是策展工具，不是运行时依赖——运行时知识面仍只有 repo 内条目）。
- CONTEXT.md 新增「知识条目 Knowledge Entry」词条。
- 2026-09-23 修订（knowledge-event-triggers）：消费面从问答扩到**判罚时刻的事件附句**。两条纪律并存——问答路=纯确定性拼装（不变，规则文本必须逐字来自条目）；事件附句路=realizer 织写语气，但规则陈述必须原样携带策展引语 `quote`（运行时 contains 守卫，失败降级确定性附句 verbatim answer）。附句搭事件反应拍便车，不立独立话轮、不走 ADR-0015 引用码门；限频=同条目每场 1 次、总量每场 2 次、quiet 禁用。触发仅限判罚类事件（var_check/var_result/goal_cancelled/red_card/penalty/penalty_awarded）；球员档案不做事件触发（进球报简历语用不成立）。
