package main

import (
	"context"
	"log"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/conversation"
	"qiuqiu/internal/relationship"
)

type pendingReplyDelivery struct {
	Trace   companion.Trace
	UserID  string
	MatchID string
}

type replyDeliveryTracker struct {
	core *conversation.DeliveryTracker
}

func newReplyDeliveryTracker() *replyDeliveryTracker {
	return &replyDeliveryTracker{core: conversation.NewDeliveryTracker(nil)}
}

func newReplyDeliveryTrackerWithLedger(ledger conversation.DeliveryLedger) *replyDeliveryTracker {
	if ledger == nil {
		return newReplyDeliveryTracker()
	}
	return &replyDeliveryTracker{core: conversation.NewDeliveryTracker(ledger)}
}

func (tracker *replyDeliveryTracker) Ledger() conversation.DeliveryLedger {
	if tracker == nil {
		return nil
	}
	return tracker.core.Ledger()
}

func (tracker *replyDeliveryTracker) BindLedger(ledger conversation.DeliveryLedger) {
	if tracker == nil {
		return
	}
	if ledger != nil {
		tracker.core.BindLedger(ledger)
	}
}

func (tracker *replyDeliveryTracker) Track(trace companion.Trace, userID, matchID string) {
	_ = tracker.TrackWithPolicy(trace, userID, matchID, trace.ID, false, 30*time.Second)
}

func (tracker *replyDeliveryTracker) TrackWithPolicy(trace companion.Trace, userID, matchID, deliveryKey string, critical bool, ttl time.Duration) error {
	if tracker == nil || trace.ID == "" {
		return conversation.ErrDeliveryConflict
	}
	now := time.Now().UTC()
	var pending any
	if trace.RelationshipDecision == nil || !hasOpenThreadMemory(trace.RelationshipDecision.Memories) {
		pending = nil
	} else {
		pending = pendingReplyDelivery{Trace: trace, UserID: userID, MatchID: matchID}
	}
	return tracker.core.Plan(conversation.DeliveryRecord{Key: trace.ID, DeliveryKey: deliveryKey, TraceID: trace.ID, MatchID: matchID, UserID: userID, State: conversation.DeliveryPlanned, Critical: critical, ExpiresAt: now.Add(ttl), UpdatedAt: now}, pending)
}

func (tracker *replyDeliveryTracker) Transition(traceID string, state conversation.DeliveryState, at time.Time) error {
	if tracker == nil || tracker.core == nil || traceID == "" {
		return conversation.ErrDeliveryConflict
	}
	if err := tracker.core.Transition(traceID, state, at); err != nil {
		log.Printf("delivery transition %q -> %q: %v", traceID, state, err)
		return err
	}
	return nil
}

func (tracker *replyDeliveryTracker) Lookup(traceID string) (conversation.DeliveryRecord, bool) {
	if tracker == nil || tracker.core == nil || tracker.core.Ledger() == nil {
		return conversation.DeliveryRecord{}, false
	}
	return tracker.core.Ledger().Get(traceID)
}

func (tracker *replyDeliveryTracker) LookupByDeliveryKey(deliveryKey string) (conversation.DeliveryRecord, bool) {
	if tracker == nil || tracker.core == nil || tracker.core.Ledger() == nil {
		return conversation.DeliveryRecord{}, false
	}
	return tracker.core.Ledger().FindByDeliveryKey(deliveryKey)
}

func (tracker *replyDeliveryTracker) Remove(traceID string) {
	if tracker == nil || traceID == "" {
		return
	}
	tracker.core.Remove(traceID)
}

func (tracker *replyDeliveryTracker) Drain() []pendingReplyDelivery {
	if tracker == nil {
		return nil
	}
	values := tracker.core.Drain()
	pending := make([]pendingReplyDelivery, 0, len(values))
	for _, value := range values {
		if delivery, ok := value.(pendingReplyDelivery); ok {
			pending = append(pending, delivery)
		}
	}
	return pending
}

func observeReplyDelivery(ctx context.Context, agent *companion.Agent, trace companion.Trace, userID, matchID, state string, now time.Time) error {
	if agent == nil || trace.RelationshipDecision == nil || trace.RelationshipDecision.ID == "" {
		return nil
	}
	_, err := agent.Plan(ctx, companion.TurnInput{Kind: companion.TurnKindDelivery, Delivery: &companion.DeliveryInput{
		SignalID: "delivery:" + trace.ID + ":text:" + state,
		TraceID:  trace.ID, UserID: userID, MatchID: matchID, DecisionID: trace.RelationshipDecision.ID,
		State: state, Purpose: "user_reply", UsedMemoryIDs: trace.RelationshipDecision.UsedMemoryIDs, Now: now,
	}})
	return err
}

type traceRecoverySource struct{ reader companion.TraceReader }

func (source traceRecoverySource) ResolveRecovery(ctx context.Context, matchID, traceID string) (conversation.RecoveryPayload, error) {
	trace, err := source.reader.GetTrace(ctx, matchID, traceID)
	if err != nil {
		return conversation.RecoveryPayload{}, err
	}
	presentation := relationship.PresentationPlan{}
	if trace.RelationshipDecision != nil {
		presentation = trace.RelationshipDecision.Presentation
	}
	return conversation.RecoveryPayload{
		TraceID:      trace.ID,
		UserID:       trace.UserID,
		MatchID:      trace.MatchID,
		Text:         trace.Output,
		Presentation: presentation,
	}, nil
}

func recoverPendingDeliveries(ctx context.Context, writer *wsWriter, session *conversation.WatchSession, reader companion.TraceReader, userID, matchID string) {
	if session == nil || reader == nil || writer == nil {
		return
	}
	recoveries, err := session.Recoveries(ctx, traceRecoverySource{reader: reader}, time.Now().UTC())
	if err != nil {
		return
	}
	for _, recovery := range recoveries {
		if recovery.UserID != userID || recovery.MatchID != matchID {
			continue
		}
		_ = writer.SendJSON(map[string]interface{}{
			"type": "event", "event": "qiuqiu_reply",
			"data": qiuqiuReplyData(recovery.Text, recovery.TraceID, "recovered_delivery", "", fallbackString(recovery.DeliveryKey, recovery.TraceID), recovery.Presentation),
		})
	}
}

func hasOpenThreadMemory(memories []relationship.RelationshipMemory) bool {
	for _, memory := range memories {
		if memory.Kind == relationship.MemoryKindOpenThread {
			return true
		}
	}
	return false
}
