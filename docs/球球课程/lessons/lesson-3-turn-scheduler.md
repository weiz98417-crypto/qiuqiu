---
lesson: 3
title: 调度层：什么时候说、被抢断怎么办
trace_position: trace 第 4 站。上游：人格层 Decision（ActReact/ActSilence，第 2 讲）；下游：赢了的回合交给语言实现层与客户端呈现（第 4-5 讲）
depends_on: [31-initiative-silence, 33a-agent-runtime]
---

# 第 3 讲 · 调度层：什么时候说、被抢断怎么办

## 这一站在 trace 上

佩德里进球想播报，先过资格门（自动化 active + 事件白名单 + quiet/manual 标签），再过自然冷却（非关键事件 90 秒）；通过后 `SubmitProactive` 进调度器排队、按 key+TTL 去重、critical 插队。用户一开口，正在播的进球播报被取消、用户回合插到最前——"被抢断"是调度器的一等公民，沉默同样是一等输出。

```
Decision(ActReact) ─> main.go:603 proactiveSchedule(urgency, ttl)
                        v
                    SubmitProactive(key, urgency, ttl, job)  scheduler.go:90
                        ├─ dedupe: key+TTL       :186-197
                        ├─ 用户抢断: cancel+插队  :198-206
                        ├─ critical 插队         :207-218
                        └─ 播放状态机             :221-262
```

## 证据锚点

- backend/internal/conversation/proactive_gate.go:15-21 —— 资格门只有两个条件，manual 通道恒真
- backend/internal/relationship/policy.go:79-92 —— 非关键事件 90 秒自然冷却 → ActSilence("natural_initiative_cooldown")
- backend/internal/relationship/types.go:222-226 —— `InitiativeBudget{Mode, LastNormalAt, LastCriticalAt}`：冷却时间戳，不是配额计数器
- backend/internal/conversation/scheduler.go:90-92 —— `SubmitProactive`：expires = now + ttl
- backend/internal/conversation/scheduler.go:139-147 —— 出队时静默丢弃已过期回合
- backend/internal/conversation/scheduler.go:186-197 —— 同 key 未过期的新回合直接丢弃
- backend/internal/conversation/scheduler.go:198-206 —— 用户回合取消活动回合并插到队首
- backend/internal/conversation/scheduler.go:207-218 —— critical 在普通主动回合前插入
- backend/internal/conversation/scheduler.go:70-72 —— PlaybackTimeout 默认 90 秒
- backend/cmd/server/main.go:2253-2260 —— goal/red_card/penalty/var_check/goal_cancelled/halftime/fulltime → UrgencyCritical
- backend/cmd/server/main.go:2262-2267 —— 兜底 TTL：critical 45s / normal 12s；:2280-2282 决策 Speech 的 TTLSeconds 覆盖兜底
- backend/cmd/server/main.go:632 —— `conversationScheduler.SubmitUser` 用户回合入口

## 代码走读

backend/internal/conversation/proactive_gate.go:15-21 —— 资格门小得惊人：没有用户分类许可体系，只有运营开关+事件白名单，标签覆盖在 main.go:572-576：

```go
func (g *ProactiveGate) Allow(policy matchstate.AutomationPolicy, eventType string, _ bool, _ time.Time) bool {
	return policy.Mode == matchstate.AutomationModeActive && containsEventType(policy.EventTypes, eventType)
}

func (g *ProactiveGate) AllowManual(_ time.Time) bool {
	return true
}
```

backend/internal/relationship/policy.go:79-92 —— 话痨控制 = 单一时间冷却。进球是 critical 不受冷却，但会记账 `LastCriticalAt`；普通事件距上次主动不足 90 秒就沉默：

```go
		cooldown := 90 * time.Second
		if signal.Match.NormalCooldownSeconds > 0 {
			cooldown = time.Duration(signal.Match.NormalCooldownSeconds) * time.Second
		}
		if !signal.Match.Critical && state.Match.Initiative.LastNormalAt != nil && now.Sub(*state.Match.Initiative.LastNormalAt) < cooldown {
			return []CommunicationAct{ActSilence}, []string{"natural_initiative_cooldown"}
		}
		if signal.Match.Critical {
			observedAt := now
			state.Match.Initiative.LastCriticalAt = &observedAt
		} else {
			observedAt := now
			state.Match.Initiative.LastNormalAt = &observedAt
		}
```

backend/internal/conversation/scheduler.go:186-206 —— 提交即三查：清过期 key、同 key TTL 内丢弃（防同一颗进球播两遍）、用户回合取消当前活动回合并插队：

```go
			now := time.Now()
			for key, expires := range seenKeys {
				if !expires.After(now) {
					delete(seenKeys, key)
				}
			}
			if !req.user && req.key != "" {
				if expires, exists := seenKeys[req.key]; exists && expires.After(now) {
					close(req.accepted)
					continue
				}
				seenKeys[req.key] = req.expires
			}
			if req.user {
				if active != nil {
					active.cancel()
					if active.timer != nil {
						active.timer.Stop()
					}
					active = nil
				}
				pending = append([]request{req}, pending...)
```

backend/internal/conversation/scheduler.go:207-218 —— 插队算法：找到队列里第一个非用户且紧急度更低的回合，插到它前面；没有就排尾：

```go
			} else {
				insertAt := len(pending)
				for index, queued := range pending {
					if !queued.user && queued.urgency < req.urgency {
						insertAt = index
						break
					}
				}
				pending = append(pending, request{})
				copy(pending[insertAt+1:], pending[insertAt:])
				pending[insertAt] = req
			}
```

## 评测联动

- `cd backend && go test ./internal/conversation/ -run 'TestUserTurn|TestDuplicate|TestCritical|TestExpired|TestPlaybackTimeout' -v`
  - `TestUserTurnPreemptsActiveProactiveTurn`（scheduler_test.go:44）：抢断失效时报 `timed out waiting for proactive turn to be canceled`（helper 在 :254-261，1 秒超时）
  - `TestDuplicateProactiveTurnIsIgnored`（:110）/ `TestExpiredProactiveTurnIsDropped`（:140）/ `TestCriticalProactiveTurnRunsBeforeNormalTurn`（:166）/ `TestPlaybackTimeoutReleasesQueuedTurn`（:197）
  - `TestProactiveGateOnlyAppliesAutomationEventSwitch`（proactive_gate_test.go:10）锁资格门
- eval：evals/cases/regression/manual-proactive-line.json —— 导播手写话术经 manual 通道逐字播出、不被球球改写（main.go:574-576）。
- 失效表现：去重坏了→同一颗进球两条播报；TTL 坏了→过期回合复活插话。

## 动手作业

1. 编辑 backend/internal/conversation/scheduler.go:199，把用户抢断分支的 `if active != nil {` 改成 `if false {`。
2. `cd backend && go test ./internal/conversation/ -run TestUserTurnPreemptsActiveProactiveTurn -v` —— 预期失败：`timed out waiting for proactive turn to be canceled`（活动播报没被取消，测试 1 秒超时）。
3. 改回后重跑全绿。再加一步只读观察：把 policy.go:79 的 `cooldown := 90 * time.Second` 改成 `5 * time.Second`，重跑 `-run TestHumanityScenarioConformance`——注意哪些场景仍然绿，想想为什么普通事件的节奏控制只被冷却时间戳约束。

## 延伸

- docs/球球课程/chapters/31-initiative-silence.facts.json（含"预算计数器是虚构、真实机制是冷却时间戳"的审计结论）、33a-agent-runtime.facts.json
- docs/adr/0003-unified-watch-turn-planning.md（所有回合走同一条规划流）
- evals/cases/regression/manual-proactive-line.json（manual 通道合同）
