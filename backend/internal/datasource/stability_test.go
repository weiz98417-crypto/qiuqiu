package datasource

// auto-hosting 2.3:稳定窗自动确认(ADR-0024)。红测试先行。

import (
	"context"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func newStabilityManager(t *testing.T, matchID string, phases [][]Event) (*Manager, *gatedEventsClient, *matchstate.Store) {
	t.Helper()
	store := matchstate.NewStore()
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "Arsenal", AwayTeam: "Liverpool"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	client := &gatedEventsClient{phases: phases}
	// 窗口 30s 由 sweep 的显式 now 推进;goroutine 节拍器关掉(窗口>0 会起),
	// 测试直接调 sweepStabilityConfirmations 控制时钟。
	manager := NewManager(context.Background(), store, client, ManagerConfig{PollInterval: 5 * time.Millisecond, AutoConfirmWindow: 50 * time.Millisecond})
	t.Cleanup(manager.Close)
	if _, err := manager.Start(matchID, SourceConfig{Type: SourceAPISports, FixtureID: 42, ImportHistory: true}); err != nil {
		t.Fatalf("start: %v", err)
	}
	return manager, client, store
}

func factStatus(t *testing.T, store *matchstate.Store, matchID, factID string) matchstate.FactStatus {
	t.Helper()
	for _, candidate := range store.Events(matchID) {
		if candidate.FactID == factID {
			return candidate.FactStatus
		}
	}
	t.Fatalf("fact %s not found", factID)
	return ""
}

func TestStabilityWindowConfirmsStableGoalAfterWindow(t *testing.T) {
	manager, _, store := newStabilityManager(t, "match-stab", [][]Event{
		{goalEvent(12, "Saka")},
	})
	goal := waitForFact(t, store, "match-stab", "goal", 3*time.Second)

	// 窗内:sweep 不确认。
	manager.sweepStabilityConfirmations(time.Now())
	if got := factStatus(t, store, "match-stab", goal.FactID); got != matchstate.FactStatusProvisional {
		t.Fatalf("in-window status = %q, want provisional", got)
	}

	// 窗满:自动确认。
	manager.sweepStabilityConfirmations(time.Now().Add(31 * time.Second))
	if got := factStatus(t, store, "match-stab", goal.FactID); got != matchstate.FactStatusConfirmed {
		t.Fatalf("post-window status = %q, want confirmed", got)
	}
}

func TestStabilityWindowVetoesOnUpstreamDrift(t *testing.T) {
	manager, client, store := newStabilityManager(t, "match-drift", [][]Event{
		{goalEvent(12, "Saka")},
		{goalEvent(12, "Odegaard")}, // 内容变化 → changed 漂移
	})
	goal := waitForFact(t, store, "match-drift", "goal", 3*time.Second)

	// 首见;漂移后窗口重置(序号制:标记序号落后于本场漂移序号即重置)。
	manager.sweepStabilityConfirmations(time.Now())
	client.advance()
	driftDeadline := time.Now().Add(3 * time.Second)
	for manager.stabilityDriftCount("match-drift") == 0 && time.Now().Before(driftDeadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if manager.stabilityDriftCount("match-drift") == 0 {
		t.Fatalf("drift not registered")
	}

	// 重置后:首 sweep 落重置(标记序号→本场最新漂移序号),再等窗满(50ms)
	// 一次 sweep 确认——veto 失灵(未重置)的话这一步在 60ms 时早已确认过,
	// 所以先用窗状态序号断言重置真的发生了。
	manager.sweepStabilityConfirmations(time.Now())
	if seq := manager.stabilityMarkDriftSeq("match-drift", goal.FactID); seq != 1 {
		t.Fatalf("window mark driftSeq = %d, want 1 (reset to latest drift)", seq)
	}
	time.Sleep(60 * time.Millisecond)
	manager.sweepStabilityConfirmations(time.Now())
	if got := factStatus(t, store, "match-drift", goal.FactID); got != matchstate.FactStatusConfirmed {
		t.Fatalf("post-reset status = %q, want confirmed", got)
	}
	if seq := manager.stabilityMarkDriftSeq("match-drift", goal.FactID); seq != -1 {
		t.Fatalf("confirmed fact should have no window mark, seq=%d", seq)
	}
}

func TestStabilityWindowNeverConfirmsOperatorOnlyTypes(t *testing.T) {
	manager, _, store := newStabilityManager(t, "match-varonly", [][]Event{
		{{Time: EventTime{Elapsed: 20}, Team: TeamRef{ID: 1, Name: "Arsenal"}, Type: "Var", Detail: "Penalty confirmed"}},
	})
	manager.sweepStabilityConfirmations(time.Now())
	checkpoint := time.Now().Add(10 * time.Minute)
	manager.sweepStabilityConfirmations(checkpoint)
	for _, candidate := range store.Events("match-varonly") {
		if candidate.EventType == "var_check" && candidate.FactStatus == matchstate.FactStatusConfirmed {
			t.Fatalf("var_check was auto-confirmed: %+v", candidate)
		}
	}
}

func TestAutoConfirmableEventTypesTable(t *testing.T) {
	// 信任表 default-deny(ADR-0024 D3):白名单六类,VAR/改判/点球类禁止。
	allowed := []string{"goal", "red_card", "yellow_card", "substitution", "kickoff", "halftime"}
	for _, eventType := range allowed {
		if !autoConfirmableEventTypes[eventType] {
			t.Fatalf("%s should be auto-confirmable", eventType)
		}
	}
	denied := []string{"var_check", "var_result", "goal_cancelled", "score_correction", "penalty", "penalty_awarded", "shot"}
	for _, eventType := range denied {
		if autoConfirmableEventTypes[eventType] {
			t.Fatalf("%s must stay operator-only", eventType)
		}
	}
}

func TestFactInOpenConflictDetected(t *testing.T) {
	conflicts := []matchstate.FactConflict{
		{ID: "c1", Status: matchstate.ConflictStatusResolved, Members: []matchstate.FactConflictMember{{FactID: "f1"}}},
		{ID: "c2", Status: matchstate.ConflictStatusOpen, Members: []matchstate.FactConflictMember{{FactID: "f2"}}},
	}
	if factInOpenConflict(conflicts, "f1") {
		t.Fatalf("resolved conflict must not block")
	}
	if !factInOpenConflict(conflicts, "f2") {
		t.Fatalf("open conflict member must block")
	}
	if factInOpenConflict(conflicts, "f3") {
		t.Fatalf("non-member must not block")
	}
}
