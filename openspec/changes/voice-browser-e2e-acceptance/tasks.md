## 1. Browser Voice Runbook

- [x] 1.1 Document the exact browser voice acceptance steps.
- [x] 1.2 Document how to seed the canonical match before voice testing.
- [x] 1.3 Document how to configure `MIMO_API_KEY`, `MIMO_MODEL`, and `MIMO_VOICE` without writing secrets to files.
- [x] 1.4 Document expected browser permission prompts and what the tester should click.

## 2. Real Voice Happy Path

- [ ] 2.1 Verify microphone permission can be granted in the user app.
- [ ] 2.2 Verify the user can ask "刚才谁助攻？" by voice.
- [x] 2.3 Verify MiMo ASR text reaches the backend companion flow.
- [x] 2.4 Verify QiuQiu replies with active director facts.
- [ ] 2.5 Verify MiMo TTS audio plays in the browser.
- [ ] 2.6 Verify Live2D enters listening and speaking states during the flow.

## 3. Fallback Paths

- [ ] 3.1 Verify microphone denial leaves text chat usable.
- [x] 3.2 Verify ASR failure shows fallback and preserves WebSocket connection.
- [x] 3.3 Verify TTS failure shows text reply and does not block agent output.
- [x] 3.4 Verify browser playback failure returns Live2D to a safe state.

## 4. Evidence And Evals

- [x] 4.1 Add or update a browser voice acceptance smoke script for non-permission checks.
- [x] 4.2 Capture trace ID, ASR text, reply text, and TTS byte count for a successful run.
- [x] 4.3 Verify browser console has no current blocking recorder, playback, or WebSocket errors.
- [x] 4.4 Add a regression check that text chat still works after a failed voice attempt.
