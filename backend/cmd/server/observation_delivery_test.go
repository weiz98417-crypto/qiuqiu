package main

import (
	"context"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/observation"
)

func TestObservationFollowUpsArePrivateAndRespectDeadline(t *testing.T) {
	now := time.Date(2026, 7, 17, 12, 0, 10, 0, time.UTC)
	responses := []companion.ObservationResponse{
		{Resolution: observation.Resolution{ObservationID: "obs-1", UserID: "user-1", FollowUpDeadline: now.Add(time.Second)}},
		{Resolution: observation.Resolution{ObservationID: "obs-2", UserID: "user-2", FollowUpDeadline: now.Add(time.Second)}},
		{Resolution: observation.Resolution{ObservationID: "obs-3", UserID: "user-1", FollowUpDeadline: now.Add(-time.Second)}},
	}

	selected := observationFollowUpsForUser(responses, "user-1", now)
	if len(selected) != 1 || selected[0].Resolution.ObservationID != "obs-1" {
		t.Fatalf("selected follow-ups = %+v", selected)
	}
}

func TestDisplayedObservationReplyCompletesRecoveryOutbox(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	coordinator := observation.NewMemoryCoordinator()
	tools := companion.NewStoreMemoryTools(matchstate.NewStore())
	agent := companion.NewAgent(tools).WithObservationCoordinator(coordinator)
	if _, err := coordinator.Record(ctx, observation.Input{
		SignalID: "turn-1", TraceID: "trace-1", UserID: "user-1", MatchID: "match-1",
		Kind: "event", EventType: "goal", ReceivedAt: now,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	followUps, err := agent.HandleObservationFactChanged(ctx, matchstate.MatchEvent{
		MatchID: "match-1", ID: "goal-1", FactID: "fact-1", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal",
		CreatedAt: now.Add(5 * time.Second).Format(time.RFC3339Nano),
	}, now.Add(5*time.Second))
	if err != nil || len(followUps) != 1 {
		t.Fatalf("follow-ups = %+v err=%v", followUps, err)
	}
	trace := followUps[0].Trace
	if err := recordDisplayedReply(ctx, tools, agent, trace.MatchID, trace.ID, trace.UserID, now.Add(6*time.Second)); err != nil {
		t.Fatalf("recordDisplayedReply: %v", err)
	}
	recovered, err := agent.RecoverObservationFollowUps(ctx, trace.UserID, trace.MatchID, now.Add(7*time.Second))
	if err != nil || len(recovered) != 0 {
		t.Fatalf("displayed reply remained recoverable: %+v err=%v", recovered, err)
	}
}
