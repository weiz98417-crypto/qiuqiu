package main

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"qiuqiu/internal/companion"
	"qiuqiu/internal/relationship"
)

// recordingPresentationSink captures the standalone presentation messages the
// delivery observer sends (stands in for wsWriter).
type recordingPresentationSink struct {
	mu       sync.Mutex
	messages []map[string]interface{}
}

func (sink *recordingPresentationSink) SendJSON(msg interface{}) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	sink.messages = append(sink.messages, decoded)
	return nil
}

func (sink *recordingPresentationSink) count() int {
	sink.mu.Lock()
	defer sink.mu.Unlock()
	return len(sink.messages)
}

func TestInterruptedOutcomeEmitsConfusedListeningReaction(t *testing.T) {
	sink := &recordingPresentationSink{}
	guard := newInterruptedReactionGuard(nil)
	trace := companion.Trace{ID: "trace-1", UserID: "user-1", MatchID: "match-1"}
	now := time.Date(2026, 9, 16, 20, 0, 0, 0, time.UTC)

	if err := observeReplyOutcome(context.Background(), nil, sink, guard, trace, "user-1", "match-1", "interrupted", now); err != nil {
		t.Fatalf("observe interrupted outcome: %v", err)
	}
	if sink.count() != 1 {
		t.Fatalf("emitted %d messages, want exactly the delivery reaction", sink.count())
	}
	message := sink.messages[0]
	if message["type"] != "presentation" || message["source"] != "delivery_interrupted" {
		t.Fatalf("message = %+v, want standalone presentation from delivery_interrupted", message)
	}
	if message["deliveryKey"] != interruptedReactionDeliveryKey("trace-1") {
		t.Fatalf("deliveryKey = %+v", message["deliveryKey"])
	}
	plan, ok := message["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("presentation data missing: %+v", message)
	}
	// presentation-map.json delivery.interrupted: the one-shot reaction body.
	if plan["expression"] != "confused" || plan["motion"] != "listening" {
		t.Fatalf("reaction plan = %+v, want confused/listening", plan)
	}
	if plan["returnMode"] != "decay_to_focus" {
		t.Fatalf("reaction plan = %+v, want decay_to_focus return", plan)
	}
}

func TestInterruptedReactionFiresOncePerInterruption(t *testing.T) {
	sink := &recordingPresentationSink{}
	guard := newInterruptedReactionGuard(nil)
	trace := companion.Trace{ID: "trace-1", UserID: "user-1", MatchID: "match-1"}

	guard.emit(sink, trace, relationship.AffectState{})
	guard.emit(sink, trace, relationship.AffectState{})
	if sink.count() != 1 {
		t.Fatalf("emitted %d messages for one interruption, want 1", sink.count())
	}

	// A different preempted turn is a new interruption and reacts again.
	other := trace
	other.ID = "trace-2"
	guard.emit(sink, other, relationship.AffectState{})
	if sink.count() != 2 {
		t.Fatalf("emitted %d messages after a second interruption, want 2", sink.count())
	}
}

func TestNonInterruptedOutcomesEmitNoReaction(t *testing.T) {
	sink := &recordingPresentationSink{}
	guard := newInterruptedReactionGuard(nil)
	trace := companion.Trace{ID: "trace-1", UserID: "user-1", MatchID: "match-1"}
	now := time.Now().UTC()

	for _, state := range []string{"delivered", "skipped", "failed", "played"} {
		if err := observeReplyOutcome(context.Background(), nil, sink, guard, trace, "user-1", "match-1", state, now); err != nil {
			t.Fatalf("observe %q outcome: %v", state, err)
		}
	}
	if sink.count() != 0 {
		t.Fatalf("emitted %d messages for non-interrupted outcomes, want none", sink.count())
	}
}

func TestVoiceWaitReplyPresentationDecaysToListening(t *testing.T) {
	// presentation-mapping 3.2: the conversation reply completes into a
	// voice-session wait for the user, so the generic return modes stamp
	// decay_to_listening onto the plan that rides with the reply.
	for _, generic := range []string{"", "watching", "decay_to_focus"} {
		plan := voiceWaitPresentation(relationship.PresentationPlan{ReturnMode: generic})
		if plan.ReturnMode != "decay_to_listening" {
			t.Fatalf("returnMode %q -> %q, want decay_to_listening", generic, plan.ReturnMode)
		}
	}
	// Deliberate quiet-stretch intents pass through untouched.
	plan := voiceWaitPresentation(relationship.PresentationPlan{ReturnMode: "decay_to_idle"})
	if plan.ReturnMode != "decay_to_idle" {
		t.Fatalf("decay_to_idle -> %q, want it preserved", plan.ReturnMode)
	}
}
