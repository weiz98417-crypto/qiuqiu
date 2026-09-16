package companion

import (
	"context"
	"strings"
	"testing"

	"qiuqiu/internal/matchstate"
	"qiuqiu/internal/relationship"
)

// presentation-mapping task 1.8 (design contract-test #4): an IntentUnknown
// user turn keeps its deterministic clarification text but rides a one-shot
// confused/listening presentation — the confused expression and the
// listen-group motion of presentation-map.json delivery.interrupted.
func TestIntentUnknownTurnRidesConfusedListeningOneShot(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "unknown-reaction-1",
		MatchID:  "unknown-reaction",
		UserID:   "user-1",
		Text:     "请你分析一下今天球场草皮对传控节奏的隐藏影响",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent != IntentUnknown {
		t.Fatalf("intent = %q, want %q", response.Intent, IntentUnknown)
	}
	if !strings.Contains(response.Reply, "没接明白") {
		t.Fatalf("deterministic reply was replaced: %q", response.Reply)
	}
	if response.Presentation.Expression != "confused" || response.Presentation.Motion != "listening" {
		t.Fatalf("presentation = (%q, %q), want (confused, listening)", response.Presentation.Expression, response.Presentation.Motion)
	}
	if response.Presentation.HoldMS <= 0 {
		t.Fatalf("one-shot hold missing: %+v", response.Presentation)
	}
	if !relationship.ClientAcceptsExpression(response.Presentation.Expression) || !relationship.ClientAcceptsMotion(response.Presentation.Motion) {
		t.Fatalf("interrupted presentation outside the client whitelist: %+v", response.Presentation)
	}
}

// A known-intent turn keeps the director's own presentation — the one-shot
// reaction is exclusive to IntentUnknown turns.
func TestKnownIntentTurnKeepsDirectorPresentation(t *testing.T) {
	agent := NewAgent(NewStoreMemoryTools(matchstate.NewStore())).WithDirector(
		relationship.NewDirector(relationship.NewMemoryRepository()),
	)
	response, err := agent.HandleMessage(context.Background(), MessageRequest{
		SignalID: "known-reaction-1",
		MatchID:  "known-reaction",
		UserID:   "user-1",
		Text:     "在吗？",
	})
	if err != nil {
		t.Fatalf("HandleMessage: %v", err)
	}
	if response.Intent == IntentUnknown {
		t.Fatalf("intent = %q, want a known intent", response.Intent)
	}
	if response.Presentation.Expression == "confused" || response.Presentation.Motion == "listening" {
		t.Fatalf("known-intent turn rode the interrupted reaction: %+v", response.Presentation)
	}
}
