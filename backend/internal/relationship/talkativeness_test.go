package relationship

import (
	"testing"
	"time"
)

func matchEventSignal(id string, critical bool, talkativeness string) Signal {
	return Signal{
		ID:         id,
		Kind:       SignalMatchEvent,
		UserID:     "user-1",
		MatchID:    "match-1",
		OccurredAt: time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC),
		Match: &MatchSignal{
			EventID:               id,
			EventType:             "shot",
			OutputAllowed:         true,
			Critical:              critical,
			NormalCooldownSeconds: 90,
			Talkativeness:         talkativeness,
		},
	}
}

func TestQuietTalkativenessSuppressesNonCriticalInitiative(t *testing.T) {
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	state := StateBundle{}
	actions, reasons := applyPolicy(&state, matchEventSignal("shot-1", false, TalkativenessQuiet), now)
	if len(actions) != 1 || actions[0] != ActSilence {
		t.Fatalf("actions = %v, quiet tier must silence non-critical proactive turns", actions)
	}
	if len(reasons) == 0 || reasons[len(reasons)-1] != "talkativeness_quiet_limits_initiative" {
		t.Fatalf("reasons = %v, want the quiet-tier reason code", reasons)
	}

	// Critical match events stay allowed: quiet only restricts, never enables.
	criticalState := StateBundle{}
	criticalActions, _ := applyPolicy(&criticalState, matchEventSignal("goal-1", true, TalkativenessQuiet), now)
	if len(criticalActions) == 0 || criticalActions[0] == ActSilence {
		t.Fatalf("critical actions = %v, quiet tier must keep critical events allowed", criticalActions)
	}
}

func TestNormalTalkativenessKeepsCurrentCooldown(t *testing.T) {
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	state := StateBundle{}
	if actions, _ := applyPolicy(&state, matchEventSignal("shot-1", false, TalkativenessNormal), now); len(actions) != 1 || actions[0] == ActSilence {
		t.Fatalf("actions = %v, normal tier keeps current proactive behavior", actions)
	}
	if actions, reasons := applyPolicy(&state, matchEventSignal("shot-2", false, TalkativenessNormal), now.Add(60*time.Second)); len(actions) != 1 || actions[0] != ActSilence {
		t.Fatalf("actions = %v reasons = %v, want the 90s cooldown still enforced for normal tier", actions, reasons)
	}
}

func TestActiveTalkativenessShortensCooldown(t *testing.T) {
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	state := StateBundle{}
	if actions, _ := applyPolicy(&state, matchEventSignal("shot-1", false, TalkativenessActive), now); len(actions) != 1 || actions[0] == ActSilence {
		t.Fatalf("actions = %v, active tier fires the first proactive turn", actions)
	}
	// 60s after the first turn: still inside the base 90s cooldown but outside
	// the active multiplier (90 x 0.6 = 54s), so the turn must be allowed.
	actions, reasons := applyPolicy(&state, matchEventSignal("shot-2", false, TalkativenessActive), now.Add(60*time.Second))
	if len(actions) != 1 || actions[0] == ActSilence {
		t.Fatalf("actions = %v reasons = %v, want the shortened active-tier cooldown to allow the turn", actions, reasons)
	}
	if ScaleCooldownForTalkativeness(90, TalkativenessActive) != 54 {
		t.Fatalf("active cooldown = %d, want 90 x 0.6", ScaleCooldownForTalkativeness(90, TalkativenessActive))
	}
}

func TestTalkativenessFeedsInitiativeModeOnUserTurns(t *testing.T) {
	now := time.Date(2026, 9, 10, 20, 0, 0, 0, time.UTC)
	cases := map[string]string{
		TalkativenessQuiet:  "quiet",
		TalkativenessNormal: "natural",
		TalkativenessActive: "active",
		"":                  "natural", // empty keeps the previous default
		"loud":              "natural", // unknown values degrade, never invent tiers
	}
	for tier, wantMode := range cases {
		state := StateBundle{}
		normalizeStateBundle(&state, "user-1", "match-1")
		signal := Signal{
			ID:         "turn-1",
			Kind:       SignalUserTurn,
			UserID:     "user-1",
			MatchID:    "match-1",
			OccurredAt: now,
			User:       &UserSignal{Text: "在吗", Talkativeness: tier},
		}
		if _, _ = applyPolicy(&state, signal, now); state.Relationship.Preferences.InitiativeMode != wantMode {
			t.Fatalf("tier %q → initiative mode %q, want %q", tier, state.Relationship.Preferences.InitiativeMode, wantMode)
		}
	}
}

func TestNormalizeTalkativenessAndModes(t *testing.T) {
	if NormalizeTalkativeness(" QUIET ") != TalkativenessQuiet {
		t.Fatal("normalize must trim and case-fold")
	}
	if NormalizeTalkativeness("whatever") != TalkativenessNormal {
		t.Fatal("unknown values must degrade to normal")
	}
	if IsQuiet("active") || !IsQuiet("quiet") {
		t.Fatal("IsQuiet must only match the quiet tier")
	}
	if InitiativeModeForTalkativeness("active") != "active" || InitiativeModeForTalkativeness("quiet") != "quiet" || InitiativeModeForTalkativeness("") != "natural" {
		t.Fatal("initiative mode mapping drifted")
	}
	if ScaleCooldownForTalkativeness(0, TalkativenessActive) != 0 || ScaleCooldownForTalkativeness(-5, TalkativenessActive) != -5 {
		t.Fatal("non-positive cooldowns must pass through unchanged")
	}
}
