---
lesson: 1
title: 事实层：进球如何成为可撤销的事实
trace_position: trace 第 2 站。上游：operator POST 佩德里进球（main.go:1978）；下游：投影快照经 Subscribe（main.go:436）流向人格层与调度层
depends_on: [47-fact-ledger-conflicts, 23-match-facts-public-score]
---

# 第 1 讲 · 事实层：进球如何成为可撤销的事实

## 这一站在 trace 上

佩德里的进球不是一条聊天记录，而是一条带 `RecordedSequence` 的追加事实。Store.Create 先在候选历史投影上校验它（进球必须精确 +1），再写入并重放出公开快照；此后 VAR 取消、操作员撤销都能沿同一账本重放出一致的新比分。人格层永远不直接改比分，只能消费这层投影。

```
POST events ──> Store.Create ──> ValidateAppend(候选历史投影) ──> 追加+RecordedSequence
                                    │                                   │
                                    v                                   v
                              拒绝：比分不精确+1                Replay(全量或 uptoSequence)
                                                                     │
                                                    快照(ProjectionVersion+ProjectedSequence)
```

## 证据锚点

- backend/internal/matchstate/store.go:29 —— FactStatus 五值枚举 provisional/confirmed/revoked/conflict/reconciled
- backend/internal/matchstate/fact_ledger.go:62 —— 进球投影六态机 pending/active/cancelled/checkpointed/revoked/unavailable
- backend/internal/matchstate/fact_ledger.go:11 —— `FactLedgerProjectorVersion = "fact-ledger-projector/v1"`
- backend/internal/matchstate/fact_ledger.go:80 —— `Replay(input, uptoSequence)`；:89-97 序号截断
- backend/internal/matchstate/fact_ledger.go:279 —— `orderFactEvents` 拒绝重复/残缺序号
- backend/internal/matchstate/store.go:679 —— Create 追加前调 `ValidateAppend`
- backend/internal/matchstate/store.go:1054 —— `transitionFact`（confirm/revoke/reconcile 统一入口）
- backend/internal/matchstate/store.go:1472 —— `validateFactTransition`：reconciled 只能从 conflict 进
- backend/internal/matchstate/store.go:1490 —— 追加校验：进球必须精确把比分 +1
- backend/migrations/022_fact_ledger_recorded_sequence.sql:1 —— recorded_sequence 序列+回填+重放索引
- backend/migrations/023_fact_conflicts.sql:1 —— fact_conflicts / fact_conflict_members 两表
- backend/migrations/024_fact_conflict_graph.sql:18 —— 有序边表 + legacy 边回填
- backend/migrations/027_fact_conflict_resolution_audit.sql:1 —— 裁决审计补 reason/resolved_by

## 代码走读

backend/internal/matchstate/store.go:27-35 —— "事实状态"只有五个值，旧文档的中文词典（待确认/有争议/已更正）在代码里就是这套英文名：

```go
type FactStatus string

const (
	FactStatusProvisional FactStatus = "provisional"
	FactStatusConfirmed   FactStatus = "confirmed"
	FactStatusRevoked     FactStatus = "revoked"
	FactStatusConflict    FactStatus = "conflict"
	FactStatusReconciled  FactStatus = "reconciled"
)
```

backend/internal/matchstate/fact_ledger.go:60-74 —— 一个进球在投影里是一台六态小状态机，不是一行数字：

```go
type projectedGoalState string

const (
	projectedGoalPending      projectedGoalState = "pending"
	projectedGoalActive       projectedGoalState = "active"
	projectedGoalCancelled    projectedGoalState = "cancelled"
	projectedGoalCheckpointed projectedGoalState = "checkpointed"
	projectedGoalRevoked      projectedGoalState = "revoked"
	projectedGoalUnavailable  projectedGoalState = "unavailable"
)

type projectedGoal struct {
	teamID string
	state  projectedGoalState
}
```

backend/internal/matchstate/fact_ledger.go:89-97 —— `Replay` 的第二参数让它能从任意历史位置重放"截至第 N 条"的世界：

```go
	if uptoSequence > 0 {
		filtered := events[:0]
		for _, event := range events {
			if event.RecordedSequence <= 0 || event.RecordedSequence <= uptoSequence {
				filtered = append(filtered, event)
			}
		}
		events = filtered
	}
```

backend/internal/matchstate/fact_ledger.go:181-198 —— 取消进球不是把比分 -1 了事，按当前状态分派：active 才回退、checkpointed 说明后面已有比分更正、重复取消直接报错：

```go
			switch goal.state {
			case projectedGoalActive:
				if err := changeTeamScore(&snapshot.Score, goal.teamID, -1); err != nil {
					return FactLedgerProjection{}, err
				}
				goal.state = projectedGoalCancelled
				goals[canonicalID] = goal
			case projectedGoalRevoked:
			case projectedGoalUnavailable:
				return FactLedgerProjection{}, fmt.Errorf("%w: goal cancellation references a goal that was never public", ErrInvalid)
			case projectedGoalCheckpointed:
				return FactLedgerProjection{}, fmt.Errorf("%w: goal %q is earlier than the latest score correction", ErrInvalid, reference)
			case projectedGoalPending:
				return FactLedgerProjection{}, fmt.Errorf("%w: goal cancellation appears before goal %q", ErrInvalid, reference)
			default:
				return FactLedgerProjection{}, fmt.Errorf("%w: goal %q cannot be cancelled from state %q", ErrInvalid, reference, goal.state)
			}
```

backend/internal/matchstate/store.go:1490-1503 —— 追加即验证：新事件先在候选历史投影上校验，进球必须精确把公开比分 +1，所以"双计"在入口就进不来：

```go
	if ev.EventType == "goal" {
		expected := current.Score
		switch ev.TeamID {
		case "home":
			expected.Home++
		case "away":
			expected.Away++
		default:
			return fmt.Errorf("%w: goal requires home or away teamId", ErrInvalid)
		}
		if ev.Score != expected {
			return fmt.Errorf("%w: goal score must move from %d-%d to %d-%d", ErrInvalid, current.Score.Home, current.Score.Away, expected.Home, expected.Away)
		}
		return nil
	}
```

## 评测联动

- `cd backend && go test ./internal/matchstate/ -run 'TestGoalVARCancellation|TestFactLedger' -v`
  - `TestGoalVARCancellationRevisesTheAuthoritativeScore`（store_test.go:233）：进球→VAR→`goal_cancelled`→断言快照比分回 0-0（store_test.go:261-263）
  - `TestFactLedgerReplayAtSequenceIsDeterministic` / `...RejectsIncompleteSequenceHistory` / `TestFactLedgerProjectsScoreAfterEarlierGoalIsRevoked`（fact_ledger_test.go:10/40/54）
- eval：evals/cases/boundary/correction-revokes-old-facts.json（更正让旧事实失效）。
- 失效表现：比分双计或取消不回退时，上面测试以 `score after cancellation = ..., want 0-0` 这类断言失败。

## 动手作业

把投影里的 +1 改成 +2，看不变量怎么接住你：

1. 编辑 backend/internal/matchstate/fact_ledger.go:165，把 `changeTeamScore(&snapshot.Score, event.TeamID, 1)` 的 `1` 改成 `2`。
2. `cd backend && go test ./internal/matchstate/` —— 预期一批测试失败，报错里出现多加的比分（如 `goal score must move from 2-0 to 3-0`，来自 store.go:1501）或断言 `score = 2-0`：因为追加校验（store.go:1490）与投影（fact_ledger.go:165）必须指向同一个世界，两处一处改、另一处立刻对不上。
3. 改回 `1`，重跑确认全绿。这个实验说明：比分不变量不是一处 if，而是"校验层+投影层"互相咬合。

## 延伸

- docs/球球课程/chapters/47-fact-ledger-conflicts.facts.json（13 条 implemented 锚点）、23-match-facts-public-score.facts.json
- docs/adr/0002-match-fact-ledger-as-source-of-truth.md（账本是唯一事实源）；设计过程见 docs/fact-ledger-replay-conflict-plan.md
