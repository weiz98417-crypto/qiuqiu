package main

import (
	"encoding/json"
	"testing"

	"qiuqiu/internal/conversation"
	"qiuqiu/internal/relationship"
)

func TestQiuqiuReplyDataCarriesDirectorPresentation(t *testing.T) {
	data := qiuqiuReplyData(
		"……还是越了。白喊。",
		"trace-1",
		"match_reaction",
		"event-1",
		relationship.PresentationPlan{
			Expression:  "deflated",
			Motion:      "settle",
			VoiceStyle:  "low_disappointed",
			VoiceEnergy: 0.35,
			VoiceSpeed:  0.92,
			HoldMS:      2800,
			ReturnMode:  "decay_to_focus",
		},
	)
	payload, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	presentation, ok := decoded["presentation"].(map[string]any)
	if !ok {
		t.Fatalf("presentation missing: %s", payload)
	}
	if presentation["expression"] != "deflated" || presentation["motion"] != "settle" || presentation["returnMode"] != "decay_to_focus" {
		t.Fatalf("presentation = %+v", presentation)
	}
	if decoded["source"] != "match_reaction" || decoded["eventId"] != "event-1" {
		t.Fatalf("client source contract = %+v", decoded)
	}
	for _, internalKey := range []string{"decisionId", "operatorId", "trace"} {
		if _, exists := decoded[internalKey]; exists {
			t.Fatalf("internal key %q leaked in payload: %+v", internalKey, decoded)
		}
	}
}

func TestProactiveUrgencyTreatsCriticalFactRevisionsAsCritical(t *testing.T) {
	for _, eventType := range []string{"goal", "penalty_awarded", "red_card", "var_result", "goal_cancelled", "match_end"} {
		if got := proactiveUrgency(eventType); got != conversation.UrgencyCritical {
			t.Fatalf("event %q urgency = %q, want critical", eventType, got)
		}
	}
}
