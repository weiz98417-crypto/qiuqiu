## 1. Demo Runbook

- [x] 1.1 Update the runbook with the final 10-minute demo script.
- [x] 1.2 Include user app, director live, setup, settings, and trace URLs.
- [x] 1.3 Document MiMo key setup without storing secrets.
- [x] 1.4 Document no external sports-data key is required.
- [x] 1.5 Add troubleshooting for port conflicts, browser microphone denial, ASR failure, TTS failure, and stale demo state.

## 2. Demo Scripts

- [x] 2.1 Verify `demo-seed` creates the canonical scenario.
- [x] 2.2 Verify `demo-smoke` covers health, state, user follow-up, and trace output.
- [x] 2.3 Verify `voice-ui-smoke` covers voice UI affordances.
- [x] 2.4 Verify `mimo-voice-smoke` covers real MiMo ASR/TTS when `MIMO_API_KEY` is set.
- [x] 2.5 Add a single documented command sequence for pre-demo verification.

## 3. Customer Materials

- [x] 3.1 Update the project introduction to reflect director-console facts and MiMo voice.
- [x] 3.2 Add an architecture diagram for director facts -> agent memory -> voice output -> trace.
- [x] 3.3 Add a plain-language value summary for enterprise customers.
- [x] 3.4 Remove or de-emphasize obsolete external sports-data language from customer-facing materials.

## 4. Acceptance Checklist

- [x] 4.1 Add checklist for text-only demo.
- [x] 4.2 Add checklist for voice-enabled demo.
- [x] 4.3 Add checklist for trace explanation.
- [x] 4.4 Add checklist for correction/authority story.

## 5. Final Verification

- [x] 5.1 Run `go test ./cmd/server ./internal/...`.
- [x] 5.2 Run `node ./scripts/demo-seed.mjs`.
- [x] 5.3 Run `node ./scripts/demo-smoke.mjs`.
- [x] 5.4 Run `node ./scripts/voice-ui-smoke.mjs`.
- [x] 5.5 Run `node ./scripts/mimo-voice-smoke.mjs` when `MIMO_API_KEY` is available.
- [x] 5.6 Verify no real secrets are present in docs, tests, OpenSpec, or source files.
