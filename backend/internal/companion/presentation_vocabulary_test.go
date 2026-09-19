package companion

import (
	"testing"

	"qiuqiu/internal/observation"
	"qiuqiu/internal/relationship"
)

// Contract lock: every presentation the companion attaches to observation
// resolutions and schedule lookups must stay inside the client whitelist
// (client/lib/services/presentation_state.dart), otherwise the client strictly
// rejects the plan and the body stays frozen.
func TestCompanionPresentationsStayInsideClientWhitelist(t *testing.T) {
	plans := map[string]relationship.PresentationPlan{
		"observation_confirmed":     observationPresentation(observation.StatusConfirmed),
		"observation_contradicted":  observationPresentation(observation.StatusContradicted),
		"observation_corroborating": observationPresentation(observation.StatusCorroborating),
		"schedule_lookup":           scheduleLookupPresentation(),
	}
	for name, plan := range plans {
		if !relationship.ClientAcceptsExpression(plan.Expression) {
			t.Errorf("%s: expression %q is outside the client whitelist", name, plan.Expression)
		}
		if !relationship.ClientAcceptsMotion(plan.Motion) {
			t.Errorf("%s: motion %q is outside the client whitelist", name, plan.Motion)
		}
		if !relationship.ClientAcceptsVoiceStyle(plan.VoiceStyle) {
			t.Errorf("%s: voiceStyle %q is outside the client whitelist", name, plan.VoiceStyle)
		}
	}
}

// Pins the C4 observation → motion mappings: a confirmed observation
// celebrates with the first-class celebrate group, a contradicted one plays
// the near-miss gesture.
func TestObservationPresentationEmitsNewMotionGroups(t *testing.T) {
	confirmed := observationPresentation(observation.StatusConfirmed)
	if confirmed.Expression != "excited" || confirmed.Motion != "celebrate" {
		t.Fatalf("confirmed = (%q, %q), want (excited, celebrate)", confirmed.Expression, confirmed.Motion)
	}
	contradicted := observationPresentation(observation.StatusContradicted)
	if contradicted.Expression != "deflated" || contradicted.Motion != "miss" {
		t.Fatalf("contradicted = (%q, %q), want (deflated, miss)", contradicted.Expression, contradicted.Motion)
	}
}
