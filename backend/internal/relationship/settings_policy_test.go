package relationship

import (
	"strings"
	"testing"
	"time"
)

func TestStickySettingOverridesInference(t *testing.T) {
	state := &StateBundle{}
	normalizeStateBundle(state, "user-1", "match-1")
	signal := Signal{
		Kind:       SignalUserTurn,
		ID:         "sig-settings-1",
		User: &UserSignal{
			Text:          "随便聊聊今天的比赛",
			Talkativeness: "quiet", // tier 推导会给出 quiet——被显式设置覆盖
			Settings: &PreferenceOverrides{
				InitiativeMode:   "active",
				AnalysisAppetite: "detailed",
			},
		},
		OccurredAt: time.Now(),
	}
	acts, codes := applyPolicy(state, signal, time.Now())
	if state.Relationship.Preferences.InitiativeMode != "active" {
		t.Fatalf("initiative = %q, want sticky setting to win over tier derivation", state.Relationship.Preferences.InitiativeMode)
	}
	if state.Relationship.Preferences.AnalysisAppetite != "detailed" {
		t.Fatalf("appetite = %q, want sticky setting", state.Relationship.Preferences.AnalysisAppetite)
	}
	joined := strings.Join(codes, ",")
	if !strings.Contains(joined, "policy_user_setting:initiative") || !strings.Contains(joined, "policy_user_setting:analysis_appetite") {
		t.Fatalf("codes = %q, want the setting reason codes", joined)
	}
	if len(acts) == 0 {
		t.Fatalf("acts must not be empty")
	}
}

func TestNoSettingsKeepsInference(t *testing.T) {
	state := &StateBundle{}
	normalizeStateBundle(state, "user-1", "match-1")
	signal := Signal{
		Kind: SignalUserTurn,
		ID:   "sig-settings-2",
		User: &UserSignal{Text: "随便聊聊今天的比赛", Talkativeness: "quiet"},
	}
	applyPolicy(state, signal, time.Now())
	if state.Relationship.Preferences.InitiativeMode != "quiet" {
		t.Fatalf("initiative = %q, want tier derivation unchanged without settings", state.Relationship.Preferences.InitiativeMode)
	}
}
