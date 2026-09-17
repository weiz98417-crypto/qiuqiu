# Director Rewrite: Split operator.html#live and Merge into the Console

## Why

`operator.html` is a 4,076-line zero-build single file whose surviving core (`#live` 实战导演) carries the heaviest operational surface: the 18-event draft model (roles/actions/intensity in `operator-live-state.js`), draft forms with fact-status/score-correction/manual-proactive channels, the fact timeline, and the voice-capture → ASR-draft → conflict-resolution → publish flow. The rest of the console (sources/automation/monitor/traces) has already migrated to the React console — `#live` is the last legacy survivor, linked from the console nav.

The 28 operator-control evals exercise this page heavily and are the safety net for a componentized rewrite.

## What Changes

- **Event model port**: `operator-live-state.js`'s 18 event definitions → `console/src/director/event-model.ts` (TypeScript data module, pure port; eval snapshot guards the port).
- **Director page**: `console/match/:id/director` — antd-componentized: event draft cards, fact-status select, score correction + reason, manual proactive channel (auto/quiet/manual), templates + preview, fact timeline (proactive line visible), voice capture → drafts flow, busy states on all writes.
- **Coexistence**: the new page lives at the console route behind a "新版实战导演" link; legacy `operator.html#live` stays usable until parity is signed off; after sign-off the link flips to default and `operator.html` is deleted (final task of the retirement wave).

## Non-goals

- No new backend routes (all director writes/reads exist); no voice engine changes; no fact-discipline changes.
- The rewrite does not proceed to retirement until: 28 evals green, parity checklist signed off by the director (user), and one live-match dress rehearsal completed.
