# Tasks

## Schema

- [x] Add a shared match event type/schema in the backend.
- [x] Add validation for required fields and allowed enum values.
- [x] Add revision fields: `revisionOf`, `status`, `updatedAt`.
- [x] Add source fields: `source`, optional `providerName`, optional `operatorId`.
- [x] Add multi-participant event fields while keeping `playerName` compatibility.

## Storage

- [x] Choose the first storage mechanism for local MVP events.
- [x] Persist event timeline by `matchId`.
- [x] Add read APIs for latest events and full timeline.
- [x] Add create/correct APIs for operator-authored events.

## Mapping

- [x] Define event type to companion intent mapping.
- [x] Define event type to default Live2D action mapping.
- [x] Define intensity rules for speech, motion, and effects.

## Verification

- [ ] Add unit tests for schema validation.
- [ ] Add API tests for create, read, and correction flows.
- [x] Verify invalid events are rejected with readable errors.
