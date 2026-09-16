# Tasks: Presentation Mapping

## 1. C2 · Single source + backend table

- [ ] 1.1 Author `client/assets/live2d/models/qiuqiu/presentation-map.json` per design.md (expressions/motions/acts/events/phases/delivery).
- [ ] 1.2 Replace `presentationFor` if-else chain with the `presentationRow` table lookup (event class → (act, quadrant) → default); director call sites unchanged.
- [ ] 1.3 Give the 4 mute acts rows: ActReact quadrant-colored, ActAsk thinking/think, ActRepair sad/agree (−0.3 energy), ActAcknowledge chat/speak.
- [ ] 1.4 Re-bind `thinking` → expression3 (provisional); add non-empty-binding contract check (never bind a non-neutral name to the empty expression file).
- [ ] 1.5 Fix the discarded hello: plain `session_opened` path delivers the computed presentation (main.go:946 `_, err :=` site).
- [ ] 1.6 Inventory-completeness contract test: every JSON key routed by ≥1 row or client-owned; every row resolvable in JSON + model asset; **and the exact-set assertion both ways — the JSON expression/motion key sets must equal the client whitelist sets (an extra key like a motion name in the expression table is a failure)**.
- [ ] 1.7 Update the two existing companion vocabulary tests for the new table shapes.
- [ ] 1.8 Behavioral test (design contract-test #4): IntentUnknown turn ⇒ confused/listening one-shot reaction; ActDisagree strong cue (personal_insult_rejected) ⇒ angry/complain.

## 2. C1 · Client phase table

- [ ] 2.1 `match_session_controller`: formalize phase → performance table (user_speaking/understanding/qiuqiu_speaking/session_open/match_end) reading presentation-map.json; replace the scattered focus/thinking hardcodes.
- [ ] 2.2 fulltime ⇒ happy/wave one-shot farewell (new; event already classified critical).
- [ ] 2.3 live2d.html + live2d_view.dart: exprMap loaded from presentation-map.json (delete the diverging inline maps); keep the lipsync driver untouched.
- [ ] 2.4 `scripts/check-presentation-map.mjs`: node consistency check (JSON vs JS-consumed keys) wired into the offline tier.
- [ ] 2.5 Dart contract test extension: expressionIndices/motionVariants read from the JSON instead of hand-mirrored constants; delete the drifted copies.

## 3. C3 · Ownership + delivery reaction

- [ ] 3.1 Hold-window rule: live2d.html scheduleIdle and motion overrides skip while a presentation hold is active (HoldMS) and phase != idle.
- [ ] 3.2 ReturnMode: backend emits all four values at the right beats (decay_to_listening on voice-session wait; decay_to_idle on quiet stretch); `presentationReturnState` maps each to its real target (no more both→focus).
- [ ] 3.3 Delivery interrupted ⇒ one-shot confused/listening before the preempting user turn proceeds (uses scheduler preempt signal; delivery.go observer).
- [ ] 3.4 Tests: gate/interrupt reaction (Go), return-mode mapping (Dart), scheduleIdle yielding (JS checked via node).
- [ ] 3.5 E2E (Playwright): unknown-intent journey — send unclassifiable text, assert confused/listening reaction surfaces and the deterministic fallback reply still shows.
- [ ] 3.6 E2E (Playwright): fulltime farewell journey — drive a match to fulltime via the demo/operator path, assert wave motion fires once and idle resumes.
- [ ] 3.7 E2E (Playwright, fake media): phase motions — with --use-fake-device-for-media-stream, assert listening motion during VAD speech and speak during playback; idle tier asserted via the live2d state bridge.

## 4. Docs & bookkeeping

- [ ] 4.1 ADR-0007: presentation mapping table, single-source JSON, inventory-completeness invariant, ownership rules (authored with this change).
- [ ] 4.2 Root CONTEXT.md: add Turn Phase（表演相位）term.
- [ ] 4.3 Refresh `docs/球球课程/chapters/32-emotion-presentation` and `33-immersive-stage` status anchors after implementation.
- [ ] 4.4 Device checklist: visual pass over 7 expressions (name them), the 17 motions, lipsync mouth params; re-bind surprises in presentation-map.json (one edit).

## Sequencing

1 → 2 → 3 → 4. C2 lands the table and kills the three bugs; C1 and C3 consume the same JSON. All work on `feat/agent-depth`.
