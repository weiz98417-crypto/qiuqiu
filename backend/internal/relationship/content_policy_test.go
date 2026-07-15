package relationship

import (
	"testing"
	"time"
)

func TestContentPolicyScopesProfanityByStageIntensityAndRepair(t *testing.T) {
	tests := []struct {
		name      string
		stage     RelationshipStage
		arousal   float64
		repair    bool
		wantLevel string
	}{
		{name: "first meeting stays clean", stage: StageFirstMeeting, arousal: 1, wantLevel: "none"},
		{name: "familiar low intensity stays clean", stage: StageFamiliar, arousal: 0.4, wantLevel: "none"},
		{name: "familiar high intensity allows mild", stage: StageFamiliar, arousal: 0.8, wantLevel: "mild_non_directed"},
		{name: "watch buddy extreme intensity allows strong", stage: StageWatchBuddy, arousal: 0.95, wantLevel: "strong_non_directed"},
		{name: "repair overrides familiarity and intensity", stage: StageOldBallmate, arousal: 1, repair: true, wantLevel: "none"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := contentPolicyFor(
				Signal{},
				[]CommunicationAct{ActAcknowledge},
				RelationshipState{
					Stage:       test.stage,
					Preferences: RelationshipPreferences{ProfanityEnabled: true},
					Repair:      RepairState{Active: test.repair},
				},
				MatchCompanionState{Affect: AffectState{Arousal: test.arousal}},
			)

			if policy.ProfanityLevel != test.wantLevel {
				t.Fatalf("profanity level = %q, want %q", policy.ProfanityLevel, test.wantLevel)
			}
		})
	}
}

func TestStablePreferenceAllowsOneValuableQuestionPerFiveTurns(t *testing.T) {
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	state := StateBundle{
		Relationship: RelationshipState{Stage: StageFamiliar},
		Match:        MatchCompanionState{},
	}
	signal := Signal{
		ID:         "preference-1",
		Kind:       SignalUserTurn,
		OccurredAt: now,
		User:       &UserSignal{Text: "我支持利物浦"},
		Grounding:  GroundedContent{Intent: "smalltalk"},
	}

	actions, _ := applyPolicy(&state, signal, now)
	if !hasAction(actions, ActAsk) {
		t.Fatalf("actions = %v, want valuable ask", actions)
	}
	policy := contentPolicyFor(signal, actions, state.Relationship, state.Match)
	if !policy.QuestionAllowed {
		t.Fatal("question should be allowed when ask is planned")
	}
	recordActions(&state.Match, signal.ID, actions, now)

	signal.ID = "preference-2"
	actions, _ = applyPolicy(&state, signal, now.Add(time.Minute))
	if hasAction(actions, ActAsk) {
		t.Fatalf("actions = %v, ask should be cooling down", actions)
	}
}

func TestProfanityFeedbackDisablesFutureProfanity(t *testing.T) {
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	state := StateBundle{
		Relationship: RelationshipState{
			Stage:       StageOldBallmate,
			Preferences: RelationshipPreferences{ProfanityEnabled: true},
		},
		Match: MatchCompanionState{Affect: AffectState{Arousal: 1}},
	}
	actions, reasons := applyPolicy(&state, Signal{
		ID:         "profanity-boundary-1",
		Kind:       SignalUserTurn,
		OccurredAt: now,
		User:       &UserSignal{Text: "别说卧槽了，我不喜欢你说脏话"},
	}, now)

	if state.Relationship.Preferences.ProfanityEnabled {
		t.Fatal("profanity preference remained enabled")
	}
	if !hasBoundary(state.Relationship.Boundaries, "profanity", "disabled") {
		t.Fatalf("boundaries = %+v", state.Relationship.Boundaries)
	}
	if len(actions) != 1 || actions[0] != ActAcknowledge || len(reasons) != 1 || reasons[0] != "profanity_boundary_recorded" {
		t.Fatalf("actions=%v reasons=%v", actions, reasons)
	}
	policy := contentPolicyFor(Signal{}, actions, state.Relationship, state.Match)
	if policy.ProfanityLevel != "none" {
		t.Fatalf("profanity level = %q after opt-out", policy.ProfanityLevel)
	}
}
