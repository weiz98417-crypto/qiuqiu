package matchstate

import (
	"errors"
	"testing"
	"time"
)

func TestFactLedgerCommandsProduceImmutableRevisions(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	input := FactLedgerProjectInput{MatchID: "commands", Now: now, Events: []MatchEvent{{
		ID: "event-1", FactID: "fact-1", MatchID: "commands", Source: "provider", EventType: "shot",
		Status: "active", Visibility: "private", FactStatus: FactStatusProvisional, FactRevision: 1,
	}}}
	confirmed, err := (FactLedgerEngine{}).Confirm(input, "fact-1", "operator-1")
	if err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	if input.Events[0].FactStatus != FactStatusProvisional {
		t.Fatal("command mutated input history")
	}
	if confirmed.Changed.FactStatus != FactStatusConfirmed || confirmed.Changed.FactRevision != 2 {
		t.Fatalf("confirmed = %+v", confirmed.Changed)
	}
	revoked, err := (FactLedgerEngine{}).Transition(FactLedgerProjectInput{MatchID: "commands", Events: confirmed.Events, Now: now.Add(time.Second)}, "fact-1", "operator-2", FactStatusRevoked)
	if err != nil {
		t.Fatalf("Transition: %v", err)
	}
	if revoked.Changed.FactStatus != FactStatusRevoked || revoked.Changed.FactRevision != 3 {
		t.Fatalf("revoked = %+v", revoked.Changed)
	}
}

func TestFactLedgerResolveConflictOwnsEventAndConflictTransitions(t *testing.T) {
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	accepted := MatchEvent{ID: "accepted", MatchID: "match-1", FactID: "fact-a", Status: "active", FactStatus: FactStatusConfirmed, Confirmed: true, FactRevision: 1}
	candidate := MatchEvent{ID: "candidate", MatchID: "match-1", FactID: "fact-b", Status: "active", FactStatus: FactStatusConflict, FactRevision: 1}
	conflict := FactConflict{
		ID: "conflict-1", MatchID: "match-1", Status: ConflictStatusOpen,
		Members: []FactConflictMember{{FactID: "fact-a", Role: ConflictMemberAccepted}, {FactID: "fact-b", Role: ConflictMemberCandidate}},
		Edges:   []FactConflictEdge{{LeftFactID: "fact-a", RightFactID: "fact-b"}},
	}
	result, err := (FactLedgerEngine{}).ResolveConflict(FactLedgerProjectInput{
		MatchID: "match-1", Events: []MatchEvent{accepted, candidate}, Now: now,
	}, conflict, []string{"fact-b"}, "operator-1", "采用候选事实")
	if err != nil {
		t.Fatalf("ResolveConflict: %v", err)
	}
	if result.Conflict.Status != ConflictStatusResolved || result.Conflict.ChosenFactID != "fact-b" {
		t.Fatalf("conflict result = %+v", result.Conflict)
	}
	if len(result.Retracted) != 1 || result.Retracted[0].FactID != "fact-a" || result.Retracted[0].FactStatus != FactStatusRevoked {
		t.Fatalf("retracted = %+v", result.Retracted)
	}
	var selected MatchEvent
	for _, event := range result.Events {
		if event.FactID == "fact-b" {
			selected = event
		}
	}
	if selected.FactStatus != FactStatusReconciled || !selected.Confirmed || selected.ConfirmedBy != "operator-1" || selected.FactRevision != 2 {
		t.Fatalf("selected event = %+v", selected)
	}
	if accepted.FactStatus != FactStatusConfirmed || candidate.FactStatus != FactStatusConflict {
		t.Fatal("input events were mutated")
	}
}

func TestFactLedgerCorrectionUsesSharedValidation(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	input := FactLedgerProjectInput{MatchID: "correction", Now: now, Events: []MatchEvent{{
		ID: "goal-1", FactID: "goal-1", MatchID: "correction", EventType: "goal", TeamID: "home",
		Score: Score{Home: 1}, Status: "active", Visibility: "public", FactStatus: FactStatusConfirmed, FactRevision: 1,
	}}}
	_, err := (FactLedgerEngine{}).Correct(input, "goal-1", MatchEvent{EventType: "goal", TeamID: "away", Score: Score{Away: 1}}, "goal-2", 2)
	if err != nil && !errors.Is(err, ErrInvalid) {
		t.Fatalf("Correct error = %v", err)
	}
}
