# Tasks: Agent Depth

## 1. C4 · Body (first; ~1 day; visible immediately)

- [ ] 1.1 Widen `presentation_state.dart` whitelist: 7 → 12 motion groups / 17 motions; extend alias table; keep strict-reject behavior for unknowns.
- [ ] 1.2 Extend backend `presentationFor`/`observationPresentation` vocabulary so policy states can emit newly whitelisted groups (celebrate, complain, analysis, agree, think, tense, wave).
- [ ] 1.3 Add Go tests: every policy-emittable expression/motion ∈ client whitelist (contract lock, both directions).
- [ ] 1.4 Integrate wLipSync (WASM) + pixi-live2d-display lipsync patch in the WebView; drive `ParamMouthOpenY` from TTS audio; remove random jaw jitter (`live2d_view.dart:375-385`).
- [ ] 1.5 Resolve the TTS-audio routing decision (WebAudio bridge vs provider viseme timeline); record the choice in this file.
- [ ] 1.6 Map affect vector → 3 idle tiers (deflated/calm/energetic) with hysteresis; idle re-picks from the model's 3 idle motions.
- [ ] 1.7 Manual visual pass on Android + Web; `flutter test test/reply_display_test.dart` green.

## 2. C1 · Memory seam + Memobase (~2 days)

- [ ] 2.1 Add `memobase-server` to `docker-compose.yml`; wire extraction-LLM endpoint/key env vars (reuse backend adapter config); document image pin.
- [ ] 2.2 Create `backend/internal/memory`: `Memories` interface + Memobase adapter + in-memory fake adapter (two adapters ⇒ real seam).
- [ ] 2.3 Async observation pipeline: turn-end hook → local queue → Memobase insert; retries with backoff; extraction decisions persisted with reason codes (audit).
- [ ] 2.4 Recall path: context assembly in `agent.go` uses `Recall` (recency × importance × relevance) alongside/ahead of `conversation.read_recent`.
- [ ] 2.5 Reflection beat: post-match + idle job synthesizing insights into profile entries; every insight cites Ledger sequence(s).
- [ ] 2.6 Degradation: Memobase down ⇒ backlog locally, fall back to `read_recent`; no user-visible error; test it.
- [ ] 2.7 Evals: callback-after-N-turns golden case; extraction audit test; degradation test.

## 3. C3 · Portrait (~1 day, after 2)

- [ ] 3.1 Portrait synthesis prompt + storage via Memobase profile; injected into realization context as a bounded block.
- [ ] 3.2 Client "球球懂我" page: portrait visible, editable, deletable; delete flows through the existing privacy lifecycle and is honored on the next turn (eval).
- [ ] 3.3 Eval: persona-consistent reply references portrait facts; delete ⇒ absent from context.

## 4. C2 · Open Threads + planner (~2 days, after 2)

- [ ] 4.1 `open_threads` table + migration; writer hooks in turn pipeline (unanswered questions, promises, emotional moments) and match events (predictions).
- [ ] 4.2 Planner beats: in-match gate requires thread/moment citation (reason code); post-match recovery pass; idle reflection pass.
- [ ] 4.3 Wire talkativeness: parse in `main.go:861-875`, map 3 tiers → planner frequency + `InitiativeMode`; delete the drift (backend now reads what the client sends).
- [ ] 4.4 Hard boundary tests: no proactive turn without citation; no out-of-session side effects; quiet tier ⇒ minimal proactive volume.
- [ ] 4.5 Evals: "隔轮补答" journey (unanswered question recovered later); 3-tier frequency tests.

## 5. Docs & bookkeeping

- [ ] 5.1 Author ADR-0006 (memory seam, Memobase adapter, async writes, degradation) — done with proposal of this change; mark accepted at merge.
- [ ] 5.2 Root `CONTEXT.md`: add Reflection（反思）, Portrait（用户画像）; note Open Thread now implemented.
- [ ] 5.3 Update `docs/球球课程/chapters/31-initiative-silence` and `57-evals-golden-set` status anchors after implementation.
- [ ] 5.4 Update `docker-compose.yml` docs/README for the new service.

## Sequencing

1 → 2 → 3 & 4 (parallelizable once 2 lands) → 5 continuous. C4 ships value on day one and de-risks the presentation contract before memory changes touch context assembly.
