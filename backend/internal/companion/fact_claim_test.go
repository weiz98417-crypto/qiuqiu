package companion

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

func TestMatchEventPraiseDoesNotAffirmMissingEvent(t *testing.T) {
	for _, input := range []string{"刚刚那个球真漂亮吧", "刚刚的进球真漂亮吧"} {
		t.Run(input, func(t *testing.T) {
			store := matchstate.NewStore()
			agent := NewAgent(NewStoreMemoryTools(store)).WithDirector(
				relationship.NewDirector(relationship.NewMemoryRepository()),
			).WithRealizer(fakeRealizer{text: "确实漂亮！这脚球太漂亮了。"}, time.Second)

			response, err := agent.HandleMessage(context.Background(), MessageRequest{
				SignalID: "match-praise-no-event",
				MatchID:  "match-praise-no-event",
				UserID:   "user-1",
				Text:     input,
				Now:      time.Date(2026, 7, 17, 6, 30, 0, 0, time.UTC),
			})
			if err != nil {
				t.Fatalf("HandleMessage error: %v", err)
			}
			if response.Intent != IntentEmotionReaction {
				t.Fatalf("intent = %q, want %q", response.Intent, IntentEmotionReaction)
			}
			if strings.Contains(response.Reply, "漂亮") || strings.Contains(response.Reply, "确实") {
				t.Fatalf("reply affirmed an event that does not exist: %q", response.Reply)
			}
			assertContains(t, response.Reply, "还没看到你说的那一下")
			assertToolCalled(t, response.Trace, "match.search_events")
			assertToolCalled(t, response.Trace, "match.verify_user_claim")
			if response.Trace.Claim == nil || response.Trace.Claim.Kind != "event_reference" || response.Trace.Claim.Status != ClaimStatusUnverified {
				t.Fatalf("claim assessment = %+v, want unverified event reference", response.Trace.Claim)
			}
		})
	}
}

func TestDeicticMatchPraiseUsesConfirmedRecentEvent(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "deictic-praise-confirmed-event"
	goal, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "31:15",
		TeamID:      "home",
		PlayerName:  "萨拉赫",
		Score:       matchstate.Score{Home: 1},
		Description: "萨拉赫禁区内推射破门。",
		Visibility:  "public",
	})
	if err != nil {
		t.Fatalf("Create goal error: %v", err)
	}
	agent := NewAgent(NewStoreMemoryTools(store)).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	).WithRealizer(fakeRealizer{text: "我没看到任何比赛动态。"}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "deictic-praise-confirmed-event",
		MatchID:  matchID,
		UserID:   "user-1",
		Text:     "刚刚那个球真漂亮吧",
		Now:      time.Date(2026, 7, 17, 6, 31, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	assertContains(t, response.Reply, "萨拉赫禁区内推射破门")
	assertToolCalled(t, response.Trace, "match.search_events")
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusConfirmed {
		t.Fatalf("claim assessment = %+v, want confirmed event reference", response.Trace.Claim)
	}
	if len(response.Trace.RetrievedEvent) != 1 || response.Trace.RetrievedEvent[0] != goal.ID {
		t.Fatalf("retrieved events = %v, want %s", response.Trace.RetrievedEvent, goal.ID)
	}
}

func TestFalseScoreClaimUsesMatchFactsBeforeRealization(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "德国队这状态真猛啊，3比0了！"}, time.Second)
	matchID := "fact-claim-score-conflict"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "德国已经3比0领先了",
		Now:     time.Date(2026, 7, 14, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Intent != IntentMatchClaim {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentMatchClaim)
	}
	assertContains(t, response.Reply, "0-0")
	assertNotContains(t, response.Reply, "3比0")
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusContradicted {
		t.Fatalf("claim assessment = %+v, want contradicted", response.Trace.Claim)
	}
	assertToolCalled(t, response.Trace, "match.read_snapshot")
	assertToolCalled(t, response.Trace, "match.verify_user_claim")
}

func TestContradictedClaimCannotPassRealizerByIncludingBothScores(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "西班牙 0-0 德国，不过德国确实3比0领先了！"}, time.Second)
	matchID := "fact-claim-realizer-double-score"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "德国已经3比0领先了",
		Now:     time.Date(2026, 7, 14, 20, 0, 30, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Reply != "还没，场上现在是西班牙 0-0 德国。" {
		t.Fatalf("reply = %q, want deterministic correction", response.Reply)
	}
	assertNotContains(t, response.Reply, "3比0")
}

func TestWrongScorerClaimIsCorrectedFromRecentGoal(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "对，刚才就是萨拉赫进的！"}, time.Second)
	matchID := "fact-claim-wrong-scorer"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam:    "西班牙",
		AwayTeam:    "德国",
		HomePlayers: []matchstate.Player{{Name: "佩德里"}},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	goal, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "23:41",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里禁区前沿推射破门。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"},
		},
	})
	if err != nil {
		t.Fatalf("Create goal error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "刚才萨拉赫进球了",
		Now:     time.Date(2026, 7, 14, 20, 1, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Intent != IntentMatchClaim {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentMatchClaim)
	}
	assertContains(t, response.Reply, "佩德里")
	assertContains(t, response.Reply, "不是萨拉赫")
	if response.Reply == "对，刚才就是萨拉赫进的！" {
		t.Fatal("unsafe realizer reply should not be emitted")
	}
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusContradicted {
		t.Fatalf("claim assessment = %+v, want contradicted", response.Trace.Claim)
	}
	assertContains(t, response.Trace.RetrievedEvent[0], goal.ID)
	assertToolCalled(t, response.Trace, "match.search_events")
}

func TestUnknownRosterNameInGoalClaimIsStillVerified(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "fact-claim-generic-player-name"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "41:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里进球。",
	}); err != nil {
		t.Fatalf("Create goal error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "刚才哈兰德进球了",
		Now:     time.Date(2026, 7, 14, 20, 7, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusContradicted {
		t.Fatalf("claim assessment = %+v, want contradicted", response.Trace.Claim)
	}
	assertContains(t, response.Reply, "不是哈兰德")
	assertContains(t, response.Reply, "佩德里")
}

func TestUnconfirmedGoalClaimWaitsForMatchEvidence(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "对，佩德里刚刚进球了！"}, time.Second)
	matchID := "fact-claim-unconfirmed-goal"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "佩德里刚刚进球了吧",
		Now:     time.Date(2026, 7, 14, 20, 2, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Intent != IntentMatchClaim {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentMatchClaim)
	}
	assertContains(t, response.Reply, "还没跟上")
	assertNotContains(t, response.Reply, "对，佩德里")
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusUnverified {
		t.Fatalf("claim assessment = %+v, want unverified", response.Trace.Claim)
	}
	if response.Trace.Claim.Certainty != "uncertain" {
		t.Fatalf("claim certainty = %q, want uncertain", response.Trace.Claim.Certainty)
	}
}

func TestScoreQuestionWithConcreteNumbersReadsSnapshot(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "对，德国已经3比0了！"}, time.Second)
	matchID := "fact-claim-score-question"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "德国3比0了吗？",
		Now:     time.Date(2026, 7, 14, 20, 3, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Intent != IntentMatchStatus {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentMatchStatus)
	}
	assertContains(t, response.Reply, "0-0")
	assertNotContains(t, response.Reply, "3比0")
	assertToolCalled(t, response.Trace, "match.read_snapshot")
}

func TestConcreteScoreQuestionCannotPassRealizerWithTwoScores(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "现在是西班牙 0-0 德国，00:00，不过德国确实3比0了。"}, time.Second)
	matchID := "fact-question-realizer-double-score"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "德国3比0了吗？",
		Now:     time.Date(2026, 7, 14, 20, 3, 10, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Reply != "现在是西班牙 0-0 德国，时间在赛前 00:00。" {
		t.Fatalf("reply = %q, want deterministic status", response.Reply)
	}
	assertNotContains(t, response.Reply, "3比0")
}

func TestScoreQuestionStaysUncertainWhenSnapshotIsInconsistent(t *testing.T) {
	baseStore := matchstate.NewStore()
	tools := &snapshotOverrideTools{
		StoreMemoryTools: NewStoreMemoryTools(baseStore),
		snapshot: matchstate.Snapshot{
			MatchID:  "fact-question-inconsistent-source",
			HomeTeam: "西班牙",
			AwayTeam: "德国",
			Score:    matchstate.Score{Home: 0, Away: 0},
			Period:   "pre_match",
			Clock:    "00:00",
			RecentEvents: []matchstate.MatchEvent{{
				ID: "legacy-invalid-goal", EventType: "goal", Period: "pre_match", Clock: "00:00", TeamID: "away", Score: matchstate.Score{Home: 0, Away: 0},
			}},
		},
	}
	tools.events = tools.snapshot.RecentEvents
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "对，德国已经3比0了！"}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: tools.snapshot.MatchID,
		UserID:  "user-1",
		Text:    "德国3比0了吗？",
		Now:     time.Date(2026, 7, 14, 20, 3, 15, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Intent != IntentMatchStatus {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentMatchStatus)
	}
	assertContains(t, response.Reply, "对不上")
	assertNotContains(t, response.Reply, "3比0")
}

func TestScoreClaimUsesTeamOrderAroundScore(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "fact-claim-score-team-order"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "15:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里进球。",
	}); err != nil {
		t.Fatalf("Create goal error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "德国0比1西班牙",
		Now:     time.Date(2026, 7, 14, 20, 3, 30, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusConfirmed {
		t.Fatalf("claim assessment = %+v, want confirmed", response.Trace.Claim)
	}
	assertContains(t, response.Reply, "西班牙 1-0 德国")
}

func TestInconsistentMatchSnapshotDoesNotOverruleUserClaim(t *testing.T) {
	baseStore := matchstate.NewStore()
	tools := &snapshotOverrideTools{
		StoreMemoryTools: NewStoreMemoryTools(baseStore),
		snapshot: matchstate.Snapshot{
			MatchID:  "fact-claim-inconsistent-source",
			HomeTeam: "西班牙",
			AwayTeam: "德国",
			Score:    matchstate.Score{Home: 0, Away: 0},
			Period:   "pre_match",
			Clock:    "00:00",
			RecentEvents: []matchstate.MatchEvent{{
				ID:         "legacy-invalid-goal",
				EventType:  "goal",
				Period:     "pre_match",
				Clock:      "00:00",
				TeamID:     "away",
				PlayerName: "佩德里",
				Score:      matchstate.Score{Home: 0, Away: 0},
			}},
		},
	}
	tools.events = tools.snapshot.RecentEvents
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "德国已经3比0领先了！"}, time.Second)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: tools.snapshot.MatchID,
		UserID:  "user-1",
		Text:    "德国已经3比0领先了",
		Now:     time.Date(2026, 7, 14, 20, 4, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusUnverified {
		t.Fatalf("claim assessment = %+v, want unverified", response.Trace.Claim)
	}
	assertContains(t, response.Reply, "还不能确定")
	assertNotContains(t, response.Reply, "3比0")
}

func TestRepeatedGoalScoreInSnapshotStaysUnverified(t *testing.T) {
	baseStore := matchstate.NewStore()
	tools := &snapshotOverrideTools{
		StoreMemoryTools: NewStoreMemoryTools(baseStore),
		snapshot: matchstate.Snapshot{
			MatchID:  "fact-claim-repeated-goal-score",
			HomeTeam: "西班牙",
			AwayTeam: "德国",
			Score:    matchstate.Score{Home: 1, Away: 0},
			Period:   "first_half",
			Clock:    "30:00",
			RecentEvents: []matchstate.MatchEvent{
				{ID: "goal-2", EventType: "goal", Period: "first_half", Clock: "30:00", TeamID: "home", Score: matchstate.Score{Home: 1, Away: 0}},
				{ID: "goal-1", EventType: "goal", Period: "first_half", Clock: "10:00", TeamID: "home", Score: matchstate.Score{Home: 1, Away: 0}},
			},
		},
	}
	tools.events = tools.snapshot.RecentEvents
	agent := NewAgent(tools)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: tools.snapshot.MatchID,
		UserID:  "user-1",
		Text:    "西班牙1比0德国",
		Now:     time.Date(2026, 7, 14, 20, 4, 30, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusUnverified {
		t.Fatalf("claim assessment = %+v, want unverified", response.Trace.Claim)
	}
	assertContains(t, response.Reply, "还不能确定")
}

func TestCrossSourceConflictMakesClaimsUnverified(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "fact-claim-cross-source-conflict"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		Source:      "operator",
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "23:41",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里进球。",
	}); err != nil {
		t.Fatalf("Create manual goal error: %v", err)
	}
	_, _, err := store.Create(matchID, matchstate.MatchEvent{
		Source:          "api-sports",
		ProviderEventID: "fixture-1:conflict",
		EventType:       "goal",
		Period:          "first_half",
		Clock:           "23:00",
		TeamID:          "home",
		TeamName:        "西班牙",
		PlayerName:      "亚马尔",
		Score:           matchstate.Score{Home: 2, Away: 0},
		Description:     "亚马尔进球。",
	})
	if !errors.Is(err, matchstate.ErrConflict) {
		t.Fatalf("Create conflicting goal error = %v, want ErrConflict", err)
	}

	agent := NewAgent(NewStoreMemoryTools(store))
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "西班牙1比0德国",
		Now:     time.Date(2026, 7, 14, 20, 5, 30, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusUnverified {
		t.Fatalf("claim assessment = %+v, want unverified", response.Trace.Claim)
	}
	assertContains(t, response.Reply, "还不能确定")
}

func TestWrongScoringTeamClaimIsCorrected(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools)
	matchID := "fact-claim-wrong-team"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam:    "西班牙",
		AwayTeam:    "德国",
		HomePlayers: []matchstate.Player{{Name: "佩德里"}},
	}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "31:00",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里进球。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"},
		},
	}); err != nil {
		t.Fatalf("Create goal error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "德国刚刚进球了",
		Now:     time.Date(2026, 7, 14, 20, 5, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if response.Trace.Claim == nil || response.Trace.Claim.Status != ClaimStatusContradicted {
		t.Fatalf("claim assessment = %+v, want contradicted", response.Trace.Claim)
	}
	assertContains(t, response.Reply, "不是德国")
	assertContains(t, response.Reply, "西班牙")
}

func TestHypotheticalScoreIsNotTreatedAsMatchFact(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "对，德国已经3比0领先了！"}, time.Second)
	matchID := "fact-claim-hypothetical"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	for index, text := range []string{"要是德国3比0就好了", "开玩笑，德国3比0了"} {
		response, err := agent.HandleMessage(context.Background(), MessageRequest{
			MatchID: matchID,
			UserID:  "user-1",
			Text:    text,
			Now:     time.Date(2026, 7, 14, 20, 6, index, 0, time.UTC),
		})
		if err != nil {
			t.Fatalf("HandleMessage(%q) error: %v", text, err)
		}
		if response.Intent == IntentMatchClaim {
			t.Fatalf("hypothetical %q intent = %q, should remain conversation", text, response.Intent)
		}
		if response.Trace.Claim != nil {
			t.Fatalf("hypothetical %q should not create fact claim: %+v", text, response.Trace.Claim)
		}
		assertNotContains(t, response.Reply, "3比0领先")
		assertToolNotCalled(t, response.Trace, "match.verify_user_claim")
	}
}

func TestPlayerGoalQuestionCannotBeTurnedIntoAffirmation(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewStoreMemoryTools(store)
	agent := NewAgent(tools).WithRealizer(fakeRealizer{text: "有，佩德里刚刚进球了！"}, time.Second)
	matchID := "fact-question-player-realizer"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		MatchID: matchID,
		UserID:  "user-1",
		Text:    "佩德里进球了吗？",
		Now:     time.Date(2026, 7, 14, 20, 6, 30, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	assertContains(t, response.Reply, "没有看到佩德里的进球记录")
	assertNotContains(t, response.Reply, "有，佩德里")
}

type snapshotOverrideTools struct {
	*StoreMemoryTools
	snapshot matchstate.Snapshot
	events   []matchstate.MatchEvent
}

func (t *snapshotOverrideTools) Snapshot(context.Context, string) (matchstate.Snapshot, error) {
	return t.snapshot, nil
}

func (t *snapshotOverrideTools) RecentEvents(context.Context, string, int) ([]matchstate.MatchEvent, error) {
	return append([]matchstate.MatchEvent(nil), t.events...), nil
}
