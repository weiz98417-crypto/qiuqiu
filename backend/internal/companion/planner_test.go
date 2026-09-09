package companion

import (
	"context"
	"testing"
	"time"

	"qiuqiu/internal/interaction"
	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

func TestAgentPlanProvidesOneUnifiedTurnSurface(t *testing.T) {
	store := matchstate.NewStore()
	tools := NewRepositoryMemoryTools(store)
	agent := NewAgent(tools)
	now := time.Date(2026, 9, 4, 12, 0, 0, 0, time.UTC)

	plan, err := agent.Plan(context.Background(), TurnInput{
		Kind: TurnKindUser,
		Message: &MessageRequest{
			SignalID: "signal-1", MatchID: "match-1", UserID: "user-1", Text: "在吗", Now: now,
		},
	})
	if err != nil {
		t.Fatalf("plan user turn: %v", err)
	}
	if plan.Kind != TurnKindUser || plan.Trace.ID == "" || plan.Reply == "" {
		t.Fatalf("unexpected user plan: %+v", plan)
	}
	if got, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "signal-2", MatchID: "match-1", UserID: "user-1", Text: "在吗", Now: now,
	}); err != nil || got.Trace.ID == "" {
		t.Fatalf("facade through planner failed: response=%+v err=%v", got, err)
	}
}

func TestAgentPlanRejectsMissingPayload(t *testing.T) {
	agent := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore()))
	if _, err := agent.Plan(context.Background(), TurnInput{Kind: TurnKindMatchEvent}); err == nil {
		t.Fatal("expected missing match event payload error")
	}
	if _, err := agent.Plan(context.Background(), TurnInput{Kind: TurnKind("unknown")}); err == nil {
		t.Fatal("expected unsupported turn kind error")
	}
}

func TestAgentPlanCanBeRebuiltFromInteractionLedger(t *testing.T) {
	ledger := interaction.NewMemoryLedger()
	agent := NewAgent(NewRepositoryMemoryTools(matchstate.NewStore())).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithInteractionLedger(ledger)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	plan, err := agent.Plan(context.Background(), TurnInput{
		Kind: TurnKindUser,
		Message: &MessageRequest{
			SignalID: "rebuild-signal", MatchID: "match-1", UserID: "user-1", Text: "在吗", Now: now,
		},
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	events, err := ledger.List(context.Background(), "user-1", "match-1", 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	turns := interaction.ProjectTurns(events)
	if len(turns) != 1 {
		t.Fatalf("turns = %+v", turns)
	}
	rebuilt := turns[0]
	if rebuilt.InputText != "在吗" || rebuilt.OutputText != plan.Reply || rebuilt.Decision == nil || rebuilt.Decision.ID != plan.Decision.ID {
		t.Fatalf("rebuilt turn = %+v, plan = %+v", rebuilt, plan)
	}
	if rebuilt.Presentation == nil || rebuilt.Presentation.Expression != plan.Presentation.Expression || rebuilt.SignalID != "rebuild-signal" || rebuilt.TraceID != plan.Trace.ID {
		t.Fatalf("rebuilt correlation = %+v", rebuilt)
	}
}

func TestAgentRefreshesUserTurnWithOneDecisionAndStalesPriorPlan(t *testing.T) {
	store := matchstate.NewStore()
	ledger := interaction.NewMemoryLedger()
	agent := NewAgent(NewRepositoryMemoryTools(store)).
		WithDirector(relationship.NewDirector(relationship.NewMemoryRepository())).
		WithInteractionLedger(ledger)
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	request := MessageRequest{SignalID: "fact-race", MatchID: "match-1", UserID: "user-1", Text: "现在几比几？", Now: now}
	first, err := agent.Plan(context.Background(), TurnInput{Kind: TurnKindUser, Message: &request})
	if err != nil {
		t.Fatalf("first Plan: %v", err)
	}
	if _, _, err := store.Create("match-1", matchstate.MatchEvent{ID: "goal-1", EventType: "goal", Clock: "12:00", TeamID: "home", Description: "主队进球", Score: matchstate.Score{Home: 1}, FactStatus: matchstate.FactStatusConfirmed, FactRevision: 1}); err != nil {
		t.Fatalf("Create fact: %v", err)
	}
	request.FactRefresh = "goal-1:1:confirmed"
	request.Now = now.Add(time.Millisecond)
	refreshed, err := agent.Plan(context.Background(), TurnInput{Kind: TurnKindUser, Message: &request})
	if err != nil {
		t.Fatalf("refreshed Plan: %v", err)
	}
	if refreshed.Trace.ID == first.Trace.ID {
		t.Fatalf("refresh reused trace %q", first.Trace.ID)
	}
	if first.Decision == nil || refreshed.Decision == nil || first.Decision.ID != refreshed.Decision.ID {
		t.Fatalf("refresh created another decision: first=%+v refreshed=%+v", first.Decision, refreshed.Decision)
	}
	events, err := ledger.List(context.Background(), request.UserID, request.MatchID, 20)
	if err != nil {
		t.Fatal(err)
	}
	turns := interaction.ProjectTurns(events)
	if len(turns) != 2 || !turns[0].Stale || turns[1].Stale {
		t.Fatalf("refresh projection = %+v", turns)
	}
}
