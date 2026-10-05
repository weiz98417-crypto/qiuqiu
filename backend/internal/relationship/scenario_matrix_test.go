package relationship

import (
	"testing"
	"time"
)

// B5 场景矩阵钉格（policy-bits 4.3）：进球全档短句快报、中场 normal/active
// 畅聊、quiet 与未知场景保持现状。数值改动必须过这里——矩阵是单源。
func TestScenarioContentForPinnedCells(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		talkativeness string
		wantSentences int
		wantChars     int
	}{
		{name: "goal_quiet_flash", eventType: "goal", talkativeness: "quiet", wantSentences: 2, wantChars: 80},
		{name: "goal_normal_flash", eventType: "goal", talkativeness: "normal", wantSentences: 2, wantChars: 80},
		{name: "goal_active_flash", eventType: "goal", talkativeness: "active", wantSentences: 2, wantChars: 80},
		{name: "halftime_quiet_keeps_status_quo", eventType: "halftime", talkativeness: "quiet", wantSentences: 2, wantChars: 80},
		{name: "halftime_normal_chat", eventType: "halftime", talkativeness: "normal", wantSentences: 4, wantChars: 200},
		{name: "halftime_active_chat", eventType: "halftime", talkativeness: "active", wantSentences: 4, wantChars: 200},
		{name: "halftime_empty_tier_degrades_to_chat", eventType: "halftime", talkativeness: "", wantSentences: 4, wantChars: 200},
		{name: "unknown_event_default", eventType: "save", talkativeness: "active", wantSentences: 2, wantChars: 80},
		{name: "empty_event_default", eventType: "", talkativeness: "quiet", wantSentences: 2, wantChars: 80},
	}
	for _, tc := range tests {
		sentences, chars := ScenarioContentFor(tc.eventType, tc.talkativeness)
		if sentences != tc.wantSentences || chars != tc.wantChars {
			t.Fatalf("%s: ScenarioContentFor = (%d, %d), want (%d, %d)", tc.name, sentences, chars, tc.wantSentences, tc.wantChars)
		}
	}
}

// B2 双门同开同关（review 修正）：quiet×赛点在 selectTurnActs 不被压成
// silence——gate 层放行了，policy 层压掉就是两门不一致；预算由
// contentPolicyFor 的单句 clamp 收口。
func TestQuietPivotalMatchEventNotSilenced(t *testing.T) {
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	director := NewDirector(NewMemoryRepository())
	signal := Signal{
		ID: "s-quiet-pivotal", Kind: SignalMatchEvent, UserID: "user", MatchID: "match-quiet-pivotal", OccurredAt: now,
		Match: &MatchSignal{EventID: "e1", EventType: "penalty_awarded", OutputAllowed: true, Pivotal: true, Talkativeness: "quiet"},
	}
	decision := applyScenario(t, director, signal)
	if hasAction(decision.Speech.Actions, ActSilence) {
		t.Fatalf("quiet pivotal silenced: %v — policy gate must mirror the proactive gate", decision.Speech.Actions)
	}
}

// contentPolicyFor 经矩阵取预算：中场畅聊落到 plan、进球快报钉住 2 句、
// 用户话轮（signal.Match 为 nil）不经矩阵。
func TestContentPolicyForConsumesScenarioMatrix(t *testing.T) {
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	state := &StateBundle{}
	state.Match.Affect = AffectState{Engagement: 1}

	halftime := Signal{
		ID: "s-halftime", Kind: SignalMatchEvent, UserID: "user", MatchID: "m1", OccurredAt: now,
		Match: &MatchSignal{EventID: "e1", EventType: "halftime", OutputAllowed: true, Talkativeness: "normal"},
	}
	plan := speechFor(halftime, []CommunicationAct{ActReact}, state.Relationship, state.Match)
	if plan.Content.MaxSentences != 4 || plan.Content.MaxCharacters != 200 {
		t.Fatalf("halftime normal budget = (%d, %d), want (4, 200)", plan.Content.MaxSentences, plan.Content.MaxCharacters)
	}

	goal := Signal{
		ID: "s-goal", Kind: SignalMatchEvent, UserID: "user", MatchID: "m1", OccurredAt: now,
		Match: &MatchSignal{EventID: "e2", EventType: "goal", OutputAllowed: true, Talkativeness: "active"},
	}
	plan = speechFor(goal, []CommunicationAct{ActReact}, state.Relationship, state.Match)
	if plan.Content.MaxSentences != 2 || plan.Content.MaxCharacters != 80 {
		t.Fatalf("goal active budget = (%d, %d), want (2, 80)", plan.Content.MaxSentences, plan.Content.MaxCharacters)
	}

	userTurn := Signal{
		ID: "s-user", Kind: SignalUserTurn, UserID: "user", MatchID: "m1", OccurredAt: now,
		User: &UserSignal{Text: "聊聊"}, Grounding: GroundedContent{Intent: "unknown"},
	}
	plan = speechFor(userTurn, []CommunicationAct{ActAcknowledge}, state.Relationship, state.Match)
	if plan.Content.MaxSentences != 2 || plan.Content.MaxCharacters != 80 {
		t.Fatalf("user turn budget = (%d, %d), want untouched (2, 80)", plan.Content.MaxSentences, plan.Content.MaxCharacters)
	}
}

// B2 quiet×赛点单句短播（4.2 默认裁决）：预算 clamp 对矩阵与 act 调整保持
// 权威；非 quiet 赛点不收窄。
func TestContentPolicyForQuietPivotalSingleSentence(t *testing.T) {
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	state := &StateBundle{}
	state.Match.Affect = AffectState{Engagement: 1}

	quietPivotal := Signal{
		ID: "s-pivotal", Kind: SignalMatchEvent, UserID: "user", MatchID: "m1", OccurredAt: now,
		Match: &MatchSignal{EventID: "e3", EventType: "goal", OutputAllowed: true, Critical: true, Pivotal: true, Talkativeness: "quiet"},
	}
	plan := speechFor(quietPivotal, []CommunicationAct{ActReact}, state.Relationship, state.Match)
	if plan.Content.MaxSentences != 1 || plan.Content.MaxCharacters != 40 {
		t.Fatalf("quiet pivotal budget = (%d, %d), want (1, 40)", plan.Content.MaxSentences, plan.Content.MaxCharacters)
	}

	normalPivotal := Signal{
		ID: "s-pivotal-2", Kind: SignalMatchEvent, UserID: "user", MatchID: "m1", OccurredAt: now,
		Match: &MatchSignal{EventID: "e4", EventType: "goal", OutputAllowed: true, Critical: true, Pivotal: true, Talkativeness: "normal"},
	}
	plan = speechFor(normalPivotal, []CommunicationAct{ActReact}, state.Relationship, state.Match)
	if plan.Content.MaxSentences != 2 || plan.Content.MaxCharacters != 80 {
		t.Fatalf("normal pivotal budget = (%d, %d), want matrix cell (2, 80)", plan.Content.MaxSentences, plan.Content.MaxCharacters)
	}
}
