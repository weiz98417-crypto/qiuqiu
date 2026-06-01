# Tasks

## Event Consumption

- [x] Subscribe frontend to `match_event` messages.
- [ ] Add a local reaction queue.
- [ ] Add priority and cooldown rules.

## Live2D Integration

- [x] Map event types to existing `performAction(action)` calls.
- [ ] Add variant selection to reduce repeated visuals.
- [ ] Add quiet reactions for low-priority events.

## AI/Speech

- [x] Decide which events generate proactive speech.
- [x] Add event-aware prompt context for proactive lines.
- [ ] Add user setting to reduce proactive chatter if needed.

## QA

- [x] Test goal, VAR, penalty, miss, and tactical shift flows.
- [ ] Verify user chat remains usable during reactions.
- [ ] Verify long chat does not hide the character.
