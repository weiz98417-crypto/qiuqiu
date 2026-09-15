---
lesson: 2
title: 人格层：谁决定球球想做什么
trace_position: trace 第 3 站。上游：账本投影的佩德里进球快照（第 1 讲）经 HandleMatchEvent 进入；下游：Decision（Actions/Speech/Presentation）交给调度层（第 3 讲）
depends_on: [28-persona-constitution, 29-relationship-stages]
---

# 第 2 讲 · 人格层：谁决定球球想做什么

## 这一站在 trace 上

佩德里进球到达时，Director.Apply 加载关系+比赛状态，applyPolicy 走确定性分支表决定"想做什么"——这里是 `ActReact`，外加 updateAffect 把唤醒度推高 0.8。人格不是提示词里的人设简介，而是一张可逐条审计的 cue→act 规则表：进球分支、脏话分级、调侃许可、边界记录全是 if/return，每个分支带 reasonCode。

```
Signal(match_event) ─> Director.Apply(director.go:23)
                        ├─ updateAffect   affect.go:9   （进球 arousal +0.8）
                        ├─ applyPolicy    policy.go:48  （cue→act 分支表）
                        ├─ advanceStage   policy.go:302 （证据驱动升阶段）
                        └─ Decision{Actions, Speech, Presentation, ReasonCodes}
```

## 证据锚点

- backend/internal/relationship/policy.go:48 —— `applyPolicy` 全部分支（:71-93 比赛事件、:104-116 脏话边界、:117-119 要安静、:127-138 工作边界、:149-154 共同瞬间、:164-166 侮辱拒绝、:173-178 调侃许可）
- backend/internal/relationship/policy.go:201 —— `"毒奶"` 命中 `CueBanterAllowed{Scope: "prediction"}`
- backend/internal/relationship/policy.go:302-320 —— `advanceStage`：familiar 需≥2 场+2 类证据，old_ballmate 需≥6 场+召回+分歧/修复
- backend/internal/relationship/policy.go:354-357 —— 纯 silence 决策 `SpeechPlan = nil`
- backend/internal/relationship/policy.go:381-385 —— `ForbiddenClaims: ["new_score","new_player","new_event","new_penalty_conclusion"]`
- backend/internal/relationship/policy.go:412-418 —— 脏话 = 唤醒度×阶段×未修复×用户未禁用
- backend/internal/relationship/policy.go:456-463 —— 调侃按六个领域逐项许可
- backend/internal/relationship/policy.go:494-517 —— 三类修复（banter_boundary/repetition/over_analysis）
- backend/internal/relationship/affect.go:9-40 —— 事件驱动的五维情感；:53-63 指数衰减（唤醒度 τ=90s 回落 0.2）
- backend/internal/relationship/affect.go:106-110 —— goal→expression "excited"/motion "cheer"
- backend/internal/relationship/director.go:71-79 —— applyPolicy→记忆→advanceStage 的调用序；:108-111 CompareAndSwap 带版本写入
- backend/internal/relationship/types.go:100-105 —— 四阶段 first_meeting/familiar/watch_buddy/old_ballmate

## 代码走读

backend/internal/relationship/policy.go:71-93 —— 进球事件的完整分支：禁言直接沉默，冷却未到沉默（第 3 讲展开），关键事件记 `LastCriticalAt`，否则 `ActReact`：

```go
	if signal.Kind == SignalMatchEvent && signal.Match != nil {
		updateAffect(&state.Match.Affect, *signal.Match, now)
		if !signal.Match.OutputAllowed {
			return []CommunicationAct{ActSilence}, []string{"match_event_observed_output_disabled"}
		}
		if state.Match.Initiative.Mode == "" {
			state.Match.Initiative.Mode = "natural"
		}
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
		return []CommunicationAct{ActReact}, []string{"match_event_affect_updated"}
	}
```

backend/internal/relationship/affect.go:12-33 —— 情感不是 LLM 的语气词，是事件驱动的五维向量，每个事件类型一组手写增量：

```go
	switch event.EventType {
	case "goal":
		state.Valence += 0.8
		state.Arousal += 0.8
		state.Tension += 0.1
		state.Confidence += 0.3
		state.Engagement += 0.4
	case "var_check":
		state.Arousal += 0.1
		state.Tension += 0.6
		state.Confidence -= 0.5
		state.Engagement += 0.2
	case "goal_cancelled":
		state.Valence -= 1.2
		state.Arousal -= 0.25
		state.Tension -= 0.2
		state.Confidence += 0.3
	case "shot_missed":
		state.Valence -= 0.2
		state.Arousal += 0.15
		state.Tension += 0.1
	}
```

backend/internal/relationship/policy.go:412-418 —— 脏话分级闸：默认 none，要唤醒度≥0.7 且用户没禁用，mild 需 familiar+，strong 需 watch_buddy+ 且唤醒度≥0.9，修复中一律 none：

```go
	if !relationshipState.Repair.Active && relationshipState.Preferences.ProfanityEnabled && matchState.Affect.Arousal >= 0.7 {
		if stageAtLeast(relationshipState.Stage, StageWatchBuddy) && matchState.Affect.Arousal >= 0.9 {
			policy.ProfanityLevel = "strong_non_directed"
		} else if stageAtLeast(relationshipState.Stage, StageFamiliar) {
			policy.ProfanityLevel = "mild_non_directed"
		}
	}
```

backend/internal/relationship/director.go:71-79 —— Apply 的主干：策略产出动作，记忆按需挑选，最后推进阶段——顺序即依赖：

```go
	actions, reasons := applyPolicy(&state, signal, updatedAt)
	reasons = append(reasons, applyRelationshipMemory(&state, signal, updatedAt)...)
	decisionID := "decision:" + signal.UserID + ":" + signal.MatchID + ":" + signal.ID
	selectedMemories := selectRelationshipMemories(&state, signal, updatedAt, decisionID, 2)
	if signal.FactRevision != "" {
		state.Match.LastFactRevision = signal.FactRevision
	}
	recordActions(&state.Match, signal.ID, actions, updatedAt)
	advanceStage(&state.Relationship)
```

## 评测联动

- `cd backend && go test ./internal/relationship/ -run 'TestContentPolicy|TestHumanityScenarioConformance' -v`
  - `TestContentPolicyScopesProfanityByStageIntensityAndRepair`（content_policy_test.go:8）锁死脏话矩阵：first_meeting+唤醒 1.0 → none；familiar+0.8 → mild；watch_buddy+0.95 → strong；old_ballmate 修复中 → none。失败输出 `profanity level = "..." want "..."`（content_policy_test.go:36）。
  - `TestHumanityScenarioConformance`（scenario_conformance_test.go:9）：20 个场景（数量断言在 :190），含 06_goal_cancelled_by_var（wantExpression "deflated"）、11_user_invites_banter（ActTease）、12_user_rejects_banter（ActRepair）、13_critical_stage_silence（ActSilence+excited）。
- eval：evals/cases/boundary/user-claim-conflicts.json（用户嘴硬时不附和）。
- 失效表现：改坏任一分支，场景测试直接报 actions/stage/expression 不匹配。

## 动手作业

1. 编辑 backend/internal/relationship/policy.go:201，把 `strings.Contains(text, "毒奶")` 改成 `"毒乃"`。
2. `cd backend && go test ./internal/relationship/ -run TestHumanityScenarioConformance -v` —— 预期 11_user_invites_banter 失败：场景输入"是不是又毒奶了？"（scenario_conformance_test.go:107）不再命中 cue，决策落到 :186 的 default_acknowledgement，而测试断言 wantActions `[tease]`。
3. 改回 `"毒奶"` 重跑全绿。体会：改一个 cue 词就是改人格行为，而行为有测试矩阵兜底。

## 延伸

- docs/球球课程/chapters/28-persona-constitution.facts.json、29-relationship-stages.facts.json
- docs/adr/0001-bounded-digital-ballmate.md（有边界的数字球友）、0004-content-constitution-evidence-anchors.md
- 底层提示词底线条款：backend/prompts/v1.0/system.txt（438 字节）；语言层硬约束见第 4 讲 realize.go:35-42
