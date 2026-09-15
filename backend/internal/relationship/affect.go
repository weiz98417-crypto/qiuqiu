package relationship

import (
	"math"
	"strings"
	"time"
)

func updateAffect(state *AffectState, event MatchSignal, now time.Time) {
	ensureAffect(state, now)
	decayAffect(state, now)
	switch event.EventType {
	case "goal":
		state.Valence += 0.8
		state.Arousal += 0.8
		state.Tension += 0.1
		state.Confidence += 0.3
		state.Engagement += 0.4
	case "var_check":
		state.Arousal += 0.1
		state.Tension += 0.6
		state.Confidence -= 0.5
		state.Engagement += 0.2
	case "goal_cancelled":
		state.Valence -= 1.2
		state.Arousal -= 0.25
		state.Tension -= 0.2
		state.Confidence += 0.3
	case "shot_missed":
		state.Valence -= 0.2
		state.Arousal += 0.15
		state.Tension += 0.1
	}
	state.Valence = clamp(state.Valence, -1, 1)
	state.Arousal = clamp(state.Arousal, 0, 1)
	state.Tension = clamp(state.Tension, 0, 1)
	state.Confidence = clamp(state.Confidence, 0, 1)
	state.Engagement = clamp(state.Engagement, 0, 1)
	state.UpdatedAt = now
}

func ensureAffect(state *AffectState, now time.Time) {
	if !state.UpdatedAt.IsZero() {
		return
	}
	state.Arousal = 0.2
	state.Tension = 0.1
	state.Confidence = 0.6
	state.Engagement = 0.4
	state.UpdatedAt = now
}

func decayAffect(state *AffectState, now time.Time) {
	if state.UpdatedAt.IsZero() || !now.After(state.UpdatedAt) {
		return
	}
	elapsed := now.Sub(state.UpdatedAt)
	state.Valence = decayTo(state.Valence, 0, elapsed, 10*time.Minute)
	state.Arousal = decayTo(state.Arousal, 0.2, elapsed, 90*time.Second)
	state.Tension = decayTo(state.Tension, 0.1, elapsed, 2*time.Minute)
	state.Confidence = decayTo(state.Confidence, 0.6, elapsed, 2*time.Minute)
	state.Engagement = decayTo(state.Engagement, 0.4, elapsed, 10*time.Minute)
}

func decayTo(value, baseline float64, elapsed, decay time.Duration) float64 {
	return baseline + (value-baseline)*math.Exp(-float64(elapsed)/float64(decay))
}

func presentationFor(affect AffectState, signal Signal, actions []CommunicationAct) PresentationPlan {
	plan := PresentationPlan{
		Affect:      affect,
		Expression:  "focus",
		Motion:      "focus",
		VoiceStyle:  "natural",
		VoiceEnergy: clamp(0.35+affect.Arousal*0.55, 0, 1),
		VoiceSpeed:  clamp(0.9+affect.Arousal*0.15, 0.8, 1.1),
		HoldMS:      1800,
		ReturnMode:  "decay_to_focus",
	}
	if signal.User != nil && (hasCue(signal.User.Cues, CueNeedsSilence) || containsQuietRequest(signal.User.Text)) {
		plan.Expression = "low"
		plan.Motion = "idle"
		plan.VoiceStyle = "quiet"
		plan.VoiceEnergy = 0.2
		plan.HoldMS = 3200
		return plan
	}
	if signal.Kind == SignalSessionOpened {
		plan.Expression = "happy"
		plan.Motion = "hello"
		plan.VoiceStyle = "warm"
		plan.VoiceEnergy = 0.45
		plan.HoldMS = 2400
		return plan
	}
	if signal.Kind == SignalUserTurn && !hasAction(actions, ActSilence) {
		plan.Expression = "chat"
		plan.Motion = "speak"
		plan.HoldMS = 1800
		// Trigger: policy "explicit_analysis_request" (isTacticalQuestion) —
		// a tactical question gets the analysis tableau instead of plain talk.
		if hasAction(actions, ActAnalyze) {
			plan.Expression = "thinking"
			plan.Motion = "analysis"
		}
		// Triggers: policy "shared_moment_recalled(_with_permission)" and
		// "open_thread_ready_for_recall" — nodding along with a callback.
		if hasAction(actions, ActRecall) {
			plan.Expression = "happy"
			plan.Motion = "agree"
		}
		// Triggers: policy "stable_opinion_disagreement",
		// "unverified_fact_requires_reserve" and "personal_insult_rejected" —
		// pushing back on the user reads as the complaint gesture.
		if hasAction(actions, ActDisagree) {
			plan.Expression = "nervous"
			plan.Motion = "complain"
		}
		// Triggers: policy "banter_invited_with_permission" and
		// "playful_fact_correction_with_permission" — teasing keeps the
		// talking body but borrows the teasing expression.
		if hasAction(actions, ActTease) {
			plan.Expression = "tease"
		}
		return plan
	}
	if signal.Match == nil {
		return plan
	}
	switch signal.Match.EventType {
	case "goal":
		// Trigger: updateAffect "goal" (valence/arousal spike). Emits the
		// first-class celebrate group now that the client whitelists it
		// ('cheer' remains accepted as a legacy alias there).
		plan.Expression = "excited"
		plan.Motion = "celebrate"
		plan.VoiceStyle = "excited"
		plan.HoldMS = 2600
	case "var_check":
		// Trigger: updateAffect "var_check" (tension spike, confidence drop) —
		// the anxious wait during the VAR review.
		plan.Expression = "tense"
		plan.Motion = "tense"
		plan.VoiceStyle = "tense"
		plan.HoldMS = 2200
	case "goal_cancelled":
		// Trigger: updateAffect "goal_cancelled" (valence crash on a
		// controversial call) — deflated body plus the referee complaint.
		plan.Expression = "deflated"
		plan.Motion = "complain"
		plan.VoiceStyle = "low_disappointed"
		plan.HoldMS = 2800
	case "shot_missed":
		// Trigger: updateAffect "shot_missed" (mild valence dip) — the
		// near-miss gesture instead of the neutral focus default.
		plan.Expression = "low"
		plan.Motion = "miss"
		plan.VoiceStyle = "low_disappointed"
		plan.HoldMS = 2200
	}
	return plan
}

func containsQuietRequest(text string) bool {
	return strings.Contains(text, "不想分析") || strings.Contains(text, "缓会儿") || strings.Contains(text, "先别说话")
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}
