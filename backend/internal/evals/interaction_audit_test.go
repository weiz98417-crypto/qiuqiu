package evals

import (
	"testing"

	"qiuqiu/internal/interaction"
)

func TestInteractionAuditBlocksMissingDeliveryAndFactRevision(t *testing.T) {
	report := AuditInteractions([]interaction.Event{{ID: "turn", Kind: interaction.KindTurnPlanned, UserID: "u1", MatchID: "m1", TraceID: "t1", FactIDs: []string{"f1"}}})
	if report.Blockers != 2 {
		t.Fatalf("report = %+v", report)
	}
}

func TestInteractionAuditAcceptsCorrelatedCompletedTurn(t *testing.T) {
	report := AuditInteractions([]interaction.Event{
		{ID: "turn", Kind: interaction.KindTurnPlanned, UserID: "u1", MatchID: "m1", TraceID: "t1", FactIDs: []string{"f1"}, FactRevision: "2:confirmed"},
		{ID: "delivery", Kind: interaction.KindDelivery, UserID: "u1", MatchID: "m1", TraceID: "t1", DeliveryState: "completed"},
	})
	if report.Blockers != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestInteractionAuditDoesNotRequireDeliveryForStaleTurn(t *testing.T) {
	report := AuditInteractions([]interaction.Event{
		{ID: "turn", Kind: interaction.KindTurnPlanned, UserID: "u1", MatchID: "m1", TraceID: "t1"},
		{ID: "stale", Kind: interaction.KindTurnStale, UserID: "u1", MatchID: "m1", TraceID: "t1", Stale: true},
	})
	if report.Blockers != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestInteractionAuditAcceptsFactRevisionFromLedger(t *testing.T) {
	report := AuditInteractions([]interaction.Event{
		{ID: "fact", Kind: interaction.KindFactRevision, UserID: "u1", MatchID: "m1", FactIDs: []string{"f1"}, FactRevision: "2:confirmed"},
		{ID: "turn", Kind: interaction.KindTurnPlanned, UserID: "u1", MatchID: "m1", TraceID: "t1", FactIDs: []string{"f1"}},
		{ID: "delivery", Kind: interaction.KindDelivery, UserID: "u1", MatchID: "m1", TraceID: "t1", DeliveryState: "completed"},
	})
	if report.Blockers != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestInteractionAuditAcceptsChosenSilenceAsSkippedDelivery(t *testing.T) {
	report := AuditInteractions([]interaction.Event{
		{ID: "turn", Kind: interaction.KindTurnPlanned, UserID: "u1", MatchID: "m1", TraceID: "t1"},
		{ID: "delivery", Kind: interaction.KindDelivery, UserID: "u1", MatchID: "m1", TraceID: "t1", DeliveryState: "skipped"},
	})
	if report.Blockers != 0 {
		t.Fatalf("report = %+v", report)
	}
}

func TestInteractionAuditBlocksDuplicateAudiblePlayback(t *testing.T) {
	report := AuditInteractions([]interaction.Event{
		{ID: "turn", Kind: interaction.KindTurnPlanned, UserID: "u1", MatchID: "m1", TraceID: "t1"},
		{ID: "delivery", Kind: interaction.KindDelivery, UserID: "u1", MatchID: "m1", TraceID: "t1", DeliveryState: "text_delivered"},
		{ID: "play-1", Kind: interaction.KindPlaybackResult, UserID: "u1", MatchID: "m1", TraceID: "t1", DeliveryKey: "key-1", PlaybackState: "completed"},
		{ID: "play-2", Kind: interaction.KindPlaybackResult, UserID: "u1", MatchID: "m1", TraceID: "t1", DeliveryKey: "key-1", PlaybackState: "ended"},
	})
	if report.Blockers != 1 {
		t.Fatalf("report = %+v", report)
	}
}

func TestInteractionAuditBlocksMediaFailureWithoutTextFallback(t *testing.T) {
	report := AuditInteractions([]interaction.Event{
		{ID: "turn", Kind: interaction.KindTurnPlanned, UserID: "u1", MatchID: "m1", TraceID: "t1"},
		{ID: "media", Kind: interaction.KindMediaDelivery, UserID: "u1", MatchID: "m1", TraceID: "t1", DeliveryState: "failed"},
		{ID: "failed", Kind: interaction.KindDelivery, UserID: "u1", MatchID: "m1", TraceID: "t1", DeliveryState: "failed"},
	})
	if report.Blockers != 1 {
		t.Fatalf("report = %+v", report)
	}
}
