package observation

import (
	"context"
	"strings"
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestProvisionalFactOnlyCorroboratesUserObservation(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	pending, err := coordinator.Record(ctx, Input{
		SignalID:      "turn-1",
		TraceID:       "trace-1",
		UserID:        "user-1",
		MatchID:       "match-1",
		Kind:          "event",
		EventType:     "goal",
		ClaimedPlayer: "萨拉赫",
		ReceivedAt:    now,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}

	resolutions, err := coordinator.OnFactChanged(ctx, matchstate.MatchEvent{
		MatchID:      "match-1",
		ID:           "event-1",
		FactID:       "fact-1",
		FactRevision: 1,
		FactStatus:   matchstate.FactStatusProvisional,
		EventType:    "goal",
		PlayerName:   "萨拉赫",
		CreatedAt:    now.Add(8 * time.Second).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("OnFactChanged: %v", err)
	}
	if len(resolutions) != 0 {
		t.Fatalf("provisional fact produced deterministic resolution: %+v", resolutions)
	}
	updated, ok := coordinator.Get(pending.ID)
	if !ok || updated.Status != StatusCorroborating || updated.CandidateFactID != "fact-1" {
		t.Fatalf("updated observation = %+v, want corroborating candidate", updated)
	}
}

func TestConfirmedFactResolvesObservationWithStableDeliveryKey(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	pending, err := coordinator.Record(ctx, Input{
		SignalID:      "turn-2",
		TraceID:       "trace-2",
		UserID:        "user-1",
		MatchID:       "match-1",
		Kind:          "event",
		EventType:     "goal",
		ClaimedPlayer: "萨拉赫",
		ReceivedAt:    now,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	event := matchstate.MatchEvent{
		MatchID:      "match-1",
		ID:           "event-2",
		FactID:       "fact-2",
		FactRevision: 2,
		FactStatus:   matchstate.FactStatusConfirmed,
		EventType:    "goal",
		PlayerName:   "萨拉赫",
		TeamName:     "利物浦",
		Score:        matchstate.Score{Home: 1},
		CreatedAt:    now.Add(8 * time.Second).Format(time.RFC3339Nano),
	}

	first, err := coordinator.OnFactChanged(ctx, event)
	if err != nil {
		t.Fatalf("OnFactChanged: %v", err)
	}
	if len(first) != 1 || first[0].Status != StatusConfirmed {
		t.Fatalf("resolutions = %+v, want one confirmed resolution", first)
	}
	wantKey := pending.ID + ":2:confirmed"
	if first[0].DeliveryKey != wantKey || first[0].ReliableText != "跟上了，确实是萨拉赫进的。" {
		t.Fatalf("resolution = %+v, want key %q and grounded text", first[0], wantKey)
	}
	repeated, err := coordinator.OnFactChanged(ctx, event)
	if err != nil {
		t.Fatalf("repeated OnFactChanged: %v", err)
	}
	if len(repeated) != 1 || repeated[0].DeliveryKey != wantKey {
		t.Fatalf("repeated resolution = %+v, want same scheduler dedupe key", repeated)
	}
	updated, ok := coordinator.Get(pending.ID)
	if !ok || updated.Status != StatusConfirmed || updated.ResolvedFactID != "fact-2" || updated.ResolvedRevision != 2 {
		t.Fatalf("updated observation = %+v", updated)
	}
}

func TestRevokedGoalCorrectsPreviouslyConfirmedObservation(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	pending, err := coordinator.Record(ctx, Input{
		SignalID: "turn-3", UserID: "user-1", MatchID: "match-1",
		Kind: "event", EventType: "goal", ClaimedPlayer: "萨拉赫", ReceivedAt: now,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	confirmed := matchstate.MatchEvent{
		MatchID: "match-1", ID: "event-3", FactID: "fact-3", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal", PlayerName: "萨拉赫",
		CreatedAt: now.Add(6 * time.Second).Format(time.RFC3339Nano),
	}
	if _, err := coordinator.OnFactChanged(ctx, confirmed); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	revoked := confirmed
	revoked.FactRevision = 3
	revoked.FactStatus = matchstate.FactStatusRevoked
	revoked.UpdatedAt = now.Add(20 * time.Second).Format(time.RFC3339Nano)

	resolutions, err := coordinator.OnFactChanged(ctx, revoked)
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if len(resolutions) != 1 || resolutions[0].Status != StatusContradicted {
		t.Fatalf("revocation resolutions = %+v", resolutions)
	}
	wantKey := pending.ID + ":3:contradicted"
	if resolutions[0].DeliveryKey != wantKey || resolutions[0].ReliableText != "结果出来了，这球没算。刚才那一下是真把人骗到了。" {
		t.Fatalf("revocation resolution = %+v, want key %q", resolutions[0], wantKey)
	}
	if !resolutions[0].FollowUpDeadline.After(now.Add(20 * time.Second)) {
		t.Fatalf("revocation deadline = %s, want a fresh correction window", resolutions[0].FollowUpDeadline)
	}
}

func TestExpiredObservationStopsParticipatingInReconciliation(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	pending, err := coordinator.Record(ctx, Input{
		SignalID: "turn-expire", UserID: "user-1", MatchID: "match-1",
		Kind: "event", EventType: "goal", ReceivedAt: now,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	expired, err := coordinator.Expire(ctx, now.Add(61*time.Second))
	if err != nil {
		t.Fatalf("Expire: %v", err)
	}
	if len(expired) != 1 || expired[0].ObservationID != pending.ID || expired[0].Status != StatusExpired || expired[0].ReliableText != "" {
		t.Fatalf("expired resolutions = %+v", expired)
	}
	late := matchstate.MatchEvent{
		MatchID: "match-1", ID: "late-goal", FactID: "late-fact", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal",
		CreatedAt: now.Add(62 * time.Second).Format(time.RFC3339Nano),
	}
	resolutions, err := coordinator.OnFactChanged(ctx, late)
	if err != nil {
		t.Fatalf("late OnFactChanged: %v", err)
	}
	if len(resolutions) != 0 {
		t.Fatalf("expired observation matched late fact: %+v", resolutions)
	}
}

func TestRepeatedSemanticObservationIsMerged(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	first, err := coordinator.Record(ctx, Input{
		SignalID: "turn-repeat-1", UserID: "user-1", MatchID: "match-1",
		Kind: "event", EventType: "goal", ClaimedPlayer: "萨拉赫", ReceivedAt: now,
	})
	if err != nil {
		t.Fatalf("first Record: %v", err)
	}
	second, err := coordinator.Record(ctx, Input{
		SignalID: "turn-repeat-2", UserID: "user-1", MatchID: "match-1",
		Kind: "event", EventType: "goal", ClaimedPlayer: "萨拉赫", ReceivedAt: now.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatalf("second Record: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("semantic duplicate created %q, want existing %q", second.ID, first.ID)
	}
	resolutions, err := coordinator.OnFactChanged(ctx, matchstate.MatchEvent{
		MatchID: "match-1", ID: "goal-repeat", FactID: "fact-repeat", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal", PlayerName: "萨拉赫",
		CreatedAt: now.Add(8 * time.Second).Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("OnFactChanged: %v", err)
	}
	if len(resolutions) != 1 {
		t.Fatalf("duplicate observation produced %d resolutions: %+v", len(resolutions), resolutions)
	}
}

func TestMultipleCandidatesRemainConflictInsteadOfGuessing(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	pending, err := coordinator.Record(ctx, Input{
		SignalID: "turn-ambiguous", UserID: "user-1", MatchID: "match-1",
		Kind: "event_reference", EventType: "goal", ReceivedAt: now,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	first := matchstate.MatchEvent{
		MatchID: "match-1", ID: "goal-a", FactID: "fact-a", FactRevision: 1,
		FactStatus: matchstate.FactStatusProvisional, EventType: "goal",
		CreatedAt: now.Add(5 * time.Second).Format(time.RFC3339Nano),
	}
	second := first
	second.ID = "goal-b"
	second.FactID = "fact-b"
	second.CreatedAt = now.Add(7 * time.Second).Format(time.RFC3339Nano)
	if _, err := coordinator.OnFactChanged(ctx, first); err != nil {
		t.Fatalf("first candidate: %v", err)
	}
	if _, err := coordinator.OnFactChanged(ctx, second); err != nil {
		t.Fatalf("second candidate: %v", err)
	}
	conflicted, ok := coordinator.Get(pending.ID)
	if !ok || conflicted.Status != StatusConflict {
		t.Fatalf("observation = %+v, want conflict", conflicted)
	}
	first.FactStatus = matchstate.FactStatusConfirmed
	first.FactRevision = 2
	resolutions, err := coordinator.OnFactChanged(ctx, first)
	if err != nil {
		t.Fatalf("confirm ambiguous candidate: %v", err)
	}
	if len(resolutions) != 0 {
		t.Fatalf("ambiguous observation was auto-resolved: %+v", resolutions)
	}
}

func TestGenericPlayObservationIgnoresCardsAndUsesPlaySpecificReply(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	pending, err := coordinator.Record(ctx, Input{
		SignalID: "turn-play", UserID: "user-1", MatchID: "match-1",
		Kind: "event_reference", EventType: "play", ReceivedAt: now,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	card := matchstate.MatchEvent{
		MatchID: "match-1", ID: "card-1", FactID: "fact-card", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "yellow_card", PlayerName: "萨拉赫",
		CreatedAt: now.Add(4 * time.Second).Format(time.RFC3339Nano),
	}
	if resolutions, err := coordinator.OnFactChanged(ctx, card); err != nil || len(resolutions) != 0 {
		t.Fatalf("card resolutions = %+v err=%v, want none", resolutions, err)
	}
	if unchanged, ok := coordinator.Get(pending.ID); !ok || unchanged.Status != StatusPendingSync {
		t.Fatalf("card changed play observation: %+v", unchanged)
	}
	shot := card
	shot.ID = "shot-1"
	shot.FactID = "fact-shot"
	shot.EventType = "shot"
	shot.CreatedAt = now.Add(6 * time.Second).Format(time.RFC3339Nano)
	resolutions, err := coordinator.OnFactChanged(ctx, shot)
	if err != nil || len(resolutions) != 1 {
		t.Fatalf("shot resolutions = %+v err=%v", resolutions, err)
	}
	if !strings.Contains(resolutions[0].ReliableText, "射门") || strings.Contains(resolutions[0].ReliableText, "进的") {
		t.Fatalf("shot reply invented a goal: %q", resolutions[0].ReliableText)
	}
}

func TestConfirmedShotContradictsSpecificGoalObservation(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := coordinator.Record(ctx, Input{
		SignalID: "turn-goal-shot", UserID: "user-1", MatchID: "match-1",
		Kind: "event", EventType: "goal", ClaimedPlayer: "萨拉赫", ReceivedAt: now,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	resolutions, err := coordinator.OnFactChanged(ctx, matchstate.MatchEvent{
		MatchID: "match-1", ID: "shot-only", FactID: "fact-shot-only", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "shot", PlayerName: "萨拉赫",
		CreatedAt: now.Add(8 * time.Second).Format(time.RFC3339Nano),
	})
	if err != nil || len(resolutions) != 1 || resolutions[0].Status != StatusContradicted {
		t.Fatalf("shot contradiction = %+v err=%v", resolutions, err)
	}
	if !strings.Contains(resolutions[0].ReliableText, "没算") {
		t.Fatalf("contradiction reply = %q", resolutions[0].ReliableText)
	}
}

func TestCustomReconcileWindowKeepsDelayedSourceObservationActive(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	if _, err := coordinator.Record(ctx, Input{
		SignalID: "turn-delayed", UserID: "user-1", MatchID: "match-1",
		Kind: "event", EventType: "goal", ClaimedPlayer: "萨拉赫", ReceivedAt: now,
		ReconcileWindow: 2 * time.Minute,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	resolutions, err := coordinator.OnFactChanged(ctx, matchstate.MatchEvent{
		MatchID: "match-1", ID: "late-goal", FactID: "late-fact", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal", PlayerName: "萨拉赫",
		CreatedAt: now.Add(90 * time.Second).Format(time.RFC3339Nano),
	})
	if err != nil || len(resolutions) != 1 || resolutions[0].Status != StatusConfirmed {
		t.Fatalf("delayed resolution = %+v err=%v", resolutions, err)
	}
}

func TestSuppressFollowUpRemovesRecoverableResolution(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	pending, err := coordinator.Record(ctx, Input{
		SignalID: "turn-in-band", UserID: "user-1", MatchID: "match-1",
		Kind: "event", EventType: "goal", ClaimedPlayer: "萨拉赫", ReceivedAt: now,
	})
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if _, err := coordinator.OnFactChanged(ctx, matchstate.MatchEvent{
		MatchID: "match-1", ID: "goal-in-band", FactID: "fact-in-band", FactRevision: 2,
		FactStatus: matchstate.FactStatusConfirmed, EventType: "goal", PlayerName: "萨拉赫",
		CreatedAt: now.Add(5 * time.Second).Format(time.RFC3339Nano),
	}); err != nil {
		t.Fatalf("OnFactChanged: %v", err)
	}
	if err := coordinator.SuppressFollowUp(ctx, pending.ID, now.Add(6*time.Second)); err != nil {
		t.Fatalf("SuppressFollowUp: %v", err)
	}
	resolutions, err := coordinator.PendingResolutions(ctx, "user-1", "match-1", now.Add(7*time.Second))
	if err != nil || len(resolutions) != 0 {
		t.Fatalf("suppressed resolution recovered: %+v err=%v", resolutions, err)
	}
}

func TestSixthPendingObservationSupersedesOldest(t *testing.T) {
	ctx := context.Background()
	coordinator := NewMemoryCoordinator()
	now := time.Date(2026, 7, 17, 12, 0, 0, 0, time.UTC)
	var first PendingObservation
	for index, player := range []string{"A", "B", "C", "D", "E", "F"} {
		created, err := coordinator.Record(ctx, Input{
			SignalID: "turn-limit-" + player, UserID: "user-1", MatchID: "match-1",
			Kind: "event", EventType: "goal", ClaimedPlayer: player,
			ReceivedAt: now.Add(time.Duration(index) * time.Second),
		})
		if err != nil {
			t.Fatalf("Record %s: %v", player, err)
		}
		if index == 0 {
			first = created
		}
	}
	superseded, ok := coordinator.Get(first.ID)
	if !ok || superseded.Status != StatusSuperseded || superseded.ResolutionReason != "pending observation limit exceeded" {
		t.Fatalf("oldest observation = %+v, want superseded", superseded)
	}
}
