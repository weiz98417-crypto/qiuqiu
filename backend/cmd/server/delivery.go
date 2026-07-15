package main

import (
	"context"
	"sync"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/relationship"
)

type pendingReplyDelivery struct {
	Trace   companion.Trace
	UserID  string
	MatchID string
}

type replyDeliveryTracker struct {
	mu      sync.Mutex
	pending map[string]pendingReplyDelivery
}

func newReplyDeliveryTracker() *replyDeliveryTracker {
	return &replyDeliveryTracker{pending: make(map[string]pendingReplyDelivery)}
}

func (tracker *replyDeliveryTracker) Track(trace companion.Trace, userID, matchID string) {
	if tracker == nil || trace.ID == "" || trace.RelationshipDecision == nil || !hasOpenThreadMemory(trace.RelationshipDecision.Memories) {
		return
	}
	tracker.mu.Lock()
	tracker.pending[trace.ID] = pendingReplyDelivery{Trace: trace, UserID: userID, MatchID: matchID}
	tracker.mu.Unlock()
}

func (tracker *replyDeliveryTracker) Remove(traceID string) {
	if tracker == nil || traceID == "" {
		return
	}
	tracker.mu.Lock()
	delete(tracker.pending, traceID)
	tracker.mu.Unlock()
}

func (tracker *replyDeliveryTracker) Drain() []pendingReplyDelivery {
	if tracker == nil {
		return nil
	}
	tracker.mu.Lock()
	pending := make([]pendingReplyDelivery, 0, len(tracker.pending))
	for _, delivery := range tracker.pending {
		pending = append(pending, delivery)
	}
	tracker.pending = make(map[string]pendingReplyDelivery)
	tracker.mu.Unlock()
	return pending
}

func observeReplyDelivery(ctx context.Context, agent *companion.Agent, trace companion.Trace, userID, matchID, state string, now time.Time) error {
	if agent == nil || trace.RelationshipDecision == nil || trace.RelationshipDecision.ID == "" {
		return nil
	}
	_, err := agent.ObserveDelivery(
		ctx,
		"delivery:"+trace.ID+":text:"+state,
		userID,
		matchID,
		trace.RelationshipDecision.ID,
		state,
		"user_reply",
		trace.RelationshipDecision.UsedMemoryIDs,
		now,
	)
	return err
}

func hasOpenThreadMemory(memories []relationship.RelationshipMemory) bool {
	for _, memory := range memories {
		if memory.Kind == relationship.MemoryKindOpenThread {
			return true
		}
	}
	return false
}
