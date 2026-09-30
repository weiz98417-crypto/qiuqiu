package relationship

import "sync"

// Server-side motion variant rotation (live2d-engine-swap 6.4). The same
// semantic slot used to emit one fixed variant — repeated celebrations and
// the plain talk body replayed the identical motion file every time, which
// reads as mechanical (the idle carousel already varied client-side; command
// bodies did not). Modeled on the backchannel phrase rotation
// (internal/backchannel: pool[rotation%len] keyed per slot): the presentation
// table keeps owning the SEMANTIC slot (expression, voice tuning, hold,
// ReturnMode untouched), and only the motion name rotates through the slot's
// variant pool before the decision ships, so the trace/interaction audit
// records the actual variant that was chosen.
//
// Recon result (2026-09-30): the advanced fork's parallelMotion is a
// parallel-composition API, not variant scheduling, and the client-side
// variant pick is either random (web surface, invisible to the trace) or
// fixed-first (embedded surface) — so the rotation lives server-side where
// every consumer can see the choice.
//
// Pool membership is deliberately narrow: only slots whose landing group's
// variants are the same semantic family rotate. complain/tense/analysis/…
// keep their canonical landing (live2d-motion-revert pinned those names to
// specific motion files on purpose). presentation_variant_rotation_test.go
// locks every pool to the client whitelist and to a single model motion
// group via presentation-map.json.
var motionVariantPools = map[string][]string{
	// Plain talk body (ActReact neutral, ActAcknowledge, ActTease, the
	// user-turn base row): the two speak-group talk variants.
	"speak": {"speak_01", "speak_02"},
	// Celebration (goal-family rows: goal event, ActReact positive,
	// observation confirmed rides observationPresentation and stays put):
	// celebrate -> speak[0], celebrate_02 -> speak[1].
	"celebrate": {"celebrate", "celebrate_02"},
	// Quiet-request idle body (presentationFor's containsQuietRequest path):
	// the three idle-group variants.
	"idle": {"idle_01", "idle_02", "idle_03"},
}

var (
	variantRotationMu   sync.Mutex
	variantRotationSeen = map[string]int{}
)

// RotatePresentationMotion returns the next variant of the slot's pool,
// never repeating consecutively (pool[rotation%len], advanced on every call).
// Unknown slots pass through unchanged.
func RotatePresentationMotion(motion string) string {
	pool, ok := motionVariantPools[motion]
	if !ok || len(pool) == 0 {
		return motion
	}
	variantRotationMu.Lock()
	defer variantRotationMu.Unlock()
	index := variantRotationSeen[motion] % len(pool)
	variantRotationSeen[motion] = variantRotationSeen[motion] + 1
	return pool[index]
}

// ApplyPresentationRotation resolves a plan's body through its variant pool.
// The plan is returned with the rotated motion plus whether the name changed,
// so the caller can pin the fact into the decision reason codes (the rotated
// name itself lands in Decision.Presentation.Motion, which the trace and the
// interaction audit both carry).
func ApplyPresentationRotation(plan PresentationPlan) (PresentationPlan, bool) {
	rotated := RotatePresentationMotion(plan.Motion)
	if rotated == plan.Motion {
		return plan, false
	}
	plan.Motion = rotated
	return plan, true
}

// ResetPresentationVariantRotation clears the rotation counters (test seam —
// production rotation state is process-lifetime, matching the backchannel
// per-connection pattern's spirit).
func ResetPresentationVariantRotation() {
	variantRotationMu.Lock()
	defer variantRotationMu.Unlock()
	variantRotationSeen = map[string]int{}
}
