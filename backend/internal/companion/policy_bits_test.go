package companion

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

// B2 理由码（policy-bits 4.1）：赛点事件的引用码 pivotal 经 writer hook
// 拼前缀后，决策理由码 = proactive_citation:pivotal。
func TestEvalPivotalCitationRidesAsProactiveReasonCode(t *testing.T) {
	ctx := context.Background()
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "policy-bits-pivotal"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	event, snapshot, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:     "red_card",
		Period:        "second_half",
		Clock:         "77:20",
		TeamID:        "away",
		TeamName:      "德国",
		Description:   "德国后卫两黄变一红被罚下。",
		ProactiveText: "德国少一人了，这下难办了。",
	})
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	proactive, err := agent.WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).HandleMatchEvent(ctx, MatchEventRequest{
		UserID: "user-1", Event: event, Snapshot: snapshot,
		OutputAllowed: true, Critical: true, Pivotal: true,
		CitationReason: "pivotal",
		Now:            time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMatchEvent error: %v", err)
	}
	found := false
	for _, code := range proactive.Decision.ReasonCodes {
		if code == "proactive_citation:pivotal" {
			found = true
		}
	}
	if !found {
		t.Fatalf("decision reason codes = %v, want proactive_citation:pivotal", proactive.Decision.ReasonCodes)
	}
}

// C2 载荷流转（policy-bits 4.5）：MessageRequest.UserAffect 到达 policy——
// 战术问答在满载荷下改道安慰型；nil 载荷走原路（字节级现状）。
func TestEvalUserAffectBiasReachesPolicyThroughMessageRequest(t *testing.T) {
	ctx := context.Background()
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)

	biased, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: "match-bias", UserID: "user-1",
		Text: "防线为什么回收得这么深",
		UserAffect: &relationship.UserAffectBias{
			Label: "sad", Confidence: 0.8, TeamBehind: true,
		},
		Now: now,
	})
	if err != nil {
		t.Fatalf("HandleMessage biased error: %v", err)
	}
	assertContains(t, strings.Join(biased.Trace.RelationshipDecision.ReasonCodes, ","), "user_affect:comfort_over_analysis")

	plain, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: "match-bias", UserID: "user-1",
		Text: "防线为什么回收得这么深",
		Now:  now,
	})
	if err != nil {
		t.Fatalf("HandleMessage plain error: %v", err)
	}
	assertContains(t, strings.Join(plain.Trace.RelationshipDecision.ReasonCodes, ","), "explicit_analysis_request")
}
