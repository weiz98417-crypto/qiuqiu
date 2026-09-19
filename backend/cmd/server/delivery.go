package main

import (
	"context"
	"log"
	"sync"
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

// voiceWaitPresentation marks a conversation reply presentation with the
// decay_to_listening ReturnMode (ADR-0007 ownership rules): at the
// reply-delivery completion point the voice session waits for the user's next
// utterance, so the held body decays to the listening pose
// (listening/listen_01) instead of the watching focus. The plan rides inside
// the reply's own qiuqiu_reply presentation — the client applies the return
// state when the HoldMS hold elapses — so the emission is wired by stamping
// the plan on the way out. Deliberate modes pass through untouched
// (decay_to_idle quiet stretches stay on the idle tier).
func voiceWaitPresentation(plan relationship.PresentationPlan) relationship.PresentationPlan {
	switch plan.ReturnMode {
	case "", "watching", "decay_to_focus":
		plan.ReturnMode = "decay_to_listening"
	}
	return plan
}

// presentationSink is the WS send the delivery reaction needs (satisfied by
// wsWriter; faked in tests).
type presentationSink interface {
	SendJSON(msg interface{}) error
}

// deliveryInterruption is one surfaced interrupted-reaction record for the
// console (ADR-0008: delivery interruptions are operator-visible).
type deliveryInterruption struct {
	TraceID string    `json:"traceId"`
	MatchID string    `json:"matchId"`
	At      time.Time `json:"at"`
}

// interruptionRing is the process-wide record of the last delivery
// interruptions (C3 delivery reaction, surfaced by
// GET /api/console/delivery-interruptions). Per-connection guards all feed
// the shared ring below.
type interruptionRing struct {
	mu    sync.Mutex
	items []deliveryInterruption
}

// interruptionRingCapacity bounds the ring (ADR-0008: last 20).
const interruptionRingCapacity = 20

// sharedInterruptions collects every interrupted-reaction emission across
// connections so the console route can surface them in one place.
var sharedInterruptions = newInterruptionRing()

func newInterruptionRing() *interruptionRing {
	return &interruptionRing{}
}

// Record appends one interruption, dropping the oldest beyond the capacity.
func (r *interruptionRing) Record(traceID, matchID string, at time.Time) {
	if r == nil || traceID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items = append(r.items, deliveryInterruption{TraceID: traceID, MatchID: matchID, At: at})
	if len(r.items) > interruptionRingCapacity {
		r.items = r.items[len(r.items)-interruptionRingCapacity:]
	}
}

// Recent returns the ring, newest first.
func (r *interruptionRing) Recent() []deliveryInterruption {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	recent := make([]deliveryInterruption, 0, len(r.items))
	for index := len(r.items) - 1; index >= 0; index-- {
		recent = append(recent, r.items[index])
	}
	return recent
}

// interruptedReactionGuard fires the delivery reaction of ADR-0007 rule 5
// once per interruption: scheduler user-preempts during playback mark the
// preempted reply's delivery "interrupted" (observeReplyOutcome below), and
// the one-shot confused/listening body goes out as a standalone presentation
// message with the same shape as the match_reaction presentation — the reply
// text of the preempting turn is never blocked or replaced. Every emission is
// also recorded into the console's interruption ring (ADR-0008).
type interruptedReactionGuard struct {
	mu      sync.Mutex
	emitted map[string]struct{}
	ring    *interruptionRing
}

func newInterruptedReactionGuard(ring *interruptionRing) *interruptedReactionGuard {
	if ring == nil {
		ring = sharedInterruptions
	}
	return &interruptedReactionGuard{emitted: make(map[string]struct{}), ring: ring}
}

// interruptedReactionDeliveryKey namespaces the standalone presentation's
// dedupe key (client-side dedupe only guards match_reaction sources, so the
// once-per-interruption rule is enforced here).
func interruptedReactionDeliveryKey(traceID string) string {
	return "delivery-interrupted:" + traceID
}

// interruptedReactionMessage builds the standalone presentation payload —
// the match_reaction shape ("type": "presentation" + source/deliveryKey).
func interruptedReactionMessage(trace companion.Trace, affect relationship.AffectState) map[string]interface{} {
	return map[string]interface{}{
		"type":        "presentation",
		"data":        relationship.InterruptedDeliveryPresentation(affect),
		"deliveryKey": interruptedReactionDeliveryKey(trace.ID),
		"source":      "delivery_interrupted",
	}
}

// emit sends the reaction if this trace's interruption has not fired one
// yet. Send failures are logged by wsWriter and never bubble into the
// preempting turn. The emission is recorded into the console ring.
func (guard *interruptedReactionGuard) emit(sink presentationSink, trace companion.Trace, affect relationship.AffectState) {
	if guard == nil || sink == nil || trace.ID == "" {
		return
	}
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if _, done := guard.emitted[trace.ID]; done {
		return
	}
	guard.emitted[trace.ID] = struct{}{}
	if err := sink.SendJSON(interruptedReactionMessage(trace, affect)); err != nil {
		log.Printf("interrupted delivery reaction error: %v", err)
	}
	guard.ring.Record(trace.ID, trace.MatchID, time.Now().UTC())
}

// observeReplyOutcome is the delivery observer's outcome path: the ordinary
// observation runs for every state, and the interrupted outcome (scheduler
// user-preempt during playback) additionally fires the one-shot delivery
// reaction. Skipped/failed stay text-fallback only (no reaction) in v1.
func observeReplyOutcome(ctx context.Context, agent *companion.Agent, sink presentationSink, guard *interruptedReactionGuard, trace companion.Trace, userID, matchID, state string, now time.Time) error {
	if state == "interrupted" {
		var affect relationship.AffectState
		if trace.RelationshipDecision != nil {
			affect = trace.RelationshipDecision.Presentation.Affect
		}
		guard.emit(sink, trace, affect)
	}
	return observeReplyDelivery(ctx, agent, trace, userID, matchID, state, now)
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

// deliverSessionOpeningPresentation fixes the discarded hello
// (presentation-mapping task 1.5): the plain session_opened path used to
// compute the relationship presentation and throw it away (`_, err :=`). It
// reuses the exact delivery shape the FirstMeetingCoordinator uses for its
// qiuqiu_reply presentation — the shared websocketResponseSink DeliverReply
// event carrying a PresentationPlan — minus the text and TTS, since a session
// opening has no reply body.
func deliverSessionOpeningPresentation(writer *wsWriter, decision relationship.Decision) {
	if writer == nil || decision.Presentation.Expression == "" {
		return
	}
	sink := websocketResponseSink{writer: writer}
	if err := sink.DeliverReply(context.Background(), conversation.ReplyDelivery{
		Text:         "",
		TraceID:      decision.ID,
		Source:       "session_open",
		DeliveryKey:  decision.ID,
		Presentation: decision.Presentation,
	}); err != nil {
		log.Printf("session opening presentation delivery error: %v", err)
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
