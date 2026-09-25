package companion

// 观察协调（C2 persisted-claim hold）温热保持判定的单测：气氛旁证行
// （kind=ambient）永不参与温热保持——ADR-0002「气氛不作主张」宪法线在
// companion 侧的负例闸。

import (
	"context"
	"testing"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/observation"
	"qiuqiu/internal/router"
)

// TestAmbientCorroborationNeverWarmHolds：活跃欢呼旁证在场时，解析失败的
// FactClaim（assessMatchClaim ok=false 的全空形状）不得命中温热保持——
// 旁证不是「用户坚持的主张」。
func TestAmbientCorroborationNeverWarmHolds(t *testing.T) {
	coordinator := observation.NewMemoryCoordinator()
	agent, store := newRoutedAgent(t, nil)
	agent.WithObservationCoordinator(coordinator)
	matchID := "ambient-hold-guard"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam: "西班牙", AwayTeam: "德国",
		AwayPlayers: []matchstate.Player{{Number: "10", Name: "穆西亚拉", Position: "AM"}},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	ctx := context.Background()
	now := fixedTime()
	if _, err := coordinator.Record(ctx, observation.AmbientCorroboration("ambient-hold-1", "user-1", matchID, "cheer", now)); err != nil {
		t.Fatalf("Record ambient corroboration error: %v", err)
	}
	// 先证旁证行确实活跃：未命中的原因必须是守卫，而非行不存在。
	active, err := coordinator.ActiveObservations(ctx, "user-1", matchID)
	if err != nil {
		t.Fatalf("ActiveObservations error: %v", err)
	}
	if len(active) != 1 || active[0].Kind != observation.KindAmbient {
		t.Fatalf("active observations = %+v, want exactly the ambient row", active)
	}
	claim := FactClaim{Kind: "match_fact", Status: ClaimStatusUnverified, Reason: "claim could not be parsed"}
	if _, matched := agent.activeMatchingObservation(ctx, AgentBoundaryRequest{MatchID: matchID, UserID: "user-1", Now: now}, claim); matched {
		t.Fatal("ambient corroboration must never warm-hold a parse-failed claim")
	}
}

// handler 级负例：欢呼旁证在场时，解析失败的主张回合走正常兜底措辞，
// 绝不出 warm hold 台词（claim_persisted_hold 只属于用户自己的重复坚持）。
func TestAmbientCorroborationDoesNotTriggerPersistedHoldReply(t *testing.T) {
	coordinator := observation.NewMemoryCoordinator()
	agent, store := newRoutedAgent(t, &scriptedRouter{scripts: map[string]router.Result{
		// 解析不出任何主张语义、但被路由进 match_fact_claim 的回合。
		"这解说也太快了吧": {Intent: "match_fact_claim", Confidence: 0.9},
	}})
	agent.WithObservationCoordinator(coordinator)
	matchID := "ambient-hold-handler"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam: "西班牙", AwayTeam: "德国",
		AwayPlayers: []matchstate.Player{{Number: "10", Name: "穆西亚拉", Position: "AM"}},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	setFactClaimClock(t, store, matchID, "first_half", 31*60+15)
	ctx := context.Background()
	if _, err := coordinator.Record(ctx, observation.AmbientCorroboration("ambient-hold-2", "user-1", matchID, "cheer", fixedTime())); err != nil {
		t.Fatalf("Record ambient corroboration error: %v", err)
	}
	response, err := agent.HandleMessage(ctx, MessageRequest{
		SignalID: "ambient-hold-turn", MatchID: matchID, UserID: "user-1", Text: "这解说也太快了吧", Now: fixedTime(),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	assertNotContains(t, response.Reply, "我知道你看到了")
	assertContains(t, response.Reply, "还没跟上")
	if hasReasonCode(&response.Trace, ReasonClaimPersistedHold) {
		t.Fatalf("reason codes = %+v, must not carry %s", reasonCodesOf(&response.Trace), ReasonClaimPersistedHold)
	}
}
