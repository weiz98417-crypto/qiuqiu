package relationship

// Presentation vocabulary contract (ADR-0005: PresentationPlan stays the single
// client contract; change agent-depth C4 widened the client whitelist from 7 to
// all 12 motion groups / 17 motions).
//
// The constants below mirror client/lib/services/presentation_state.dart
// (allowedExpressions / expressionAliases / allowedMotions / motionAliases).
// Keep the two files in sync:
//   - presentation_vocabulary_test.go asserts that every expression/motion the
//     policy layer can emit (presentationFor, observationPresentation) is
//     accepted here;
//   - client/test/presentation_whitelist_test.dart asserts the client whitelist
//     against the actual female_01Arkit_6.model3.json asset.
var clientExpressionAliases = map[string]string{
	"low":      "sad",
	"tense":    "nervous",
	"deflated": "sad",
}

var clientMotionAliases = map[string]string{
	"hold":   "focus",
	"settle": "idle",
	"slump":  "idle",
	"nod":    "agree",
	// presentation-map.json names two listen/idle-group motions outside its
	// motions section: delivery.interrupted says "confused/listening" and
	// events.var_overturn says "surprised/confused". The web surface plays
	// `listening` via an explicit motionGroups key and falls back to the
	// idle group for unknown keys like `confused`
	// (motionGroups[group] || motionGroups.idle); the strict whitelist
	// absorbs both names here so mapped plans are accepted.
	"listening": "listen",
	"confused":  "idle",
}

var clientAllowedExpressions = map[string]bool{
	"idle":      true,
	"listening": true,
	"focus":     true,
	"thinking":  true,
	"confused":  true,
	"excited":   true,
	"chat":      true,
	"tease":     true,
	"happy":     true,
	"nervous":   true,
	"sad":       true,
	"surprised": true,
	"angry":     true,
}

var clientAllowedMotions = map[string]bool{
	// The 12 motion groups of female_01Arkit_6.model3.json.
	"hello":     true,
	"idle":      true,
	"listen":    true,
	"speak":     true,
	"think":     true,
	"celebrate": true,
	"miss":      true,
	"complain":  true,
	"analysis":  true,
	"tense":     true,
	"agree":     true,
	"wave":      true,
	// Individual variants inside multi-motion groups.
	"idle_01":      true,
	"idle_02":      true,
	"idle_03":      true,
	"listen_01":    true,
	"listen_02":    true,
	"speak_01":     true,
	"speak_02":     true,
	"celebrate_01": true,
	"celebrate_02": true,
	// Legacy synthetic names pre-dating the full motion pack.
	"cheer": true,
	"focus": true,
}

var clientAllowedVoiceStyles = map[string]bool{
	"natural":          true,
	"quiet":            true,
	"warm":             true,
	"excited":          true,
	"tense":            true,
	"low_disappointed": true,
	"calm":             true,
	"soft":             true,
}

// NormalizeClientExpression resolves legacy expression aliases the way the
// client's normalizeExpression does.
func NormalizeClientExpression(value string) string {
	if alias, ok := clientExpressionAliases[value]; ok {
		return alias
	}
	return value
}

// NormalizeClientMotion resolves legacy motion aliases the way the client's
// fromReplyData does.
func NormalizeClientMotion(value string) string {
	if alias, ok := clientMotionAliases[value]; ok {
		return alias
	}
	return value
}

// ClientAcceptsExpression reports whether the client whitelist accepts the
// expression name (after legacy aliasing).
func ClientAcceptsExpression(value string) bool {
	return clientAllowedExpressions[NormalizeClientExpression(value)]
}

// ClientAcceptsMotion reports whether the client whitelist accepts the motion
// name (after legacy aliasing).
func ClientAcceptsMotion(value string) bool {
	return clientAllowedMotions[NormalizeClientMotion(value)]
}

// ClientAcceptsVoiceStyle reports whether the client whitelist accepts the
// voice style.
func ClientAcceptsVoiceStyle(value string) bool {
	return clientAllowedVoiceStyles[value]
}
