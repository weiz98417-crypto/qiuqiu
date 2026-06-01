# Tasks

## State Engine

- [x] Build match state derivation from event timeline.
- [x] Store score, clock, period, recent events, and key events.
- [x] Add correction handling when an event is revised.

## AI Context

- [x] Add compact match context to chat prompts.
- [x] Add tone guidance for solo casual fans.
- [x] Ensure secrets and API keys are never included in prompts or logs.

## Frontend

- [x] Expose current match snapshot to the app.
- [x] Show lightweight match context in the user-facing UI if design approves.
- [ ] Keep Live2D visible as chat grows.

## Verification

- [ ] Test chat replies after a goal event.
- [ ] Test chat replies after a correction event.
- [ ] Test missing match state fallback.
