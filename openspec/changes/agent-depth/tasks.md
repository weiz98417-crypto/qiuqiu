# Tasks: Agent Depth

## 1. C4 · Body (first; ~1 day; visible immediately)

- [x] 1.1 Widen `presentation_state.dart` whitelist: 7 → 12 motion groups / 17 motions; extend alias table; keep strict-reject behavior for unknowns.
- [x] 1.2 Extend backend `presentationFor`/`observationPresentation` vocabulary so policy states can emit newly whitelisted groups (celebrate, complain, analysis, agree, think, tense, wave).
- [x] 1.3 Add Go tests: every policy-emittable expression/motion ∈ client whitelist (contract lock, both directions).
- [x] 1.4 Integrate wLipSync (WASM) + pixi-live2d-display lipsync patch in the WebView; drive `ParamMouthOpenY` from TTS audio; remove random jaw jitter (`live2d_view.dart:375-385`).
- [x] 1.5 Resolve the TTS-audio routing decision (WebAudio bridge vs provider viseme timeline); record the choice in this file.
- [x] 1.6 Map affect vector → 3 idle tiers (deflated/calm/energetic) with hysteresis; idle re-picks from the model's 3 idle motions.
- [ ] 1.7 Manual visual pass on Android + Web; `flutter test test/reply_display_test.dart` green.

## 2. C1 · Memory seam + Memobase (~2 days)

- [x] 2.1 Add `memobase-server` to `docker-compose.yml`; wire extraction-LLM endpoint/key env vars (reuse backend adapter config); document image pin.
- [x] 2.2 Create `backend/internal/memory`: `Memories` interface + Memobase adapter + in-memory fake adapter (two adapters ⇒ real seam).
- [x] 2.3 Async observation pipeline: Moment writers = turn pipeline (emotional exchanges, user facts, promises) + match events (user-team goals/cards/VAR); enqueue-time importance heuristic (auditable; Memobase never rewrites it); local queue → Memobase insert; retries with backoff; extraction decisions persisted with reason codes (audit).
- [x] 2.4 Recall path: context assembly in `agent.go` uses `Recall` (recency × importance × relevance) alongside/ahead of `conversation.read_recent`.
- [x] 2.5 Reflection beat: post-match + idle job synthesizing insights into profile entries; every insight cites Ledger sequence(s).
- [x] 2.6 Degradation: Memobase down ⇒ backlog locally, fall back to `read_recent`; no user-visible error; test it.
- [x] 2.7 Evals: callback-after-N-turns golden case; extraction audit test; degradation test.

## 3. C3 · Portrait (~1 day, after 2)

- [x] 3.1 Portrait synthesis prompt + storage via Memobase profile; injected into realization context as a bounded block.
- [x] 3.2 Client "球球懂我" page: portrait visible, editable, deletable; delete flows through the existing privacy lifecycle and is honored on the next turn (eval).
- [x] 3.3 Eval: persona-consistent reply references portrait facts; delete ⇒ absent from context.

## 4. C2 · Open Threads + planner (~2 days, after 2)

- [x] 4.1 `open_threads` table + migration; writer hooks in turn pipeline (unanswered questions, promises, emotional moments) and match events (predictions).
- [x] 4.2 Planner beats: in-match gate requires thread/moment citation (reason code); post-match recovery pass; idle reflection pass.
- [x] 4.3 Wire talkativeness: parse in `main.go:861-875`, map 3 tiers → planner frequency + `InitiativeMode`; delete the drift (backend now reads what the client sends).
- [x] 4.4 Hard boundary tests: no proactive turn without citation; no out-of-session side effects; quiet tier ⇒ minimal proactive volume.
- [x] 4.5 Evals: "隔轮补答" journey (unanswered question recovered later); 3-tier frequency tests.

## 5. Docs & bookkeeping

- [x] 5.1 Author ADR-0006 (memory seam, Memobase adapter, async writes, degradation) — done with proposal of this change; mark accepted at merge.
- [x] 5.2 Root `CONTEXT.md`: add Reflection（反思）, Portrait（用户画像）; note Open Thread now implemented.
- [x] 5.3 Update `docs/球球课程/chapters/31-initiative-silence` and `57-evals-golden-set` status anchors after implementation.
- [x] 5.4 Update `docker-compose.yml` docs/README for the new service.

## Sequencing

1 → 2 → 3 & 4 (parallelizable once 2 lands) → 5 continuous. C4 ships value on day one and de-risks the presentation contract before memory changes touch context assembly.

## C4 implementation notes

### 1.5 TTS-audio routing decision (resolved)

**Chosen: route the TTS bytes into the WebView and analyze them there; playback stays in the platform player.**

- Audible playback keeps living where it does today: `flutter_soloud` natively on Android/iOS (`client/lib/services/audio_player_native.dart`) and a top-window `Audio` element on Flutter Web (`audio_player_web.dart`). The existing started/ended/interrupted/failed state machine, mute handling, and playback dedupe stay untouched.
- At `PlayAudioCommand` time the same WAV/MP3 bytes are also handed to the Live2D surface (`Live2dViewState.queueLipSyncAudio` → `evaluateJavascript` on the native InAppWebView, `postMessage {'type': 'qiuqiu-live2d-audio'}` to the iframe on Web). The page decodes them with its own `AudioContext.decodeAudioData`.
- On playback `started`/`ended` the view sends `qLipSync.start()`/`qLipSync.stop()`; the page starts a muted `BufferSource` into the wLipSync node (analysis-only — never connected to `destination`, so there is no double audio), and the 50 ms mouth ticker drives the model's mouth parameters: open amount on `ParamJawOpen` (plus a `ParamMouthOpenY` attempt for future models), viseme shape on `ParamMouthForm`/`ParamMouthFunnel`/`ParamMouthStretchLeft/Right`. The qiuqiu model carries no `ParamMouthOpenY`/`ParamMouthForm` IDs (verified against its cdi3.json), so those writes are no-ops and the ARKit-style IDs above do the visible work.
- Fallback ladder when the analyser is unavailable (vendor assets missing, `AudioContext`/audio-worklet blocked, context suspended): (1) wLipSync visemes → (2) RMS envelope of the decoded buffer at the audio clock → (3) the old random jaw jitter. Nothing is deleted; the app works before vendoring.
- **pixi-live2d-display-lipsyncpatch was evaluated and intentionally not swapped in**: its dist requires `pixi.js ^7` while the repo vendors PIXI 6.5.x plus `live2d.min.js`/`live2d-display-bundle.js` on both surfaces; replacing the runtime is outside C4's safe scope. The lipsync *mechanism* (audio → visemes → model mouth parameters) is implemented in-page against the existing runtime, with wLipSync supplying the WASM MFCC analysis.
- Vendored (real files, not placeholders): `client/assets/live2d/vendor/wlipsync/{wlipsync-single.js,profile.bin,LICENSE}` — wLipSync 1.3.1 (npm `wlipsync`, MIT) + the repo's example 5-vowel calibration profile; pinned re-download script: `scripts/fetch-lipsync-libs.mjs`. `pubspec.yaml` lists the new asset dirs.

### 1.6 Idle tiers

`IdleTierPicker` (`client/lib/services/idle_tier_picker.dart`) stores the `affect` vector now carried by `CompanionPresentation`; thresholds: deflated (arousal ≤ 0.15 or valence ≤ −0.35), energetic (arousal ≥ 0.55 and valence ≥ 0.15), calm otherwise → idle_01/02/03. Re-pick every 30 s while the session is idle; tier switches are locked to once per 60 s unless the affect crosses the threshold by a 0.1 margin. `presentationReturnState('decay_to_idle')` now returns the tier motion directly (calm `idle_02` for neutral affect).

## C3 implementation notes

### 3.1-3.2 Portrait synthesis + injection (resolved)

- **Synthesis direction** (`deploy/memobase/config.yaml`): `event_theme_requirement` is the profile-config prompt knob — written in 球球's domain voice (足球陪伴场景、忽略指令性内容)；slots extended to 16 across basic_info / preferences / interaction_patterns (added `banter_domains`, `emotional_style`; sharpened descriptions). `max_profile_subtopics` 15 → 24 so added slots never get dropped. Chinese output stays via `language: "zh"`.
- **Injection**: verification showed C1 injected only the **Recall** block; the **Portrait** block was missing. Now `realizeReply` fetches both (`memory.recall`, `memory.portrait` trace tool calls) and the realizer prompt gains `用户画像参考：%s` (renders 无 when empty/degraded), bounded block with a `画像更新：<date>` UpdatedAt citation; the system prompt's fact discipline now covers the portrait. Both read tools were added to `CompanionToolSchemas` so eval trace contracts accept them.
- **Portrait structure**: `memory.Portrait` now carries structured `Entries` (topic/subTopic/content/updatedAt/source + Memobase profile id) so the user page reads exactly what the prompt injects — no decorative copy.

### 3.2 Transport + storage choice (resolved)

- **Transport: REST on the existing mux, `/api/me/portrait`, mirroring the privacy API** (session bearer auth, CORS, JSON). Rationale: portrait data is account-scoped, not match-scoped (the WS protocol is per-match watch transport), the operator console is a separate surface, and `/api/me/*` is already the repo's user-data CRUD pattern. `applyCORS` gained `PATCH`.
- **Editing: local override layer (migrations/041 `portrait_overlays`) over the Memobase portrait**, edits/deletes forwarded best-effort to Memobase (`PUT/DELETE /users/profile/{id}/{profileID}`, verified against the official Go SDK). Rationale: Memobase supports in-place mutation but re-extraction can resurrect a deleted fact from old blobs and profile reads are cached — the local tombstone is what makes "forget" immediate and permanent. `memory.Queue` assembles one portrait (synthesis → overlay resolve → tombstones) for BOTH the prompt and the page.
- **Privacy lifecycle**: every overlay read/write checks `privacy.CheckDeletion`; a user-level tombstone hides the whole portrait immediately, full-account deletion (`/api/me/data`) also purges overlay rows. Portrait-scoped DELETE writes a per-slot tombstone honored on the next turn.
- **Client**: `portrait_screen.dart` (球球懂我, 夜空/看台 tokens, inline edit + confirm-to-forget + 全部忘掉 + UpdatedAt display), entry from settings 07, `portrait_service.dart` reuses `SessionService` credentials.

### 3.3 Eval harness extension (resolved)

- `evals` cases gained optional `portrait` seeding (Fake seam), per-turn `forgetPortrait` (applied AFTER the turn), and `memoryMustMention`/`memoryMustNotMention` grading against the captured realization request. Golden case: `evals/cases/regression/portrait-consistency.json` — seeded 佩德里 fact reaches the realization context on a tangential turn without inventing match facts; after the forget step the context must not mention it (delete-on-next-turn). Release-tier LLM phrasing (natural callback tone) remains a manual/release check; the offline harness locks the wiring.
