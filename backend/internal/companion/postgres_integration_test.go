package companion

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestPostgresCompanionPersistenceIntegration(t *testing.T) {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL not set")
	}

	ctx := context.Background()
	store, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("OpenPostgresStore error: %v", err)
	}
	defer store.Close()
	traces, err := OpenPostgresTraceWriter(ctx, databaseURL)
	if err != nil {
		t.Fatalf("OpenPostgresTraceWriter error: %v", err)
	}
	defer traces.Close()

	matchID := "pg-companion-eval-" + time.Now().UTC().Format("20060102150405")
	otherMatchID := matchID + "-other"
	if err := store.Reset(matchID); err != nil {
		t.Fatalf("Reset match error: %v", err)
	}
	if err := traces.Reset(matchID); err != nil {
		t.Fatalf("Reset traces error: %v", err)
	}
	if _, _, err := store.SetConfig(matchID, matchstate.MatchConfig{HomeTeam: "西班牙", AwayTeam: "德国"}); err != nil {
		t.Fatalf("SetConfig error: %v", err)
	}
	created, _, err := store.Create(matchID, matchstate.MatchEvent{
		EventType:   "goal",
		Period:      "first_half",
		Clock:       "24:10",
		TeamID:      "home",
		TeamName:    "西班牙",
		PlayerName:  "佩德里",
		Score:       matchstate.Score{Home: 1, Away: 0},
		Description: "佩德里禁区内抢点破门。",
		Participants: []matchstate.Participant{
			{Role: "scorer", Name: "佩德里", TeamID: "home", TeamName: "西班牙"},
			{Role: "assist", Name: "法比安", TeamID: "home", TeamName: "西班牙"},
			{Role: "pre_assist", Name: "亚马尔", TeamID: "home", TeamName: "西班牙"},
		},
	})
	if err != nil {
		t.Fatalf("Create goal error: %v", err)
	}

	agent := NewAgent(NewRepositoryMemoryTools(store).WithTraceWriter(traces).WithTurnReader(traces))
	firstRequest := MessageRequest{
		SignalID: "pg-turn-" + matchID,
		MatchID:  matchID,
		UserID:   "pg-user",
		Text:     "刚才谁助攻？",
		Now:      time.Now().UTC(),
	}
	reply, err := agent.HandleMessage(ctx, firstRequest)
	if err != nil {
		t.Fatalf("HandleMessage error: %v", err)
	}
	if !strings.Contains(strings.Join(reply.Trace.RetrievedEvent, ","), created.ID) {
		t.Fatalf("trace did not retrieve created goal: %+v", reply.Trace)
	}
	if _, err := agent.HandleMessage(ctx, firstRequest); err != nil {
		t.Fatalf("HandleMessage retry error: %v", err)
	}
	retriedTurns, err := traces.RecentTurns(ctx, matchID, "pg-user", 10)
	if err != nil {
		t.Fatalf("RecentTurns after retry error: %v", err)
	}
	if len(retriedTurns) != 2 {
		t.Fatalf("retry stored %d turns, want 2", len(retriedTurns))
	}
	claimReply, err := agent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "pg-user",
		Text:    "德国已经3比0领先了",
		Now:     time.Now().UTC().Add(time.Millisecond),
	})
	if err != nil {
		t.Fatalf("HandleMessage claim error: %v", err)
	}
	if claimReply.Trace.Claim == nil || claimReply.Trace.Claim.Status != ClaimStatusContradicted {
		t.Fatalf("claim trace mismatch: %+v", claimReply.Trace)
	}
	if err := traces.WriteTrace(ctx, Trace{
		ID:        "trace-other-" + matchID,
		MatchID:   otherMatchID,
		UserID:    "pg-user",
		Input:     "other",
		Intent:    IntentSmalltalk,
		Output:    "other",
		Reason:    "test",
		CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("WriteTrace other error: %v", err)
	}

	reopenedStore, err := matchstate.OpenPostgresStore(ctx, databaseURL, "../../migrations")
	if err != nil {
		t.Fatalf("reopen store error: %v", err)
	}
	defer reopenedStore.Close()
	reopenedTraces, err := OpenPostgresTraceWriter(ctx, databaseURL)
	if err != nil {
		t.Fatalf("reopen traces error: %v", err)
	}
	defer reopenedTraces.Close()

	reopenedSnapshot := reopenedStore.Snapshot(matchID)
	if reopenedSnapshot.Score != (matchstate.Score{Home: 1, Away: 0}) || len(reopenedSnapshot.KeyEvents) != 1 {
		t.Fatalf("reopened snapshot mismatch: %+v", reopenedSnapshot)
	}
	traceList, err := reopenedTraces.ListTraces(ctx, matchID, 10)
	if err != nil {
		t.Fatalf("ListTraces error: %v", err)
	}
	if len(traceList) < 2 {
		t.Fatalf("trace did not persist: %+v", traceList)
	}
	var persistedClaim *FactClaim
	for _, trace := range traceList {
		if trace.Intent == IntentMatchClaim {
			persistedClaim = trace.Claim
			break
		}
	}
	if persistedClaim == nil || persistedClaim.Status != ClaimStatusContradicted {
		t.Fatalf("claim assessment did not persist: %+v", traceList)
	}
	turns, err := reopenedTraces.RecentTurns(ctx, matchID, "pg-user", 10)
	if err != nil {
		t.Fatalf("RecentTurns error: %v", err)
	}
	if len(turns) < 2 {
		t.Fatalf("conversation turns did not persist: %+v", turns)
	}
	followUpAgent := NewAgent(NewRepositoryMemoryTools(reopenedStore).WithTurnReader(reopenedTraces))
	followUp, err := followUpAgent.HandleMessage(ctx, MessageRequest{
		MatchID: matchID,
		UserID:  "pg-user",
		Text:    "谁策动的？",
		Now:     time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("follow-up after reopen error: %v", err)
	}
	if !strings.Contains(followUp.Reply, "亚马尔") {
		t.Fatalf("follow-up did not use persisted turns/events: %+v", followUp)
	}

	if err := reopenedStore.Reset(matchID); err != nil {
		t.Fatalf("Reset store after reopen error: %v", err)
	}
	if err := reopenedTraces.Reset(matchID); err != nil {
		t.Fatalf("Reset traces after reopen error: %v", err)
	}
	if got := reopenedStore.Events(matchID); len(got) != 0 {
		t.Fatalf("reset did not clear events: %+v", got)
	}
	resetTraces, err := reopenedTraces.ListTraces(ctx, matchID, 10)
	if err != nil {
		t.Fatalf("ListTraces after reset error: %v", err)
	}
	if len(resetTraces) != 0 {
		t.Fatalf("reset did not clear traces: %+v", resetTraces)
	}
	otherTraces, err := reopenedTraces.ListTraces(ctx, otherMatchID, 10)
	if err != nil {
		t.Fatalf("ListTraces other error: %v", err)
	}
	if len(otherTraces) != 1 {
		t.Fatalf("reset should not clear other match traces: %+v", otherTraces)
	}
}
