package companion

// 意图词汇表锁（openspec/changes/agent-engine-extraction）：意图词汇曾散在
// 4 处（Intent 枚举、Classify、router schema 枚举、routedTurnIntent 映射）
// 靠人肉保持 1:1。router schema 枚举已收敛为 router 包的单一真源
// （routableIntents），本测试把它与 routedTurnIntent 的映射机器锁定——
// 漂移（加意图漏改映射、改枚举名）在测试期即红。

import (
	"testing"

	"qiuqiu/internal/router"
)

func TestIntentVocabularyLockedAcrossRouterSeam(t *testing.T) {
	// 正向完备：schema 枚举里的每个意图（unknown 除外）都必须能映射到
	// 一个具体的后端 intent——加了新枚举值漏改 routedTurnIntent 即红。
	for _, raw := range router.RoutableIntents() {
		if raw == "unknown" {
			continue
		}
		if routedTurnIntent(raw) == IntentUnknown {
			t.Fatalf("router intent %q maps to unknown — routedTurnIntent is missing the mapping (vocabulary drift)", raw)
		}
	}

	// 逐项锁定：改枚举值/改映射即红。
	expected := map[string]Intent{
		"smalltalk":             IntentSmalltalk,
		"schedule_question":     IntentSchedule,
		"match_status_question": IntentMatchStatus,
		"recent_event_question": IntentRecentEvent,
		"follow_up_question":    IntentFollowUp,
		"player_question":       IntentPlayerQuestion,
		"match_fact_claim":      IntentMatchClaim,
		"emotion_reaction":      IntentEmotionReaction,
		"personal_share":        IntentPersonalShare,
		"control_command":       IntentControlCommand,
		"unknown":               IntentUnknown,
		// match_reaction 是主动回合专属、用户回合不可达，落情绪路径。
		"match_reaction": IntentEmotionReaction,
	}
	for raw, want := range expected {
		if got := routedTurnIntent(raw); got != want {
			t.Fatalf("routedTurnIntent(%q) = %q, want %q", raw, got, want)
		}
	}

	// schema 枚举不得包含 match_reaction（用户回合不可达）。
	for _, raw := range router.RoutableIntents() {
		if raw == "match_reaction" {
			t.Fatal("router schema must not offer the proactive-only match_reaction intent")
		}
	}

	// 第三向：companion Intent 枚举全集必须被上表覆盖——新增 Intent 常量
	// 而不接入路由映射表时在此即红。IntentMatchReaction 例外：主动回合
	// 专属（HandleMatchEvent），不在用户回合映射值集，只断言其别名落点。
	for _, intent := range []Intent{
		IntentSmalltalk, IntentSchedule, IntentMatchStatus, IntentRecentEvent,
		IntentFollowUp, IntentPlayerQuestion, IntentEmotionReaction,
		IntentPersonalShare, IntentControlCommand, IntentMatchClaim, IntentUnknown,
	} {
		if _, ok := intentInExpected(expected, intent); !ok {
			t.Fatalf("Intent %q is not covered by the routedTurnIntent lock table (vocabulary drift)", intent)
		}
	}
	if routedTurnIntent("match_reaction") != IntentEmotionReaction {
		t.Fatalf("proactive-only match_reaction must alias onto the emotion path")
	}
}

// intentInExpected 报告 intent 是否出现在期望映射的值集里。
func intentInExpected(expected map[string]Intent, intent Intent) (string, bool) {
	for raw, want := range expected {
		if want == intent {
			return raw, true
		}
	}
	return "", false
}
