package companion

// 意图注册表不变式锁（openspec/changes/intent-registry）：意图词汇的唯一
// 声明点是 intent_registry.go，本测试把「注册表 ↔ router 硬编码事实」的
// 镜像关系与各意图标记值集锁死——加意图漏改、改枚举名、prompt 行漂移、
// flag 值集变化在此即红。启动期另有 ValidateIntentRegistry fail-fast。

import (
	"testing"

	"qiuqiu/internal/router"
)

func TestIntentRegistryMirrorValid(t *testing.T) {
	if err := ValidateIntentRegistry(); err != nil {
		t.Fatalf("intent registry mirror validation failed: %v", err)
	}
}

func TestIntentVocabularyLockedAcrossRouterSeam(t *testing.T) {
	// 正向完备：schema 枚举里的每个意图（unknown 除外）都必须能映射到
	// 一个具体的后端 intent——加了新枚举值漏改注册表即红。
	for _, raw := range router.RoutableIntents() {
		if raw == "unknown" {
			continue
		}
		if routedTurnIntent(raw) == IntentUnknown {
			t.Fatalf("router intent %q maps to unknown — registry is missing the spec (vocabulary drift)", raw)
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

	// 第三向：companion Intent 枚举全集必须被注册表覆盖——新增 Intent 常量
	// 而不接入注册表时在此即红。IntentMatchReaction 例外：主动回合专属
	//（HandleMatchEvent），不在用户回合规格集，只断言其别名落点。
	for _, intent := range []Intent{
		IntentSmalltalk, IntentSchedule, IntentMatchStatus, IntentRecentEvent,
		IntentFollowUp, IntentPlayerQuestion, IntentEmotionReaction,
		IntentPersonalShare, IntentControlCommand, IntentMatchClaim, IntentUnknown,
	} {
		if routedTurnIntent(string(intent)) != intent {
			t.Fatalf("Intent %q is not covered by the registry router mapping (vocabulary drift)", intent)
		}
	}
	if routedTurnIntent("match_reaction") != IntentEmotionReaction {
		t.Fatalf("proactive-only match_reaction must alias onto the emotion path")
	}
}

// TestIntentSpecFlagValueSets 锁三个意图标记的值集（ADR-0009 locked
// decisions 4/5 与 design decision 4）——flag 语义变化必须显式改这里。
func TestIntentSpecFlagValueSets(t *testing.T) {
	isFactWants := map[Intent]bool{
		IntentMatchClaim: true, IntentMatchStatus: true, IntentRecentEvent: true,
		IntentFollowUp: true, IntentPlayerQuestion: true,
	}
	confidenceGatedWants := map[Intent]bool{
		IntentMatchClaim: true, IntentMatchStatus: true, IntentRecentEvent: true,
		IntentFollowUp: true, IntentPlayerQuestion: true,
		IntentSchedule: true, IntentControlCommand: true,
	}
	replyEligibleWants := map[Intent]bool{
		IntentSmalltalk: true, IntentEmotionReaction: true,
		IntentPersonalShare: true, IntentUnknown: true,
	}
	for _, spec := range intentRegistry.specs {
		if spec.IsFact != isFactWants[spec.Intent] {
			t.Fatalf("intent %q IsFact = %v, want %v", spec.Intent, spec.IsFact, isFactWants[spec.Intent])
		}
		if spec.ConfidenceGated != confidenceGatedWants[spec.Intent] {
			t.Fatalf("intent %q ConfidenceGated = %v, want %v", spec.Intent, spec.ConfidenceGated, confidenceGatedWants[spec.Intent])
		}
		if spec.ReplyEligible != replyEligibleWants[spec.Intent] {
			t.Fatalf("intent %q ReplyEligible = %v, want %v", spec.Intent, spec.ReplyEligible, replyEligibleWants[spec.Intent])
		}
	}
}

// TestClassifyPipelineCoversIntents 锁分类管道对意图的覆盖——每个有确定
// 性 handler 的意图至少要有一条管道步骤可达（Unknown 除外：它由空文本与
// 管道未命中兜底）。
func TestClassifyPipelineCoversIntents(t *testing.T) {
	reachable := make(map[Intent]bool)
	for _, step := range intentRegistry.pipeline {
		reachable[step.Intent] = true
	}
	for _, spec := range intentRegistry.specs {
		if spec.Intent == IntentUnknown {
			continue
		}
		if !reachable[spec.Intent] {
			t.Fatalf("intent %q has a handler but no classify pipeline step — keyword path can never reach it", spec.Intent)
		}
	}
}
