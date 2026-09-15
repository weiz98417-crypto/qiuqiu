# Design: Agent Depth

## Decisions locked (grilling round 1)

| # | Decision | Choice |
| --- | --- | --- |
| 1 | Memory division | Ledger = immutable fact stream (untouched); Memobase = mutable synthesis (portrait, insights) behind an in-domain interface |
| 2 | Write path | Asynchronous queue; observation never blocks a watch turn |
| 3 | Proactivity scope | In-session only, never push; frequency gated by the (newly wired) talkativeness setting |
| 4 | C4 scope | Whitelist expansion + real lip sync + idle tiers, all in v1 |
| 6 | Interface | Single draft (below), no design-it-twice |

## C1/C3 · The memory seam (ADR-0006)

```go
// backend/internal/memory — in-domain seam; Memobase sits behind it and is swappable.
type Memories interface {
    Observe(ctx context.Context, m Moment) error      // enqueue; async flush to Memobase
    Recall(ctx context.Context, q Query) []Recall     // recency × importance × relevance top-k
    Portrait(ctx context.Context, u UserID) Portrait  // synthesized user portrait (C3)
    Threads(ctx context.Context, u UserID) []Thread   // Open Thread ledger (C2; local table)
}
```

- **Division of labor**: the Interaction Ledger keeps recording immutable facts (turns, match events, delivery outcomes). Memobase stores mutable synthesis: the user profile and event summaries. Nothing deletes from the Ledger; Memobase entries always cite a Ledger sequence for provenance.
- **Write path**: turn ends → Ledger append (unchanged, ms) → async queue → Memobase insert (LLM extraction happens here; retries with backoff; every accepted/rejected extraction gets a reason code persisted locally for audit — the fact-first culture applies to memory too).
- **Read path**: `agent.go` context assembly replaces/augments `conversation.read_recent` with `Recall` (top-k) + `Portrait`. The realizer receives the portrait block; ForbiddenClaims/RequiredAnchors discipline unchanged.
- **Reflection**: scheduled beat (post-match + idle) synthesizes insights ("user cares about midfield playmakers") — stored as profile entries, cited by later turns.
- **Degradation**: Memobase unreachable → observe queue drains to local disk/backlog table, system runs Ledger-only (read_recent fallback). No user-visible error.

**Infra**: `docker-compose.yml` gains `memobase-server` (config points its extraction LLM at the same endpoint/key env the backend adapter uses). QiuQiu talks to it via the official Go SDK.

## C2 · Open Threads + planner beats

- **Table** (local Postgres): `open_threads(id, user_id, kind{unanswered_question, promise, emotional_moment, prediction}, content, created_at, state{open, addressed, expired}, source_turn)`.
- **Writers**: turn pipeline inserts candidates (question marks without answers, promises "待会儿告诉你", strong emotional exchanges); match events link predictions.
- **Planner beats**: (a) in-match — existing `proactive_gate` additionally requires a thread/moment citation; (b) post-match — recover open threads ("上一场你问谁助攻的——法比安"); (c) idle reflection — feeds C1 reflection.
- **Talkativeness wiring**: parse the setting in `main.go:861-875` (fixes drift), map quiet/normal/active → planner frequency caps + `InitiativeMode`; policy tests must cover all three tiers.
- **Hard boundary**: in-session only. No push, no notifications, no out-of-session side effects (L3, separate change).

## C4 · Body

- **Whitelist**: `presentation_state.dart` 7 → 12 motion groups / 17 motions (hello, idle×3, listen×2, speak×2, think, celebrate×2, miss, complain, analysis, tense, agree, wave); alias table gains entries per new group; backend `presentationFor` vocabulary mapping extended to emit them on matching policy states.
- **Lip sync**: wLipSync (WASM, MFCC → visemes) + pixi-live2d-display lipsync patch drive `ParamMouthOpenY` from the TTS audio, replacing random jitter at `live2d_view.dart:375-385`. **Key integration decision (open point for implementation):** if TTS audio plays natively in Flutter, route it into the WebView's WebAudio context so wLipSync can tap it; fallback is a pre-computed viseme timeline if the TTS provider exposes phoneme timestamps.
- **Idle tiers**: affect vector → idle selection: deflated (low arousal/valence), calm (mid), energetic (high). Idle re-picks every N seconds with hysteresis; physics already handles secondary motion.

## Evals (per repo tier culture)

- C4: whitelist unit tests (13+ expressions / 12 groups), alias coverage, Go-side presentation mapping tests; manual visual pass.
- C1: memory recall eval — seed conversation → N turns later assert callback ("你上次说…"); degradation test (Memobase down ⇒ Ledger-only); extraction audit log test.
- C3: portrait injection eval (persona-consistent reply references profile facts); privacy: portrait delete ⇒ absent from context.
- C2: gate unit tests (no citation ⇒ no proactive turn); talkativeness 3-tier frequency tests; "隔轮补答" journey eval.

## Docs

- ADR-0006 (memory seam + Memobase adapter + async writes + degradation) — authored with this change.
- Root `CONTEXT.md` gains: Reflection（反思）, Portrait（用户画像）; Open Thread is already defined and now gains an implementation.
- `docs/球球课程/chapters/31-initiative-silence` and `57-evals-golden-set` anchors updated after implementation (status flags move to implemented-at).
