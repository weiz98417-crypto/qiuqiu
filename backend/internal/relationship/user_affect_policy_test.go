package relationship

import (
	"testing"
	"time"
)

// containsCode 报告理由码列表是否含目标码。
func containsCode(codes []string, wanted string) bool {
	for _, code := range codes {
		if code == wanted {
			return true
		}
	}
	return false
}

// C2 useraffect 偏置（policy-bits 4.5）：叹气/低落 × 支持队落后时，战术问
// 答（解说型 ActAnalyze）改道安慰型 ActReact，理由码 user_affect: 随行。
func TestUserAffectBiasDivertsTacticalQuestionToComfort(t *testing.T) {
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	director := NewDirector(NewMemoryRepository())
	signal := Signal{
		ID: "s-affect", Kind: SignalUserTurn, UserID: "user", MatchID: "match-affect", OccurredAt: now,
		User: &UserSignal{
			Text: "防线为什么回收得这么深",
			Affect: &UserAffectBias{
				Label:      "sad",
				Confidence: 0.8,
				TeamBehind: true,
			},
		},
	}
	decision := applyScenario(t, director, signal)
	if !hasAction(decision.Speech.Actions, ActReact) || hasAction(decision.Speech.Actions, ActAnalyze) {
		t.Fatalf("biased turn actions = %v, want comfort ActReact without ActAnalyze", decision.Speech.Actions)
	}
	if !containsCode(decision.ReasonCodes, "user_affect:comfort_over_analysis") {
		t.Fatalf("reason codes = %v, want user_affect:comfort_over_analysis", decision.ReasonCodes)
	}
}

// 信号缺席 = 现状（字节级）：同一话轮不带 Affect 载荷，战术问答走原路
// ActAnalyze + explicit_analysis_request——偏置入口的存在不可观测。
func TestUserAffectAbsentKeepsByteLevelStatusQuo(t *testing.T) {
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	director := NewDirector(NewMemoryRepository())
	signal := Signal{
		ID: "s-plain", Kind: SignalUserTurn, UserID: "user", MatchID: "match-affect", OccurredAt: now,
		User: &UserSignal{Text: "防线为什么回收得这么深"},
	}
	decision := applyScenario(t, director, signal)
	if !hasAction(decision.Speech.Actions, ActAnalyze) {
		t.Fatalf("unbiased tactical question actions = %v, want ActAnalyze", decision.Speech.Actions)
	}
	if !containsCode(decision.ReasonCodes, "explicit_analysis_request") {
		t.Fatalf("reason codes = %v, want explicit_analysis_request", decision.ReasonCodes)
	}
	if containsCode(decision.ReasonCodes, "user_affect:comfort_over_analysis") {
		t.Fatal("absent signal must not leak the bias code")
	}
}

// 只偏置不支配：四取齐条件的每一项缺席都不偏置——置信门 0.55、标签低落、
// 支持队落后、载荷在场。改道只发生在战术问答出口：非问答话轮即使带满
// 载荷也走原路。
func TestUserAffectBiasGatesAllRequired(t *testing.T) {
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		affect *UserAffectBias
	}{
		{name: "confidence_below_gate", affect: &UserAffectBias{Label: "sad", Confidence: 0.54, TeamBehind: true}},
		{name: "label_not_dejected", affect: &UserAffectBias{Label: "happy", Confidence: 0.9, TeamBehind: true}},
		{name: "team_not_behind", affect: &UserAffectBias{Label: "sad", Confidence: 0.9, TeamBehind: false}},
	}
	for _, tc := range cases {
		director := NewDirector(NewMemoryRepository())
		signal := Signal{
			ID: "s-gate", Kind: SignalUserTurn, UserID: "user", MatchID: "match-affect", OccurredAt: now,
			User: &UserSignal{Text: "防线为什么回收得这么深", Affect: tc.affect},
		}
		decision := applyScenario(t, director, signal)
		if !hasAction(decision.Speech.Actions, ActAnalyze) || !containsCode(decision.ReasonCodes, "explicit_analysis_request") {
			t.Fatalf("%s: actions = %v codes = %v, want unbiased ActAnalyze", tc.name, decision.Speech.Actions, decision.ReasonCodes)
		}
	}

	// 满载荷但非战术问答（情绪反应话轮）：原路 user_emotion_reaction。
	director := NewDirector(NewMemoryRepository())
	emotional := Signal{
		ID: "s-emotional", Kind: SignalUserTurn, UserID: "user", MatchID: "match-affect", OccurredAt: now,
		User: &UserSignal{Text: "唉", Affect: &UserAffectBias{Label: "sad", Confidence: 0.9, TeamBehind: true}},
		Grounding: GroundedContent{Intent: "emotion_reaction"},
	}
	decision := applyScenario(t, director, emotional)
	if !hasAction(decision.Speech.Actions, ActReact) || !containsCode(decision.ReasonCodes, "user_emotion_reaction") {
		t.Fatalf("non-question biased turn = %v / %v, want original path", decision.Speech.Actions, decision.ReasonCodes)
	}

	// 标签集单源：大小写与空白归一化，未知标签不偏置。
	if !UserAffectDejected(" SAD ") || UserAffectDejected("angry") || UserAffectDejected("") {
		t.Fatal("UserAffectDejected normalization broken")
	}
}
