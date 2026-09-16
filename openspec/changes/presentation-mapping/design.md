# Design: Presentation Mapping

## Locked decisions

| # | Decision |
| --- | --- |
| 1 | Single source = `models/qiuqiu/presentation-map.json`; JS/Dart read it at runtime, Go table locked to it by contract test |
| 2 | All 30 slots routed; inventory-completeness is a failing test, not a doc note |
| 3 | Four mute acts get bodies per the assignment table below |
| 4 | `thinking` binds expression3 (hands-on-hips) provisionally; 7-expression visual naming is a device checklist |
| 5 | Ownership: backend authoritative during HoldMS; client phase/idle owns after ReturnMode; JS idle scheduler fills only idle-phase gaps |
| 6 | Order: C2 (backend table + single source) → C1 (client phase table) → C3 (ownership/ReturnMode/delivery reaction); ADR-0007 with the work |

## presentation-map.json schema

```json
{
  "expressions": {
    "focus": 0, "idle": 0, "listening": 0, "confused": 0,
    "excited": 1, "thinking": 3, "chat": 3, "tease": 3, "happy": 3,
    "nervous": 4, "sad": 4, "complain": 4, "surprised": 5, "angry": 6
  },
  "motions": {
    "hello": {"group": "hello", "variant": 0},
    "idle_01": {"group": "idle", "variant": 0},
    "idle_02": {"group": "idle", "variant": 1},
    "idle_03": {"group": "idle", "variant": 2},
    "listen_01": {"group": "listen", "variant": 0},
    "listen_02": {"group": "listen", "variant": 1},
    "speak_01": {"group": "speak", "variant": 0},
    "speak_02": {"group": "speak", "variant": 1},
    "think": {"group": "think", "variant": 0},
    "celebrate": {"group": "celebrate", "variant": 0},
    "celebrate_02": {"group": "celebrate", "variant": 1},
    "miss": {"group": "miss", "variant": 0},
    "complain": {"group": "complain", "variant": 0},
    "analysis": {"group": "analysis", "variant": 0},
    "tense": {"group": "tense", "variant": 0},
    "agree": {"group": "agree", "variant": 0},
    "wave": {"group": "wave", "variant": 0}
  },
  "acts": {
    "ActReact":       {"positive": "excited/celebrate", "negative": "nervous/complain", "neutral": "chat/speak"},
    "ActAsk":         {"any": "thinking/think"},
    "ActAnalyze":     {"any": "thinking/analysis"},
    "ActRecall":      {"any": "happy/agree"},
    "ActRepair":      {"any": "sad/agree", "energyDelta": -0.3},
    "ActDisagree":    {"mild": "nervous/complain", "strong": "angry/complain"},
    "ActTease":       {"any": "tease/speak"},
    "ActAcknowledge": {"any": "chat/speak"}
  },
  "events": {
    "goal": "excited/celebrate",
    "goal_cancelled": "surprised/complain",
    "var_overturn": "surprised/confused",
    "var_check": "tense/tense",
    "shot_missed": "sad/miss"
  },
  "phases": {
    "user_speaking": "listening/listen_01",
    "understanding": "thinking/think",
    "qiuqiu_speaking": "chat/speak_01",
    "session_open": "happy/hello",
    "match_end": "happy/wave",
    "idle": "affect-idle-tier"
  },
  "delivery": { "interrupted": "confused/listening" }
}
```

Notes: expression indices are provisional (thinking→3 pending the device naming pass); `variant` selects within multi-motion groups; backend continues to emit group names for single-motion groups and `celebrate` (client variant-picker already randomizes).

## Go mapping table

`backend/internal/relationship/presentation_table.go`:

```go
// presentationRow is one routed cell: a (act, quadrant, eventClass) key maps
// to an exact performance. Rows are data; the function is a lookup.
type presentationRow struct {
    act        relationship.CommunicationAct // or ActAny
    quadrant   AffectQuadrant                // Exuberant/Serene/Anxious/Deflated or Any
    eventClass string                        // goal/goal_cancelled/var_check/var_overturn/shot_missed/"" (non-match)
    expression string
    motion     string
    energyDelta float64
}
```

`presentationFor` keeps its signature (director call sites unchanged) but resolves through the table: event class first (match signal), then (act, quadrant) for user turns, falling back to watching default. Quadrants from the existing affect thresholds (mirrors client tierFor). The 4 previously ignored acts get rows (ActReact quadrant-colored; ActAsk thinking/think; ActRepair sad/agree −0.3 energy; ActAcknowledge unchanged).

## Contract tests (the invariant is CI-enforced)

1. **Inventory completeness** (`presentation_vocabulary_test.go` extension): every motion/expression key in presentation-map.json is referenced by ≥1 table row OR listed in the `phases`/`delivery` sections owned by the client; every table row's expression/motion exists in the JSON and in the model asset. No route ⇒ test fails.
2. **Three-surface consistency**: the Dart `expressionIndices`/`motionVariants` and the two JS exprMaps must equal the JSON (Dart test reads the asset JSON; JS checked via node script `scripts/check-presentation-map.mjs` in the offline tier).
3. **Non-empty binding**: expression index 0 resolves to the empty expression file — the table must never bind a non-neutral name to it (`thinking` moves to 3).
4. Behavioral: IntentUnknown turn ⇒ confused/listening one-shot; fulltime ⇒ wave; session_opened plain path delivers hello (bug fix); interrupted delivery ⇒ confused one-shot.

## Ownership rules (C3)

- **Backend authoritative** from presentation delivery until HoldMS elapses: JS `applyMouth`/motion calls must not override a held presentation.
- **Client owns** turn phases (listen/speak/think/understanding) and idle tiers at all times outside the hold window; the JS `scheduleIdle` timer only fires when phase == idle and no presentation is held.
- **ReturnMode semantics**: `watching` = hold then stay on watching/focus; `decay_to_focus` = return to focus; `decay_to_listening` = voice session waiting for the user (pairs with phase listen); `decay_to_idle` = quiet stretch, C4 tier picker takes the body.
- **Delivery reaction**: user interruption (scheduler preempt during playback) ⇒ one-shot confused/listening, then the interrupted user turn proceeds. Skipped/failed stay text-fallback only (no reaction) in v1.

## Open point for implementation

- Which real face each of expression5–7 renders is unknown until the device naming pass; the JSON's provisional bindings for surprised/angry (5/6) are guesses flagged for that pass. Re-binding is a one-line JSON edit.
- The `speaking` flag in live2d.html already gates canInterrupt; the new phase table must respect it (no listen motion while TTS audio is playing).
