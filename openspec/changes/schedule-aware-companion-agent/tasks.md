# Schedule-Aware Companion Agent Tasks

## 1. Intent and Context

- [x] Define `football_schedule` intent DTO with `action`, `scope`, competition, and confidence.
- [x] Replace complete-phrase enumeration with a semantic classifier seam and bounded deterministic fallback.
- [x] Preserve score, clock, event, and player-status fact guards before schedule routing.
- [x] Add `current`, `today`, `tomorrow`, and `nearby` scope resolution using the user's timezone.
- [x] Add tests for natural variants, mixed wording, and ambiguous date scope.

## 2. Current Match Grounding

- [ ] Extend the public match snapshot with competition and freshness when available.
- [ ] Add an active-match predicate covering in-progress, paused, stale, conflicted, and finished states.
- [x] Make current snapshot lookup precede external schedule search for `current` and `nearby` requests.
- [ ] Add regression coverage for Spain–Germany at 1-0 and for stale/conflicted snapshots.

## 3. Search Sources

- [x] Replace `TodayFixtures`-only access with a typed date-range `schedule.search` contract.
- [x] Extend the structured football provider adapter to today and tomorrow in the user's timezone.
- [x] Normalize competition, kickoff, lifecycle, score, source, and freshness fields.
- [ ] Add a configurable Web Search adapter for discovery-only results.
- [ ] Reject incomplete or conflicting search results instead of fabricating a fixture.
- [ ] Add source timeout, quota, empty-result, and credential-missing tests.

## 4. Progressive and Proactive Delivery

- [x] Add a lookup ID and pending lookup state to the conversation session.
- [x] Emit an immediate acknowledgement when external search is required.
- [x] Schedule a result announcement through the existing proactive WebSocket delivery path.
- [x] Link acknowledgement and result traces and expose `schedule_lookup` as the delivery source.
- [x] Cancel, deduplicate, and expire pending lookups on user speech, reconnect, or context changes.
- [x] Add browser tests that assert two ordered `qiuqiu_reply` messages.

## 5. Reminder Offer

- [ ] Add a reminder proposal response that does not mutate state.
- [ ] Add explicit-confirmation parsing and an idempotent `reminder.create` tool.
- [ ] Persist fixture ID, reminder time, timezone, source, and cancellation key.
- [ ] Add confirmation, ambiguity, duplicate, and cancellation tests.

## 6. Observability and Rollout

- [ ] Add lookup scope, source, freshness, duration, and delivery state to traces.
- [x] Keep provider credentials and raw search payloads out of user-facing output.
- [x] Add eval cases for live context, today, tomorrow, no-data, provider failure, interruption, and stale results.
- [x] Document the required provider configuration and local fallback behavior.
- [x] Gate rollout behind the existing WebSocket and fact-safety eval suites.
