# Tasks

## Product Definition

- [x] Finalize the first operator event set.
- [x] Decide first route name, e.g. `/operator.html` or `/admin/match`.
- [x] Decide whether the first version is local-only or password-protected.

## Backend

- [x] Add event create/correct endpoints.
- [x] Add match state update on accepted events.
- [x] Add WebSocket broadcast for `match_event`.
- [x] Add basic operator error responses for invalid events.

## Frontend

- [ ] Build the operator panel layout from approved Stitch design.
- [x] Add match header, score controls, and clock controls.
- [x] Add pre-match home/away lineup entry for faster player selection.
- [x] Add event-type-specific participant slots for multi-player events.
- [x] Add quick event buttons.
- [x] Add detailed event templates for common football moments.
- [x] Replace single dropdown templates with a searchable template chip bank.
- [x] Split home-team and away-team event entry panels.
- [x] Add match clock `+1'` and `-1'` controls.
- [x] Add structured event composer.
- [x] Add live event timeline with correction flow.
- [ ] Add companion preview for action and speech intent.

## QA

- [x] Test a full match event flow from operator API to frontend companion.
- [x] Verify fast event entry can be completed in under 3 seconds.
- [x] Verify correcting a goal updates score and timeline consistently.
