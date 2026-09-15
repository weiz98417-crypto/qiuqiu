---
id: 28-persona-constitution
title: 人格宪法与角色边界
source_chapter: docs/球球全套资料/4.产品设计/28-人格宪法与角色边界设计.md
status_summary: { implemented: 11, partial: 0, planned: 0, concept: 1 }
---

# 人格宪法与角色边界

本章管球球的「身份、底线与表达边界」。真实程度：旧文档里的宪法条款凡涉及具体行为的，大多已落地为可审计的策略表分支与提示词硬约束；但「宪法治理」那一整套（版本分级、红队、审核门）没有代码对应，只存在于旧文档。

## 系统实际怎么工作

**人格是一张策略表，不是一段人设。** 球球的行为由 `applyPolicy` 里的确定性分支决定：信号进来 → 命中关键词/线索 → 产出沟通动作 + 理由码，Director 把它变成带 reasonCodes 的 Decision，可逐条审计（implemented-at backend/internal/relationship/policy.go:48-187；backend/internal/relationship/director.go:71-107）。

**语言层的硬边界。** LLM 只做语言实现，提示词明文：球球是「一起看足球的数字球友，不是客服、解说员、治疗师或恋爱伴侣」「禁止恋爱化、排他化、依赖性表达，禁止编造人类生活经历」，且不能改变沟通动作、关系阶段、事实、边界、调侃许可（implemented-at backend/internal/companion/realize.go:35-42）。

**438 字节的底线。** 语音助手底层提示词共 4 行（实测 438 字节）：「不攻击球队/球员/裁判，不谈政治/宗教/种族，不鼓励赌博，不用脏话，不做绝对预测」，并限定每次≤2 句（implemented-at backend/prompts/v1.0/system.txt:1-4）。

**脏话是分级阀门不是开关。** 默认 ProfanityLevel=none；放行需用户未禁用且比赛唤醒度≥0.7；mild_non_directed 需关系≥familiar，strong_non_directed 需关系≥watch_buddy 且唤醒度≥0.9（implemented-at backend/internal/relationship/policy.go:412-418）。分级矩阵由测试锁定（eval：backend/internal/relationship/content_policy_test.go:8-41）。唤醒度本身由比赛事件驱动：goal +0.8、var_check 张力 +0.6，并按 90 秒半衰期衰减（implemented-at backend/internal/relationship/affect.go:13-23, 59）。

**用户一句「别说脏话」永久生效。** 「别说脏话/别爆粗/不要爆粗/别说卧槽/不喜欢你说脏话」把 ProfanityEnabled 置 false、写入显式边界记录（implemented-at backend/internal/relationship/policy.go:104-116, 526-528），此后脏话阀门整体关闭；回归测试覆盖（eval：backend/internal/relationship/content_policy_test.go:73-100）。

**边界变成禁话题。** 「工作 + 别问/不想说/不碰」写入 work_details/do_not_ask 边界，之后每次表达策略的 ForbiddenTopics 携带「工作细节」（implemented-at backend/internal/relationship/policy.go:127-138, 422-433）。

**调侃按领域逐项许可。** 「毒奶」把 prediction 领域置为 allowed，「别拿……开我玩笑」置为 denied，许可记录 Status/EvidenceCount/来源信号（implemented-at backend/internal/relationship/policy.go:256-291）。领域枚举为 match_judgment/favorite_team/favorite_player/prediction/watching_habit/real_life（implemented-at backend/internal/relationship/policy.go:456-463）。调侃动作还需关系≥familiar 才放行（implemented-at backend/internal/relationship/policy.go:173-178）。

**不附和侮辱与注入。** 侮辱词表（废物/垃圾/蠢货/裁判瞎/人没了）触发 disagree+opinion（implemented-at backend/internal/relationship/policy.go:164-166, 552-559；场景测试 17，eval：backend/internal/relationship/scenario_conformance_test.go:155-160）。用户试图「把比分改成 / 记录进球」被意图分类器直接归为 IntentUnknown，走「这句我没接明白」兜底（implemented-at backend/internal/companion/agent.go:1783-1785, 1042-1043）。

**事实禁区随每次表达下发。** ForbiddenClaims 固定含 new_score/new_player/new_event/new_penalty_conclusion，连同 RequiredAnchors 一起传给语言实现层（implemented-at backend/internal/relationship/policy.go:381-385；backend/internal/companion/realize.go:53-56）。用户话含事实语言但检索不到锚点时，直接禁用 LLM 改写、强制确定性底稿（`fact_language_policy`，implemented-at backend/internal/companion/agent.go:1045-1050, 816-823）。

## 与旧设计的差异

- 旧文档把宪法写成五层行为优先级 + 冲突决策表（旧稿 §3）。现实没有显式优先级数据结构：优先级以分支顺序隐式存在——脏话边界、安静线索、修复触发的判断顺序先于普通闲聊（implemented-at backend/internal/relationship/policy.go:98-186）。
- 旧文档的「禁止句式库」（12 条排他/恋爱化句子，旧稿 §5.2）没有词表实现；对应物是 realize.go 提示词里的类别禁令（恋爱化、排他化、依赖性表达、编造人生经历，implemented-at backend/internal/companion/realize.go:35-42），按类别而非逐句。
- 旧文档要求「身份陈述在首次体验和帮助页以用户可理解方式出现」（旧稿 §2.1）。现实身份只出现在语言层提示词与底层 system.txt（implemented-at backend/internal/companion/realize.go:36；backend/prompts/v1.0/system.txt:1），没有独立的用户可见身份页。
- 旧文档的「变更分级 V0-V4、变更记录最小字段、红队场景库、八项审核门」（旧稿 §9、§10）无任何代码或流程对应（concept）。最接近的现实机制是 20 个场景的一致性测试与 evals/cases/boundary 边界用例（eval：backend/internal/relationship/scenario_conformance_test.go:9-212；evals/cases/boundary/）。
- 旧文档「调侃六项准入条件」（旧稿 §7.3）。现实收敛为三条硬条件：领域许可 allowed、关系≥familiar、修复期间清零（implemented-at backend/internal/relationship/policy.go:173-178, 398-404），其余条件（笑点不针对身份、可忽略等）无实现。

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| 人格=确定性策略表，可审计 | implemented-at | policy.go:48-187；director.go:71-107 | code |
| 语言层硬边界（非治疗师/恋爱伴侣，禁编造人生） | implemented-at | realize.go:35-42 | code |
| 438 字节底线条款 | implemented-at | backend/prompts/v1.0/system.txt:1-4 | code |
| 脏话按唤醒度×阶段分级 | implemented-at | policy.go:412-418；content_policy_test.go:8-41 | eval |
| 用户禁脏话永久生效 | implemented-at | policy.go:104-116, 526-528；content_policy_test.go:73-100 | eval |
| 工作细节边界→ForbiddenTopics | implemented-at | policy.go:127-138, 422-433 | code |
| 调侃按领域许可+阶段门槛 | implemented-at | policy.go:256-291, 456-463, 173-178 | code |
| 侮辱不附和 | implemented-at | policy.go:164-166, 552-559；scenario_conformance_test.go:155-160 | eval |
| 比赛事实改写指令拒绝 | implemented-at | agent.go:1783-1785, 1042-1043 | code |
| ForbiddenClaims+RequiredAnchors 注入 | implemented-at | policy.go:381-385；realize.go:53-56 | code |
| 无锚事实语言强制确定性底稿 | implemented-at | agent.go:1045-1050, 816-823 | code |
| 宪法治理（优先级/红线库/V0-V4/红队/审核门） | concept | 旧稿 §3/§5.2/§9/§10；scenario_conformance_test.go:9-212 | doc |
