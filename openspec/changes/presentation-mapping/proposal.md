# Presentation Mapping: Full-Inventory Routing & Ownership

## Why

The model ships 17 motions (12 groups) and 7 expression files; the client whitelist carries 13 expression names. An emission audit (2026-09-17) found the agent information does NOT reach most of this inventory:

- **Dead inventory**: listen ×2, think, wave have zero backend emission paths; 5 of 13 whitelisted expressions (idle, listening, confused, surprised, angry) are never emitted.
- **Mute acts**: ActReact, ActAsk, ActRepair, ActAcknowledge are emitted by policy but never alter the body — 4 of 8 communication acts have no physical language.
- **Real bugs**: `thinking` maps to expression1, an EMPTY parameter file (thinking renders no face); the default motion name "focus" plays the tense motion on the web surface but listen on the embedded mobile surface — one word, two bodies; the expression-name→file mapping exists in THREE diverging copies (web exprMap, mobile map, Dart expressionIndices) and the Dart copy is test-only.
- **Lost work**: the plain `session_opened` path computes the hello presentation and discards it (`_, err :=`).
- **Double ownership**: the JS idle scheduler re-plays idle every 8–14s and can silently override a backend presentation.

## What Changes

- **Single source**: `client/assets/live2d/models/qiuqiu/presentation-map.json` becomes the one mapping file — expression name → expression file index, motion name → group/variant, (Act × affect quadrant) → performance, match event class → performance, turn phase → performance. The web page, the embedded mobile page, and Dart constants all read/derive from it; Go contract tests lock the JSON, the Go table, and the model asset to each other.
- **Full-coverage invariant (the user's requirement)**: every one of the 17 motions and 13 expressions must be reachable through at least one mapping row or explicitly owned by the client phase table. A slot with no route = failing test. This retires all 9 dead slots:
  - `thinking` → expression3 (hands-on-hips provisional; device visual pass to confirm) — fixes the empty-face bug.
  - `confused` → IntentUnknown ("这句我没接明白") + delivery interrupted.
  - `surprised` → goal_cancelled / VAR overturn moments.
  - `angry` → ActDisagree strong tier (personal_insult_rejected).
  - `listening` / `idle` → client turn phases.
  - `listen ×2 / speak ×2` → client turn phases (user speaking / QiuQiu playing).
  - `think` → client understanding phase + ActAsk.
  - `wave` → fulltime farewell (new route on match end).
  - `hello` → fix the discarded session_opened path.
- **(Act × affect quadrant) table**: `presentationFor`'s if-else chain collapses into one table-driven pure function; the 4 previously ignored acts get bodies (ActReact → quadrant-colored react; ActAsk → thinking/think; ActRepair → sad/agree at reduced energy; ActAcknowledge → chat/speak unchanged).
- **Client phase table (C1)**: `match_session_controller`'s existing phase state machine becomes the single owner of turn-phase motions (user_speaking → listening/listen_01; understanding → thinking/think; qiuqiu_speaking → chat/speak_01; session_open → happy/hello; fulltime → happy/wave). Backend makes no phase-driven calls.
- **Ownership rules (C3)**: during HoldMS the backend presentation is authoritative; after ReturnMode elapses the client phase/idle layer owns the body; the JS idle scheduler only fills when no presentation is held AND phase is idle. ReturnMode all four values get real semantics (watch / decay_to_focus / decay_to_listening for voice-wait / decay_to_idle for quiet stretches). Delivery interrupted → one-shot confused/listening.

## Non-goals

- No new model assets; ADR-0005 unchanged — everything routes behind PresentationPlan.
- Mouth-parameter coloring during speech (Pucker on U, Close on consonant gaps, Smile/Frown tinted by affect — 25 mouth params track) is a SEPARATE follow-up change riding on the lipsync driver; this change covers the 30-slot motion/expression routing only.
- Full 7-expression visual naming audit is a device checklist item, not a blocker: provisional bindings ship, one JSON edit re-binds after review.
- No push notifications, no new backend information sources — routing only.
