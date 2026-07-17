package companion

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/observation"
)

func TestUnverifiedGoalClaimIsRecordedWithoutChangingPublicScore(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "late-fact-user-ahead"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam:    "利物浦",
		AwayTeam:    "阿森纳",
		HomePlayers: []matchstate.Player{{Name: "萨拉赫"}},
	}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	startLiveMatch(t, store, matchID)

	coordinator := observation.NewMemoryCoordinator()
	agent := NewAgent(NewStoreMemoryTools(store)).WithObservationCoordinator(coordinator)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "turn-user-ahead-1",
		MatchID:  matchID,
		UserID:   "user-1",
		Text:     "萨拉赫进球了！",
		Now:      now,
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}

	if response.Trace.Observation == nil || response.Trace.Observation.Status != observation.StatusPendingSync {
		t.Fatalf("observation trace = %+v, want pending_sync", response.Trace.Observation)
	}
	if response.Trace.Observation.UserID != "user-1" || response.Trace.Observation.MatchID != matchID {
		t.Fatalf("observation escaped user/match scope: %+v", response.Trace.Observation)
	}
	if !strings.Contains(response.Reply, "还没跟上") || strings.Contains(response.Reply, "萨拉赫进了") {
		t.Fatalf("reply = %q, want immediate reserved response", response.Reply)
	}
	if snapshot := store.PublicSnapshot(matchID); snapshot.Score != (matchstate.Score{}) {
		t.Fatalf("public score changed from user observation: %+v", snapshot.Score)
	}
}

func TestConfirmedObservationCreatesGroundedFollowUp(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "late-fact-follow-up"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{
		HomeTeam: "利物浦", AwayTeam: "阿森纳", HomePlayers: []matchstate.Player{{Name: "萨拉赫"}},
	}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	startLiveMatch(t, store, matchID)
	coordinator := observation.NewMemoryCoordinator()
	agent := NewAgent(NewStoreMemoryTools(store)).WithObservationCoordinator(coordinator)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "turn-follow-up-1", MatchID: matchID, UserID: "user-1", Text: "萨拉赫进球了！", Now: now,
	}); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	event := matchstate.MatchEvent{
		ID: "goal-1", MatchID: matchID, FactID: "fact-goal-1", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal", PlayerName: "萨拉赫",
		TeamName: "利物浦", Score: matchstate.Score{Home: 1},
		CreatedAt: now.Add(8 * time.Second).Format(time.RFC3339Nano),
	}

	followUps, err := agent.HandleObservationFactChanged(context.Background(), event, now.Add(8*time.Second))
	if err != nil {
		t.Fatalf("HandleObservationFactChanged: %v", err)
	}
	if len(followUps) != 1 || followUps[0].Resolution.UserID != "user-1" {
		t.Fatalf("follow-ups = %+v", followUps)
	}
	if followUps[0].Reply != "跟上了，确实是萨拉赫进的。" || followUps[0].Trace.ID == "" {
		t.Fatalf("follow-up = %+v", followUps[0])
	}
	if followUps[0].Presentation.Expression != "excited" || followUps[0].Presentation.Motion != "cheer" {
		t.Fatalf("presentation = %+v", followUps[0].Presentation)
	}
}

func TestUnverifiedDeicticReactionCreatesPendingObservation(t *testing.T) {
	store := matchstate.NewStore()
	startLiveMatch(t, store, "match-1")
	coordinator := observation.NewMemoryCoordinator()
	agent := NewAgent(NewStoreMemoryTools(store)).WithObservationCoordinator(coordinator)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)

	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "turn-deictic-1", MatchID: "match-1", UserID: "user-1",
		Text: "刚刚那个球真漂亮吧", Now: now,
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Trace.Observation == nil || response.Trace.Observation.Status != observation.StatusPendingSync {
		t.Fatalf("observation = %+v, want pending_sync", response.Trace.Observation)
	}
	if response.Trace.Observation.Kind != "event_reference" {
		t.Fatalf("observation kind = %q", response.Trace.Observation.Kind)
	}
	if response.Trace.Observation.EventType != "play" {
		t.Fatalf("observation event type = %q, want play", response.Trace.Observation.EventType)
	}
}

func TestPreMatchGoalClaimIsRejectedWithoutPendingObservation(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "pre-match-claim"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "利物浦", AwayTeam: "阿森纳"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	agent := NewAgent(NewStoreMemoryTools(store)).WithObservationCoordinator(observation.NewMemoryCoordinator())
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "turn-pre-match", MatchID: matchID, UserID: "user-1", Text: "萨拉赫进球了！",
		Now: time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Trace.Observation != nil {
		t.Fatalf("pre-match claim created pending observation: %+v", response.Trace.Observation)
	}
	if !strings.Contains(response.Reply, "还没开始") {
		t.Fatalf("reply = %q, want pre-match grounding", response.Reply)
	}
}

func TestObservationFollowUpCanRecoverUntilDisplayed(t *testing.T) {
	store := matchstate.NewStore()
	matchID := "recover-observation-follow-up"
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "利物浦", AwayTeam: "阿森纳"}); err != nil {
		t.Fatalf("SetConfig: %v", err)
	}
	startLiveMatch(t, store, matchID)
	coordinator := observation.NewMemoryCoordinator()
	agent := NewAgent(NewStoreMemoryTools(store)).WithObservationCoordinator(coordinator)
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "turn-recover", MatchID: matchID, UserID: "user-1", Text: "萨拉赫进球了！", Now: now,
	}); err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if _, err := agent.HandleObservationFactChanged(context.Background(), matchstate.MatchEvent{
		MatchID: matchID, ID: "goal-recover", FactID: "fact-recover", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal", PlayerName: "萨拉赫",
		CreatedAt: now.Add(5 * time.Second).Format(time.RFC3339Nano),
	}, now.Add(5*time.Second)); err != nil {
		t.Fatalf("HandleObservationFactChanged: %v", err)
	}

	recovered, err := agent.RecoverObservationFollowUps(context.Background(), "user-1", matchID, now.Add(6*time.Second))
	if err != nil || len(recovered) != 1 {
		t.Fatalf("recovered = %+v err=%v", recovered, err)
	}
	if err := agent.MarkObservationResolutionDelivered(context.Background(), recovered[0].Resolution.DeliveryKey, now.Add(7*time.Second)); err != nil {
		t.Fatalf("MarkObservationResolutionDelivered: %v", err)
	}
	recovered, err = agent.RecoverObservationFollowUps(context.Background(), "user-1", matchID, now.Add(8*time.Second))
	if err != nil || len(recovered) != 0 {
		t.Fatalf("delivered follow-up recovered again: %+v err=%v", recovered, err)
	}
}

func startLiveMatch(t *testing.T, store *matchstate.Store, matchID string) {
	t.Helper()
	if _, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType: "kickoff", Period: "first_half", Clock: "00:01", Score: matchstate.Score{},
		Description: "比赛开始。", Visibility: "public",
	}); err != nil {
		t.Fatalf("start live match: %v", err)
	}
}
