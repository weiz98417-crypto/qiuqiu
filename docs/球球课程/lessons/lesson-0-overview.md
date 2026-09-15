---
lesson: 0
title: 系统全景与佩德里进球 trace 预告
trace_position: 全程鸟瞰——事件注入到 eval 收口的 10 跳，后续 6 讲每讲下钻一站；上游是 operator/数据源，下游是客户端与评测
depends_on: [20-product-overview]
---

# 第 0 讲 · 系统全景与佩德里进球 trace 预告

## 这一站在 trace 上

演示种子造出这场比赛：西班牙对德国，第 24:10 分钟佩德里禁区内抢点破门，法比安助攻、亚马尔策动，比分 1-0（scripts/demo-seed.mjs:84-105）。本讲不进入任何一层，只把这次进球从注入到收口的每一跳钉在真实文件上。整个后端是一个单体：backend/cmd/server 一个进程跑完接收、记账、决策、调度、说话。学完 7 讲你会沿着同一颗进球走完这条链。

```
operator / 数据源                                    Flutter 客户端
  | POST /api/matches/test/events                        ^ WS /ws/match/{id}
  | main.go:1978                                         | presentation+reply+TTS
  v                                                      |
matchstate.Store.Create  store.go:629                    |
  ValidateAppend        fact_ledger.go:254               |
  RecordedSequence      store.go:685                     |
  快照 = Replay 投影     fact_ledger.go:80                |
  | Subscribe 推送      main.go:436                      |
  v                                                      |
proactiveGate.Allow     proactive_gate.go:15             |
  标签覆盖 quiet/manual main.go:572-576                  |
  v                                                      |
companion.Agent.HandleMatchEvent  main.go:577            |
  v                                                      |
relationship.Director.Apply  director.go:23              |
  applyPolicy policy.go:48 / updateAffect affect.go:9    |
  => Decision(Actions/Speech/Presentation)               |
  v                                                      |
conversation.Scheduler.SubmitProactive  scheduler.go:90  |
  用户可抢断 scheduler.go:198                             |
  v                                                      |
LLMReplyRealizer.Realize  realize.go:24（或确定性底稿）    |
  v                                                      |
ResponseDeliveryService.Deliver  response_delivery.go:124
  --------------------------------------------------------+
  v
evals：evals/cases/baseline/goal-assist-follow-up.json（"pedri-goal"）
```

## 证据锚点

- scripts/demo-seed.mjs:92 —— 演示事件 `playerName: "佩德里"`，:94-96 scorer/assist/pre_assist 三人
- backend/cmd/server/main.go:1978 —— operator POST events 入口（单体进程，main.go 共 2796 行）
- backend/internal/matchstate/store.go:629 —— `Store.Create` 追加事件并投影快照
- backend/internal/matchstate/fact_ledger.go:80 —— `Replay(input, uptoSequence)` 确定性重放
- backend/cmd/server/main.go:436 —— `matchStore.Subscribe` 把事件推给每条 WS 连接
- backend/internal/conversation/proactive_gate.go:15 —— 主动发言资格门
- backend/cmd/server/main.go:577 —— `companionAgent.HandleMatchEvent` 生成比赛回合
- backend/internal/relationship/director.go:23 —— `Director.Apply` 产出 Decision
- backend/internal/conversation/scheduler.go:90 —— `SubmitProactive` 排队/去重/插队
- backend/internal/companion/realize.go:24 —— LLM 只做语言实现
- backend/internal/conversation/response_delivery.go:124 —— 统一投递（文字/TTS/表演）
- client/lib/widgets/live2d_view.dart:10 —— `Live2dView`，客户端 Live2D 呈现
- evals/cases/baseline/goal-assist-follow-up.json:19 —— eval 用例键 `"pedri-goal"`

仓库形态核对：backend/internal 下 20 个领域包（asr/auth/companion/conversation/matchstate/relationship/...）；backend/migrations 40 个 SQL 迁移（001–038，含同名双编号）；backend/cmd 下只有 server/verify-datasource/eval-audit/evals/latency-test 五个入口；architecture/archify/ 下 15 个 .json 图源；评测是 Node 装置（package.json `eval:offline` → scripts/evals/run.mjs）。

## 代码走读

backend/cmd/server/main.go:568-586 —— 资格门与 Agent 调用同点汇合：

```go
						policy := matchStore.Config(matchIDStr).Automation
						critical := proactiveUrgency(ev.EventType) == conversation.UrgencyCritical
						now := time.Now()
						allowed := proactiveGate.Allow(policy, ev.EventType, critical, now)
						if hasEventTag(ev, "proactive=quiet") {
							allowed = false
						} else if hasEventTag(ev, "proactive=manual") {
							allowed = proactiveGate.AllowManual(now)
						}
						response, err := companionAgent.HandleMatchEvent(connectionCtx, companion.MatchEventRequest{
							UserID:                userID,
							Event:                 ev,
							Snapshot:              snapshot,
							OutputAllowed:         allowed,
							Critical:              critical,
							UserSpeaking:          userSpeaking.Load() || userTurnActive.Load(),
							NormalCooldownSeconds: policy.CooldownSeconds,
							Now:                   now,
						})
```

注意 `OutputAllowed: allowed`——能不能说话是主流程算好传进去的，人格层只负责"想不想"，不越权改"允不允许"。

backend/internal/relationship/director.go:85-107 —— 一个 Decision 装下所有下游要用的东西：

```go
		decision := Decision{
			ID:           decisionID,
			SignalID:     signal.ID,
			FactRevision: signal.FactRevision,
			StateVersion: state.Match.Version,
			Actions:      actions,
			Relationship: RelationshipView{
				Stage:               state.Relationship.Stage,
				RepairActive:        state.Relationship.Repair.Active,
				RepairCategory:      state.Relationship.Repair.Category,
				BoundaryCount:       len(state.Relationship.Boundaries),
				AllowedBanterScopes: allowedBanterScopes(state.Relationship.Banter),
				GreetingDelivered:   state.Relationship.GreetingDeliveredAt != nil,
				InitiativeMode:      state.Relationship.Preferences.InitiativeMode,
				AnalysisAppetite:    state.Relationship.Preferences.AnalysisAppetite,
			},
			Presentation:  presentationFor(state.Match.Affect, signal, actions),
			Speech:        speechFor(signal, actions, state.Relationship, state.Match),
			Memories:      selectedMemories,
			PlaybackState: state.Match.PlaybackState,
			ReasonCodes:   append([]string{"relationship_decision"}, reasons...),
			CreatedAt:     updatedAt,
		}
```

文本、语音、Live2D 表演消费同一份 Decision 的不同字段，`ReasonCodes` 让每个决定可审计。这是"决策/表达分离"的落点：LLM 拿到的只是这份结构（realize.go:35-42 的系统提示词明写"只负责把已经决定好的沟通动作说成自然中文"）。

## 评测联动

- `npm run eval:offline`（package.json）= `node scripts/evals/run.mjs --tier offline`，先 `go test ./...`（backend 模块）再 `go run ./cmd/evals -suite all`。任何一讲改坏，这条命令最先红。
- 单测级收口：`TestEvalBaselineFullMatchFlow`（backend/internal/matchstate/store_test.go:157）一次跑完配置→开赛→事件→快照。
- eval 用例 `baseline.goal-assist-follow-up`（evals/cases/baseline/goal-assist-follow-up.json）就是佩德里进球：主动线必须提到佩德里+法比安，追问"刚才谁助攻？"必须答法比安+亚马尔（:49-58）。

## 动手作业

1. `cd backend && go test ./internal/matchstate/ -run TestEvalBaselineFullMatchFlow -v` —— 一条测试跑完整场流程，观察 PASS。
2. 起本地服务后 `node scripts/demo-seed.mjs`（可用环境变量 `QIUQIU_BASE_URL`/`QIUQIU_MATCH_ID` 改目标）—— 输出 JSON 里 `score: {"home":1,"away":0}`、`eventId`，即这颗进球的真实 id（demo-seed.mjs:402-413）。
3. 跑一遍 `npm run eval:offline`，记下用时——后面每讲的作业都用它或它的子集验收。

## 延伸

- docs/球球课程/chapters/20-product-overview.facts.json、33a-agent-runtime.facts.json、57-evals-golden-set.facts.json
- docs/adr/0001-bounded-digital-ballmate.md（产品边界）、0003-unified-watch-turn-planning.md（统一回合规划）、0005-presentation-single-contract.md（单一呈现合同）
- 非技术版同一故事的讲法：docs/直播课-15张架构图逐字稿-非技术版.md:11
