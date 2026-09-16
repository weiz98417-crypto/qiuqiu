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
	citation := EventCitation("event-1")

	if !gate.Allow(policy, "shot", citation, "normal", false, now) {
		t.Fatal("first enabled normal event should be allowed")
	}
	if !gate.Allow(policy, "shot", citation, "normal", false, now.Add(5*time.Second)) {
		t.Fatal("director, not automation gate, should own proactive cooldown")
	}
	if !gate.Allow(policy, "goal", citation, "normal", true, now.Add(6*time.Second)) {
		t.Fatal("critical event should bypass cooldown")
	}
	if !gate.Allow(policy, "shot", citation, "normal", false, now.Add(12*time.Second)) {
		t.Fatal("critical event must not mutate a second cooldown")
	}
	if gate.Allow(policy, "save", citation, "normal", false, now.Add(30*time.Second)) {
		t.Fatal("disabled event type should be suppressed")
	}

	policy.Mode = matchstate.AutomationModePaused
	if gate.Allow(policy, "goal", citation, "normal", true, now.Add(time.Minute)) {
		t.Fatal("paused policy should suppress critical events")
	}
}

func TestProactiveGateRequiresCitation(t *testing.T) {
	gate := NewProactiveGate()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	policy := matchstate.AutomationPolicy{
		Mode:            matchstate.AutomationModeActive,
		EventTypes:      []string{"shot", "goal"},
		CooldownSeconds: 10,
	}

	if gate.Allow(policy, "goal", "", "normal", true, now) {
		t.Fatal("no citation must mean no proactive turn, even for critical whitelisted events")
	}
	if gate.Allow(policy, "shot", "   ", "normal", false, now) {
		t.Fatal("blank citation must mean no proactive turn")
	}
	if !gate.Allow(policy, "shot", ThreadCitation("123"), "normal", false, now) {
		t.Fatal("an open-thread citation should allow the proactive turn")
	}
	if !gate.Allow(policy, "goal", EventCitation("event-9"), "normal", true, now) {
		t.Fatal("a shared-moment citation should allow the proactive turn")
	}
}

func TestProactiveGateQuietTierOnlyRestricts(t *testing.T) {
	gate := NewProactiveGate()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	policy := matchstate.AutomationPolicy{
		Mode:            matchstate.AutomationModeActive,
		EventTypes:      []string{"shot", "goal"},
		CooldownSeconds: 10,
	}
	citation := EventCitation("event-1")

	if gate.Allow(policy, "shot", citation, "quiet", false, now) {
		t.Fatal("quiet tier must suppress non-critical proactive turns")
	}
	if !gate.Allow(policy, "goal", citation, "quiet", true, now) {
		t.Fatal("quiet tier must keep critical match events allowed (already policy-allowed)")
	}
	if !gate.Allow(policy, "shot", citation, "", false, now) {
		t.Fatal("missing tier must degrade to current (normal) behavior")
	}
	if gate.Allow(policy, "shot", "", "quiet", true, now) {
		t.Fatal("quiet must not bypass the citation requirement")
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
	if !gate.Allow(policy, "shot", EventCitation("event-1"), "normal", false, now.Add(5*time.Second)) {
		t.Fatal("manual line must not create a cooldown outside the director")
	}
}
