package relationship

import "strings"

// Talkativeness is the user's 话痨程度 setting (settings_screen.dart), sent by
// the client inside every user_speech payload and now parsed by the backend —
// this closes the drift where the field was sent but never read.
const (
	TalkativenessQuiet  = "quiet"
	TalkativenessNormal = "normal"
	TalkativenessActive = "active"
)

// ActiveCooldownMultiplier shortens the proactive cooldown for active users:
// a "热闹" user opted into more initiative, never into new proactive classes.
const ActiveCooldownMultiplier = 0.6

// NormalizeTalkativeness maps any client value onto the three tiers; empty or
// unknown values degrade to the current behavior ("normal").
func NormalizeTalkativeness(tier string) string {
	switch strings.ToLower(strings.TrimSpace(tier)) {
	case TalkativenessQuiet:
		return TalkativenessQuiet
	case TalkativenessActive:
		return TalkativenessActive
	default:
		return TalkativenessNormal
	}
}

// IsQuiet reports whether the tier restricts proactive volume. Quiet is
// L0-safe by construction: it can only suppress, never enable.
func IsQuiet(tier string) bool {
	return NormalizeTalkativeness(tier) == TalkativenessQuiet
}

// ScaleCooldownForTalkativeness applies the tier multiplier to the operator's
// base proactive cooldown: quiet/normal keep the base, active shortens it.
func ScaleCooldownForTalkativeness(baseSeconds int, tier string) int {
	if baseSeconds <= 0 {
		return baseSeconds
	}
	if NormalizeTalkativeness(tier) != TalkativenessActive {
		return baseSeconds
	}
	scaled := int(float64(baseSeconds) * ActiveCooldownMultiplier)
	if scaled < 1 {
		return 1
	}
	return scaled
}

// InitiativeModeForTalkativeness feeds the tier into the policy's
// InitiativeMode so the relationship view reflects the user's choice:
// quiet → "quiet", normal → "natural" (previous permanent value), active →
// "active".
func InitiativeModeForTalkativeness(tier string) string {
	switch NormalizeTalkativeness(tier) {
	case TalkativenessQuiet:
		return TalkativenessQuiet
	case TalkativenessActive:
		return TalkativenessActive
	default:
		return "natural"
	}
}
