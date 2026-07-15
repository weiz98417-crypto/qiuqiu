package conversation

import (
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

func TestProactiveGateOnlyAppliesAutomationEventSwitch(t *testing.T) {
	gate := NewProactiveGate()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	policy := matchstate.AutomationPolicy{
		Mode:            matchstate.AutomationModeActive,
		EventTypes:      []string{"shot", "goal"},
		CooldownSeconds: 10,
	}

	if !gate.Allow(policy, "shot", false, now) {
		t.Fatal("first enabled normal event should be allowed")
	}
	if !gate.Allow(policy, "shot", false, now.Add(5*time.Second)) {
		t.Fatal("director, not automation gate, should own proactive cooldown")
	}
	if !gate.Allow(policy, "goal", true, now.Add(6*time.Second)) {
		t.Fatal("critical event should bypass cooldown")
	}
	if !gate.Allow(policy, "shot", false, now.Add(12*time.Second)) {
		t.Fatal("critical event must not mutate a second cooldown")
	}
	if gate.Allow(policy, "save", false, now.Add(30*time.Second)) {
		t.Fatal("disabled event type should be suppressed")
	}

	policy.Mode = matchstate.AutomationModePaused
	if gate.Allow(policy, "goal", true, now.Add(time.Minute)) {
		t.Fatal("paused policy should suppress critical events")
	}
}

func TestManualProactiveLineDoesNotMutateAutomaticPolicy(t *testing.T) {
	gate := NewProactiveGate()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	if !gate.AllowManual(now) {
		t.Fatal("manual proactive line should always be allowed")
	}
	policy := matchstate.AutomationPolicy{
		Mode:            matchstate.AutomationModeActive,
		EventTypes:      []string{"shot"},
		CooldownSeconds: 10,
	}
	if !gate.Allow(policy, "shot", false, now.Add(5*time.Second)) {
		t.Fatal("manual line must not create a cooldown outside the director")
	}
}
