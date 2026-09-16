package relationship

import (
	"testing"
)

// Contract lock (client side mirrored in client/lib/services/presentation_state.dart):
// every expression/motion/voice style the policy layer can emit through
// presentationFor must be accepted by the client whitelist, otherwise the
// client strictly rejects the whole PresentationPlan and the body stays frozen.
func TestPresentationForStaysInsideClientWhitelist(t *testing.T) {
	matchEvent := func(eventType string) Signal {
		return Signal{Kind: SignalMatchEvent, Match: &MatchSignal{
			EventID:       "evt-" + eventType,
			EventType:     eventType,
			OutputAllowed: true,
			Critical:      true,
		}}
	}
	userTurn := Signal{Kind: SignalUserTurn, User: &UserSignal{Text: "这球你怎么看"}}
	plans := map[string]PresentationPlan{
		"default_delivery":      presentationFor(AffectState{}, Signal{Kind: SignalDeliveryResult}, nil),
		"silence_kept_default":  presentationFor(AffectState{}, userTurn, []CommunicationAct{ActSilence}),
		"quiet_cue":             presentationFor(AffectState{}, Signal{Kind: SignalUserTurn, User: &UserSignal{Cues: []UserCue{{Kind: CueNeedsSilence}}}}, nil),
		"quiet_text":            presentationFor(AffectState{}, Signal{Kind: SignalUserTurn, User: &UserSignal{Text: "先别说话，缓会儿"}}, nil),
		"session_opened":        presentationFor(AffectState{}, Signal{Kind: SignalSessionOpened}, nil),
		"user_turn_default":     presentationFor(AffectState{}, userTurn, nil),
		"user_turn_analyze":     presentationFor(AffectState{}, userTurn, []CommunicationAct{ActAnalyze}),
		"user_turn_recall":      presentationFor(AffectState{}, userTurn, []CommunicationAct{ActRecall}),
		"user_turn_recall_tease": presentationFor(AffectState{}, userTurn, []CommunicationAct{ActRecall, ActTease}),
		"user_turn_disagree":    presentationFor(AffectState{}, userTurn, []CommunicationAct{ActDisagree}),
		"user_turn_tease":       presentationFor(AffectState{}, userTurn, []CommunicationAct{ActTease}),
		"event_goal":            presentationFor(AffectState{}, matchEvent("goal"), []CommunicationAct{ActReact}),
		"event_var_check":       presentationFor(AffectState{}, matchEvent("var_check"), []CommunicationAct{ActReact}),
		"event_goal_cancelled":  presentationFor(AffectState{}, matchEvent("goal_cancelled"), []CommunicationAct{ActReact}),
		"event_shot_missed":     presentationFor(AffectState{}, matchEvent("shot_missed"), []CommunicationAct{ActReact}),
		"event_unknown":         presentationFor(AffectState{}, matchEvent("yellow_card"), []CommunicationAct{ActReact}),
		"intent_unknown":        InterruptedDeliveryPresentation(AffectState{}),
	}
	for name, plan := range plans {
		if !ClientAcceptsExpression(plan.Expression) {
			t.Errorf("%s: expression %q is outside the client whitelist", name, plan.Expression)
		}
		if !ClientAcceptsMotion(plan.Motion) {
			t.Errorf("%s: motion %q is outside the client whitelist", name, plan.Motion)
		}
		if !ClientAcceptsVoiceStyle(plan.VoiceStyle) {
			t.Errorf("%s: voiceStyle %q is outside the client whitelist", name, plan.VoiceStyle)
		}
		if plan.ReturnMode != "decay_to_focus" && plan.ReturnMode != "decay_to_listening" && plan.ReturnMode != "decay_to_idle" && plan.ReturnMode != "watching" {
			t.Errorf("%s: returnMode %q is outside the client whitelist", name, plan.ReturnMode)
		}
	}
}

// Pins the C4 policy-state → motion mappings so regressions surface here
// instead of as a frozen avatar in the UI. Each case cites its policy trigger.
func TestPresentationForEmitsNewMotionGroups(t *testing.T) {
	matchEvent := func(eventType string) Signal {
		return Signal{Kind: SignalMatchEvent, Match: &MatchSignal{EventType: eventType, OutputAllowed: true}}
	}
	cases := []struct {
		name           string
		plan           PresentationPlan
		wantExpression string
		wantMotion     string
	}{{
		// policy.go: "explicit_analysis_request" (isTacticalQuestion).
		name:           "tactical question",
		plan:           presentationFor(AffectState{}, Signal{Kind: SignalUserTurn, User: &UserSignal{Text: "为什么右路回收了"}}, []CommunicationAct{ActAnalyze}),
		wantExpression: "thinking",
		wantMotion:     "analysis",
	}, {
		// policy.go: "shared_moment_recalled".
		name:           "shared moment recalled",
		plan:           presentationFor(AffectState{}, Signal{Kind: SignalUserTurn, User: &UserSignal{Text: "还记得上次那个球"}}, []CommunicationAct{ActRecall}),
		wantExpression: "happy",
		wantMotion:     "agree",
	}, {
		// policy.go: "stable_opinion_disagreement".
		name:           "opinion disagreement",
		plan:           presentationFor(AffectState{}, Signal{Kind: SignalUserTurn, User: &UserSignal{Text: "你说得不对吧"}}, []CommunicationAct{ActDisagree}),
		wantExpression: "nervous",
		wantMotion:     "complain",
	}, {
		// updateAffect "goal" — celebration targets the celebrate group.
		name:           "goal",
		plan:           presentationFor(AffectState{}, matchEvent("goal"), []CommunicationAct{ActReact}),
		wantExpression: "excited",
		wantMotion:     "celebrate",
	}, {
		// updateAffect "var_check" — the anxious wait during the review.
		name:           "var check",
		plan:           presentationFor(AffectState{}, matchEvent("var_check"), []CommunicationAct{ActReact}),
		wantExpression: "tense",
		wantMotion:     "tense",
	}, {
		// Trigger: updateAffect "goal_cancelled" — VAR-overturn startle plus
		// the referee complaint (presentation-map.json events.goal_cancelled).
		name:           "goal cancelled",
		plan:           presentationFor(AffectState{}, matchEvent("goal_cancelled"), []CommunicationAct{ActReact}),
		wantExpression: "surprised",
		wantMotion:     "complain",
	}, {
		// updateAffect "shot_missed" — near-miss gesture.
		name:           "shot missed",
		plan:           presentationFor(AffectState{}, matchEvent("shot_missed"), []CommunicationAct{ActReact}),
		wantExpression: "sad",
		wantMotion:     "miss",
	}}
	for _, tc := range cases {
		if tc.plan.Expression != tc.wantExpression || tc.plan.Motion != tc.wantMotion {
			t.Errorf("%s: presentation = (%q, %q), want (%q, %q)", tc.name, tc.plan.Expression, tc.plan.Motion, tc.wantExpression, tc.wantMotion)
		}
	}
}

// Legacy backend vocabulary must keep resolving through the client alias
// table so already-shipped clients accept older messages.
func TestClientAliasMirrorMatchesLegacyVocabulary(t *testing.T) {
	for _, expression := range []string{"low", "tense", "deflated"} {
		if !ClientAcceptsExpression(expression) {
			t.Errorf("legacy expression %q no longer accepted through aliases", expression)
		}
	}
	if NormalizeClientExpression("low") != "sad" || NormalizeClientExpression("tense") != "nervous" || NormalizeClientExpression("deflated") != "sad" {
		t.Fatalf("expression alias table drifted from the client")
	}
	for _, motion := range []string{"hold", "settle", "slump", "nod", "cheer", "listening", "confused"} {
		if !ClientAcceptsMotion(motion) {
			t.Errorf("legacy motion %q no longer accepted through aliases", motion)
		}
	}
	if NormalizeClientMotion("hold") != "focus" || NormalizeClientMotion("settle") != "idle" || NormalizeClientMotion("slump") != "idle" || NormalizeClientMotion("nod") != "agree" || NormalizeClientMotion("listening") != "listen" || NormalizeClientMotion("confused") != "idle" {
		t.Fatalf("motion alias table drifted from the client")
	}
	// Unknown names must stay rejected (strict-reject contract).
	if ClientAcceptsExpression("javascript:alert") || ClientAcceptsMotion("eval") {
		t.Fatalf("whitelist accepted an unknown name")
	}
}
