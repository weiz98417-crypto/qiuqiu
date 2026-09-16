package memory

import (
	"strings"

	"qiuqiu/internal/relationship"
)

// Enqueue-time importance heuristic (ADR-0006): fully deterministic so the
// score in the audit table is reproducible; Memobase synthesis may refine the
// profile but never rewrites a moment's score.
//
//	score = clamp01(class base + marker bonus) x stage weight
//
// Class base (first matching rule wins):
//
//	match moment containing 进球/绝杀/点球/扳平/反超/goal     0.90
//	match moment containing 红牌/VAR/取消/red_card/var        0.80
//	match moment containing 黄牌/yellow_card                  0.55
//	promise                                                   0.80
//	user_fact                                                 0.70
//	emotional_exchange                                        0.60
//	anything else                                             0.30
//
// Marker bonus (each applied at most once, additive):
//
//	explicit preference/boundary statement via the policy-table
//	cues (我喜欢/我支持/我不喜欢/别拿…开我玩笑/不想分析)      +0.10
//	promise wording (待会儿/回头告诉你/答应/下次)             +0.10
//	high-arousal wording (绝了/气死/破防/泪目/太离谱/卧槽)     +0.05
//
// Stage weight: first_meeting x0.90, familiar x1.00, watch_buddy x1.05,
// old_ballmate x1.10 — later-stage moments are slightly more durable.

// MinExtractionImportance is the floor below which a moment is audited as
// rejected_low_importance instead of being sent to Memobase (an extraction
// LLM call is not worth it for routine chatter).
const MinExtractionImportance = 0.35

// PromiseMarkers and EmotionMarkers are the user-signal phrase lists shared
// by the importance heuristic and the turn-pipeline moment writers.
var (
	PromiseMarkers = []string{"待会儿告诉你", "待会儿", "回头告诉你", "回头聊", "答应", "下次告诉你", "等会儿告诉你"}
	EmotionMarkers = []string{"绝了", "气死", "破防", "泪目", "太离谱", "卧槽", "真爽", "难受", "心疼"}
)

// ScoreImportance computes the auditable enqueue-time importance in [0, 1].
func ScoreImportance(kind MomentKind, content string, stage Stage) float64 {
	score := classBase(kind, content) + markerBonus(content)
	return clamp01(score * stageWeight(stage))
}

func classBase(kind MomentKind, content string) float64 {
	if kind == MomentMatchEvent {
		return matchEventBase(content)
	}
	switch kind {
	case MomentPromise:
		return 0.80
	case MomentUserFact:
		return 0.70
	case MomentEmotionalExchange:
		return 0.60
	default:
		return 0.30
	}
}

func matchEventBase(content string) float64 {
	switch {
	case containsAny(content, "进球", "绝杀", "点球", "扳平", "反超", "goal", "penalty"):
		return 0.90
	case containsAny(content, "红牌", "VAR", "var_", "red_card", "取消"):
		return 0.80
	case containsAny(content, "黄牌", "yellow_card"):
		return 0.55
	default:
		return 0.75
	}
}

func markerBonus(content string) float64 {
	bonus := 0.0
	for _, cue := range relationship.InferUserCues(content) {
		if cue.Kind == relationship.CueStablePreference || cue.Kind == relationship.CueBanterDenied || cue.Kind == relationship.CueNeedsSilence {
			bonus += 0.10
			break
		}
	}
	if containsAny(content, PromiseMarkers...) {
		bonus += 0.10
	}
	if containsAny(content, EmotionMarkers...) {
		bonus += 0.05
	}
	return bonus
}

func stageWeight(stage Stage) float64 {
	switch stage {
	case StageFirstMeeting:
		return 0.90
	case StageWatchBuddy:
		return 1.05
	case StageOldBallmate:
		return 1.10
	default:
		return 1.00
	}
}

func containsAny(text string, phrases ...string) bool {
	for _, phrase := range phrases {
		if phrase != "" && strings.Contains(text, phrase) {
			return true
		}
	}
	return false
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}
