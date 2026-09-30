package relationship

import (
	"context"
	"testing"
	"time"
)

// The rotation contract (live2d-engine-swap 6.4): every pool member is
// client-whitelisted and lands inside a single model motion group through
// presentation-map.json; consecutive rotations of a slot never repeat; and
// the Director flags the rotation into the decision reason codes while the
// semantic slot (expression, tuning, hold, ReturnMode) stays the table's.
func TestPresentationVariantPoolsResolveInContract(t *testing.T) {
	mapping := loadPresentationMap(t)
	model := loadModelAsset(t)
	for slot, pool := range motionVariantPools {
		if len(pool) < 2 {
			t.Fatalf("slot %q pool has %d members; single-member pools must not exist (rotation without a pool is dead code)", slot, len(pool))
		}
		group := ""
		for _, name := range pool {
			if !ClientAcceptsMotion(name) {
				t.Fatalf("slot %q pool member %q is outside the client whitelist", slot, name)
			}
			entry, ok := mapping.Motions[name]
			if !ok {
				t.Fatalf("slot %q pool member %q has no presentation-map.json motions row", slot, name)
			}
			variants, isGroup := model.FileReferences.Motions[entry.Group]
			if !isGroup {
				t.Fatalf("slot %q pool member %q targets unknown model group %q", slot, name, entry.Group)
			}
			if entry.Variant < 0 || entry.Variant >= len(variants) {
				t.Fatalf("slot %q pool member %q variant %d out of range in group %q", slot, name, entry.Variant, entry.Group)
			}
			if group == "" {
				group = entry.Group
			} else if group != entry.Group {
				t.Fatalf("slot %q pool spans model groups %q and %q — a variant pool must stay inside one semantic family", slot, group, entry.Group)
			}
		}
	}
}

func TestRotationNeverRepeatsConsecutively(t *testing.T) {
	ResetPresentationVariantRotation()
	defer ResetPresentationVariantRotation()
	for slot, pool := range motionVariantPools {
		previous := ""
		for index := 0; index < len(pool)*3; index++ {
			next := RotatePresentationMotion(slot)
			if next == previous {
				t.Fatalf("slot %q repeated %q on consecutive rotations", slot, next)
			}
			previous = next
		}
		// The rotation cycles the whole pool, not a subset.
		seen := map[string]bool{}
		for index := 0; index < len(pool); index++ {
			seen[RotatePresentationMotion(slot)] = true
		}
		if len(seen) != len(pool) {
			t.Fatalf("slot %q rotation covered %d of %d pool members", slot, len(seen), len(pool))
		}
	}
}

func TestUnknownSlotsPassThroughUnrotated(t *testing.T) {
	for _, motion := range []string{"complain", "tense", "analysis", "agree", "miss", "hello", "wave", "think", "focus", "celebrate_02"} {
		if rotated := RotatePresentationMotion(motion); rotated != motion {
			t.Fatalf("motion %q must not rotate (deliberately pinned landing), got %q", motion, rotated)
		}
	}
}

func TestApplyPresentationRotationKeepsSemanticSlot(t *testing.T) {
	ResetPresentationVariantRotation()
	defer ResetPresentationVariantRotation()
	plan := PresentationPlan{
		Expression:    "excited",
		Motion:        "celebrate",
		VoiceStyle:    "excited",
		HoldMS:        holdLastFrameHoldFloorMS,
		ReturnMode:    "watching",
		HoldLastFrame: true,
	}
	rotated, changed := ApplyPresentationRotation(plan)
	if changed || rotated.Motion != "celebrate" {
		t.Fatalf("first rotation = (%q, changed=%v), want the unchanged pool head celebrate", rotated.Motion, changed)
	}
	if rotated.Expression != plan.Expression || rotated.VoiceStyle != plan.VoiceStyle ||
		rotated.HoldMS != plan.HoldMS || rotated.ReturnMode != plan.ReturnMode ||
		!rotated.HoldLastFrame {
		t.Fatalf("rotation drifted the semantic slot: %+v -> %+v", plan, rotated)
	}
	rotated, changed = ApplyPresentationRotation(plan)
	if !changed || rotated.Motion != "celebrate_02" {
		t.Fatalf("second rotation = (%q, changed=%v), want celebrate_02", rotated.Motion, changed)
	}
	if rotated.Expression != plan.Expression || rotated.VoiceStyle != plan.VoiceStyle ||
		rotated.HoldMS != plan.HoldMS || rotated.ReturnMode != plan.ReturnMode ||
		!rotated.HoldLastFrame {
		t.Fatalf("rotation drifted the semantic slot: %+v -> %+v", plan, rotated)
	}
}

// TestDirectorRotatesVariantAndFlagsReason pins the Director integration:
// consecutive same-slot decisions rotate the motion name and carry the
// "motion_variant_rotated" reason code, while a non-pool body (hello) never
// rotates and never flags.
func TestDirectorRotatesVariantAndFlagsReason(t *testing.T) {
	ResetPresentationVariantRotation()
	defer ResetPresentationVariantRotation()
	ctx := context.Background()
	now := time.Date(2026, 7, 15, 20, 0, 0, 0, time.UTC)
	director := NewDirector(NewMemoryRepository())

	first, err := director.Apply(ctx, Signal{
		ID: "rotation-1", Kind: SignalUserTurn, UserID: "user-rotation", MatchID: "match-rotation", OccurredAt: now,
		User: &UserSignal{Text: "在吗"}, Grounding: GroundedContent{Intent: "smalltalk"},
	})
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	second, err := director.Apply(ctx, Signal{
		ID: "rotation-2", Kind: SignalUserTurn, UserID: "user-rotation", MatchID: "match-rotation", OccurredAt: now.Add(time.Minute),
		User: &UserSignal{Text: "你怎么看"}, Grounding: GroundedContent{Intent: "smalltalk"},
	})
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if first.Presentation.Expression != "chat" || second.Presentation.Expression != "chat" {
		t.Fatalf("talk slot expressions = (%q, %q), want chat/chat", first.Presentation.Expression, second.Presentation.Expression)
	}
	if first.Presentation.Motion != second.Presentation.Motion {
		t.Fatalf("consecutive talk motions = (%q, %q), want identical canonical speak (speak 出池:live2d-motion-revert 钉死+phase-motions/workflows eval 契约)", first.Presentation.Motion, second.Presentation.Motion)
	}
	if first.Presentation.Motion != "speak" {
		t.Fatalf("talk motion = %q, want canonical speak", first.Presentation.Motion)
	}
	for _, decision := range []Decision{first, second} {
		for _, code := range decision.ReasonCodes {
			if code == "motion_variant_rotated" {
				t.Fatalf("talk decision must not flag rotation after speak 出池: %+v", decision.ReasonCodes)
			}
		}
	}

	greeting, err := director.Apply(ctx, Signal{
		ID: "rotation-greeting", Kind: SignalSessionOpened, UserID: "user-rotation", MatchID: "match-rotation", OccurredAt: now.Add(2 * time.Minute),
	})
	if err != nil {
		t.Fatalf("greeting Apply: %v", err)
	}
	if greeting.Presentation.Motion != "hello" {
		t.Fatalf("greeting motion = %q, want the unrotated hello", greeting.Presentation.Motion)
	}
	for _, code := range greeting.ReasonCodes {
		if code == "motion_variant_rotated" {
			t.Fatalf("hello rotation must not flag the reason code: %+v", greeting.ReasonCodes)
		}
	}
}
