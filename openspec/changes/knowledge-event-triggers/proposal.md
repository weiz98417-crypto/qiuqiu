# Knowledge Event Triggers: 判罚时刻的引语锚定织写

## Why

知识条目（ADR-0017 第二事实域）只有问答路（knowledge_question → `Library.Search` 单条最优 → 确定性拼装）。但判罚时刻的知识价值在「顺嘴说」——点球判罚瞬间补 VAR 规则，「等用户问就晚了」（Lorebook/WorldInfo 条件触发是被陪伴产品验证过的模式）。织写纪律经 2026-09-23 grilling 修订（用户推翻纯确定性附句）：**规则陈述必须逐字，织的只是氛围**。

## What Changes

- 知识条目 YAML 增两字段：`triggers:`（事件类型列表，仅判罚类）与 `quote:`（策展短引文，一句 ≤40 字）；策展补 4-6 条判罚类条目（VAR/进球无效越位/红牌/点球），answer/quote 仍逐字锚定 source。
- `Library.TriggerLookup(eventType)`：事件类型→条目索引（Load 时建 map）。
- 触发挂点：判罚类比赛事件（var_check/goal_cancelled/red_card/penalty 类）进入事件反应拍（ActReact）规划时，命中条目作为素材注入 realizer 语境 + 硬指令「规则陈述以引号原样携带 quote」。
- **运行时守卫**：`contains(reply, quote)` 校验——失败则降级为确定性附句（模板「补一句规则：{answer}」追加在回复尾部），LLM 织写部分保留。
- 限频：同条目每场 ≤1、知识附句每场总量 ≤2、quiet 档禁用；独立计数（与 backchannel 分开）；trace 记 `knowledge:<id>` 引用码 + Ledger 记账。
- ADR-0017 修订注：消费面从问答扩到事件附句；问答路纯拼装不变，事件路=织写语气+verbatim 引语锚+运行时守卫。不立新 ADR。

## User Stories

1. As a 用户, I want 点球判罚时球球顺嘴讲一句规则, so that 看得懂又不打断气氛。

## Non-goals

- 球员档案事件触发（进球报简历语用很怪，档案留问答）；运营台编辑（Q14 后置）；自定义触发词（用户侧 Lorebook 化远期）。

## Success Criteria

- go test + evals（E 域 6 例：触发条件、引语在场、quote 缺失降级、限频、quiet 禁用、trace 引用码）；quote 校验失败路径可注入测试。
