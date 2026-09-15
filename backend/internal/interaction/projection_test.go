package interaction

import (
	"testing"

	"qiuqiu/internal/relationship"
)

func TestProjectJourneyRebuildsCorrelatedOutcome(t *testing.T) {
	events := []Event{
		{ID: "t1", Kind: KindTurnPlanned, UserID: "u1", MatchID: "m1", TraceID: "trace1", DecisionID: "d1", FactIDs: []string{"f2", "f1"}},
		{ID: "d1", Kind: KindDelivery, UserID: "u1", MatchID: "m1", TraceID: "trace1", DeliveryState: "completed"},
	}
	journey := ProjectJourney(events)
	if journey.TurnCount != 1 || journey.DeliveryCount != 1 || journey.DeliveryByState["completed"] != 1 {
		t.Fatalf("journey = %+v", journey)
	}
	if len(journey.FactIDs) != 2 || journey.FactIDs[0] != "f1" || journey.TraceIDs[0] != "trace1" || journey.DecisionIDs[0] != "d1" {
		t.Fatalf("correlation = %+v", journey)
	}
}

func TestProjectJourneySpansMultipleMatchesForOneUser(t *testing.T) {
	events := []Event{
		{ID: "t1", Kind: KindTurnPlanned, UserID: "u1", MatchID: "m1", TraceID: "trace1"},
		{ID: "t2", Kind: KindTurnPlanned, UserID: "u1", MatchID: "m2", TraceID: "trace2"},
	}
	journey := ProjectJourney(events)
	if journey.MatchID != "" || len(journey.MatchIDs) != 2 || journey.MatchIDs[0] != "m1" || journey.MatchIDs[1] != "m2" {
		t.Fatalf("multi-match journey = %+v", journey)
	}
}

func TestProjectTurnsRebuildsOneCorrelatedTurn(t *testing.T) {
	decision := relationship.Decision{ID: "decision", SignalID: "signal", Actions: []relationship.CommunicationAct{relationship.ActReact}}
	presentation := relationship.PresentationPlan{Expression: "excited", Motion: "cheer", VoiceStyle: "excited"}
	events := []Event{
		{ID: "s", Kind: KindSignal, UserID: "u", MatchID: "m", SignalID: "signal", InputText: "这个球算吗？"},
		{ID: "t", Kind: KindTurnPlanned, UserID: "u", MatchID: "m", TraceID: "trace", SignalID: "signal", DecisionID: "decision", Decision: &decision, Presentation: &presentation, OutputText: "不算，进球已取消。", FactIDs: []string{"fact"}, FactRevision: "3:reconciled"},
		{ID: "d", Kind: KindDelivery, UserID: "u", MatchID: "m", TraceID: "trace", DeliveryKey: "delivery", DeliveryState: "text_delivered"},
		{ID: "m", Kind: KindMediaDelivery, UserID: "u", MatchID: "m", TraceID: "trace", DeliveryKey: "delivery", DeliveryState: "failed", DeliveryReason: "provider unavailable", MediaType: "audio/mpeg"},
		{ID: "p", Kind: KindPlaybackResult, UserID: "u", MatchID: "m", TraceID: "trace", SignalID: "delivery:trace:completed", DeliveryKey: "delivery", PlaybackState: "completed"},
		{ID: "stale", Kind: KindTurnStale, UserID: "u", MatchID: "m", TraceID: "trace", Stale: true},
	}
	turns := ProjectTurns(events)
	if len(turns) != 1 || turns[0].InputText != "这个球算吗？" || turns[0].OutputText != "不算，进球已取消。" {
		t.Fatalf("turn content = %+v", turns)
	}
	if turns[0].Decision == nil || turns[0].Decision.ID != "decision" || turns[0].Presentation == nil || turns[0].Presentation.Expression != "excited" {
		t.Fatalf("turn plan = %+v", turns[0])
	}
	if turns[0].DecisionID != "decision" || turns[0].FactRevision != "3:reconciled" || turns[0].DeliveryKey != "delivery" || turns[0].MediaType != "audio/mpeg" || turns[0].PlaybackStates[0] != "completed" || !turns[0].Stale {
		t.Fatalf("turns = %+v", turns)
	}
	if turns[0].SignalID != "signal" {
		t.Fatalf("delivery event replaced original signal: %+v", turns[0])
	}
	if len(turns[0].DeliveryReasons) != 1 || turns[0].DeliveryReasons[0] != "provider unavailable" {
		t.Fatalf("delivery reason = %+v", turns[0].DeliveryReasons)
	}
}
