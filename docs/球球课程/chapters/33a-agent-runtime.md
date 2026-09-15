---
id: 33a-agent-runtime
title: 数字球友 Agent 运行时与决策编排
source_chapter: docs/球球全套资料/4.产品设计/33A-数字球友Agent运行时与决策编排设计.md
status_summary: { implemented: 10, partial: 1, planned: 0, concept: 2 }
---

# 数字球友 Agent 运行时与决策编排

旧文档 33A 描述的是一份"产品级运行蓝图"：manifest、优先级图 P0–P5、run 状态机、预算字段、Trace 事件合同。真实代码里没有这些东西的名字——真实存在的是一个 3061 行的 `agent.go` 单体加一个 `relationship.Director` 决策器：意图确定性分类、事实意图不走 LLM、决策带版本 CAS 写入、沉默是一等输出、事实更正使旧回合失效。本章按真实结构重讲。

## 系统实际怎么工作

**一个回合 = 一种 TurnKind + 一份 Decision。** 统一入口 `Plan` 分发 5 种回合：用户回合、比赛事件、初次见面、观察消解、投递回执 [agent.go:85-93](../../../backend/internal/companion/agent.go) [agent.go:502-560](../../../backend/internal/companion/agent.go)。用户回合先经 `Classify` 做确定性意图分类：比分/事件断言、比分询问、控制命令（别说/少说/闭嘴/安静/别播报）、日程查询，甚至拒绝"把比分改成"类写入请求 [agent.go:1777-1801](../../../backend/internal/companion/agent.go)。

**事实类意图根本不进 LLM。** 比分断言、比分状态、最近事件、追问、球员问题全部 `allowRealize=false`，用模板基于事实快照作答 [agent.go:818-946](../../../backend/internal/companion/agent.go)。用户喊出的比分先过 `assessMatchClaim` 核实，核实不了标 Unverified 并转入观察流程（agent.go:832-846）——这就是旧文档"待确认只能产生等待解释"的真实形态。

**回应计划是结构化 Decision。** `Director.Apply` 跑策略（applyPolicy 得到 10 种沟通动作之一），组装 `Decision{Actions, RelationshipView, PresentationPlan, SpeechPlan, Memories, ReasonCodes}` 并以 CompareAndSwap 带版本写入 [director.go:71-117](../../../backend/internal/relationship/director.go) [types.go:246-261](../../../backend/internal/relationship/types.go)。文本、语音、舞台消费同一份 Decision 的不同字段——旧文档"同一回应计划多出口"在这一点上是成立的。

**幂等与沉默。** 同一 Signal 的决策经 `DecisionBySignal` 复用 [director.go:30-56](../../../backend/internal/relationship/director.go)，traceID 由 userId+matchId+signal 稳定派生 [agent.go:788-795](../../../backend/internal/companion/agent.go)，服务端另有信号级 deduper [main.go:134](../../../backend/cmd/server/main.go)。沉默路径：输出被禁用/冷却中/用户要安静 → ActSilence [policy.go:73-84](../../../backend/internal/relationship/policy.go) [policy.go:117-118](../../../backend/internal/relationship/policy.go)，`speechFor` 对 ActSilence 返回 nil [policy.go:354-357](../../../backend/internal/relationship/policy.go)，空回合记 chosen_silence [agent.go:651-661](../../../backend/internal/companion/agent.go)。

**控制与失效。** OutputAllowed=false 直接沉默（policy.go:73-75）；用户说话中语音 InterruptMode=after_user（policy.go:365-367）；呈现门前 Expression 为空不发表演、Reply 为空不投递 [main.go:591-602](../../../backend/cmd/server/main.go)。事实更正链：`markPriorFactTurnsStale` 把同 signal 旧回合标 Stale [agent.go:679-708](../../../backend/internal/companion/agent.go)，关键事件决策只允许一次事实刷新且刷新时 Speech/Presentation 重算 [director.go:33-46](../../../backend/internal/relationship/director.go) [agent.go:1549-1552](../../../backend/internal/companion/agent.go)。

**工具留痕与表达护栏。** 每次工具调用追加进 `Trace.ToolCalls`：conversation.read_recent（agent.go:928、2782-2791）、match.read_snapshot、match.search_events、relationship.apply 等 [agent.go:1490-1492](../../../backend/internal/companion/agent.go)。语言实现层只把已决定动作说成中文，产出过 `validateRealizedText` 禁词表（跨度 2684-2780，含恋爱化、依赖性、"导播台"等内部术语）与对话一致性校验，失败回退确定性底稿 [realize.go:24-42](../../../backend/internal/companion/realize.go) [agent.go:2572-2590](../../../backend/internal/companion/agent.go)。

## 与旧设计的差异

| 旧设计（33A 章） | 现实 | 锚点 |
| --- | --- | --- |
| agent manifest（agent_id/mission/tools/guardrails 表）+ 多组件编排 | 单体 agent.go + Director；无 manifest 结构 | [agent.go:85-93](../../../backend/internal/companion/agent.go) |
| 裁决优先级图 P0–P5，用户控制必胜 | 无显式优先级图；控制优先分散为 OutputAllowed 门、ActSilence、after_user、呈现门 | [policy.go:73-75](../../../backend/internal/relationship/policy.go)、[main.go:591-602](../../../backend/cmd/server/main.go) |
| run 状态机 received→…→presenting + 预算字段 + 终止原因枚举 | 不存在；回合是同步函数调用，唯一 Budget 是 InitiativeBudget 冷却时间戳 | [types.go:222-226](../../../backend/internal/relationship/types.go) |
| 7 类 Trace 事件、可按 run_id 回放 | 单条 Trace 记录（Intent/ToolCalls/Claim/Reason/LatencyMS），日志型而非事件流 | [agent.go:170-179](../../../backend/internal/companion/agent.go) |
| 工具合同 input_schema/risk_level/幂等键 | 工具调用只有名字+字符串参数留痕，无 schema 校验层 | [agent.go:2782-2791](../../../backend/internal/companion/agent.go) |
| 主动性=四问资格 + 许可分类 | 实现为模式（natural）+ 90 秒冷却 + 关键事件单独记账 | [policy.go:76-92](../../../backend/internal/relationship/policy.go) |
| 更正使旧计划端到端失效 | 已实现（Stale 标记 + 单次刷新），粒度是回合级而非媒体队列级 | [agent.go:679-708](../../../backend/internal/companion/agent.go) |

## 主张-锚点表

| 主张 | status | 锚点 | 证据 |
| --- | --- | --- | --- |
| Agent 单体（3061 行）与 5 种回合类型 | implemented-at | [agent.go:85-93](../../../backend/internal/companion/agent.go) | code |
| Classify 确定性意图路由（含拒绝比分写入） | implemented-at | [agent.go:1777-1801](../../../backend/internal/companion/agent.go) | code |
| 事实意图不经 LLM、模板确定性作答 | implemented-at | [agent.go:818-946](../../../backend/internal/companion/agent.go) | code |
| Decision 结构化回应计划 + CAS 版本写入 | implemented-at | [director.go:71-117](../../../backend/internal/relationship/director.go) | code |
| DecisionBySignal/稳定 traceID/信号去重幂等 | implemented-at | [director.go:30-56](../../../backend/internal/relationship/director.go) | code |
| 工具调用留痕 Trace.ToolCalls（含 read_recent） | implemented-at | [agent.go:2782-2791](../../../backend/internal/companion/agent.go) | code |
| 沉默是一等输出（ActSilence→nil Speech→chosen_silence） | implemented-at | [policy.go:354-357](../../../backend/internal/relationship/policy.go) | code |
| 事实失效与关键事件单次刷新 | implemented-at | [agent.go:679-708](../../../backend/internal/companion/agent.go) | code |
| 用户控制门（OutputAllowed/after_user/呈现门） | implemented-at | [main.go:591-602](../../../backend/cmd/server/main.go) | code |
| 表达护栏与确定性回退 | implemented-at | [agent.go:2572-2590](../../../backend/internal/companion/agent.go) | code |
| 主动性冷却（无许可分类） | partial | [policy.go:76-92](../../../backend/internal/relationship/policy.go) | code |
| run 状态机/预算/审批/版本锁定 | concept | 旧 33A 章 317-335 行 | doc |
| Trace 事件合同与回放能力 | concept | [agent.go:170-179](../../../backend/internal/companion/agent.go)（证伪锚） | code |
