package conversation

import (
	"testing"
	"time"

	"qiuqiu/internal/matchstate"
)

// B2 赛点分类（policy-bits 4.1）：点球判罚/红牌按类型命中；决胜时段一球
// 差按类型+时钟+事件后比分命中；其余一律不抬档。
func TestIsPivotalMatchEvent(t *testing.T) {
	tests := []struct {
		name  string
		event matchstate.MatchEvent
		want  bool
	}{
		{
			name:  "penalty_awarded_is_pivotal",
			event: matchstate.MatchEvent{EventType: "penalty_awarded", Clock: "60:00"},
			want:  true,
		},
		{
			name:  "penalty_is_pivotal",
			event: matchstate.MatchEvent{EventType: "penalty", Clock: "88:00"},
			want:  true,
		},
		{
			name:  "red_card_is_pivotal",
			event: matchstate.MatchEvent{EventType: "red_card", Clock: "55:12"},
			want:  true,
		},
		{
			name:  "late_goal_one_goal_lead_is_pivotal",
			event: matchstate.MatchEvent{EventType: "goal", Clock: "76:30", Score: matchstate.Score{Home: 2, Away: 1}},
			want:  true,
		},
		{
			name:  "late_goal_two_goal_lead_is_not",
			event: matchstate.MatchEvent{EventType: "goal", Clock: "80:00", Score: matchstate.Score{Home: 3, Away: 1}},
			want:  false,
		},
		{
			name:  "early_goal_one_goal_lead_is_not",
			event: matchstate.MatchEvent{EventType: "goal", Clock: "23:41", Score: matchstate.Score{Home: 1, Away: 0}},
			want:  false,
		},
		{
			name:  "late_equalizer_is_not",
			event: matchstate.MatchEvent{EventType: "goal", Clock: "85:00", Score: matchstate.Score{Home: 1, Away: 1}},
			want:  false,
		},
		{
			name:  "goal_without_clock_is_not",
			event: matchstate.MatchEvent{EventType: "goal", Score: matchstate.Score{Home: 1, Away: 0}},
			want:  false,
		},
		{
			name:  "shot_is_never_pivotal",
			event: matchstate.MatchEvent{EventType: "shot", Clock: "90:00", Score: matchstate.Score{Home: 1, Away: 0}},
			want:  false,
		},
		{
			name:  "effective_score_after_wins_over_reported",
			event: matchstate.MatchEvent{EventType: "goal", Clock: "78:00", Score: matchstate.Score{Home: 3, Away: 0}, EffectiveScoreAfter: &matchstate.Score{Home: 2, Away: 1}},
			want:  true,
		},
	}
	for _, tc := range tests {
		if got := IsPivotalMatchEvent(tc.event); got != tc.want {
			t.Fatalf("%s: IsPivotalMatchEvent = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// B2：quiet 档赛点放行（gate 只管放行，单句短播预算在 relationship
// contentPolicyFor）；非赛点的 quiet 纪律原样。
func TestProactiveGatePivotalPassesQuietTier(t *testing.T) {
	gate := NewProactiveGate()
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	policy := matchstate.AutomationPolicy{
		Mode:            matchstate.AutomationModeActive,
		EventTypes:      []string{"goal", "shot"},
		CooldownSeconds: 10,
	}

	if !gate.Allow(policy, "goal", CitationPivotal, "quiet", true, true, now) {
		t.Fatal("quiet tier must allow pivotal moments (4.2 default ruling)")
	}
	if gate.Allow(policy, "shot", EventCitation("event-1"), "quiet", false, false, now) {
		t.Fatal("quiet tier must still suppress non-pivotal non-critical events")
	}
	if !gate.Allow(policy, "goal", CitationPivotal, "normal", true, true, now) {
		t.Fatal("pivotal moments pass for normal tier unchanged")
	}
	if gate.Allow(policy, "goal", "", "quiet", true, true, now) {
		t.Fatal("pivotal must not bypass the citation requirement")
	}
}
