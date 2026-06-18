## 1. Demo Baseline

- [x] 1.1 Document the canonical Spain vs Germany demo scenario.
- [x] 1.2 Choose the memory-mode reset strategy: guarded demo reset endpoint or unique demo match ID.
- [x] 1.3 Add a UTF-8 safe seed/reset mechanism for the demo match.
- [x] 1.4 Ensure the seed creates config, players, goal event, participants, score, and proactive text.
- [x] 1.5 Ensure re-running the seed produces predictable state without stale polluted events.

## 2. Browser Stability

- [x] 2.1 Investigate the user app `MutationObserver` console error; no project source calls `MutationObserver`, and the only browser log observed was stale from 2026-06-16 rather than a current blocking app error.
- [x] 2.2 Verify the user page shows connected status and Live2D iframe.
- [x] 2.3 Verify the operator live, setup, settings, and traces hash routes load.
- [x] 2.4 Verify goal templates render after selecting the goal event type.
- [x] 2.5 Verify the operator context preview includes match status, realtime event, and generation task.

## 3. End-to-end Demo Smoke

- [x] 3.1 Add or document a smoke flow for health check.
- [x] 3.2 Add or document a smoke flow for publishing the canonical goal.
- [x] 3.3 Add or document a smoke flow for asking "刚才谁助攻？".
- [x] 3.4 Assert the reply mentions 法比安 and 亚马尔.
- [x] 3.5 Assert trace output includes intent, tool call, retrieved event ID, output, reason, and timestamp.
- [x] 3.6 Assert Chinese match facts and user questions do not become `???`.

## 4. Runbook

- [x] 4.1 Add a local demo runbook with user, operator, setup, settings, and traces URLs.
- [x] 4.2 Document how to recover when port 8080 is already occupied.
- [x] 4.3 Document the Windows compiled-binary blocking workaround using `go run`.
- [x] 4.4 Document that no external ASR/TTS/sports data keys are required for this demo.
- [x] 4.5 Document the chosen memory reset strategy and dirty-state recovery path.

## 5. Verification

- [x] 5.1 Run backend tests with `go test ./cmd/server ./internal/...`.
- [x] 5.2 Run the browser demo smoke manually or with browser automation.
- [x] 5.3 Capture final evidence in the change notes before commit.
